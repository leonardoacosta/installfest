# Plan 006: Surface roadmap-pulse cache staleness on cc-tmux row 3 and decouple the row from hook liveness

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**:
> ```bash
> git -C /home/nyaptor/dev/personal/installfest diff --stat 60a1441..HEAD -- \
>   apps/cc-tmux/src/cc_tmux/cli.py \
>   apps/cc-tmux/src/cc_tmux/render.py \
>   apps/cc-tmux/src/cc_tmux/tmux.py \
>   apps/cc-tmux/src/cc_tmux/testing.py
> ```
> If any in-scope file changed since this plan was written, compare the
> "Current state" excerpts against the live code before proceeding; on a
> mismatch, treat it as a STOP condition.

## Status

- **Priority**: P3
- **Effort**: S
- **Risk**: LOW
- **Depends on**: none
- **Category**: bug
- **Planned at**: commit `60a1441`, 2026-07-11

## Why this matters

cc-tmux's status row 3 (the beads/openspec row) renders counts from a
`roadmap-pulse.<code>.line` cache file that is written by an external
refresher under a ~5-minute stale-while-revalidate contract. The reader
(`_read_roadmap_pulse` in `apps/cc-tmux/src/cc_tmux/cli.py`) reads only the
file's *content*, never its mtime, and `render_beads_bar` has no age input —
so counts hours out of date display exactly like fresh ones (audit finding
BEADS-01, CONFIRMED: the live `roadmap-pulse.if.line` was 2h+ old during an
active session and still rendered as current). This is the user-reported
"openspec numbers don't update faithfully" symptom. Separately (BEADS-03,
CONFIRMED), row 3 blanks entirely for any window with no `@cc-state`-tracked
pane, even though the row's only real input is a pane *cwd* — with the Claude
plugin hooks dead (the 0.1.1 update silently disabled the plugin; re-enabled
by the operator 2026-07-11), every window created during the outage got no
row at all despite a valid cache. After this plan: stale counts carry a
visible trailing age marker (e.g. `(2h)`), and row 3 falls back to the
window's active pane cwd when no tracked pane exists.

This plan deliberately does NOT add any cache-refresh spawning to cc-tmux.
That is settled by spec (2026-07-11-cc-tmux-session-usage-bars design
invariant: no new background process; cc-tmux is a passive reader of the
roadmap-pulse cache). If headless refresh is ever wanted, that is a
design-change proposal against the archived spec — an operator decision, not
this plan.

## Current state

Repo facts (inline — the executor has no other context):

- Repo: personal dotfiles (`installfest`, `/home/nyaptor/dev/personal/installfest`,
  chezmoi-managed, project code `if`). Target app: `apps/cc-tmux` — a
  **Python 3.10+ STDLIB-ONLY** tmux + Claude Code plugin
  (`apps/cc-tmux/pyproject.toml`: `requires-python = ">=3.10"`,
  `dependencies = []` — adding any dependency is forbidden).
- Quality gates: `apps/cc-tmux/bin/cc-tmux self-test` (built-in pure-function
  suite, exit code = failure count; currently **42/42 passed, exit 0**) and
  `apps/cc-tmux/bin/cc-tmux doctor` (env diagnostics, always exit 0). New pure
  functions MUST get self-test coverage in
  `apps/cc-tmux/src/cc_tmux/testing.py`.
- Design invariants (from the `tmux.py` module docstring, lines 1–39) that
  constrain this plan: (1) tmux pane options are the ONLY tracked-state store —
  no new state files for pane state (short-TTL caches of *external* data like
  roadmap-pulse are acceptable; this plan adds no files at all); (2) views
  derive, never store; (5) fail open — every hook/status entrypoint exits 0
  and never raises, so it can never block tmux or Claude.
- Plugin dual-install: the tmux side (status rows, including `beads-bar`) runs
  repo HEAD via the `~/.tmux/plugins/cc-tmux` symlink. The Claude *hook* side
  runs a snapshot at `~/.claude/plugins/cache/cc-tmux/cc-tmux/0.1.1/`. **This
  plan touches only the tmux-side code path (status-format jobs), no
  hooks.json and no register-path code, so NO plugin version bump / update
  gate is required** — changes go live on the next status refresh after the
  commit.
- tmux 3.6a, `status-interval` 1s. Row 3 is rendered by a top-level
  `status-format[2]` slot invoking `#(cc-tmux beads-bar #{window_id})`.

### Relevant files

- `apps/cc-tmux/src/cc_tmux/cli.py` — CLI handlers. `_read_roadmap_pulse`
  (lines 585–604) and `cmd_beads_bar` (lines 704–718) are the targets.
- `apps/cc-tmux/src/cc_tmux/render.py` — pure presentation functions.
  `render_beads_bar` (lines 328–357) and the reusable `format_duration`
  (lines 101–117) live here. Colors come from `usage.py`: `DIM = "#454D54"`
  (usage.py line 51).
- `apps/cc-tmux/src/cc_tmux/tmux.py` — tmux subprocess layer.
  `get_window_top_pane` (lines 248–274) selects a pane by `@cc-state`;
  `_run_tmux` (lines 129–150) is the single fail-open choke point tests
  monkeypatch.
- `apps/cc-tmux/src/cc_tmux/testing.py` — the self-test suite.
  `_test_render_beads_bar` (line 688), `_test_cli_read_roadmap_pulse_fail_open`
  (line 782), `_test_tmux_get_window_top_pane` (line 599, the mocking pattern
  to copy), and the `_TESTS` registry list (ends around line 933).

### Code as it exists today (fresh reads at 60a1441)

`cli.py:585-604` — content-only read, no mtime:

```python
def _read_roadmap_pulse(pane_id: str) -> str:
    """Raw ``roadmap-pulse.<code>.line`` content for ``pane_id``'s project, or ``''``.
    ...
    """
    if not pane_id:
        return ""
    try:
        cwd = tmux._run_tmux(["display-message", "-p", "-t", pane_id, "#{pane_current_path}"])
        if not cwd:
            return ""
        code = registry.resolve_project_code(cwd)
        if not code:
            return ""
        return (_cc_state_dir() / f"roadmap-pulse.{code}.line").read_text(encoding="utf-8").strip()
    except Exception:
        return ""
```

`cli.py:704-718` — hard dependency on a tracked (`@cc-state`) pane:

```python
def cmd_beads_bar(args) -> int:
    """Emit the row-3 beads/roadmap status-format string for a window (Req rows 3).
    ...
    """
    pane = tmux.get_window_top_pane(args.window)
    if not pane:
        return 0
    out = render.render_beads_bar(_read_roadmap_pulse(pane))
    if out:
        sys.stdout.write(out)
    return 0
```

`render.py:325-357` — no age input:

```python
_BEADS_SEP = f"#[fg={DIM}] | "


def render_beads_bar(pulse_line: str) -> str:
    """Row-3 status-format string from roadmap-pulse cache content, or ``''``.
    ...
    Pure function of its input (no tmux/subprocess).
    """
    if not pulse_line:
        return ""
    lines = [ln for ln in pulse_line.splitlines() if ln.strip()]
    if not lines:
        return ""

    other_lines = [ln for ln in lines if not ln.startswith("next:")]
    if not other_lines:
        return ""

    segments = [f"#[fg={DIM}]{ln}" for ln in other_lines]
    return _BEADS_SEP.join(segments) + "#[default]"
```

`render.py:101-117` — existing duration formatter to REUSE for the age marker
(do not write a new one; it already has self-test coverage at testing.py:360):

```python
def format_duration(seconds: float) -> str:
    """Compact human duration: ``5s`` / ``3m`` / ``2h`` / ``1d`` (floored)."""
```

`tmux.py:129-150` — `_run_tmux` returns stripped stdout or `None` on any
failure (never raises). `tmux.py:248-274` — `get_window_top_pane` returns the
highest-priority `@cc-state` pane id or `""`.

Conventions that apply:

- Every cli/tmux read is fail-open: catch-all `except Exception` returning an
  empty/None sentinel, never a raise (match `_read_session_context`,
  cli.py:607-636, which already returns a `Tuple[str, Optional[float]]` — use
  it as the shape exemplar for the new tuple return).
- `render.py` functions are pure (no tmux, no subprocess, no `time.time()`
  inside — the caller passes the clock-derived value in, exactly like
  `animated_icon(state, now)` at render.py:65).
- `Tuple`, `Optional` are already imported in cli.py; `time` is already
  imported in cli.py (used at line 560).

## Commands you will need

| Purpose | Command | Expected on success |
|---------|---------|---------------------|
| Self-test (pure-function suite) | `apps/cc-tmux/bin/cc-tmux self-test` | `cc-tmux self-test: 44/44 passed`, exit 0 (42/42 before this plan; +2 new tests) |
| Env diagnostics | `apps/cc-tmux/bin/cc-tmux doctor` | checklist prints, exit 0 |
| Spot-check a pure function | `PYTHONPATH=apps/cc-tmux/src python3 -c "..."` | see per-step expected output |
| Diff hygiene | `git -C /home/nyaptor/dev/personal/installfest status --short` | only in-scope files modified |

All commands run from `/home/nyaptor/dev/personal/installfest` unless a path
is absolute. There is no pip install step — the package is stdlib-only and
run via the `bin/cc-tmux` shim which sets `PYTHONPATH` itself.

Note: `git status` may already show modified
`apps/cc-tmux/.claude-plugin/marketplace.json` and
`apps/cc-tmux/.claude-plugin/plugin.json` — those are pre-existing operator
changes. Do NOT stage, revert, or otherwise touch them.

## Scope

**In scope** (the only files you may modify):

- `apps/cc-tmux/src/cc_tmux/render.py` — add staleness constant + optional
  `age_sec` parameter to `render_beads_bar`.
- `apps/cc-tmux/src/cc_tmux/cli.py` — `_read_roadmap_pulse` returns
  `(content, age_sec)`; new `_beads_pane` helper; `cmd_beads_bar` wiring.
- `apps/cc-tmux/src/cc_tmux/tmux.py` — add `get_window_active_pane`.
- `apps/cc-tmux/src/cc_tmux/testing.py` — update/extend tests, register 2 new
  test rows.
- `plans/README.md` — status row update only.

**Out of scope** (do NOT touch, even though they look related):

- `cmd_session_bar`, `_read_session_context`, `_active_usage`,
  `render_session_bar`, `usage.py` — row 2 / usage-segment territory; other
  concurrently-drafted plans own those seams. If a fix seems to require them,
  STOP.
- Any revalidation/refresh spawning of `roadmap-pulse` from cc-tmux — REFUTED
  / settled by spec (passive-reader design). Do not add background processes,
  `subprocess.Popen` refreshers, or cache writes.
- `~/dev/personal/nexus` (`apps/nexus-statusline/src/index.ts`) — the cache
  *writer* side lives there; it is a separate repo with its own remote and is
  owned by plan 004. Nothing in this plan touches it.
- `apps/cc-tmux/hooks/hooks.json`, `cmd_register`, `cc-tmux.tmux`,
  `.claude-plugin/*` — hook-side / plugin-packaging surfaces; touching them
  would trigger the plugin version-bump operator gate this plan explicitly
  avoids.
- `apps/cc-tmux/src/cc_tmux/registry.py`, `priority.py`, `parser.py` — no
  changes needed (the `beads-bar` subcommand signature is unchanged).

## Git workflow

- Work directly on the current branch (`main`) — this repo uses the ad-hoc
  lane: targeted `git add <files>` (NEVER `git add .` / `-A`), one commit, no
  push unless the operator asked for it.
- Commit message style `type(scope): subject`, e.g. recent history:
  `fix(cc-tmux): restore tab click-to-switch via range=window, fix spacing`.
  Suggested: `fix(cc-tmux): stale-age marker on beads row + active-pane cwd fallback`.
- Write the commit message to a temp file and use `git commit -F <file>`
  (repo convention; never a HEREDOC chained with `&&`).

## Steps

### Step 1: Add the staleness threshold and `age_sec` parameter to `render_beads_bar`

In `apps/cc-tmux/src/cc_tmux/render.py`, directly above `_BEADS_SEP`
(line 325), add:

```python
# Row 3 stale threshold: the roadmap-pulse cache is written under a ~5-minute
# SWR contract (rules/TOOLING.md Ambient Surfacing); 15 minutes = three missed
# refresh cycles, at which point the counts get a trailing age marker so stale
# data never masquerades as current (plan 006 / BEADS-01).
BEADS_STALE_AFTER_SEC = 900.0
```

Change `render_beads_bar`'s signature to:

```python
def render_beads_bar(pulse_line: str, age_sec: Optional[float] = None) -> str:
```

Keep every existing branch byte-identical. Change ONLY the final return line
from:

```python
    return _BEADS_SEP.join(segments) + "#[default]"
```

to:

```python
    out = _BEADS_SEP.join(segments)
    if age_sec is not None and age_sec > BEADS_STALE_AFTER_SEC:
        out += f" ({format_duration(age_sec)})"
    return out + "#[default]"
```

The marker inherits the preceding `#[fg={DIM}]` run, so the whole row —
counts and age marker — stays DIM. Update the docstring: add a line stating
`age_sec` is the cache file's age in seconds (or `None` when unknown), and
that ages beyond `BEADS_STALE_AFTER_SEC` append a DIM trailing
`" (<duration>)"` marker via `format_duration`. Keep the "Pure function of
its input" sentence (it still holds — the caller supplies the age).

**Verify**:

```bash
cd /home/nyaptor/dev/personal/installfest && PYTHONPATH=apps/cc-tmux/src python3 -c "
from cc_tmux import render
print(repr(render.render_beads_bar('12 open / 24 waiting')))
print(repr(render.render_beads_bar('12 open / 24 waiting', 60.0)))
print(repr(render.render_beads_bar('12 open / 24 waiting', 7500.0)))
print(repr(render.render_beads_bar('', 7500.0)))
print(repr(render.render_beads_bar('next: /apply foo 2o 3u', 7500.0)))
"
```

Expected output (DIM is `#454D54`):

```
'#[fg=#454D54]12 open / 24 waiting#[default]'
'#[fg=#454D54]12 open / 24 waiting#[default]'
'#[fg=#454D54]12 open / 24 waiting (2h)#[default]'
''
''
```

(No-age and fresh-age calls are unchanged; stale age appends ` (2h)`; empty
and `next:`-only content still render nothing even when stale.)

### Step 2: Return `(content, age_sec)` from `_read_roadmap_pulse`

In `apps/cc-tmux/src/cc_tmux/cli.py`, change `_read_roadmap_pulse`
(lines 585–604) to return `Tuple[str, Optional[float]]` — content plus the
cache file's mtime age in seconds. Target shape:

```python
def _read_roadmap_pulse(pane_id: str) -> Tuple[str, Optional[float]]:
    """``(content, age_sec)`` of ``roadmap-pulse.<code>.line`` for ``pane_id``'s project.

    Resolves the pane's cwd (``#{pane_current_path}``) to a registry short code
    (longest-prefix match, NOT the ``@cc-project`` display name) and reads the
    matching roadmap-pulse cache line plus its mtime age (``time.time() -
    st_mtime``, floored at 0), so the caller can flag stale counts (plan 006 /
    BEADS-01). Fail-open: no pane, no cwd, no code, missing/unreadable/empty
    file -> ``("", None)``; content readable but stat fails -> ``(content, None)``.
    """
    if not pane_id:
        return "", None
    try:
        cwd = tmux._run_tmux(["display-message", "-p", "-t", pane_id, "#{pane_current_path}"])
        if not cwd:
            return "", None
        code = registry.resolve_project_code(cwd)
        if not code:
            return "", None
        path = _cc_state_dir() / f"roadmap-pulse.{code}.line"
        content = path.read_text(encoding="utf-8").strip()
        try:
            age: Optional[float] = max(0.0, time.time() - path.stat().st_mtime)
        except Exception:
            age = None
        return content, age
    except Exception:
        return "", None
```

`Tuple`, `Optional`, `time` are already imported in cli.py — add no imports.

**Verify** (fail-open shape, no tmux needed):

```bash
cd /home/nyaptor/dev/personal/installfest && PYTHONPATH=apps/cc-tmux/src python3 -c "
from cc_tmux import cli
print(cli._read_roadmap_pulse(''))
"
```

Expected output: `('', None)`

Note: `apps/cc-tmux/bin/cc-tmux self-test` will FAIL at this point
(`cli.read_roadmap_pulse_fail_open` still asserts the old string return).
That is expected; Step 4 fixes the tests. Do not "fix" it by reverting.

### Step 3: Active-pane cwd fallback in `cmd_beads_bar` (BEADS-03 decision: implement fallback)

Decision recorded here so it is not re-litigated: BEADS-03 offered "fallback
or document". This plan **implements the fallback** — row 3's only real input
is a pane cwd, and gating it on a hook-written `@cc-state` pane made the row
blank for every window created while the Claude plugin was disabled. Known,
accepted behavior change: a window with NO Claude pane whose active pane is
cd'd into a registry-tracked project will now also show row 3 (project-scoped
info, previously blank). The revert path is in Maintenance notes.

3a. In `apps/cc-tmux/src/cc_tmux/tmux.py`, add directly after
`get_window_top_pane` (i.e. after line 274, before `get_window_tabs`):

```python
def get_window_active_pane(window_target: str) -> str:
    """Id of ``window_target``'s ACTIVE pane, or ``""``. No @cc-state required.

    Fallback pane source for views whose only real input is a pane cwd (the
    beads row — plan 006 / BEADS-03): ``display-message -t <window>`` resolves
    a window target to its active pane, so this works for windows with no
    tracked Claude pane at all. Fail-open: no tmux / bad target -> ``""``.
    """
    out = _run_tmux(["display-message", "-p", "-t", window_target, "#{pane_id}"])
    return out or ""
```

3b. In `apps/cc-tmux/src/cc_tmux/cli.py`, add a helper directly above
`cmd_beads_bar` and rewire the handler:

```python
def _beads_pane(window_target: str) -> str:
    """The pane whose cwd drives row 3: top tracked pane, else the active pane.

    Prefers :func:`tmux.get_window_top_pane` (the same representative-pane
    choice row 2 uses) but falls back to the window's plain active pane when
    no ``@cc-state`` pane exists — row 3 needs only a cwd, so it must not
    depend on hook liveness (plan 006 / BEADS-03). ``""`` when tmux is
    unavailable or the window is empty.
    """
    return tmux.get_window_top_pane(window_target) or tmux.get_window_active_pane(window_target)
```

In `cmd_beads_bar`, replace the body after the docstring with:

```python
    pane = _beads_pane(args.window)
    if not pane:
        return 0
    content, age_sec = _read_roadmap_pulse(pane)
    out = render.render_beads_bar(content, age_sec)
    if out:
        sys.stdout.write(out)
    return 0
```

Also update `cmd_beads_bar`'s docstring: representative pane now falls back
to the window's active pane, and the render carries the cache age.

**Verify** (pure wiring check with monkeypatched tmux, no live server):

```bash
cd /home/nyaptor/dev/personal/installfest && PYTHONPATH=apps/cc-tmux/src python3 -c "
from cc_tmux import cli, tmux
saved_top, saved_active = tmux.get_window_top_pane, tmux.get_window_active_pane
tmux.get_window_top_pane = lambda w: ''
tmux.get_window_active_pane = lambda w: '%9'
print(cli._beads_pane('@1'))
tmux.get_window_top_pane = lambda w: '%2'
print(cli._beads_pane('@1'))
tmux.get_window_top_pane, tmux.get_window_active_pane = saved_top, saved_active
"
```

Expected output:

```
%9
%2
```

### Step 4: Update and extend the self-test suite

All edits in `apps/cc-tmux/src/cc_tmux/testing.py`.

4a. **`_test_cli_read_roadmap_pulse_fail_open`** (line 782): the function now
returns tuples. Change every fail-open assertion from `== ""` to
`== ("", None)` (lines 784, 790, 796, 835, 839 in the current file). Replace
the positive-read assertion (lines 828–831) with:

```python
        content, age = cli._read_roadmap_pulse("%1")
        _check(content == "next: /apply zz-thing 1o 2u", "reads + strips the resolved pulse line")
        _check(isinstance(age, float) and 0.0 <= age < 60.0, "fresh write -> small non-negative float age")
```

4b. **`_test_render_beads_bar`** (line 688): keep every existing assertion
(they exercise the `age_sec=None` default and must stay green unchanged).
Append staleness cases at the end of the function:

```python
    # Staleness marker (plan 006 / BEADS-01): fresh or unknown age -> unchanged;
    # age beyond BEADS_STALE_AFTER_SEC -> DIM trailing "(<duration>)" marker.
    base = f"#[fg={render.DIM}]12 open / 24 waiting#[default]"
    _check(render.render_beads_bar("12 open / 24 waiting", None) == base, "age None -> no marker")
    _check(render.render_beads_bar("12 open / 24 waiting", 60.0) == base, "fresh age -> no marker")
    _check(
        render.render_beads_bar("12 open / 24 waiting", render.BEADS_STALE_AFTER_SEC) == base,
        "age exactly at threshold -> not yet stale (strict >)",
    )
    _check(
        render.render_beads_bar("12 open / 24 waiting", 901.0)
        == f"#[fg={render.DIM}]12 open / 24 waiting (15m)#[default]",
        "just past threshold -> (15m) marker",
    )
    _check(
        render.render_beads_bar("12 open / 24 waiting", 7500.0)
        == f"#[fg={render.DIM}]12 open / 24 waiting (2h)#[default]",
        "2h-stale -> (2h) marker inside the DIM run",
    )
    out_stale_multi = render.render_beads_bar("12 open / 24 waiting\n3 blocked", 7500.0)
    _check(out_stale_multi.endswith(" (2h)#[default]"), "multi-line stale -> single trailing marker")
    _check(out_stale_multi.count("(2h)") == 1, "marker appears exactly once")
    _check(render.render_beads_bar("", 7500.0) == "", "stale but empty content -> still ''")
    _check(render.render_beads_bar("next: /apply foo 2o 3u", 7500.0) == "", "stale next:-only -> still ''")
```

4c. **New test `_test_tmux_get_window_active_pane`** — place directly after
`_test_tmux_get_window_top_pane` (ends line 620) and copy its monkeypatch
convention exactly (save/restore `tmux._run_tmux` in `try/finally`, fakes
accept `(args, *, check_available=True)`):

```python
def _test_tmux_get_window_active_pane() -> None:
    # Fallback pane source for row 3 (plan 006 / BEADS-03): plain active pane,
    # no @cc-state required. Mirrors _test_tmux_get_window_top_pane's mocking.
    saved = tmux._run_tmux
    try:
        def fake_active(args, *, check_available: bool = True):
            if args[:1] == ["display-message"]:
                return "%5"
            return None
        tmux._run_tmux = fake_active  # type: ignore[assignment]
        _check(tmux.get_window_active_pane("@1") == "%5", "active pane id passes through")

        tmux._run_tmux = lambda args, *, check_available=True: None  # type: ignore[assignment]
        _check(tmux.get_window_active_pane("@1") == "", "no tmux -> '' (fail-open)")
    finally:
        tmux._run_tmux = saved  # type: ignore[assignment]
```

4d. **New test `_test_cli_beads_pane_fallback`** — place directly after
`_test_cli_read_roadmap_pulse_fail_open` (ends line 847):

```python
def _test_cli_beads_pane_fallback() -> None:
    # Row 3 pane resolution (plan 006 / BEADS-03): tracked pane wins; with no
    # tracked pane the window's active pane is used; both empty -> ''.
    saved_top = tmux.get_window_top_pane
    saved_active = tmux.get_window_active_pane
    try:
        tmux.get_window_top_pane = lambda w: "%2"  # type: ignore[assignment]
        tmux.get_window_active_pane = lambda w: "%9"  # type: ignore[assignment]
        _check(cli._beads_pane("@1") == "%2", "tracked pane preferred over active pane")

        tmux.get_window_top_pane = lambda w: ""  # type: ignore[assignment]
        _check(cli._beads_pane("@1") == "%9", "no tracked pane -> active-pane fallback")

        tmux.get_window_active_pane = lambda w: ""  # type: ignore[assignment]
        _check(cli._beads_pane("@1") == "", "no pane at all -> '' (fail-open)")
    finally:
        tmux.get_window_top_pane = saved_top  # type: ignore[assignment]
        tmux.get_window_active_pane = saved_active  # type: ignore[assignment]
```

4e. Register both new tests in the `_TESTS` list (ends ~line 933). Insert
`("tmux.get_window_active_pane", _test_tmux_get_window_active_pane),` right
after the `("tmux.get_window_top_pane", _test_tmux_get_window_top_pane),` row
(line 925), and `("cli.beads_pane_fallback", _test_cli_beads_pane_fallback),`
right after `("cli.read_roadmap_pulse_fail_open", ...)` (line 930).

**Verify**: `apps/cc-tmux/bin/cc-tmux self-test` →
`cc-tmux self-test: 44/44 passed`, exit 0.

### Step 5: Run the full gates

```bash
cd /home/nyaptor/dev/personal/installfest
apps/cc-tmux/bin/cc-tmux self-test; echo "self-test exit=$?"
apps/cc-tmux/bin/cc-tmux doctor >/dev/null; echo "doctor exit=$?"
git status --short
```

**Verify**: `self-test exit=0` (44/44), `doctor exit=0`, and `git status`
shows modifications ONLY in the four in-scope source files (plus the
pre-existing `.claude-plugin/*.json` operator changes, untouched).

### Step 6 (optional, only if running inside tmux): live runtime spot-check

If `$TMUX` is set in your shell:

```bash
apps/cc-tmux/bin/cc-tmux beads-bar "$(tmux display-message -p '#{window_id}')"; echo; echo "exit=$?"
```

**Verify**: exit 0 always. If the current window's active pane cwd is inside
a registered project with a `roadmap-pulse.<code>.line` cache file under
`~/.claude/scripts/state/`, a `#[fg=#454D54]...` string prints — now even
when the window has no Claude pane (the BEADS-03 fix); with a cache file
older than 15 minutes the string ends with an age marker like ` (2h)` before
`#[default]`. If not inside tmux, skip this step — the monkeypatched tests in
Step 4 are the required evidence.

### Step 7: Commit

```bash
cd /home/nyaptor/dev/personal/installfest
git add apps/cc-tmux/src/cc_tmux/cli.py apps/cc-tmux/src/cc_tmux/render.py \
        apps/cc-tmux/src/cc_tmux/tmux.py apps/cc-tmux/src/cc_tmux/testing.py
printf 'fix(cc-tmux): stale-age marker on beads row + active-pane cwd fallback\n\nBEADS-01: _read_roadmap_pulse now returns (content, mtime age); render_beads_bar\nappends a DIM "(2h)"-style marker past a 15-min threshold so stale roadmap-pulse\ncounts never masquerade as current. BEADS-03: cmd_beads_bar falls back to the\nwindow active pane cwd when no @cc-state pane exists, decoupling row 3 from\nClaude hook liveness. Self-test 42 -> 44.\n\nPlan: plans/006-cc-tmux-beads-row-staleness.md\n' > /tmp/commit-msg-$$.txt
git commit -F /tmp/commit-msg-$$.txt
```

Do NOT push unless the operator asked. Note the repo's pre-commit hook may
flush/stage beads files (`.beads/`) automatically — that is expected;
everything else stays targeted.

**Verify**: `git show --stat HEAD` lists exactly the four source files (plus
any hook-added `.beads/` entries).

## Test plan

- All new/changed logic is covered in `apps/cc-tmux/src/cc_tmux/testing.py`
  (the repo's only test surface — stdlib self-test, no pytest):
  - `render.beads_bar` (extended): age `None`/fresh/at-threshold/past-threshold,
    `(15m)` and `(2h)` marker rendering, single marker on multi-line, stale
    empty / stale `next:`-only still render `""`.
  - `cli.read_roadmap_pulse_fail_open` (updated): every fail-open branch now
    `("", None)`; positive read yields content + small float age.
  - `tmux.get_window_active_pane` (new): passthrough + fail-open, modeled on
    `_test_tmux_get_window_top_pane` (testing.py:599).
  - `cli.beads_pane_fallback` (new): tracked-pane preference, active-pane
    fallback, all-empty fail-open, modeled on `_test_tmux_session_count_glyph`'s
    save/restore monkeypatch style (testing.py:576).
- Verification: `apps/cc-tmux/bin/cc-tmux self-test` → `44/44 passed`, exit 0.

## Done criteria

Machine-checkable. ALL must hold:

- [ ] `apps/cc-tmux/bin/cc-tmux self-test` prints `44/44 passed` and exits 0
- [ ] `apps/cc-tmux/bin/cc-tmux doctor` exits 0
- [ ] `PYTHONPATH=apps/cc-tmux/src python3 -c "from cc_tmux import render; print(render.render_beads_bar('12 open / 24 waiting', 7500.0))"` prints `#[fg=#454D54]12 open / 24 waiting (2h)#[default]`
- [ ] `PYTHONPATH=apps/cc-tmux/src python3 -c "from cc_tmux import cli; print(cli._read_roadmap_pulse(''))"` prints `('', None)`
- [ ] `grep -n "get_window_active_pane" apps/cc-tmux/src/cc_tmux/tmux.py` shows the new function; `grep -n "_beads_pane" apps/cc-tmux/src/cc_tmux/cli.py` shows helper + call site
- [ ] `grep -rn "Popen\|spawn" apps/cc-tmux/src/cc_tmux/cli.py apps/cc-tmux/src/cc_tmux/render.py` shows no NEW background-process spawning added by this plan (passive-reader invariant)
- [ ] `git status --short` shows no modifications outside the in-scope list (ignoring the pre-existing `.claude-plugin/*.json` operator changes and hook-managed `.beads/` files)
- [ ] `plans/README.md` status row for 006 updated

## STOP conditions

Stop and report back (do not improvise) if:

- The drift check shows any in-scope file changed since `60a1441` and the
  "Current state" excerpts no longer match — other cc-tmux plans are being
  executed concurrently in this tree; report rather than merge by hand.
- `render_beads_bar` or `_read_roadmap_pulse` already accepts/returns an age
  when you open the file (another executor got here first) — report; do not
  layer a second implementation.
- The baseline `self-test` run (before your edits) is not `42/42 passed` —
  the tree is already broken or drifted; report the failing test names.
- A fix appears to require touching `cmd_session_bar` / `usage.py` /
  `hooks.json` / `cc-tmux.tmux` / anything in `~/dev/personal/nexus` — those
  are other plans' or the operator's territory.
- You find yourself adding a cache *refresh* (background process, Popen,
  writing to `~/.claude/scripts/state/`) — that is the settled-by-spec
  non-goal; report instead.
- A verification fails twice after a reasonable fix attempt.

## Maintenance notes

- **BEADS-03 decision record**: fallback implemented (not just documented).
  Accepted side effect: non-Claude windows cd'd into a registered project now
  show row 3. If the operator dislikes that, the one-line revert is
  `_beads_pane` → `tmux.get_window_top_pane` in `cmd_beads_bar` (keep
  `get_window_active_pane` and its test; other views may want it).
- **Threshold tuning**: `BEADS_STALE_AFTER_SEC = 900.0` is 3x the writer's
  ~5-min SWR TTL. If the roadmap-pulse writer's TTL changes (nexus-statusline
  `PULSE_CACHE_TTL_MS`, plan 004 territory), revisit this constant.
- **What this deliberately does NOT fix**: the cache being stale in the first
  place. cc-tmux is a passive reader by spec; the marker makes staleness
  visible, the *refresher* liveness is the nx statusline's job (cross-repo,
  plan 004). If Leo wants cc-tmux-side headless refresh, that is a
  design-change proposal against the archived
  `cc-tmux-session-usage-bars` spec — operator decision.
- **Reviewer focus**: (1) every touched entrypoint still exits 0 on all
  failure paths (Invariant 5); (2) `render_beads_bar` stays pure — the age is
  computed in cli.py, never inside render.py; (3) no new state files.
- **No plugin version bump needed**: `beads-bar` runs on the tmux side (repo
  HEAD via the `~/.tmux/plugins/cc-tmux` symlink). Only hooks.json /
  register-path changes need the snapshot-update operator gate; this plan has
  none.
