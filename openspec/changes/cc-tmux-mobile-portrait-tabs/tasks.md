<!-- beads:epic:if-bqw -->
<!-- beads:feature:if-rhh8 -->

# Tasks: cc-tmux-mobile-portrait-tabs

> Literal `## DB/API/UI/E2E Batch` headers per `/feature`'s wave-plan-build contract. Task 1.1
> required a human to live-verify OSC 66 — RESOLVED 2026-07-16 via a `tmux display-popup`
> prototype: `printf`-emitted OSC 66 (`s=2`/`s=3`) rendered at NORMAL size, no scaling observed,
> on the terminal actually attached to this tmux session. Every API/UI task below now follows
> the PADDING-ONLY fallback path — the OSC-66 path is retained in `_osc66_scale`/comments as
> dead-but-harmless code (per task 1.2's own note) in case a future terminal/tmux version adds
> real support, but is NOT wired into the active render path this proposal ships.

## DB Batch

- [x] [1.1] [user] RESOLVED: live-verified via a `tmux display-popup` prototype (2026-07-16) — [beads:if-g3h5]
  printed OSC 66 sequences at `s=2` and `s=3` scale inside a real floating pane on the tmux
  session's actual attached terminal. Result: text rendered at NORMAL size in all cases — OSC 66
  scaling did NOT take effect (either the terminal doesn't support it, or tmux's
  `allow-passthrough on` doesn't carry it through cleanly for this construction). Outcome:
  PADDING-ONLY is the fallback path for this proposal, not an unverified contingency — task 2.2
  below implements padding/spacing directly, no conditional branch needed at implementation
  time. [owner:user]
- [x] [1.2] `apps/cc-tmux/src/cc_tmux/render.py`: add a pure `_detect_portrait(client_width: [beads:if-9491]
  int, client_height: int) -> bool` (`client_height > client_width`) and a pure
  `_compute_tab_rows(tab_segments: List[str], client_width: int, mobile: bool) -> int`
  (returns how many physical rows are needed to fit all tab segments without horizontal
  overflow, at 3x width when `mobile` else 1x). Add `_osc66_scale(text: str, scale: int = 3) ->
  str` returning the ESC (0x1B) byte, followed by the literal text `]66;s={scale};{text}`,
  followed by the BEL (0x07) byte — construct via `chr(0x1B)`/`chr(0x07)` in the actual Python
  source, not backslash-escape notation, to sidestep any markdown/shell/JSON escaping ambiguity
  — regardless of task 1.1's outcome (the helper
  itself is harmless to have defined even if unused by the fallback path).
  [owner:general-purpose] [type:api]

## API Batch

- [ ] [2.1] `home/dot_config/tmux/tmux.conf.tmpl`: add `#{client_width}` `#{client_height}` as [beads:if-0v8k]
  additional arguments to the `status-format[0]` render-all job string (alongside the existing
  `#{window_id}`). [owner:general-purpose] [type:api]
- [ ] [2.2] `apps/cc-tmux/src/cc_tmux/cli.py` (`cmd_render_all`/`_build_tabs_row`): accept the [beads:if-13t7]
  new `client_width`/`client_height` arguments, call `_detect_portrait`, and widen each tab
  segment's horizontal padding/spacing (no escape sequence — task 1.1 confirmed OSC 66 does not
  render at scale here) when in portrait mode. Call `_compute_tab_rows` to determine
  `@cc-tab-rows`, and issue `tmux set-option -g @cc-tab-rows <N>` as a side effect before
  emitting the tab-row content, so the theme files' row lookups (task 3.x) see the correct value
  on the SAME render tick. [owner:general-purpose] [type:api]

## UI Batch

- [ ] [3.1] `apps/cc-tmux/src/cc_tmux/render.py` (`render_tabs_row`): when `@cc-tab-rows` > 1, [beads:if-traq]
  split the composed tab segments across that many physical row strings instead of one, and
  issue `tmux set-option -g status <@cc-tab-rows + 2>` as a side effect (landscape/1-row case:
  `status` stays `3`, byte-identical to today). [owner:general-purpose] [type:ui]
- [ ] [3.2] All four theme files (`nord-theme.conf`, `one-hunter-vercel-theme.conf`, [beads:if-261k]
  `vercel-theme.conf`, `tokyo-night-abyss-theme.conf`): replace the hardcoded
  `status-format[1]`/`[2]` `@cc-row-session`/`@cc-row-beads` assignments with a computed index
  read from `@cc-tab-rows` (e.g. via a small tmux conditional or a helper the theme sources) —
  `@cc-tab-rows` resolving to `1` (the common landscape case) MUST reproduce today's exact
  `status-format[1]`/`[2]` assignment, byte-identical. [owner:general-purpose] [type:ui]

## E2E Batch

- [ ] [4.1] Extend `apps/cc-tmux/src/cc_tmux/testing.py`: self-tests for `_detect_portrait` [beads:if-dh5x]
  (landscape/portrait/square-aspect edge case), `_compute_tab_rows` (fits-in-one-row and
  needs-multiple-rows cases), and `_osc66_scale`'s exact escape-sequence output.
  [owner:general-purpose] [type:api]
- [ ] [4.2] Run `python3 -m py_compile` and the full self-test suite (`cd apps/cc-tmux && [beads:if-p6jj]
  python3 -c "import sys; sys.path.insert(0,'src'); from cc_tmux.testing import
  run_self_test; sys.exit(run_self_test())"`) — confirm pass count increases, zero failures.
  Run `./scripts/check.sh` at the repo root (covers the tmux.conf.tmpl/theme-file template
  rendering and shellcheck passes too). Live-verify: force a narrow/tall client window (or use
  `tmux resize-window`/a real portrait terminal, or a `tmux display-popup` prototype as task 1.1
  used), confirm tabs actually widen (padding-based, per the resolved task 1.1 outcome) and wrap
  correctly with the row-2/row-3 content landing at the right shifted index, and confirm the
  landscape case is untouched (compare a real capture before/after this change on a normal
  desktop-width terminal). [owner:general-purpose] [type:api]
