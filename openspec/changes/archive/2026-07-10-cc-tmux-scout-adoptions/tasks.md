<!-- beads:epic:if-bqw -->
<!-- beads:feature:if-mgt -->

# Tasks: cc-tmux-scout-adoptions

> Batches follow this repo's convention (Script → Config → Verification), matching the archived
> `cc-tmux-plugin` change. Owner: general-purpose engineer agents.

## Script Batch

- [x] [1.1] [P-1] Add `@cc-visited` to the pane-option surface in `apps/cc-tmux/src/cc_tmux/tmux.py`: extend the `get_hop_panes()` `list-panes -F` format string + `PaneInfo` with a `visited` field (epoch float, 0.0 when unset); add `set_pane_visited(pane_id)` writing the current epoch [owner:general-purpose] [beads:if-y9w]
- [x] [1.2] [P-1] Implement the recency tiebreak in `apps/cc-tmux/src/cc_tmux/priority.py`: `_visited_of()` sibling to `_timestamp_of()`; within-group sort key becomes `(-visited, -timestamp)` in `group_by_state()` and `sort_panes()`; group priority order and pending/cycle semantics unchanged [owner:general-purpose] [beads:if-0vo]
- [x] [1.3] [P-1] Add `cc-tmux focus <pane_id>` subcommand (`parser.py` + `cli.py` `cmd_focus`): stamps `@cc-visited` on the given pane iff it is a tracked pane (has `@cc-state`); silent no-op otherwise; fail-open exit 0 [owner:general-purpose] [beads:if-8x6]
- [x] [1.4] [P-1] Implement `reconcile()` in `apps/cc-tmux/src/cc_tmux/tmux.py`: extract the inbox-open self-heal (process scan → clear stale `@cc-state`) into a shared function rate-limited by a `@cc-last-reconcile` tmux global-option epoch stamp (min interval 10s, overridable via `@cc-reconcile-interval`); wire it into the `inbox`, `picker-data`, `cycle`, and `status` handlers in `cli.py` [owner:general-purpose] [beads:if-dag]
- [x] [1.5] [P-1] Implement `cc-tmux doctor` (`parser.py` + `cli.py` `cmd_doctor`): PASS/FAIL/WARN rows for tmux ≥ 3.2, fzf on PATH, python ≥ 3.10, `$TMUX` set, `~/.tmux/plugins/cc-tmux` symlink resolves, Claude plugin registered (`claude plugin list`, WARN if `claude` absent), `pane-focus-in[9909]` present when `@cc-track-focus` is on, tracked-pane count; always exit 0 [owner:general-purpose] [beads:if-523]
- [x] [1.6] [P-2] Extend `apps/cc-tmux/src/cc_tmux/testing.py` self-test: visited-beats-timestamp within a group, group order unchanged by visits, missing-visited timestamp fallback, reconcile rate-limit stamp logic (pure part) [owner:general-purpose] [beads:if-bts]
- [x] [1.7] [P-1] Add the fzf preview to the inbox/picker popups in `apps/cc-tmux/cc-tmux.tmux`: `--delimiter '\t' --with-nth 1 --preview 'tmux capture-pane -ep -t {2} | tail -40'` with a right-side preview window; `display-menu` fallback unchanged [owner:general-purpose] [beads:if-7lp]
- [x] [1.8] [P-1] Install the focus hook in `apps/cc-tmux/cc-tmux.tmux`: `set-hook -g 'pane-focus-in[9909]' "run-shell -b '<plugin-dir>/bin/cc-tmux focus #{pane_id}'"` when `@cc-track-focus` is not off, `set-hook -gu 'pane-focus-in[9909]'` when off (mirror tmux-scout's idempotent fixed-slot idiom) [owner:general-purpose] [beads:if-lzb]

## Config Batch

- [x] [2.1] [P-2] Document the new surface in `apps/cc-tmux/README.md`: `doctor` + `focus` subcommands, `@cc-track-focus`, `@cc-reconcile-interval`, preview pane behavior [owner:general-purpose] [beads:if-buv]
- [x] [2.2] [P-2] Update `docs/tmux-layout-keybindings.md` with the preview-pane note on the inbox/picker entries and a one-line pointer to `cc-tmux doctor` for troubleshooting [owner:general-purpose] [beads:if-ze9]

## Verification Batch

- [x] [3.1] [P-1] Run `cc-tmux self-test` — all cases incl. the new recency/reconcile ones pass (paste stdout) [owner:general-purpose] [beads:if-jd5]
- [x] [3.2] [P-1] Open the inbox with ≥2 tracked panes; assert the preview panel renders the highlighted pane's tail and switches as the highlight moves (paste `tmux display` evidence or describe observed popup with capture-pane output) [owner:general-purpose] [beads:if-51d]
- [x] [3.3] [P-1] Kill a tracked pane's Claude process (`kill -9`), wait past the reconcile interval, run `cc-tmux status`; assert the stale pane's `@cc-state` was cleared without opening the inbox (paste `tmux show-options -p -t <pane>` before/after) [owner:general-purpose] [beads:if-2cd]
- [x] [3.4] [P-1] Run `cc-tmux doctor` inside tmux on this machine (all PASS, exit 0 — paste stdout + `echo $?`) and via `env -u TMUX cc-tmux doctor` (FAIL row for `$TMUX`, still exit 0 — paste both) [owner:general-purpose] [beads:if-38j]
- [x] [3.5] [P-1] Source tmux.conf twice; assert exactly one `pane-focus-in[9909]` hook exists (`tmux show-hooks -g | grep -c 'pane-focus-in\[9909\]'` → 1, paste output); focus two panes in sequence and assert their `@cc-visited` stamps order the inbox accordingly (paste inbox rows) [owner:general-purpose] [beads:if-rn9]
- [x] [3.6] [P-2] Set `@cc-track-focus off` and reload; assert the hook slot is unset and no new `@cc-visited` stamps appear on focus (paste `tmux show-hooks -g` grep) [owner:general-purpose] [beads:if-gi3]
