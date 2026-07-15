# Plan 014: cc-tmux dead-code sweep — delete the legacy session-context reader, ANSI context bar, paths.py, legacy usage segment, transitional row wrappers, window-icon path, unwired status surfaces, and stub scaffolding

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**:
> `git diff --stat 9399b92..HEAD -- apps/cc-tmux/`
> If any in-scope file changed since this plan was written, compare the
> "Current state" excerpts against the live code before proceeding; on a
> mismatch, treat it as a STOP condition.

## Status

- **Priority**: P2
- **Effort**: M
- **Risk**: MED (deletion-only, but two steps are gated on runtime fleet verification)
- **Depends on**: none (plans 001–007 are DONE; this deletes leftovers they documented)
- **Category**: tech-debt
- **Planned at**: commit `9399b92`, 2026-07-14

**Operator approval recorded upfront**: this plan deletes one whole module
(`paths.py`, 113 lines) and nine other dead surfaces across `apps/cc-tmux/`.
That exceeds the trivial-deletion threshold, so approval to execute THE WHOLE
PLAN is the operator gate — approved by dispatching this plan. The executor
does not need to re-ask per step, with ONE exception: Step 11
(`notify/windows.py`) has its own explicit operator gate and is SKIPPED by
default.

## Why this matters

The cc-tmux package (apps/cc-tmux/src/cc_tmux/, ~10.4K lines) has accumulated
ten distinct dead or superseded surfaces, each left behind by a feature
migration that never circled back: the nx-agent migration left
`_read_session_context` alive "for its own self-test coverage"; the
braille-glyph change orphaned the whole ANSI shade-block bar chain; the
plan-005 render consolidation left three wrapper subcommands plus the
window-icon path wired into the parser; row-2's `extract_active` superseded
the `usage` segment, whose dead copy still carries the pre-if-lh9u
stale-duplicate bug (actively divergent, not just idle); and `paths.py` is an
entire module with zero production importers. Every keep is documented in
prose, but none carries a tracked removal issue — so they will linger
indefinitely and each future reader pays the comprehension tax. This plan is
the one consolidation pass that removes all of them, with runtime gates for
the surfaces that older deployed tmux confs could still invoke.

## Current state

All excerpts below are from fresh reads at commit `9399b92` (clean tree).

Relevant files:

- `apps/cc-tmux/src/cc_tmux/cli.py` (2081 lines) — CLI handlers + `_DISPATCH` map (dead: `_read_session_context`, `cmd_status`, `cmd_status_inbox`, `cmd_window_icon`, `_window_subagent_counts`, `cmd_session_bar`, `cmd_beads_bar`, `cmd_tabs_row`, `_stub`, `_STUB_OWNERS`)
- `apps/cc-tmux/src/cc_tmux/render.py` (1153 lines) — pure render functions (dead: `render_context_bar_ansi` chain, `render_status`, `DEFAULT_STATUS_FORMAT`, `_TOKEN_RE`)
- `apps/cc-tmux/src/cc_tmux/usage.py` (589 lines) — nx-agent usage data (dead: `render_usage`, `build_segment`, `cmd_usage`)
- `apps/cc-tmux/src/cc_tmux/paths.py` (112 lines) — DELETE ENTIRELY (zero production importers)
- `apps/cc-tmux/src/cc_tmux/parser.py` (289 lines) — argparse registrations for the dead subcommands
- `apps/cc-tmux/src/cc_tmux/tmux.py` (928 lines) — dead: `get_window_top_state` (line 254), `set_pane_state`'s `resolve_git` kwarg (line 579)
- `apps/cc-tmux/src/cc_tmux/testing.py` (3682 lines) — self-test suite; tests OF deleted code get deleted, tests of live code stay
- `apps/cc-tmux/src/cc_tmux/__init__.py` — line 10 claims `paths` as public surface
- `apps/cc-tmux/skills/cc-status/SKILL.md`, `apps/cc-tmux/skills/cc-config/SKILL.md` — plugin skills referencing `cc-tmux status`/`status-inbox`/`window-icon`
- `apps/cc-tmux/.claude-plugin/plugin.json` + `marketplace.json` — plugin version `0.1.2` (bump required because skills change)
- `apps/cc-tmux/README.md` — line 70 lists `paths.py` in the module tree

Key verified facts the executor must know:

1. **Nothing live invokes any deleted subcommand.** The repo's only tmux
   status wiring is `home/dot_config/tmux/tmux.conf.tmpl:270`:
   ```
   set -g status-format[0] "#(~/.tmux/plugins/cc-tmux/bin/cc-tmux render-all #{window_id})"
   ```
   with rows 1/2 reading `#{@cc-row-session}` / `#{@cc-row-beads}` options.
   Verified live on this machine (homelab): `tmux show-options -g | grep
   status-format` shows exactly render-all + the two option lookups.
   `apps/cc-tmux/cc-tmux.tmux` invokes only: switch, cycle, back,
   picker-data, inbox, inbox-clear, accounts-popup, accounts-popup-launch,
   conductor, focus, discover. `apps/cc-tmux/hooks/hooks.json` invokes only:
   `register`, `conductor context`, `clear`.
2. **`~/.tmux/plugins/cc-tmux` is a symlink to
   `$DOTFILES/apps/cc-tmux`** (verified: `readlink` resolves to
   `/home/nyaptor/dev/personal/installfest/apps/cc-tmux`), so "deployed
   plugin" == repo on this machine.
3. **The CC plugin cache** (`~/.claude/plugins/cache/cc-tmux/cc-tmux/0.1.2`,
   the `installPath` in `~/.claude/plugins/installed_plugins.json`) is a
   snapshot; its `hooks.json` invokes only register/conductor/clear. Cache
   copies 0.1.0/0.1.1 wire `status-inbox` in their `cc-tmux.tmux:121`, but
   tmux never loads the cache's entrypoint — only
   `~/.tmux/plugins/cc-tmux/cc-tmux.tmux` (the repo symlink). Stale cache
   dirs are not callers.
4. **The self-test baseline at 9399b92 is 105/106 with ONE pre-existing,
   environment-sensitive failure**: `cli.beads_pane_fallback` fails when a
   live tmux server has tracked panes (the test monkeypatches
   `get_window_top_pane`/`get_window_active_pane` but not
   `tmux.get_pane_option`, so the real server's pane state leaks in). This
   failure is UNRELATED to this plan — do not fix it here, and do not let it
   block the done criteria (see Done criteria).
5. **The one adaptation vs. the audit evidence**: the audit called
   `cmd_status`/`cmd_status_inbox` consumer-free, but
   `apps/cc-tmux/skills/cc-status/SKILL.md:23-35` instructs running
   `cc-tmux status-inbox` and `cc-tmux status` (a live routing surface — the
   skill ships in the installed plugin). Step 9 therefore edits that skill in
   the same step; the skill's other two commands (`inbox`, `picker-data`)
   stay and already cover its purpose.

Excerpts proving each target is dead (grep-verified repo-wide at 9399b92;
the only references are the internal chains, docstrings, and the self-tests
listed per step):

`cli.py:989-997` (ENT-01):
```
    Consumption note (as of nx-yn6c2): ZERO production callers read this
    function anymore. ... This
    function and its 5-tuple parsing are retained only for
    :mod:`testing`'s existing coverage of the legacy file shape — no new
    caller should be added; MUST NOT be assumed live.
```

`render.py:350-357` (ENT-02):
```
# _BAR_FILLED/_BAR_EMPTY: retained (unlike CONTEXT_BAR_WIDTH and the
# render_context_bar tmux-format function, both retired by
# cc-tmux-braille-usage-glyph task 3.3) — _context_bar_parts below still
# builds the shade-block ``bar`` string from these, and is itself still a
# live dependency of render_context_bar_ansi (the ANSI counterpart; no tmux
# real caller today, but kept per the same change's task instructions).
_BAR_FILLED = "▓"
_BAR_EMPTY = "░"
```

`testing.py:25` (the ONLY import of `paths` anywhere, ENT-03):
```
from . import cli, conductor, nx_agent, paths, priority, registry, render, tmux, usage
```

`usage.py:220-224` (ENT-04 — the dead copy still scans the RAW list with no
`dedupe_credentials`, preserving the stale-duplicate bug `extract_active`
fixed):
```
    active = None
    for candidate in credentials:
        if isinstance(candidate, dict) and candidate.get("isActive") is True:
            active = candidate
            break
```

`cli.py:1365-1367` (ENT-05, same "kept for other machines" note on all three
wrappers):
```
    refresh. Thin wrapper over :func:`_build_session_bar` — kept for other
    machines' deployed confs that still call this subcommand directly until
    they re-apply the plan-005 conf change (see Maintenance notes).
```

`cli.py:60-61` (ENT-08 — the stub map is empty and the branch unreachable):
```
# No remaining stub subcommands: every registered command has a handler below.
_STUB_OWNERS: Dict[str, str] = {}
```

`tmux.py:579` (ENT-10 — no call site anywhere passes `resolve_git=`; tests
use the `git_resolver` seam):
```
    resolve_git: Optional[bool] = None,
```

## Commands you will need

| Purpose | Command | Expected on success |
|---------|---------|---------------------|
| Repo quality gate | `bash scripts/check.sh` | exit 0, last line `ALL CHECKS PASSED` (baseline verified green at 9399b92) |
| cc-tmux test suite | `apps/cc-tmux/bin/cc-tmux self-test` | exit code == number of failed tests; baseline `105/106 passed, 1 FAILED` (`cli.beads_pane_fallback` only — see Current state fact 4) |
| Python syntax check | `python3 -m compileall -q apps/cc-tmux/src/cc_tmux` | exit 0, no output |
| Live render evidence | `apps/cc-tmux/bin/cc-tmux render-all "$(tmux display-message -p '#{window_id}')"` | non-empty tabs-row string printed (run from inside tmux) |
| Dead-ref sweep | see Done criteria grep | 0 hits |

Run all commands from the repo root: `/home/nyaptor/dev/personal/installfest`.
`npm run check` is an alias for `bash scripts/check.sh`. Note `check.sh` does
NOT lint Python — `compileall` + `self-test` are the Python gates.

## Scope

**In scope** (the only files you may modify):

- `apps/cc-tmux/src/cc_tmux/cli.py`
- `apps/cc-tmux/src/cc_tmux/render.py`
- `apps/cc-tmux/src/cc_tmux/usage.py`
- `apps/cc-tmux/src/cc_tmux/parser.py`
- `apps/cc-tmux/src/cc_tmux/paths.py` (delete the file)
- `apps/cc-tmux/src/cc_tmux/testing.py`
- `apps/cc-tmux/src/cc_tmux/tmux.py`
- `apps/cc-tmux/src/cc_tmux/__init__.py`
- `apps/cc-tmux/skills/cc-status/SKILL.md`
- `apps/cc-tmux/skills/cc-config/SKILL.md`
- `apps/cc-tmux/README.md` (one line: the module tree)
- `apps/cc-tmux/.claude-plugin/plugin.json`, `apps/cc-tmux/.claude-plugin/marketplace.json` (version bump only)
- `apps/cc-tmux/src/cc_tmux/notify/windows.py` + `notify/__init__.py` — ONLY if Step 11's operator gate is explicitly confirmed
- `plans/README.md` (status row only)

**Out of scope** (do NOT touch, even though they look related):

- `apps/cc-tmux/cc-tmux.tmux` — keybinding/wiring entrypoint; not owned by
  this plan. Its comment near line 113 mentions `cmd_status_inbox` — leave
  the stale comment; it is wiring-file prose, recorded in Maintenance notes.
- `apps/cc-tmux/hooks/hooks.json` — hook wiring, verified already free of
  deleted subcommands; changing it would force a hooks-surface migration.
- `home/dot_config/tmux/tmux.conf.tmpl` and the theme `.conf` files — already
  render-all-only; no edits needed.
- `apps/cc-tmux/src/cc_tmux/conductor.py`, `nx_agent.py`, `priority.py`,
  `registry.py`, `notify/linux.py`, `notify/macos.py`, `notify/base.py`,
  `bin/`, `__main__.py` — no dead surfaces here.
- `apps/cc-tmux/pyproject.toml` — its `version = "0.1.0"` is already
  inconsistent with plugin.json's 0.1.2; syncing it is a separate decision.
- KEEP (do not delete even though they sit next to deleted code):
  `format_context_tokens`, `resolve_context_color`, `_context_color_pair`
  (live: session-bar SES label + idle meter), `resolve_tab_icon` (live:
  `resolve_tab_glyph` render.py:236 wraps it), `_ANSI_RESET`/`strip_ansi`
  (live: popup + testing), `color_for`/`pct_for`/`_extract_util`/
  `_account_label`/`_account_identity`/`_extract_reset_at`/`_query`/
  `dedupe_credentials`/`_freshest_active`/`extract_active`/`active_usage`
  and every colour constant in usage.py (live: row 2 + accounts popup),
  `tmux.get_window_top_pane` (live: `_resolve_session_pane`,
  `_window_subagent_counts`'s replacement path), `_cc_state_dir` (live:
  register trace + roadmap-pulse), `_build_session_bar`/`_build_beads_bar`/
  `_build_tabs_row`/`cmd_render_all` (the live render path), the
  `git_resolver` kwarg of `set_pane_state` (live test seam).

## Git workflow

- Work on `main` (repo convention: ad-hoc lane, direct commits — see recent
  history, e.g. `adb2529 fix(cc-tmux): extract_active picks freshest ...`).
- ONE commit for the whole sweep. Write the message to a file first and use
  `git commit -F <file>` (never a HEREDOC chained with `&&`). Suggested:
  `chore(cc-tmux): delete ten dead surfaces (plan 014 entropy sweep)` with a
  body listing ENT-01..ENT-10 dispositions.
- Stage targeted paths only (`git add apps/cc-tmux/... plans/README.md`),
  plus `git add .beads/` if beads changed. NEVER `git add .` / `-A`.
  Note `git rm apps/cc-tmux/src/cc_tmux/paths.py` stages the deletion.
- Push after commit per this repo's session-close convention, unless the
  dispatching operator said to hold.
- This machine's `core.hooksPath` is `.beads/hooks` (bd-managed) — do not
  edit git hooks; the pre-commit hook runs automatically.

## Steps

Order matters only where noted; every step ends with the same cheap gate:

```bash
python3 -m compileall -q apps/cc-tmux/src/cc_tmux && apps/cc-tmux/bin/cc-tmux self-test | tail -2
```

Expected after each step: compile exits 0; self-test total shrinks as rows
are removed and the ONLY failure ever shown is `cli.beads_pane_fallback`
(pre-existing). Any OTHER failure = you deleted a test of live code or a
live dependency — STOP condition after one fix attempt.

### Step 0: Preflight + fleet runtime gate

1. Confirm clean tree at/after 9399b92 and run the drift check from the header.
2. Record baselines:
   - `bash scripts/check.sh` → exit 0.
   - `apps/cc-tmux/bin/cc-tmux self-test | tail -2` → `105/106 passed, 1 FAILED` / `FAIL cli.beads_pane_fallback: ...`.
3. **Local runtime gate** (this machine, homelab):
   ```bash
   tmux show-options -g | grep status-format
   ```
   Expected exactly (modulo theme bg colour):
   ```
   status-format[0] "#(~/.tmux/plugins/cc-tmux/bin/cc-tmux render-all #{window_id})"
   status-format[1] "#[bg=#000000]#{@cc-row-session}"
   status-format[2] "#[bg=#000000]#{@cc-row-beads}"
   ```
   Also: `readlink ~/.tmux/plugins/cc-tmux` →
   `/home/nyaptor/dev/personal/installfest/apps/cc-tmux`.
4. **Remote fleet gate** (gates Steps 6, 7, 8, 9 — the surfaces old deployed
   confs could still call). The tmux fleet is this machine + the Mac
   (`ssh mac`, per `ssh-mesh/README.md`; CloudPC is Windows and runs no tmux
   status line):
   ```bash
   ssh mac 'tmux show-options -g 2>/dev/null | grep status-format; grep -cE "session-bar|beads-bar|tabs-row|window-icon|cc-tmux usage|cc-tmux status" ~/.config/tmux/tmux.conf'
   ```
   Expected: the same render-all `status-format[0]` line (or no tmux server
   running — then the conf grep alone decides), and the grep count `0`.
   - If the Mac's conf still wires any legacy subcommand: run
     `ssh mac 'chezmoi apply'` then reload tmux there
     (`ssh mac 'tmux source-file ~/.config/tmux/tmux.conf'`), and re-check.
   - If `ssh mac` is unreachable: STOP condition (see below) — Steps 1–5 may
     still proceed (they touch nothing a deployed conf can invoke), but do
     NOT execute Steps 6–9.

**Verify**: all expected outputs above reproduced; paste them into your report.

### Step 1: ENT-08 — delete `_stub` + `_STUB_OWNERS`

In `apps/cc-tmux/src/cc_tmux/cli.py`:

1. Delete lines 60–61 (the comment + `_STUB_OWNERS: Dict[str, str] = {}`).
2. Delete the whole `_stub` function (lines 2044–2056, from `def _stub(` to
   `return 2`).
3. In `main()` (line ~2069), replace:
   ```python
   handler = _DISPATCH.get(command)
   if handler is None:
       return _stub(command)
   ```
   with:
   ```python
   handler = _DISPATCH.get(command)
   if handler is None:  # defensive: parser and _DISPATCH should always agree
       parser.print_help()
       return 2
   ```

**Verify**: `grep -n '_stub\|_STUB_OWNERS' apps/cc-tmux/src/cc_tmux/*.py` → no
output; step gate passes (still 106 tests — no test rows touched).

### Step 2: ENT-10 — drop `set_pane_state`'s dead `resolve_git` kwarg

In `apps/cc-tmux/src/cc_tmux/tmux.py` (`set_pane_state`, lines 572–631):

1. Remove the parameter line `resolve_git: Optional[bool] = None,` (line 579).
2. In the docstring, replace the sentence
   `Pass ``resolve_git`` to force the decision either way. ``git_resolver`` is an injection seam for tests.`
   with
   `` `git_resolver` is an injection seam for tests.`` (keep the preceding
   "Git identity (invariant 4): resolved only for ``waiting`` / ``idle`` by
   default (``active`` — the hot path — skips it)." sentence).
3. Replace the body block (lines 624–627):
   ```python
   # Hot-path guard: resolve git identity only for pending states unless forced.
   if resolve_git is None:
       resolve_git = state in PENDING_STATES
   if resolve_git:
   ```
   with:
   ```python
   # Hot-path guard: resolve git identity only for pending states.
   if state in PENDING_STATES:
   ```

**Verify**: `grep -rn 'resolve_git' apps/cc-tmux/src/` → no output
(`git_resolver` hits are fine and expected); step gate passes — the four
`tmux.set_pane_state_*` tests still pass (they use `git_resolver`, verified
at 9399b92).

### Step 3: ENT-01 — delete `_read_session_context` + `SESSION_CONTEXT_MAX_AGE_SECS`

In `apps/cc-tmux/src/cc_tmux/cli.py`:

1. Delete lines 953–1035: the comment block starting
   `# session-context.<pane>.json freshness cutoff (plan 003): ...`, the
   constant `SESSION_CONTEXT_MAX_AGE_SECS = 900.0`, and the entire
   `_read_session_context` function (ends with
   `return letter, pct, branch, dirty, ahead`).
2. Prose cleanups (docstrings only — reword minimally, keep everything else):
   - Lines 93–98 (comment in `cmd_register`): replace the final two lines
     `# The session-bar now reads the model letter fresh on every render from`
     / `` # `session-context.<pane>.json` instead — see `_read_session_context`. ``
     with `# The session-bar now reads the model letter fresh on every render`
     / `# from nx-agent — see _resolve_model_letter.`
   - Line 1254 (`_resolve_model_letter` docstring): change
     ``(:func:`_read_session_context`'s index-0 return)`` to
     `(the retired session-context.<pane>.json read)`.
   - Line 1290 (`_build_session_bar` docstring): change
     ``(``session-context.<pane>.json``, via :func:`_read_session_context`)``
     to `` (``session-context.<pane>.json``, since removed)``.
   - Line 1755 (`_parse_roadmap_pulse_counts` docstring): change the example
     ``(e.g. :func:`_read_session_context`'s per-field ``None`` degradation)``
     to ``(e.g. :func:`_resolve_git_status`'s per-field fallback)``.

In `apps/cc-tmux/src/cc_tmux/tmux.py`:

3. Module docstring lines 23–28 (`NOTE (cc-tmux-bar-cleanup): ...`): replace
   the last clause `the session-bar row now reads the model letter fresh on
   every render from ``session-context.<pane>.json`` (see
   cli._read_session_context) instead of from pane-option state.` with
   `the session-bar row now reads the model letter fresh on every render
   from nx-agent (see cli._resolve_model_letter) instead of from
   pane-option state.`

In `apps/cc-tmux/src/cc_tmux/testing.py`:

4. Delete `_test_cli_read_session_context` entirely (lines 2564–2674, from
   `def _test_cli_read_session_context() -> None:` through the
   `shutil.rmtree(tmpdir, ignore_errors=True)` cleanup — the next def is
   `_test_conductor_attach_command` at 2677).
5. Delete its registration row (line 3626):
   `("cli.read_session_context", _test_cli_read_session_context),`
6. In `_test_cli_resolve_model_letter` (lines 1700–1742): delete the
   monkeypatch shim of the deleted function — lines 1709
   (`saved_read_ctx = cli._read_session_context`), 1712–1713 (the `# OLD
   path:` comment + `cli._read_session_context = lambda pane: ...`), and
   1742 (`cli._read_session_context = saved_read_ctx  # type: ignore[assignment]`).
   In its docstring (line 1704), change
   ``per-pane ``session-context.<pane>.json`` file (:func:`cli._read_session_context`),``
   to ``per-pane ``session-context.<pane>.json`` file (since removed),``.
   The rest of the test stays — it covers the LIVE `_resolve_model_letter`.

**Verify**:
`grep -rn '_read_session_context\|SESSION_CONTEXT_MAX_AGE_SECS' apps/cc-tmux/`
→ no output. Step gate passes; self-test total drops to 105.

### Step 4: ENT-02 — delete the ANSI shade-block bar chain

In `apps/cc-tmux/src/cc_tmux/render.py`:

1. Delete the retention comment + constants at lines 350–357 (`# _BAR_FILLED/
   _BAR_EMPTY: retained ...` through `_BAR_EMPTY = "░"`).
2. In the section-header comment just above (lines ~338–348, `# label +
   shade-block fill bar: "252.5k:▓▓▓░░░░░░░". Two independent scales, ...`):
   this block documents the COLOUR tiers too, which survive. Trim only the
   bar-specific first line; keep the two-scales explanation but reword its
   opening to refer to the SES label colour vs fill split historically, or
   simplest: replace the first line with
   `# SES colour tiers (label colour driven by raw_tokens; see below).`
3. Delete `_context_bar_parts` (lines 414–430), `_hex_to_ansi_fg`
   (lines 433–437), and `render_context_bar_ansi` (lines 440–454). KEEP
   `format_context_tokens` (407–411), `resolve_context_color` (392–404), and
   `_context_color_pair` (360–389) — all live (session bar + idle meter).
4. Line 710 comment: change ``see ``_green``/``_hex_to_ansi_fg`` above`` to
   ``see ``_green`` above``.

In `apps/cc-tmux/src/cc_tmux/testing.py`:

5. Rewrite `_test_context_bar_format` (lines 1500–1542): the three
   `format_context_tokens` assertions (lines 1510–1512) cover a LIVE function
   and must survive; everything from line 1514 (`out_ansi = ...`) to 1542
   dies with `render_context_bar_ansi`. Replace the whole function with:
   ```python
   def _test_format_context_tokens() -> None:
       """format_context_tokens: the row-2 SES token-count label."""
       _check(render.format_context_tokens(None) == "--", "no tokens -> '--'")
       _check(render.format_context_tokens(252_500) == "252.5k", "252500 -> '252.5k'")
       _check(render.format_context_tokens(0) == "0.0k", "0 -> '0.0k'")
   ```
6. Update the registration row (line 3603) from
   `("render.context_bar_format", _test_context_bar_format),` to
   `("render.format_context_tokens", _test_format_context_tokens),`.

**Verify**:
`grep -rn 'render_context_bar_ansi\|_context_bar_parts\|_hex_to_ansi_fg\|_BAR_FILLED\|_BAR_EMPTY' apps/cc-tmux/`
→ no output. Step gate passes; total stays 105 (one test replaced in place).
`render.context_bar_colors` (tests `resolve_context_color`) must still pass.

### Step 5: ENT-03 — delete `paths.py`

1. `git rm apps/cc-tmux/src/cc_tmux/paths.py` (whole-module deletion —
   covered by the plan-level operator approval in Status).
2. `apps/cc-tmux/src/cc_tmux/testing.py`:
   - Line 25: remove `paths` from the import list →
     `from . import cli, conductor, nx_agent, priority, registry, render, tmux, usage`
   - Delete the `# paths.py tests` section (lines ~519–549):
     `_test_tmux_conf_candidates`, `_test_find_tmux_conf_override`,
     `_test_find_plugin_dir`, including the section-divider comment.
   - Delete registration rows 3573–3575 (`paths.tmux_conf_candidates`,
     `paths.find_tmux_conf_override`, `paths.find_plugin_dir`).
3. `apps/cc-tmux/src/cc_tmux/__init__.py`: delete line 10
   (`  * :mod:`cc_tmux.paths`    — tmux.conf + plugin-dir detection`).
4. `apps/cc-tmux/README.md`: delete line 70
   (`    paths.py            # tmux.conf + plugin-dir detection`).

**Verify**:
`grep -rn 'cc_tmux.paths\|from \. import.*paths\|paths\.find_\|paths\.tmux_conf\|paths\.plugin_dir\|CC_TMUX_PLUGIN_DIR' apps/cc-tmux/ --include='*.py' --include='*.tmux' --include='*.sh'`
→ no output. `test -f apps/cc-tmux/src/cc_tmux/paths.py` → exits 1.
Step gate passes; self-test total drops to 102.

### Step 6: ENT-04 — delete the legacy `usage` status segment (GATED on Step 0.4)

In `apps/cc-tmux/src/cc_tmux/usage.py`:

1. Delete `render_usage` (lines 209–245), `build_segment` (569–577), and
   `cmd_usage` (580–589; end of file). Also delete the now-orphaned section
   comment `# Query + CLI handler` above `_query` (lines 248–250) — retitle
   it `# Credentials query` (a one-line comment) since `_query` stays.
2. Trim the module docstring (lines 1–41): it describes the retired segment.
   Replace the first line with
   `"""Claude multi-account usage data for the status rows (nx-agent /credentials)."""`-style
   framing: keep the payload-shape block (lines 12–20) and the fail-open
   invariant paragraph (36–38), drop the sh-script render bullets (23–34
   minus the payload block) and the `status-right` framing. Minimal edit is
   fine; the requirement is that no sentence claims a `usage` subcommand or a
   rendered tmux segment from this module.
3. `sys` import: `cmd_usage` was the only user of `sys` in usage.py — check
   with `grep -n 'sys\.' apps/cc-tmux/src/cc_tmux/usage.py`; if no uses
   remain, remove `import sys` (line 47).

In `apps/cc-tmux/src/cc_tmux/cli.py`:

4. Delete line 28: `from .usage import cmd_usage`.
5. Delete the dispatch row (line 2032): `"usage": cmd_usage,`.

In `apps/cc-tmux/src/cc_tmux/parser.py`:

6. Delete the `usage` registration (lines 138–140, comment + `sub.add_parser("usage", ...)`).
7. Module docstring lines 9–16: remove `usage (argless, Req-8),` from the
   implemented-subcommands list (you will prune more of this list in Steps
   7–9; final wording checked in Step 9).

In `apps/cc-tmux/src/cc_tmux/render.py`:

8. Line 562 comment: change ``:func:`cc_tmux.usage.render_usage`, reusing
   that module's ``CYAN``/``DIM```` to ``the retired
   ``cc_tmux.usage.render_usage`` did, reusing that module's ``CYAN``/``DIM````.

In `apps/cc-tmux/src/cc_tmux/testing.py`:

9. Delete `_test_usage_render_segment` (lines 895–918) and
   `_test_usage_fail_open` (lines 921–937), plus registration rows 3592–3593
   (`usage.render_segment`, `usage.fail_open`). All other usage tests
   (color_thresholds, pct_formatting, extract_util, account_label,
   extract_active, extract_reset_at, dedupe_credentials, active_usage_ttl,
   account_identity) cover LIVE functions — keep them.

**Verify**:
`grep -rn 'render_usage\|build_segment\|cmd_usage' apps/cc-tmux/ --include='*.py' --include='*.tmux'`
→ no output (a prose hit of the literal string `render_usage` inside the
render.py:562 comment you just reworded is acceptable if you kept the name;
if so, confirm it is comment-only). `apps/cc-tmux/bin/cc-tmux usage` → prints
argparse error mentioning invalid choice, exit 2. Step gate passes; total
drops to 100.

### Step 7: ENT-05 — delete the plan-005 transitional wrappers (GATED on Step 0.4)

In `apps/cc-tmux/src/cc_tmux/cli.py`:

1. Delete `cmd_session_bar` (lines 1360–1372), `cmd_beads_bar` (1825–1837),
   `cmd_tabs_row` (1884–1898). The `_build_*` functions they wrap are LIVE
   (`cmd_render_all` calls them) — do not touch those.
2. Delete dispatch rows 2036–2038 (`"session-bar"`, `"beads-bar"`,
   `"tabs-row"`).
3. `_build_tabs_row` docstring (line 1843): change `Body of the former
   ``cmd_tabs_row`` handler, extracted (plan 005)` — already says "former",
   keep as-is. In `_build_session_bar`'s docstring (line 1278) `Body of the
   former ``cmd_session_bar`` handler` — also already "former", keep. No
   edit needed; this item is a check, not a change.

In `apps/cc-tmux/src/cc_tmux/parser.py`:

4. Delete the three registrations with their comments: session-bar
   (lines 178–189), beads-bar (191–201), tabs-row (203–213). Keep
   `render-all` (215–227).

In `apps/cc-tmux/src/cc_tmux/render.py`:

5. Line 563 comment: change `The CLI handlers
   (``cmd_session_bar``/``cmd_beads_bar``) read tmux/cache state` to
   `The CLI handler (``cmd_render_all``) reads tmux/cache state`.

In `apps/cc-tmux/src/cc_tmux/tmux.py`:

6. Line 484 docstring: change ``Used by ``cc-tmux tabs-row``
   (:func:`cc_tmux.cli.cmd_tabs_row`) to`` to ``Used by ``cc-tmux
   render-all`` (:func:`cc_tmux.cli.cmd_render_all`) to``.

**Verify**:
`grep -rn 'cmd_session_bar\|cmd_beads_bar\|cmd_tabs_row' apps/cc-tmux/ --include='*.py' --include='*.tmux'`
→ no output. `apps/cc-tmux/bin/cc-tmux session-bar @1` → argparse invalid
choice, exit 2. Step gate passes (no test rows for the wrappers existed).
Then RUNTIME EVIDENCE (from inside tmux):
`apps/cc-tmux/bin/cc-tmux render-all "$(tmux display-message -p '#{window_id}')"`
→ non-empty tabs-row output, and the live status line still renders
(visually, or `tmux show-options -gv '@cc-row-session'` → non-empty within a
few seconds of the render-all call while a Claude pane is tracked).

### Step 8: ENT-06 — delete the window-icon path (GATED on Step 0.4)

In `apps/cc-tmux/src/cc_tmux/cli.py`:

1. Delete `cmd_window_icon` (lines 871–890) and `_window_subagent_counts`
   (lines 764–784). KEEP `prune_background_entries` (741–749) and
   `_subagent_bg_timeout` (752–761) — both live in `_build_tabs_row`.
2. Delete dispatch row 2035 (`"window-icon": cmd_window_icon,`).
3. `_maybe_rename_window` docstring (lines 796–802): replace the sentence
   spanning `The icon is rendered separately, from the tmux
   ``window-status-format`` string itself (``#(cc-tmux window-icon
   #{window_id})``), re-evaluated on every status-bar refresh — see
   tmux.conf.tmpl / the theme ``.conf`` files.` with `The icon is rendered
   separately by the render-all tabs row (see :func:`_build_tabs_row` /
   :func:`render.render_tabs_row`).` Also in the same docstring change
   ``(see ``render.animated_icon`` / ``cmd_window_icon``)`` to
   ``(see ``render.animated_icon``)``.

In `apps/cc-tmux/src/cc_tmux/parser.py`:

4. Delete the window-icon registration + comment (lines 164–176).

In `apps/cc-tmux/src/cc_tmux/tmux.py`:

5. Delete `get_window_top_state` (lines 254–280). KEEP
   `get_window_top_pane` (283+) — live. Fix the two prose refs:
   line 133 (``:func:`get_window_top_state` uses for a single window``) —
   reword to reference `get_window_top_pane`; lines 327/337 (in
   `get_window_tabs`'s docstring, `not one ``get_window_top_state`` call per
   window` and `precedence :func:`get_window_top_state` applies`) — reword
   both to `get_window_top_pane` (same priority logic, still true).
6. Line 258's docstring dies with the function (it referenced
   `cc-tmux window-icon`).

In `apps/cc-tmux/src/cc_tmux/render.py` (prose only — all these functions stay):

7. Section comment lines 46–59: rewrite the mechanism description to name
   the live invoker: replace the clause naming `cli.cmd_window_icon` /
   `#(cc-tmux window-icon #{window_id})` (lines 55–57) with `the render-all
   tabs-row job (status-format[0]) re-renders every window's icon each
   status-interval tick, so :func:`animated_icon` picks a frame purely from
   the caller-supplied wall-clock time`.
8. Line 153 (`:func:`cc_tmux.cli.cmd_window_icon` supplies the real
   ``time.time()``.`): change to `callers supply the real ``time.time()``
   (see :func:`cc_tmux.cli._build_tabs_row`).`
9. Lines 223–232 (`resolve_tab_glyph` docstring): replace
   `:func:`cc_tmux.cli.cmd_window_icon` still calls it directly and must
   keep` (line 224 area) with `legacy callers called it directly and had to
   keep` — or simply reword to state `resolve_tab_icon` remains the
   glyph-precedence core that this wrapper extends.
10. Lines 940–950 (`render_tabs_row` docstring): reword the two
    `cmd_window_icon` references to name `resolve_tab_icon`'s documented
    contract instead (the contract text itself still holds).

In `apps/cc-tmux/src/cc_tmux/testing.py`:

11. Delete `_test_tmux_get_window_top_state` (lines 775–795) and its
    registration row 3586 (`tmux.get_window_top_state`).
12. Line 1800 comment (`# Pane-id analogue of get_window_top_state — mirror
    that test's fixture shape.`): change to `# Priority-pick pane resolution
    — two tracked panes, waiting outranks idle.`
13. Line 2192 comment (`matching cmd_window_icon's existing untracked
    contract`): change to `matching resolve_tab_icon's untracked contract`.

In `apps/cc-tmux/skills/cc-config/SKILL.md`:

14. Line 46 (tab-icon row): replace the mechanism text `Rendered from
    ``window-status-format`` (``cc-tmux window-icon``), not baked into the
    window name` with `Rendered by the render-all tabs row
    (status-format[0]), not baked into the window name`.

**Verify**:
`grep -rn 'cmd_window_icon\|_window_subagent_counts\|get_window_top_state\|window-icon' apps/cc-tmux/ --include='*.py' --include='*.md' --include='*.tmux'`
→ no output. Step gate passes; total drops to 99. Re-run the Step 7 runtime
render-all evidence — animated tab icons must still appear in the live tabs
row output (glyph characters present for tracked windows).

### Step 9: ENT-07 — delete the unwired `status` / `status-inbox` surfaces (GATED on Step 0.4)

In `apps/cc-tmux/src/cc_tmux/cli.py`:

1. Delete `cmd_status` (lines 673–689) and `cmd_status_inbox` (692–711),
   plus the section header comment above them (lines 669–671, `# Status
   sources + window rename (Req-7, task 1.8)`) — replace with
   `# Window rename (Req-7)` since `_maybe_rename_window` etc. remain below.
2. Delete the now-unused option constants: line 45
   (`_STATUS_FORMAT_OPT = "@cc-status-format" ...`) and line 49
   (`_STATUS_INBOX_STYLE_OPT = "@cc-status-inbox-{state}-style" ...`).
   First confirm no other user: `grep -n '_STATUS_FORMAT_OPT\|_STATUS_INBOX_STYLE_OPT' apps/cc-tmux/src/cc_tmux/cli.py` must show only the definition + the two deleted handlers.
3. Delete dispatch rows 2030–2031 (`"status"`, `"status-inbox"`).
4. Line 715 comment `# View helpers (shared by inbox / picker / status)` →
   `# View helpers (shared by inbox / picker)`.
5. Check `group_by_state` and `pending_panes` imports/usages: `cmd_status`
   used `group_by_state`; `cmd_status_inbox` used `pending_panes`. Run
   `grep -n 'group_by_state\|pending_panes' apps/cc-tmux/src/cc_tmux/*.py`.
   Both come from `priority` (cli.py:29 import block). If either now has ZERO
   remaining cli.py call sites, remove it from the import list ONLY —
   do NOT delete the functions from priority.py (they have their own tests
   and `pending_panes` is used by cycle logic; verify before touching the
   import).

In `apps/cc-tmux/src/cc_tmux/parser.py`:

6. Delete lines 135–136 (`sub.add_parser("status", ...)` and
   `sub.add_parser("status-inbox", ...)`).
7. Now finalize the module docstring list (lines 9–16) so it names exactly
   the surviving subcommands: register, cycle, back, switch, focus, discover,
   clear, self-test, doctor, inbox, inbox-clear, picker-data,
   accounts-popup, accounts-popup-launch, render-all, conductor.

In `apps/cc-tmux/src/cc_tmux/render.py`:

8. Delete `render_status` (lines 275–289), `DEFAULT_STATUS_FORMAT` (41–42
   incl. comment), `_TOKEN_RE` (44). Confirm `_TOKEN_RE` has no other user:
   `grep -n '_TOKEN_RE' apps/cc-tmux/src/cc_tmux/render.py` → only those two
   sites. (`re` stays imported — `_ANSI_SGR_RE` uses it.)

In `apps/cc-tmux/src/cc_tmux/testing.py`:

9. Delete `_test_render_status` (lines 565–572) and registration row 3577
   (`render.render_status`).

In `apps/cc-tmux/skills/cc-status/SKILL.md`:

10. Remove the two dead command entries from the "How to gather the data"
    block (lines 23–35): delete the `cc-tmux status-inbox` entry (lines
    24–25) and the `cc-tmux status` entry (lines 27–28), keeping `cc-tmux
    inbox` and `cc-tmux picker-data`. The rest of the skill already reports
    from inbox/picker-data rows; no other edits needed.

In `apps/cc-tmux/skills/cc-config/SKILL.md`:

11. Delete line 47 (the `@cc-status-format` option row — its consumer is gone).

**Verify**:
`grep -rn 'cmd_status\|render_status\|DEFAULT_STATUS_FORMAT\|status-inbox\|@cc-status-format' apps/cc-tmux/ --include='*.py' --include='*.md'`
→ no output EXCEPT `apps/cc-tmux/cc-tmux.tmux` (out of scope, see Maintenance
notes) and historical mentions under `openspec/`. Step gate passes; total
drops to 98. `apps/cc-tmux/bin/cc-tmux status` → argparse invalid choice,
exit 2.

### Step 10: plugin version bump (required — skills changed)

Plugin-visible surfaces changed (two SKILL.md files, README). Per the wave-1
plugin-snapshot propagation gate, bump the plugin version so the CC plugin
cache refresh picks the changes up:

1. `apps/cc-tmux/.claude-plugin/plugin.json` line 3: `"version": "0.1.2"` →
   `"0.1.3"`.
2. `apps/cc-tmux/.claude-plugin/marketplace.json` line 14:
   `"version": "0.1.2"` → `"0.1.3"`.

**Verify**: `grep -n '0.1.3' apps/cc-tmux/.claude-plugin/*.json` → both files
hit. (Cache propagation happens on the next plugin update; the currently
installed 0.1.2 cache keeps working — its hooks call only
register/conductor/clear, all untouched.)

### Step 11: ENT-09 — `notify/windows.py` (OPERATOR-GATED, default SKIP)

DO NOT execute without an explicit operator (Leo) confirmation that no
cygwin/native-Windows-python-inside-tmux use case exists (CloudPC is Windows
11; WSL reports `linux` and would NOT use this backend). If unconfirmed:
skip, and record "Step 11 skipped — operator gate not confirmed" in your
report. If confirmed:

1. `git rm apps/cc-tmux/src/cc_tmux/notify/windows.py`.
2. In `apps/cc-tmux/src/cc_tmux/notify/__init__.py` `_platform_module`
   (lines 41–59): delete the `if plat.startswith("win") or plat == "cygwin":`
   branch (lines 53–56).

**Verify**: `grep -rn 'windows' apps/cc-tmux/src/cc_tmux/notify/` → no
output; step gate passes.

### Step 12: final gates + commit

1. `python3 -m compileall -q apps/cc-tmux/src/cc_tmux` → exit 0.
2. `apps/cc-tmux/bin/cc-tmux self-test | tail -2` → `97/98 passed, 1 FAILED`
   with the only FAIL being `cli.beads_pane_fallback` (or `98/98 passed` if
   run with no live tracked panes). Any other failure = STOP.
3. `bash scripts/check.sh` → exit 0, `ALL CHECKS PASSED`.
4. Full dead-symbol sweep (expect NO output):
   ```bash
   grep -rnE '_read_session_context|SESSION_CONTEXT_MAX_AGE_SECS|render_context_bar_ansi|_context_bar_parts|_hex_to_ansi_fg|_BAR_FILLED|_BAR_EMPTY|cc_tmux\.paths|CC_TMUX_PLUGIN_DIR|build_segment|cmd_usage|cmd_session_bar|cmd_beads_bar|cmd_tabs_row|cmd_window_icon|_window_subagent_counts|get_window_top_state|cmd_status|render_status|DEFAULT_STATUS_FORMAT|_STUB_OWNERS' \
     apps/cc-tmux/ home/ scripts/ --include='*.py' --include='*.tmux' --include='*.sh' --include='*.tmpl' --include='*.conf' --include='*.md' --include='*.json' | grep -v __pycache__
   ```
   (Hits under `openspec/changes/archive/`, `docs/`, or `plans/` are
   historical records, excluded by the path list above; a hit in
   `apps/cc-tmux/cc-tmux.tmux`'s comment for `cmd_status_inbox` is the one
   accepted, documented exception.)
5. Runtime evidence (from inside tmux): the Step 7 render-all command emits a
   non-empty row AND the visible status line still shows tabs + rows 2/3.
   Paste the command output into your report.
6. Deployed-plugin coherence: `readlink ~/.tmux/plugins/cc-tmux` still
   resolves into the repo (symlink deploy — repo state IS deployed state on
   this machine).
7. `git status --porcelain` → only in-scope files modified/deleted.
8. Commit (message via `Write` to `/tmp/commit-msg-$$.txt`, then
   `git commit -F`), targeted `git add`/`git rm` paths only; push as a
   separate command per repo convention.
9. Update the plan 014 row in `plans/README.md` (status DONE +
   `spec-impact: none`, direct commit).

## Test plan

This is a deletion plan: no new behavior, so no new tests. The test work is
subtractive and must be EXACT — delete only the tests OF deleted code:

| Deleted test | Registration row removed | Dies with |
|---|---|---|
| `_test_cli_read_session_context` (testing.py:2564–2674) | `cli.read_session_context` (3626) | ENT-01 |
| ANSI-bar half of `_test_context_bar_format` (1514–1542; function rewritten) | row renamed to `render.format_context_tokens` (3603) | ENT-02 |
| `_test_tmux_conf_candidates` / `_test_find_tmux_conf_override` / `_test_find_plugin_dir` (523–549) | 3 `paths.*` rows (3573–3575) | ENT-03 |
| `_test_usage_render_segment` (895–918), `_test_usage_fail_open` (921–937) | `usage.render_segment`, `usage.fail_open` (3592–3593) | ENT-04 |
| `_test_tmux_get_window_top_state` (775–795) | `tmux.get_window_top_state` (3586) | ENT-06 |
| `_test_render_status` (565–572) | `render.render_status` (3577) | ENT-07 |

Expected suite size: 106 → 98 registered tests. Structural pattern to mimic
when rewriting `_test_format_context_tokens`: any short pure-render test in
the same file, e.g. `_test_render_format_duration` (registration row
`render.format_duration`). Tests that MUST still pass because they cover
survivors: `render.context_bar_colors`, `usage.color_thresholds`,
`usage.pct_formatting`, `usage.extract_active`, `usage.dedupe_credentials`,
`usage.active_usage_ttl`, `tmux.get_window_top_pane`, all four
`tmux.set_pane_state_*` rows, `cli.resolve_model_letter`,
`render.resolve_tab_icon`, `render.resolve_tab_glyph_precedence`,
`render.tabs_row`.

## Done criteria

Machine-checkable. ALL must hold:

- [ ] `python3 -m compileall -q apps/cc-tmux/src/cc_tmux` exits 0
- [ ] `apps/cc-tmux/bin/cc-tmux self-test` reports 98 total tests with 0
      failures, OR exactly one failure that is `cli.beads_pane_fallback`
      (pre-existing at baseline 9399b92 — verified failing BEFORE any change
      when a live tmux server has tracked panes)
- [ ] `bash scripts/check.sh` exits 0 (`ALL CHECKS PASSED`)
- [ ] The Step 12.4 dead-symbol grep returns no output (sole documented
      exception: the `cmd_status_inbox` comment in `apps/cc-tmux/cc-tmux.tmux`)
- [ ] `apps/cc-tmux/src/cc_tmux/paths.py` does not exist
- [ ] Each deleted subcommand (`usage`, `status`, `status-inbox`,
      `window-icon`, `session-bar`, `beads-bar`, `tabs-row`) now exits 2 with
      an argparse invalid-choice error
- [ ] Runtime evidence pasted: `cc-tmux render-all <window_id>` emits a
      non-empty tabs row on the live server post-change
- [ ] `tmux show-options -g | grep status-format` (local) and the Step 0.4
      Mac check show render-all-only wiring, captured BEFORE Steps 6–9 ran
- [ ] Plugin version is 0.1.3 in both `.claude-plugin/plugin.json` and
      `.claude-plugin/marketplace.json`
- [ ] `git status` shows no modified files outside the in-scope list
- [ ] `plans/README.md` status row updated

## STOP conditions

Stop and report back (do not improvise) if:

- The drift check shows any in-scope file changed since `9399b92` and the
  "Current state" excerpts no longer match (line numbers here are exact to
  that commit; concurrent plans 009–013 may land first — re-anchor by symbol
  name, and STOP only if a target symbol is missing or has grown a caller).
- `ssh mac` is unreachable or the Mac's tmux conf still wires any legacy
  subcommand after a `chezmoi apply` + reload attempt → skip Steps 6–9
  entirely, ship Steps 1–5 + 10, and report which steps were deferred.
- The self-test after any step shows a NEW failure (anything other than
  `cli.beads_pane_fallback`) and one fix attempt does not resolve it.
- A grep in any step's Verify finds a caller this plan says does not exist
  (e.g. a new `render_context_bar_ansi` or `paths.` import landed since
  9399b92).
- Removing `group_by_state`/`pending_panes` from cli.py's import (Step 9.5)
  turns out to break another cli.py call site — leave the import and report.
- The operator gate for Step 11 is unconfirmed — skip it (this is the normal
  path, not an error; just record it).
- You find yourself needing to edit `cc-tmux.tmux`, `hooks/hooks.json`, or
  any `home/dot_config/tmux/` file — that means a live wiring reference
  exists that this plan says does not; stop.

## Maintenance notes

- **The recurring pattern this closes**: every cc-tmux feature migration left
  its predecessor wired (plan-005 wrappers, nx-agent migration, braille
  glyph, row-2 usage). Going forward, any "kept for transition/tests" surface
  must carry a `cc-debt: <ceiling>, <upgrade path> [beads:if-xxxx]` marker
  per the repo deletion-bar exception rule — prose-only keeps are what let
  these ten accumulate.
- **Plugin cache lag is expected**: the installed CC plugin (0.1.2 snapshot
  at `~/.claude/plugins/cache/cc-tmux/cc-tmux/0.1.2`) keeps running the
  pre-sweep code for hooks (register/clear/conductor — all untouched) until
  the 0.1.3 update propagates. Stale cache dirs 0.1.0/0.1.1 still contain
  `status-inbox` wiring in their entrypoints; they are inert (tmux loads the
  repo symlink's entrypoint) and may be pruned by normal plugin-cache GC.
- **Out-of-scope stale prose left behind deliberately**:
  `apps/cc-tmux/cc-tmux.tmux` comment (~line 113) mentions
  `cmd_status_inbox`; `apps/cc-tmux/README.md:10` says "multi-account usage
  segment" (row 2 still renders usage gauges, so it is arguably still true);
  `pyproject.toml` version drift (0.1.0). Clean these the next time those
  files are touched for real work.
- **Reviewer scrutiny points**: (1) the `_test_cli_resolve_model_letter` shim
  removal (Step 3.6) must not weaken the live assertions — the three
  nx-agent-path checks stay; (2) Step 9.5's import pruning must be
  evidence-based (grep output), not assumed; (3) confirm the render.py
  colour-tier functions (`_context_color_pair`, `resolve_context_color`,
  `format_context_tokens`) survived Step 4 — they are visually load-bearing
  on row 2.
- **Deferred follow-ups**: fixing the pre-existing `cli.beads_pane_fallback`
  live-server leak (monkeypatch `tmux.get_pane_option` in that test) is a
  one-line test fix but belongs to a bug bead, not this sweep; ENT-09
  (windows notify backend) remains Leo's call if Step 11 was skipped; the
  nx-agent-side credentials-row prune (if-lp8v/if-m5q6) is server-side work
  that keeps `dedupe_credentials` necessary until it lands.
