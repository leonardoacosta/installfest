# wavetui Specification

## ADDED Requirements

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
