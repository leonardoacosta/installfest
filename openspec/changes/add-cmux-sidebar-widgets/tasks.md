---
stack: t3
---
<!-- beads:epic:if-bqw -->
<!-- beads:feature:if-g2mg -->

# Implementation Tasks

## DB Batch

- [x] [1.1] Verify live whether `workspaces.latestMessage`/`.unread`/`.remote` populate for a plain-terminal `claude` launch (our current `cmux-workspaces.sh` architecture) vs. only cmux's native agent-session surface type — SSH into the Mac, create a disposable test workspace, launch `claude` as a plain pane command, inspect `cmux workspace list --json` for those fields, record the finding [beads:if-tuox]

  **Finding (cmux 0.64.19, live-verified 2026-07-18)**: they do NOT populate. Created disposable
  workspace `cmux ssh homelab --name cmux-evolve-test-if-tuox`, launched `claude` via `cmux send`
  (same plain-pane-injection pattern as `cmux-workspaces.sh`'s `pane_exec`), confirmed via
  `capture-pane`/`read-screen` that Claude actually ran and answered a prompt ("Paris"), then
  re-queried `cmux workspace list --json`. `latest_conversation_message`, `latest_submitted_message`,
  and `latest_submitted_at` all stayed `null` throughout — before, during, and after the exchange.
  Note the real field names are snake_case (`latest_conversation_message` /
  `latest_submitted_message` / `latest_submitted_at`), not `latestMessage`; there is no `unread`
  field anywhere in the schema (verified via a full-payload grep across `workspace list --json`,
  zero matches). By contrast, pre-existing workspaces opened through cmux's own native
  agent-session mechanism (workspace:3/41/2 in the live list) show these fields populated with real
  conversation summaries and timestamps. Conclusion: these fields are sourced from cmux's native
  agent-session surface type (`new-surface --type agent-session --provider claude`), not from a
  plain terminal pane running the `claude` binary via shell injection — our `cmux-workspaces.sh`
  architecture will never see them populate no matter how long a plain-pane `claude` session runs.
  `remote.*` fields (`.connected`, `.state`, etc.) are unaffected — confirmed pre-existing and
  populated for both surface types, as expected. Cleanup verified: `cmux close-workspace` + a bare
  `cmux list-workspaces` confirming `workspace:52` no longer listed.
- [ ] [1.2] Define the shared smuggled-field encoding scheme (state token, optional wait-reason, transition epoch, openspec summary, beads summary, usage 5H/7D figures) as a single documented format both the Python writer and the Swift reader consume identically — one canonical reference, not reinvented per field [beads:if-c2ad]
- [x] [1.3] Confirm the exact `cmux workspace-action` CLI param names and `cmux()` action-method name/params for setting a workspace's `description` from both a CLI call (Python) and in-sidebar context, via `cmux docs api` and the `cli-contract.md` raw resource [beads:if-nxb6]

## API Batch

- [ ] [2.1] Extend `apps/cc-tmux/src/cc_tmux/tmux.py`'s hook handlers to dual-write the [1.2] encoding via `cmux workspace-action --description` on every existing state transition, gated on `CMUX_WORKSPACE_ID` being set; fail-open on any cmux-call error, matching the existing hook fail-open invariants [beads:if-3jd4]
  - depends on: 1.2, 1.3
- [ ] [2.2] Port `apps/cc-tmux/src/cc_tmux/usage.py`'s `color_for`/`pct_for`/`_extract_util` logic to a standalone JS module; build a static HTML page that fetches `http://localhost:7400/credentials` client-side and renders the full multi-account dashboard (per-account progress bars, reset countdowns, summary header) using that ported logic [beads:if-5oeg]
- [ ] [2.3] Build a git-tree generator script (`git log --graph --all --format=...` parsed into HTML) that runs wherever it's invoked (local or remote SSH host) and is wired via `cmux browser open` from the workspace's own context [beads:if-f51y]
- [ ] [2.4] Build a small periodic writer that populates the [1.2]-encoded openspec-status, beads-status, and usage 5H/7D fields via `cmux workspace-action`/the `cmux()` action confirmed in [1.3] [beads:if-34sn]
  - depends on: 1.2, 1.3

## UI Batch

- [ ] [3.1] Prototype a minimal left custom sidebar (`home/dot_config/cmux/sidebars/claude-sessions.swift.tmpl`) using only cmux's natively-free fields: `title`/`directory` (truncated to 5 chars) + session name, `branch`, `dirty` — verify it renders correctly in a real cmux session before adding smuggled-field rows on top [beads:if-boqe]
- [ ] [3.2] Extend the sidebar with the Claude-state indicator: SF-Symbol/shape stand-in for the Claude mark, solid when `idle`, pulsating opacity when `active` (alternating on `clock` wall-clock parity), pulsing red when `waiting` with reason `permission`; parses the [1.2] encoding from `description` [beads:if-n2oq]
  - depends on: 2.1, 3.1
- [ ] [3.3] Extend the sidebar with the openspec-status + beads-status row and the compact usage-meter footer (CYAN/YELLOW/RED thresholds ported from `usage.py`), both sourced from the [2.4] writer's smuggled fields [beads:if-6to1]
  - depends on: 2.4, 3.1
- [ ] [3.4] Wire the sidebar file into chezmoi deployment (`home/dot_config/cmux/sidebars/claude-sessions.swift.tmpl`) so it deploys to `~/.config/cmux/sidebars/` on `chezmoi apply` [beads:if-wut0]

## E2E Batch

- [ ] [4.1] Live-verify cc-tmux's dual-write: trigger a real state transition (prompt submit, permission prompt, stop) inside a cmux workspace, confirm `cmux workspace-action` fires and the workspace's `description` updates with the correct encoding [beads:if-35jf]
  - depends on: 2.1
- [ ] [4.2] Live-verify the full left sidebar in a real cmux session: git state, project/session name, Claude-state icon animation (all three modes), openspec/beads segments, usage footer — against a real disposable test workspace, cleaned up after [beads:if-g4u2]
  - depends on: 3.2, 3.3, 3.4
- [ ] [4.3] Live-verify the git-tree panel against both a local workspace and an SSH-backed workspace (homelab), confirming the SSH case renders the REMOTE repository's graph, not a stale local one [beads:if-tf9q]
  - depends on: 2.3
- [ ] [4.4] Live-verify the usage dashboard panel against real nexus-agent data (or its unreachable-degradation path) [beads:if-kg51]
  - depends on: 2.2
