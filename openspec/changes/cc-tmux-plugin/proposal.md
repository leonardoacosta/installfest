# Proposal: Build `cc-tmux` — a custom Claude Code + tmux plugin

## Change ID
`cc-tmux-plugin`

## Summary
Stand up a new `apps/` directory and build `cc-tmux`, a first-party combined Claude Code plugin +
tmux plugin that makes parallel Claude Code sessions visible and navigable inside tmux. It tracks
every Claude pane's state (`waiting`/`idle`/`active`) via Claude Code hooks, then provides
priority-based pane cycling, jump-back, an fzf notification inbox, OS notifications with terminal
auto-focus, status-bar integration, and window auto-rename. It folds in the existing multi-account
Claude usage display (currently `tmux-nexus-creds`) as a status segment, and ships a **Conductor** —
a persistent background Claude session that dispatches tasks to other panes (switch / send-prompt /
spawn-task / spawn-in-worktree). This is a clean-room adaptation of the architecture surveyed in
`docs/recon/unsafe9-claude-tmux-hop.md` (Adapt + Monitor findings) and
`docs/recon/tmux-claude-plugins.md` (nexus-creds usage segment), NOT a fork — the surveyed repo
declares MIT but ships no LICENSE file, so all code is original.

## Context
- Extends: creates a NEW top-level `apps/` directory (first app: `apps/cc-tmux/`); wires into
  `home/dot_config/tmux/tmux.conf.tmpl` (plugin load, mirrors the `tmux-which-key` `if-shell`
  guard shipped this session), the active theme's `status-right` (`home/dot_config/tmux/vercel-theme.conf`),
  and a new `home/run_onchange_after_install-cc-tmux.sh.tmpl` (mirrors `run_onchange_after_install-tmux-which-key.sh.tmpl`)
- Related: `home/dot_local/bin/executable_tmux-nexus-creds` (existing usage segment, folded in);
  `home/run_onchange_after_install-tmux-which-key.sh.tmpl` (install-script template); `scripts/cmux-workspaces.sh`
  (adjacent workspace launcher, NOT a dependency); `docs/tmux-layout-keybindings.md` (keybinding doc to extend);
  recon sources `docs/recon/unsafe9-claude-tmux-hop.md`, `docs/recon/tmux-claude-plugins.md`
- depends on: none
- touches: `apps/cc-tmux/pyproject.toml`, `apps/cc-tmux/bin/cc-tmux`, `apps/cc-tmux/cc-tmux.tmux`, `apps/cc-tmux/.claude-plugin/plugin.json`, `apps/cc-tmux/hooks/hooks.json`, `apps/cc-tmux/src/cc_tmux/tmux.py`, `apps/cc-tmux/src/cc_tmux/priority.py`, `apps/cc-tmux/src/cc_tmux/cli.py`, `apps/cc-tmux/src/cc_tmux/notify/__init__.py`, `apps/cc-tmux/src/cc_tmux/usage.py`, `apps/cc-tmux/src/cc_tmux/conductor.py`, `apps/cc-tmux/src/cc_tmux/paths.py`, `home/dot_config/tmux/tmux.conf.tmpl`, `home/dot_config/tmux/vercel-theme.conf`, `home/run_onchange_after_install-cc-tmux.sh.tmpl`, `docs/tmux-layout-keybindings.md`

## Motivation
Leo runs many Claude Code sessions in parallel tmux panes (cmux workspaces across the B&B /
Priceless / Personal project sets). Today there is zero ambient signal about which pane just
finished, which is blocked on a permission prompt, and which is still working — you switch panes to
check. Two prior recons surveyed third-party solutions:

- `docs/recon/tmux-claude-plugins.md` recommended Adapting `accessd/tmux-agent-indicator` for
  visual pane-state feedback, and Skipped `docker-run/tmux-claude-usage` because
  `tmux-nexus-creds` already does multi-account usage better.
- `docs/recon/unsafe9-claude-tmux-hop.md` **superseded** the agent-indicator recommendation:
  `claude-tmux-hop` covers the same state-visibility domain with a richer, more hardened hook set
  (10s timeouts, `PostToolUseFailure`/`StopFailure` coverage, a real state-transition guard against
  re-fired hooks) plus priority cycling, jump-back, a notification inbox, and an opt-in Conductor.

Rather than install three separate third-party plugins (one of which has no LICENSE file), the
decision is to build ONE first-party plugin that owns this domain, deploys via chezmoi to all of
Leo's machines, and registers its Claude hooks through the Claude Code plugin manifest mechanism —
so it needs **zero edits to cc's `settings.json`** (the cross-repo governance concern flagged twice
in recon is sidestepped entirely: a `claude plugin install` self-registers the hooks). `tmux-which-key`
(installed this session) stays a separate third-party plugin — it is a general tmux action menu, a
different concern, and folding it in would be scope creep.

Implementation language is Python 3.10+ stdlib-only (no external deps), matching the surveyed
architecture: the state/priority/inbox/notify logic is genuinely complex (dataclasses, a per-OS
notification Strategy, testable pure functions) and would be painful and untestable in pure bash.
`python3` is already present on all target machines.

## Requirements

### Req-1: Create the `apps/` tree and the `cc-tmux` Python package skeleton
Create the first top-level `apps/` directory with `apps/cc-tmux/` containing:
- `pyproject.toml` — package metadata, `requires-python = ">=3.10"`, no runtime dependencies
- `bin/cc-tmux` — executable CLI wrapper that sets `PYTHONPATH` to the bundled `src/` and invokes
  `python3 -m cc_tmux`
- `src/cc_tmux/__init__.py`, `__main__.py`, `cli.py` (argparse subcommands → `cmd_<name>()` handlers),
  `paths.py` (tmux.conf + plugin-path detection covering XDG, `~/.tmux.conf`, and manual clone dir)
The CLI must run stand-alone from the repo clone with no install step (invoked by tmux keybindings
and Claude hooks by absolute path).

### Req-2: Pane state stored in tmux pane options (single source of truth)
Pane state lives ONLY in tmux pane options — no external state files. Every view (status bar,
picker, cycle, inbox) derives from a single `get_hop_panes()`-equivalent read so they cannot
disagree, and state auto-deletes when a pane closes. Options: `@cc-state`
(`waiting`/`idle`/`active`), `@cc-timestamp`, `@cc-task`, `@cc-wait-reason`
(`question`/`plan`/`permission`/`elicitation`), `@cc-project`, `@cc-branch`. The `active` register is
the hot path and skips git-identity resolution; only `waiting`/`idle` resolve it.

### Req-3: Claude Code plugin manifest + hooks (state mapping, self-registering)
Ship `apps/cc-tmux/.claude-plugin/plugin.json` and `apps/cc-tmux/hooks/hooks.json` so the plugin
installs via `claude plugin install` and self-registers its hooks WITHOUT editing cc's
`settings.json`. Hook → state mapping (each command carries `timeout: 10` so a hung tmux cannot
block Claude for the 60s default):
- `SessionStart[startup|resume]` → register `idle`
- `UserPromptSubmit` → register `active`
- `PreToolUse[AskUserQuestion]` → `waiting`/reason `question`; `PreToolUse[ExitPlanMode]` → `waiting`/`plan`
- `PostToolUse[AskUserQuestion|ExitPlanMode]` → `active`; `PostToolUseFailure[same]` → `active`
- `Notification[permission_prompt]` → `waiting`/`permission`; `[elicitation_dialog]` → `waiting`/`elicitation`; `[idle_prompt]` → `idle`
- `Stop` / `StopFailure` → `idle`
- `SessionEnd` → clear
Intentionally NOT hooked: PreCompact/PostCompact/SubagentStart/SubagentStop (infra events, not
user-visible state transitions).

### Req-4: Priority-based cycling and jump-back
Priority order `waiting`(0) > `idle`(1) > `active`(2), newest-first within each group. Two cycle
modes via `@cc-cycle-mode`: `priority` (cycle only the highest-priority non-empty group) and `flat`
(all panes in priority order). `cc-tmux cycle` advances to the next pane; `cc-tmux back` jumps to
the previous pane across sessions/windows. `active` panes are listed but never cycled — only
`{waiting, idle}` are pending.

### Req-5: Notification inbox (fzf popup with menu fallback)
`cc-tmux inbox` lists every tracked pane — attention first (`waiting` → `idle`), then `active` — as
aligned columns: state icon, `session:window`, project, branch, time-in-state, wait reason, task
summary. An fzf popup (`display-popup`) is used when tmux ≥ 3.2 and `fzf` is present, else a
`display-menu` fallback. `enter` switches to the pane; `ctrl-x` dismisses the waiting/idle entries
(a view filter via a global cleared-at stamp — never mutates state, so status counts are untouched;
active rows stay). On open, self-heal stale state left by a `kill -9`'d Claude (a failed process
scan shows everything rather than mass-clearing live sessions).

### Req-6: OS notification + terminal focus (per-OS Strategy)
`@cc-notify` and `@cc-focus-app` (both comma-separated state lists, default empty = off) fire an OS
notification / bring the terminal to the foreground when a pane enters a listed state. Per-OS
Strategy modules: macOS (AppleScript `display notification`, optional `terminal-notifier`
click-to-focus, iTerm2/Terminal.app tab focus), Linux (`notify-send`, `wmctrl`/`xdotool`), Windows
(PowerShell toast). Smart suppression: skip notify/focus when the terminal (and the correct tab on
macOS) is already frontmost. Notifications dedup per pane within a cooldown. **Auto-hop and app
focus fire ONLY on a real state transition** — a re-asserted state (e.g. `idle_prompt` re-firing
after `Stop` already set `idle`) must not re-yank focus to a pane the user already left.

### Req-7: Status-bar integration and window auto-rename
`cc-tmux status` emits pane counts (`@cc-status`, e.g. `{waiting:icon} {idle:icon} {active:icon}`
via `@cc-status-format`); `cc-tmux status-inbox` emits a clickable pending-pane badge list for an
optional second status line (`#[range=pane|<id>]` + tmux's default `MouseDown1Status` switch-client
gives click-to-hop free). `@cc-window-rename` (default off) renames the window to
`<state-icon> <dir basename>`, icon tracking the highest-priority state among its Claude panes while
the directory name stays a stable label; `automatic-rename` stays off.

### Req-8: Claude usage segment (fold in the nexus-agent query, replace tmux-nexus-creds)
`cc-tmux usage` renders the multi-account Claude usage segment currently produced by
`home/dot_local/bin/executable_tmux-nexus-creds`: query `nexus-agent` at
`http://localhost:7402/credentials`, read `active_account` + per-account `five_hour.utilization` /
`seven_day.utilization`, render `<account> 5H:xx% 7D:xx%` with the existing cyan/yellow/red
thresholds (≥0.50 amber, >0.80 red), output nothing on failure. The active theme's `status-right`
switches from `#(tmux-nexus-creds)` to the cc-tmux segment. This is a **replacement** of the
standalone script (its logic moves into `cc_tmux/usage.py`); `executable_tmux-nexus-creds` is
removed in the same change so there is no dead duplicate.

### Req-9: Conductor — persistent orchestrator session with dispatch (same batch as core)
`@cc-conductor-enabled` (default off) enables a persistent detached tmux session (default name
`conductor`, via `@cc-conductor-session`) running `exec claude`. `prefix + y` opens a popup attached
to it (created on demand); detaching keeps work running; `prefix + Y` kills + respawns (destructive,
picks up refreshed instructions). The conductor sees a live snapshot of all tracked panes on every
prompt (injected via its own SessionStart/UserPromptSubmit hooks, shell-guarded on
`CC_TMUX_CONDUCTOR=1` so non-conductor sessions skip the interpreter) and routes each task via four
dispatch modes: switch to an existing pane, `send-prompt` into one (refuses `active` panes unless
forced), `spawn-task` (new window in the project root), or spawn into a fresh git worktree. Mode
SELECTION lives in the injected conductor instructions; CLI SHAPE lives only in the `cc-dispatch`
skill (Req-10) so flag changes land in one place. The conductor session name doubles as a filter —
its panes are excluded from every pane view.

### Req-10: Ship Claude Code skills (`cc-status`, `cc-config`, `cc-dispatch`)
Bundle three skills in `apps/cc-tmux/skills/`, available in any Claude Code session once installed:
`cc-status` (summarize all tracked Claude sessions + states), `cc-config` (inspect/persistently edit
`@cc-*` tmux options and update conductor instructions), `cc-dispatch` (route a task to another pane
— the single home of the dispatch CLI shape used by both the Conductor and ad-hoc sessions).

### Req-11: tmux entrypoint + keybindings (`cc-tmux.tmux`)
`apps/cc-tmux/cc-tmux.tmux` is the tmux-side entrypoint (loaded via `run-shell`, guarded by an
`if-shell` presence check mirroring the `tmux-which-key` block in `tmux.conf.tmpl`). It binds
(defaults, all overridable via `@cc-*-key`): `prefix + Space` cycle, `prefix + C-f` picker,
`prefix + i` inbox, `C-Space` (root, no prefix) jump-back, and — when the conductor is enabled —
`prefix + y` / `prefix + Y`. It sets `@cc-status` / `@cc-status-inbox` and auto-discovers already
running Claude sessions on load. Cycle/inbox keys must not collide with the existing
`tmux-which-key` `prefix + Space` binding — resolve the collision (see design.md; `cc` cycle moves
to a non-conflicting key OR which-key's menu key moves) rather than silently double-binding.

### Req-12: chezmoi install wiring + package prerequisites
Create `home/run_onchange_after_install-cc-tmux.sh.tmpl` (mirrors the which-key install script,
`DOTFILES="{{ .chezmoi.workingTree }}"`, `scripts/utils.sh` helpers, source-guard strict mode). It
must: (a) symlink or copy `apps/cc-tmux` into `~/.tmux/plugins/cc-tmux` for the tmux `run-shell`
load, (b) register the Claude Code plugin from the local path (`claude plugin install` against the
repo clone, idempotent), (c) warn (not fail) if `fzf` or `python3` is absent. Add the `run-shell`
load line to `tmux.conf.tmpl`, switch the theme `status-right` to the cc-tmux usage segment, and add
`fzf` to `platform/homebrew/Brewfile` + the Arch package list if not already present. Update
`docs/tmux-layout-keybindings.md` with the new keybindings.

### Req-13: Clean-room implementation, licensing, and graceful degradation
All code is original (the surveyed `claude-tmux-hop` declares MIT but ships no LICENSE file, so it
is not a safe source for verbatim copy); the architecture is adopted, the code is written fresh, and
`apps/cc-tmux/LICENSE` (MIT, Leo) is included. Every CLI subcommand must fail open: absent `$TMUX`
or missing `tmux` → exit 0 silently; a hook that errors must never block Claude; missing `fzf` →
menu fallback; missing `nexus-agent` → empty usage segment. Include a `cc-tmux self-test`
(`testing.py`) covering the priority sort, state transitions, and path detection.

## Testing
- Req-2/4 (state + priority): `cc-tmux self-test` unit coverage of `priority.py` sort order and
  `set_pane_state` transition detection (pure functions, no tmux needed).
- Req-3 (hooks): install the plugin, drive a real Claude session, assert `@cc-state` flips
  `active`→`waiting`→`idle` across a permission prompt (Verification Batch runtime check with pasted
  `tmux show-options -p` output).
- Req-5 (inbox): open the inbox with ≥2 tracked panes, assert aligned columns render and `enter`
  switches (runtime check).
- Req-8 (usage): assert the cc-tmux usage segment output byte-matches the retired
  `tmux-nexus-creds` output against a live nexus-agent (runtime diff).
- Req-9 (conductor): enable conductor, open the popup, assert dispatch `send-prompt` reaches a
  target pane (runtime check).
- Req-11 (keybindings): assert no `prefix + Space` double-bind after load (`tmux list-keys` diff vs
  which-key).
