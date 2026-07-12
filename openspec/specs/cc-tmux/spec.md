# cc-tmux Specification

## Purpose
TBD - created by archiving change cc-tmux-plugin. Update Purpose after archive.
## Requirements
### Requirement: Claude pane state is tracked in tmux pane options
The `cc-tmux` plugin SHALL track each Claude Code pane's state (`waiting`/`idle`/`active`) using
tmux pane options as the single source of truth, driven by Claude Code hooks. State MUST
auto-delete when the pane closes.

#### Scenario: session start registers idle
- Given: the cc-tmux Claude Code plugin is installed
- When: a Claude Code session starts in a tmux pane
- Then: the pane's `@cc-state` option is set to `idle`

#### Scenario: prompt submit registers active
- Given: a tracked Claude pane
- When: the user submits a prompt
- Then: `@cc-state` becomes `active`

#### Scenario: permission prompt registers waiting
- Given: a tracked Claude pane in `active`
- When: Claude fires a `permission_prompt` notification
- Then: `@cc-state` becomes `waiting` and `@cc-wait-reason` becomes `permission`

#### Scenario: stop registers idle
- Given: a tracked Claude pane in `active`
- When: the turn ends (Stop hook)
- Then: `@cc-state` becomes `idle`

#### Scenario: state dies with the pane
- Given: a tracked Claude pane with `@cc-state` set
- When: the pane closes
- Then: no external state file retains the pane's state

### Requirement: Hooks self-register via the Claude Code plugin manifest
The plugin SHALL ship a `.claude-plugin/plugin.json` + `hooks/hooks.json` so `claude plugin install`
registers the state-tracking hooks without editing the global `settings.json`. Every hook command
MUST carry a 10-second timeout.

#### Scenario: install self-registers hooks
- Given: the cc-tmux plugin directory
- When: `claude plugin install` runs against it
- Then: the state-tracking hooks are active with no manual edit to `~/.claude/settings.json`

#### Scenario: a hung tmux does not block Claude
- Given: a registered hook whose tmux call hangs
- When: the hook fires
- Then: it is killed at 10 seconds rather than blocking Claude for the 60-second default

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

### Requirement: Opt-in window rename supports a project-code + session-title format
When `@cc-window-rename` is on and `@cc-window-rename-format` is `title`, the plugin SHALL rename
the pane's window to `<project-code>·<session-title>`, hard-truncated to 20 characters combined.
The project code SHALL resolve from the dotfiles project registry (`home/projects.toml`) by the
pane's current directory; the session title SHALL be captured from the `SessionStart` hook
payload's `session_title` field (the custom title if set via `/rename` or `-n`, else Claude's own
default) and persisted in `@cc-title`. Either half MAY be absent; the plugin MUST fall back to
whichever half resolved rather than leaving the window unnamed. The renamed text does NOT include
a state icon — see "Animated tab icon" below for how the icon is rendered instead. The plugin
SHALL capture the actual success/failure of the underlying `tmux rename-window` call (not merely
that it was issued) and record the outcome in the register-trace log (see "cc-tmux register logs
a hook-invocation trace for window-rename diagnostics" below) so a failed rename is distinguishable
from a successful one.

#### Scenario: registered project gets a code-prefixed title
- Given: `@cc-window-rename-format` is `title`, the pane's cwd is inside a project registered in
  `home/projects.toml` with code `if`, and `@cc-title` holds `"Fix ssh mesh"`
- When: the window is renamed
- Then: the window name is `if·Fix ssh mesh` (15 characters, rendered in full — within the
  20-character combined budget, no truncation)

#### Scenario: over-budget combination still truncates
- Given: `@cc-window-rename-format` is `title`, the project code is `if`, and `@cc-title` holds
  `"Fix ssh mesh auth flow"` (25 characters combined including the divider)
- When: the window is renamed
- Then: the window name is `if·Fix ssh mesh auth` (hard-truncated to 20 characters combined)

#### Scenario: unregistered project falls back to title alone
- Given: the pane's cwd is not covered by any registry entry, and `@cc-title` holds a title
- When: the window is renamed
- Then: the window name is the title alone, truncated to 20 characters

#### Scenario: no session title yet falls back to the resolved project name
- Given: `@cc-title` is unset (no `SessionStart` hook has fired yet) and the registry has no code
  for the pane's cwd
- When: the window is renamed
- Then: the window name falls back to `@cc-project` (git toplevel basename or dir name)

#### Scenario: a failed rename-window call is surfaced, not silently swallowed
- Given: `@cc-window-rename` is on and the underlying `tmux rename-window` call fails (stale pane
  id, a race with the window closing, or any other non-zero exit)
- When: `cc-tmux register` processes that hook fire
- Then: `_maybe_rename_window` returns `False` (not `True`, even though a rename was attempted),
  the register-trace log's `rename_succeeded` field is `false` for that entry, and the window's
  tab name is left unchanged from before the attempt

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

### Requirement: OS notification and terminal focus fire on real transitions
`@cc-notify` and `@cc-focus-app` (state lists, default empty) MUST fire an OS notification /
terminal focus only on a genuine state transition, via per-OS Strategy modules. The plugin SHALL
smart-suppress notify/focus when the terminal is already focused.

#### Scenario: notify only on a real transition
- Given: a pane already `idle` from a Stop hook
- When: an `idle_prompt` notification re-asserts `idle`
- Then: focus is NOT re-yanked to the pane (no state change occurred)

#### Scenario: suppress when already focused
- Given: `@cc-focus-app` includes `waiting` and the terminal is frontmost with the correct tab
- When: a pane enters `waiting`
- Then: the notification/focus is suppressed

#### Scenario: platform strategy selected
- Given: the plugin runs on Linux
- When: a notification fires
- Then: it is delivered via `notify-send`

### Requirement: Status-bar integration and window auto-rename
`cc-tmux status` SHALL emit pane counts for the status bar; `cc-tmux status-inbox` SHALL emit a
clickable pending-pane badge list. When `@cc-window-rename` is on, the plugin MUST rename the
window to the dir basename (default `state` format) — the state icon is rendered separately, see
"Animated tab icon" below.

#### Scenario: status shows counts
- Given: two waiting and one idle pane
- When: the status bar renders `#{E:@cc-status}`
- Then: it shows the waiting and idle counts with their icons

#### Scenario: window rename label is the directory basename
- Given: `@cc-window-rename` is on, `@cc-window-rename-format` is `state`, and a window has a
  tracked Claude pane
- When: the window is renamed
- Then: the window name is the directory basename alone (no icon prefix in the renamed text)

### Requirement: Animated tab icon reflects state via a wall-clock-driven refresh
The tab icon SHALL be rendered from a top-level status-format job (`#(cc-tmux tabs-row)`) that
composes the ENTIRE window-tabs row itself — icon, index, and name per window, with
active-window highlighting — rather than from the tmux-native per-window
`window-status-format`/`window-status-current-format` options. This relocation is required
because `#()` shell jobs nested inside tmux's default per-window `#{T:window-status-format}`
expansion do not execute on this fleet's tmux version (confirmed: a literal job embedded in
`window-status-format` and read back via `#{T:...}` never runs, across repeated timed retries),
while top-level status-format jobs are proven to execute (row 2 and row 3 already render
correctly via exactly this mechanism). No background process or timer SHALL be introduced by
this plugin to achieve the animation — the row is re-evaluated on tmux's existing
`status-interval` cadence, identical to how row 2/row 3 already refresh, just via a job placed
where jobs actually run. Each tracked state SHALL use the same distinct motion language as
before: `waiting` cycles a rising/falling shade pulse (`░▒▓█▓▒░`); `active` cycles a rotating
block edge (`▁▏▔▕`); `idle` renders a single static glyph, never animated. A window with no
tracked Claude pane MUST render no icon at all (not even the idle glyph).

#### Scenario: waiting state pulses through the shade sequence
- Given: a window's highest-priority tracked state is `waiting`
- When: the live tabs row is captured at two different wall-clock seconds one second apart
- Then: it shows two different frames from `░▒▓█▓▒░` for that window, advancing by one position

#### Scenario: active state rotates through the block sequence
- Given: a window's highest-priority tracked state is `active`
- When: the live tabs row is captured at two different wall-clock seconds one second apart
- Then: it shows two different frames from `▁▏▔▕` for that window, advancing by one position

#### Scenario: idle state never animates
- Given: a window's highest-priority tracked state is `idle`
- When: the live tabs row is captured at any two different wall-clock times
- Then: it shows the same static glyph for that window both times

#### Scenario: untracked window renders no icon
- Given: a window with no tracked Claude pane (a plain shell)
- When: the live tabs row renders
- Then: that window's entry shows no icon prefix

#### Scenario: the icon actually appears in the live render
- Given: the `tabs-row` job is wired into a top-level status-format slot
- When: the live rendered tab row is byte-captured (e.g. via `tmux display-message -F`)
- Then: the icon glyph is present in the captured output — not silently dropped the way the
  prior per-window `window-status-format` mechanism dropped it

### Requirement: Multi-account Claude usage segment replaces tmux-nexus-creds
`cc-tmux usage` SHALL render the multi-account Claude usage segment (account + 5H/7D utilization
with color thresholds) by querying nexus-agent at `http://localhost:7400/credentials`. The
subcommand SHALL remain invocable on demand but SHALL NOT be wired into any tmux status bar —
Claude usage statistics render exclusively in the in-pane Claude statusline. The standalone
`tmux-nexus-creds` script remains removed.

#### Scenario: usage segment renders from nexus-agent on demand
- Given: nexus-agent is serving credentials at `http://localhost:7400/credentials`
- When: `cc-tmux usage` is invoked manually
- Then: it shows `<account> 5H:xx% 7D:xx%` with cyan/amber/red thresholds

#### Scenario: usage segment is silent on failure
- Given: nexus-agent is unreachable
- When: the usage segment renders
- Then: it outputs nothing (no error output)

#### Scenario: no theme wires the segment into a status bar
- Given: this change is applied
- When: the theme confs under `home/dot_config/tmux/` are inspected
- Then: no `status-right` (or any status-format line) invokes `cc-tmux usage`

### Requirement: Conductor dispatches tasks to panes (opt-in)
The plugin MUST be disabled by default (`@cc-conductor-enabled` off). When enabled, a persistent
detached Claude session SHALL dispatch tasks to other panes via four modes (switch / send-prompt /
spawn-task / spawn-in-worktree) and MUST see a live pane snapshot on each prompt.

#### Scenario: conductor is inert when disabled
- Given: `@cc-conductor-enabled` is off (default)
- When: the plugin loads
- Then: no conductor keybinding is registered and `conductor --popup` refuses

#### Scenario: dispatch reaches a target pane
- Given: the conductor is enabled and a tracked idle pane exists
- When: the conductor dispatches a `send-prompt` to that pane
- Then: the prompt is injected into that pane

#### Scenario: send-prompt refuses an active pane
- Given: a target pane in `active`
- When: a `send-prompt` dispatch targets it without force
- Then: the dispatch is refused

### Requirement: The plugin ships Claude Code skills
The plugin SHALL bundle `cc-status`, `cc-config`, and `cc-dispatch` skills, usable in any Claude
session once installed.

#### Scenario: cc-status summarizes sessions
- Given: the plugin is installed with tracked panes
- When: the `cc-status` skill runs
- Then: it summarizes each tracked Claude session and its state

#### Scenario: cc-dispatch is the single home of the dispatch CLI shape
- Given: both the Conductor and an ad-hoc session can dispatch
- When: the dispatch CLI flag shape changes
- Then: it changes in exactly one place (the `cc-dispatch` skill)

### Requirement: tmux entrypoint binds keys without colliding with which-key
`cc-tmux.tmux` MUST load via `run-shell` under an `if-shell` presence guard and SHALL bind
cycle/picker/inbox/back/conductor keys without colliding with the installed `tmux-which-key`
`prefix + Space` binding.

#### Scenario: no double-bind of prefix + Space
- Given: `tmux-which-key` binds `prefix + Space`
- When: cc-tmux loads
- Then: `prefix + Space` is not double-bound (cc-tmux cycle uses a different default key)

#### Scenario: load is guarded on presence
- Given: a machine where the plugin has not been cloned yet
- When: tmux.conf is sourced
- Then: the rest of the config still loads (the cc-tmux load is skipped by the if-shell guard)

### Requirement: chezmoi install wiring and graceful degradation
A `run_onchange` install script MUST deploy the plugin (tmux clone + `claude plugin install`), and
every subcommand MUST fail open (exit 0) on a missing dependency or environment.

#### Scenario: install is idempotent
- Given: the plugin is already installed
- When: `chezmoi apply` runs the install script again
- Then: it updates in place without error

#### Scenario: subcommand outside tmux exits cleanly
- Given: no `$TMUX` in the environment
- When: any `cc-tmux` subcommand runs
- Then: it exits 0 without error

### Requirement: Usage polling is consolidated to a single Anthropic caller
`nexus-agent`'s credential-usage-poller SHALL be the only process that calls Anthropic's
`/api/oauth/usage` endpoint. It SHALL write the active credential's polled 5-hour/7-day usage to a
shared, on-disk cache file on every successful poll tick. `nexus-statusline` SHALL read that cache
file instead of independently calling Anthropic; its own direct Anthropic usage-polling code path
SHALL be removed.

#### Scenario: statusline reads the shared cache, not Anthropic
- Given: nexus-agent's poller has successfully written the shared usage-cache file
- When: nexus-statusline resolves usage data for a render
- Then: it reads the cache file and does not issue an HTTP request to Anthropic

#### Scenario: stale or missing cache degrades to no gauges
- Given: the shared usage-cache file is missing or unreadable
- When: nexus-statusline resolves usage data for a render
- Then: the usage gauges are omitted from that render (same degradation as an Anthropic call
  failing today) — no exception, no stale-forever data

### Requirement: A minimal per-render session-context field bridges the one field only nexus-statusline can see
`nexus-statusline` SHALL write the current session's context-window used-percentage AND the model
family letter (the same letter `modelEffortToken` computes for its own row) to a per-pane cache
file on every render, gated on the `TMUX_PANE` environment variable being set. This is scoped to
exactly these two fields — no other per-render field (cost, lines, clock, output style, speed,
worktree, spec) SHALL be written by this mechanism.

#### Scenario: model letter reaches cc-tmux
- Given: nexus-statusline has written a per-pane session-context cache file for the active pane
  with `model` set to `F`
- When: cc-tmux renders the session-bar row for that pane's window
- Then: the model letter `F` renders in the row's left side

#### Scenario: context percentage remains in the cache file
- Given: nexus-statusline renders with a known context-window used-percentage
- When: the per-pane session-context cache file is written
- Then: it carries both `context_used_pct` and the model letter in the same JSON object

#### Scenario: no TMUX_PANE means no write
- Given: a Claude Code process running outside tmux (no `TMUX_PANE` set)
- When: nexus-statusline renders
- Then: no session-context cache file is written for that process

### Requirement: A dedicated tmux status row shows session identity and usage
The plugin SHALL render a dedicated tmux status row (`status-format[1]`) showing, left-justified,
a single-letter model tag (Fable=F, Opus=O, Haiku=H, Sonnet=S), the project code, and the git
branch. The model letter SHALL be sourced from the per-pane `session-context.<pane>.json` cache
written by nexus-statusline — not from the SessionStart hook payload, whose `model` field is
unreliable and which never re-fires on a mid-session `/model` switch. Right-justified on the same
row, the plugin SHALL render Claude usage statistics for the active nexus-agent credential: an
account label, and SES:/5H:/7D: utilization gauges (session-context %, 5-hour, and 7-day), each
coloured by utilization threshold. This row SHALL remain separate from the window-tabs row, whose
own `status-right` stays usage-free.

The window's representative pane — the pane whose project/branch/model/usage this row renders —
SHALL be the window's tmux-ACTIVE (focused) pane when that pane carries a valid `@cc-state`
(i.e. it is itself a tracked Claude pane). Only when the active pane is untracked (e.g. a plain
shell pane focused in a split alongside a background Claude pane) SHALL the plugin fall back to
the existing priority-based pick (highest-priority `@cc-state` among the window's tracked panes,
ties broken by pane order).

#### Scenario: row 2 renders the session identity and usage
- Given: a tracked Claude pane in project `if` on branch `main`, model Fable, and the active
  nexus-agent credential has usage data
- When: the session-bar row renders
- Then: the left side shows `F if > main` (model letter, project, branch) and the right side
  shows the account label plus SES:/5H:/7D: gauges

#### Scenario: the active pane is used, not the priority-first pane
- Given: a window with two tracked Claude panes, pane A (`idle`, lower pane index) and pane B
  (`idle`, higher pane index, currently focused)
- When: the session-bar row renders
- Then: the left/right side reflects pane B's project/branch/model/usage, not pane A's

#### Scenario: an untracked focused pane falls back to the priority pick
- Given: a window with a focused plain-shell pane (no `@cc-state`) and a background tracked
  Claude pane in `waiting`
- When: the session-bar row renders
- Then: the row reflects the `waiting` Claude pane (fallback to the existing priority-based
  pick), not an empty row

#### Scenario: model letter tracks a mid-session model switch
- Given: a tracked pane whose `session-context.<pane>.json` model letter changes from `F` to `O`
  after a `/model` switch
- When: the session-bar row next renders
- Then: the model letter shown is `O` (no SessionStart event required)

#### Scenario: missing session-context cache drops the letter only
- Given: a tracked pane with no `session-context.<pane>.json` file
- When: the session-bar row renders
- Then: the row renders project and branch with no model letter (fail-open, no error)

#### Scenario: unpolled usage windows render as '--'
- Given: an active nexus-agent credential that has not yet been polled for 5-hour/7-day usage
- When: the session-bar row renders
- Then: the SES:/5H:/7D: gauges render `--` in a dimmed colour rather than a stale/wrong percent

#### Scenario: untracked window shows nothing on this row
- Given: a tmux window with no tracked Claude pane
- When: the session-bar row renders for that window
- Then: the row is empty (no session identity, no usage) for that window

### Requirement: A dedicated tmux status row surfaces open/ready beads and proposals
The plugin SHALL render a second dedicated tmux status row (`status-format[2]`) showing the
current project's cached roadmap-pulse counts line ONLY, read directly from
`~/.claude/scripts/state/roadmap-pulse.<code>.line`. When the cache contains a `next:` line, it
SHALL NOT be rendered on this row — only the open/unarchived counts line renders. No new data
production mechanism SHALL be introduced for this row — it reads the cache nexus-statusline's
own `getRoadmapPulse()` already maintains.

#### Scenario: row 3 renders only the counts line
- Given: a cached roadmap-pulse file containing `next: /apply foo…` and `0 open, 2 unarchived`
  on separate lines
- When: the beads-bar row renders
- Then: it shows only `0 open, 2 unarchived` — the `next:` line does not appear anywhere on the
  row

#### Scenario: counts-only cache renders as-is
- Given: a cached roadmap-pulse file containing only a counts line (no `next:` line)
- When: the beads-bar row renders
- Then: it shows that line alone, unchanged

#### Scenario: no cache yet renders nothing
- Given: no roadmap-pulse cache file exists yet for the current project
- When: the beads-bar row renders
- Then: the row is empty — no error, no placeholder text

### Requirement: The tmux status bar is three lines
`home/dot_config/tmux/tmux.conf.tmpl` SHALL set `status 3`, and each shipped theme
(`vercel-theme.conf`, `one-hunter-vercel-theme.conf`, `tokyo-night-abyss-theme.conf`,
`nord-theme.conf`) SHALL define both `status-format[1]` and `status-format[2]`. A theme MUST NOT
enable the three-line bar without defining both extra rows.

#### Scenario: all four themes define both extra rows
- Given: `status 3` is enabled in `tmux.conf.tmpl`
- When: any of the four shipped themes is active
- Then: both `status-format[1]` and `status-format[2]` render the session/usage and
  beads/proposals rows respectively — none falls back to tmux's default pane-list row

### Requirement: cc-tmux register logs a hook-invocation trace for window-rename diagnostics
Every `cc-tmux register` invocation SHALL append one line to a debug trace log
(`~/.claude/scripts/state/cc-tmux-register-trace.log`) recording the invocation's timestamp,
`hook_event_name`, resolved pane id, whether a window-rename was attempted, and whether it
fired. The log SHALL be bounded (rotated or capped) so it never grows unbounded. This is
diagnostic-only — it MUST NOT alter `_maybe_rename_window`'s existing rename behavior.

#### Scenario: a register call is traced
- Given: `cc-tmux register` is invoked for any hook event
- When: the invocation completes
- Then: one new line appears in the trace log recording that event's hook name, pane, and
  rename attempt/fire outcome

#### Scenario: the trace log is bounded
- Given: the trace log has been written to over an extended period
- When: its size is inspected
- Then: it does not grow without bound — old entries are rotated or capped

