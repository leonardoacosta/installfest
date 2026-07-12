# cc-tmux Tabs Rendering Fix + Row-3 Simplification

Follow-up to `cc-tmux-bar-cleanup`, driven by live feedback during `/openspec:explore`
(2026-07-11 19:20-19:30) — a fresh screenshot showed three more defects.

## Why

1. **Row 3 still shows `next:`**: the just-shipped counts-before-next reorder fix wasn't enough
   — the user does not want the `next:` segment on the tmux row at all, only the roadmap-pulse
   counts.
2. **Tab icons never animate, live**: confirmed with hard runtime evidence, not a guess.
   `cc-tmux window-icon <window>` invoked directly returns the correct glyph (`█ ` for idle).
   The live rendered tab, byte-captured via `tmux display-message -F`, shows no icon character
   at all. Isolated repro: setting `window-status-format` to a literal `TEST#(echo HELLO)END`
   and reading it back via `#{T:window-status-format}` — the exact mechanism tmux's own default
   `status-format[0]` uses to expand each tab — returns `TESTEND`. The job never runs, confirmed
   across 3 retries with 2s delays (rules out async job-cache timing). This is a real tmux 3.6a
   limitation of `#()` jobs nested inside `T:`-expanded strings, not a config-wiring mistake —
   row 2 and row 3's OWN top-level `#()` jobs (in `status-format[1]`/`[2]`) render correctly,
   proving top-level job execution works fine; only the nested-via-`T:` path is broken.
3. **Window titles don't update for long-running panes**: the rename code itself is provably
   correct — replaying a SessionStart-shaped hook payload against a live pane instantly renamed
   its window to `if·Fix cc` exactly per spec (10-char truncation). The bug is that real hook
   traffic across a multi-hour session isn't re-triggering `_maybe_rename_window`, even though
   it runs on every `register` call, not just SessionStart. Root cause isn't pinned yet.

## What Changes

- **`render_beads_bar` (row 3)**: drop the `next:` segment entirely — render only the
  roadmap-pulse counts line (or nothing, if the cache has no counts line). No more line-joining;
  the counts-before-next ordering logic added earlier today is removed along with `next:` itself.
- **Tab rendering (icons + names)**: replace the broken per-window `window-status-format` /
  `window-status-current-format` job wiring with a new top-level `cc-tmux tabs-row` subcommand
  that renders the ENTIRE window-tabs row (icon + index + name per window, active-window
  highlighting) as one string, wired directly into a status-format slot the way row 2/row 3
  already are — top-level `#()` jobs are proven to execute; nested-via-`T:` jobs are not. This
  keeps the "no background process or timer" constraint intact (same status-interval-driven
  re-evaluation cadence as today, just relocated to a job-execution path that actually runs).
- **Window-rename diagnostics**: add trace logging to `cmd_register` (hook_event_name, resolved
  pane, whether a rename was attempted/fired) to a debug log file, so the actual live hook
  cadence can be observed over real usage time instead of guessed at. The rename BUG ITSELF is
  NOT fixed in this proposal — see Non-Goals.

## Non-Goals

- **The window-rename reliability bug is not fixed here.** Its root cause requires live
  hook-trace data gathered over real usage time (see `[user]` task in E2E Batch) — this
  proposal ships the instrumentation only. A follow-up change will implement the actual fix
  once the trace data shows what's actually happening.
- **5H/7D usage still showing `--` is out of scope**, tracked separately as `nx-8ahjt` (nx repo,
  filed before this session) — credential-usage-poller has failed 100% of calls for months,
  unrelated to anything in cc-tmux.
- No changes to row 2 (session identity + usage gauges) — that shipped correctly earlier today.

## Context

- touches: `apps/cc-tmux/src/cc_tmux/render.py`, `apps/cc-tmux/src/cc_tmux/cli.py`, `apps/cc-tmux/src/cc_tmux/parser.py`, `apps/cc-tmux/src/cc_tmux/tmux.py`, `apps/cc-tmux/src/cc_tmux/testing.py`, `home/dot_config/tmux/tokyo-night-abyss-theme.conf`, `home/dot_config/tmux/vercel-theme.conf`, `home/dot_config/tmux/one-hunter-vercel-theme.conf`, `home/dot_config/tmux/nord-theme.conf`, `home/dot_config/tmux/tmux.conf.tmpl`

Design decisions (from `/openspec:explore` + user AskUserQuestion answers, 2026-07-11):
- Icon fix: custom top-level tabs-row rendering (chosen over reverting to a background updater,
  or dropping animation entirely).
- Rename bug: instrument-first — trace logging ships now, the fix ships as a follow-up once
  live trace data is available.

## Testing

| Seam | Coverage |
| --- | --- |
| `render_beads_bar` with `next:` fully removed | cc-tmux self-test (`testing.py`) — unit tasks in DB Batch |
| New `render_tabs_row` pure function (icon/index/name composition, active-window highlight) | cc-tmux self-test — unit tasks in API Batch |
| Register-call trace logging (event/pane/rename-fired fields written correctly) | cc-tmux self-test — unit tasks in DB Batch |
| Live end-to-end: row 3 has no `next:`, tabs animate over 2 wall-clock samples, self-test passes deployed | E2E Batch: chezmoi apply + live capture, pasted |
| Window-rename reliability | N/A — explicitly deferred to a follow-up proposal once trace data is gathered (see Non-Goals) |
