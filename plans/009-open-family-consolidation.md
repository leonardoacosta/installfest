# Plan 009: Open-family — fix ropen's dead `~/dev/if` registry fallback (live defect), then consolidate gopen/sopen/mopen/iopen into one basename-dispatched wrapper

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**:
> `git diff --stat 9399b92..HEAD -- scripts/ropen-server.py scripts/ropen.sh scripts/gopen.sh scripts/sopen.sh scripts/mopen.sh scripts/iopen.sh scripts/lib/open-core.sh scripts/lib/registry.sh home/dot_local/bin/ docs/open-family.md`
> If any in-scope file changed since this plan was written, compare the
> "Current state" excerpts against the live code before proceeding; on a
> mismatch, treat it as a STOP condition.

## Status

- **Priority**: P1
- **Effort**: M
- **Risk**: MED (Step 4 onward retargets deployed `~/.local/bin` commands; Steps 1–2 alone are LOW)
- **Depends on**: none (coordinates with plans/008 — see Scope)
- **Category**: bug (Steps 1–2) + tech-debt (Steps 3–8)
- **Planned at**: commit `9399b92`, 2026-07-14

## Why this matters

Two problems in the `*open` command family (docs/open-family.md), one live, one structural:

1. **Live defect (OPEN-01, CONFIRMED)**: `scripts/ropen-server.py:24` falls back to
   `~/dev/if` for `DOTFILES` — a path that no longer exists (the repo relocated to
   `~/dev/personal/installfest`). The fallback is *reachable in the normal deployment*:
   the systemd unit (`home/dot_config/systemd/user/ropen.service`) sets only
   `Environment=ROPEN_PORT=8889`, and `scripts/ropen.sh:39` assigns `DOTFILES` **without
   `export`**, so the python child spawned at `ropen.sh:99` never receives it. The server
   then tries to read `~/dev/if/home/projects.toml`, `load_registry()`'s bare
   `except Exception: return []` fails open, and the "Registered projects" index on the
   server's root page has been **silently empty on every render** under systemd.

2. **Clone spread (OPEN-02, CONFIRMED)**: `gopen.sh`/`sopen.sh` are token-identical
   ~59-line wrappers (only the browser differs), and `mopen.sh`/`iopen.sh` are
   token-identical ~54-line wrappers (only `nx_mopen` vs `nx_ropen` differs) — four clones
   over the shared engine `scripts/lib/open-core.sh`. The repo already proves the fix
   in-house: `scripts/ideopen.sh` serves two commands (`vopen`/`zopen`) from one script via
   a `case "$(basename "$0")"` dispatch behind symlinks. The clones are already drifting
   (gopen/sopen carry a vestigial no-op `trap - EXIT INT TERM` line that the newer
   mopen/iopen dropped — OPEN-04), and every future browser/notification target would mint
   clone #5. This plan also folds in two stale `# Sourced by:` headers (OPEN-06) on the
   shared libs, which mislead exactly the deletion audits this repo's conventions mandate.

## Current state

All excerpts are fresh reads at commit `9399b92`.

### Repo facts (you have zero other context — these are load-bearing)

- Dotfiles/dev-env repo managed by **chezmoi** (`.chezmoiroot` = `home/`). A file
  `home/dot_local/bin/symlink_NAME.tmpl` deploys as a symlink `~/.local/bin/NAME`
  pointing at the path in the template body. `chezmoi apply` deploys.
- Quality gate: `bash scripts/check.sh` (also `npm run check`) — zsh syntax, `bash -n`
  over `scripts/**/*.sh` + ssh-mesh + platform, chezmoi template render (+ `bash -n` on
  rendered `*.sh.tmpl`), `shellcheck --severity=error`, terraform when initialized.
  Exit 0 = all pass. **New/deleted `scripts/*.sh` files are picked up automatically**
  (the file set is built by `find scripts -name '*.sh'` at check.sh:57).
- Git on this machine: `core.hooksPath=.beads/hooks` (bd-managed pre-commit).
  `.beads/issues.jsonl` is gitignored; the pre-commit hook may print
  `Warning: auto-export: git add failed: exit status 1` — **expected noise, non-fatal**.
- Commit pattern: targeted `git add <files>` (never `git add .`), one commit per unit
  of work, `type(scope): subject` messages (recent example:
  `fix(cc-tmux): extract_active picks freshest isActive credential, not first-in-list`).

### The defect (Step 1–2 targets)

`scripts/ropen-server.py:24-25`:

```python
DOTFILES = pathlib.Path(os.environ.get('DOTFILES', str(pathlib.Path.home() / 'dev' / 'if')))
REGISTRY_PATH = DOTFILES / 'home' / 'projects.toml'
```

`/home/nyaptor/dev/if` does not exist. `load_registry()` (ropen-server.py:27-55) opens
`REGISTRY_PATH` inside `try:` and returns `[]` on any exception (deliberate fail-open for
*broken* registries — not meant to mask a permanently wrong default path).

`scripts/ropen.sh:39` (unexported assignment) and `ropen.sh:90-100` (the `--serve` branch
systemd runs):

```bash
DOTFILES="${DOTFILES:-$HOME/dev/personal/installfest}"      # line 39 — NO export
...
if [[ $DO_SERVE -eq 1 ]]; then                               # line 90
  mkdir -p "$STATE_DIR"
  [[ -f "$MOUNTS_JSON" ]] || echo '{"mounts":{},"sentinels":{}}' > "$MOUNTS_JSON"
  : > "$LOG_FILE"
  ...
  exec "$PY" "$DOTFILES/scripts/ropen-server.py" "$ROPEN_PORT" "$MOUNTS_JSON"   # line 99
fi
```

The `exec` argv expands `$DOTFILES` (so the server *script* is found), but the child's
`os.environ` never contains `DOTFILES`. `home/dot_config/systemd/user/ropen.service`
(line 9 `ExecStart=%h/.local/bin/ropen --serve`, line 13 `Environment=ROPEN_PORT=8889`)
provides no `DOTFILES` either.

`scripts/ropen.sh:141` is a vestigial no-op — no `trap '<handler>'` is ever set anywhere
in ropen.sh or the sourced libs (verified: `grep -rn "trap" scripts/lib/` returns nothing):

```bash
trap - EXIT INT TERM
```

The server's root page renders a `Registered projects` heading (ropen-server.py:304) only
when the registry loads non-empty — that string is the runtime probe this plan uses.

### The clone cluster (Step 3–8 targets)

Four scripts, each deployed via its own symlink template:

| Deployed command | Script | Symlink template | Distinguishing lines |
| --- | --- | --- | --- |
| `gopen` | `scripts/gopen.sh` | `home/dot_local/bin/symlink_gopen.tmpl` | `:42` `exec open -a "Google Chrome" ...` (Darwin); `:57` `open_core_dispatch_browser ... chrome`; `:59` no-op trap |
| `sopen` | `scripts/sopen.sh` | `home/dot_local/bin/symlink_sopen.tmpl` | `:42` `exec open -a Safari ...`; `:57` `... safari`; `:59` no-op trap |
| `mopen` | `scripts/mopen.sh` | `home/dot_local/bin/symlink_mopen.tmpl` | `:48` `command -v nx_mopen ... && nx_mopen "$OPEN_URL"`; no Darwin branch, no `-q`, watcher arg `0` |
| `iopen` | `scripts/iopen.sh` | `home/dot_local/bin/symlink_iopen.tmpl` | `:49` `command -v nx_ropen ... && nx_ropen "$OPEN_URL"`; otherwise identical to mopen |

Common skeleton (all four): source-guard strict mode, `VERSION="1.0.0"`,
`OPEN_CORE_SELF="$0"`, `DOTFILES` default + `. "$DOTFILES/scripts/lib/open-core.sh"`,
sed-based `usage()`, arg loop, `open_core_resolve_target` then `open_core_resolve_url`.
gopen/sopen pass spawn-watcher `1` and honor `-q`; mopen/iopen pass `0`, reject `-q`, and
end with `echo "$OPEN_URL"`. mopen/iopen source `${HOME}/.claude/scripts/lib/nx-send.sh`
if present and warn to stderr if absent.

The in-house exemplar to copy — `scripts/ideopen.sh:32` + `:48-55`:

```bash
SELF="$(basename "$0")"
...
case "$SELF" in
  vopen) CLI=code; KIND=vscode ;;
  zopen) CLI=zed;  KIND=zed ;;
  ...
  *) echo "ideopen: symlink me as vopen | zopen" >&2; exit 1 ;;
esac
```

with `symlink_vopen.tmpl` and `symlink_zopen.tmpl` both containing
`{{ .chezmoi.sourceDir }}/../scripts/ideopen.sh`.

Symlink template body format (all of them, one line):

```
{{ .chezmoi.sourceDir }}/../scripts/gopen.sh
```

Complete reference set to the four scripts (verified by repo-wide grep at 9399b92 —
nothing else in live code points at them): the 4 symlink templates above, plus the stale
comment header `scripts/lib/open-core.sh:11`. `docs/open-family.md` references the
*commands* by name (stays true after consolidation) and `scripts/ideopen.sh:7` mentions
them in prose (out of scope).

### The stale headers (OPEN-06, folded into Step 7)

`scripts/lib/open-core.sh:2-3` and `:11` — both omit `iopen` (which sources it at
`iopen.sh:23`):

```bash
# open-core.sh — shared engine for the VIEW-family *open commands (ropen,
# sopen, gopen, mopen). Portal-aware: ...
...
# Sourced by: scripts/ropen.sh, scripts/mopen.sh, scripts/sopen.sh, scripts/gopen.sh
```

`scripts/lib/registry.sh:10-11` — omits `open-core.sh` (which sources it at
`open-core.sh:27-30` behind a readability guard):

```bash
# Sourced by: wsenv, generate-profiles, wk-ready, cmux-workspaces.sh,
# generate-raycast.sh, mux-remote.sh, copen.
```

### Settled decisions this plan must honor (do not re-litigate)

- `mac-open.sh` coexists with ropen by recorded decision (bead `if-34u` reversal,
  docs/open-family.md § History). Do not touch it.
- `ropen` keeps its own script — it owns server lifecycle (`--serve`/`--mount`/`--list`)
  and is not part of the consolidation.
- EDIT-family (`ideopen.sh`, `executable_copen`) deliberately does NOT source
  open-core.sh (open-core.sh:8-9, docs/open-family.md). Do not "fix" that.

## Commands you will need

| Purpose | Command | Expected on success |
| --- | --- | --- |
| Quality gate | `bash scripts/check.sh` | last line `==> ALL CHECKS PASSED`, exit 0 |
| Deploy symlinks | `chezmoi apply ~/.local/bin` | exit 0 |
| Preview deploy | `chezmoi diff ~/.local/bin` | only the 4 retargeted symlinks |
| Server runtime probe | see Step 2 verify block | `1` from the grep |
| Restart deployed server | `systemctl --user restart ropen` | exit 0 |

Run everything from the repo root: `/home/nyaptor/dev/personal/installfest`.

## Scope

**In scope** (the only files you should modify):

- `scripts/ropen-server.py` (edit line 24)
- `scripts/ropen.sh` (add export in `--serve` branch; delete line 141 trap)
- `scripts/viewopen.sh` (CREATE)
- `scripts/gopen.sh`, `scripts/sopen.sh`, `scripts/mopen.sh`, `scripts/iopen.sh` (DELETE — Step 6, after the operator gate)
- `home/dot_local/bin/symlink_gopen.tmpl`, `symlink_sopen.tmpl`, `symlink_mopen.tmpl`, `symlink_iopen.tmpl` (retarget)
- `scripts/lib/open-core.sh` (comment header lines 2-3 and 11 ONLY — no function bodies)
- `scripts/lib/registry.sh` (comment header lines 10-11 ONLY)
- `docs/open-family.md` (small factual updates)
- `plans/README.md` (status row only, at the end)

**Out of scope** (do NOT touch, even though they look related):

- `scripts/dbpro.sh` — its deletion is owned by **plan 008**; it merely shares this
  audit cluster.
- `scripts/mac-open.sh` — standalone by recorded decision `if-34u`; its private `ts_ip()`
  copy stays.
- `scripts/view.sh` + `home/dot_local/bin/symlink_view.tmpl` — different tool (tmux-split
  terminal renderer), despite the similar name to the new `viewopen.sh`.
- `scripts/ideopen.sh` — its `resolve_remote()` Tailscale duplication (audit finding
  OPEN-05) is explicitly DEFERRED, not part of this plan (see Maintenance notes).
- `home/dot_config/systemd/user/ropen.service` — no unit change needed; the fix is in
  the scripts it runs.
- `home/dot_zsh/rc/linux.zsh` (`flink`'s Tailscale copy) — OPEN-05, deferred.
- Function bodies in `scripts/lib/open-core.sh` / `registry.sh` — comment headers only.
- Any behavior change to `ropen`'s CLI surface beyond the export + trap-line removal.

## Git workflow

- Work on the current branch (`main` at planning time) unless the dispatching operator
  said otherwise.
- **Two commits**, targeted adds only:
  1. `fix(open-family): ropen-server DOTFILES fallback ~/dev/if -> installfest; export DOTFILES in --serve; drop no-op trap`
     — files: `scripts/ropen-server.py scripts/ropen.sh`
  2. `refactor(open-family): collapse gopen/sopen/mopen/iopen into basename-dispatched viewopen.sh`
     — files: `scripts/viewopen.sh scripts/gopen.sh scripts/sopen.sh scripts/mopen.sh scripts/iopen.sh home/dot_local/bin/symlink_gopen.tmpl home/dot_local/bin/symlink_sopen.tmpl home/dot_local/bin/symlink_mopen.tmpl home/dot_local/bin/symlink_iopen.tmpl scripts/lib/open-core.sh scripts/lib/registry.sh docs/open-family.md`
- Push after committing only if the operator who dispatched you instructed it; otherwise
  leave commits local and say so in your report.

## Steps

### Step 1: Fix the ropen-server.py fallback path

In `scripts/ropen-server.py`, change line 24 from:

```python
DOTFILES = pathlib.Path(os.environ.get('DOTFILES', str(pathlib.Path.home() / 'dev' / 'if')))
```

to:

```python
DOTFILES = pathlib.Path(os.environ.get('DOTFILES', str(pathlib.Path.home() / 'dev' / 'personal' / 'installfest')))
```

Also update the docstring at line 10-11 if it needs it — it already says
"DOTFILES env var if set, else ~/dev/personal/installfest", so after this edit the
docstring is finally true. Do not change anything else in the file.

**Verify**:
`grep -n "dev' / 'if'" scripts/ropen-server.py` → no output (exit 1), and
`grep -c "installfest" scripts/ropen-server.py` → `2` (docstring + fallback).

### Step 2: Export DOTFILES in ropen.sh's `--serve` branch and drop the no-op trap

Two edits to `scripts/ropen.sh`:

1. In the `--serve` block (lines 90-100), add an `export DOTFILES` line so the python
   child inherits it (belt-and-suspenders with Step 1). Insert it as the first statement
   inside the `if`:

   ```bash
   if [[ $DO_SERVE -eq 1 ]]; then
     export DOTFILES
     mkdir -p "$STATE_DIR"
     ...
   ```

   Do NOT add `export` to line 39 itself — the narrow `--serve`-branch export is the
   documented fix shape (the other consumers are shell-side and don't need it in the
   environment).

2. Delete line 141 (`trap - EXIT INT TERM`) — the final line of the file. No trap is
   ever set in ropen.sh or its sourced libs; this is a copy-paste relic (OPEN-04).

**Verify (static)**: `bash scripts/check.sh` → `==> ALL CHECKS PASSED`, exit 0.

**Verify (runtime — the actual defect)**: run the server without `DOTFILES` in the
environment (exactly the systemd condition) and probe the registry index:

```bash
echo '{"mounts":{},"sentinels":{}}' > /tmp/ropen-plan009-mounts.json
env -u DOTFILES python3 scripts/ropen-server.py 18899 /tmp/ropen-plan009-mounts.json &
SVPID=$!
sleep 1
curl -s http://127.0.0.1:18899/ | grep -c "Registered projects"
kill "$SVPID"; rm -f /tmp/ropen-plan009-mounts.json
```

→ the grep prints `1`. (Pre-fix, on this machine, it prints `0` — the registry silently
loads empty. If you want the before/after proof, run the block once with
`git stash` / `git stash pop` around it.)

**Verify (deployed)**: `~/.local/bin/ropen` is a symlink into this repo, so the running
service picks the fix up on restart:

```bash
systemctl --user restart ropen
sleep 1
curl -s http://127.0.0.1:8889/ | grep -c "Registered projects"
```

→ `1`. (Restarting your own user service is reversible; `Restart=always` guards it.)

Then make **commit 1** (message + file list in Git workflow above).

### Step 3: OPERATOR GATE — get explicit approval before consolidating

The remainder of this plan deletes 4 scripts and modifies 8 more files (~12 paths total),
which exceeds this repo's 5-file threshold for silent batch changes. **STOP here and
report to the operator**:

> Commit 1 (ropen live fix) is done and verified. Next: collapse
> gopen.sh/sopen.sh/mopen.sh/iopen.sh into one basename-dispatched
> scripts/viewopen.sh (the ideopen.sh pattern), retarget 4 symlink templates,
> and fix 2 stale lib headers + docs/open-family.md. Deployed command names and
> behavior are unchanged. Approve?

Proceed to Step 4 only on explicit approval. If approval is not given, mark this plan's
README row `DONE (steps 1-2 only; consolidation declined)` and finish.

### Step 4: Create `scripts/viewopen.sh`

Create the file with the following content (this is the full reference implementation —
it preserves each command's observable behavior, including mopen/iopen's rejection of
`-q`, their missing Darwin short-circuit, and the nx-send fail-soft warning):

```bash
#!/usr/bin/env bash
# viewopen — basename-dispatched wrapper for the four thin VIEW-family *open
# commands over scripts/lib/open-core.sh (same pattern as ideopen.sh's
# vopen/zopen dispatch; see docs/open-family.md):
#
#   gopen  -> force-open in Google Chrome on the Mac        (browser mode)
#   sopen  -> force-open in Safari on the Mac               (browser mode)
#   mopen  -> clickable Nexus desktop notification on Mac   (notify mode)
#   iopen  -> clickable Nexus APNS push to iPhone           (notify mode)
#
# Invoked by basename — symlink gopen/sopen/mopen/iopen -> viewopen.sh
# (home/dot_local/bin/symlink_*.tmpl). ropen keeps its own script: it
# additionally owns server lifecycle (--serve/--mount/--list).
#
# NOT related to scripts/view.sh (tmux-split terminal renderer).
#
# Env: ATLAS_BASE_URL, OPEN_MAC_HOST — see scripts/lib/open-core.sh.
(return 0 2>/dev/null) || set -euo pipefail

VERSION="1.0.0"
OPEN_CORE_SELF="$0"
SELF="$(basename "$0")"

DOTFILES="${DOTFILES:-$HOME/dev/personal/installfest}"
# shellcheck source=/dev/null
. "$DOTFILES/scripts/lib/open-core.sh"

MODE=""; DISPATCH=""; DARWIN_APP=""; NX_FN=""; DESC=""
case "$SELF" in
  gopen) MODE=browser; DISPATCH=chrome; DARWIN_APP="Google Chrome"
         DESC="force-open a file/dir/registry-code in Google Chrome on the Mac" ;;
  sopen) MODE=browser; DISPATCH=safari; DARWIN_APP="Safari"
         DESC="force-open a file/dir/registry-code in Safari on the Mac" ;;
  mopen) MODE=notify; NX_FN=nx_mopen
         DESC="post a clickable Nexus desktop notification on the Mac (no auto-open)" ;;
  iopen) MODE=notify; NX_FN=nx_ropen
         DESC="post a clickable Nexus APNS push to iPhone (no auto-open)" ;;
  *) echo "viewopen: symlink me as gopen | sopen | mopen | iopen" >&2; exit 1 ;;
esac

usage() {
  cat <<EOF
$SELF — $DESC.
VIEW-family, Atlas-portal-aware (see scripts/lib/open-core.sh).

Usage: $SELF [OPTIONS] <file|dir|registry-code>

Options:
EOF
  [[ "$MODE" == browser ]] && echo "  -q, --quiet    Suppress output"
  cat <<EOF
  -h, --help     Show this help
  -v, --version  Show version

Env: ATLAS_BASE_URL, OPEN_MAC_HOST — see scripts/lib/open-core.sh.
EOF
}

QUIET=0; POSITIONAL=()
while [[ $# -gt 0 ]]; do
  case "$1" in
    -h|--help)    usage; exit 0 ;;
    -v|--version) echo "$SELF $VERSION"; exit 0 ;;
    -q|--quiet)
      if [[ "$MODE" == browser ]]; then QUIET=1; shift
      else echo "$SELF: unknown option $1" >&2; exit 1; fi ;;
    -*)           echo "$SELF: unknown option $1" >&2; exit 1 ;;
    *)            POSITIONAL+=("$1"); shift ;;
  esac
done

[[ ${#POSITIONAL[@]} -eq 0 ]] && { echo "$SELF: missing file argument (try --help)" >&2; exit 1; }

# On macOS, we ARE the Mac — force-launch the browser directly, no Tailscale
# hop. Notify mode has no Darwin shortcut (a notification link is still wanted).
if [[ "$MODE" == browser && "$(uname)" == "Darwin" ]]; then
  exec open -a "$DARWIN_APP" "${POSITIONAL[0]}"
fi

open_core_resolve_target "${POSITIONAL[0]}" || exit 1

if [[ "$MODE" == browser ]]; then
  open_core_resolve_url "$OPEN_RESOLVED_PATH" "$OPEN_RESOLVED_IS_DIR" 1
  [[ $QUIET -eq 0 ]] && {
    if [[ "$OPEN_VIA" == "atlas" ]]; then
      echo "$SELF: '$OPEN_RESOLVED_PATH' → Atlas (portal-indexed)"
    else
      echo "$SELF: mount → $OPEN_RESOLVED_PATH"
    fi
    echo "       → ${OPEN_URL}"
  }
  open_core_dispatch_browser "$OPEN_URL" "$OPEN_URL_PREFIX" "$DISPATCH"
else
  # Notification link is a one-shot — no live-reload watcher (arg 3 = 0).
  open_core_resolve_url "$OPEN_RESOLVED_PATH" "$OPEN_RESOLVED_IS_DIR" 0
  # nx-send.sh is owned by cc; degrade gracefully if absent.
  NX_SEND="${HOME}/.claude/scripts/lib/nx-send.sh"
  if [[ -f "$NX_SEND" ]]; then
    # shellcheck source=/dev/null
    . "$NX_SEND"
    command -v "$NX_FN" >/dev/null 2>&1 && "$NX_FN" "$OPEN_URL"
  else
    echo "$SELF: warning: nx-send.sh not found at $NX_SEND — notification skipped" >&2
  fi
  echo "$OPEN_URL"
fi
```

Then: `chmod +x scripts/viewopen.sh`

**Verify**:
- `ls -la scripts/viewopen.sh` → file exists at that exact path, mode `-rwxr-xr-x`
  (this repo has a recorded incident of the Write tool silently landing files at the
  wrong path — confirm the path yourself).
- `bash -n scripts/viewopen.sh` → exit 0, no output.
- `bash scripts/check.sh` → `==> ALL CHECKS PASSED`, exit 0 (the find-glob picks the
  new file up for shellcheck automatically).
- Direct-invocation guard: `./scripts/viewopen.sh --help; echo "exit=$?"` →
  `viewopen: symlink me as gopen | sopen | mopen | iopen` on stderr, `exit=1`.
- Behavior parity by basename (no deploy needed yet):

  ```bash
  for n in gopen sopen mopen iopen; do ln -sf "$PWD/scripts/viewopen.sh" "/tmp/plan009-$n"; done
  /tmp/plan009-gopen --version   # -> gopen 1.0.0
  /tmp/plan009-iopen --version   # -> iopen 1.0.0
  /tmp/plan009-gopen --help | head -1   # -> gopen — force-open a file/dir/registry-code in Google Chrome on the Mac.
  /tmp/plan009-mopen -q x 2>&1; echo "exit=$?"   # -> mopen: unknown option -q / exit=1
  /tmp/plan009-gopen 2>&1; echo "exit=$?"        # -> gopen: missing file argument (try --help) / exit=1
  rm -f /tmp/plan009-{gopen,sopen,mopen,iopen}
  ```

### Step 5: Retarget the four symlink templates

Each of these single-line files currently ends `.../scripts/<name>.sh`. Change the body
of all four to the identical line:

```
{{ .chezmoi.sourceDir }}/../scripts/viewopen.sh
```

Files: `home/dot_local/bin/symlink_gopen.tmpl`, `symlink_sopen.tmpl`,
`symlink_mopen.tmpl`, `symlink_iopen.tmpl`. (This mirrors how `symlink_vopen.tmpl` and
`symlink_zopen.tmpl` both point at `scripts/ideopen.sh`.)

**Verify**:
`grep -l "viewopen.sh" home/dot_local/bin/symlink_*.tmpl | sort` → exactly the 4 files
above (vopen/zopen still say ideopen.sh; ropen/view/mac-open unchanged).

### Step 6: Delete the four clone scripts

Pre-deletion reference gate (must be clean before deleting):

```bash
grep -rn --exclude-dir=.git --exclude-dir=__pycache__ --exclude-dir=.beads \
  -E "gopen\.sh|sopen\.sh|mopen\.sh|iopen\.sh" . | grep -v '^\./plans/'
```

Expected remaining hits: ONLY `scripts/lib/open-core.sh:11` (the stale header Step 7
fixes). If any OTHER live file references the four scripts, STOP and report.

Then:

```bash
git rm scripts/gopen.sh scripts/sopen.sh scripts/mopen.sh scripts/iopen.sh
```

(The three no-op `trap - EXIT INT TERM` lines at gopen.sh:59 / sopen.sh:59 disappear
with the deletion; ropen.sh:141 was already removed in Step 2 — OPEN-04 fully closed.)

**Verify**: `ls scripts/gopen.sh scripts/sopen.sh scripts/mopen.sh scripts/iopen.sh 2>&1`
→ four "No such file or directory" lines. `bash scripts/check.sh` → exit 0.

### Step 7: Fix the stale lib headers and docs (OPEN-06)

1. `scripts/lib/open-core.sh` lines 2-3: change the parenthetical list so the header reads

   ```bash
   # open-core.sh — shared engine for the VIEW-family *open commands (ropen,
   # plus gopen/sopen/mopen/iopen via viewopen.sh). Portal-aware: before falling back to the live-mount
   ```

   (keep the rest of the sentence from line 3 onward intact), and line 11:

   ```bash
   # Sourced by: scripts/ropen.sh, scripts/viewopen.sh (as gopen/sopen/mopen/iopen)
   ```

2. `scripts/lib/registry.sh` lines 10-11: append the missing consumer so it reads

   ```bash
   # Sourced by: wsenv, generate-profiles, wk-ready, cmux-workspaces.sh,
   # generate-raycast.sh, mux-remote.sh, copen, scripts/lib/open-core.sh.
   ```

3. `docs/open-family.md`: in the "VIEW family" section (around line 17, "All five share
   one engine, `scripts/lib/open-core.sh`:"), add one sentence after that line:

   ```
   `gopen`/`sopen`/`mopen`/`iopen` are a single script, `scripts/viewopen.sh`,
   dispatched by basename behind their symlinks (the same pattern `ideopen.sh`
   uses for `vopen`/`zopen`); `ropen` keeps its own script because it also owns
   the server lifecycle (`--serve`/`--mount`/`--list`).
   ```

   Leave the per-command behavior table and everything else untouched.

**Verify**:
`grep -n "Sourced by" scripts/lib/open-core.sh scripts/lib/registry.sh` → open-core line
lists `viewopen.sh`; registry line ends with `open-core.sh.`.
`grep -c "viewopen.sh" docs/open-family.md` → `1` (or more).

### Step 8: Deploy, smoke-test, commit

```bash
chezmoi diff ~/.local/bin
```

→ the only changes are the 4 symlinks retargeting to `.../scripts/viewopen.sh`. If
anything else shows up in the diff, STOP and report.

```bash
chezmoi apply ~/.local/bin
for n in gopen sopen mopen iopen; do readlink -f ~/.local/bin/$n; done
```

→ all four print `/home/nyaptor/dev/personal/installfest/scripts/viewopen.sh`.

Smoke-run every surviving open command through its deployed name:

```bash
gopen --help >/dev/null && echo gopen-ok      # -> gopen-ok
sopen --version                                # -> sopen 1.0.0
mopen --help >/dev/null && echo mopen-ok      # -> mopen-ok
iopen --version                                # -> iopen 1.0.0
ropen --help >/dev/null && echo ropen-ok      # -> ropen-ok
```

Final gate: `bash scripts/check.sh` → `==> ALL CHECKS PASSED`, exit 0.

Then make **commit 2** (message + full file list in Git workflow above), and update this
plan's row in `plans/README.md`.

## Test plan

This repo has no shell unit-test harness (cc-tmux's `self-test` covers only
`apps/cc-tmux` python — unrelated here); the gate for shell work is
`bash scripts/check.sh` plus behavior probes. The probes for this plan, all specified
inline above with expected outputs:

- **Regression probe for the bug being fixed** (Step 2): `env -u DOTFILES` server start +
  `curl | grep -c "Registered projects"` → `1` (the systemd-equivalent condition), plus
  the deployed-service restart probe on port 8889.
- **Parity probes for the consolidation** (Step 4): `--version`, `--help` first line,
  `-q` rejection in notify mode, missing-argument error — one per behavioral seam that
  differs across the four basenames, run via temp symlinks before deployment.
- **Deployment probes** (Step 8): `readlink -f` on all four deployed names +
  `--help`/`--version` smoke through `$PATH`.

## Done criteria

Machine-checkable. ALL must hold (from repo root):

- [ ] `bash scripts/check.sh` exits 0 (`==> ALL CHECKS PASSED`)
- [ ] `grep -n "dev' / 'if'" scripts/ropen-server.py` → no matches
- [ ] `grep -n "export DOTFILES" scripts/ropen.sh` → exactly one match, inside the `--serve` block
- [ ] `grep -rn "trap - EXIT INT TERM" scripts/` → no matches
- [ ] `curl -s http://127.0.0.1:8889/ | grep -c "Registered projects"` → `1` (after `systemctl --user restart ropen`)
- [ ] `test -x scripts/viewopen.sh && echo ok` → `ok`; `ls scripts/{gopen,sopen,mopen,iopen}.sh 2>/dev/null | wc -l` → `0`
- [ ] `grep -l "viewopen.sh" home/dot_local/bin/symlink_*.tmpl | wc -l` → `4`
- [ ] `for n in gopen sopen mopen iopen; do readlink -f ~/.local/bin/$n; done | sort -u` → single line ending `scripts/viewopen.sh`
- [ ] Each of `gopen sopen mopen iopen ropen` exits 0 on `--help`
- [ ] `git status --porcelain` shows no modifications outside the in-scope list
- [ ] `plans/README.md` status row for 009 updated

(If the operator declined the Step 3 gate: only the first five boxes plus the README row
apply, and the plan closes as "steps 1-2 only".)

## STOP conditions

Stop and report back (do not improvise) if:

- The drift check shows any in-scope file changed since `9399b92`, or any "Current state"
  excerpt no longer matches the live file (this tree is shared — concurrent sessions
  exist; if the tree looks inconsistent, re-read via `git show 9399b92:<path>` and
  compare).
- The Step 3 operator approval is not granted — finish after commit 1, do not
  "partially" consolidate.
- The Step 6 pre-deletion grep finds a live reference to any of the four scripts beyond
  `scripts/lib/open-core.sh:11`.
- `bash scripts/check.sh` fails twice on the same section after one reasonable fix
  attempt. NOTE: `scripts/mux-remote.sh` (SC1071) and `scripts/gk-github-auth.sh`
  (SC2148) are pre-existing, documented exclusions inside check.sh itself — those are
  not failures you caused.
- The Step 2 deployed probe still prints `0` after the fix + restart (something else is
  wrong with the service — do not start editing the systemd unit or open-core.sh to
  chase it).
- `chezmoi diff ~/.local/bin` shows changes beyond the 4 symlinks.
- The fix appears to require touching `scripts/dbpro.sh` (plan 008's), `mac-open.sh`,
  `view.sh`, `ideopen.sh`, or `ropen.service`.
- A Bash invocation fails with exit 1/127/134 and zero output — this machine has a
  recorded transient Bash-tool flake; retry once and verify file state with Read before
  assuming your edit broke something.

## Maintenance notes

- **The next `*open` variant** (new browser, new notification target) must be a new
  `case` arm in `scripts/viewopen.sh` + one new `symlink_*.tmpl` — never a new clone
  script. A reviewer should reject any new `scripts/*open*.sh` that re-clones the
  skeleton.
- **OPEN-05 explicitly deferred** (Tailscale-IP resolution exists 4x:
  `open_core_resolve_ts_ip` at open-core.sh:75-83, `mac-open.sh` `ts_ip()` ~:75-78,
  `ideopen.sh` `resolve_remote()` :36-46, `flink` in home/dot_zsh/rc/linux.zsh:94-96).
  Deferred because it is NOT a drop-in dedup: `open_core_resolve_ts_ip` returns an IPv4
  only, while `ideopen.sh`'s copy *prefers the MagicDNS DNSName* (functionally
  load-bearing for its ssh-back addressing), and EDIT-family scripts deliberately do not
  source open-core.sh (open-core.sh:8-9, docs/open-family.md). A real consolidation
  would need a new tiny `scripts/lib/` helper with a DNSName-aware mode — that is a
  design decision for the operator, not a silent step here. mac-open.sh's copy stays
  regardless (decision if-34u).
- **Reviewer focus for commit 2**: behavior parity of the notify branch (`-q` rejection,
  no Darwin short-circuit, `echo "$OPEN_URL"` as the final line, nx-send fail-soft), and
  that `OPEN_CORE_SELF="$0"` is set before sourcing open-core.sh (its error messages
  basename it).
- **Deployment coupling**: the deployed commands are symlinks into this working tree —
  a checkout of an older commit changes live behavior immediately. Nothing new; noted
  because viewopen.sh now backs four commands instead of one.
- The three "Registered projects" runtime probes assume the ropen service is enabled on
  this machine (it is at planning time; `Restart=always`). On a machine without the
  service, substitute the Step 2 scratch-port probe.
