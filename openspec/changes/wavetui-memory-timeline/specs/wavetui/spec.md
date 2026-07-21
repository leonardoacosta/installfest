# wavetui Specification

## ADDED Requirements

### Requirement: MemoryTimelinePane renders one interleaved timeline for the selected queue item
The system SHALL provide `MemoryTimelinePane`, implementing `wavetui-core`'s `Pane` interface and
attached to the existing focus ring, which renders one interleaved, date-grouped timeline for
whichever item is currently selected, merging bead lifecycle events, memory/journal entries, and
openspec archive milestones.

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

### Requirement: BeadsHistorySource reads bead lifecycle events from .beads/interactions.jsonl, never the internal database
`BeadsHistorySource` SHALL read `.beads/interactions.jsonl` directly to recover creation, claim,
close-with-reason, and comment/decision-resolution events for the selected item's bead ID (and its
children, when the selected item is an epic or feature). It MUST NOT parse `.beads/*.db` directly.

#### Scenario: a bead's lifecycle events are recovered from the interactions log
- Given: `.beads/interactions.jsonl` contains creation, claim, and close-with-reason rows for a
  selected bead
- When: `BeadsHistorySource` queries that bead ID
- Then: all three events are returned as timeline entries, each carrying its recorded timestamp

#### Scenario: an unrecognized interaction kind renders as a generic activity entry
- Given: `.beads/interactions.jsonl` contains a row with an interaction kind this source does not
  recognize
- When: it is encountered during a query
- Then: it is rendered as a generic "activity" entry rather than dropped or erroring

#### Scenario: a missing interactions log degrades the bead-lifecycle lane only
- Given: `.beads/interactions.jsonl` does not exist in the target project
- When: `BeadsHistorySource` is queried
- Then: the bead-lifecycle lane renders an "unavailable" badge, and the memory and archive lanes
  are unaffected

### Requirement: OpenSpecArchiveSource resolves an archive-landing milestone via git log, never a database or new index
For a selected item associated with a proposal slug, `OpenSpecArchiveSource` SHALL locate a
matching directory under `openspec/changes/archive/` and, if found, resolve its archive-landing
timestamp via `git log --diff-filter=A` on that path. It MUST NOT maintain a separate index of
archived proposals.

#### Scenario: an archived proposal's milestone is recovered
- Given: a proposal slug has a matching directory under `openspec/changes/archive/`
- When: `OpenSpecArchiveSource` queries that item
- Then: the add-to-archive commit's timestamp is returned as a single archive-milestone entry

#### Scenario: an item never archived produces no entry, not a badge
- Given: a proposal slug has no matching directory under `openspec/changes/archive/`
- When: queried
- Then: no archive entry is returned and no badge is raised — this is a normal outcome

### Requirement: MemoryHistorySource prefers a dated journal.md and falls back to git-log reconstruction against the memory directory's own resolved git root
`MemoryHistorySource` SHALL resolve the target project's Claude Code memory directory
(`~/.claude/projects/<flattened-cwd>/memory/`, symlink-resolved), prefer a dated `journal.md`
inside it when present, and otherwise reconstruct history via `git log -p` scoped to that
directory — using the git repository root resolved from INSIDE the memory directory itself, never
assumed to equal the target project's own repository root.

#### Scenario: a dated journal.md is preferred when present
- Given: the resolved memory directory contains a `journal.md` with dated entries
- When: `MemoryHistorySource` is queried
- Then: those dated entries are returned as first-person timeline entries, and no git-log
  reconstruction is performed

#### Scenario: git-log reconstruction runs against the memory directory's own repo root
- Given: no `journal.md` exists, and the memory directory's resolved git root differs from the
  target project's own repository root
- When: `MemoryHistorySource` falls back to git-log reconstruction
- Then: `git log` is invoked with the memory directory's own resolved root, not the target
  project's root

#### Scenario: an absent or non-git memory directory degrades the memory lane only
- Given: the target project has never had a Claude Code session, so its memory directory does not
  exist
- When: `MemoryHistorySource` is queried
- Then: the memory lane renders an "unavailable" badge, and the bead-lifecycle and archive lanes
  are unaffected

### Requirement: A git-log-derived memory entry is visually labeled as a distilled change, never rendered as a first-person record
Every timeline entry produced by the git-log-reconstruction fallback SHALL be labeled
`source=distilled` and rendered visually distinct from a `source=journal` entry, since a
diff-derived event is an approximation of what happened, not a first-person record of it.

#### Scenario: a distilled entry is visually distinguishable from a journal entry
- Given: the timeline contains both a `source=journal` entry and a `source=distilled` entry
- When: `MemoryTimelinePane` renders them
- Then: the `source=distilled` entry carries a visible "distilled change" label distinguishing it
  from the journal entry

### Requirement: Journal-to-bead matching prefers an inline bead-ID reference and falls back to timestamp-proximity fuzzy matching rendered as visually tentative
The system SHALL match a memory/journal entry to a bead by an inline bead-ID reference when
present (confident match, full-confidence rendering), and otherwise by timestamp-proximity fuzzy
matching within a configurable window, rendered visually tentative (e.g. dimmed, question-marked)
and never asserted as certain.

#### Scenario: an inline bead-ID reference produces a confident match
- Given: a journal entry's text contains a bracketed reference matching this repo's bead-ID
  grammar
- When: matched against bead lifecycle entries
- Then: the match renders at full confidence, with no tentative-match styling

#### Scenario: no inline reference falls back to timestamp-proximity fuzzy matching
- Given: a journal or distilled entry has no inline bead-ID reference
- When: a bead lifecycle event falls within the configured proximity window
- Then: the match is rendered visually tentative (dimmed, question-marked), never as a confident
  match

#### Scenario: no bead falls within the proximity window
- Given: a distilled entry has no inline reference and no bead lifecycle event within the
  proximity window
- When: rendered
- Then: it appears in its date group unmatched to any bead, with no fabricated match

### Requirement: Timeline entries render date-grouped and never fabricate cross-source intra-day ordering when source precision differs
The merge function SHALL group timeline entries by date and MUST NOT assert a specific intra-day
ordering between a date-only-precision entry and a full-timestamp-precision entry sharing the same
date.

#### Scenario: same-day entries of mixed precision render as one unordered group
- Given: a full-timestamp bead event and a date-only git-log-derived entry share the same
  calendar date
- When: `merge.Interleave` produces the timeline
- Then: both appear in that date's group with no implied ordering between them

#### Scenario: entries on different dates are ordered by date
- Given: entries spanning multiple distinct dates
- When: rendered
- Then: date groups appear in chronological order

### Requirement: The pane and all three timeline sources are strictly read-only over memory, journal, bead, and archive content
The system SHALL restrict `MemoryTimelinePane`, `BeadsHistorySource`, `OpenSpecArchiveSource`, and
`MemoryHistorySource` to rendering only content that already exists in memory, journal, bead-note,
or openspec files. They MUST NOT write to, summarize, rewrite, or otherwise generate new content
in any of those files under any code path.

#### Scenario: no source opens a memory, journal, bead-note, or openspec file for writing
- Given: the full set of file operations performed by this proposal's code
- When: audited
- Then: every open of a memory, journal, bead-note, or openspec path is read-only

### Requirement: A missing interactions log, memory directory, or archive history degrades to an unavailable badge for that lane only, never a crash
Each of the three timeline lanes SHALL degrade independently: a missing or unreadable data source
for one lane renders an "unavailable" badge for that lane only, without crashing the pane or
affecting the other two lanes.

#### Scenario: one lane's data source failing does not affect the other two
- Given: `.beads/interactions.jsonl` is missing while the memory directory and archive history are
  both available
- When: `MemoryTimelinePane` renders
- Then: only the bead-lifecycle lane shows an "unavailable" badge; the memory and archive lanes
  render normally
