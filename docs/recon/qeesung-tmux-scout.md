# Repo Context: qeesung/tmux-scout

> Source: https://github.com/qeesung/tmux-scout
> Context: project (`if` dotfiles)   ·   Stars: 26   ·   Last push: 2026-07-09   ·   License: MIT
> Ask: none — general adoption sweep

## Ask
No rider supplied. Evaluated against the `if` dotfiles repo — specifically the just-shipped
first-party `apps/cc-tmux/` plugin (commits `d3c486f` / `f38008a`, 2026-07-10), which occupies the
same problem space: making parallel AI-agent tmux sessions visible and navigable. This recon's job
is to find what tmux-scout does that cc-tmux does **not**, and whether any of it is worth folding in.

## Purpose
A tmux plugin (Node.js ≥16) that monitors and navigates AI coding-agent sessions across **10 CLIs**
(Claude Code, Codex, Gemini, Kimi, Copilot, OpenCode, Cursor, Hermes, Trae, Traex). Provides an fzf
session picker (`prefix + O`) with status tags + a right-side pane preview, a status-bar count
widget, crash/stale detection, and a background watchdog daemon that reconciles session state.

## Architecture & Key Patterns

**Fundamental architectural split from cc-tmux — this frames every verdict below:**

| Axis | tmux-scout | cc-tmux (local) |
|------|-----------|-----------------|
| State store | External JSON in `~/.tmux-scout/` (`status.json`, per-session files, `access-history.json`) | **tmux pane options** (`@cc-state`) — single source of truth, dies with the pane |
| Freshness | **Watchdog daemon** (single tmux-owned Node process) + single-writer Unix socket bridge (`run/bridge.sock`), 2s/30s/60s hybrid loop | Hook-driven only, no daemon; self-heal on inbox open (process scan) |
| Agent scope | 10 agent CLIs via a generic hook adapter | Claude Code only (first-party by design) |
| Hook install | `setup.sh install` edits each CLI's config; idempotent, backs up + chains existing hooks | `claude plugin install` self-registers via `.claude-plugin/plugin.json` + `hooks/hooks.json` |
| Picker sort | **MRU — most-recently-visited floats to top** via `pane-focus-in[9909]` hook → `access-history.json` | Attention-priority: `waiting > idle > active`, newest-first within group (`priority.py`) |
| Picker UX | fzf popup + right-side `--preview` of last 40 pane lines | fzf popup of aligned `label\tpane_id` rows, **no preview pane** |

Novel implementation details worth citing:
- **Indexed hook slot for idempotent coexistence** (`tmux-scout.tmux`): `set-hook -g 'pane-focus-in[9909]'`
  — a fixed numeric slot overwrites itself on every config reload and sits *alongside* a user's own
  `pane-focus-in` hook rather than clobbering it. Clean reusable idiom.
- **PATH capture at load** (`tmux set-environment -g SCOUT_PATH "$PATH"`): saves the interactive
  `$PATH` so `run-shell` (which has a minimal env) can still find `node` under nvm/fnm/homebrew.
- **Single-writer socket bridge**: agent hooks prefer writing status to a Unix socket so one process
  serializes writes; atomic file-write fallback when the socket is down.
- **`doctor` subcommand**: environment diagnostics (tmux ≥3.2, fzf `--listen`/`--tmux` support, node,
  hook-install status) — distinct from a unit-test runner.

## Findings

0 Steal · 2 Adapt · 1 Monitor · 4 Skip. No harness proposed (over-adoption guard satisfied); both
Adapts extend an existing `apps/cc-tmux/` file with a named integration point.

---

### ADAPT 1 — fzf `--preview` pane in the inbox/picker popup
**Source:** README § Usage "Pane preview — right-side preview panel shows the last 40 lines of each
session's tmux pane".

**Coverage: PARTIAL — `apps/cc-tmux/src/cc_tmux/cli.py` (`cmd_inbox`/`cmd_picker_data`) + the fzf
popup wiring in `apps/cc-tmux/cc-tmux.tmux` own this domain.** cc-tmux emits aligned `label\tpane_id`
rows (`cli.py:228`) but the fzf popup renders them flat — no preview. (searched:
`rg -n 'preview|capture-pane' apps/cc-tmux/src/cc_tmux/*.py` → only render-width helpers, zero preview.)

- **Before:** inbox popup lists panes as text rows; the user hops blind, then reads the pane.
- **After:** fzf popup gains `--preview 'tmux capture-pane -ep -t {N}'` keyed on the pane_id column,
  showing the last ~40 lines of the highlighted session live in the right panel.
- **Gain:** pick the right pane without hopping first — the single biggest UX lift tmux-scout has over
  a flat list.
- **Effort:** small (one fzf flag + ensure pane_id is a stable preview key; column order may need a tweak).

**Placement Verdict**
| # | Row | Verdict |
|---|-----|---------|
| 1 | Layer | Command/entrypoint wiring (tmux keybinding → fzf popup), backed by the existing `cli.py` data producer |
| 2 | Landing path | `apps/cc-tmux/cc-tmux.tmux` (fzf popup invocation for the inbox key) + `apps/cc-tmux/src/cc_tmux/cli.py` if the pane_id column must move to a `--preview`-addressable field |
| 3 | Extend-before-create | `cmd_inbox` already produces the rows and `cc-tmux.tmux` already owns the fzf popup — extend both; a new "preview" subcommand is unwarranted |
| 4 | Standalone vs facet | Facet of the existing inbox — no new module |
| 5 | Scope | Project-local only. cc/fleet would not want a personal tmux plugin feature — n/a global |
| 6 | Tracked medium | `git ls-files -s` → `100755 …/cc-tmux.tmux`, `100644 …/cli.py` (both tracked real files) |
| 7 | Gitignore hazard | None — edits to already-tracked files |
| 8 | Description class | n/a (not a skill) — caller is the tmux inbox keybinding |
| 9 | Wiring sites | Already wired: the inbox key in `cc-tmux.tmux`; no new pointer needed |
| 10 | Caller + cadence | `prefix`-key inbox popup, invoked interactively every session |
| 11 | Fleet propagation | n/a — single-repo dotfiles |

---

### ADAPT 2 — `cc-tmux doctor` environment-diagnostics subcommand
**Source:** README § Hook Setup `setup.sh doctor` + `scripts/doctor.js` (9.7 KB); `status --any --quiet`
preflight in `tmux-scout.tmux`.

**Coverage: PARTIAL — `apps/cc-tmux/src/cc_tmux/parser.py` + `cli.py` own the subcommand surface;
`self-test` (`cli.py:134`, `testing.py`) is the sibling but tests *pure functions*, not the
environment.** cc-tmux fails open silently on a missing dep — good for hooks, but it leaves the user
with no way to ask "why isn't the picker working?" (searched: `rg -n 'doctor|diagnos|tmux -V|which fzf'
apps/cc-tmux/src/cc_tmux/*.py` → only `self-test`, no environment probe.)

- **Before:** a missing fzf / unregistered hook / old tmux fails silently; user has no diagnostic.
- **After:** `cc-tmux doctor` prints a checklist — tmux ≥3.2, fzf present, `$TMUX` set, plugin
  installed, Claude hooks registered — each PASS/FAIL, exit 0 (fail-open like every other subcommand).
- **Gain:** turns silent degradation into a one-command self-diagnosis; also a natural post-install
  verification hook for the `run_onchange` installer.
- **Effort:** small-medium (new `cmd_doctor` + a handful of `shutil.which` / `tmux -V` / pane-option probes).

**Placement Verdict**
| # | Row | Verdict |
|---|-----|---------|
| 1 | Layer | Agent/CLI execution subcommand (a `cmd_<name>` handler), stdlib-only |
| 2 | Landing path | `apps/cc-tmux/src/cc_tmux/cli.py` (`cmd_doctor`) + `apps/cc-tmux/src/cc_tmux/parser.py` (`add_parser("doctor")`) |
| 3 | Extend-before-create | Extends the existing argparse subcommand registry; `self-test` cannot absorb it (pure-function scope, no env I/O) |
| 4 | Standalone vs facet | Facet — one handler in the existing `cli.py`, no new module |
| 5 | Scope | Project-local. The *pattern* (env doctor over silent fail-open) is generic, but the impl is cc-tmux-specific — n/a global |
| 6 | Tracked medium | `git ls-files -s` → `100644 …/cli.py`, `100644 …/parser.py` (tracked) |
| 7 | Gitignore hazard | None — edits to tracked files |
| 8 | Description class | n/a (not a skill) — caller is the user + the install script |
| 9 | Wiring sites | Optionally called at the tail of `home/run_onchange_after_install-cc-tmux.sh.tmpl` as an install-time self-check |
| 10 | Caller + cadence | Manual (`cc-tmux doctor` when the picker misbehaves) + once per `chezmoi apply` if wired into install |
| 11 | Fleet propagation | n/a — single-repo dotfiles |

---

### MONITOR 1 — MRU (visit-recency) picker ordering + `pane-focus-in[9909]` idempotent hook slot
tmux-scout floats the *most-recently-visited* session to the top of the picker, tracked by a
`pane-focus-in[9909]` hook writing `~/.tmux-scout/access-history.json`. cc-tmux sorts strictly by
attention-priority (`waiting > idle > active`, `priority.py`). **Coverage: NONE** for visit-recency
(searched: `rg -n 'focus-in|recenc|MRU|access|last-visit' apps/cc-tmux` → nothing). Held at Monitor,
not Adapt, because it changes cc-tmux's core sort philosophy (urgency-first vs recency-first) and has
no caller demanding it — adopting it would need a design decision (recency as tiebreak? alt sort
mode?), which is a `/feature`-sized call, not a drop-in. **Worth remembering regardless:** the
fixed-index hook slot (`set-hook -g 'pane-focus-in[9909]'`) is a clean idiom for installing a tmux
hook idempotently *without* clobbering the user's own `pane-focus-in` hook — reusable if cc-tmux ever
needs its own pane-focus hook.

### SKIP 1 — watchdog daemon + single-writer Unix socket bridge
**Coverage: FULL by deliberate architectural choice.** cc-tmux's spec makes tmux pane options the
single source of truth with **no external state file and no daemon** (`openspec/specs/cc-tmux/spec.md`
Req "state dies with the pane"; README "stdlib only, no runtime dependencies"). Confirmed absent by
design: `rg -n 'socket|daemon|watchdog|\.sock' apps/cc-tmux/src/cc_tmux/*.py` → NONE. A watchdog +
socket bridge directly contradicts that invariant — this is an architecture divergence, not a gap.

### SKIP 2 — multi-agent support (Codex/Gemini/Kimi/Copilot/OpenCode/Cursor/Hermes/Trae/Traex)
Out of scope. cc-tmux is a "first-party **Claude Code** + tmux plugin" by design (README line 3). The
10-CLI generic hook adapter solves a problem cc-tmux intentionally doesn't have.

### SKIP 3 — idempotent multi-CLI hook installer (`setup.sh install/uninstall/status`, backup+chain)
**Coverage: FULL via a different mechanism.** cc-tmux self-registers hooks through the Claude Code
plugin manifest (`apps/cc-tmux/.claude-plugin/plugin.json` + `apps/cc-tmux/hooks/hooks.json`), so
`claude plugin install` handles registration with zero `settings.json` editing. tmux-scout's installer
(config-file surgery + backup/chain across 10 CLIs) solves a problem the manifest approach designed away.

### SKIP 4 — configurable status-format placeholder DSL (`@scout-status-format '{W}/{B}/{D}'`)
cc-tmux `status` already emits per-state counts with configurable per-state styling
(`@cc-status-inbox-<state>-style`, `cli.py:41`). A full format-string mini-DSL is over-engineering for
a single-user plugin — low value, no caller.

## Prior Coverage
No prior recon mentions tmux-scout (`rg -il 'tmux-scout|qeesung' docs/recon/` → none). Adjacent
targets already recon'd this session-space and shaped the cc-tmux build:
- `docs/recon/tmux-claude-plugins.md` — tmux-agent-indicator (Adapt) + tmux-claude-usage (Skip).
- `docs/recon/unsafe9-claude-tmux-hop.md` — the architecture cc-tmux was clean-room adapted from.

tmux-scout is a **fresh** target; no liveness refresh applies.
