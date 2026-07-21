# wavetui Specification

## Purpose
TBD - created by archiving change wavetui-core. Update Purpose after archive.
## Requirements
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

### Requirement: FlairManager derives animation triggers by diffing consecutive Store snapshots, never by intercepting the event bus
`FlairManager` SHALL derive every animation trigger by comparing two consecutive `Snapshot`
values (`Diff(prev, next) []FlairEvent`) — a pure function with no side effects. It MUST NOT
subscribe to `wavetui-core`'s internal event bus, and MUST NOT read, write, or otherwise touch
`Store` state directly.

#### Scenario: an item present in the previous snapshot and absent in the next produces a closed/archived event
- Given: item `X` is present in `prev.Items` and absent from `next.Items`
- When: `Diff(prev, next)` runs
- Then: the result includes a `FlairEvent` for item `X`, keyed by `Item.Kind` to select the
  bead-closed vs. proposal-archived effect

#### Scenario: an item present in the next snapshot and absent from the previous produces an appeared event
- Given: item `Y` is absent from `prev.Items` and present in `next.Items`
- When: `Diff(prev, next)` runs
- Then: the result includes an `EventItemAppeared` event for item `Y`

#### Scenario: a blocker clearing on an item present in both snapshots produces a blocker-resolved event
- Given: item `Z` has a non-nil `Blocker` in `prev` and a nil `Blocker` in `next`
- When: `Diff(prev, next)` runs
- Then: the result includes an `EventBlockerResolved` event for item `Z`

#### Scenario: Diff never mutates its inputs
- Given: any two `Snapshot` values
- When: `Diff` is called
- Then: neither `Snapshot` value is modified, and calling `Diff` twice with the same inputs
  produces the same output

### Requirement: The tick loop runs only while an animation is live and idles at zero cost otherwise
`FlairManager` SHALL schedule a `tea.Tick` only when at least one animation is active
(`NeedsTick() == true`). It MUST NOT run a fixed-rate loop (e.g. a permanent 30fps `tea.Tick`)
regardless of animation state.

#### Scenario: no active animation means no scheduled tick
- Given: `FlairManager.active` is empty
- When: the root model's `Update` completes processing the current message
- Then: no `tea.Tick` command is issued for flair

#### Scenario: an active animation schedules exactly one next tick
- Given: at least one entry exists in `FlairManager.active`
- When: the root model's `Update` completes processing the current message
- Then: exactly one `tea.Tick` command is scheduled for the next frame

#### Scenario: an animation settling removes its tick requirement
- Given: the last active animation entry settles (reaches its harmonica settling threshold or
  fixed duration) during a frame
- When: that frame's `Update` completes
- Then: `NeedsTick()` returns `false` and no further tick is scheduled

### Requirement: Disabling flair produces byte-for-byte-identical rendering minus animation frames
When `config.FlairConfig.Enabled` is `false`, the application SHALL render identically to the
same configuration with `Enabled` set to `true`, for the same sequence of `Snapshot` values,
except for the presence of animation frames/overlays themselves. `FlairManager.Diff` and the
overlay compositor MUST NOT be invoked at all when `Enabled` is `false` — not merely invoked and
suppressed.

#### Scenario: disabled flair skips Diff entirely
- Given: `cfg.Enabled == false`
- When: a new `Snapshot` arrives at the root model
- Then: `FlairManager.Diff` is never called for that snapshot

#### Scenario: base pane rendering is unaffected by flair's enabled state
- Given: the same `Snapshot` value
- When: `QueuePane.View()` is rendered once with flair enabled and once with flair disabled
- Then: the two renders are identical except for any highlight/overlay flair itself added — no
  underlying data, layout, or non-flair styling differs

### Requirement: Flair auto-degrades on non-truecolor terminals and respects a global calm-mode toggle
`FlairManager` SHALL detect terminal color-profile capability at startup (via `lipgloss/v2`'s
color-profile detection) and MUST substitute nearest-ANSI-equivalent colors (via `go-colorful`'s
distance-based color matching) instead of emitting truecolor escape sequences on a terminal that
does not support them. When `config.FlairConfig.CalmMode` is `true`, every effect MUST resolve to
its static-glyph fallback (no frame cycling, no spring motion) while still reflecting the
underlying state signal the effect represents.

#### Scenario: a non-truecolor terminal gets ANSI-equivalent colors, not broken escape codes
- Given: the detected terminal color profile is not truecolor
- When: a color-lerp effect (e.g. row flash fade) would otherwise emit a truecolor gradient
- Then: the nearest 16-color ANSI equivalent is substituted at each step instead

#### Scenario: calm mode replaces an animated presence sprite with a static glyph
- Given: `cfg.CalmMode == true` and an item has a "blocked-on-you" session state
- When: the presence sprite for that item is rendered
- Then: a single static glyph representing "blocked-on-you" is shown, with no frame cycling

### Requirement: Negative-attention effects are reserved exclusively for genuinely bad events
The horizontal-shake-plus-red-pulse effect SHALL be triggered only by `EventNegative` (an item's
`Stale` field transitioning from `false` to `true`). It MUST NOT be reused as the effect for any
other event kind.

#### Scenario: a zombie-adjacent stale transition triggers the negative effect
- Given: item `X`'s `Stale` field is `false` in `prev` and `true` in `next`
- When: `Diff(prev, next)` runs
- Then: the result includes an `EventNegative` event for item `X`, and only that event kind maps
  to the shake-plus-red-pulse effect

#### Scenario: no other event kind ever produces the negative effect
- Given: any `FlairEvent` whose `Kind` is not `EventNegative`
- When: `FlairManager` selects an effect for that event
- Then: the shake-plus-red-pulse effect is never selected

### Requirement: Victory-recap numbers are computed from the same Snapshot data the queue already renders, never a separate accounting path
Any future recap/summary display (e.g. items closed during a session) SHALL derive its counts by
accumulating `Diff`-produced events over time, never by issuing a separate query against `bd` or
`openspec` directly.

#### Scenario: a closed-item count matches the accumulated Diff events
- Given: a sequence of snapshots over which three `EventItemClosed` events were produced by
  `Diff`
- When: a recap display computes "items closed"
- Then: the displayed count equals the number of accumulated `EventItemClosed` events, with no
  independent `bd`/`openspec` query involved

### Requirement: Session-linked presence sprites and wave-progress triggers degrade gracefully when their source data is absent
The presence-sprite feature SHALL check, at build/execution time, whether a session-state field
is present on `Item` (added by `wavetui-sessions`, if landed) before rendering any sprite. If
absent, the feature MUST be skipped entirely — no placeholder state, no error. The same
gate applies to any future wave-progress trigger pending a progress event from
`wavetui-dispatch`.

#### Scenario: presence sprites render when session-state data is present
- Given: `wavetui-sessions` has landed and `Item` exposes a session-state accessor
- When: an item with an active session is rendered in the queue pane
- Then: a presence sprite reflecting that session's state is shown alongside the row

#### Scenario: presence sprites are absent, not broken, when session-state data is absent
- Given: `wavetui-sessions` has not landed and `Item` exposes no session-state accessor
- When: the queue pane renders
- Then: no presence sprite is shown for any row, and no error or placeholder state appears

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

### Requirement: TranscriptSource tails Claude Code transcript files with tolerant, offset-based decoding
`TranscriptSource` SHALL watch `~/.claude/projects/<flattened-path>/*.jsonl` via fsnotify write
events, maintaining a per-file byte offset and reading only newly-appended bytes. It MUST buffer
a partial trailing line across reads (parsing only complete lines) and MUST reset its offset to 0
when the current file size is smaller than the stored offset (the file was replaced or
truncated). Unknown top-level `type` values or unknown fields within a known type MUST be ignored
without error.

#### Scenario: a new write is read from the stored offset, not from the start
- Given: `TranscriptSource` has previously read up to byte offset N in a transcript file
- When: fsnotify fires a write event for that file
- Then: only bytes from offset N onward are read and parsed

#### Scenario: a partial trailing line is buffered, not parsed
- Given: a write event delivers a line that is not yet terminated by a newline
- When: `TranscriptSource` processes the read
- Then: the partial line is held in a remainder buffer and parsed only once its terminating
  newline arrives in a later read

#### Scenario: a truncated file resets the offset
- Given: a tracked file's current size is smaller than the stored offset
- When: `TranscriptSource` next reads that file
- Then: the offset resets to 0 and the file is re-read from the start

#### Scenario: an unrecognized line type is ignored, not a parse failure
- Given: a transcript line has a `type` value `TranscriptSource` has never seen before
- When: it is decoded
- Then: the line is skipped with no error and no degraded badge, consistent with tolerant
  decoding across the whole source

### Requirement: A claimed item is linked to its session via an /apply reference or cwd+timestamp proximity
`TranscriptSource` SHALL link a Claude Code session to a claimed beads/openspec item using, in
order: (1) an exact `/apply <id>` reference found in a `user`-type line's message text, or (2) a
fallback match on the transcript's `cwd` field against the item's known repo path AND
claim-timestamp proximity within a configurable window (default 10 minutes). A subagent sidechain
transcript (`isSidechain: true`) SHALL inherit its parent session's item linkage via `parentUuid`
rather than being matched independently. The transcript's own `cwd` field MUST be trusted over any
inference from directory-name flattening.

#### Scenario: an exact /apply reference links immediately
- Given: a `user`-type transcript line contains the text `/apply if-abc12`
- When: `TranscriptSource` processes that line
- Then: the session is linked to item `if-abc12` with no further matching needed

#### Scenario: cwd+timestamp fallback links when no exact reference exists
- Given: no transcript line contains an `/apply <id>` reference
- When: the transcript's `cwd` matches a claimed item's repo path and the transcript's earliest
  timestamp falls within the configured window of that item's claim timestamp
- Then: the session is linked to that item via the fallback path

#### Scenario: cwd match alone, without timestamp proximity, does not link
- Given: a transcript's `cwd` matches a claimed item's repo path
- When: the transcript's earliest timestamp is outside the configured proximity window
- Then: no link is made — cwd alone is not sufficient

#### Scenario: a sidechain file inherits its parent's linkage
- Given: a transcript line has `isSidechain: true` and a `parentUuid`
- When: `TranscriptSource` processes it
- Then: it is attributed to whatever item the parent session is linked to, never matched
  independently

#### Scenario: directory-name flattening is never trusted over the transcript's own cwd field
- Given: a transcript file's flattened directory name could plausibly map to more than one real
  project path
- When: `TranscriptSource` determines the session's working directory
- Then: it uses the `cwd` field recorded inside the transcript lines, never a path reconstructed
  from the flattened directory name

### Requirement: Context gauge derives a percent-of-window estimate and badges at a 70% threshold
`TranscriptSource` SHALL derive a context-percent estimate per session from cumulative
`input_tokens` + `cache_read_input_tokens` (summed across that session's `assistant`-type
`message.usage` entries) against an approximate model context-window size, and SHALL raise a
handoff-prompt badge when that estimate crosses 70%.

#### Scenario: context percent updates as the transcript grows
- Given: a linked session's transcript gains a new `assistant`-type line with `message.usage`
- When: `TranscriptSource` processes it
- Then: the session's context-percent estimate is recalculated from the updated cumulative token
  sum

#### Scenario: crossing 70% raises a handoff badge
- Given: a session's context-percent estimate is below 70%
- When: a new `usage` entry pushes the cumulative estimate to 70% or above
- Then: the item's handoff-prompt badge becomes visible

### Requirement: Zombie detection flags a stale claim with a one-key, never-automatic release action
The system SHALL badge a claimed item as a zombie claim when its linked transcript has not grown
in >= N minutes (config, default 15) AND, when `TmuxSource` has pane data for that session, that
pane is not in `@cc-state: active`. The system SHALL expose a one-key operator action that
releases the bd claim. No automatic release of any claim SHALL occur under any circumstance.

#### Scenario: inactivity alone badges when no tmux data exists for the pane
- Given: a linked session's transcript has not grown in 15+ minutes
- When: `TmuxSource` has no `@cc-state` data for that session's pane (e.g. not run inside a
  cc-tmux-tracked pane)
- Then: the item is badged as a zombie claim based on inactivity alone

#### Scenario: an active tmux pane suppresses the zombie badge despite transcript inactivity
- Given: a linked session's transcript has not grown in 15+ minutes
- When: `TmuxSource` reports that session's pane `@cc-state` as `active`
- Then: the item is NOT badged as a zombie — the tmux signal overrides transcript-only inactivity

#### Scenario: pressing the release action releases the claim without touching other items
- Given: an item is badged as a zombie claim
- When: the operator presses the one-key release action on that item
- Then: only that item's bd claim is released — no other claimed item is affected and no release
  happens without this explicit key press

#### Scenario: no claim is ever released without an explicit operator action
- Given: an item has been zombie-badged for an arbitrarily long time
- When: no operator action is taken
- Then: the claim remains held — the system never releases it on its own

### Requirement: Error feed attributes tool-result error classes to their item and agent
`TranscriptSource` SHALL classify `tool_result` entries carrying an error (read-first violations,
string-not-found edit failures, `gate.sh BLOCKED` outputs, and other recognizable error shapes)
and attribute each to the linked item and, where determinable from the transcript's agent
metadata, the specific agent that produced it.

#### Scenario: a read-first violation is attributed to its item
- Given: a linked session's transcript contains a tool_result with a read-first-violation error
  shape
- When: `TranscriptSource` processes that entry
- Then: the error is added to that item's error feed with its error class recorded

#### Scenario: an unrecognized error shape is not silently dropped
- Given: a tool_result carries an error that does not match any known classification
- When: it is processed
- Then: it is still recorded in the error feed under a generic/unclassified class rather than
  being discarded

### Requirement: Token meter tracks output tokens by model per session, item, and wave, and flags opus in an executor lane
`TranscriptSource` SHALL accumulate `output_tokens` by model name per session, rolling up to the
linked item and (when wave metadata is available from the linked item) the wave. It SHALL flag
when a model other than the fleet's designated executor-tier model (opus) is running in a
role/lane conventionally reserved for a lighter-weight executor model.

#### Scenario: output tokens accumulate per model
- Given: a linked session's transcript gains two `assistant` lines using different models
- When: `TranscriptSource` processes both
- Then: each model's output-token total is tracked separately under that session

#### Scenario: opus running in an executor lane is flagged
- Given: a session identified (via its linked item's agent-role metadata) as an executor-lane
  dispatch
- When: its transcript's model field is `opus`-tier
- Then: the token meter raises a flag for that session

### Requirement: Rate-limit signals in the transcript stream surface a backpressure banner
`TranscriptSource` SHALL detect a rate-limit indicator in the transcript stream and publish a
`RateLimitSignal` event; `KPIBar` SHALL render the resulting banner. This proposal SHALL NOT
build any queue or scheduling logic that consumes this signal to pause dispatch — emission only.

#### Scenario: a rate-limit indicator raises a banner
- Given: a transcript line indicates a rate-limited response
- When: `TranscriptSource` processes it
- Then: a `RateLimitSignal` event is published and `KPIBar` displays the backpressure banner

#### Scenario: the signal is emitted with no consuming queue logic
- Given: a `RateLimitSignal` event has been published
- When: inspecting this proposal's code
- Then: no dispatch-queue or scheduling component exists that reads and acts on this event — it
  is rendered only

### Requirement: TmuxSource reads cc-tmux's @cc-state pane option as its primary source of pane state
`TmuxSource` SHALL read the `@cc-state` tmux pane option (via `tmux show-options -p -v -t <pane>
@cc-state`, the same primitive cc-tmux's own `get_pane_option` wraps) for every pane cc-tmux has
tagged, as its primary and preferred data path. It MUST NOT re-derive state for a cc-tmux-tracked
pane via a process-tree walk. A process-tree walk (`ps -axo pid,ppid,comm`) SHALL be used only as
a fallback for panes cc-tmux has not tagged, and MUST NOT assume any positional relationship
("the adjacent pane") between panes.

#### Scenario: a cc-tmux-tracked pane is read via @cc-state, not re-derived
- Given: cc-tmux has tagged a pane with `@cc-state: active`
- When: `TmuxSource` queries that pane
- Then: it reads `@cc-state` directly and reports `active` — it does not walk that pane's process
  tree to confirm

#### Scenario: an untagged pane falls back to process-tree walking
- Given: a pane has no `@cc-state` option set (cc-tmux not installed, or the pane predates
  cc-tmux tracking)
- When: `TmuxSource` queries that pane
- Then: it falls back to a process-tree walk to look for a `claude` process, and reports no
  result (not a guess) when none is found

#### Scenario: no positional assumption is made between panes
- Given: two adjacent panes in the same window, one tracked and one not
- When: `TmuxSource` resolves the untracked pane's state
- Then: it does not infer anything about the untracked pane from the tracked neighbor's state

### Requirement: SessionsPane renders the pane map, context gauges, and zombie badges as a focus-ring pane
`SessionsPane` SHALL implement `wavetui-core`'s `Pane` interface (`Update(Snapshot) Pane`,
`View() string`, `Focusable() bool`) and render, per linked session: its pane identity (when
known), context-percent gauge, and zombie badge (when applicable). It SHALL attach to
`wavetui-core`'s existing focus ring without requiring any change to the root model.

#### Scenario: SessionsPane implements the shared Pane interface
- Given: `wavetui-core`'s root model's pane collection
- When: `SessionsPane` is added to it
- Then: it satisfies the same `Pane` interface as `QueuePane` and `DetailPane`, requiring no root
  model changes

#### Scenario: a linked session's context gauge is visible
- Given: an item has a linked session with a context-percent estimate
- When: `SessionsPane` renders that item's row
- Then: the context-percent gauge is displayed and reflects the current estimate

### Requirement: KPIBar renders continue-count, rate-limit incidents, and stale-claim minutes as a focus-ring pane
`KPIBar` SHALL implement `wavetui-core`'s `Pane` interface and render a continue-count proxy
metric, a count of rate-limit incidents observed in the current run, and the elapsed minutes
since the oldest currently-zombie-badged claim went stale.

#### Scenario: KPIBar implements the shared Pane interface
- Given: `wavetui-core`'s root model's pane collection
- When: `KPIBar` is added to it
- Then: it satisfies the same `Pane` interface used by the focus ring

#### Scenario: a rate-limit incident increments the KPIBar counter
- Given: `KPIBar` has observed zero rate-limit incidents so far in the current run
- When: a `RateLimitSignal` event is published
- Then: `KPIBar`'s rate-limit incident counter increments by one

### Requirement: A malformed or truncated transcript line degrades the sessions pane, never the whole app
`TranscriptSource` SHALL degrade only the sessions pane to an "unavailable" badge for the affected
session on any parse failure (malformed JSON, an unexpected field type where a specific type was
expected) — it MUST NOT crash the process or affect any other pane.

#### Scenario: malformed JSON on one line degrades only that session's state
- Given: a transcript file contains one malformed JSON line among otherwise well-formed lines
- When: `TranscriptSource` encounters it
- Then: that session's state is badged "unavailable" and processing continues to subsequent
  well-formed lines — the app does not crash

#### Scenario: a transcript parse failure never affects QueuePane or DetailPane
- Given: `TranscriptSource` has degraded a session to "unavailable"
- When: `QueuePane` and `DetailPane` render
- Then: both continue to render their own (unrelated) state normally

### Requirement: Dispatcher interface abstracts prompt delivery behind Dispatch(item, promptText)
`apps/wavetui/internal/dispatch` SHALL define a `Dispatcher` interface with a single method,
`Dispatch(ctx context.Context, item store.Item, promptText string) error`, implemented by
`TmuxDispatcher` and `ClipboardDispatcher`. The signature MUST NOT assume a tmux pane exists, so
a future `HeadlessDispatcher` (out of scope here) can implement it without a breaking change.

#### Scenario: interface has exactly one method
- Given: the `Dispatcher` interface definition
- When: a new implementation is added
- Then: it only needs to implement `Dispatch(ctx, item, promptText) error` — no tmux-specific
  method leaks into the interface

#### Scenario: a future HeadlessDispatcher can implement the same signature
- Given: the `Dispatcher` interface as shipped by this proposal
- When: a later proposal adds `HeadlessDispatcher` (a `claude -p` scheduler)
- Then: it implements `Dispatch(ctx, item, promptText) error` with no change to the interface

### Requirement: TmuxDispatcher targets a linked or best-guess pane via bracketed paste, never a literal multi-line send-keys
`TmuxDispatcher` SHALL resolve a target pane by preferring `item.Session.PaneID` (set by
`wavetui-sessions`) when present, else scoring candidates from `cc-tmux conductor list --json`
same-window > same-session > other, with ties prompting the operator rather than picking
silently. Delivery MUST use `tmux load-buffer` + `paste-buffer -p` (bracketed paste) followed by
a SEPARATE `send-keys Enter` call — never a single `send-keys` invocation carrying a literal
multi-line prompt string.

#### Scenario: linked session pane is used directly
- Given: an item with `Session.PaneID` set by `wavetui-sessions`
- When: `TmuxDispatcher.Dispatch` runs
- Then: it targets that pane ID directly, skipping candidate scoring

#### Scenario: same-window candidate outranks same-session
- Given: no linked session, and two candidate panes — one in the same tmux window as the
  item's project, one merely in the same tmux session
- When: candidates are scored
- Then: the same-window candidate is selected

#### Scenario: a scoring tie prompts rather than picks silently
- Given: two candidate panes with equal score
- When: `TmuxDispatcher` resolves a target
- Then: the operator is prompted to choose; no candidate is picked automatically

#### Scenario: multi-line prompt is delivered via bracketed paste, not literal send-keys
- Given: a `promptText` containing embedded newlines
- When: `TmuxDispatcher` delivers it to a pane
- Then: it uses `load-buffer` + `paste-buffer -p`, followed by a separate `send-keys Enter` call
  — at no point is the full multi-line string passed to a single `send-keys -l` invocation

### Requirement: TmuxDispatcher refuses a pane whose linked session is mid-turn or in copy-mode
`TmuxDispatcher` MUST check `#{pane_in_mode}` before pasting and refuse (surfacing a warning,
never force-pasting) when the pane is in copy-mode. It MUST also refuse when the target item's
linked session (per `wavetui-sessions`) is actively streaming, queuing or warning rather than
blind-pasting into a generating REPL.

#### Scenario: copy-mode pane is refused
- Given: the target pane's `#{pane_in_mode}` reads `1`
- When: `TmuxDispatcher.Dispatch` runs
- Then: the dispatch is refused with a copy-mode-specific error, no paste is attempted

#### Scenario: a mid-turn session is refused, never blind-pasted
- Given: the target item's linked session is currently streaming (per `wavetui-sessions`'
  transcript-derived state)
- When: `TmuxDispatcher.Dispatch` runs
- Then: the dispatch is refused and `QueuePane` renders "queued — session busy"; no paste
  reaches the pane

#### Scenario: an idle linked session proceeds normally
- Given: the target item's linked session is idle (not streaming)
- When: `TmuxDispatcher.Dispatch` runs
- Then: the paste proceeds through the normal active-pane-state check

### Requirement: ClipboardDispatcher is the fallback when no tmux target exists
`ClipboardDispatcher` SHALL write `promptText` via an OSC52 escape sequence to `/dev/tty` when
OSC52 support is detected (or forced via a per-project config override), else fall back to a
pbcopy-equivalent resolved via `exec.LookPath` in the order `pbcopy` (Darwin) ->
`xclip -selection clipboard` -> `xsel --clipboard --input` -> `wl-copy`, surfacing failure rather
than silently no-op'ing if none resolve. It is used whenever `TmuxDispatcher` finds zero
candidate panes or no tmux session exists at all.

#### Scenario: OSC52 is used when detected
- Given: the terminal advertises OSC52 support (or the config override forces it)
- When: `ClipboardDispatcher.Dispatch` runs
- Then: the prompt is written as an OSC52 sequence to `/dev/tty`, no external process is spawned

#### Scenario: pbcopy-equivalent fallback probes real binaries, not shell aliases
- Given: OSC52 is not detected and no config override forces it, on a Linux host with `xclip`
  installed but no zsh alias in the invoking process's environment
- When: `ClipboardDispatcher.Dispatch` runs
- Then: it resolves `xclip -selection clipboard` via `exec.LookPath` directly — it does not
  attempt to exec a literal `pbcopy` command name on Linux

#### Scenario: zero tmux candidates falls back to clipboard
- Given: `cc-tmux conductor list --json` returns an empty pane array
- When: Start is triggered on an item with no linked session
- Then: `ClipboardDispatcher` is used instead of `TmuxDispatcher`

#### Scenario: no tmux session at all falls back to clipboard
- Given: `$TMUX` is unset and no tmux server is reachable
- When: Start is triggered
- Then: `ClipboardDispatcher` is used, never a hard failure

### Requirement: Dispatch-boundary values are validated against an id-shaped regex before crossing into a shell or tmux buffer
Any value interpolated into a dispatch command (pane IDs, item IDs) SHALL be validated against
`^[A-Za-z0-9_-]+$` before crossing the dispatch boundary. `promptText` is exempt from this check
(it is free-form prose delivered exclusively via paste-buffer/OSC52 payload, never via shell
argument interpolation) — bead/proposal titles and notes MUST NOT be substituted for an id at
this boundary.

#### Scenario: a non-id-shaped pane ID is rejected
- Given: a resolved pane target string containing a shell metacharacter
- When: `validateDispatchTarget` runs on it
- Then: it returns an error and no tmux command is issued

#### Scenario: promptText is never regex-validated
- Given: a `promptText` containing arbitrary punctuation and newlines
- When: `TmuxDispatcher.Dispatch` runs
- Then: `promptText` passes through unchanged to the paste buffer without being matched against
  the id-shaped regex

### Requirement: Dispatch failures surface immediately with no automatic retry
A `Dispatcher.Dispatch` failure SHALL surface as an immediate UI-visible failure badge with no
backoff loop, re-attempt, or queue-and-retry-later behavior in this proposal's code paths.

#### Scenario: a tmux paste failure surfaces once, not retried
- Given: `TmuxDispatcher.Dispatch` returns an error (e.g. the pane died mid-dispatch)
- When: the error propagates to the caller
- Then: `QueuePane` renders a failure badge and no automatic re-attempt occurs

#### Scenario: a clipboard write failure surfaces, not silently swallowed
- Given: `ClipboardDispatcher.Dispatch` fails (no resolvable binary and OSC52 undetected)
- When: the error propagates
- Then: the failure is rendered, not silently dropped

### Requirement: QueuePane Start dispatches the selected item with one keypress
`QueuePane` SHALL bind a "Start" key that dispatches the currently-selected item's rendered
prompt via the resolved `Dispatcher`, with the prompt landing in the target pane (or clipboard)
without additional operator steps.

#### Scenario: Start dispatches the highlighted item
- Given: one item is highlighted in `QueuePane`
- When: the operator presses the Start key
- Then: that item's prompt is dispatched via the resolved `Dispatcher` in one action

### Requirement: QueuePane select mode builds a wave plan with file-overlap conflict warnings
`QueuePane` SHALL support a multi-select mode that accumulates candidate items ordered by
`Item.FanOutScore`, computing file-overlap conflicts across candidates' `Item.TouchedFiles` and
rendering one warning row per conflicting path (naming both item IDs) before the operator
finalizes. Finalizing writes a wave file per the format decided in `design.md` § Open Question
(gated on operator confirmation, not implemented until then).

#### Scenario: multi-select accumulates candidates ordered by fan-out score
- Given: the operator multi-selects three items with differing `FanOutScore`
- When: the wave-builder view renders the selection
- Then: candidates are ordered by `FanOutScore` descending

#### Scenario: overlapping touched files are flagged before finalizing
- Given: two selected candidates whose `TouchedFiles` share a path
- When: the wave-builder view renders
- Then: a warning row names both item IDs and the shared path — neither candidate is silently
  dropped from the selection

#### Scenario: finalizing with no conflicts proceeds
- Given: a selection with zero file-overlap conflicts
- When: the operator finalizes
- Then: a wave file is written per the confirmed format

### Requirement: HeadlessDispatcher implements the Dispatcher interface with no signature change
`apps/wavetui/internal/daemon` SHALL define `HeadlessDispatcher` implementing
`wavetui-dispatch`'s `Dispatcher` interface exactly as shipped —
`Dispatch(ctx context.Context, item store.Item, promptText string) error` — with no additional
exported method and no change to that interface's signature.

#### Scenario: HeadlessDispatcher satisfies the existing Dispatcher interface
- Given: `wavetui-dispatch`'s `Dispatcher` interface as already shipped
- When: `HeadlessDispatcher` is compiled against it
- Then: it satisfies the interface with `Dispatch(ctx, item, promptText) error` and no interface
  change is required in `internal/dispatch`

#### Scenario: Dispatch is an admission decision, not a completion wait
- Given: a `HeadlessDispatcher.Dispatch` call that successfully spawns a child process
- When: `Dispatch` returns
- Then: it returns `nil` immediately after a successful spawn — it does not block until the
  child process exits

### Requirement: Admission is bounded by a config-driven concurrency semaphore
`HeadlessDispatcher` SHALL bound concurrent headless children to `Config.HeadlessConcurrencyCap`
(additive field in `internal/config/config.go`, default `2` when unset or `<= 0`) via a
semaphore. `Dispatch` SHALL return `ErrConcurrencyCapReached` immediately, without spawning a
process, when no slot is available.

#### Scenario: admission is refused at the cap, not queued internally
- Given: `HeadlessConcurrencyCap` is `2` and two children are already running
- When: `Dispatch` is called for a third item
- Then: it returns `ErrConcurrencyCapReached` immediately and no third process is spawned

#### Scenario: a completed child frees its slot for the next admission
- Given: two of two concurrency slots are in use
- When: one running child's process exits (success or failure)
- Then: its slot becomes available and the next `Dispatch` call for a different item succeeds

#### Scenario: default cap of 2 applies when unset
- Given: `Config.HeadlessConcurrencyCap` is unset (zero value)
- When: `HeadlessDispatcher` is constructed
- Then: it enforces a concurrency cap of `2`, never unbounded admission

### Requirement: Dispatched prompts embed /apply <id> so session linkage requires no new code
`HeadlessDispatcher` SHALL compose the `claude -p` prompt as `/apply <item.ID>` when
`item.TaskProgress` is nil or not started, else `/apply <item.ID> --continue`, reusing
`wavetui-sessions`' existing exact-match session-linkage algorithm (a literal `/apply <id>`
substring scan) with no new linkage code in this proposal.

#### Scenario: a fresh item is dispatched with a plain /apply prompt
- Given: an item with `TaskProgress == nil`
- When: `HeadlessDispatcher` composes its prompt
- Then: the prompt is exactly `/apply <item.ID>`

#### Scenario: a partially-started item is dispatched with --continue
- Given: an item with `TaskProgress` indicating work already started
- When: `HeadlessDispatcher` composes its prompt
- Then: the prompt is exactly `/apply <item.ID> --continue`

#### Scenario: the spawned child's session links to the item with no new linkage code
- Given: a headless child spawned with a composed `/apply <id>` prompt
- When: `wavetui-sessions`' `TranscriptSource` observes that child's transcript
- Then: it links the session to the item via its existing exact-match algorithm — no code in
  `internal/daemon` performs its own session-linkage matching

### Requirement: A rate-limit signal from Snapshot.RateLimitBanner pauses admission until explicit operator resume
`HeadlessDispatcher` SHALL read `Snapshot.RateLimitBanner` (populated by `wavetui-sessions`'
`TranscriptSource`) and, on transition from nil to non-nil, pause new admission (`Dispatch`
returns `ErrQueuePaused` for every new call) without terminating already-running children.
Resuming SHALL require an explicit operator action in `internal/ui/headlessbar.go`; no
timer-based or automatic resume path SHALL exist.

#### Scenario: a rate-limit signal pauses new admission only
- Given: `Snapshot.RateLimitBanner` transitions from nil to non-nil while one child is running
- When: a new `Dispatch` call is made for a different ready item
- Then: it returns `ErrQueuePaused` and no new process is spawned, while the already-running
  child continues uninterrupted

#### Scenario: the pause banner renders while paused
- Given: the headless queue is paused due to a rate-limit signal
- When: `headlessbar.go` renders
- Then: it shows a visible banner naming the pause and the resume keybinding

#### Scenario: resume requires an explicit keypress, never a timer
- Given: the headless queue is paused
- When: any amount of wall-clock time elapses with no operator keypress
- Then: admission remains paused — no code path resumes it automatically

#### Scenario: an explicit operator resume clears the pause
- Given: the headless queue is paused
- When: the operator presses the bound resume key in `headlessbar.go`
- Then: `Dispatch` accepts new admissions again, up to the concurrency cap

### Requirement: A zombie-flagged headless session frees its concurrency slot without auto-release or auto-retry
`HeadlessDispatcher` SHALL stop counting a headless-dispatched item against its concurrency
accounting once `Item.Session.Zombie` becomes `true` (per `wavetui-sessions`' existing
two-signal zombie detection), without killing the underlying process, without releasing the
item's bd claim, and without re-dispatching it. The zombie state SHALL be surfaced only through
`wavetui-sessions`' existing `SessionsPane` badge — this proposal SHALL NOT render a second
zombie indicator.

#### Scenario: a zombied headless item frees its slot for new admission
- Given: a headless-dispatched item whose linked session becomes zombie-flagged
- When: the daemon controller observes the next `Snapshot`
- Then: that item's concurrency slot is freed for a new `Dispatch` call, while the underlying
  process is not killed

#### Scenario: a zombied headless item does not auto-release its claim
- Given: a headless-dispatched item whose linked session is zombie-flagged
- When: the concurrency slot is freed per the scenario above
- Then: the item's bd claim remains held — only the existing one-key operator action in
  `SessionsPane` releases it

#### Scenario: zombie state renders through the existing badge only
- Given: a headless-dispatched item is zombie-flagged
- When: the UI renders
- Then: `SessionsPane`'s existing zombie badge shows it; `headlessbar.go` renders no separate
  zombie indicator for the same item

### Requirement: A non-zero or errored child exit surfaces immediately with no automatic retry
`HeadlessDispatcher` SHALL publish a `HeadlessExitEvent{ItemID, ExitCode, Err, Failed}` onto
`wavetui-core`'s event bus when a headless child's process exits, whether success or failure.
A `Failed` event SHALL surface as an immediate UI-visible failure indication with no backoff loop,
re-attempt, or queue-and-retry-later behavior anywhere in this proposal's code paths.

#### Scenario: a non-zero exit publishes a failed event
- Given: a headless child process exits with a non-zero status
- When: `awaitExit` observes the exit
- Then: it publishes `HeadlessExitEvent{Failed: true, ExitCode: <non-zero>}` and the slot is
  released

#### Scenario: a failed exit is never automatically retried
- Given: a `HeadlessExitEvent` with `Failed: true`
- When: the event is observed by the daemon controller
- Then: no new `Dispatch` call for the same item is issued automatically — a human decides
  whether to re-dispatch

#### Scenario: a successful exit publishes a non-failed event
- Given: a headless child process exits with status `0`
- When: `awaitExit` observes the exit
- Then: it publishes `HeadlessExitEvent{Failed: false, ExitCode: 0}` and the slot is released

### Requirement: A queue item whose blocker note matches the blocked:<type> grammar renders a lane badge naming the type
`internal/lanes.DetectLane` SHALL derive a `LaneState{Type, Since}` from `Item.Blocker` whenever
it is non-nil, reusing `wavetui-core`'s already-parsed `BlockerNote.Type` verbatim — no new
grammar, no re-parsing of note text. `QueuePane` SHALL render a badge naming that type for every
item with a non-nil lane state.

#### Scenario: a decision-typed blocker note renders a badge naming its type
- Given: an item's `Blocker.Type` is `"decision"`
- When: `QueuePane` renders that item's row
- Then: a lane badge is shown naming `decision`

#### Scenario: an item with no blocker note has no lane
- Given: an item's `Blocker` is nil
- When: `DetectLane` runs for that item
- Then: it returns nil — no lane badge is rendered

#### Scenario: lane state is preserved across snapshots when the note is unchanged
- Given: an item already has a `LaneState` with `PaneID` set from a prior spawn
- When: a new `Snapshot` arrives with the same `Blocker.Type` for that item
- Then: `DetectLane` returns the prior state unchanged — `PaneID`/`SpawnedAt` are not reset

### Requirement: Pressing the lane action spawns a claude session via a Spawner extension of the Dispatcher family, never a second pane-creation mechanism
`internal/dispatch` SHALL define a `Spawner` interface (`Spawn(ctx, promptText string) (paneID
string, err error)`) as a sibling to `wavetui-dispatch`'s existing `Dispatcher` interface, in the
SAME package. `TmuxSpawner` SHALL implement it by shelling `cc-tmux conductor dispatch --mode
spawn-task` — the CLI primitive `wavetui-dispatch`'s own proposal.md already cites as available
but never wires up. This proposal MUST NOT add a second, independent tmux-pane-creation code path.

#### Scenario: lane button triggers Spawn, not Dispatch
- Given: an item with a non-nil lane state
- When: the operator presses the lane key
- Then: `TmuxSpawner.Spawn` is called with a rendered prompt — `Dispatcher.Dispatch` is not
  invoked for this action, since there is no existing target to resolve

#### Scenario: Spawn shells cc-tmux's spawn-task mode, not a bespoke tmux split command
- Given: `TmuxSpawner.Spawn` is called
- When: it creates the new pane
- Then: it invokes `cc-tmux conductor dispatch --mode spawn-task` — no direct `tmux split-window`
  or equivalent raw tmux pane-creation call appears in `internal/dispatch/spawn.go`

#### Scenario: Spawn returns the new pane ID for liveness tracking
- Given: `TmuxSpawner.Spawn` succeeds
- When: it returns
- Then: the returned `paneID` is stored on the item's `LaneState.PaneID` for later liveness checks

### Requirement: The spawned session's prompt template mandates writing its resolution into a bd note or openspec delta before exiting
The prompt text passed to `Spawn` SHALL instruct the spawned session to ask clarifying questions
and, before exiting, persist its resolution via `bd comment`/`bd update` (bead-backed items) or an
edit to the item's source `## Context` blocker line (openspec-backed items). The template MUST
include an `/apply <item.ID>`-shaped reference line so `wavetui-sessions`' existing linkage scan
links the spawned session to the same item with no new linkage code.

#### Scenario: prompt names the persistence requirement explicitly
- Given: a lane spawn for a bead-backed item
- When: the rendered prompt is inspected
- Then: it contains an explicit instruction to run `bd comment`/`bd update` before exiting, and
  states that exiting without persisting leaves the blocker unresolved

#### Scenario: prompt includes an /apply-shaped reference for session linkage
- Given: a lane spawn for item `if-abcd`
- When: the rendered prompt is inspected
- Then: it contains the literal substring `/apply if-abcd`

### Requirement: A lane badge clears only when the linked item's blocker-note text changes, observed via the existing fsnotify+re-query pipeline
No new IPC, callback channel, or exit-code poll SHALL be introduced for spawn completion. The
badge clears exclusively when `BeadsSource`/`OpenSpecSource` (wavetui-core, unmodified) re-query
after an fsnotify-triggered debounce and the resulting `Item.Blocker` becomes nil or changes type.

#### Scenario: a session that writes its resolution clears the badge on the next snapshot
- Given: a spawned session writes a `bd update` removing the blocker note
- When: `BeadsSource` re-queries after the next fsnotify debounce
- Then: the item's `Blocker` becomes nil and `DetectLane` returns nil — the badge disappears

#### Scenario: a session that exits without writing anything leaves the badge in place
- Given: a spawned session exits (cleanly or otherwise) without modifying the bead note or
  openspec delta
- When: the next snapshot arrives
- Then: `Item.Blocker` is unchanged and the badge remains

#### Scenario: no new completion channel exists
- Given: this proposal's implementation
- When: `internal/lanes` and `internal/dispatch/spawn.go` are inspected
- Then: neither defines a socket, lock file, or exit-code-polling mechanism for the spawned
  process — the only completion signal is the Store's existing re-query pipeline

### Requirement: Lane-session liveness is shown by reusing wavetui-sessions' TmuxSource @cc-state signal, never a new liveness mechanism
`internal/lanes` SHALL determine whether a spawned lane session is still active by reading the
SAME item's `Item.Session` (populated by `wavetui-sessions`' linkage algorithm) and its
`Zombie`/`@cc-state`-derived state — no separate polling of the spawned `paneID` is implemented.

#### Scenario: an actively-streaming lane session shows as live
- Given: a lane's item has `Item.Session != nil` and `Session.Zombie == false`
- When: `QueuePane` renders that lane
- Then: it shows a live/active indicator, not a stale prompt

#### Scenario: liveness reuses wavetui-sessions' linkage, not a direct pane poll
- Given: `internal/lanes`' liveness check
- When: its implementation is inspected
- Then: it reads `Item.Session` fields only — it does not independently call
  `tmux show-options` or poll the spawned pane directly

### Requirement: A lane session that exits without writing a resolution leaves the badge in place and surfaces a manual-cleanup prompt after a configurable idle window
`LaneState.IsStale` SHALL return true when a spawn has occurred (`SpawnedAt` non-zero) and either
`Item.Session` is nil or zombie, AND the configured idle window (default matching
`wavetui-sessions`' 15-minute zombie default) has elapsed since `SpawnedAt`. `QueuePane` SHALL
render a distinct stale-lane badge with a manual cleanup key that only clears the lane's local
presentation state — it MUST NOT touch the underlying bead note, openspec delta, or bd claim, and
MUST NOT fire automatically.

#### Scenario: a stale lane surfaces a cleanup prompt, not an automatic release
- Given: a lane was spawned 20 minutes ago (idle window default 15 minutes) and its linked
  session is now zombie
- When: `QueuePane` renders that lane
- Then: it shows a "stale — clean up?" badge; no automatic action has been taken

#### Scenario: manual cleanup only clears local lane state
- Given: the operator presses the cleanup key on a stale lane
- When: the action executes
- Then: the lane entry is removed from `QueuePane`'s local map only — the underlying bead note,
  openspec delta, and bd claim are left untouched

#### Scenario: a lane within the idle window is never marked stale
- Given: a lane was spawned 5 minutes ago with no live session signal yet
- When: `IsStale` is evaluated
- Then: it returns false — the badge shows the normal spawned-but-not-yet-live state, not stale

