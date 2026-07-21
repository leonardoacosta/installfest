---
stack: t3
---
<!-- beads:epic:if-tkva -->
<!-- beads:feature:if-0zyk -->

<!-- stack: one of t3 | cc-meta | effect | dotnet — see commands/apply/references/stacks.md § "Stack vocabulary crosswalk" for the full tasks.md-stack:/--stack-profile/detect_stack() mapping -->

# Implementation Tasks

## DB Batch

- [x] [1.1] Extend `wavetui-core`'s `internal/store/store.go` additively: add `Item.LaneState [beads:if-nha1]
  *lanes.LaneState` field per `design.md` § Lane detection — no existing field renamed, removed,
  or re-typed [type:db]
- [x] [1.2] Scaffold `apps/wavetui/internal/lanes/lanes.go`: `LaneState` struct [beads:if-1fap]
  (`Type`/`Since`/`PaneID`/`SpawnedAt`) and `DetectLane(item, prior) *LaneState` per `design.md`
  § Lane detection [type:db]
  - depends on: 1.1

## API Batch

- [x] [2.1] Implement `apps/wavetui/internal/dispatch/spawn.go`: `Spawner` interface [beads:if-3icp]
  (`Spawn(ctx, promptText string) (paneID string, err error)`) and `TmuxSpawner` shelling
  `cc-tmux conductor dispatch --mode spawn-task` per `design.md` § Spawn gap — same package as
  `wavetui-dispatch`'s `Dispatcher`, no rename or signature change to existing types [type:api]
- [x] [2.2] Implement the spawn prompt template (persistence-mandate + `/apply <item.ID>` [beads:if-spm5]
  reference line) per `design.md` § Spawn prompt template, as a pure render function taking
  `store.Item` [type:api]
- [x] [2.3] Implement `LaneState.IsStale(item, idleWindow) bool` per `design.md` § Lane liveness / [beads:if-r849]
  § Manual-cleanup prompt: reads `Item.Session.Zombie` only, no direct pane polling [type:api]
  - depends on: 1.2

## UI Batch

- [x] [3.1] Extend `QueuePane` (`apps/wavetui/internal/ui/queuepane.go`) with lane badge rendering [beads:if-ec6g]
  for any item with a non-nil `LaneState`, naming the blocker type per `design.md` § Lane
  detection [type:ui]
  - depends on: 1.1, 1.2
- [x] [3.2] Wire `QueuePane`'s lane key binding: calls `TmuxSpawner.Spawn` with the rendered [beads:if-9khi]
  prompt template, stores the returned `paneID`/`SpawnedAt` onto the item's `LaneState`, renders
  an immediate failure badge on a `Spawn` error (no automatic retry, consistent with
  `wavetui-dispatch`'s own no-retry precedent) [type:ui]
  - depends on: 2.1, 2.2, 3.1
- [x] [3.3] Render the stale-lane badge + manual-cleanup key binding in `QueuePane` per [beads:if-e3bc]
  `design.md` § Manual-cleanup prompt: cleanup only drops the lane's local map entry, never
  touches the bead note, openspec delta, or bd claim [type:ui]
  - depends on: 2.3, 3.1
- [x] [3.4] Wire `cmd/wavetui/main.go`: instantiate `TmuxSpawner`, thread into `QueuePane`; [beads:if-crv5]
  capture runtime evidence of the badge appearing on a synthetic `blocked: decision - ...` note
  and clearing after the note is edited away, against a real tmux session in this repo [type:ui]
  - depends on: 3.2, 3.3

## E2E Batch

- [ ] [4.1] `go test` for `internal/lanes`: `DetectLane` nil/non-nil/preserved-across-snapshots [beads:if-n34y]
  cases, `IsStale` idle-window boundary cases (just under, just over, never-spawned, live-session
  short-circuit) [type:testing]
  - depends on: 1.2, 2.3
- [ ] [4.2] `go test` for `internal/dispatch/spawn.go`: `TmuxSpawner.Spawn` against a mock [beads:if-tb5o]
  `cc-tmux` CLI invocation asserting `--mode spawn-task` is used and no raw `tmux split-window`
  call appears; prompt-template rendering asserts the `/apply <item.ID>` substring and the
  persistence-mandate text are present [type:testing]
  - depends on: 2.1, 2.2
- [ ] [4.3] Runtime-verify end-to-end: run `apps/wavetui/cmd/wavetui` in a real tmux session in [beads:if-fmwr]
  this repo, trigger a lane spawn on a synthetic blocked-decision item, confirm a new pane opens
  running `claude` with the templated prompt (paste pty output), edit the underlying note to
  remove the blocker and confirm the badge clears on the next render with no manual action, then
  simulate a stale lane (idle window elapsed, no live session) and confirm the cleanup prompt
  renders and the manual-cleanup key only removes the local badge — paste terminal/pty output as
  evidence [type:testing]
  - depends on: 3.4, 4.1, 4.2
