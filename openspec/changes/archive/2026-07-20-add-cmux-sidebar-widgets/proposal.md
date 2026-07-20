---
order: 0718b
---

# Proposal: cmux Sidebar Widgets — Claude Session Panel, Git Tree, Usage Meter

## Change ID
`add-cmux-sidebar-widgets`

## Summary
Add three cmux custom-sidebar/browser-panel surfaces built on `/explore` research this session:
a left-sidebar active-Claude-session widget (porting `cc-tmux`'s hook-driven state machine), a
right-sidebar gitkraken-style commit graph for the workspace's CWD (SSH-aware), and a 5H/7D
multi-account usage meter (compact left footer + full right detail), reusing `cc-tmux`'s existing
`usage.py` nexus-agent client.

## Context
- Extends: `apps/cc-tmux/src/cc_tmux/` (hook handlers, `usage.py`), `openspec/specs/cc-tmux/spec.md`
- Related: `openspec/specs/cc-tmux/spec.md` (parent capability — the state machine and usage
  client this proposal reuses), `docs/recon/qeesung-tmux-scout.md` (prior-art comparison,
  confirmed cc-tmux is the first-party answer, no need to re-evaluate)
- touches: `apps/cc-tmux/src/cc_tmux/tmux.py`, `apps/cc-tmux/src/cc_tmux/render.py`, `apps/cc-tmux/src/cc_tmux/usage.py`, `home/dot_config/cmux/sidebars/claude-sessions.swift.tmpl`, `scripts/cmux-usage-dashboard.py`, `scripts/cmux-git-tree.py`, `openspec/specs/cc-tmux/spec.md`, `openspec/specs/cmux-sidebars/spec.md`

## Motivation
cmux's built-in left sidebar shows only a bare workspace list; the right sidebar's built-in modes
(Files/Find/Vault/Feed/Dock) have no git-aware or Claude-session-aware view. `cc-tmux` already
solves the state-tracking problem for tmux; cmux's custom-sidebar mechanism (SwiftUI-style,
`~/.config/cmux/sidebars/*.swift`) and browser-panel mechanism (`cmux browser open`, a real
WebView) give two different, complementary ways to surface the same underlying signals natively
inside cmux instead of needing a separate tmux window at all.

## Requirements

### Requirement: Left sidebar shows live git state for the focused workspace
See `specs/cmux-sidebars/spec.md`.

### Requirement: Left sidebar shows Claude Code session state via a smuggled workspace field
See `specs/cmux-sidebars/spec.md`.

### Requirement: cc-tmux hooks dual-write session state for cmux consumption
See `specs/cc-tmux/spec.md` (MODIFIED — extends the existing hook handlers).

### Requirement: Right sidebar renders a gitkraken-style commit graph, SSH-aware
See `specs/cmux-sidebars/spec.md`.

### Requirement: Usage meter — full detail (right) and compact footer (left)
See `specs/cmux-sidebars/spec.md`.

## Scope
- **IN**: left custom sidebar (git branch/dirty, session title, Claude state icon w/ SF-Symbol
  stand-in, openspec/beads status, compact usage footer); right browser panels (git commit graph,
  full usage dashboard); cc-tmux hook extension to dual-write state into a cmux-readable field.
- **OUT**: the literal Claude SVG logo (blocked — cmux's sidebar interpreter has no
  image-loading support today; ships with an SF-Symbol/shape stand-in instead, tracked as a
  follow-up once cmux ships image support). Replacing or modifying `lazygit`'s pane-3 placement
  in `cmux-workspaces.sh`. Any change to cc-tmux's tmux-side behavior (tmux pane options, the
  fzf inbox, cycling) — this proposal only ADDS a parallel cmux write path.

## Testing
| Affected seam | Unit task | E2E task |
|----------------|-----------|----------|
| cc-tmux hook dual-write (state -> `cmux workspace-action`) | `[2.1]` | `[4.1]` |
| Left sidebar `.swift` file (git fields, smuggled state parsing, pulse animation) | N/A — SwiftUI-interpreter file, no unit test harness exists | `[4.2]` |
| Right git-tree browser panel (SSH-aware) | `[2.3]` | `[4.3]` |
| Right + compact usage meter (nexus-agent client reuse) | `[2.2]` | `[4.4]` |

## Impact
| Area | Change |
|------|--------|
| `apps/cc-tmux/src/cc_tmux/tmux.py` | Hook handlers gain a dual-write call to `cmux workspace-action --description` on every state transition |
| `apps/cc-tmux/src/cc_tmux/usage.py` | Extracted pure helpers (`color_for`, `pct_for`, `_extract_util`) ported to a standalone JS module reused by the usage dashboard |
| `home/dot_config/cmux/` (new, chezmoi-managed) | New custom sidebar `.swift` file + usage/git-tree HTML dashboards |
| `openspec/specs/cc-tmux/` | MODIFIED — new Requirement for the dual-write behavior |
| `openspec/specs/cmux-sidebars/` | ADDED — new capability, first spec under this name |

## Risks
| Risk | Mitigation |
|------|-----------|
| `workspaces.latestMessage`/`.unread`/`.remote` may not populate for a plain-terminal `claude` launch (unverified going in) | First task in DB batch verifies this live before any dependent work starts; downstream tasks scope down if it doesn't |
| Smuggling state into `description` is fragile string-encoding across two independent write/read ends | Single shared encoding scheme defined once (task `[1.2]`) and referenced by both the Python writer and the Swift reader — not reinvented per-field |
| cmux's SwiftUI interpreter is a "growing subset" per its own docs — some modifier/view combo may not render as expected | Prototype task `[3.1]` ships the minimal free-field version first and is manually verified in a real cmux session before the state-dependent rows are added on top |
