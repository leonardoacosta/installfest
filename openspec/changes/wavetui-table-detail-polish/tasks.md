---
stack: t3
---
<!-- beads:feature:if-tbn5 -->

<!-- beads:epic:if-tkva -->

<!-- stack: one of t3 | cc-meta | effect | dotnet — see commands/apply/references/stacks.md § "Stack vocabulary crosswalk" for the full tasks.md-stack:/--stack-profile/detect_stack() mapping -->

# Implementation Tasks

## DB Batch

- [x] [1.1] Change `Store.Snapshot()`'s item sort in `apps/wavetui/internal/store/store.go` [beads:if-nol8]
  from pure `ID` ascending to `FanOutScore` descending, `CreatedAt` ascending, `ID` ascending
  (final tiebreaker) — per this proposal's `specs/wavetui/spec.md` MODIFIED "Store derives
  normalized queue state..." Requirement's new scenarios.

## UI Batch

- [x] [2.1] Render `🧿`/`📃` for `KindBead`/`KindProposal` in `apps/wavetui/internal/ui/ [beads:if-pzs7]
  queuepane.go`'s Item column, replacing the plain kind string — per spec.md's "a bead renders
  its glyph" / "a proposal renders its glyph" scenarios
  - depends on: 1.1
- [x] [2.2] Merge the `Created` column into the Item column as a leading `MM-dd ` prefix on the [beads:if-dob9]
  title in `queuepane.go`; drop the separate `Created` entry from `queueColumns`
  - depends on: 2.1
  - FIXED (orchestrator, wave 5 post-wave review MUST finding): the old long-form
    `formatCreatedAt` (superseded by `formatCreatedAtShort`) was left behind as dead code —
    zero production call sites, only its own now-orphaned unit test kept it "alive". Removed
    the function, its stale doc-comment references in two other functions' comments, and
    replaced its unit test with an equivalent `TestFormatCreatedAtShort` covering the actual
    live function instead. Full suite re-verified green (13/13 packages).
- [x] [2.3] Add an overflow indicator to `queuepane.go`'s `View()` — when [beads:if-k6zr]
  `len(q.table.Rows()) > q.table.Height()`, render a "N more below" (or equivalent) line — per
  spec.md's "the item count exceeds the visible table height" scenario
  - depends on: 2.2
- [x] [2.4] Remove the unconditional `"\nUnblocked.\n"` line from `apps/wavetui/internal/ui/ [beads:if-kb4q]
  detailpane.go`'s `View()` — render nothing when `Blocker == nil`; the existing
  `Blocked: <type>` branch is unchanged — per spec.md's "an unblocked item's detail pane shows
  no blocker line" scenario
- [x] [2.5] Thread `Root.layout()`'s already-computed `tableHeight` into `DetailPane` (new [beads:if-u48v]
  field + `SetSize`-style setter, mirroring `QueuePane.SetSize`'s existing precedent) so
  `DetailPane`'s bordered box renders at the same height as `QueuePane`'s — per spec.md's "the
  detail pane matches the queue table's height" scenario
  - depends on: 2.4
- [x] [2.6] Change `apps/wavetui/internal/ui/root.go`'s `extraPaneReservedRows` constant from [beads:if-62p5]
  12 to 5 — sized to Sessions/KPI's real ~1-4 line content with slack (see proposal.md's
  Risks table for why a static budget was chosen over dynamic measurement)
- [x] [2.7] Reorder `Root.View()` so `persistentExtras()` (Sessions/KPI/HeadlessBar) renders [beads:if-0w31]
  BEFORE the tab bar/tab content instead of after — they already render regardless of active
  tab (`paneVisible` only gates queue/memory); this changes only their vertical position
  - depends on: 2.6

## E2E Batch

- [x] [3.1] `go test` for `apps/wavetui/internal/store/store.go`: confirm existing `Snapshot()` [beads:if-1n6r]
  tests still pass unmodified, plus new coverage for the FanOutScore-desc/CreatedAt-asc/
  ID-asc ordering (equal-fanout tiebreak, mixed fanout+date cases)
  - depends on: 1.1
- [x] [3.2] Runtime-verify: build `apps/wavetui/cmd/wavetui`, run it in a real pty against a [beads:if-l6n2]
  populated `.beads/`/`openspec/` tree, and paste terminal output as evidence confirming: icons
  render, `MM-dd Title` merged column, no wasted "Unblocked" line, queue table fills the real
  terminal height on launch AND after a resize, detail pane height matches the queue table,
  an overflow indicator appears when items exceed visible height, items ordered by
  FanOutScore-then-date, and Sessions/KPI/HeadlessBar appear above the tab bar
  - depends on: 2.1, 2.2, 2.3, 2.4, 2.5, 2.6, 2.7, 3.1
