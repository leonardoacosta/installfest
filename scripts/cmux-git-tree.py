#!/usr/bin/env python3
"""cmux-git-tree — render `git log --graph --all` as an HTML commit graph and
open it in cmux's embedded browser panel, wherever this script is invoked.

SSH-aware by construction: this does its work against whatever git repo is the
CURRENT working directory (or `--repo`) on whatever host runs it — a local Mac
pane or a remote SSH-backed workspace (e.g. homelab) alike. It never assumes
"the repo" lives on the machine driving cmux; it renders the repo that is
actually here, then opens it in cmux's browser panel via one of two dispatch
paths (see `find_opener`): the native `cmux browser open file://...` CLI when
running ON the Mac itself (`platform.system() == "Darwin"`; live-verified — no
HTTP server needed for this case), or `mac-open --cmux` (scripts/mac-open.sh)
otherwise — the same local-socket-vs-forwarded-Tailscale channel every other
remote-to-cmux caller in this repo already uses (fview/mac-open.sh, if-ox3), so
a homelab-rendered graph reaches the Mac correctly without this script
reinventing that dispatch. Gating on Darwin (not merely "is a `cmux` binary on
PATH") matters: cmux's own SSH-backed remote-workspace mechanism installs a
relay-forwarding `cmux` shim on remote hosts too, so PATH presence alone does
not mean the browser panel will render locally — see `find_opener`'s docstring
for the live-verified failure this fixed.

Usage:
    cmux-git-tree.py [--repo PATH] [--out PATH] [--no-open] [--max-commits N]

    --repo PATH       Git repo to render (default: CWD)
    --out PATH        Write HTML to this path instead of the cache location
                       (~/.cache/cmux-git-tree/<repo>-<hash>.html)
    --no-open         Only generate the HTML file; print its path, don't open it
    --max-commits N   Cap commits rendered (default 500 — a safety valve for
                       very large histories, not a correctness requirement)
"""

import argparse
import hashlib
import html
import os
import platform
import re
import shutil
import subprocess
import sys
from datetime import datetime, timezone
from pathlib import Path

RECORD_SEP = "\x1e"  # ASCII RS — marks the start of a commit's data on its graph line
FIELD_SEP = "\x1f"  # ASCII US — separates fields within a commit's data
# %ad (not %aI) so --date=iso below actually controls the format we parse.
GIT_FORMAT = RECORD_SEP + FIELD_SEP.join(["%h", "%p", "%an", "%ad", "%d", "%s"])

# Cycled by graph-column index to give distinct "branch lanes" a visually
# different color — an approximation of a real per-branch color assignment
# (which would need to track lane identity through the graph), acceptable for
# a v1 monospace-graph rendering per the task brief.
LANE_COLORS = [
    "#e06c75", "#61afef", "#98c379", "#e5c07b",
    "#c678dd", "#56b6c2", "#d19a66", "#be5046",
]

CACHE_DIR = Path.home() / ".cache" / "cmux-git-tree"


def is_git_repo(repo: Path) -> bool:
    proc = subprocess.run(
        ["git", "-C", str(repo), "rev-parse", "--is-inside-work-tree"],
        capture_output=True, text=True,
    )
    return proc.returncode == 0 and proc.stdout.strip() == "true"


def repo_display_name(repo: Path) -> str:
    proc = subprocess.run(
        ["git", "-C", str(repo), "rev-parse", "--show-toplevel"],
        capture_output=True, text=True,
    )
    top = proc.stdout.strip() if proc.returncode == 0 else str(repo)
    return Path(top).name or str(repo)


def current_branch(repo: Path) -> str:
    proc = subprocess.run(
        ["git", "-C", str(repo), "branch", "--show-current"],
        capture_output=True, text=True,
    )
    name = proc.stdout.strip()
    return name if proc.returncode == 0 and name else "(detached HEAD)"


def run_git_log(repo: Path, max_commits: int) -> str:
    proc = subprocess.run(
        [
            "git", "-C", str(repo), "log", "--graph", "--all", "--date=iso",
            f"--max-count={max_commits}", f"--format={GIT_FORMAT}",
        ],
        capture_output=True, text=True,
    )
    if proc.returncode != 0:
        raise RuntimeError(proc.stderr.strip() or "git log failed")
    return proc.stdout


def relative_time(dt: datetime) -> str:
    now = datetime.now(dt.tzinfo or timezone.utc)
    delta = now - dt
    seconds = delta.total_seconds()
    if seconds < 0:
        return "just now"
    if seconds < 60:
        return "just now"
    minutes = seconds / 60
    if minutes < 60:
        n = int(minutes)
        return f"{n} minute{'s' if n != 1 else ''} ago"
    hours = minutes / 60
    if hours < 24:
        n = int(hours)
        return f"{n} hour{'s' if n != 1 else ''} ago"
    days = hours / 24
    if days < 30:
        n = int(days)
        return f"{n} day{'s' if n != 1 else ''} ago"
    months = days / 30.44
    if months < 12:
        n = int(months)
        return f"{n} month{'s' if n != 1 else ''} ago"
    years = days / 365.25
    n = int(years)
    return f"{n} year{'s' if n != 1 else ''} ago"


def parse_date(raw: str):
    # git --date=iso -> "2026-07-18 09:41:22 -0500"
    try:
        return datetime.strptime(raw, "%Y-%m-%d %H:%M:%S %z")
    except ValueError:
        return None


def parse_refs(raw: str):
    # %d -> " (HEAD -> main, tag: v1.0, origin/main)" or ""
    raw = raw.strip()
    if not raw:
        return []
    raw = raw.strip("()")
    return [r.strip() for r in raw.split(",") if r.strip()]


def parse_log(raw: str):
    """Split `git log --graph` output into rows: commit rows carry parsed
    fields, pure connector rows (the vertical/branch-merge lines between
    commits with no attached format data) carry only their graph glyphs."""
    rows = []
    for line in raw.split("\n"):
        if not line:
            continue
        if RECORD_SEP in line:
            graph_part, data_part = line.split(RECORD_SEP, 1)
            fields = data_part.split(FIELD_SEP, 5)
            fields += [""] * (6 - len(fields))
            commit_hash, parents, author, date_raw, refs_raw, subject = fields
            rows.append({
                "graph": graph_part,
                "hash": commit_hash,
                "parents": parents.split() if parents else [],
                "author": author,
                "date": parse_date(date_raw),
                "date_raw": date_raw,
                "refs": parse_refs(refs_raw),
                "subject": subject,
            })
        else:
            rows.append({"graph": line, "hash": None})
    return rows


def render_graph_html(graph: str) -> str:
    """Colorize each graph glyph column so parallel branch lanes read as
    visually distinct lines rather than a flat wall of `|` characters."""
    out = []
    for i, ch in enumerate(graph):
        if ch == " ":
            out.append(" ")
            continue
        color = LANE_COLORS[i % len(LANE_COLORS)]
        out.append(f'<span style="color:{color}">{html.escape(ch)}</span>')
    return "".join(out)


def render_ref_badge(ref: str) -> str:
    label = ref
    color = "#61afef"
    if ref.startswith("HEAD -> "):
        label = ref[len("HEAD -> "):]
        color = "#98c379"
    elif ref == "HEAD":
        color = "#e5c07b"
    elif ref.startswith("tag: "):
        color = "#c678dd"
    elif "/" in ref:
        color = "#56b6c2"
    return (
        f'<span class="ref-badge" style="border-color:{color};color:{color}">'
        f"{html.escape(label)}</span>"
    )


PAGE_STYLE = """
:root { color-scheme: light dark; }
body {
  margin: 0; padding: 1.5rem 2rem 3rem;
  background: #ffffff; color: #24292e;
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
}
@media (prefers-color-scheme: dark) {
  body { background: #1e2127; color: #d7dae0; }
  .meta { color: #9aa1ac !important; }
  .row:hover { background: #2a2e37 !important; }
  .commit-hash { background: #2a2e37 !important; color: #9aa1ac !important; }
}
h1 { font-size: 1.15rem; margin: 0 0 0.15rem; }
.meta { color: #6a737d; font-size: 0.85rem; margin-bottom: 1.25rem; }
.tree { font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; font-size: 13px; }
.row {
  display: flex; align-items: baseline; gap: 0.65rem;
  padding: 1px 0.4rem; border-radius: 4px; white-space: pre;
}
.row:hover { background: #f0f3f6; }
.graph { white-space: pre; }
.info { display: flex; align-items: baseline; gap: 0.6rem; flex-wrap: wrap; white-space: normal; }
.commit-hash {
  background: #eef1f4; color: #57606a; border-radius: 3px;
  padding: 0 5px; font-size: 12px;
}
.subject { font-weight: 500; }
.author, .date { color: #6a737d; font-size: 12px; }
.ref-badge {
  border: 1px solid; border-radius: 10px; padding: 0 7px;
  font-size: 11px; font-weight: 600;
}
.empty-state {
  display: flex; align-items: center; justify-content: center;
  min-height: 60vh; flex-direction: column; gap: 0.5rem;
  color: #6a737d; text-align: center;
}
.empty-state .glyph { font-size: 2.5rem; opacity: 0.5; }
"""


def render_page(title: str, body: str) -> str:
    return (
        "<!doctype html><html><head><meta charset=\"utf-8\">"
        f"<title>{html.escape(title)}</title>"
        f"<style>{PAGE_STYLE}</style></head><body>{body}</body></html>"
    )


def render_placeholder(message: str, detail: str = "") -> str:
    body = (
        '<div class="empty-state">'
        '<div class="glyph">&#8942;</div>'
        f"<div>{html.escape(message)}</div>"
        + (f'<div class="meta">{html.escape(detail)}</div>' if detail else "")
        + "</div>"
    )
    return render_page("cmux git tree", body)


def render_tree(repo: Path, rows) -> str:
    repo_name = repo_display_name(repo)
    branch = current_branch(repo)
    commit_count = sum(1 for r in rows if r.get("hash"))
    generated = datetime.now().astimezone().strftime("%Y-%m-%d %H:%M:%S %Z")

    line_html = []
    for row in rows:
        graph_html = render_graph_html(row["graph"])
        if row.get("hash") is None:
            line_html.append(f'<div class="row"><span class="graph">{graph_html}</span></div>')
            continue
        date = row.get("date")
        date_label = relative_time(date) if date else (row.get("date_raw") or "")
        date_title = row.get("date_raw", "")
        refs_html = "".join(render_ref_badge(r) for r in row["refs"])
        line_html.append(
            '<div class="row">'
            f'<span class="graph">{graph_html}</span>'
            '<span class="info">'
            f'<span class="commit-hash">{html.escape(row["hash"])}</span>'
            f'{refs_html}'
            f'<span class="subject">{html.escape(row["subject"])}</span>'
            f'<span class="author">{html.escape(row["author"])}</span>'
            f'<span class="date" title="{html.escape(date_title)}">{html.escape(date_label)}</span>'
            "</span>"
            "</div>"
        )

    body = (
        f"<h1>{html.escape(repo_name)}</h1>"
        f'<div class="meta">branch: {html.escape(branch)} &middot; '
        f"{commit_count} commit{'s' if commit_count != 1 else ''} shown &middot; "
        f"generated {html.escape(generated)}</div>"
        f'<div class="tree">{"".join(line_html)}</div>'
    )
    return render_page(f"git tree — {repo_name}", body)


def cache_path_for(repo: Path) -> Path:
    abs_repo = repo.resolve()
    digest = hashlib.sha1(str(abs_repo).encode()).hexdigest()[:8]
    slug = re.sub(r"[^A-Za-z0-9_-]+", "-", abs_repo.name).strip("-") or "repo"
    return CACHE_DIR / f"{slug}-{digest}.html"


def find_opener(out_path: Path):
    """Return an argv list that opens `out_path` in cmux's embedded browser.

    Two dispatch paths, tried in order:

    1. Native `cmux browser open file://<path>` — ONLY when this process is
       actually running ON the Mac (`platform.system() == "Darwin"`), not
       merely when a `cmux` binary happens to be on PATH. Live-verified
       2026-07-18 against cmux 0.64.19: on the Mac itself, `cmux browser
       open` accepts a bare `file://` URL directly and renders it — no local
       static HTTP server needed for this case.

       Gating on Darwin (not `shutil.which("cmux")` alone) fixes a real bug
       found during verification: cmux's SSH-backed remote-workspace
       mechanism installs a relay-forwarding `cmux` shim
       (`~/.cmux/bin/cmux`) on remote hosts too (e.g. homelab), so
       `shutil.which("cmux")` succeeds there as well. That shim genuinely
       reaches the real cmux server over the relay and CAN open a browser
       split — but the WebView renders on the Mac, where a homelab-local
       `file://` path doesn't exist. Verified live: calling the native path
       from homelab created a real browser split (`cmux list-panels` showed
       `"type": "browser"`, title = the homelab file:// URL), but
       `cmux browser snapshot` on that surface showed the Mac's own
       "Can't open this page" error page, not the generated commit graph —
       cmux-on-PATH is not equivalent to cmux-on-the-Mac.
    2. `mac-open --cmux <path>` (scripts/mac-open.sh) otherwise — the case
       where this script runs on a remote SSH-backed host (e.g. homelab),
       Darwin or not. mac-open.sh serves the file over Tailscale HTTP (a URL
       the Mac's WebView can actually fetch, unlike a remote-local file://
       path) and dispatches into cmux's browser panel via its own
       `/tmp/cmux.sock` / `cmux-bridge.py` detection, the same
       remote-to-cmux channel `fview`/if-ox3 already established.
    """
    if platform.system() == "Darwin":
        cmux_bin = shutil.which("cmux")
        if cmux_bin:
            return [cmux_bin, "browser", "open", f"file://{out_path}"]
    resolved = shutil.which("mac-open")
    if resolved:
        return [resolved, "--cmux", str(out_path)]
    dotfiles = os.environ.get("DOTFILES", str(Path.home() / "dev/personal/installfest"))
    candidate = Path(dotfiles) / "scripts" / "mac-open.sh"
    if candidate.exists():
        return [str(candidate), "--cmux", str(out_path)]
    return None


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--repo", default=".", help="Git repo to render (default: CWD)")
    parser.add_argument("--out", default=None, help="Write HTML to this path instead of the cache location")
    parser.add_argument("--no-open", action="store_true", help="Only write the HTML file; skip opening it")
    parser.add_argument("--max-commits", type=int, default=500, help="Cap commits rendered (default 500)")
    args = parser.parse_args()

    repo = Path(args.repo).expanduser()

    def write_and_open(html_out: str) -> Path:
        out_path = Path(args.out).expanduser() if args.out else cache_path_for(repo)
        out_path.parent.mkdir(parents=True, exist_ok=True)
        out_path.write_text(html_out)
        print(str(out_path))
        if not args.no_open:
            opener = find_opener(out_path)
            if opener:
                subprocess.run(opener, check=False)
            else:
                print("cmux-git-tree: neither cmux nor mac-open found on PATH/$DOTFILES — printed path only", file=sys.stderr)
        return out_path

    if not is_git_repo(repo):
        write_and_open(render_placeholder(
            "No git repository here",
            f"{repo.resolve() if repo.exists() else repo} has no .git at its root",
        ))
        return 0

    try:
        raw = run_git_log(repo, args.max_commits)
    except RuntimeError as e:
        write_and_open(render_placeholder("Could not read git history", str(e)))
        return 0

    rows = parse_log(raw)
    if not any(r.get("hash") for r in rows):
        html_out = render_placeholder("No commits yet", repo_display_name(repo))
    else:
        html_out = render_tree(repo, rows)

    write_and_open(html_out)
    return 0


if __name__ == "__main__":
    sys.exit(main())
