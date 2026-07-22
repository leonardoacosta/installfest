---
order: 0722c
---

# Proposal: wavetui-table-detail-polish — icons, layout, sort order, and a real height budget

## Change ID
`wavetui-table-detail-polish`

## Summary
A visual/layout pass on `QueuePane`/`DetailPane`/`Root`, sourced from a live `/explore` session
against operator screenshot feedback and verified directly against source (not guessed): bead/
proposal icons, a merged `MM-dd Title` item column, a conditionally-rendered blocker line, a
real fix for the queue table not using its full available height, a matching-height detail
pane, an overflow indicator, sorting items by blocking weight before date, and moving Sessions/
KPI/HeadlessBar above the tab bar so they're visible without switching tabs.

## Context
- depends on: none
- touches: `apps/wavetui/internal/ui/queuepane.go`, `apps/wavetui/internal/ui/detailpane.go`,
  `apps/wavetui/internal/ui/root.go`, `apps/wavetui/internal/store/store.go`,
  `apps/wavetui/internal/store/store_test.go`
- **Overlap (wave-level, not logical)**: in-flight `wavetui-context-pane` (order 0722b) also
  touches `apps/wavetui/internal/ui/root.go` — it adds a third `[3] Context` tab mirroring this
  session's own `r.memory` tab-wiring pattern. Disjoint changes (this proposal reorders/resizes
  existing tab content and the persistent-extras strip; that proposal appends a new tab) — no
  hard conflict, declared so the wave conflict matrix serializes them rather than landing as a
  silent merge collision.
- **Found live, not speculative**: this proposal originates from a `/explore` session
  (2026-07-22) against two real operator screenshots of the running TUI, immediately following
  the same session's `/frontend-design` pass that shipped the tab bar / width cap / mouse
  support these findings build on. Every finding below was verified against actual source
  (`store.Item`'s real field set, `layout()`'s real height math, `table.Model`'s real exported
  API), not inferred from the screenshots alone.
- **Reuse-not-rebuild (Reader Gate)**: item ordering reuses `Item.FanOutScore` (already means
  "count of transitive dependents this item unblocks" — exactly what "blocking order" asks for)
  instead of computing a new dependency-weight metric. The overflow indicator reuses
  `table.Model`'s existing `Rows()`/`Height()` accessors — no new bubbles/v2 API needed. The
  height-budget fix (see Risks) is a constant change, not new sizing machinery.
- Capability Preflight: not applicable — local dev tool, no hosting/deploy component, same
  precedent every prior wavetui proposal cites (`stack: t3`-as-placeholder, `rules/PATTERNS.md`).

## Motivation
The redesign that shipped tabs, a width cap, and mouse support in the same session this
exploration follows made several pre-existing issues newly visible (a cramped, unreadable
table on a real terminal) and left a few new ones as a direct side effect (the persistent-extras
strip's flat 12-row-per-pane reservation, which existed before tabs but now starves the queue
table's height on every run). An operator using the TUI daily needs the table to actually use
the screen, icons/dates that don't force horizontal scanning across five columns, and item
ordering that surfaces what's actually blocking the most work first.

## Requirements

### Requirement: QueuePane renders the live queue with blocker badges and fan-out score
See `specs/wavetui/spec.md`.

### Requirement: DetailPane renders full detail for the selected queue row
See `specs/wavetui/spec.md`.

### Requirement: Store derives normalized queue state by re-querying CLIs, never by inferring from which file changed
See `specs/wavetui/spec.md`.

## Scope
- **IN**:
  - `queuepane.go`: render `🧿`/`📃` for `KindBead`/`KindProposal` in place of the plain kind
    string; merge `Created` into the `Item` column as a leading `MM-dd ` prefix, dropping the
    separate `Created` column; an overflow indicator ("N more below") when `len(table.Rows()) >
    table.Height()`.
  - `detailpane.go`: remove the unconditional `"\nUnblocked.\n"` line — render nothing when
    `Blocker == nil`, keep the existing `Blocked: <type>` branch unchanged; match `DetailPane`'s
    rendered height to `QueuePane`'s real table height (both already computed in `layout()`).
  - `root.go`: fix `extraPaneReservedRows` (12 → 5, sized to Sessions/KPI's real ~1-4 line
    content with slack — see Risks for why a static budget over dynamic measurement); reorder
    `View()` so the persistent extras (Sessions/KPI/HeadlessBar) render ABOVE the tab bar instead
    of below, so they're visible regardless of active tab without scrolling past tab content.
  - `store.go`: change `Snapshot()`'s sort from pure `ID` ascending to `FanOutScore` descending,
    `CreatedAt` ascending as tiebreaker, `ID` ascending as final tiebreaker (deterministic
    ordering when both are equal, matching this file's existing tie-break-by-ID convention for
    `Errors`).
- **OUT**: item description rendering (no `Description` field exists on `store.Item` today —
  real feature, its own proposal); headless-dispatch discoverability (needs a UX design
  decision first, its own proposal); memory timeline data richness (its own proposal); a new
  `wavetui-context-pane`-style tab (that proposal's own scope, not this one's).

## Done Means
- Operator sees `🧿`/`📃` instead of a plain "bead"/"proposal" type string in the queue
- Operator sees `MM-dd Title` in the Item column, with no separate Created column
- Operator sees the "Unblocked" line disappear entirely for an unblocked item — it never
  occupies a wasted line
- Operator sees the queue table fill the real available terminal height on a fresh launch AND
  correctly recalculate after a live terminal resize
- Operator sees the detail pane's bordered box match the queue table's height exactly
- Operator sees an explicit overflow indicator whenever the item count exceeds the visible
  table height
- Operator sees items ordered by `FanOutScore` descending (most-blocking first), then by
  `CreatedAt` ascending
- Operator sees Sessions/KPI/HeadlessBar rendered above the Items/Memories tab bar, visible
  regardless of which tab is active

## Testing
| Affected seam | Unit task | E2E task |
|----------------|-----------|----------|
| `internal/store/store.go` (`Snapshot()` sort order) | `[1.1]` | `[4.1]` |
| `internal/ui/queuepane.go` (icons, date-merged column, overflow indicator) | N/A — no pure-function render logic beyond Go compile | `[4.2]` (pty runtime verification) |
| `internal/ui/detailpane.go` (conditional blocker line, height match) | N/A | `[4.2]` |
| `internal/ui/root.go` (`extraPaneReservedRows`, persistent-extras reorder) | N/A | `[4.2]` |

## Impact
| Area | Change |
|------|--------|
| `apps/wavetui/internal/store/store.go` | `Snapshot()`'s sort comparator changes from ID-only to FanOutScore/CreatedAt/ID — existing `Errors` sort untouched |
| `apps/wavetui/internal/ui/queuepane.go` | `queueColumns` drops the `Created` column; `renderTitleCell`/row-building gains icon + date-prefix; a new overflow-indicator render path |
| `apps/wavetui/internal/ui/detailpane.go` | `View()` drops the unconditional Unblocked branch; gains a height parameter/field threaded from `layout()` |
| `apps/wavetui/internal/ui/root.go` | `extraPaneReservedRows` constant changes 12→5; `View()`'s render order changes (persistent extras before tab bar) |
| `openspec/specs/wavetui/spec.md` | Three existing Requirements get `## MODIFIED Requirements` deltas (full text pasted, new/changed scenarios) |
| Existing repo files outside the five `- touches:` paths | None modified |

## Risks
| Risk | Mitigation |
|------|-----------|
| A smaller static `extraPaneReservedRows` could still overflow if Sessions ever renders more than ~4 lines (e.g. several linked sessions at once) | 5 rows covers the realistic common case with slack (empty=1 line, KPI=1 line, a couple of session rows=3-4); a genuinely large session count degrading gracefully (clipping, not corruption) is the same class of problem `MemoryTimelinePane.clipLines` already solves elsewhere in this package — reuse that pattern in a follow-up if real usage ever needs it, not preemptively here |
| Sort-order change is a behavior change existing operators may not expect | Called out explicitly in Done Means and Motivation; `CreatedAt`/`ID` tiebreakers keep it deterministic, matching this file's existing tie-break convention |
| None of the five touched files are touched by any other in-flight proposal except the declared root.go overlap with `wavetui-context-pane` | Confirmed via `wave-plan-build build --json` at Phase 2.3 of this feature's own authoring |
