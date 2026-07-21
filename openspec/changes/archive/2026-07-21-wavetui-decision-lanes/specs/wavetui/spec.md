## ADDED Requirements

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
