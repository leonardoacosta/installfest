# Implementation Plans (retired)

This directory held numbered `NNN-*.md` implementation plans (001-006, all shipped
2026-07-02 through 2026-07-03). The convention is retired as of 2026-07-18: new work
goes through `bd` (tracked follow-ups) or an OpenSpec proposal (`openspec/changes/`)
instead of a standalone plan doc here.

Historical plan bodies are recoverable via `git log -- docs/plans/` (they were removed
in the same commit that added this note). The plans' Backlog + Rejected findings were
reconciled 2026-07-18 and dispositioned as follows — already resolved (no bead needed),
or minted as beads under `if-vit` (workspace-resilience) / `if-7cce` (unsorted):

| Finding | Disposition |
|---|---|
| `az` wrapper word-splitting | resolved |
| stale `.notified-epoch` | resolved |
| dead `scripts/cmux-debug.sh` | resolved (deleted) |
| `*-proxied` wrapper trio | resolved (deleted) |
| Upstash API key on argv | resolved |
| Rust `cmux-bridge` zero-caller question | resolved (`if-j8g`, closed — it's live, deployed via LaunchAgent) |
| TOTP via `nx_notify` routing | correctly rejected (handled in its own openspec change) |
| mesh-heartbeat JSONL rotation | correctly rejected (nova/nx own rendering) |
| `paste-image.sh` size | correctly rejected (investigate-only) |
| 2026-03 `docs/audit/` CRITICALs | resolved, historical |
| root `.env` presence | verified correct by design |
| mx-broker ADO-PAT retirement | functionally done; only a doc status line was stale |
| scheduler installer bricks bun-less Linux | `if-vit.1` |
| strict-mode/repo-root convention drift | `if-7cce.1` |
| doc drift bundle (CLAUDE.md dirs, undocumented tools, stale mx-broker doc status) | `if-7cce.2` |
| entry-point sprawl (no index, missing `--help`) | `if-7cce.3` |
| generator prune pass (raycast/cmux-workspaces orphans) | `if-vit.2` (feature-sized — run `/feature` when picked up) |
| nexus/nova naming drift | `if-7cce.4` (`[user]` — needs Leo's live-state verification) |
| CloudPC bootstrap fix-or-retire | `if-7cce.5` (`[user]` — direction call) |
| `wk open <code>` launcher verb | resolved — `wk` retired 2026-07-18, folded into `mux` (`mux doctor`/`mux ready`); `if-vit.3` closed, no separate verb added |
| registry convergence (cc `projects.json` vs if `projects.toml`) | `if-7cce.6` (`[user]` — cross-repo decision) |
