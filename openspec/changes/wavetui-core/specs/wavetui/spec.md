# wavetui Specification

## ADDED Requirements

### Requirement: Typed event bus delivers source events to exactly one Store writer
The system SHALL provide an event bus that sources publish typed events onto, with the Store as
the bus's only subscriber that mutates shared state. Sources MUST NOT touch Store state directly
or touch each other's state.

#### Scenario: a source publishes an event
- Given: `BeadsSource` detects a debounced filesystem change
- When: it re-queries `bd` and builds a typed event
- Then: the event is published on the bus and no other source or the UI is mutated directly

#### Scenario: bus delivery survives one source failing
- Given: `OpenSpecSource` is mid-publish when `BeadsSource` errors
- When: `BeadsSource`'s error event is published
- Then: `OpenSpecSource`'s event still reaches the Store unaffected

### Requirement: Store derives normalized queue state by re-querying CLIs, never by inferring from which file changed
The Store SHALL be the single writer of derived queue state (items, dep graph, fan-out score,
staleness clocks). It MUST NOT infer item-level meaning from which file path changed; it acts on
a "something in this source changed" signal and always resolves current truth by re-querying the
source's CLI.

#### Scenario: a .beads/id.db-wal write triggers a full re-query, not a partial diff
- Given: a single bead is updated
- When: `.beads/id.db-wal` changes and the debounce window elapses
- Then: the Store re-queries `bd list --json` and `bd ready --json` in full rather than
  attempting to infer which single bead changed from the file event

#### Scenario: fan-out score reflects transitive dependents
- Given: bead A blocks B, and B blocks C
- When: the Store derives fan-out score for A
- Then: A's fan-out score counts both B and C

### Requirement: BeadsSource watches .beads/ and re-queries via bd after a trailing debounce
`BeadsSource` SHALL watch `.beads/` (the main db file, `-wal`, and `-shm`) via fsnotify, debounce
trailing-edge for 300-500ms, then re-query via `bd list --json` and `bd ready --json`. It MUST
also poll every 15 seconds as a belt-and-braces fallback and MUST NOT parse the SQLite database
directly.

#### Scenario: a WAL-only write still triggers a re-query
- Given: a commit touches only `.beads/beads.db-wal`, not the main db file
- When: fsnotify fires on the `-wal` file
- Then: `BeadsSource` still debounces and re-queries via `bd`, exactly as it would for a main-db
  write

#### Scenario: 15-second poll fallback fires when no fs event arrives
- Given: no fsnotify event has fired in over 15 seconds
- When: the poll interval elapses
- Then: `BeadsSource` re-queries via `bd` regardless of fs-event state

#### Scenario: editor tmp+rename save re-arms the watch
- Given: an editor saves `.beads/config.yaml` via write-to-tmp then rename
- When: the rename event fires
- Then: `BeadsSource` re-watches the final path by name rather than losing the orphaned
  inode-based watch

### Requirement: OpenSpecSource watches openspec/changes/ and optionally plans/ and advisor-plans/
`OpenSpecSource` SHALL watch `openspec/changes/` via fsnotify (non-recursive walk-then-watch,
re-arming on directory-create events), parsing each proposal's `proposal.md` header and
`tasks.md` checkbox counts. Watching `plans/` and `advisor-plans/` SHALL be gated behind a config
flag, default off, and rendered as visually second-class when enabled.

#### Scenario: a new proposal directory is picked up without restart
- Given: `openspec/changes/` is being watched
- When: a new proposal directory is created under it
- Then: `OpenSpecSource` adds a watch on the new directory and begins tracking its
  `proposal.md`/`tasks.md`

#### Scenario: plans/ is invisible by default
- Given: the config flag for `plans/` visibility is unset (default off)
- When: a file changes under `plans/`
- Then: `OpenSpecSource` does not surface it in the queue

#### Scenario: plans/ renders second-class when enabled
- Given: the config flag for `plans/` visibility is explicitly enabled
- When: `OpenSpecSource` surfaces a `plans/` item
- Then: it is visually distinguished from an `openspec/changes/` proposal in the queue

### Requirement: Blocker-note convention is parsed from a structured "blocked:" line
The system SHALL parse a `blocked: <type> - <reason> (see <ref>)` line (per `design.md`'s
formalized grammar) from bead notes or a proposal's `## Context` section, where `<type>` routes
a badge lane and `(see <ref>)` is optional.

#### Scenario: a well-formed blocker note is parsed
- Given: a bead note contains `blocked: decision - awaiting Leo's call (see cc-abc12)`
- When: `BeadsSource` re-queries that bead
- Then: the item's `Blocker` field is populated with type `decision`, reason text, and ref
  `cc-abc12`

#### Scenario: a malformed line is silently ignored, not an error
- Given: a bead note contains free text that does not match the grammar
- When: it is parsed
- Then: no `Blocker` is set and no error or badge is raised

### Requirement: Store snapshots reach the bubbletea Program via Program.Send(), never via watcher logic in Update()
The root bubbletea model's `Update()` SHALL only ever react to a `SnapshotMsg` sent via
`Program.Send()`. No fsnotify, polling, or CLI-invocation logic SHALL live inside `Update()`.

#### Scenario: Update() never calls a source or CLI directly
- Given: the root model's `Update()` implementation
- When: it receives any message
- Then: it never invokes `bd`, `openspec`, or fsnotify APIs directly — only Store-provided
  `SnapshotMsg` values

#### Scenario: renders coalesce under a burst
- Given: ten filesystem events fire within 200ms
- When: the Store publishes resulting snapshots
- Then: the Program renders at roughly a 10fps cap rather than once per event

### Requirement: QueuePane renders the live queue with blocker badges and fan-out score
`QueuePane` SHALL render a bubbles table with columns for item, type, created-at, blocker badge,
and fan-out score, updating from each `Snapshot` it receives.

#### Scenario: a blocked item shows its badge
- Given: an item's `Blocker` field is populated
- When: `QueuePane` renders that row
- Then: the blocker badge is visible in the row

#### Scenario: an unblocked item shows no badge
- Given: an item's `Blocker` field is nil
- When: `QueuePane` renders that row
- Then: no blocker badge is shown

### Requirement: DetailPane renders full detail for the selected queue row
`DetailPane` SHALL render notes, blocker reason (if any), and task progress for whichever row is
currently selected in `QueuePane`.

#### Scenario: selecting a row updates the detail pane
- Given: the operator moves the queue selection to a different row
- When: the selection changes
- Then: `DetailPane` immediately reflects the newly selected item's notes, blocker reason, and
  task progress

### Requirement: A missing .beads/ or openspec/ directory degrades to an unavailable badge, never a crash
When `.beads/` or `openspec/changes/` does not exist at startup, the corresponding source SHALL
render an "unavailable" badge instead of erroring, and SHALL watch the parent directory so a
later creation of the directory is picked up live.

#### Scenario: startup with no .beads/ directory
- Given: the current project directory has no `.beads/` directory
- When: `wavetui` starts
- Then: the beads portion of the queue shows an "unavailable" badge, and the app does not crash
  or render a blank screen

#### Scenario: the directory appears after startup
- Given: `wavetui` is running with an "unavailable" beads badge
- When: `.beads/` is created (e.g. via `bd init`)
- Then: `BeadsSource` detects the creation via its parent-directory watch and transitions out of
  the unavailable state without a restart

### Requirement: Root model exposes a pluggable pane and focus-ring architecture for future sibling panes
The root model SHALL hold an ordered collection of panes implementing a shared `Pane` interface
plus a focus index (the focus ring), so a future sibling proposal can attach a new pane without
modifying the root model's existing logic.

#### Scenario: QueuePane and DetailPane both implement the shared interface
- Given: the root model's pane collection
- When: inspected
- Then: both `QueuePane` and `DetailPane` satisfy the same `Pane` interface used by the focus
  ring

#### Scenario: focus moves between panes
- Given: `QueuePane` currently has focus
- When: the operator triggers focus-next
- Then: focus moves to `DetailPane` per the focus ring's ordering

### Requirement: Source failures render as badges, never panics, with serialized and coalesced CLI invocations
Every source failure SHALL be represented as state (a badge with retry backoff), never a panic.
CLI invocations per project SHALL be serialized to one in-flight call with pending requests
coalesced, so a burst of filesystem events does not cause a thundering herd of subprocess calls.

#### Scenario: a non-zero bd exit keeps the last-good snapshot
- Given: `bd list --json` exits non-zero
- When: `BeadsSource` observes the failure
- Then: the Store keeps the last-good snapshot, badges the pane stale, and schedules a retry with
  backoff — it never renders an empty queue as if it were current truth

#### Scenario: malformed JSON is tolerated
- Given: `bd ready --json` emits malformed JSON
- When: `BeadsSource` attempts to decode it
- Then: the same stale-badge-and-retry path is taken as a non-zero exit, never a panic

#### Scenario: a burst of fs events coalesces into one CLI call
- Given: five fsnotify events fire for the same source within the debounce window
- When: the debounce elapses
- Then: exactly one `bd`/`openspec` invocation is made, not five
