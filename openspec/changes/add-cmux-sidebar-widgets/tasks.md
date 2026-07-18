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
- [x] [1.2] Define the shared smuggled-field encoding scheme (state token, optional wait-reason, transition epoch, openspec summary, beads summary, usage 5H/7D figures) as a single documented format both the Python writer and the Swift reader consume identically — one canonical reference, not reinvented per field [beads:if-c2ad]
- [x] [1.3] Confirm the exact `cmux workspace-action` CLI param names and `cmux()` action-method name/params for setting a workspace's `description` from both a CLI call (Python) and in-sidebar context, via `cmux docs api` and the `cli-contract.md` raw resource [beads:if-nxb6]

## API Batch

- [ ] [2.1] Extend `apps/cc-tmux/src/cc_tmux/tmux.py`'s hook handlers to dual-write the [1.2] encoding via `cmux workspace-action --description` on every existing state transition, gated on `CMUX_WORKSPACE_ID` being set; fail-open on any cmux-call error, matching the existing hook fail-open invariants [beads:if-3jd4]
  - depends on: 1.2, 1.3
- [x] [2.2] Port `apps/cc-tmux/src/cc_tmux/usage.py`'s `color_for`/`pct_for`/`_extract_util` logic to a standalone JS module; build a static HTML page that fetches `http://localhost:7400/credentials` client-side and renders the full multi-account dashboard (per-account progress bars, reset countdowns, summary header) using that ported logic [beads:if-5oeg]

  Ported `color_for`/`pct_for`/`_extract_util`/`_extract_reset_at`/`_account_label`/
  `dedupe_credentials`/`_freshest_active` faithfully to `scripts/cmux-usage-dashboard/
  usage-logic.js` (ES module), plus new countdown/refill-time formatters
  (`formatCountdown`/`formatRefillTime`, no Python equivalent existed). Built
  `scripts/cmux-usage-dashboard/index.html` — fetches the credentials endpoint client-side,
  dedupes, renders a compact summary chip row + full per-account 5H/7D meter cards, fails
  open to a clean "Usage unavailable" state on any fetch/parse error. Verified live: served
  via `python3 -m http.server`, Playwright headless load showed zero console errors; a
  same-origin fetch to `localhost:7400` (real nexus-agent, reachable on this machine) hit a
  genuine CORS block and the page correctly failed open (no uncaught exception); routing the
  real captured `/credentials` payload (125 raw rows, 3 unique post-dedupe identities) through
  `page.route` confirmed correct rendering — 3 summary chips + 3 account cards with correct
  colors/percentages/active badges, including a past-due reset correctly showing "Resetting…".
- [ ] [2.3] Build a git-tree generator script (`git log --graph --all --format=...` parsed into HTML) that runs wherever it's invoked (local or remote SSH host) and is wired via `cmux browser open` from the workspace's own context [beads:if-f51y]
- [x] [2.4] Build a small periodic writer that populates the [1.2]-encoded openspec-status, beads-status, and usage 5H/7D fields via `cmux workspace-action`/the `cmux()` action confirmed in [1.3] [beads:if-34sn]
  - depends on: 1.2, 1.3

  Reviewed and confirmed correct the partial files left by a crashed prior attempt:
  `scripts/cmux-status-writer.py`, `scripts/lib/cmux_status_encoding.py` (shared encode/decode,
  reused per the [1.2] doc's "implement once" instruction), and
  `home/Library/LaunchAgents/com.leonardoacosta.cmux-status-writer.plist` (`StartInterval=120`,
  `RunAtLoad=true`, matches the existing `StartInterval`-based launchd convention already used by
  `validate-proxy.plist`/`mic-priority.plist` — no residual wiring gap). Confirmed the writer
  correctly reuses `scripts/bin/openspec-status --json --no-enrich` and
  `bd ready --json -n 0` / `bd list --status blocked --json -n 0` (the `-n 0` unlimited-results
  form, avoiding the known `bd ready` default-limit-10 undercounting footgun) rather than
  reinventing either. Usage carrier selection reuses `cc_tmux.usage.active_usage()` directly (no
  reimplementation) and correctly converts its 0..1 float to the encoding's integer-percent string.

  Live-verified on the real Mac (`ssh mac`, cmux 0.64.19) against the disposable test workspace
  `cmux-evolve-test-if-34sn` (workspace:13) left over from the crashed prior attempt: confirmed
  real field names (`current_directory`, `ref`, `remote.enabled/connected/destination`,
  `selected`, `description`) match the writer's assumptions exactly via a live
  `cmux workspace list --json` dump. Simulated a pre-existing state-only description
  (`CC1|waiting|permission|1737158765||||`, standing in for [2.1]'s not-yet-shipped hook
  dual-write), ran the writer targeting only that workspace twice, and confirmed both times the
  `state`/`wait_reason`/`epoch` segments survived byte-for-byte while `openspec`
  (`1 in-progress`) and `beads` (`46 ready, 0 blocked`) populated with real live data — idempotent,
  no corruption. Separately ran the writer against the real selected/carrier workspace
  (`workspace:11`, safe/reversible — my own live session) to confirm carrier-only usage writing
  and correct fail-open empty usage fields when nexus-agent's `/credentials` endpoint was
  unreachable from the Mac. Cleanup verified per the session's safety protocol: closed ONLY
  `workspace:13` via `cmux close-workspace --workspace workspace:13` (never `close-others`),
  confirmed via `cmux workspace list --json` before/after that `workspace:11`
  (main), `workspace:14` (a concurrent agent's own `cmux-evolve-test-git-tree-remote`),
  `workspace:9` (brown), and `workspace:10` (tc) were all unaffected.

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
