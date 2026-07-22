---
stack: t3
---
<!-- beads:feature:if-i8w2 -->

<!-- beads:epic:if-tkva -->

<!-- stack: one of t3 | cc-meta | effect | dotnet — see commands/apply/references/stacks.md § "Stack vocabulary crosswalk" for the full tasks.md-stack:/--stack-profile/detect_stack() mapping -->

# Implementation Tasks

## DB Batch

- [x] [1.1] Add additive `Actor string` field to `timeline.Entry` in `apps/wavetui/internal/ [beads:if-1mtc]
  timeline/types.go`, per this proposal's `specs/wavetui/spec.md` MODIFIED
  "MemoryTimelinePane renders one interleaved timeline..." Requirement — no existing field
  renamed, removed, or re-typed.

## API Batch

- [x] [2.1] Thread `interactionRecord.Actor` into the `Entry` returned by [beads:if-zjhw]
  `apps/wavetui/internal/timeline/beads_history.go`'s conversion function — per spec.md's "a
  bead-sourced entry with a known actor names them" scenario
  - depends on: 1.1

## UI Batch

- [x] [3.1] Update `renderTimelineEntry` in `apps/wavetui/internal/ui/memorytimelinepane.go` [beads:if-qm2j]
  to prefix a time-of-day (only when `Precision == PrecisionTimestamp`) and suffix the actor
  (only when non-empty) — per spec.md's "a timestamp-precision entry shows its time-of-day" /
  "a date-only-precision entry shows no time" / "an entry with no actor shows none" scenarios
  - depends on: 2.1

## E2E Batch

- [x] [4.1] `go test` for `apps/wavetui/internal/timeline` and `apps/wavetui/internal/ui`: [beads:if-hgyi]
  confirm existing tests pass unmodified, plus new coverage for `Actor` round-tripping through
  `beads_history.go`, the time-of-day precision gate, and a runtime pty verification pass
  (build `apps/wavetui/cmd/wavetui`, run against this repo's own `.beads/` history for a busy
  item like `if-tkva`, paste terminal output showing time-of-day and actor both rendering)
  - depends on: 1.1, 2.1, 3.1
