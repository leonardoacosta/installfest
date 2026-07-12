<!-- beads:epic:if-bqw -->
<!-- beads:feature:if-ob4q -->

# Tasks: cc-tmux-tabs-and-rename-fix

## DB Batch

- [x] 1.1 `apps/cc-tmux/src/cc_tmux/render.py`: `render_beads_bar` drops the `next:` segment [beads:if-j237]
  entirely — render only the roadmap-pulse counts line (or `''` if the cache has no counts
  line). Remove the counts-before-next join logic added earlier today; there is no longer
  anything to join. Verify with a direct call showing a two-line cache input (`next: ...` +
  counts) now renders ONLY the counts line.
  - touches: `apps/cc-tmux/src/cc_tmux/render.py`
- [x] 1.2 `apps/cc-tmux/src/cc_tmux/cli.py`: add trace logging to `cmd_register` — every [beads:if-6ip2]
  invocation appends one line (timestamp, `hook_event_name`, resolved `pane_id`,
  `rename_attempted: bool`, `rename_fired: bool`) to a debug log file under
  `~/.claude/scripts/state/cc-tmux-register-trace.log` (rotate/cap similar to other cc-tmux
  state files — do not let it grow unbounded; cap at the last N lines or rotate daily). This is
  diagnostic-only — it does NOT change `_maybe_rename_window`'s behavior, only observes it.
  - touches: `apps/cc-tmux/src/cc_tmux/cli.py`

## API Batch

- [x] 2.1 `apps/cc-tmux/src/cc_tmux/render.py`: new `render_tabs_row(windows, active_window_id) [beads:if-6isi]
  -> str` pure function composing the FULL top-level window-tabs row (icon + index + name per
  window, active-window highlighting) as one string, replacing the broken per-window
  `window-status-format`/`window-status-current-format` job mechanism. Reuses the existing icon
  glyph/animation logic (`animated_icon`/`resolve_icons`) and session-glyph conventions already
  in this module — do not reimplement icon-state mapping a second time.
  - touches: `apps/cc-tmux/src/cc_tmux/render.py`
- [x] 2.2 `apps/cc-tmux/src/cc_tmux/tmux.py`: add a helper enumerating all windows (id, index, [beads:if-dc2f]
  name, tracked Claude pane's highest-priority state if any) for `render_tabs_row`'s input —
  reuse `get_hop_panes`/`priority` module logic rather than re-deriving state precedence.
  - touches: `apps/cc-tmux/src/cc_tmux/tmux.py`
- [x] 2.3 `apps/cc-tmux/src/cc_tmux/parser.py` + `cli.py`: register a new `tabs-row` subcommand [beads:if-oxlx]
  (`cmd_tabs_row`) that gathers live window data via the 2.2 helper and calls
  `render.render_tabs_row`. Invoked as `#(cc-tmux tabs-row)` from a top-level status-format slot
  (wired in the UI batch below), re-evaluated on every status-bar refresh — same daemon-free,
  no-background-process cadence as the existing `session-bar`/`beads-bar` subcommands.
  - touches: `apps/cc-tmux/src/cc_tmux/parser.py`, `apps/cc-tmux/src/cc_tmux/cli.py`
- [x] 2.4 `apps/cc-tmux/src/cc_tmux/testing.py`: update `_test_render_beads_bar` for the [beads:if-0ggk]
  `next:`-removed behavior (delete the counts-before-next assertions from today's earlier fix,
  add a case proving a `next:`-containing cache line is dropped entirely). Add test coverage for
  `render_tabs_row` (icon rendering, active-window highlight, empty-window-list case) and for
  the 1.2 trace-logging helper (writes the expected fields, caps/rotates as designed). Run the
  self-test; paste passing output.
  - touches: `apps/cc-tmux/src/cc_tmux/testing.py`

## UI Batch

- [x] 3.1 `home/dot_config/tmux/tmux.conf.tmpl` + the four theme confs [beads:if-thgc]
  (`tokyo-night-abyss-theme.conf`, `vercel-theme.conf`, `one-hunter-vercel-theme.conf`,
  `nord-theme.conf`): wire `status-format[0]` (or an equivalent top-level slot) to
  `#(~/.tmux/plugins/cc-tmux/bin/cc-tmux tabs-row)`, replacing tmux's default per-window
  `#{W:...#{T:window-status-format}...}` template. Remove the now-dead
  `window-status-format`/`window-status-current-format` `setw` lines from all four theme confs
  (they no longer do anything once status-format[0] is replaced). Each theme's existing
  color palette should still inform the new tabs-row rendering — check whether `render_tabs_row`
  needs a theme-color parameter or whether a single universal palette is acceptable; if the
  four themes' tab colors meaningfully diverge, escalate this as a design question rather than
  silently picking one theme's colors for all.
  - touches: `home/dot_config/tmux/tmux.conf.tmpl`, `home/dot_config/tmux/tokyo-night-abyss-theme.conf`, `home/dot_config/tmux/vercel-theme.conf`, `home/dot_config/tmux/one-hunter-vercel-theme.conf`, `home/dot_config/tmux/nord-theme.conf`

## E2E Batch

- [ ] 4.1 [user] searched: cannot synthesize real multi-hour Claude Code hook-firing cadence [beads:if-29v6]
  within a single /apply run — replaying hook payloads in a tight loop would not reproduce the
  actual timing/ordering of Claude Code's real hook dispatch across a long session, which is
  exactly what's suspected broken; this genuinely needs live elapsed usage time, not something
  resolvable by the orchestrator itself. Run this session normally (or start a fresh Claude Code
  session in a tmux pane) for a period of real usage after the 1.2 trace logging deploys, then
  paste the contents of `~/.claude/scripts/state/cc-tmux-register-trace.log` back for analysis.
- [ ] 4.2 Deploy: `chezmoi apply` for the theme conf + tmux.conf.tmpl changes; reinstall/reload [beads:if-kyaf]
  the live tmux session (`tmux source-file`) so the new `status-format[0]` wiring takes effect.
  Verify nexus-statusline/row-2/row-3 remain unaffected (no regression from today's earlier
  fixes).
- [ ] 4.3 Live verification with pasted evidence: (a) row 3 shows only the counts line, no [beads:if-tdml]
  `next:` anywhere; (b) `cc-tmux tabs-row` invoked directly, and the live rendered tab row
  (byte-captured via `tmux display-message`), both show a real icon glyph per window — sample
  the SAME window at two different wall-clock seconds and show the glyph advancing for a
  `waiting`/`active` state (or confirm a static glyph for `idle`, per spec); (c) window names
  still reflect whatever the current rename format produces (title fix is NOT expected to be
  resolved by this task — only confirm the new tabs-row rendering doesn't regress whatever
  naming currently exists).
- [ ] 4.4 cc-tmux self-test run in the deployed location; paste passing output. [beads:if-7sw2]
