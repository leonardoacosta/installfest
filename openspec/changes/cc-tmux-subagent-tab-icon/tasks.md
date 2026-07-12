<!-- beads:feature:if-murq -->

<!-- beads:epic:if-bqw -->

# Tasks: cc-tmux-subagent-tab-icon

> Literal `## DB/API/UI/E2E Batch` headers per `/feature`'s wave-plan-build contract — mapped by
> domain fit (DB = pane-option state + hook wiring; API = cli.py/render.py logic; UI = the glyph
> mapping's exact 4-way resolution, since it's a user-facing design decision; E2E = tests + live
> verification). Owner: general-purpose engineer agents.

## DB Batch

- [x] [1.1] [P-1] Confirm with the user, before writing `render.py` logic: the clarified mapping
  is "foreground 1 -> `◎`, foreground 2+ -> `◉`, background replaces the originally-proposed
  failure state" — but whether that means background gets its OWN two glyphs (a 6-way condition
  set: fg-1/fg-2+/bg-1/bg-2+, needing 2 more marks beyond the 4 the user named) or the 4 named
  glyphs split as fg-1/fg-2+/bg-1/bg-2+ directly (using `◎`/`◉` a second time for background, or
  a different unambiguous assignment) was not resolved in the `/openspec:explore` session that
  scaffolded this proposal. Resolve this before task 2.3. [owner:general-purpose] [type:api] [beads:if-569r]
- [x] [1.2] [P-1] `apps/cc-tmux/src/cc_tmux/tmux.py`: add `OPT_SUBAGENT_FG = "@cc-subagent-fg"`
  (int count) and `OPT_SUBAGENT_BG = "@cc-subagent-bg"` (JSON list of launch epoch timestamps) to
  the tracked pane-option table and `_ALL_OPTS` (cleared on `SessionEnd` like every other tracked
  option). [owner:general-purpose] [type:api] [beads:if-xumj]
- [x] [1.3] [P-1] `apps/cc-tmux/hooks/hooks.json`: add a `PreToolUse` entry matched on `"Task"`
  (mirroring the already-live matcher shape confirmed in `~/dev/cc/settings.json`'s own
  `Task`-matched `PostToolUse` telemetry hook) calling `cc-tmux register --state active
  --subagent-start`; extend the existing unmatched `PostToolUse` entry (or add a `"Task"`-matched
  one ahead of it) to call `cc-tmux register --state active --subagent-stop`.
  [owner:general-purpose] [type:api] [beads:if-96lw]

## API Batch

- [x] [2.1] [P-1] `apps/cc-tmux/src/cc_tmux/cli.py` (`cmd_register`): handle
  `--subagent-start`/`--subagent-stop` flags — increment/decrement `@cc-subagent-fg` via
  `tmux.py` primitives, floored at 0 (a stray stop with no matching start must not go negative).
  If the hook payload exposes whether the `Task` dispatch was `run_in_background` (check the
  actual payload shape live — do not assume the field name), branch to task 2.2's background path
  instead of the foreground counter. [owner:general-purpose] [type:api] [beads:if-l4w9]
- [x] [2.2] [P-1] `apps/cc-tmux/src/cc_tmux/cli.py`: on a detected background dispatch, append the
  current epoch to `@cc-subagent-bg`'s JSON list; provide a pure helper (e.g.
  `prune_background_entries(entries, now, timeout)`) that filters out entries older than
  `@cc-subagent-bg-timeout` (default a few minutes, overridable global option) — called on every
  read, not just write, so aging is lazy and correct even with no further hook activity.
  [owner:general-purpose] [type:api] [beads:if-0tuw]
- [x] [2.3] [P-1] `apps/cc-tmux/src/cc_tmux/render.py` (`animated_icon` or a new wrapper): resolve
  the mapping from task 1.1, add the 4-glyph selection logic, foreground-precedence-over-background
  per the spec delta's scenario. [owner:general-purpose] [type:api] [beads:if-kejf]

## UI Batch

- [x] [3.1] [P-2] If task 1.1 introduces new global options (e.g.
  `@cc-subagent-bg-timeout`), document their defaults in `home/dot_config/tmux/tmux.conf.tmpl`
  alongside the existing `@cc-window-rename`/`@cc-cycle-mode` style option-setting lines (no new
  theme-file changes expected — this feature has no new status-bar row, only a per-window icon
  change already wired into the existing tabs row). [owner:general-purpose] [type:config] [beads:if-brvk]

## E2E Batch

- [x] [4.1] [P-1] Extend `apps/cc-tmux/src/cc_tmux/testing.py`: self-test cases for the
  increment/decrement pair (including the floored-at-0 stray-stop case), `prune_background_entries`
  (fresh entries kept, stale entries pruned), and the resolved glyph-mapping logic. Run
  `cc-tmux self-test` and paste the passing stdout. [owner:general-purpose] [type:testing] [beads:if-rcbf]
- [x] [4.2] [P-1] Live verification, foreground: dispatch a real foreground sub-agent from a
  tracked pane, observe the tab icon reflect the new glyph while it's in flight, and revert once
  it returns — paste observed output. [owner:general-purpose] [type:testing] [beads:if-pstd]
- [x] [4.3] [P-2] Live verification, background: dispatch a real background agent, observe the
  tab icon during the timeout window and confirm it ages out afterward — paste observed output,
  explicitly noting where the heuristic diverges from the agent's true completion time (if
  observable). [owner:general-purpose] [type:testing] [beads:if-j57i]
