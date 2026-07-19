#!/usr/bin/env python3
"""docs-hygiene-daily.py — nightly /improve:docs sweep across active fleet repos.

Deterministic gate (this script), autonomous execution (claude -p), safe landing
(pushed review branch, never trunk). See docs/adr or the owning openspec change
for the full design; summary:

1. Read home/projects.toml (installfest's own registry) -- category in
   {"personal", "priceless"} only. "b-and-b" repos are NEVER touched by this
   script, full stop -- no flag, no override.
2. Per eligible repo with a local checkout: compare current HEAD against the
   last-processed SHA recorded in state. No diff since last run -> skip.
3. A repo with a diff gets a throwaway git worktree off whichever branch is
   currently checked out (main or dev), a headless `claude -p "/improve:docs"`
   run inside it (autonomous plan-selection -- see lens-shared.md), and, if
   that produced anything, a push of a `docs/<slug>` branch to origin for
   review at next session open. Nothing lands on trunk unattended.
4. Per-repo result recorded to a JSONL results log roadmap-pulse (cc) reads
   for its ambient digest row.

Exit 0 always (cron/systemd contract) -- failures are logged, not fatal to the
run as a whole; one repo's failure must not block the rest of the fleet.
"""

import json
import os
import re
import subprocess
import sys
import tomllib
from datetime import datetime, timezone
from pathlib import Path

HOME = Path.home()
INSTALLFEST_ROOT = Path(__file__).resolve().parent.parent
PROJECTS_TOML = INSTALLFEST_ROOT / "home" / "projects.toml"

STATE_DIR = HOME / ".local" / "state" / "docs-hygiene-daily"
LAST_RUN_FILE = STATE_DIR / "last-run.json"
RESULTS_FILE = STATE_DIR / "results.jsonl"
LOG_FILE = STATE_DIR / "run.log"

ELIGIBLE_CATEGORIES = {"personal", "priceless"}
ALLOWED_BRANCHES = {"main", "dev"}
CLAUDE_TIMEOUT_S = 1800  # 30 min ceiling per repo -- a stuck session must not wedge the fleet


def log(msg):
    line = f"{datetime.now(timezone.utc).isoformat()} {msg}"
    print(line, file=sys.stderr)
    try:
        STATE_DIR.mkdir(parents=True, exist_ok=True)
        with open(LOG_FILE, "a") as f:
            f.write(line + "\n")
    except OSError:
        pass


def run(cmd, cwd=None, timeout=60):
    try:
        return subprocess.run(
            cmd, cwd=cwd, capture_output=True, text=True, timeout=timeout
        )
    except subprocess.TimeoutExpired as e:
        log(f"TIMEOUT: {' '.join(cmd)} in {cwd}")
        return subprocess.CompletedProcess(cmd, 124, stdout="", stderr=str(e))


def load_projects():
    if not PROJECTS_TOML.exists():
        log(f"ERROR: {PROJECTS_TOML} not found")
        return []
    with open(PROJECTS_TOML, "rb") as f:
        data = tomllib.load(f)
    out = []
    for p in data.get("projects", []):
        category = p.get("category")
        if category not in ELIGIBLE_CATEGORIES:
            continue
        rel_path = p.get("path", "")
        if not rel_path:
            continue
        if rel_path.startswith("dev/archive/") or "/dev/archive/" in rel_path:
            continue  # decommissioned checkouts -- category="personal" but not live work
        abs_path = (HOME / rel_path).expanduser()
        out.append({"code": p.get("code"), "name": p.get("name"), "path": abs_path})
    return out


def load_state():
    if LAST_RUN_FILE.exists():
        try:
            return json.loads(LAST_RUN_FILE.read_text())
        except (json.JSONDecodeError, OSError):
            return {}
    return {}


def save_state(state):
    STATE_DIR.mkdir(parents=True, exist_ok=True)
    tmp = LAST_RUN_FILE.with_suffix(".tmp")
    tmp.write_text(json.dumps(state, indent=2))
    os.replace(tmp, LAST_RUN_FILE)


def append_result(record):
    STATE_DIR.mkdir(parents=True, exist_ok=True)
    with open(RESULTS_FILE, "a") as f:
        f.write(json.dumps(record) + "\n")


def current_head(repo_path):
    r = run(["git", "rev-parse", "HEAD"], cwd=repo_path)
    return r.stdout.strip() if r.returncode == 0 else None


def current_branch(repo_path):
    r = run(["git", "branch", "--show-current"], cwd=repo_path)
    return r.stdout.strip() if r.returncode == 0 else None


def derive_slug(claude_stdout, fallback_repo_code):
    m = re.search(r"^BRANCH:\s*([a-z0-9-]+)\s*$", claude_stdout, re.MULTILINE)
    if m:
        slug = m.group(1).strip("-")[:60]
        if slug:
            return slug
    # Fallback: no structured slug emitted -- use a dated generic name rather
    # than fail the run (a nightly sweep should not lose its output over a
    # cosmetic naming miss).
    return f"{fallback_repo_code}-hygiene-{datetime.now(timezone.utc):%Y%m%d}"


def process_repo(proj, state):
    code = proj["code"]
    path = proj["path"]
    if not path.is_dir():
        return None  # repo not cloned on this machine -- silent skip, not an error

    branch = current_branch(path)
    if branch not in ALLOWED_BRANCHES:
        log(f"{code}: skip -- checked out branch '{branch}' not in {ALLOWED_BRANCHES}")
        return None

    head = current_head(path)
    if not head:
        log(f"{code}: skip -- could not resolve HEAD")
        return None

    last_sha = state.get(code, {}).get("last_sha")
    if last_sha == head:
        return None  # no diff since last run -- nothing to do, not an error

    log(f"{code}: diff detected ({last_sha or 'never-run'} -> {head}), proceeding")

    ts = datetime.now(timezone.utc).strftime("%Y%m%d%H%M%S")
    tmp_branch = f"docs-hygiene-tmp-{ts}"
    worktree_path = path / ".worktrees" / f"docs-hygiene-{ts}"

    wt = run(
        ["git", "worktree", "add", str(worktree_path), "-b", tmp_branch, branch],
        cwd=path,
        timeout=60,
    )
    if wt.returncode != 0:
        log(f"{code}: worktree add failed: {wt.stderr.strip()}")
        return {"repo": code, "status": "error", "detail": "worktree_add_failed"}

    try:
        prompt = (
            "/improve:docs quick\n\n"
            "You are running unattended (nightly scheduled sweep, no user present). "
            "Follow the autonomous branch of Phase 2 (lens-shared.md): draft all "
            "confirmed clusters, record the default. When finished, on its own "
            "final line, print exactly: BRANCH: <five-word-max-kebab-slug> "
            "summarizing what this run found/fixed (e.g. 'BRANCH: fix-stale-frontmatter-and-dangling-refs'). "
            "If there is nothing to report, print BRANCH: none and do not create any plan."
        )
        claude = run(
            [
                "claude",
                "-p",
                prompt,
                "--model",
                "opus",
                "--permission-mode",
                "bypassPermissions",
            ],
            cwd=worktree_path,
            timeout=CLAUDE_TIMEOUT_S,
        )
        if claude.returncode != 0:
            log(f"{code}: claude -p failed (exit {claude.returncode}): {claude.stderr[:400]}")
            return {"repo": code, "status": "error", "detail": "claude_invocation_failed"}

        if re.search(r"^BRANCH:\s*none\s*$", claude.stdout, re.MULTILINE):
            log(f"{code}: nothing to report this run")
            state[code] = {"last_sha": head, "checked_at": datetime.now(timezone.utc).isoformat()}
            return None

        # Safety net: lens-shared.md Phase 4 step 3 should already have committed
        # (plans/ + beads only) -- commit any residual dirty state so nothing is
        # silently lost, but never touch anything outside plans/.beads convention.
        status = run(["git", "status", "--porcelain"], cwd=worktree_path)
        if status.stdout.strip():
            run(["git", "add", "plans/", "advisor-plans/", ".beads/"], cwd=worktree_path)
            run(
                ["git", "commit", "-m", "chore(docs-hygiene): residual plan/beads state"],
                cwd=worktree_path,
            )

        diff_check = run(["git", "log", f"{branch}..HEAD", "--oneline"], cwd=worktree_path)
        if not diff_check.stdout.strip():
            log(f"{code}: no commits produced -- skipping push")
            state[code] = {"last_sha": head, "checked_at": datetime.now(timezone.utc).isoformat()}
            return None

        slug = derive_slug(claude.stdout, code)
        final_branch = f"docs/{slug}"
        run(["git", "branch", "-m", tmp_branch, final_branch], cwd=worktree_path)
        push = run(["git", "push", "origin", final_branch], cwd=worktree_path, timeout=60)
        if push.returncode != 0:
            log(f"{code}: push failed: {push.stderr.strip()}")
            return {"repo": code, "status": "error", "detail": "push_failed"}

        plan_count = len(diff_check.stdout.strip().splitlines())
        log(f"{code}: pushed {final_branch} ({plan_count} commit(s))")
        state[code] = {"last_sha": head, "checked_at": datetime.now(timezone.utc).isoformat()}
        return {
            "repo": code,
            "status": "pushed",
            "branch": final_branch,
            "commits": plan_count,
            "pushed_at": datetime.now(timezone.utc).isoformat(),
        }
    finally:
        run(["git", "worktree", "remove", "--force", str(worktree_path)], cwd=path, timeout=30)


def notify(results):
    pushed = [r for r in results if r and r.get("status") == "pushed"]
    if not pushed:
        return
    summary = f"docs-hygiene: {len(pushed)} repo(s) with review branches pushed"
    try:
        sys.path.insert(0, str(HOME / ".claude" / "scripts" / "lib"))
        # nx-send.sh is bash, not importable -- shell out instead.
        run(
            ["bash", "-c", f'source "{HOME}/.claude/scripts/lib/nx-send.sh" && nx_notify "{summary}"'],
            timeout=15,
        )
    except Exception as e:
        log(f"notify failed (non-fatal): {e}")


def main():
    if len(sys.argv) > 1 and sys.argv[1] in ("-h", "--help"):
        print(__doc__)
        return 0

    projects = load_projects()
    if not projects:
        log("no eligible projects found -- exiting")
        return 0

    state = load_state()
    results = []
    for proj in projects:
        try:
            result = process_repo(proj, state)
        except Exception as e:
            log(f"{proj['code']}: unhandled exception: {e}")
            result = {"repo": proj["code"], "status": "error", "detail": str(e)}
        if result:
            results.append(result)
            append_result(result)
        save_state(state)  # persist incrementally -- one repo's crash shouldn't lose prior progress

    notify(results)
    log(f"run complete: {len(results)} result(s) across {len(projects)} eligible repo(s)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
