# Design: wavetui-decision-lanes

## Architecture

```
wavetui-core's Store (existing, unmodified writer)
      │  Snapshot.Items[].Blocker *BlockerNote  (wavetui-core, already parses "blocked: <type> - ...")
      ▼
internal/lanes  ──────────────────────────────────────────────────────────────┐
      │  DetectLane(item) -> *LaneState{Type, Since}                          │
      │  reads item.Session (wavetui-sessions' SessionLink) for liveness      │
      ▼                                                                       │
QueuePane  ── renders badge + lane key handler ──► internal/dispatch.Spawner  │
                                                          │                    │
                                                          ▼                    │
                                              TmuxSpawner.Spawn(ctx, promptText)
                                                          │
                                              cc-tmux `conductor dispatch --mode spawn-task`
                                                          │
                                                    new tmux pane running `claude`
                                                          │
                              (no callback — completion observed only via fsnotify)
                                                          ▼
                                    operator answers, spawned session writes bd note /
                                    openspec delta, exits
                                                          │
                                                          ▼
                              BeadsSource / OpenSpecSource re-query (wavetui-core, unmodified)
                                                          │
                                                          ▼
                                    Store mutates Item.Blocker -> badge clears on next Snapshot
```

`internal/lanes` never touches `Store` state directly (same invariant every prior wavetui
proposal holds) — it is a pure derivation over a `Snapshot` the `QueuePane` already has, plus a
small idle-window timer keyed by lane state kept in `QueuePane`'s own model (not the Store), since
"is this specific lane stale" is view-local presentation state, not domain state the rest of the
app needs.

## Lane detection (reuses wavetui-core's parse, adds no new grammar)

```go
package lanes

type LaneState struct {
    Type       string    // wavetui-core's BlockerNote.Type, copied verbatim — decision/dependency/external/review/unknown
    Since      time.Time // first time this item's blocker note was observed in this exact form
    PaneID     string    // "" until a spawn has happened for this item
    SpawnedAt  time.Time // zero until spawned
}

// DetectLane returns nil when item.Blocker is nil or item.Blocker.Type == "" (no lane).
// It never re-parses the note text -- wavetui-core's BlockerNote is already the parsed form.
func DetectLane(item store.Item, prior *LaneState) *LaneState {
    if item.Blocker == nil {
        return nil
    }
    if prior != nil && prior.Type == item.Blocker.Type {
        return prior // preserve Since/PaneID/SpawnedAt across snapshots -- identity by item ID, not by note text
    }
    return &LaneState{Type: item.Blocker.Type, Since: time.Now()}
}
```

`QueuePane` keeps a `map[itemID]*LaneState` in its own model, rebuilt each `Update(Snapshot)` by
calling `DetectLane` per item and carrying forward `PaneID`/`SpawnedAt` from the prior state when
the item's blocker note is unchanged. **The moment `item.Blocker` becomes `nil` or its `Type`
changes, the lane entry is dropped from the map** — this IS the badge-clear signal; there is no
separate "resolved" flag anywhere. This is a direct consequence of the operator's simplicity
instruction: no polling, no callback, "did the blocker-note text change" as observed through the
existing pipeline is the complete mechanism.

## Spawn gap (confirmed, not assumed) and the Spawner extension

Read directly from `wavetui-dispatch`'s shipped `design.md` § Dispatcher interface:

```go
type Dispatcher interface {
    Dispatch(ctx context.Context, item store.Item, promptText string) error
}
```

Every documented resolution path for `Dispatch` (§ Target resolution) either (1) uses
`item.Session.PaneID` when a session is already linked, or (2) scores existing panes via
`conductor list --json`, or (3) falls back to `ClipboardDispatcher`. None of the three creates a
new pane. `wavetui-dispatch`'s own `proposal.md` § Context names `cc-tmux conductor dispatch
--mode <switch|send-prompt|spawn-task|spawn-worktree>` as the CLI primitive available, but its
Requirements and shipped code only wire up `switch` and a hardened `send-prompt` replacement —
`spawn-task`/`spawn-worktree` are cited as available, never implemented. This is confirmed by
reading the design.md end to end, not inferred from an incomplete search.

Per the operator's explicit fallback instruction ("document the gap ... proceed with a task that
assumes the interface will be extended, rather than stalling"), this proposal adds a **sibling
interface in the same package**, not a rename or a breaking change to `Dispatcher`:

```go
package dispatch // apps/wavetui/internal/dispatch — same package wavetui-dispatch owns

// Spawner creates a NEW target (unlike Dispatcher, which always resolves to an existing one)
// and starts a claude session in it, delivering promptText as that session's opening prompt.
// Returns the new pane's ID so callers (internal/lanes) can track liveness via wavetui-sessions'
// TmuxSource without re-deriving pane identity.
type Spawner interface {
    Spawn(ctx context.Context, promptText string) (paneID string, err error)
}

type TmuxSpawner struct{}

func (s *TmuxSpawner) Spawn(ctx context.Context, promptText string) (string, error) {
    // Shells the already-cited, already-existing CLI primitive wavetui-dispatch's own
    // proposal.md names but never wires up -- this is completing that citation, not
    // inventing a new mechanism. --prompt is passed the same way wavetui-dispatch's own
    // send-prompt equivalent handles multi-line text: via a temp buffer, never shell-arg
    // string interpolation of promptText itself (same injection-hazard avoidance
    // wavetui-dispatch's design.md already establishes for TmuxDispatcher.sendPrompt).
    out, err := exec.CommandContext(ctx, "cc-tmux", "conductor", "dispatch",
        "--mode", "spawn-task", "--prompt-file", promptFile).Output()
    // parse `out` for the new pane ID; cc-tmux's `conductor list --json` shape
    // ({id, session, window, state, project, branch, task, wait_reason, timestamp})
    // is the same shape wavetui-dispatch's Resolver already parses -- reused, not
    // reinvented, for confirming the new pane registered.
    ...
}
```

**Why a sibling interface, not a method added to `Dispatcher` itself**: `Dispatcher.Dispatch`'s
signature takes a `store.Item` because every existing caller (`QueuePane`'s Start action, the wave
executor) is dispatching an already-known queue item to a target. A spawn action has no `item` in
that sense — it is spawning a session to interrogate a blocker, and the "item" it's about is
purely contextual to the prompt text, not something `Dispatch`'s resolver logic (linked-session
lookup, best-guess pane scoring) applies to at all. Reusing the exact same method signature would
force a fake resolver arg for a resolution `Spawn` deliberately does not perform. `HeadlessDispatcher`
(named as a future `Dispatcher` implementer in `wavetui-dispatch`'s own design.md) is a different
future extension and unaffected by this addition.

**Coordination note for whoever implements `wavetui-dispatch`'s tasks.md** (this proposal does
not modify that spec's own tasks.md — it is called out here for the human/apply-time reader):
`spawn.go` lands in the SAME package `wavetui-dispatch` owns (`internal/dispatch/`), so `- touches:`
correctly declares an overlap, and `wave-plan-build`'s conflict matrix will serialize the two
proposals into different waves. No API of `wavetui-dispatch`'s existing `Dispatcher`,
`TmuxDispatcher`, or `ClipboardDispatcher` types is renamed, removed, or has its signature changed.

## Lane liveness (reuses wavetui-sessions' @cc-state, no new mechanism)

Once `TmuxSpawner.Spawn` returns a `paneID`, `internal/lanes` never polls that pane directly.
Liveness is read exactly the way `wavetui-sessions`' `TmuxSource` already exposes it on
`Item.Session` — `SessionLink.PaneID` + the pane's own `@cc-state` (`waiting`/`idle`/`active`) via
`wavetui-sessions`' existing read path (`tmux show-options -p -v -t <pane> @cc-state`, cited from
its `design.md` § Alternatives / Related Work, not re-implemented here). `internal/lanes` looks up
`Item.Session` for the SAME item the lane belongs to (a lane-triggered spawn's session becomes
that item's linked session per `wavetui-sessions`' existing linkage algorithm, since the spawned
prompt embeds the same `/apply <id>`-shaped reference `wavetui-sessions`' exact-match linkage
already scans for — no new linkage code needed, the prompt template just has to include that
substring).

```go
func (ls *LaneState) IsStale(item store.Item, idleWindow time.Duration) bool {
    if ls.SpawnedAt.IsZero() {
        return false // never spawned -- badge shown, no session yet, not "stale"
    }
    if item.Session != nil && item.Session.Zombie == false {
        return false // still alive by wavetui-sessions' own zombie-detection cross-check
    }
    return time.Since(ls.SpawnedAt) > idleWindow // config, default matches wavetui-sessions' 15min zombie default
}
```

`item.Session == nil` after a spawn (the linked session ended and `wavetui-sessions`' linkage no
longer resolves it, or the linkage never matched) is treated the same as `Zombie == true` for this
check — the lane has no live signal, so it counts toward staleness.

## Manual-cleanup prompt (never automatic)

`QueuePane` renders a lane whose `IsStale` returns true with a distinct "stale — clean up?" badge
state and a separate keybinding that only DROPS the lane entry from `QueuePane`'s local map
(clearing the badge from view). It never touches the underlying bead note, openspec delta, or bd
claim — those are the operator's own domain, same boundary `wavetui-sessions`' zombie-release
action already respects (one-key, never automatic, never touches anything but the lane's own
presentation state here). If the operator wants the blocker itself resolved differently, they
edit the note directly — this proposal's cleanup action is scoped to "stop showing me this lane
badge," not "resolve the blocker for me."

## Spawn prompt template (capture-back contract)

```
You are resolving a blocker on {item.Kind} {item.ID}: "{item.Title}".
Blocker: {item.Blocker.Type} - {item.Blocker.Reason}
Reference: {item.Blocker.Ref}

Ask Leo whatever clarifying questions you need to resolve this blocker. Before you exit, you
MUST persist the resolution somewhere this system can observe:
- For a bead-backed item: `bd comment {item.ID} --body "<resolution>"` or `bd update {item.ID}
  --notes "<updated note with the blocker replaced or removed>"`.
- For an openspec-backed item: edit `{item.SourcePath}`'s `## Context` section, replacing the
  `blocked: ...` line with the resolution (or removing it if fully resolved).

Do not exit without writing one of the above. A silent answer with nothing persisted leaves the
blocker showing as unresolved indefinitely -- there is no other completion signal.

Reference: /apply {item.ID}
```

The trailing `Reference: /apply {item.ID}` line is deliberate — it is the exact substring
`wavetui-sessions`' session-linkage exact-match scan already looks for (see its `design.md` §
Session linkage algorithm, step 1), so the spawned session links to the SAME item automatically,
giving `internal/lanes`' liveness check (above) a `Item.Session` to read with zero new linkage
code.

## Alternatives / Related Work

**Considered and rejected: a callback channel (Unix socket, file lock, or exit-code poll) for the
spawned session to signal completion.** Rejected per the operator's explicit simplicity
instruction — the app is already watching the exact files (`.beads/*`, `openspec/changes/*`) a
resolution would land in, via the same fsnotify+re-query pipeline `wavetui-core` already
established. Any new channel would be a second, redundant completion signal that could
disagree with the first (a session writes the note, the callback fails to fire, the badge stays
forever — worse than today's single-source-of-truth design), and would require this proposal's
spawn action to hold open a listener for a session whose entire lifecycle it does not otherwise
manage. The single-source-of-truth choice fails safe: a session that dies mid-answer, before
writing anything, simply leaves the badge up — exactly the desired behavior — with no separate
error path to keep in sync.

**Considered and rejected: auto-releasing a stale lane after the idle window.** Rejected for the
same reason `wavetui-sessions`' zombie-release action is manual-only (cited in its Requirements) —
an idle window is a heuristic, not proof the blocker went unanswered; auto-dropping presentation
state is comparatively low-risk (it does not touch the underlying bead/proposal), but the
operator instruction was explicit that even this lighter-weight cleanup stays a manual keypress,
never automatic.
