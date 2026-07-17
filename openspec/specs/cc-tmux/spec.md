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
the pane's window to `<project-code>·<session-title>`, hard-truncated to 20 characters combined,
WHENEVER a session title is present. The project code SHALL resolve from the dotfiles project
registry (`home/projects.toml`) by the pane's current directory; the session title SHALL be
captured from the `SessionStart` hook payload's `session_title` field (the custom title if set via
`/rename` or `-n`, else Claude's own default) and persisted in `@cc-title`. When no session title
is present (`@cc-title` unset or empty), the plugin SHALL fall back to the raw current-directory
basename (`os.path.basename(pane_current_path)`) alone — the project-code prefix is used ONLY
when a title is present, never as a title-absent fallback on its own. The renamed text does NOT
include a state icon — see "Animated tab icon" below for how the icon is rendered instead. The
`rename-window` command's actual success or failure SHALL be observed and reported (not assumed
true once issued) — a failed rename MUST NOT be recorded as having renamed the window.

#### Scenario: registered project gets a code-prefixed title
- Given: `@cc-window-rename-format` is `title`, the pane's cwd is inside a project registered in
  `home/projects.toml` with code `if`, and `@cc-title` holds `"Fix ssh mesh auth flow"`
- When: the window is renamed
- Then: the window name is `if·Fix ssh mesh auth` (20 characters, code + title truncated
  together)

#### Scenario: unregistered project falls back to title alone
- Given: the pane's cwd is not covered by any registry entry, and `@cc-title` holds a title
- When: the window is renamed
- Then: the window name is the title alone, truncated to 20 characters

#### Scenario: no session title falls back to the folder name, even inside a registered project
- Given: `@cc-title` is unset or empty (no `SessionStart` hook has fired yet, or Claude never set
  a title), and the pane's cwd IS inside a project registered in `home/projects.toml` with code
  `if`
- When: the window is renamed
- Then: the window name is the raw current-directory basename alone (e.g. `new-service`), NOT
  `if` — the project-code prefix is not applied when there is no title to prefix

#### Scenario: no session title and no registered project both fall back to the same folder name
- Given: `@cc-title` is unset or empty, and the registry has no code for the pane's cwd
- When: the window is renamed
- Then: the window name is the raw current-directory basename alone — identical fallback whether
  or not the project happens to be registered

#### Scenario: a failed rename is reported as not fired
- Given: `tmux rename-window` fails (non-zero exit, e.g. a stale pane id or a race with the
  window closing)
- When: `_maybe_rename_window` runs
- Then: it returns `False` and the diagnostic trace records the attempt as failed, not succeeded

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
expansion do not execute on this fleet's tmux version, while top-level status-format jobs are
proven to execute. No background process or timer SHALL be introduced by this plugin to achieve
the animation — the row is re-evaluated on tmux's existing `status-interval` cadence.

All tracked states SHALL speak ONE visual language: the 17-state session-usage ramp — `░` at
state 0, braille fill `⡀⣀⣄⣤⣦⣶⣷` to `⣿` at 50%, braille drain `⢿⠿⠻⠛⠙⠉⠈` toward 93.75%, `▓` at
the top state — indexed by `round(clamp(ratio, 0, 1) * 16)` where `ratio` is the session's
absolute context-token burn over a fixed 1,000,000-token scale. The solid block `█` SHALL NOT
render as any tab state (changed from the prior version, where `█` was the idle no-data
fallback).

Per state:
- `waiting` flashes between two braille glyphs (`◉` colored YELLOW, `◎` default/unstyled) —
  UNCHANGED from the prior version.
- `active` SHALL flash between the ramp glyph at the session's current meter state `i` and the
  ramp glyph at state `min(i + 1, 16)`, alternating by wall-clock tick parity (changed from the
  prior fixed two-glyph braille pair); at meter state 16 the pair SHALL be states 15 and 16 so
  two distinct frames always render. When the session's raw token count is unavailable
  (`None`), the active icon SHALL flash between the state-0 and state-1 ramp glyphs (`░` and
  `⡀`) with no meter colour applied.
- `idle` renders a single-cell session-usage meter: the ramp glyph for the session's meter
  state, coloured by the existing context-severity ramp (`resolve_context_color`) applied to
  the same raw token count — including its pulsing tiers — reused verbatim with no
  meter-specific colour logic. State 0 (nearly fresh) MUST flash by alternating `░` with a
  same-width blank on the same wall-clock parity the colour pulse uses; every other meter state
  renders a data-driven-static glyph. When the session's raw token count is unavailable
  (`None`), the idle icon MUST render the state-0 glyph `░` STATICALLY in DIM styling with no
  meter colour and no flash — a data gap MUST NOT render as the fresh-session flash (rule
  preserved from the prior version; only the fallback glyph changed from `█` to DIM `░`).

The active-state ramp lookup SHALL resolve the session's raw token count for active windows
through the same resolution path idle windows already use, relying on the existing short-TTL
nx-agent response cache — no new cache layer and no per-window network amplification beyond
that existing cache's contract. A window with no tracked Claude pane MUST render no icon at all.

#### Scenario: waiting state pulses between the permission glyphs
- Given: a window's highest-priority tracked state is `waiting`
- When: the live tabs row is captured at two different wall-clock seconds one second apart
- Then: it shows `◉` (colored YELLOW) at one capture and `◎` (default color) at the other,
  alternating by wall-clock tick parity

#### Scenario: active state pulses between ramp-adjacent glyphs
- Given: a window's highest-priority tracked state is `active` and its session's raw token
  count resolves to 70,000 (meter state 1 on the 1M scale)
- When: the live tabs row is captured at two different wall-clock seconds one second apart
- Then: it shows the state-1 glyph (`⡀`) at one capture and the state-2 glyph (`⣀`) at the
  other, alternating by wall-clock tick parity

#### Scenario: active state at the top of the ramp still shows two frames
- Given: an active window whose session's raw token count resolves to meter state 16
- When: the tabs row is captured at two wall-clock seconds of opposite parity
- Then: the icon alternates between the state-15 (`⠈`) and state-16 (`▓`) glyphs

#### Scenario: active state with unavailable usage data pulses the ramp base
- Given: an active window whose session raw-token resolution returns `None`
- When: the tabs row is captured at two wall-clock seconds of opposite parity
- Then: the icon alternates between `░` and `⡀` with no meter colour applied — never a fixed
  braille pair and never the solid block `█`

#### Scenario: idle meter reflects session usage on the 1M scale
- Given: a window's highest-priority tracked state is `idle` with no sub-agent overlay, and its
  session's raw token count resolves to 500,000
- When: the tabs row renders
- Then: that window's icon is `⣿` (state 8 of the ramp), coloured by `resolve_context_color`
  for 500,000 tokens, and the index/name text keeps its unchanged active/inactive colouring

#### Scenario: nearly fresh idle session flashes the light shade block
- Given: an idle window whose session's raw token count resolves below ~31,250 tokens (meter
  state 0)
- When: the tabs row is captured at two wall-clock seconds of opposite parity
- Then: the icon alternates between `░` and a same-width blank — the label column does not shift

#### Scenario: unavailable usage data renders the dimmed ramp base, not a solid block
- Given: an idle window whose session raw-token resolution returns `None` (e.g. nx-agent
  unreachable or no session id)
- When: the live tabs row is captured at any two different wall-clock times
- Then: it shows the state-0 glyph `░` in DIM styling both times — static (no flash), no meter
  colour, and the solid block `█` appears nowhere in the row

#### Scenario: untracked window renders no icon
- Given: a window with no tracked Claude pane (a plain shell)
- When: the live tabs row renders
- Then: that window's entry shows no icon prefix

#### Scenario: the icon actually appears in the live render
- Given: the `tabs-row` job is wired into a top-level status-format slot
- When: the live rendered tab row is byte-captured (e.g. via `tmux display-message -F`)
- Then: the icon glyph is present in the captured output — not silently dropped

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
`cc-tmux` SHALL query nx-agent's session-context HTTP endpoint (`GET
http://localhost:7400/sessions/:id/context`) to obtain the current session's context-window
used-percentage, keyed by a `session_id` captured from the Claude Code hook payload on
`SessionStart` and stored as a new `@cc-session-id` pane option (same capture block as the
existing `session_title` read in `cmd_register`). This replaces the retired per-pane
`session-context.<pane_id>.json` file `nexus-statusline` used to write — that file is no longer
written by nx as of the 2026-07-13 nx API migration. The query SHALL be cached on disk with a
short TTL (~5s) so the 1Hz session-bar render tick does not re-fetch on every tick; a cache miss
or expiry SHALL fall through to a live fetch with a 1s timeout, fail-open to absent on any error
(unreachable agent, non-2xx, malformed body, missing/stale entry).

This requirement is scoped to exactly this one field (`context_used_pct`). The model family
letter is NOT covered — see the sibling "A dedicated tmux status row shows session identity and
usage" requirement for its (unchanged, now-degrading) sourcing.

#### Scenario: session_id captured on SessionStart
- Given: a `SessionStart` hook payload carrying `session_id: "abc123"`
- When: `cc-tmux register --state idle` runs (the SessionStart hook entrypoint)
- Then: the pane's `@cc-session-id` option is set to `abc123`

#### Scenario: context percentage sourced from the nx endpoint
- Given: a tracked pane with `@cc-session-id` set, and nx-agent's `/sessions/abc123/context`
  returns `{"usedPercentage": 42, ...}`
- When: the session-bar row renders for that pane's window
- Then: the SES gauge reflects 42%, sourced from the endpoint response (not any on-disk file)

#### Scenario: unreachable nx-agent degrades to absent, not an error
- Given: a tracked pane with `@cc-session-id` set, and nx-agent is not running (connection
  refused)
- When: the session-bar row renders for that pane's window
- Then: the SES gauge renders `--` (fail open), and no exception propagates

#### Scenario: no session_id captured yet
- Given: a tracked pane whose `@cc-session-id` option has never been set (e.g. a pane registered
  before this feature shipped, or a hook fired before the first `SessionStart`)
- When: the session-bar row renders for that pane's window
- Then: the SES gauge renders `--` (fail open), identical to the unreachable-agent case

### Requirement: A dedicated tmux status row shows session identity and usage
The plugin SHALL render a dedicated tmux status row (`status-format[1]`) showing, left-justified,
a single-letter model tag (Fable=F, Opus=O, Haiku=H, Sonnet=S) colored per model (see "Model
letter color" below), the project code, the git branch, and (when any of the six working-tree
metrics below is nonzero) working-tree indicators. Right-justified on the same row, the plugin
SHALL render Claude usage statistics for the active nexus-agent credential: a token-count label
for SES with NO trailing colon (e.g. `252.5k`, changed from the prior `cc-tmux-context-bar`
format's `252.5k:`) plus exact `5H:xx%`/`7D:xx%` text (the `5H:`/`7D:` colons are UNCHANGED),
followed by a single space and LAST a combined Unicode Braille usage glyph (20 cells wide,
doubled from the prior 10-cell width) encoding all three values in one glyph run — top two
dot-rows = SES, third dot-row = 5H, fourth (bottom) dot-row = 7D, each row an independent
proportional left-to-right fill. The glyph renders in a neutral/unstyled color; the exact text
values remain the sole color-coded signal (unchanged `usage.color_for`/`_context_color_pair`
thresholds). The active account's identity (email + org id) is NOT rendered on this row — see
the beads/proposals row requirement below, which now carries it. This row SHALL remain separate
from the window-tabs row.

**Model letter color** (NEW): the model-letter segment SHALL be colored by the resolved model
name — Opus=YELLOW, Sonnet=GREEN, Haiku=LIGHT_GREEN, Fable=RED — falling back to the prior
static CYAN for an unrecognized or empty model value (fail-open, matching this row's existing
"empty field drops out" convention). Model letter SOURCING (unchanged sourcing, disclosed
degradation) and **branch** (unchanged dual source: nx `project_git_status` primary, local
`@cc-branch` fallback) are otherwise UNCHANGED by this requirement version — see the prior
MODIFIED delta (`cc-tmux-adopt-nx-context-and-git-status`) for their full sourcing contract,
still in force; only the letter's COLOR is new.

**Working-tree indicators** (per-field dual source, six metrics): the plugin SHALL render, in
this fixed left-to-right order after the branch name, each of the following ONLY when its count
is nonzero (a zero-count metric renders nothing — no glyph, no leading space beyond the single
separator to the next nonzero metric):

| Metric | Glyph | Color |
| --- | --- | --- |
| Modified | `<N>M` | GREEN |
| Untracked | `<N>U` | YELLOW |
| Deleted | `<N>D` | RED |
| Renamed | `<N>R` | BLUE |
| Ahead of upstream | `⇡<N>` | (unstyled/DIM, matching branch segment styling) |
| Behind upstream | `⇣<N>` | (unstyled/DIM, matching branch segment styling) |

For EACH of the six metrics independently: the plugin SHALL prefer the value from nx-agent's
`GET /projects/:id/status` `git` object (`nx_agent.project_git_status`) when that specific key is
present in nx's response, and SHALL fall back to the corresponding field of the local
`@cc-git-status` pane option (a JSON-encoded object with `modified`/`untracked`/`deleted`/
`renamed`/`ahead`/`behind` int fields, written by `tmux.set_pane_git_identity` via a single
`git status --porcelain=v2 --branch` parse on `waiting`/`idle` transitions) when nx's response is
absent, unreachable, or does not carry that key. As of this requirement version, nx's `git` object
carries only `modified`/`untracked` — `deleted`/`renamed`/`ahead`/`behind` SHALL always fall back
to local until nx's payload is extended (tracked externally; this requirement's per-field
resolution rule requires no future code change when that happens).

**Combined usage glyph** (`render_usage_glyph`, 20 braille cells): for a metric with ratio `r`
(0..1) and a bit-order table of `k` bits per cell (SES: 4 bits/cell, rows 1-2; 5H: 2 bits/cell,
row 3; 7D: 2 bits/cell, row 4), the total dot budget is `k * 20` and `dots_lit =
round(r * budget)`, filled sequentially cell-by-cell left to right — the same segmented-fill
principle as the prior token-count bar, generalized to 3 independently-filling rows sharing one
20-cell run (doubled from the prior 10-cell run; the fill algorithm itself is unchanged, only the
cell count). A metric whose data is unavailable (see the unpolled scenario below) contributes
ZERO dots to its own row(s) only — other metrics' rows are unaffected (per-metric degrade, not an
all-or-nothing glyph blackout).

#### Scenario: row 2 renders the session identity and usage, no account identity
- Given: a tracked Claude pane in project `if` on branch `main`, model Fable, and the active
  nexus-agent credential has usage data
- When: the session-bar row renders
- Then: the left side shows `F if > main` (model letter in RED for Fable, project, branch) and
  the right side shows `252.5k 5H:xx% 7D:xx%` text (SES's token-count label with no trailing
  colon, plus 5H/7D percentages with their colons unchanged), a single space, then LAST the
  combined 20-cell braille glyph with each row's fill proportional to that metric's value — no
  account label or identity text appears anywhere on this row

#### Scenario: the model letter is colored per model
- Given: four separate tracked panes, one each running Opus, Sonnet, Haiku, and Fable
- When: each pane's session-bar row renders
- Then: the model letter renders YELLOW for Opus (`O`), GREEN for Sonnet (`S`), LIGHT_GREEN for
  Haiku (`H`), and RED for Fable (`F`) — an unrecognized or empty model value falls back to the
  prior static CYAN

#### Scenario: modified and untracked prefer nx, deleted/renamed/ahead/behind fall back to local
- Given: a tracked pane in project `if`; `GET /projects/if/status` returns a `git` object with
  `dirty: {modified: 3, untracked: 1}` (no `deleted`/`renamed`/`ahead`/`behind` keys present);
  the local `@cc-git-status` option holds `{modified: 5, untracked: 9, deleted: 2, renamed: 1,
  ahead: 4, behind: 1}`
- When: the session-bar row renders
- Then: the row shows `3M 1U 2D 1R ⇡4 ⇣1` — modified/untracked from nx (3/1, not the local 5/9),
  deleted/renamed/ahead/behind from local (2/1/4/1, nx had no such keys)

#### Scenario: nx unreachable falls all six metrics back to local
- Given: a tracked pane in project `if` with local `@cc-git-status` = `{modified: 1, untracked: 0,
  deleted: 0, renamed: 0, ahead: 0, behind: 0}`, and `GET /projects/if/status` fails (connection
  refused)
- When: the session-bar row renders
- Then: the row shows `1M` (only the nonzero metric renders; all six sourced from local)

#### Scenario: a fully nx-extended response prefers nx for every field
- Given: a tracked pane where `GET /projects/if/status`'s `git` object carries all six keys
  (`modified`, `untracked`, `deleted`, `renamed`, `ahead`, `behind`, hypothetically once nx's
  payload is extended)
- When: the session-bar row renders
- Then: every one of the six metrics is sourced from nx's response, none from the local
  `@cc-git-status` fallback — proving the per-field rule requires no code change to adopt an
  expanded nx payload

#### Scenario: an all-clean, up-to-date tree shows no working-tree indicators
- Given: a tracked pane with a clean working tree, no commits ahead or behind upstream (all six
  metrics resolve to 0 regardless of source)
- When: the session-bar row renders
- Then: no working-tree indicator segment renders at all — just model/project/branch on the left

#### Scenario: registry-code mismatch at nx falls back to local, same as unreachable
- Given: a tracked pane whose registry project code is not present in nx's own project registry
  and `GET /projects/<code>/status` returns 404
- When: the session-bar row renders
- Then: all six working-tree metrics fall back to the local `@cc-git-status` pane option,
  identical to the unreachable-agent case

#### Scenario: model letter degrades to blank once nx stops writing the legacy file
- Given: a tracked pane whose legacy `session-context.<pane>.json` file is absent or older than
  the existing freshness cutoff (nx no longer writes it)
- When: the session-bar row renders
- Then: the row renders project/branch/working-tree-indicators/usage as normal with no model
  letter (fail open, no error) — unchanged from the prior requirement version

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

#### Scenario: unpolled usage windows render as '--' and blank that metric's glyph row(s) only
- Given: an active nexus-agent credential that has not yet been polled for 5-hour/7-day usage,
  while SES has live data
- When: the session-bar row renders
- Then: the `5H:`/`7D:` text renders `--` in a dimmed colour rather than a stale/wrong percent,
  the combined glyph's row 3 (5H) and row 4 (7D) render zero dots, and the glyph's rows 1-2 (SES)
  still render SES's live fill unaffected

#### Scenario: untracked window shows nothing on this row
- Given: a tmux window with no tracked Claude pane
- When: the session-bar row renders for that window
- Then: the row is empty (no session identity, no usage) for that window

### Requirement: A dedicated tmux status row surfaces open/ready beads and proposals
The plugin SHALL render a second dedicated tmux status row (`status-format[2]`) showing the
current project's roadmap-pulse content, PLUS a third independent segment carrying the active
nexus-agent credential's identity (email + 8-character org id, e.g. `leo@priceless.dev·bc7da511`
— the same format used by the accounts popup's identity rows).

**Data source: nx-agent first, local file as fallback.** The row's counts/next content SHALL be
sourced by calling nx-agent's `GET /projects/:code/pulse` endpoint (via `nx_agent.py`'s
`roadmap_pulse`, the same cached-fetch client pattern as `session_context`/`project_git_status` —
on-disk TTL cache, negative caching, fail-open on any error). When that call returns a non-`None`
result, row3's content is built from it. When it returns `None` — nx-agent unreachable,
negative-cached, or malformed — the row falls back to reading
`~/.claude/scripts/state/roadmap-pulse.<code>.line` directly, exactly as before this requirement
version. Neither path introduces a new refresh mechanism owned by cc-tmux itself: freshness is
nx-agent's responsibility on the primary path, and the fallback path stays whatever staleness the
file already carries (nothing in cc-tmux triggers a refresh of that file).

**Left-side content cycles between two phases on a wall-clock timer, prefixed by a countdown
glyph.** Phase is `int(now / 8.0) % 2`, `now` the
caller-supplied wall-clock time at render (the same daemon-free, `status-interval`-driven cadence
`animated_icon` already uses for the tabs row — no timer process, no new tmux hook):

- **Phase 0** (counts): the openspec/beads portion renders in cc's abbreviated form `op:
  {open}o {in_progress}ip {ua}ua ({age}) | bd: {open}o {ready}r {blocked}b ({age})` (if-bqw.1, cc
  commit `b6b9a234` / cc-w83ov.4), where `ua` is the closure-debt count — specs that are done but
  not yet archived — and the `bd:` half counts only "standalone" beads — issues that are NOT a
  transitive descendant, via a `parent-child` dependency, of any issue whose title starts with
  `[SPEC]` or `[CAPABILITY]` — so the two halves are additive rather than double-counting
  OpenSpec-tracked work. The `bd:` half's `open` count is the total standalone beads currently
  open/in_progress/blocked, alongside the pre-existing `ready`/`blocked` counts. Each half's
  numeric values SHALL be coloured by semantic threshold (DIM for a healthy zero/low count on
  `open`/`in_progress`/`ready`, YELLOW when `ua > 0` or `standalone_blocked > 0`, RED above a
  documented high-count threshold).
- **Phase 1** (next): the row instead renders the source's `next:` line verbatim (already
  pre-truncated by the producer, whether nx-agent or the local file's cache) in place of the
  `op:`/`bd:` segments — the two never render simultaneously.
- A `radar:` line SHALL NOT be rendered in either phase (unchanged from the prior requirement
  version — defense against a stale or rolled-back cache carrying that token).
- When no `next:` line is available, phase 1 falls back to rendering phase 0's content instead
  (never a blank left side when counts ARE available).

**Account identity segment**: the plugin SHALL append the active credential's identity as a
third segment, independent of the openspec/beads/next cycle — present whenever an active
nexus-agent credential resolves, regardless of cycle phase or whether roadmap-pulse content
(from either source) exists at all. The segment SHALL be clickable, bound to `cc-tmux
accounts-popup`, via the same `#[range=user|accounts]` mouse-range marker mechanism, in both
phases.

#### Scenario: nx-agent resolves fresh counts — primary path
- Given: `nx_agent.roadmap_pulse(code)` returns a non-`None` JSON dict carrying `op:`/`bd:`
  counts and, optionally, a `next:` line
- When: the beads-bar row renders
- Then: row3's content is built entirely from the nx-agent response — the local `.line` file is
  never read for this render

#### Scenario: nx-agent unreachable falls back to the local file, unchanged
- Given: `nx_agent.roadmap_pulse(code)` returns `None` (timeout, non-2xx, negative-cached, or
  malformed body), and a cached `roadmap-pulse.<code>.line` file exists with counts and/or a
  `next:` line
- When: the beads-bar row renders
- Then: row3's content is built from the local file exactly as it was before this requirement
  version — same parsing, same age display, same fallback-to-phase-0 behavior when no `next:`
  line is present

#### Scenario: phase 0 renders counts with the countdown glyph, plus the account identity
- Given: a roadmap-pulse source (nx-agent or local file) whose counts are `1o 0ip 0ua` (openspec)
  and `1o 1r 0b` (standalone beads), an active nexus-agent credential `leo@priceless.dev` / org
  `bc7da511-...`, and a render `now` several ticks past the most recent phase-0 swap boundary
  (transition already settled)
- When: the beads-bar row renders
- Then: it shows `[countdown-glyph] op: 1o 0ip 0ua (<age>) | bd: 1o 1r 0b (<age>) |
  leo@priceless.dev·bc7da511` with all counts coloured DIM, and the account segment clickable via
  the same mouse-range marker

#### Scenario: phase 1 renders the next-action line instead of counts
- Given: the same roadmap-pulse source additionally carries a `next: [WORKSPACE-CMDCENTER]
  Wor...` line, and a render `now` several ticks past the most recent phase-1 swap boundary
  (transition already settled)
- When: the beads-bar row renders
- Then: the left side shows `[countdown-glyph] next: [WORKSPACE-CMDCENTER] Wor...` — the `op:`/
  `bd:` segments do NOT appear — and the account segment still renders on the right, unchanged

#### Scenario: no next: line available falls back to phase 0's content in phase 1
- Given: a roadmap-pulse source with counts but no `next:` line, and a render `now` that resolves
  to phase 1
- When: the beads-bar row renders
- Then: the left side shows the phase-0 `op:`/`bd:` content (with countdown glyph) instead of a
  blank phase-1 slot — the row never goes empty just because the cycle landed on an unavailable
  phase

#### Scenario: no roadmap-pulse data from either source, but an active account resolves
- Given: `nx_agent.roadmap_pulse(code)` returns `None` AND no local `.line` file exists yet for
  the current project, and an active nexus-agent credential resolves
- When: the beads-bar row renders
- Then: the row shows ONLY the account identity segment (`leo@priceless.dev·bc7da511`) — not an
  empty row, in either phase, since the account segment is independent of both data sources

#### Scenario: roadmap-pulse data present, no active account resolves
- Given: a roadmap-pulse source (nx-agent or local file) with real counts and a `next:` line, and
  nexus-agent is unreachable for credential resolution (no active credential resolves)
- When: the beads-bar row renders
- Then: the row shows only the cycling left-side content (counts or next, per phase) — no empty
  account segment, no error

#### Scenario: a stray radar: line never renders, in either phase
- Given: a roadmap-pulse source containing a `next: …` line, a `radar:stale` line (stale
  pre-fix content), and a counts line
- When: the beads-bar row renders, in both phase 0 and phase 1
- Then: the `radar:` line never appears on the row in either phase — only the phase-appropriate
  content (counts or next) and, if applicable, the account segment

#### Scenario: standalone beads exclude OpenSpec-tracked work
- Given: 5 open beads total, 3 of which are tasks under a `[SPEC] some-proposal` feature (itself
  under a `[CAPABILITY]` epic), and 2 of which have no epic ancestor at all
- When: the standalone-beads count is computed (by nx-agent on the primary path, or by cc's
  `roadmap-pulse` script on the fallback path)
- Then: only the 2 unparented beads count toward `bd: {open}o {ready}r {blocked}b` — the 3
  OpenSpec-tracked tasks do not, since they're already represented by the `op:` half

#### Scenario: nothing available renders nothing
- Given: `nx_agent.roadmap_pulse(code)` returns `None`, no local `.line` file exists yet for the
  current project, and no active nexus-agent credential resolves
- When: the beads-bar row renders
- Then: the row is empty — no error, no placeholder text, no countdown glyph with nothing to
  prefix/render (unchanged from the prior requirement version's "no cache yet" contract, now also
  gated on the account segment's own absence)

### Requirement: The tmux status bar is three lines
`home/dot_config/tmux/tmux.conf.tmpl` SHALL set a BASE `status 3` (landscape/desktop default),
and each shipped theme (`vercel-theme.conf`, `one-hunter-vercel-theme.conf`,
`tokyo-night-abyss-theme.conf`, `nord-theme.conf`) SHALL define its session/usage,
beads/proposals, AND agents/title rows via COMPUTED indices (the `@cc-tab-rows` global tmux
option the render job maintains) rather than literal `status-format[N]` indices: session at
`[N]`, beads at `[N+1]`, and — when the `@cc-row-agents` payload is nonempty — agents/title at
`[N+2]` (changed from the prior version, which defined only the session and beads rows). The
render job SHALL set the total line count to `tab_rows + 2`, plus one when the agents-row
payload is nonempty, capped at tmux's hard five-line ceiling; at the cap the agents row is the
one omitted (lowest priority — a portrait client whose tabs wrap to three rows keeps session
and beads rows and drops agents). On a landscape/desktop client with no agents-row content,
this is BYTE-IDENTICAL to the prior fixed behavior (`status 3`, session/beads at `[1]`/`[2]`).
A theme MUST NOT enable the multi-line bar without defining all extra rows relative to
whatever `@cc-tab-rows` resolves to.

#### Scenario: landscape client without agents content is byte-identical to the prior behavior
- Given: a landscape client whose focused window publishes an empty agents-row payload
- When: the status bar renders
- Then: `@cc-tab-rows` resolves to `1`, `status` is `3`, and `status-format[1]`/`[2]` hold the
  session-bar/beads-bar content exactly as the prior requirement version specified

#### Scenario: landscape client with agents content gains a fourth line
- Given: a landscape client whose focused window publishes a nonempty agents-row payload
- When: the status bar renders
- Then: `status` is `4` and the agents/title row renders at `status-format[3]`, below the
  beads row

#### Scenario: portrait client with wrapped tabs shifts the extra rows
- Given: a portrait client where tabs wrap across 2 physical rows and the agents payload is
  nonempty
- When: the status bar renders
- Then: `@cc-tab-rows` resolves to `2`, `status` is `5`, and session/beads/agents render at
  `status-format[2]`/`[3]`/`[4]` — no row collision, no dropped content

#### Scenario: the five-line ceiling drops the agents row first
- Given: a portrait client where tabs wrap across 3 physical rows and the agents payload is
  nonempty
- When: the status bar renders
- Then: `status` is `5`, session/beads render at `status-format[3]`/`[4]`, and the agents row
  is omitted — session and beads rows are never the ones sacrificed

#### Scenario: all four themes define the extra rows relative to the computed index
- Given: `@cc-tab-rows` has resolved to any value N >= 1
- When: any of the four shipped themes is active
- Then: every enabled extra row renders at its computed index (`[N]`, `[N+1]`, and `[N+2]`
  when present) — none falls back to tmux's default pane-list row, regardless of N

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

### Requirement: Clicking the row-2 account label opens a read-only accounts popup
The plugin SHALL bind a click on the account-identity segment (row 3, per the beads/proposals
row requirement above) to `cc-tmux accounts-popup`, a read-only floating pane (positioned
immediately above the current status-bar row) listing every tracked Claude account's 5-hour/7-day
utilization as text plus a combined 2-metric braille glyph (20 cells wide: rows 1-2 = 5H, rows
3-4 = 7D, each metric using the full 4-dot-per-cell budget) — uniformly for every account,
active or not. The popup SHALL NOT render any session-context-window (SES) data anywhere: SES is
a property of the currently-focused pane's session, not of an account, and does not belong in an
account-usage view. The active account is distinguished ONLY by a leading `*` marker — no
separate glyph shape, token-count label, or other session-scoped field marks it. When fzf and
tmux >= 3.2 are available (the same `supports_popup` gate `cc-tmux inbox`/`picker-data` already
use), the popup pipes through fzf with `--no-input` (query box hidden/disabled — genuinely
cannot be typed into, not merely dismissed on the first keystroke) and a `[x]`-labeled header
bound via `--bind 'click-header:abort'` (a real clickable close target — tmux's own
`display-popup` has no native mouse-click dismissal). Row clicks and Enter are inert
(`--bind 'left-click:ignore'`/`'enter:ignore'`) — this is a read-only view, it MUST NOT switch or
swap the active credential. Without fzf/tmux 3.2+, the popup falls back to a static
`display-popup` dismissed by any keystroke. The fzf-backed popup SHALL render its full account
list within the popup pane's actual available height — it MUST NOT truncate the list to less
than the pane's true height when the account count would otherwise fit.

#### Scenario: popup lists every tracked account uniformly, no SES anywhere
- Given: 3 tracked nexus-agent credentials, one active, and the click lands on the row-3 account
  identity segment
- When: the accounts popup opens
- Then: all 3 accounts (active and non-active alike) each show `<label> 5H:xx% 7D:xx%` (no SES
  field anywhere) plus a 20-cell 2-metric braille glyph (rows 1-2 = 5H, rows 3-4 = 7D); the
  active account's row is distinguished only by its leading `*` marker

#### Scenario: duplicate and orphaned credential rows collapse or drop before display
- Given: nexus-agent's `/credentials` payload contains multiple historical rows for the same
  `(accountEmail, orgUuid)` pair (per if-lp8v/if-m5q6), and/or orphaned rows with no
  `accountEmail` and `status: refresh_failed`
- When: the accounts popup resolves its account list
- Then: exactly one row appears per distinct `(accountEmail, orgUuid)` pair using its
  most-recently-seen usage data, and orphaned no-email/`refresh_failed` rows are dropped
  entirely rather than rendered as fake accounts

#### Scenario: popup positions above the current row
- Given: the accounts popup opens
- When: it renders
- Then: it appears as a floating pane positioned immediately above the current status-bar row,
  not overlapping it

#### Scenario: the popup fills its actual available height, no truncation
- Given: the accounts popup opens with 3 deduped accounts (5 rendered lines each: summary,
  identity, two reset lines, separator)
- When: the popup renders
- Then: every account's full block is visible with no row cut off, using the popup pane's actual
  available height rather than a fixed fraction that leaves real content off-screen

#### Scenario: unreachable nexus-agent shows nothing
- Given: nexus-agent is unreachable
- When: the account identity segment is clicked
- Then: the popup shows no accounts (fail-open, no error) — same degradation convention as every
  other nexus-agent-dependent segment in this plugin

#### Scenario: popup is dismissed via a real click target when fzf is available
- Given: fzf and tmux >= 3.2 are available, and the accounts popup is open
- When: the user clicks the `[x]` header or presses `q`
- Then: the popup closes (`click-header:abort` / `q:abort`), and at no point does the popup
  accept typed query input (`--no-input`) or act on a row click/Enter

#### Scenario: popup falls back to any-keystroke dismiss without fzf
- Given: fzf is unavailable or tmux is below 3.2
- When: the accounts popup opens
- Then: it renders as a static `display-popup`, dismissed by any single keystroke (no click
  target in this fallback)

### Requirement: The animated tab icon reflects sub-agent activity
The animated tab icon SHALL render a single flashing diamond pair — `◇` alternating with `◆` by
wall-clock tick parity — when a pane has one or more sub-agent dispatches tracked as active,
whether foreground (via a matched `PreToolUse`/`PostToolUse` pair on the `Task` tool) or
background (via the time-boxed heuristic), and regardless of count (changed from the PRIOR
four dedicated braille flash pairs keyed by foreground/background and count 1/2+; per-agent
count and idle detail is delegated to the dedicated sub-agent status row introduced by the
`cc-tmux-row4-session-title` proposal). When no sub-agent activity is tracked for a pane, the
tab icon SHALL render exactly as the "Animated tab icon reflects state via a wall-clock-driven
refresh" Requirement specifies (unchanged). Foreground/background tracking mechanics —
increment/decrement on hook fire, background time-boxed age-out pruning on read — are UNCHANGED
by this requirement version; only the rendering collapses to one pair.

#### Scenario: no sub-agents tracked renders the existing icon unchanged
- Given: a tracked pane with `@cc-subagent-fg` at 0 and no unexpired `@cc-subagent-bg` entries
- When: the tab icon renders
- Then: it shows the existing `@cc-state`-driven glyph (waiting/idle/active), unaffected by
  this Requirement

#### Scenario: any sub-agent activity flashes the diamond pair
- Given: four separate tracked panes, one each in foreground-count-1, foreground-count-2+,
  background-count-1, and background-count-2+ states
- When: each pane's tab icon is captured at two different wall-clock seconds one second apart
- Then: every one of the four panes shows `◇` at one capture and `◆` at the other — the same
  single pair for all four cases

#### Scenario: a foreground sub-agent dispatch increments and decrements the count
- Given: a pane whose Claude session dispatches a foreground (blocking) sub-agent
- When: the dispatch's `PreToolUse` (`Task` matcher) fires
- Then: `@cc-subagent-fg` increments; when the matching `PostToolUse` fires (the dispatch
  returned), it decrements back — and the tab icon returns to its state-driven glyph once both
  counts are zero

#### Scenario: a background dispatch ages out of the active count
- Given: a pane's Claude session dispatches a background sub-agent, recorded in
  `@cc-subagent-bg` with a launch timestamp
- When: more than `@cc-subagent-bg-timeout` seconds have elapsed since that launch
- Then: that entry no longer counts toward the diamond overlay (pruned on read, not
  necessarily deleted immediately)

### Requirement: The window-tabs row adapts to a portrait/mobile client
The plugin SHALL detect portrait orientation from `client_width`/`client_height` (passed as job
arguments to the render-all command, the same pattern `#{window_id}` already uses) and, when
`client_height > client_width`, render the tabs row at an enlarged size instead of the default
landscape sizing. Enlargement SHALL be attempted via Kitty's OSC 66 text-sizing escape sequence
(`s=3`, literal 3x character scale) wrapping each tab's index/icon/name segment, contingent on a
documented fallback: if live verification (see the Testing section of this capability's owning
proposal) finds OSC 66 renders incorrectly or corrupts the surrounding status bar in this
fleet's actual terminal (Ghostty), the plugin SHALL instead render tabs at increased horizontal
padding/spacing only, with no escape-sequence-based scaling. When the combined width of all
enlarged tab segments exceeds `client_width`, the plugin SHALL wrap tabs across multiple
physical status-format rows (see "The tmux status bar is three lines" MODIFIED delta for how the
extra rows shift to accommodate this) rather than truncating or overflowing off the right edge.
On a landscape client, this requirement has NO effect — tabs render exactly as the pre-existing
`render_tabs_row` behavior, single row, default sizing.

#### Scenario: landscape client renders tabs unchanged
- Given: a client where `client_width >= client_height`
- When: the tabs row renders
- Then: every tab renders at default (1x) sizing, in a single physical row, byte-identical to
  the pre-existing behavior

#### Scenario: portrait client enlarges tabs via OSC 66
- Given: a client where `client_height > client_width`, and OSC 66 has been confirmed to render
  correctly in this fleet's terminal (live-verified, not assumed)
- When: the tabs row renders
- Then: each tab's index/icon/name segment is wrapped in an OSC 66 `s=3` escape sequence,
  rendering at approximately 3x the default character size

#### Scenario: portrait client falls back to padding-only sizing if OSC 66 is unreliable
- Given: a client where `client_height > client_width`, and OSC 66 has been found (via live
  verification) to render incorrectly or corrupt the status bar in this fleet's terminal
- When: the tabs row renders
- Then: tabs render at increased horizontal padding/spacing only — no OSC 66 escape sequence is
  emitted, and the surrounding status bar renders correctly

#### Scenario: many tabs on a narrow portrait client wrap to a second row
- Given: a portrait client whose enlarged tab segments' combined width exceeds `client_width`
  when rendered in a single row
- When: the tabs row renders
- Then: tabs wrap across as many physical status-format rows as needed to fit without horizontal
  overflow, and the session-bar/beads-bar rows shift down to accommodate (per the MODIFIED
  "three lines" delta)

#### Scenario: a portrait client with few tabs still fits in one row
- Given: a portrait client whose enlarged tab segments' combined width fits within
  `client_width` in a single row
- When: the tabs row renders
- Then: `@cc-tab-rows` resolves to `1` and no wrapping occurs, even though sizing is enlarged

### Requirement: A dedicated status row shows the session title or per-agent detail
The plugin SHALL publish a fourth status-row payload (`@cc-row-agents` global option, produced
by the same `render-all` job that publishes the session and beads rows) for the focused
window's representative tracked pane. Content contract, in precedence order:

1. When the pane has one or more tracked sub-agent dispatches (foreground count nonzero or
   unexpired background entries), the row SHALL render a glyph strip with one glyph per
   tracked background dispatch in launch order: `◌` alternating with `○` by wall-clock tick
   parity while the dispatch is inside its busy window (not-idle), and a static `●` once the
   dispatch is older than the busy window but not yet aged out (settled/idle). The busy window
   SHALL be a tunable pane/global option (`@cc-subagent-bg-busy-window`) defaulting below the
   existing `@cc-subagent-bg-timeout` age-out. Because no hook signals a background dispatch's
   true completion on this CC fleet, busy-vs-idle is an elapsed-time heuristic — documented as
   such, mirroring the existing background age-out rule's posture.
2. Otherwise, when the pane carries a Claude session title (`@cc-title`, captured from the
   SessionStart `session_title` payload), the row SHALL render that title (truncated to the
   client width).
3. Otherwise the published payload SHALL be empty and the row SHALL NOT occupy a status line
   (see the MODIFIED three-lines requirement below).

The row is display-only: it SHALL NOT introduce any new hook, daemon, or network fetch — all
inputs are existing pane options read during the same `render-all` tick.

#### Scenario: foreground-only titled session shows the title
- Given: the focused window's tracked pane has `@cc-title` set, `@cc-subagent-fg` at 0, and no
  unexpired `@cc-subagent-bg` entries
- When: the status bar renders on a landscape client
- Then: the fourth row renders the session title text

#### Scenario: a busy background agent pulses the dotted circle
- Given: the pane has one `@cc-subagent-bg` entry younger than `@cc-subagent-bg-busy-window`
- When: the row is captured at two wall-clock seconds of opposite parity
- Then: the row shows `◌` at one capture and `○` at the other, in place of the title

#### Scenario: a settled background agent renders the filled circle
- Given: the pane has one `@cc-subagent-bg` entry older than the busy window but younger than
  `@cc-subagent-bg-timeout`
- When: the row renders at any two different wall-clock times
- Then: the row shows a static `●` both times

#### Scenario: multiple background agents render one glyph each
- Given: the pane has two unexpired `@cc-subagent-bg` entries, one inside and one past the
  busy window
- When: the row renders
- Then: the strip shows two glyphs in launch order — a pulsing `◌`/`○` and a static `●`

#### Scenario: no title and no agents publishes an empty payload
- Given: a focused window whose tracked pane has no `@cc-title` and no sub-agent activity
- When: `render-all` publishes the row options
- Then: `@cc-row-agents` is empty and the status bar renders without the fourth row

