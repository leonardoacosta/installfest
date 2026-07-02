# Plan 003: Wire the orphaned secret-scan + raycast-regen pre-commit logic into the beads hook chain

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `docs/plans/README.md`.
>
> **Drift check (run first)**: `git diff --stat 2068bad..HEAD -- home/run_onchange_set-git-hooks.sh.tmpl scripts/hooks/pre-commit .githooks/pre-commit`
> On any drift, compare the "Current state" excerpts against the live code
> before proceeding; on a mismatch, STOP.

## Status

- **Priority**: P1
- **Effort**: S
- **Risk**: LOW
- **Depends on**: none
- **Category**: dx
- **Planned at**: commit `2068bad`, 2026-07-02

## Why this matters

This repo has a secret-scanning pre-commit hook (`.githooks/pre-commit`) that
blocks commits containing private keys / API tokens — and it never runs on any
machine. `git config core.hooksPath` is `.beads/hooks` (set by
`home/run_onchange_set-git-hooks.sh.tmpl:35`), and `.beads/hooks/pre-commit`
only runs `bd hooks run pre-commit`. Nothing anywhere invokes `.githooks/`.
The same gap bypasses `scripts/hooks/pre-commit`, which regenerates
`platform/raycast-scripts/` when `home/projects.toml` is staged — so registry
edits can commit with stale generated launchers, the exact drift that hook was
written to prevent. The chezmoi script already solves this problem for
pre-push and post-merge with idempotent marker-guarded delegation blocks; this
plan applies the identical, proven pattern to pre-commit.

## Current state

- `home/run_onchange_set-git-hooks.sh.tmpl` (78 lines) — chezmoi
  `run_onchange` script. When `.beads/hooks/pre-push` exists it sets
  `core.hooksPath .beads/hooks` and appends marker-guarded delegation blocks:
  `IF-DEPLOY v1` into `.beads/hooks/pre-push` (lines 38-52, delegates to
  `scripts/hooks/pre-push` with `|| true`) and `IF-POSTMERGE v1` into
  `.beads/hooks/post-merge` (lines 54-73). There is NO block for pre-commit.
  Fallback branch (line 74-77): no beads → `core.hooksPath scripts/hooks`.
  Note line 4-5: the script re-runs when its content changes (that is the
  `run_onchange_` contract) — adding a new block WILL re-trigger it on
  `chezmoi apply`, which is exactly what we want.

```bash
# home/run_onchange_set-git-hooks.sh.tmpl:44-47 (the pattern to replicate)
_if_deploy_root=$(git rev-parse --show-toplevel 2>/dev/null)
if [ -n "$_if_deploy_root" ] && [ -x "$_if_deploy_root/scripts/hooks/pre-push" ]; then
    "$_if_deploy_root/scripts/hooks/pre-push" "$@" || true
fi
```

- `scripts/hooks/pre-commit` (71 lines, POSIX sh) — two sections:
  raycast regeneration (lines 9-30) and beads JSONL flush (lines 32-70).
  It does NOT run the secret scan.
- `.githooks/pre-commit` (69 lines, bash) — standalone secret scanner:
  pattern list at lines 7-12 (private-key blocks, `sk-`, `AKIA`, the
  openssh-key base64 magic), scans staged content via `git show ":$file"`,
  exits 1 on a hit. Self-contained; skips binaries, itself, and `docs/audit/*`.
- `.beads/hooks/pre-commit` — beads-managed, runs `bd hooks run pre-commit`
  between `BEGIN/END BEADS INTEGRATION` markers. Content OUTSIDE the markers
  survives beads upgrades (stated at `set-git-hooks.sh.tmpl:15-16`).
- Current config on this machine: `git config core.hooksPath` → `.beads/hooks`.

Conventions: delegation blocks are marker-guarded (`# --- BEGIN IF-<NAME> v1 ---`
/ `# --- END ... ---`), appended once, idempotent via `grep -Fq` check. Match
that exactly. IMPORTANT difference from pre-push: the pre-push delegation uses
`|| true` (deploy failure must not block a push). Pre-commit is a GATE — the
delegation must propagate a non-zero exit so a failing secret scan blocks the
commit.

## Commands you will need

| Purpose | Command | Expected on success |
|---------|---------|---------------------|
| Syntax (tmpl is plain bash after chezmoi vars) | `bash -n <(chezmoi execute-template < home/run_onchange_set-git-hooks.sh.tmpl)` | exit 0 |
| Syntax | `sh -n scripts/hooks/pre-commit` | exit 0 |
| Lint | `shellcheck scripts/hooks/pre-commit .githooks/pre-commit` | no new errors |
| Apply | `chezmoi apply` | exit 0, prints the "injected" line once |

## Scope

**In scope**:
- `home/run_onchange_set-git-hooks.sh.tmpl` (add IF-PRECOMMIT block)
- `scripts/hooks/pre-commit` (add secret-scan step)

**Out of scope**:
- `.githooks/pre-commit` — unchanged; it stays the canonical scanner,
  now invoked instead of orphaned. Do not move or rewrite it.
- `.beads/hooks/*` — NEVER edit directly; they are generated/managed. The
  tmpl script appends to them at apply time.
- The scanner's pattern list — extending it (gitleaks etc.) is a follow-up,
  not this plan.
- `scripts/hooks/pre-push`, `post-merge`, `post-commit` — working as designed.

## Git workflow

- Current branch; conventional commit, e.g.
  `fix(hooks): chain secret scan + raycast regen into beads pre-commit`.
- Do NOT push unless instructed.

## Steps

### Step 1: Secret scan as step 0 of scripts/hooks/pre-commit

At the top of `scripts/hooks/pre-commit` (before the raycast section), add:

```sh
# === 0. Secret scan (blocks commit on findings) ===
REPO_ROOT="$(git rev-parse --show-toplevel 2>/dev/null)"
if [ -n "$REPO_ROOT" ] && [ -x "$REPO_ROOT/.githooks/pre-commit" ]; then
    "$REPO_ROOT/.githooks/pre-commit" || exit 1
fi
```

(Keep the existing `REPO_ROOT` computation at line 13 or hoist it — one
computation is fine; the file is `#!/bin/sh`, stay POSIX.)

**Verify**: `sh -n scripts/hooks/pre-commit` → exit 0.

### Step 2: IF-PRECOMMIT delegation block in the tmpl

In `home/run_onchange_set-git-hooks.sh.tmpl`, inside the beads branch (after
the IF-POSTMERGE block, before the closing `else`), add — following the
existing pattern verbatim, marker `IF-PRECOMMIT v1`, target
`$DOTFILES/.beads/hooks/pre-commit`:

```bash
    BEADS_PRECOMMIT="$DOTFILES/.beads/hooks/pre-commit"
    PRECOMMIT_BEGIN="# --- BEGIN IF-PRECOMMIT v1 ---"
    if [ -f "$BEADS_PRECOMMIT" ] && ! grep -Fq "$PRECOMMIT_BEGIN" "$BEADS_PRECOMMIT"; then
        cat >> "$BEADS_PRECOMMIT" <<'HOOK'

# --- BEGIN IF-PRECOMMIT v1 ---
# Managed by chezmoi (home/run_onchange_set-git-hooks.sh.tmpl).
# Delegates to scripts/hooks/pre-commit (secret scan + raycast regen).
# NOTE: no `|| true` — a failing scan MUST block the commit.
_if_pc_root=$(git rev-parse --show-toplevel 2>/dev/null)
if [ -n "$_if_pc_root" ] && [ -x "$_if_pc_root/scripts/hooks/pre-commit" ]; then
    "$_if_pc_root/scripts/hooks/pre-commit" "$@" || exit 1
fi
unset _if_pc_root
# --- END IF-PRECOMMIT v1 ---
HOOK
        echo "chezmoi: injected if-precommit v1 into $BEADS_PRECOMMIT"
    fi
```

Also update the header comment line 5 (the chezmoi hash-trigger line listing
managed markers) to mention `if-precommit-v1`, so the change-detection content
reflects the new block.

**Verify**: `bash -n <(chezmoi execute-template < home/run_onchange_set-git-hooks.sh.tmpl)` → exit 0.

### Step 3: Apply and confirm injection

```
chezmoi apply
grep -c 'IF-PRECOMMIT v1' .beads/hooks/pre-commit   # expect 2 (BEGIN+END)
chezmoi apply
grep -c 'IF-PRECOMMIT v1' .beads/hooks/pre-commit   # still 2 (idempotent)
```

### Step 4: Live gate test (safe, uses a fake staged file)

```
printf 'AKIA%s\n' 'AAAAAAAAAAAAAAAA' > /tmp/fake-secret.txt
cp /tmp/fake-secret.txt fake-secret-test.txt
git add fake-secret-test.txt
git commit -m 'test: should be blocked' ; echo "exit=$?"   # expect non-zero + SECRET SCAN FAILED
git reset HEAD fake-secret-test.txt && rm fake-secret-test.txt /tmp/fake-secret.txt
```

Then confirm a normal commit still works: `git commit --allow-empty -m 'test: hook chain ok'`
→ exit 0, then `git reset --soft HEAD~1` to drop the empty commit.

## Test plan

Steps 3-4 are the tests (idempotency, block-on-secret, pass-on-clean). Paste
the Step 4 outputs into the commit message body.

## Done criteria

- [ ] `sh -n scripts/hooks/pre-commit` and the tmpl render+`bash -n` exit 0
- [ ] `.beads/hooks/pre-commit` contains exactly one IF-PRECOMMIT block after
      two consecutive `chezmoi apply` runs
- [ ] Staging a file with a fake AKIA token blocks the commit (exit != 0)
- [ ] A clean empty commit succeeds
- [ ] `.githooks/pre-commit` and `.beads/hooks` managed sections untouched by hand
- [ ] `docs/plans/README.md` status row updated

## STOP conditions

- `git config core.hooksPath` is not `.beads/hooks` on this machine (the
  environment differs from the plan's assumption — report what it is).
- `.beads/hooks/pre-commit` does not exist (beads version changed its layout).
- The Step 4 block test FAILS to block — do not commit the plan work until
  you find why (most likely the delegation `|| exit 1` was lost).
- Beads integration markers appear damaged after your edit.

## Maintenance notes

- Beads version bumps rewrite only content between its own markers; the
  IF-PRECOMMIT block below them survives. If beads ever starts truncating the
  whole file, all three IF-* injections break together — the tmpl re-injects
  on next `chezmoi apply` because `run_onchange` re-fires only on tmpl content
  change; consider re-running `chezmoi apply --force` after beads upgrades.
- Reviewer should scrutinize: exit-code propagation (`|| exit 1`, NOT
  `|| true`) — this is the one place the new block deliberately differs from
  the IF-DEPLOY pattern it copies.
- Follow-up deferred: richer scanner (gitleaks) and running `scripts/check.sh`
  (plan 004) in the same chain.
