# Repo Context: unsafe9/claude-tmux-hop

> Source: https://github.com/unsafe9/claude-tmux-hop
> Context: project (`if` dotfiles)   ·   Stars: 1   ·   Last push: 2026-06-12   ·   License: self-declared MIT (README + `plugin.json`), **no LICENSE file in repo** (`GET /LICENSE` → 404, GitHub API `license` field null)
> Ask: none — general sweep, triggered by Leo's plan to build a custom combined `cc-tmux` plugin

## Ask
No rider supplied. Context: Leo is planning `/feature` to scaffold a custom `apps/cc-tmux` plugin
folding in `tmux-which-key` (installed), `tmux-agent-indicator` (Adapt, prior recon), and
`tmux-claude-usage`'s pattern (Skip vs `tmux-nexus-creds`, prior recon) — then asked to recon this
repo before scaffolding. This recon's job is to determine whether it changes that plan. **It does**:
see Prior Coverage.

## Purpose
A combined Claude Code plugin + tmux plugin: tracks every Claude Code pane's state
(`waiting`/`idle`/`active`) via Claude Code hooks, then provides priority-based pane cycling,
jump-back, an fzf notification inbox, OS notifications + terminal auto-focus, status-bar
integration, window auto-rename, and an opt-in "conductor" — a persistent orchestrator Claude
session that dispatches tasks to other panes (switch / send-prompt / spawn-task / spawn-in-worktree).

## Architecture & Key Patterns

- **Single source of truth**: all state lives in tmux pane options (`@hop-state`, `@hop-timestamp`,
  `@hop-task`, `@hop-wait-reason`, `@hop-project`/`@hop-branch`) — no external files, auto-cleanup
  when panes close, every view (status bar, picker, cycle, inbox) derives from one
  `get_hop_panes()` read so they can't disagree (`CLAUDE.md` § Key Invariants; `tmux.py:set_pane_state()`, `get_hop_panes()`).
- **Hook → state mapping** (`hooks/hooks.json`) is far more complete than `tmux-agent-indicator`'s
  (prior recon): `SessionStart→idle`, `UserPromptSubmit→active`, `PreToolUse[AskUserQuestion]→waiting/question`,
  `PreToolUse[ExitPlanMode]→waiting/plan`, `PostToolUse[same]→active`, `Notification[permission_prompt]→waiting/permission`,
  `Notification[elicitation_dialog]→waiting/elicitation`, `Notification[idle_prompt]→idle`,
  `Elicitation→waiting/elicitation`, `ElicitationResult→active`, `Stop/StopFailure→idle`,
  `SessionEnd→clear`. Every hook carries `timeout: 10` so a hung tmux can't block Claude for the
  60s default — a hardening detail `tmux-agent-indicator` doesn't have.
- **Priority model** (`priority.py`): `waiting`(0) > `idle`(1) > `active`(2), newest-first within
  each group; `PENDING_STATES = {waiting, idle}` is what's cycled/dismissable — `active` is an
  overview only. Two cycle modes: `priority` (stay within the highest group) and `flat`.
- **Real state-transition guard**: auto-hop/app-focus fire only when `set_pane_state()` reports an
  actual state change — a re-asserted `idle` (e.g. `idle_prompt` re-firing after `Stop` already set
  idle) must not re-yank focus. Only the OS notification (its own fingerprint+cooldown dedup) fires
  on a re-register. This is a subtler correctness bar than `tmux-agent-indicator`'s implementation.
- **Notification Strategy pattern** (`notify/`): per-OS modules (macOS AppleScript/`terminal-notifier`,
  Linux `notify-send`+`xdotool`/`wmctrl`, Windows PowerShell toast) behind a common protocol;
  smart-suppression skips notify when the terminal (and correct tab on macOS) is already focused.
- **Notification inbox**: fzf popup (display-menu fallback) listing tracked panes as aligned
  columns (state icon, session:window, project, branch, time, wait reason, task); self-heals stale
  state left by a `kill -9`'d Claude process on open.
- **Conductor** (opt-in, off by default): persistent detached tmux session running `exec claude`;
  sees a live snapshot of all tracked panes on every prompt; four dispatch modes (switch,
  `send-prompt`, `spawn-task`, spawn-in-fresh-worktree). Dispatch mode selection lives in injected
  instructions (`install.py:CONDUCTOR_INSTRUCTIONS`), CLI shape lives only in the `hop-dispatch`
  skill — one place per concern. Also exposed standalone via the `hop-dispatch` Claude Code skill in
  any session ("spawn a fresh claude on this in a worktree").
- **Claude Code skills shipped**: `hop-status` (session overview), `hop-config` (inspect/edit
  `@hop-*` options + persist conductor instructions), `hop-dispatch` (route a task to another pane).
- **Path detection** (`paths.py`): covers XDG, `~/.tmux.conf`, oh-my-tmux, and TPM (env var or
  standard locations) — directly reusable logic for a from-scratch custom plugin's install script.

## Findings

### 1. claude-tmux-hop core (state/priority/cycle/inbox/notify/status) — ADAPT
**Source:** `unsafe9/claude-tmux-hop` (self-declared MIT, no LICENSE file, 1★) ·
`hooks/hooks.json`, `src/claude_tmux_hop/{tmux.py,priority.py,cli.py,notify/}`

**Coverage: NONE** (searched: `rg -il "hop|cycle-key|inbox|priority.based|jump-back" home/dot_config/tmux/ home/dot_local/bin/` → only false-positive "hop" matches in `executable_copen`'s SSH-hop comments; `rg -il "conductor|spawn-task|send-prompt" home/` → none).

**Supersedes the prior Adapt finding.** `docs/recon/tmux-claude-plugins.md` recommended Adapting
`accessd/tmux-agent-indicator` for pane-state visual feedback. `claude-tmux-hop` covers that same
domain (pane border/title/status-icon-equivalent via `@hop-status`) **and goes further**: priority
cycling, jump-back, a real notification inbox, window auto-rename, and — critically — a
notably more complete + hardened hook set (10s timeouts, `PostToolUseFailure`/`StopFailure`
coverage, real-transition guard against re-fired hooks). **When `apps/cc-tmux` is scaffolded, use
this repo's `hooks.json` + `tmux.py`/`priority.py` architecture as the state-tracking template
instead of `tmux-agent-indicator`'s.**

**Before:** No pane-state awareness in `if`'s tmux at all (confirmed both recons). Switching
between parallel Claude panes (cmux workspaces) means manually checking each one.
**After:** Every Claude pane's state is tracked; `prefix+space`-equivalent cycles to the pane that
needs you first; a notification inbox lists everything waiting/idle across all sessions; OS
notification + terminal auto-focus fire on `waiting`.

**Effort:** medium-large (this is the bulk of the planned `apps/cc-tmux` scaffold, not a drop-in
plugin — see Placement Verdict). **Files that would change:** new `apps/cc-tmux/` tree (Python or
bash port of the state/priority/hook logic), `home/dot_config/tmux/tmux.conf.tmpl` (load + keybind
config), `if`'s Claude-side hook wiring (separate cc-governed decision, same gate as the prior
agent-indicator finding), `docs/tmux-layout-keybindings.md` (new keys).

#### Placement Verdict
| # | Row | Verdict |
|---|-----|---------|
| 1 | **Layer** | Custom application: a Python CLI (state/priority/notify logic, mirroring `src/claude_tmux_hop/`) + a tmux-plugin shell entrypoint (mirroring `hop.tmux`) + a Claude Code hooks config — not a scripts/bin data-producer, not a skill, not a cc command. |
| 2 | **Landing path** | `apps/cc-tmux/` (new top-level dir per Leo's stated plan) — e.g. `apps/cc-tmux/src/cc_tmux/{cli.py,tmux.py,priority.py,notify/}`, `apps/cc-tmux/cc-tmux.tmux` (TPM-style entrypoint, loaded manually like `tmux-which-key` — no TPM in this repo), `apps/cc-tmux/hooks/hooks.json`. |
| 3 | **Extend-before-create** | No existing artifact owns this domain in `if` — `tmux-nexus-creds` is usage-only, `tmux-which-key` is action-menu-only (installed this session), `tmux-agent-indicator` was Adapt-recommended but never installed. New standalone `apps/cc-tmux` is correct: Leo explicitly wants ONE custom plugin combining all threads rather than 3 separate third-party installs. |
| 4 | **Standalone vs facet** | Standalone application (not a skill facet). |
| 5 | **Scope** | Personal tmux/Claude workflow — `if`-repo-local (project scope), chezmoi-deployed to all Leo's machines. Not cc-global; not fleet-bound. |
| 6 | **Tracked medium** | `git ls-files -s apps` → empty (dir doesn't exist yet). This IS the vendor-first precondition: `/feature` scaffolds `apps/cc-tmux/` as a real tracked dir from day one, no symlink, no vendoring behind `~/.agents`. |
| 7 | **Gitignore hazard** | `apps/` is not currently gitignored (confirm at scaffold time); no hazard expected — flag if `/feature` adds a build-artifact subpath that needs one. |
| 8 | **Description class** | n/a — not a skill. |
| 9 | **Wiring sites** | Durable pointers (project-context, not cc): `if/CLAUDE.md` (repo overview gets a line), `docs/tmux-layout-keybindings.md` (new keybindings), `home/dot_config/tmux/tmux.conf.tmpl` load line. |
| 10 | **Caller + cadence** | Leo's tmux, every session with parallel Claude panes (cmux workspaces) — high, continuous. |
| 11 | **Fleet propagation** | n/a — single-repo personal dotfiles; chezmoi handles multi-machine deploy, same as `tmux-which-key`. **Same open cross-repo item as the prior agent-indicator finding:** the hook config wants to land in `~/.claude/settings.json` = `~/dev/cc` symlink (cc-governed). Decide hook placement (cc change vs. `CLAUDE_CONFIG_DIR` scoping vs. process-detection-only fallback) before `/feature` finalizes the design — same STOP-and-ask as before, now consolidated to one decision instead of two. |

---

### 2. Conductor (orchestrator dispatch) — MONITOR
**Source:** `unsafe9/claude-tmux-hop` `install.py:CONDUCTOR_INSTRUCTIONS`, `tmux.py:spawn_conductor_session()`, `skills/hop-dispatch`

Genuinely interesting pattern (persistent background Claude session dispatching to worktree-spawned
panes, four dispatch modes, injected live pane-snapshot context per turn) — but it's opt-in in the
source repo itself, and `if` has no caller for it yet: `scripts/cmux-workspaces.sh` is adjacent
prior art (launches project workspaces via cmux, macOS-only) but solves a different problem
(initial workspace launch, not runtime task dispatch to already-running panes). Per the
over-adoption guard, this is NOT bundled into the core Adapt card above — build and prove out the
state/cycle/inbox core first (item 1), then revisit conductor as a follow-on `/feature` once that
core has a real caller and Leo has used it enough to know if the dispatch model fits his cmux-based
parallel-Claude workflow.

## Prior Coverage
No prior recon of this exact target (`docs/recon/unsafe9-claude-tmux-hop.{md,html}` didn't exist,
no matching archived openspec, `bd search` empty). **Directly supersedes part of a related prior
recon**: `docs/recon/tmux-claude-plugins.md` (2026-07-09) Adapted `accessd/tmux-agent-indicator`
for the same pane-state-visibility domain — see Finding 1 above for why `claude-tmux-hop`'s
architecture should be the template instead when `apps/cc-tmux` is built. That prior recon's Skip
verdict on `docker-run/tmux-claude-usage` (duplicate of `tmux-nexus-creds`) is unaffected and
still holds.
