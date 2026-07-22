## MODIFIED Requirements

### Requirement: Store derives normalized queue state by re-querying CLIs, never by inferring from which file changed
The Store SHALL be the single writer of derived queue state (items, dep graph, fan-out score,
staleness clocks). It MUST NOT infer item-level meaning from which file path changed; it acts on
a "something in this source changed" signal and always resolves current truth by re-querying the
source's CLI. `Snapshot()`'s returned `Items` slice MUST be ordered by `FanOutScore` descending
(most transitively-blocking first), then `CreatedAt` ascending, then `ID` ascending as a final
deterministic tiebreaker.

#### Scenario: a .beads/id.db-wal write triggers a full re-query, not a partial diff
- Given: a single bead is updated
- When: `.beads/id.db-wal` changes and the debounce window elapses
- Then: the Store re-queries `bd list --json` and `bd ready --json` in full rather than
  attempting to infer which single bead changed from the file event

#### Scenario: fan-out score reflects transitive dependents
- Given: bead A blocks B, and B blocks C
- When: the Store derives fan-out score for A
- Then: A's fan-out score counts both B and C

#### Scenario: items are ordered by blocking weight, then date
- Given: item A has FanOutScore 3, item B has FanOutScore 1, both created on different days
- When: `Snapshot()` builds its `Items` slice
- Then: A appears before B, regardless of which was created first

#### Scenario: equal fan-out score falls back to creation date
- Given: two items share the same FanOutScore
- When: `Snapshot()` orders them
- Then: the earlier-created item appears first

### Requirement: QueuePane renders the live queue with blocker badges and fan-out score
`QueuePane` SHALL render a bubbles table with a single merged Item column (a glyph indicating
bead vs. proposal, a leading `MM-dd ` creation-date prefix, then the title), a blocker badge
column, and a fan-out score column, updating from each `Snapshot` it receives. When the item
count exceeds the table's visible height, `QueuePane` SHALL render an explicit indicator that
more items exist below the visible rows.

#### Scenario: a blocked item shows its badge
- Given: an item's `Blocker` field is populated
- When: `QueuePane` renders that row
- Then: the blocker badge is visible in the row

#### Scenario: an unblocked item shows no badge
- Given: an item's `Blocker` field is nil
- When: `QueuePane` renders that row
- Then: no blocker badge is shown

#### Scenario: a bead renders its glyph
- Given: an item's `Kind` is `KindBead`
- When: `QueuePane` renders that row's Item column
- Then: the row shows the 🧿 glyph before the date and title

#### Scenario: a proposal renders its glyph
- Given: an item's `Kind` is `KindProposal`
- When: `QueuePane` renders that row's Item column
- Then: the row shows the 📃 glyph before the date and title

#### Scenario: the item count exceeds the visible table height
- Given: the queue has more items than the table's current visible row height
- When: `QueuePane` renders
- Then: an indicator naming how many additional items exist below the fold is shown

### Requirement: DetailPane renders full detail for the selected queue row
`DetailPane` SHALL render notes, blocker reason (if any), and task progress for whichever row is
currently selected in `QueuePane`, at the same rendered height as `QueuePane`'s own table.
`DetailPane` MUST NOT render an "Unblocked" line for an unblocked item — the absence of a
blocker line IS the unblocked signal.

#### Scenario: selecting a row updates the detail pane
- Given: the operator moves the queue selection to a different row
- When: the selection changes
- Then: `DetailPane` immediately reflects the newly selected item's notes, blocker reason, and
  task progress

#### Scenario: an unblocked item's detail pane shows no blocker line
- Given: the selected item's `Blocker` field is nil
- When: `DetailPane` renders
- Then: no "Unblocked" (or any other blocker-status) line is shown at all

#### Scenario: a blocked item's detail pane names the blocker
- Given: the selected item's `Blocker` field is populated
- When: `DetailPane` renders
- Then: the blocker's type and reason are shown

#### Scenario: the detail pane matches the queue table's height
- Given: `QueuePane`'s table is sized to N visible rows by `Root.layout()`
- When: `DetailPane` renders
- Then: its bordered box renders at the same height as `QueuePane`'s bordered box
