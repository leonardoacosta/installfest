# cc-tmux Spec Delta: cc-tmux-scout-adoptions

## MODIFIED Requirements

### Requirement: Notification inbox lists tracked panes
`cc-tmux inbox` MUST list tracked panes — waiting and idle first, then active — as aligned columns
(state icon, session:window, project, branch, time, wait reason, task), in an fzf popup with a
`display-menu` fallback. Enter SHALL switch to the selected pane; ctrl-x SHALL dismiss waiting/idle
entries as a view filter only. When running in the fzf popup, the highlighted row's tmux pane tail
(last ~40 lines) MUST render in a right-side preview panel via `tmux capture-pane -ep`.

#### Scenario: inbox opens in an fzf popup when available
- Given: tmux ≥ 3.2 and fzf installed, with ≥1 tracked pane
- When: the user opens the inbox
- Then: an fzf popup lists the panes as aligned columns

#### Scenario: preview shows the highlighted pane's tail
- Given: the inbox fzf popup is open with ≥2 tracked panes
- When: the user moves the highlight to a different row
- Then: the right-side preview panel shows the last ~40 lines of that row's pane

#### Scenario: preview degrades with the menu fallback
- Given: fzf is not installed
- When: the user opens the inbox
- Then: a `display-menu` lists the panes (no preview — menu fallback is unchanged)

#### Scenario: dismiss does not change state counts
- Given: a waiting pane shown in the inbox
- When: the user presses ctrl-x to dismiss
- Then: the pane is hidden from the inbox but the status-bar waiting count is unchanged

#### Scenario: self-heal on open
- Given: a pane whose Claude was `kill -9`'d, leaving stale `@cc-state`
- When: the inbox opens and the process scan confirms the process is gone
- Then: that pane's stale state is cleared

### Requirement: Priority-based cycling and jump-back
The plugin MUST cycle panes in priority order `waiting` > `idle` > `active`, in a `priority` or
`flat` mode. Within each state group, panes MUST order by most-recently-visited first
(`@cc-visited`, when focus tracking is on), falling back to most-recent state-change timestamp
for panes never visited. Jump-back SHALL return to the previous pane across sessions and windows.

#### Scenario: priority cycle targets waiting first
- Given: panes in `waiting`, `idle`, and `active`, and `@cc-cycle-mode` is `priority`
- When: the user triggers cycle
- Then: cycling stays within the `waiting` group

#### Scenario: recency breaks ties within a group
- Given: two `waiting` panes A and B, where B changed state more recently but A was visited more recently
- When: the inbox or cycle ring orders the waiting group
- Then: A sorts before B

#### Scenario: unvisited panes fall back to timestamp
- Given: two `idle` panes neither of which has a `@cc-visited` stamp
- When: the group is ordered
- Then: the pane with the newer state-change timestamp sorts first

#### Scenario: active panes are never cycled
- Given: only `active` panes exist
- When: the user triggers cycle
- Then: nothing is cycled (active is listed, not pending)

#### Scenario: jump-back returns to the previous pane
- Given: the user switched from pane A to pane B via cycle
- When: the user triggers jump-back
- Then: focus returns to pane A even across a session/window boundary

## ADDED Requirements

### Requirement: Pane focus is tracked in a pane option via a fixed-index hook slot
The plugin SHALL record every pane focus by setting a `@cc-visited` epoch stamp on the focused
pane, installed as the tmux hook `pane-focus-in[9909]` so config reloads are idempotent and a
user's own `pane-focus-in` hook is never clobbered. Tracking MUST be disableable via
`@cc-track-focus off`, which unsets the hook slot. No external history file SHALL be written.

#### Scenario: focus stamps the pane option
- Given: focus tracking is on (default)
- When: the user focuses a tracked pane
- Then: that pane's `@cc-visited` option is set to the current epoch

#### Scenario: hook slot is idempotent across reloads
- Given: cc-tmux is loaded
- When: tmux.conf is sourced a second time
- Then: exactly one `pane-focus-in[9909]` hook exists

#### Scenario: opt-out removes the hook
- Given: `@cc-track-focus off`
- When: cc-tmux loads
- Then: the `pane-focus-in[9909]` slot is unset and no visit stamps are written

#### Scenario: state dies with the pane
- Given: a pane with `@cc-visited` set
- When: the pane closes
- Then: no external file retains the visit history

### Requirement: cc-tmux doctor reports environment diagnostics
`cc-tmux doctor` SHALL print a PASS/FAIL checklist covering: tmux present and ≥ 3.2, fzf present,
python ≥ 3.10, `$TMUX` set, plugin symlink resolving, Claude Code plugin registration, hook wiring,
and tracked-pane sanity. It MUST exit 0 regardless of findings (fail-open convention — the
checklist is the diagnostic, not the exit code).

#### Scenario: healthy environment
- Given: a fully-installed machine inside tmux
- When: `cc-tmux doctor` runs
- Then: every row prints PASS and the exit code is 0

#### Scenario: degraded environment still exits 0
- Given: `$TMUX` is unset (run outside tmux)
- When: `cc-tmux doctor` runs
- Then: the `$TMUX` row prints FAIL with a hint, and the exit code is still 0

### Requirement: On-demand reconcile keeps state fresh without a daemon
The self-heal pass (process scan clearing stale `@cc-state`) SHALL run on the `inbox`,
`picker-data`, `cycle`, and `status` entry points, rate-limited via a `@cc-last-reconcile`
tmux global-option stamp (minimum interval ≥ 10 seconds) so high-frequency status renders do
not pay a process scan per tick. No background process SHALL be introduced.

#### Scenario: stale state clears on a status render
- Given: a pane whose Claude process was killed, leaving stale `@cc-state`, and the reconcile interval has elapsed
- When: the status bar invokes `cc-tmux status`
- Then: the stale pane's state is cleared and the counts reflect it

#### Scenario: rate limit suppresses back-to-back scans
- Given: a reconcile ran less than the minimum interval ago
- When: `cc-tmux status` runs again
- Then: no process scan is performed (counts render from current pane options)

#### Scenario: no daemon exists
- Given: cc-tmux is installed and active
- When: the process table is inspected
- Then: no persistent cc-tmux background process is running
