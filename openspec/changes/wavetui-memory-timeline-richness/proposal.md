---
order: 0722f
---

# Proposal: wavetui-memory-timeline-richness — surface time-of-day and actor in timeline entries

## Change ID
`wavetui-memory-timeline-richness`

## Summary
Render each timeline entry's time-of-day (not just its date-group header), and add an additive
`Actor` field to `timeline.Entry` populated from `.beads/interactions.jsonl`'s existing `actor`
field, rendered alongside each entry. Closes a data-richness gap found live during a `/explore`
session against a real (if sparse-history) item: `Entry.Time` already carries full clock
precision and `interactionRecord`'s JSON already carries `actor`, but neither reaches the
screen — `renderTimelineEntry` shows only `[source] text (beadID)`.

## Context
- depends on: `wavetui-memory-timeline` (archived — this proposal is additive rendering on top
  of already-shipped timeline merge/query infrastructure, not a new source)
- touches: `apps/wavetui/internal/timeline/types.go`, `apps/wavetui/internal/timeline/beads_history.go`,
  `apps/wavetui/internal/timeline/beads_history_test.go`, `apps/wavetui/internal/ui/memorytimelinepane.go`
- **Found live, not speculative**: from the same `/explore` session (2026-07-22) that produced
  `wavetui-table-detail-polish` (0722c), `wavetui-item-description` (0722d), and
  `wavetui-headless-discoverability` (0722e). Verified directly against real
  `.beads/interactions.jsonl` records for a busier item (`if-tkva`/`if-wfel`) before writing this
  proposal — confirmed `created_at` carries full timestamp precision and `actor` is present on
  every record, not assumed from the screenshot's sparse single-entry example alone.
- **Reuse-not-rebuild (Reader Gate)**: `Entry.Time`/`Precision` already exist and are already
  populated to full clock resolution where the source supports it (`beads_history.go`) — this
  proposal only changes what `renderTimelineEntry` displays, not how `Time` is derived.
  `fieldChangeText`/`activityText`/`commentText` already surface close-reason/field-transition
  text well; this proposal does not touch that logic, only adds the actor.

## Motivation
An operator viewing an item's memory timeline today sees `[bead] claimed (if-1ydm)` with no
indication of when in the day it happened or who did it — both pieces of information already
exist in the underlying `.beads/interactions.jsonl` record. A busier item's timeline (verified
against `if-tkva`'s real history during this exploration) shows the same gap at greater scale:
several same-day entries are indistinguishable from each other without time-of-day, and every
entry is anonymous without an actor.

## Requirements

### Requirement: MemoryTimelinePane renders one interleaved timeline for the selected queue item
See `specs/wavetui/spec.md`.

## Scope
- **IN**: render `Entry.Time`'s time-of-day (e.g. `14:32`) alongside each entry when
  `Precision == PrecisionTimestamp` (an entry precise only to the day, `PrecisionDateOnly`,
  shows no time — it has none to show); additive `Actor string` field on `timeline.Entry`,
  populated from `interactionRecord.Actor` (bead-sourced entries only — archive/journal entries
  have no equivalent actor field in their own source data) and rendered alongside the entry
  text when non-empty.
- **OUT**: any change to `fieldChangeText`/`activityText`/`commentText`'s existing text
  construction (already adequate per this proposal's own verification against real data); an
  actor for archive/journal-sourced entries (no such field exists in `OpenSpecArchiveSource`'s
  git-log-derived data or `MemoryHistorySource`'s journal data — out of scope, not dropped);
  richer match-confidence styling beyond the existing trailing `?` (not verified as a real gap
  during this exploration, unlike time/actor).

## Done Means
- Operator viewing a timestamp-precision entry sees its time-of-day, not just its date group
- Operator viewing a bead-sourced entry sees who performed the action, when `actor` is present
  on the underlying record

## Testing
| Affected seam | Unit task | E2E task |
|----------------|-----------|----------|
| `internal/timeline/types.go` additive `Actor` field | `[1.1]` | `[3.1]` |
| `internal/timeline/beads_history.go` (threads `interactionRecord.Actor` through) | `[1.2]` | `[3.1]` |
| `internal/ui/memorytimelinepane.go` (`renderTimelineEntry` shows time + actor) | N/A — no pure-function render logic beyond Go compile | `[3.1]` (pty runtime verification) |

## Impact
| Area | Change |
|------|--------|
| `apps/wavetui/internal/timeline/types.go` | Additive `Entry.Actor string` field only |
| `apps/wavetui/internal/timeline/beads_history.go` | One-line addition threading `rec.Actor` into the returned `Entry` |
| `apps/wavetui/internal/ui/memorytimelinepane.go` | `renderTimelineEntry` gains a time-of-day prefix (precision-gated) and an actor suffix (when non-empty) |
| `openspec/specs/wavetui/spec.md` | One existing Requirement gets a `## MODIFIED Requirements` delta |
| Existing repo files outside the four `- touches:` paths | None modified |

## Risks
| Risk | Mitigation |
|------|-----------|
| A busy day's date group could get noisier with a time+actor prefix on every line | Verified against real data this proposal is additive text, not a layout change — `MemoryTimelinePane.clipLines`'s existing overflow handling is untouched and still applies |
| None of the four touched files are touched by any other in-flight proposal | Confirmed via `wave-plan-build build --json` at Phase 2.3 of this feature's own authoring |
