# Plan 005: Single render-all invocation per status tick + trace-write gating + doctor that detects a disabled/stale plugin

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**:
> `git -C ~/dev/personal/installfest diff --stat 60a1441..HEAD -- apps/cc-tmux/src/cc_tmux/cli.py apps/cc-tmux/src/cc_tmux/parser.py apps/cc-tmux/src/cc_tmux/testing.py home/dot_config/tmux/`
> Plans 001 and 003 are EXPECTED to have changed `cli.py` since 60a1441 (see
> "Depends on"). For those files, compare intent, not bytes: the functions named
> in "Current state" must still exist with the same roles. For the tmux conf
> files, any change to the `status-format[0..2]` lines since 60a1441 is a STOP
> condition.

## Status

- **Priority**: P2
- **Effort**: M
- **Risk**: MED (touches the live status bar wiring; every code path must stay fail-open)
- **Depends on**: plans/001-*.md (reconcile call placement in cli.py) and plans/003-*.md (usage/HTTP cache) — **execute this plan only after both are DONE** in `plans/README.md`. render-all absorbs their per-command logic; running first would force a re-merge.
- **Category**: perf
- **Planned at**: commit `60a1441`, 2026-07-11

## Why this matters

The tmux status bar refreshes every second (`status-interval 1`) and each tick
spawns THREE separate Python interpreters (`cc-tmux tabs-row`, `session-bar`,
`beads-bar`), each ~54–112 ms, plus ~9 tmux subprocesses, a `projects.toml`
re-parse, and an HTTP call — **~223 ms of measured work per 1-second interval**
(baseline pasted below). Consolidating into one `render-all` invocation cuts
this to one interpreter spawn and one shared pane scan. Separately: the
`register` hook rewrites a ~260 KB trace file on EVERY Claude hook fire (11
`register` wirings across 9 hook events), and `doctor` reports PASS for the
Claude plugin even when it is disabled — the exact blindness that let the
2026-07-11 disabled-plugin outage run ~88 minutes unnoticed.

## Current state

All excerpts are from fresh reads at commit `60a1441` (2026-07-11).
**Expected drift**: plans 001/003 will have modified `cmd_session_bar` /
`_active_usage` / reconcile placement in `cli.py` before you run — the
structure below is your map, not a byte-for-byte contract for that one file.

### Repo facts (inline — you have no other context)

- Repo: personal dotfiles (`~/dev/personal/installfest`, chezmoi-managed).
  Target app: `apps/cc-tmux` — a **Python 3.10+ STDLIB-ONLY** tmux + Claude
  Code plugin (`apps/cc-tmux/pyproject.toml`: `dependencies = []`; adding a
  dependency is forbidden).
- Quality gates: `apps/cc-tmux/bin/cc-tmux self-test` (pure-function suite,
  non-zero exit on failure) and `apps/cc-tmux/bin/cc-tmux doctor` (env
  diagnostics, ALWAYS exit 0). New pure functions MUST get self-test coverage
  in `apps/cc-tmux/src/cc_tmux/testing.py`.
- Design invariants (from `src/cc_tmux/tmux.py:1-39` module docstring) that
  constrain every step: (1) tmux pane options are the ONLY tracked-pane state
  store — no new state files for pane state; (2) views derive, never store;
  (3) real-transition guard; (4) the hot `active` path skips git identity;
  (5) **fail open** — every hook/status entrypoint exits 0, never blocks
  tmux/Claude (`cli.py:883-891` is the boundary: blanket `except` →
  `log.warn` → `return 0` for everything except `self-test`).
- tmux 3.6a. `status-interval` is 1 s (`home/dot_config/tmux/tmux.conf.tmpl:231`).
  Whole status rows render via top-level `status-format[0..2]` slots because
  `#()` nested inside `window-status-format` never re-evaluates on this tmux
  version (known, documented in `render.py:260-272`).
- Dual install: the **tmux side** runs repo HEAD via the
  `~/.tmux/plugins/cc-tmux` symlink (config changes go live on
  `chezmoi apply` + tmux `source-file`); the **Claude hook side** runs a
  SNAPSHOT at `~/.claude/plugins/cache/cc-tmux/cc-tmux/0.1.1/` — code changes
  to hook-path code (`cmd_register`, `_trace_register`) only reach runtime
  after a plugin version bump + `claude plugin update` (OPERATOR GATE, see
  Maintenance notes). The plugin was silently disabled by the 0.1.1 update and
  re-enabled by the operator 2026-07-11 — root cause of the outage doctor
  failed to see.
- Commit pattern: `type(scope): subject`, ad-hoc lane, targeted `git add`
  of named files only (never `git add .`).

### Per-tick cost today (the RPF-6 finding, verified)

`tmux.conf.tmpl:250` (repo-wide row 1):

```
set -g status-format[0] "#(~/.tmux/plugins/cc-tmux/bin/cc-tmux tabs-row)"
```

Per-theme rows 2/3 — four files, same shape, only the `bg=` hex differs:

| File | status-format[1] line | status-format[2] line | bg |
|------|----------------------|----------------------|-----|
| `home/dot_config/tmux/one-hunter-vercel-theme.conf` | 57 | 61 | `#000000` |
| `home/dot_config/tmux/tokyo-night-abyss-theme.conf` | 57 | 61 | `#0D0E15` |
| `home/dot_config/tmux/vercel-theme.conf` | 59 | 63 | `#000000` |
| `home/dot_config/tmux/nord-theme.conf` | 52 | 56 | `#2E3440` |

e.g. `one-hunter-vercel-theme.conf:57`:

```
set -g status-format[1] "#[bg=#000000]#(~/.tmux/plugins/cc-tmux/bin/cc-tmux session-bar #{window_id})"
```

Each `#()` job runs `apps/cc-tmux/bin/cc-tmux`, a bash shim ending in
`exec python3 -m cc_tmux "$@"` — a full interpreter spawn per job per tick.

tmux subprocesses per tick (9 total), from `cli.py` + `tmux.py` at 60a1441:

- `tabs-row` (`cli.py:721-741`): `list-windows` + `list-panes -s`
  (`tmux.py:296,313` in `get_window_tabs`) + `display-message`
  (`tmux.py:388` in `current_window_id`) = 3
- `session-bar` (`cli.py:667-701`): `list-panes -t` (`get_window_top_pane`,
  `tmux.py:259`) + 2x `show-options -p` (`get_pane_option` for
  `@cc-project`/`@cc-branch`, `tmux.py:356`) + server-wide `list-panes -a`
  (`get_hop_panes`, `tmux.py:178`, for the session count at `cli.py:690`) = 4,
  **plus** an HTTP GET to `http://localhost:7400/credentials` with a 1 s
  timeout (`_active_usage` at `cli.py:639-664` → `usage._query`,
  `usage.py:182-197`) — plan 003 owns caching this.
- `beads-bar` (`cli.py:704-718`): `list-panes -t` (`get_window_top_pane`) +
  `display-message #{pane_current_path}` (`cli.py:596` in
  `_read_roadmap_pulse`) = 2. It also re-parses `home/projects.toml` uncached
  every tick (`registry.py:33-53`, `_load_path_to_code` does
  `open()`+`tomllib.load` per call).

**Measured baseline** (fresh, 2026-07-11, this machine, inside live tmux 3.6a,
at 60a1441 — re-measure yourself in Step 1):

```
$ cd ~/dev/personal/installfest/apps/cc-tmux
$ time (for i in 1 2 3 4 5; do ./bin/cc-tmux tabs-row >/dev/null 2>&1; done)
  0.269 total   # ~54 ms/invocation
$ W=$(tmux display-message -p '#{window_id}')
$ time (for i in 1 2 3; do ./bin/cc-tmux session-bar "$W" >/dev/null 2>&1; done)
  0.336 total   # ~112 ms/invocation (includes the HTTP query)
$ time (for i in 1 2 3; do ./bin/cc-tmux beads-bar "$W" >/dev/null 2>&1; done)
  0.171 total   # ~57 ms/invocation
```

Sum: **~223 ms/tick, 3 interpreter spawns**.

### The transport mechanism (runtime-verified)

One `#()` job produces one string for one slot — three slots cannot share a
single job's stdout. The chosen transport: `render-all` (invoked from slot 0)
prints the tabs row on stdout AND writes rows 2/3 into two tmux **global user
options**; slots 1/2 consume them via bare `#{@option}` format lookup — an
in-process option read at draw time, zero extra spawns.

Verified live on this machine's tmux 3.6a:

```
$ tmux set -g @cc-probe-005 '#[fg=red]X#[default]'
$ tmux display-message -p '#{@cc-probe-005}'
#[fg=red]X#[default]
```

The stored `#[...]` style directives come through the format lookup intact;
tmux parses styles on the final expanded string at draw time (rows 2/3 already
rely on exactly this for their `#()` output today). Use bare `#{@cc-row-...}`,
NOT `#{E:@cc-row-...}` — `E:` would re-expand the content as a format, so a
branch name containing `#{` could break the row.

Why options and not a per-tick cache file: a file still needs a `#(cat ...)`
job per slot (2 extra spawns/tick), and tmux options are already this
codebase's canonical store (invariant 1's spirit). These two options are a
render TRANSPORT, not state: written fresh every tick, never read back by any
Python code, only by tmux's drawing pass. Document that in a comment.

Known accepted trade-offs (put them in the code comments too):
- Rows 2/3 trail the tabs row by at most one tick (slot-0's job writes the
  options while/after the current draw pass reads the previous values). At a
  1 s interval with slow-changing content this is invisible.
- Global options mean two clients attached to DIFFERENT sessions would share
  rows 2/3 (last writer wins). Today's per-window `#()` jobs isolate per
  client. This is a single-operator, effectively single-client setup —
  documented, not fixed here (see Maintenance notes).

### The trace-write finding (RPF-5, verified)

`cli.py:118-157`: `_REGISTER_TRACE_FILE = "cc-tmux-register-trace.log"`,
`_REGISTER_TRACE_MAX_LINES = 2000`. `_trace_register` — called
unconditionally from `cmd_register` (`cli.py:92`) — reads the ENTIRE trace
file, trims to 2000 lines, and rewrites it via `.tmp` + `os.replace`, on every
call:

```python
        lines: List[str] = []
        if path.is_file():
            lines = path.read_text(encoding="utf-8").splitlines()
        lines = lines[-(_REGISTER_TRACE_MAX_LINES - 1):]
        lines.append(json.dumps(entry, sort_keys=True))
        tmp = path.with_suffix(path.suffix + ".tmp")
        tmp.write_text("\n".join(lines) + "\n", encoding="utf-8")
        os.replace(tmp, path)
```

`hooks.json` wires `cc-tmux register` in 11 command entries across 9 hook
events (SessionStart, UserPromptSubmit, PreToolUse, PostToolUse,
PostToolUseFailure, Notification, Stop, StopFailure, SessionEnd) — so this
O(file) read+rewrite fires on every tool call/prompt/stop of every session.
The read-trim-rewrite also loses lines when two panes' hooks race (both read
the same base; second `os.replace` wins).

### The doctor blindness (RPF-3, corrected form, verified)

`cli.py:315-319` — the plugin check is a substring match that passes for a
DISABLED plugin:

```python
            listing = f"{proc.stdout}\n{proc.stderr}"
            if "cc-tmux" in listing:
                add("PASS", "claude plugin registered", "cc-tmux present")
```

`claude plugin list` lists disabled plugins too. Verified on this machine —
`claude plugin list --json` is the reliable source:

```json
  {
    "id": "cc-tmux@cc-tmux",
    "version": "0.1.1",
    "enabled": true,
    ...
  }
```

(the human-readable output has per-plugin `Status: ✔ enabled` / `✘ disabled`
lines, but `--json`'s `enabled` boolean is what you should parse).

Doctor also has no hook-liveness row: its rows stop at "tracked panes count"
(`cli.py:338-340`). During the outage, tracked panes existed with silently
aging `@cc-timestamp` values and nothing surfaced it.

**Deliberately NOT in this plan**: persisting `log.warn` output to a file for
doctor to tail. The audit's adversarial verification corrected that idea: with
the plugin disabled, no hook code runs at all, so no warns would ever have
been recorded — a warn file could not have caught the incident class that
matters, and it contradicts `log.py:8-10` ("There is intentionally no log
FILE ... a log file would be a parallel store to reason about."). Do not add
one.

## Commands you will need

| Purpose | Command | Expected on success |
|---------|---------|---------------------|
| Unit gate | `cd ~/dev/personal/installfest/apps/cc-tmux && ./bin/cc-tmux self-test --verbose` | exit 0, all tests `ok` |
| Env gate | `./bin/cc-tmux doctor` | exit 0, PASS/WARN/FAIL rows print |
| Deploy conf | `chezmoi apply ~/.config/tmux` | exit 0 |
| Reload tmux conf | `tmux source-file ~/.config/tmux/tmux.conf` | exit 0, no error line |
| Read a live option | `tmux show-options -g @cc-row-session` | option line prints |
| Timing | `W=$(tmux display-message -p '#{window_id}'); time (for i in 1 2 3 4 5; do ./bin/cc-tmux render-all "$W" >/dev/null 2>&1; done)` | total/5 = per-tick cost |

All Python work must run inside a live tmux session for the runtime checks
(`tmux.py` fails open to no-ops without `$TMUX`).

## Scope

**In scope** (the only files you may modify):

- `apps/cc-tmux/src/cc_tmux/cli.py`
- `apps/cc-tmux/src/cc_tmux/parser.py`
- `apps/cc-tmux/src/cc_tmux/testing.py`
- `home/dot_config/tmux/tmux.conf.tmpl`
- `home/dot_config/tmux/one-hunter-vercel-theme.conf`
- `home/dot_config/tmux/tokyo-night-abyss-theme.conf`
- `home/dot_config/tmux/vercel-theme.conf`
- `home/dot_config/tmux/nord-theme.conf`
- `plans/README.md` (status row only)

**Out of scope** (do NOT touch, even though they look related):

- `apps/cc-tmux/src/cc_tmux/usage.py` and any HTTP caching — plan 003 owns it.
  Call whatever `_active_usage`/usage helper exists after 003; do not add or
  change caching yourself.
- Reconcile scheduling policy — plan 001 owns where the reconcile heartbeat
  lives. Step 3 tells you how to preserve its placement, not change it.
- `apps/cc-tmux/hooks/hooks.json` — no hook wiring changes needed.
- `apps/cc-tmux/.claude-plugin/plugin.json` / `marketplace.json` — the version
  bump is an OPERATOR action (see Maintenance notes), not yours.
- `apps/cc-tmux/src/cc_tmux/tmux.py`, `render.py`, `registry.py`, `log.py` —
  everything needed already exists there (`set_global_option`,
  `get_window_top_pane`, `get_window_tabs`, the pure render functions).
- `~/dev/personal/nexus` (plan 004's territory — separate repo).
- The retired `window-status-format` mechanism and any theme lines other than
  the two `status-format[1]`/`status-format[2]` lines listed above.

## Git workflow

- Work on the current branch (`main`) — this repo's ad-hoc lane commits
  directly, no feature branch.
- ONE commit at the end (Step 8), message style `type(scope): subject`
  matching `git log` (e.g. `fix(cc-tmux): restore tab click-to-switch via
  range=window, fix spacing`). Write the message to a temp file and use
  `git commit -F <file>`.
- Targeted adds only — name every file; never `git add .` or a bare directory.
- Do NOT push unless the operator instructed it.

## Steps

### Step 1: Preconditions + fresh baseline

1. Confirm `plans/README.md` shows plans 001 and 003 as DONE. If either is not
   DONE → STOP.
2. Re-measure the three-command baseline (commands under "Current state" →
   "Measured baseline"). Save the output — the Done criteria require pasting
   before/after numbers.
3. Capture current row output for later diffing:

```bash
cd ~/dev/personal/installfest/apps/cc-tmux
W=$(tmux display-message -p '#{window_id}')
./bin/cc-tmux session-bar "$W" > /tmp/005-before-session.txt
./bin/cc-tmux beads-bar "$W"   > /tmp/005-before-beads.txt
./bin/cc-tmux tabs-row          > /tmp/005-before-tabs.txt
```

**Verify**: `./bin/cc-tmux self-test` → exit 0 (clean starting point; echo `$?`).

### Step 2: Extract row-builder helpers in `cli.py` (no behavior change)

Refactor the three handlers into string-returning helpers so one process can
compose all rows. Keep the handlers as thin wrappers — the subcommands must
keep working (other machines' deployed confs still call them until re-applied).

- `_build_session_bar(window: str, pane: Optional[str] = None) -> str` — the
  body of `cmd_session_bar` (post-001/003 form, whatever it now contains) with
  `print`/`sys.stdout.write` removed; returns the row string or `""`. When
  `pane` is None, resolve it via `tmux.get_window_top_pane(window)` exactly as
  the handler does today; when provided, skip that lookup.
- `_build_beads_bar(window: str, pane: Optional[str] = None) -> str` — same
  treatment for `cmd_beads_bar`.
- `_build_tabs_row(active_window_id: str) -> str` — the body of `cmd_tabs_row`
  but taking the active window id as a parameter instead of calling
  `tmux.current_window_id()`; returns the row string or `""`.
- Wrappers: `cmd_session_bar`/`cmd_beads_bar` call their builder and
  `sys.stdout.write` a non-empty result, `return 0`; `cmd_tabs_row` calls
  `_build_tabs_row(tmux.current_window_id())`.

**Reconcile placement (plan 001 coordination)**: if plan 001 put a
`tmux.reconcile(...)` call inside any of these three handlers, keep that call
inside the corresponding `_build_*` helper — do not move, duplicate, or drop
it in this step. Step 3 handles deduplication.

**Verify**:

```bash
./bin/cc-tmux self-test && \
./bin/cc-tmux session-bar "$W" | diff /tmp/005-before-session.txt - && \
./bin/cc-tmux beads-bar "$W"   | diff /tmp/005-before-beads.txt - && echo REFACTOR-OK
```

→ prints `REFACTOR-OK` (byte-identical rows; the usage gauges can differ if
live utilization moved between captures — if the diff is ONLY in `SES:`/`5H:`/
`7D:` percentages or the account gauge colors, that is acceptable; any
structural diff is not). Do not diff tabs-row: its animation frame is
wall-clock-driven and legitimately differs.

### Step 3: Add the `render-all` subcommand

**`parser.py`** — after the `tabs-row` block (`parser.py:164-174`), add:

```python
    # -- render-all: all three status rows from ONE interpreter spawn ----------
    # Invoked FROM status-format[0] (`#(cc-tmux render-all #{window_id})`).
    # Prints the tabs row on stdout and writes rows 2/3 to the global user
    # options @cc-row-session / @cc-row-beads, consumed by status-format[1]/[2]
    # via bare `#{@cc-row-session}` lookup (zero extra processes).
    p_render_all = sub.add_parser(
        "render-all",
        help="Emit the tabs row and publish rows 2/3 as @cc-row-* options (one spawn per tick).",
    )
    p_render_all.add_argument(
        "window",
        help="Active window id (#{window_id} from the status-format context).",
    )
```

**`cli.py`** — add near the other row handlers (after `cmd_tabs_row`):

```python
# Global user options carrying the pre-rendered rows 2/3 for status-format[1]/[2]
# (bare `#{@cc-row-session}` lookup). Render TRANSPORT, not state (invariant 2):
# overwritten on every render-all tick, never read back by any Python code —
# only tmux's drawing pass consumes them. Rows 2/3 therefore trail the tabs row
# by at most one status-interval tick.
_ROW_SESSION_OPT = "@cc-row-session"
_ROW_BEADS_OPT = "@cc-row-beads"


def cmd_render_all(args) -> int:
    """All three status rows from one interpreter spawn (plan 005).

    Replaces the 3-spawns-per-tick wiring (tabs-row + session-bar + beads-bar
    as separate #() jobs). The window's representative pane is resolved ONCE
    and shared by both row builders. Fail-open: any failure inside a builder
    degrades that row to '' (options are ALWAYS rewritten, so a failing tick
    blanks a row rather than freezing stale content).
    """
    window = args.window
    pane = tmux.get_window_top_pane(window)
    session_row = _build_session_bar(window, pane=pane) if pane else ""
    beads_row = _build_beads_bar(window, pane=pane) if pane else ""
    tmux.set_global_option(_ROW_SESSION_OPT, session_row)
    tmux.set_global_option(_ROW_BEADS_OPT, beads_row)
    tabs = _build_tabs_row(window)
    if tabs:
        sys.stdout.write(tabs)
    return 0
```

Register it in `_DISPATCH` (`cli.py:830-851`): `"render-all": cmd_render_all,`
after the `"tabs-row"` entry. The `main()` fail-open boundary
(`cli.py:883-891`) already covers the new command — do not add your own
try/except around the whole handler.

**Reconcile dedup (plan 001 coordination)**: after this step, `render-all`
must trigger the reconcile-capable pane read **exactly once per invocation**.
`tmux.reconcile()` is internally rate-limited via `@cc-last-reconcile`
(`tmux.py:629-650`, 10 s default), so extra calls are cheap-but-pointless, not
harmful — if two builders each inherited a reconcile call from 001, keep the
one in the earliest-called builder and drop the other, with a comment citing
plan 001.

**Verify**:

```bash
./bin/cc-tmux render-all "$W"; echo; tmux show-options -g @cc-row-session; tmux show-options -g @cc-row-beads
```

→ first line(s): the tabs row string (same shape as `/tmp/005-before-tabs.txt`);
then two `@cc-row-*` option lines whose values match the Step 2 wrapper
outputs for the same window. Then `./bin/cc-tmux self-test` → exit 0.

### Step 4: Rewire `status-format[0..2]` in the confs

**`home/dot_config/tmux/tmux.conf.tmpl:250`** — replace the tabs-row job:

```
set -g status-format[0] "#(~/.tmux/plugins/cc-tmux/bin/cc-tmux render-all #{window_id})"
```

Also update the comment block above it (lines 236-249 at 60a1441) to say rows
2/3 are published by this same job as `@cc-row-session`/`@cc-row-beads` and
consumed per-theme via `#{@cc-row-*}` — one interpreter spawn per tick total.
(`#{window_id}` in a top-level status-format expands to the client's current
window — the theme confs' rows 2/3 already rely on exactly this today.)

**Each of the 4 theme confs** (line numbers in the table under "Current
state") — replace the two job lines, keeping each theme's own `bg=` hex:

```
set -g status-format[1] "#[bg=<THEME-BG>]#{@cc-row-session}"
set -g status-format[2] "#[bg=<THEME-BG>]#{@cc-row-beads}"
```

Update the adjacent comments (rows are published by the render-all job in
tmux.conf.tmpl, not by their own `#()` jobs).

**Verify** (deploy + live check):

```bash
chezmoi apply ~/.config/tmux && tmux source-file ~/.config/tmux/tmux.conf && \
grep -n "render-all" ~/.config/tmux/tmux.conf && \
grep -rn "cc-tmux session-bar\|cc-tmux beads-bar\|cc-tmux tabs-row" ~/.config/tmux/ ; echo "grep-rc=$?"
```

→ `render-all` found once in the deployed tmux.conf; the second grep finds NO
remaining `#()` wiring of the three old subcommands in deployed confs
(`grep-rc=1`). Then wait 3+ seconds and look at the actual status bar (all
three rows render; tab icon still animates; rows 2/3 populated) and run
`tmux show-options -g @cc-row-session` → value refreshed (re-run twice a few
seconds apart if unsure; the animation frame in row 1 proves the job ticks).

### Step 5: Gate the `_trace_register` rewrite

In `cli.py`, replace the read-trim-rewrite (`cli.py:148-155` at 60a1441) with
append-by-default + size-gated trim:

```python
_REGISTER_TRACE_FILE = "cc-tmux-register-trace.log"
_REGISTER_TRACE_MAX_LINES = 2000
# Trim only when the file exceeds ~2x its line cap (2000 lines ~= 260 KB
# observed; 512 KB ~= 4000 lines). The common path is then a single O(1)
# O_APPEND write instead of a full read+rewrite per hook fire — which also
# removes the read-modify-write lost-line race between concurrent pane hooks
# on the hot path (the rare trim can still race; acceptable for a diagnostic).
_REGISTER_TRACE_TRIM_BYTES = 512 * 1024


def trace_needs_trim(size_bytes: int, threshold: int = _REGISTER_TRACE_TRIM_BYTES) -> bool:
    """Pure gate: trim the register trace only past the byte threshold."""
    return size_bytes > threshold
```

Inside `_trace_register`'s `try:` block, after building `entry` and `path`:

```python
        path.parent.mkdir(parents=True, exist_ok=True)
        with open(path, "a", encoding="utf-8") as f:
            f.write(json.dumps(entry, sort_keys=True) + "\n")
        if trace_needs_trim(path.stat().st_size):
            lines = path.read_text(encoding="utf-8").splitlines()
            lines = lines[-_REGISTER_TRACE_MAX_LINES:]
            tmp = path.with_suffix(path.suffix + ".tmp")
            tmp.write_text("\n".join(lines) + "\n", encoding="utf-8")
            os.replace(tmp, path)
```

Keep the enclosing `except Exception: pass` and update the docstring (the
"read-trim-rewrite ... on every call" sentence is no longer true).

NOTE: this function runs on the **Claude hook side** — it executes from the
0.1.1 snapshot until the operator bumps + updates the plugin (Maintenance
notes). Your verification is against repo HEAD, which is correct and
sufficient for this plan.

**Verify**: self-test still exits 0, plus a direct probe:

```bash
python3 - <<'EOF'
import sys; sys.path.insert(0, "src")
from cc_tmux import cli
assert cli.trace_needs_trim(0) is False
assert cli.trace_needs_trim(cli._REGISTER_TRACE_TRIM_BYTES) is False
assert cli.trace_needs_trim(cli._REGISTER_TRACE_TRIM_BYTES + 1) is True
print("trace-gate-OK")
EOF
```

→ prints `trace-gate-OK`.

### Step 6: Doctor — disabled-plugin detection + hook-freshness row

**6a. Plugin check** — in `cmd_doctor`, replace the substring check
(`cli.py:309-321` at 60a1441) with a `--json` parse; keep the existing check
as the fallback when `--json` is unavailable:

```python
        try:
            proc = subprocess.run(
                ["claude", "plugin", "list", "--json"],
                capture_output=True, text=True, timeout=10,
            )
            plugins = json.loads(proc.stdout)
            entry = next(
                (p for p in plugins
                 if isinstance(p, dict) and str(p.get("id", "")).startswith("cc-tmux@")),
                None,
            )
            if entry is None:
                add("WARN", "claude plugin", "cc-tmux not installed")
            elif entry.get("enabled") is True:
                add("PASS", "claude plugin", f"cc-tmux enabled (v{entry.get('version', '?')})")
            else:
                add("FAIL", "claude plugin",
                    "cc-tmux installed but DISABLED - hooks are NOT firing; "
                    "run: claude plugin enable cc-tmux@cc-tmux")
        except Exception:
            # Older CLI without --json: fall back to presence-only substring check.
            try:
                proc = subprocess.run(
                    ["claude", "plugin", "list"],
                    capture_output=True, text=True, timeout=10,
                )
                listing = f"{proc.stdout}\n{proc.stderr}"
                if "cc-tmux" in listing:
                    add("WARN", "claude plugin", "cc-tmux present (enable state unknown - no --json)")
                else:
                    add("WARN", "claude plugin", "cc-tmux not in `claude plugin list`")
            except (OSError, subprocess.SubprocessError) as exc:
                add("WARN", "claude plugin", f"`claude plugin list` failed: {exc}")
```

(Verified on this machine: `claude plugin list --json` emits a JSON array of
`{"id": "cc-tmux@cc-tmux", "version": "0.1.1", "enabled": true, ...}`.)
`doctor` must still ALWAYS exit 0 — a FAIL row is a report, not an exit code.

**6b. Hook-freshness row** — add a pure helper near the other cli helpers:

```python
_HOOK_STALE_AFTER_SECS = 1800.0  # 30 min without any @cc-timestamp movement


def hook_freshness(timestamps: List[float], now: float,
                   stale_after: float = _HOOK_STALE_AFTER_SECS) -> str:
    """'none' (no tracked panes), 'fresh', or 'stale' for the doctor liveness row.

    Pure (self-tested). 'stale' means tracked panes exist but the NEWEST
    @cc-timestamp is older than ``stale_after`` - the disabled-plugin
    signature: panes still carry state while no hook has written for ages.
    """
    real = [t for t in timestamps if t > 0]
    if not real:
        return "none"
    return "fresh" if (now - max(real)) <= stale_after else "stale"
```

In `cmd_doctor`, right after the tracked-pane count row (`cli.py:338-340` at
60a1441), reuse the same pane read instead of a second `get_hop_panes()` call
(capture `panes = tmux.get_hop_panes()` once and derive `count = len(panes)`):

```python
    verdict = hook_freshness([p.timestamp for p in panes], time.time())
    if verdict == "none":
        add("INFO", "hook freshness", "no tracked panes (nothing to assess)")
    elif verdict == "fresh":
        newest = max(p.timestamp for p in panes if p.timestamp > 0)
        add("PASS", "hook freshness", f"newest @cc-timestamp {int(time.time() - newest)}s ago")
    else:
        add("WARN", "hook freshness",
            "newest @cc-timestamp > 30min old - if a Claude session is active, "
            "hooks may not be reaching cc-tmux (check plugin enabled + version)")
```

**Verify**:

```bash
./bin/cc-tmux doctor; echo "exit=$?"
```

→ `exit=0`; output contains a `claude plugin` row reading `PASS ... enabled
(v0.1.1)` (or the plugin's current version) and a `hook freshness` row. Then:
`claude plugin disable cc-tmux@cc-tmux && ./bin/cc-tmux doctor | grep "DISABLED" && claude plugin enable cc-tmux@cc-tmux && ./bin/cc-tmux doctor | grep "enabled"`
→ both greps match (re-enable MUST run even if the grep pipeline fails — never
leave the plugin disabled; run the enable command unconditionally if in doubt).

### Step 7: Self-test coverage for the new pure functions

In `testing.py`, add two test functions (model them on the existing minimal
style, e.g. `_test_is_real_transition_pure` for a pure predicate):

```python
def _test_cli_trace_needs_trim() -> None:
    _check(cli.trace_needs_trim(0) is False, "empty file must not trim")
    _check(cli.trace_needs_trim(cli._REGISTER_TRACE_TRIM_BYTES) is False, "at threshold must not trim")
    _check(cli.trace_needs_trim(cli._REGISTER_TRACE_TRIM_BYTES + 1) is True, "past threshold must trim")
    _check(cli.trace_needs_trim(100, threshold=10) is True, "explicit threshold honored")


def _test_cli_hook_freshness() -> None:
    _check(cli.hook_freshness([], 1000.0) == "none", "no panes -> none")
    _check(cli.hook_freshness([0.0], 1000.0) == "none", "zero timestamps -> none")
    _check(cli.hook_freshness([900.0], 1000.0) == "fresh", "recent -> fresh")
    _check(cli.hook_freshness([100.0], 10000.0) == "stale", "old -> stale")
    _check(cli.hook_freshness([100.0, 9990.0], 10000.0) == "fresh", "newest wins")
    _check(cli.hook_freshness([100.0], 1000.0, stale_after=899.0) == "stale", "custom window")
```

Register both at the end of `_TESTS` (`testing.py:889` at 60a1441):

```python
    ("cli.trace_needs_trim", _test_cli_trace_needs_trim),
    ("cli.hook_freshness", _test_cli_hook_freshness),
```

**Verify**: `./bin/cc-tmux self-test --verbose 2>&1 | grep -E "trace_needs_trim|hook_freshness"`
→ both lines print with `ok`; overall exit 0.

### Step 8: Measure after, then commit

1. **After-timing** (same shape as Step 1's baseline):

```bash
cd ~/dev/personal/installfest/apps/cc-tmux
W=$(tmux display-message -p '#{window_id}')
time (for i in 1 2 3 4 5; do ./bin/cc-tmux render-all "$W" >/dev/null 2>&1; done)
```

Expected: total/5 well under the ~223 ms/tick baseline sum — target < 130 ms
per invocation (< ~90 ms if plan 003's usage cache is warm). One interpreter
spawn per tick instead of three. Paste before AND after numbers into the
commit message and the plans/README.md row.

2. Final gates: `./bin/cc-tmux self-test && ./bin/cc-tmux doctor` → both exit 0.

3. Commit (single commit, targeted adds; write the message with the Write
   tool to `/tmp/005-commit-msg.txt`, then):

```bash
cd ~/dev/personal/installfest
git add apps/cc-tmux/src/cc_tmux/cli.py apps/cc-tmux/src/cc_tmux/parser.py \
        apps/cc-tmux/src/cc_tmux/testing.py home/dot_config/tmux/tmux.conf.tmpl \
        home/dot_config/tmux/one-hunter-vercel-theme.conf \
        home/dot_config/tmux/tokyo-night-abyss-theme.conf \
        home/dot_config/tmux/vercel-theme.conf home/dot_config/tmux/nord-theme.conf \
        plans/README.md
git commit -F /tmp/005-commit-msg.txt
```

Suggested subject: `perf(cc-tmux): one render-all spawn per status tick; gate register trace; doctor detects disabled plugin`

**Verify**: `git status --short` → nothing staged/modified outside the
in-scope list (a `.beads/` change from hooks is fine per repo convention).

## Test plan

- New self-tests (Step 7) in `apps/cc-tmux/src/cc_tmux/testing.py`:
  `_test_cli_trace_needs_trim` (boundary at threshold, past threshold, custom
  threshold) and `_test_cli_hook_freshness` (none/fresh/stale, zero-timestamp
  filtering, newest-wins, custom window). Structural pattern to mimic:
  `_test_is_real_transition_pure` / `_test_compose_title_name` (plain
  `_check` asserts on a pure function, no tmux).
- Behavior-preservation checks are runtime diffs, not unit tests: Step 2's
  byte-diff of wrapper output, Step 3's option-vs-wrapper comparison, Step 4's
  live status-bar observation.
- Verification: `./bin/cc-tmux self-test` → exit 0 with 2 new tests listed in
  `--verbose` output.

## Done criteria

Machine-checkable. ALL must hold:

- [ ] `cd apps/cc-tmux && ./bin/cc-tmux self-test; echo $?` → `0`, and
      `--verbose` output contains `cli.trace_needs_trim` and
      `cli.hook_freshness`
- [ ] `./bin/cc-tmux doctor; echo $?` → `0`, output contains a
      `claude plugin` row distinguishing enabled/disabled (Step 6 disable/
      enable round-trip demonstrated once)
- [ ] `grep -rn "cc-tmux session-bar\|cc-tmux beads-bar\|cc-tmux tabs-row" home/dot_config/tmux/`
      → no `#(` job wiring matches (comments referring to the retired wiring
      must also have been updated)
- [ ] `grep -c "render-all" home/dot_config/tmux/tmux.conf.tmpl` → `>= 1`, and
      `grep -c "@cc-row-session" home/dot_config/tmux/*.conf` → `4` themes wired
- [ ] `tmux show-options -g @cc-row-session` on the live server returns a row
      (or empty value) that refreshes across ticks after `chezmoi apply` +
      `source-file`
- [ ] Measured per-tick cost pasted (before ~223 ms/3 spawns → after: one
      `render-all` < 130 ms), in both the commit message and the
      `plans/README.md` row
- [ ] `grep -n "read_text" apps/cc-tmux/src/cc_tmux/cli.py` shows
      `_trace_register`'s full-file read only under the `trace_needs_trim(...)`
      gate (append is the default path)
- [ ] `git status --short` shows no modifications outside the in-scope list
- [ ] `plans/README.md` status row updated

## STOP conditions

Stop and report back (do not improvise) if:

- `plans/README.md` marks plan 001 or 003 as anything other than DONE.
- The `status-format[0..2]` lines in `tmux.conf.tmpl` or any theme conf differ
  from the "Current state" table (someone else rewired the slots since
  60a1441).
- `cmd_session_bar` / `cmd_beads_bar` / `cmd_tabs_row` no longer exist in
  `cli.py`, or plans 001/003 restructured them so heavily that the Step 2
  extraction is no longer a mechanical body-move (e.g. the handlers were
  already merged, or a builder/cache layer with the same purpose already
  exists — in that case this plan may be partially done; report what you
  found).
- The Step 3 probe fails: `tmux show-options -g @cc-row-session` does not
  return the value `render-all` just wrote, or the live status rows 2/3 render
  the literal text `#{@cc-row-session}` after Step 4 (the `#{@option}`
  transport, runtime-verified on this machine's tmux 3.6a, does not work on
  the target server). Do NOT invent a file-based transport on the fly.
- After Step 4 the status bar goes blank or tmux reports a config error on
  `source-file` — revert the conf changes (`git checkout -- home/dot_config/tmux/`,
  `chezmoi apply ~/.config/tmux`, `tmux source-file ~/.config/tmux/tmux.conf`),
  then report.
- `claude plugin list --json` emits something other than a JSON array of
  objects with `id`/`enabled` keys (CLI format drifted from the sample under
  "Current state").
- Any step's verification fails twice after a reasonable fix attempt.

## Maintenance notes

- **OPERATOR GATE — plugin version bump**: Step 5 (`_trace_register` gating)
  touches register-path code that the Claude hook side runs from the SNAPSHOT
  at `~/.claude/plugins/cache/cc-tmux/cc-tmux/0.1.1/`. It is inert at hook
  runtime until the operator bumps `apps/cc-tmux/.claude-plugin/plugin.json`
  (and the marketplace entry) past 0.1.1 and runs `claude plugin update`.
  The executor must NOT do the bump (out of scope); report it as pending.
  Steps 2-4 and 6 are tmux-side / manual-invocation code served from repo HEAD
  via the `~/.tmux/plugins/cc-tmux` symlink — live immediately.
- **Multi-client caveat**: rows 2/3 now live in GLOBAL options — two clients
  attached to different sessions share them (last render-all writer wins). If
  this setup ever becomes genuinely multi-client, move the options to session
  scope (`set-option -t <session>`) and re-verify the `#{@...}` lookup order
  (session options shadow global).
- **One-tick lag**: rows 2/3 trail row 1 by ≤ 1 s by design. If
  `status-interval` is ever raised, the lag grows with it.
- **Other machines**: deployed tmux confs elsewhere keep calling
  `session-bar`/`beads-bar`/`tabs-row` until `chezmoi apply` runs there —
  that is why the wrappers must stay. Removing the three subcommands is a
  separate, later cleanup once the fleet has re-applied.
- **Reviewer focus**: (1) fail-open preserved — no code path in `render-all`
  can raise past `main()`'s boundary or leave `@cc-row-*` unset on a tick
  (staleness); (2) the Step 2 refactor did not drop whatever reconcile/cache
  logic plans 001/003 landed; (3) doctor still ALWAYS exits 0.
- **Explicitly deferred**: warn-persistence file for doctor to tail — rejected
  (contradicts `log.py:8-10`'s no-log-file invariant, and the audit's
  adversarial verification showed it could not have detected the
  disabled-plugin incident: disabled plugin = no hook code runs = nothing to
  persist). Also deferred: collapsing `render-all`'s internal tmux subprocess
  count further (e.g. one combined `list-panes -a` serving tabs + session
  count + representative pane) — worthwhile only if the measured after-number
  misses target; file a bead instead of expanding this plan's scope.
- Beads prefix for follow-ups: `if-`. OpenSpec (`openspec/changes/`) is
  available if a follow-up graduates to a spec.
