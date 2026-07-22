## MODIFIED Requirements

### Requirement: MemoryTimelinePane renders one interleaved timeline for the selected queue item
The system SHALL provide `MemoryTimelinePane`, implementing `wavetui-core`'s `Pane` interface and
attached to the existing focus ring, which renders one interleaved, date-grouped timeline for
whichever item is currently selected, merging bead lifecycle events, memory/journal entries, and
openspec archive milestones. Each rendered entry SHALL show its time-of-day when its `Precision`
is `PrecisionTimestamp`, and the acting operator (`Actor`) when non-empty.

#### Scenario: selecting an item populates the timeline pane
- Given: the operator moves the queue selection to an item with bead lifecycle history
- When: the selection changes
- Then: `MemoryTimelinePane` queries all three timeline sources for that item and renders the
  merged result once the query completes

#### Scenario: an item with no history in any lane renders an empty state, not a badge
- Given: a freshly created item with no bead lifecycle events, no memory entries, and no archive
  milestone
- When: it is selected
- Then: `MemoryTimelinePane` renders an empty state, not an "unavailable" badge

#### Scenario: a timestamp-precision entry shows its time-of-day
- Given: an entry's `Precision` is `PrecisionTimestamp`
- When: `MemoryTimelinePane` renders that entry
- Then: its time-of-day is shown alongside the entry text, not just its date group header

#### Scenario: a date-only-precision entry shows no time
- Given: an entry's `Precision` is `PrecisionDateOnly`
- When: `MemoryTimelinePane` renders that entry
- Then: no time-of-day is shown for it — there is none to show

#### Scenario: a bead-sourced entry with a known actor names them
- Given: a `SourceBead` entry whose underlying record carries a non-empty actor
- When: `MemoryTimelinePane` renders that entry
- Then: the actor is shown alongside the entry text

#### Scenario: an entry with no actor shows none
- Given: an entry whose `Actor` field is empty (archive/journal-sourced entries, or a bead
  record with no actor recorded)
- When: `MemoryTimelinePane` renders that entry
- Then: no actor placeholder or blank label is shown
