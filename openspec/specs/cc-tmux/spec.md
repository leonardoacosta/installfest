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
The plugin MUST cycle panes in priority order `waiting` > `idle` > `active`, newest-first within
each group, in a `priority` or `flat` mode. Jump-back SHALL return to the previous pane across
sessions and windows.

#### Scenario: priority cycle targets waiting first
- Given: panes in `waiting`, `idle`, and `active`, and `@cc-cycle-mode` is `priority`
- When: the user triggers cycle
- Then: cycling stays within the `waiting` group

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
entries as a view filter only.

#### Scenario: inbox opens in an fzf popup when available
- Given: tmux ≥ 3.2 and fzf installed, with ≥1 tracked pane
- When: the user opens the inbox
- Then: an fzf popup lists the panes as aligned columns

#### Scenario: inbox falls back to a menu
- Given: fzf is not installed
- When: the user opens the inbox
- Then: a `display-menu` lists the panes instead

#### Scenario: dismiss does not change state counts
- Given: a waiting pane shown in the inbox
- When: the user presses ctrl-x to dismiss
- Then: the pane is hidden from the inbox but the status-bar waiting count is unchanged

#### Scenario: self-heal on open
- Given: a pane whose Claude was `kill -9`'d, leaving stale `@cc-state`
- When: the inbox opens and the process scan confirms the process is gone
- Then: that pane's stale state is cleared

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
window to `<state-icon> <dir basename>`.

#### Scenario: status shows counts
- Given: two waiting and one idle pane
- When: the status bar renders `#{E:@cc-status}`
- Then: it shows the waiting and idle counts with their icons

#### Scenario: window rename tracks highest-priority state
- Given: `@cc-window-rename` is on and a window has an idle and a waiting Claude pane
- When: the window is renamed
- Then: the icon reflects `waiting` (highest priority) and the label is the directory basename

### Requirement: Multi-account Claude usage segment replaces tmux-nexus-creds
`cc-tmux usage` SHALL render the multi-account Claude usage segment (account + 5H/7D utilization
with color thresholds) by querying nexus-agent. This MUST replace the standalone
`tmux-nexus-creds` script, which SHALL be removed in the same change.

#### Scenario: usage segment renders from nexus-agent
- Given: nexus-agent is serving credentials at `http://localhost:7402/credentials`
- When: the status bar renders the cc-tmux usage segment
- Then: it shows `<account> 5H:xx% 7D:xx%` with cyan/amber/red thresholds

#### Scenario: usage segment is silent on failure
- Given: nexus-agent is unreachable
- When: the usage segment renders
- Then: it outputs nothing (no error in the status bar)

#### Scenario: the standalone script is removed
- Given: this change is applied
- When: the repo is inspected
- Then: `home/dot_local/bin/executable_tmux-nexus-creds` no longer exists and `status-right` calls the cc-tmux segment

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

