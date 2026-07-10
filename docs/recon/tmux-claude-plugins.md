# Repo Context: tmux Claude-Code plugins (2-target recon)

> Sources: https://github.com/accessd/tmux-agent-indicator · https://github.com/docker-run/tmux-claude-usage
> Context: project (`if` dotfiles)   ·   Stars: 72 / 10   ·   Last push: 2026-03-10 / 2026-07-08   ·   License: MIT / MIT
> Ask: none — general adoption sweep

## Ask
No rider supplied. Paired targets (`&`), both tmux status-bar plugins that integrate Claude Code.
Evaluated against the `if` tmux setup (just extended this session with `tmux-which-key`).

## Purpose
- **tmux-agent-indicator** — visual feedback for AI-agent lifecycle (`running` / `needs-input` /
  `done`) surfaced via pane borders, window-title colors, and status-bar icons. Driven by Claude
  Code hooks (`UserPromptSubmit`, `PermissionRequest`, `Stop`), with a process-detection fallback.
- **tmux-claude-usage** — Claude 5h/7d usage (progress bar + percent + reset) in the tmux status
  bar. Harvests official `rate_limits` from Claude Code's statusLine JSON into a cache file, then a
  tmux segment renders it. No API calls.

## Architecture & Key Patterns

### tmux-agent-indicator
- **State machine** (`scripts/agent-state.sh`): per-pane state stored in tmux global env; sets
  pane-border / window-title-bg-fg / status icon per state. Deferred reset (`reset-on-focus`) keeps
  the "done" color until you look at the pane, not when the hook fires.
- **Hook wiring** (`hooks/claude-hooks.json`): installer merges these into `~/.claude/settings.json`.
  `UserPromptSubmit → running`, `PermissionRequest → needs-input`, `Stop → done`.
- **Multi-agent adapters**: Claude (hooks), Codex (`notify`), OpenCode (plugin) — plus a generic
  `agent-state.sh` any wrapper can call.
- **Session dots** (`scripts/session-dots.sh`): `○●○●` row showing every session, current one
  highlighted, attention-needing ones in a different color.

### tmux-claude-usage
- **Harvester** (`scripts/statusline.sh`): a Claude Code statusLine command that reads session JSON
  on stdin, extracts `rate_limits.{five_hour,seven_day}`, writes `~/.cache/claude-usage/usage`, and
  **prints nothing**. Has a freshness guard (accept a snapshot only if window is newer or the
  same-window reading is higher) to stop idle sessions replaying stale snapshots.
- **Single-slot chaining** (`scripts/chain.sh`): Claude Code has exactly ONE statusLine slot. This
  wrapper reads the JSON once, feeds a copy to the silent harvester, then re-runs your *original*
  statusLine command (stored as a file, read via `jq`) and passes its output through — so usage
  harvesting and your existing line coexist in one slot. Genuinely elegant quoting-safe design.

## Findings

### 1. tmux-agent-indicator — ADAPT
**Source:** `accessd/tmux-agent-indicator` (MIT, 72★) · `scripts/agent-state.sh`, `hooks/claude-hooks.json`

**Coverage: NONE** (searched: `rg -il "needs-input|agent.state|pane.border.*yellow|window-status.*done|running.*done" home/dot_config/tmux/` → no match). `if` has no visual agent-state
indicator; the only adjacent thing is `claude-tab` window renaming (title text, not state color).

**Before:** Running N parallel Claude panes (cmux workspaces), you switch panes to check which one
finished or is blocking on a permission prompt — no ambient signal.
**After:** Pane border + window-title color + status icon flips on `done`/`needs-input`; session
dots row shows which session needs attention at a glance.

**Why Adapt not Steal:** needs modification, not verbatim install —
(a) do NOT use their `curl|bash` installer; mirror the `if` manual-clone `run_onchange` pattern;
(b) the Claude-hook half writes into `~/.claude/settings.json`, which on Leo's machine is a symlink
to `~/dev/cc` — a **cross-repo, cc-governed** surface. That hook addition is a separate cc decision,
not an `if` change.

**Effort:** medium. **Files that would change:** new `home/run_onchange_after_install-tmux-agent-indicator.sh.tmpl`; `home/dot_config/tmux/tmux.conf.tmpl` (run-shell load); theme `status-right`
(add `#{agent_indicator}` / `#{agent_session_dots}`); + a cc-side hooks decision.

#### Placement Verdict
| # | Row | Verdict |
|---|-----|---------|
| 1 | **Layer** | External tmux plugin + `run_onchange` install script (project dotfile-managed tool) — not a scripts/bin producer, skill, or command. |
| 2 | **Landing path** | Install: `home/run_onchange_after_install-tmux-agent-indicator.sh.tmpl`. Load: `home/dot_config/tmux/tmux.conf.tmpl` (plugins block). Segment: `status-right` in the active theme (`home/dot_config/tmux/vercel-theme.conf`). Plugin clone: `~/.tmux/plugins/tmux-agent-indicator` (runtime, not tracked). |
| 3 | **Extend-before-create** | `home/run_onchange_after_install-tmux-which-key.sh.tmpl` (shipped this session) is the exact template — same manual-clone-no-TPM shape. New sibling script (different plugin), reusing its structure verbatim. |
| 4 | **Standalone vs facet** | Standalone plugin. n/a for skill-facet split. |
| 5 | **Scope** | Personal multi-Claude-pane workflow → `if` dotfiles is correct (Leo's env, chezmoi-deployed to all his machines). NOT cc-global. The Claude-hooks half is the only cc-touching piece. |
| 6 | **Tracked medium** | Install script tracked via `git add` (see `git ls-files -s home/run_onchange_after_install-tmux-which-key.sh.tmpl` — the mirror precedent is tracked, mode 100644). Plugin clone runtime-only, correct. |
| 7 | **Gitignore hazard** | `home/run_onchange_*.tmpl` is NOT gitignored (which-key committed cleanly). No `-f` needed. |
| 8 | **Description class** | n/a (not a skill). |
| 9 | **Wiring sites** | `tmux.conf.tmpl` plugins block (run-shell guard, mirror which-key `if-shell` guard) + theme `status-right`. Both stable, `if`-owned surfaces. |
| 10 | **Caller + cadence** | Leo's tmux, every session running Claude panes (high — cmux parallel workflow). |
| 11 | **Fleet propagation** | n/a — single-repo personal dotfiles; chezmoi handles multi-machine. **Open cross-repo item:** Claude hooks land in `~/dev/cc/settings.json` (cc-governed). Decide: (a) wire hooks in cc, (b) rely on process-detection fallback only (no cc change), or (c) scope hooks to a non-cc `CLAUDE_CONFIG_DIR`. STOP-and-ask before any cc settings.json write. |

---

### 2. tmux-claude-usage — SKIP (duplicate)
**Source:** `docker-run/tmux-claude-usage` (MIT, 10★)

**Coverage: FULL — `home/dot_local/bin/executable_tmux-nexus-creds`.** `if` already renders Claude
5h/7d usage in `status-right` of every theme via `#(tmux-nexus-creds)`, and the existing
implementation is **architecturally superior** for Leo's setup:
- **Multi-account** — queries `nexus-agent` (`localhost:7402/credentials`) with an `active_account`
  and per-account 5h/7d utilization; matches Leo's triple-identity setup (BBAdmin/O365/Civalent/
  personal). `tmux-claude-usage` is single-account.
- **Central agent vs shared-cache-clobber** — nexus-agent is one source of truth; `tmux-claude-usage`
  has *every* Claude session write one shared `~/.cache/claude-usage/usage` file (its own
  `statusline.sh` comments admit the stale-replay flicker problem it must guard against).
- Same color-threshold UX (cyan/yellow/red on 50%/80%) already present in `tmux-nexus-creds`.

Adopting it would be a regression. Skip.

### 3. statusLine harvester + `chain.sh` single-slot chaining — MONITOR
**Source:** `docker-run/tmux-claude-usage` `scripts/{statusline.sh,chain.sh}`

The pattern — read Claude's official `rate_limits` from the statusLine stdin JSON (token-free) and
chain it behind an existing statusLine command via a file-stored passthrough — is genuinely novel
and quoting-safe. Not actionable now: nexus-agent already sources this data some other way, so this
is only relevant IF nexus-agent ever needs a token-free on-device usage source or a statusLine-slot
coexistence trick. Revisit then; no caller today.

## Prior Coverage
None found — first recon of both targets (`docs/recon/` was empty this run).
