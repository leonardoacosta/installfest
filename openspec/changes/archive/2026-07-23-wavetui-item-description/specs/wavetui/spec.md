## MODIFIED Requirements

### Requirement: Store derives normalized queue state by re-querying CLIs, never by inferring from which file changed
The Store SHALL be the single writer of derived queue state (items, dep graph, fan-out score,
staleness clocks). It MUST NOT infer item-level meaning from which file path changed; it acts on
a "something in this source changed" signal and always resolves current truth by re-querying the
source's CLI. Each `Item` MAY carry a `Description` field sourced from the backing bead's own
`description` field or the backing proposal's `## Summary` section — absent for either source
when no such content exists (empty string, not an error).

#### Scenario: a .beads/id.db-wal write triggers a full re-query, not a partial diff
- Given: a single bead is updated
- When: `.beads/id.db-wal` changes and the debounce window elapses
- Then: the Store re-queries `bd list --json` and `bd ready --json` in full rather than
  attempting to infer which single bead changed from the file event

#### Scenario: fan-out score reflects transitive dependents
- Given: bead A blocks B, and B blocks C
- When: the Store derives fan-out score for A
- Then: A's fan-out score counts both B and C

#### Scenario: a bead's description is threaded through from bd's own output
- Given: `bd list --json` returns a bead with a non-empty `description` field
- When: `BeadsSource` converts that record to a `store.Item`
- Then: `Item.Description` carries that same text verbatim

#### Scenario: a proposal's Summary section is extracted
- Given: `proposal.md` contains a `## Summary` section with body text
- When: `OpenSpecSource` parses that proposal into a `store.Item`
- Then: `Item.Description` carries the Summary section's body text

#### Scenario: an item with no description source has an empty Description
- Given: a bead with no `description` set, or a proposal with no `## Summary` section
- When: the Store builds that item
- Then: `Item.Description` is the empty string, not an error or a placeholder

### Requirement: DetailPane renders full detail for the selected queue row
`DetailPane` SHALL render notes, blocker reason (if any), task progress, and the item's
description (if any) for whichever row is currently selected in `QueuePane`.

#### Scenario: selecting a row updates the detail pane
- Given: the operator moves the queue selection to a different row
- When: the selection changes
- Then: `DetailPane` immediately reflects the newly selected item's notes, blocker reason, and
  task progress

#### Scenario: an item with a description shows it in the detail pane
- Given: the selected item's `Description` field is non-empty
- When: `DetailPane` renders
- Then: the description text is shown

#### Scenario: an item with no description shows no extra section
- Given: the selected item's `Description` field is empty
- When: `DetailPane` renders
- Then: no blank description section or placeholder label is shown
