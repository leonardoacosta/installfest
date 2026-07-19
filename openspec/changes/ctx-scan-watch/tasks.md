---
stack: t3
---
<!-- beads:epic:if-wfel -->
<!-- beads:feature:if-ci2x -->

# Implementation Tasks

## DB Batch

- [x] [1.1] Define the snapshot record shape in `src/history.ts`: `{timestamp, project, [beads:if-4c6b]
  scanOutput, auditOutput}`, one JSON line per snapshot in `~/.ctx-scan/history.jsonl`
  (append-only).

## API Batch

- [x] [2.1] Implement `src/watch.ts`: chokidar watcher over the discovered fleet's project [beads:if-1rj9]
  roots, debounced re-scan of the changed project on file-change, invoking `ctx-scan scan` +
  `ctx-scan audit` for that project only.
  - depends on: 1.1
- [x] [2.2] Implement snapshot append in `src/history.ts`: write one new line to [beads:if-pg2a]
  `~/.ctx-scan/history.jsonl` per re-scan triggered by `[2.1]`.
  - depends on: 2.1
- [x] [2.3] Implement `src/diff.ts`: load two named snapshots (by timestamp or index), compare [beads:if-4g67]
  every rubric row's `band` value between them, and produce a list of `{rule, from, to}`
  transitions — empty when nothing changed.
  - depends on: 1.1

## UI Batch

- [x] [3.1] Wire `ctx-scan watch` and `ctx-scan diff <a> <b>` as top-level commander.js commands. [beads:if-w7xv]
  - depends on: 2.1, 2.3
- [x] [3.2] Extend `ctx-scan render --fleet`'s leaderboard (from `ctx-scan-render`) with a [beads:if-mqes]
  per-project sparkline summarizing recent `history.jsonl` entries, rendered only when at least
  two snapshots exist for that project.
  - depends on: 2.2

## E2E Batch

- [x] [4.1] Fixture project edit under an active `watch` process; assert exactly one new [beads:if-m642]
  `history.jsonl` line is appended for that project.
  - depends on: 2.2
- [x] [4.2] Two fixture snapshots with one seeded band transition (e.g. A4 GREEN → RED); assert [beads:if-8kfk]
  `diff` reports exactly that transition and nothing else.
  - depends on: 2.3
- [x] [4.3] Real-time test: edit a real CLAUDE.md while `ctx-scan watch` is running; assert a new [beads:if-o5ot]
  snapshot appears in `history.jsonl` within 5 seconds.
  - depends on: 3.1
- [x] [4.4] Run `ctx-scan diff` on the two fixtures from `[4.2]` via the CLI command; assert the [beads:if-w2z4]
  printed output matches the seeded transition exactly.
  - depends on: 3.1
- [x] [4.5] Multi-snapshot fixture history (3+ snapshots for one project with varying bands); [beads:if-l1ej]
  assert the fleet sparkline renders without error and reflects the snapshot count.
  - depends on: 3.2
- [x] [4.6] `tsc --noEmit` and `bun test` both green. [beads:if-vhp0]
  - depends on: 3.1, 3.2
