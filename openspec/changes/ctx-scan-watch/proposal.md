---
order: 0718g
---

# Proposal: ctx-scan Snapshot History and Drift Diff

## Change ID
`ctx-scan-watch`

## Summary
`ctx-scan watch` re-scans changed projects on file-change and appends timestamped snapshots to
`~/.ctx-scan/history.jsonl`; `ctx-scan diff <a> <b>` compares two snapshots and prints exactly
which rubric bands transitioned — the "what regressed this week" answer. The watcher is the
feeder; the snapshot history and diff are the actual deliverable.

## Context
- Extends: invokes `ctx-scan scan` + `ctx-scan audit` (both from earlier proposals) on a
  change-triggered cadence; no changes to their source.
- depends on: `ctx-scan-refs`
- touches: `apps/ctx-scan/src/watch.ts`, `apps/ctx-scan/src/diff.ts`,
  `apps/ctx-scan/src/history.ts`, `apps/ctx-scan/test/fixtures/watch/**`

## Motivation
Every prior proposal answers "what is the state right now." Nothing yet answers "did this get
worse." A snapshot history with a diff command is the minimal shape that does — cheaper and more
reliable than a long-running daemon, and the roadmap itself flags the daemon as the fragile part
("consider a scheduled task instead of a daemon if watch proves fragile"), so this proposal
treats `watch` as a feeder into `history.jsonl`, with the file itself (plus `diff`) as the real,
durable deliverable that survives even if the watcher is later swapped for a scheduled task.

## Requirements

### Requirement: Timestamped snapshot history
`ctx-scan watch` SHALL re-scan a changed project on file-change (via chokidar) and append a
timestamped snapshot — the full `ctx-scan scan` + `ctx-scan audit` output for that project — to
`~/.ctx-scan/history.jsonl`.

### Requirement: Fleet drift sparklines
The fleet view (`ctx-scan render --fleet`) SHALL gain a per-project sparkline summarizing recent
snapshot history, when `~/.ctx-scan/history.jsonl` has at least two snapshots for that project.

### Requirement: Snapshot diff
`ctx-scan diff <snapshot-a> <snapshot-b>` SHALL compare two snapshots and print every rubric band
that transitioned between them (e.g. `A4: GREEN → RED`), and nothing for bands that did not
change.

## Scope
- **IN**: the chokidar-based watcher, append-only snapshot history, the fleet sparkline, the
  `diff` command's band-transition report.
- **OUT**: any daemon/service-management concern beyond a foreground `watch` process; replacing
  `watch` with a scheduled task is an explicit future option, not built here — this proposal
  ships the feeder in its simplest working form and the durable diff/history layer that survives
  either feeder choice.

## Testing
| Affected seam | Unit task | E2E task |
|----------------|-----------|----------|
| Snapshot append | `[4.1]` fixture project edit, assert one new `history.jsonl` line | `[4.3]` editing a real CLAUDE.md while watching produces a new snapshot within 5s |
| Diff band-transition report | `[4.2]` two fixture snapshots with a seeded band transition | `[4.4]` `diff` on the two fixtures reports exactly the seeded transition, nothing else |
| Fleet sparkline | `[4.5]` multi-snapshot fixture history | N/A |

## Impact
| Area | Change |
|------|--------|
| `apps/ctx-scan/src/watch.ts` | New — chokidar-based re-scan trigger |
| `apps/ctx-scan/src/history.ts` | New — append-only `~/.ctx-scan/history.jsonl` writer |
| `apps/ctx-scan/src/diff.ts` | New — band-transition comparator |
| `apps/ctx-scan/src/render/level0-fleet.ts` | Gains sparkline rendering when history exists (from `ctx-scan-render`) |

## Risks
| Risk | Mitigation |
|------|-----------|
| `history.jsonl` grows unbounded | Flagged as a known tradeoff, not solved defensively here — append-only is the simplest correct behavior; rotation/pruning is a follow-up if the file becomes a real problem, not a speculative feature now |
| chokidar watcher proves fragile (per the roadmap's own caveat) | Scope explicitly separates the watcher (replaceable feeder) from `history.jsonl`/`diff` (the durable deliverable) so a future swap to a scheduled task needs no format change |
| Diff reports noise from unrelated measurement jitter (e.g. token-estimate rounding) | Diff compares `band` transitions only, not raw measured values — a same-band fluctuation produces no diff output by construction |
