---
stack: t3
---
<!-- beads:epic:if-tkva -->
<!-- beads:feature:if-ugxa -->

<!-- stack: one of t3 | cc-meta | effect | dotnet — see commands/apply/references/stacks.md § "Stack vocabulary crosswalk" for the full tasks.md-stack:/--stack-profile/detect_stack() mapping -->

# Implementation Tasks

## DB Batch

- [x] [1.1] Extend `internal/config/config.go` additively: `HeadlessConcurrencyCap int` field (default `2` when unset or `<= 0`) per `design.md` § Concurrency cap default — no existing field renamed, removed, or re-typed [beads:if-9geg]
- [x] [1.2] Extend `internal/store/store.go` additively: `Snapshot.HeadlessQueue *HeadlessQueueState` and the `HeadlessQueueState` struct (`Enabled`, `ConcurrencyCap`, `ActiveCount`, `Paused`, `PausedSince`, `PauseSignal`) per `design.md` § Additive Snapshot field — no existing `Item`/`Snapshot` field renamed or removed [beads:if-rk6n]
  - depends on: 1.1

## API Batch

- [x] [2.1] Implement `internal/daemon/headless_dispatcher.go`: `HeadlessDispatcher` satisfying `wavetui-dispatch`'s exact `Dispatcher` interface (`Dispatch(ctx, item, promptText) error`), semaphore-bounded admission (`ErrConcurrencyCapReached`), `composePrompt` (`/apply <id>` vs `/apply <id> --continue` per `item.TaskProgress`), and `awaitExit` publishing `HeadlessExitEvent` on the existing event bus per `design.md` § Dispatcher interface / § Prompt composition [beads:if-uzzw]
  - depends on: 1.1, 1.2
- [x] [2.2] Implement `internal/daemon/daemon.go` rate-limit pause/resume controller: `onSnapshot` reads `Snapshot.RateLimitBanner`, calls `dispatcher.pause()` on nil->non-nil transition (`ErrQueuePaused` for new admissions, already-running children untouched), exposes `resume()` for the explicit operator action — no timer-based resume path anywhere in this file, per `design.md` § Rate-limit backpressure [beads:if-p0m3]
  - depends on: 2.1
- [x] [2.3] Implement `internal/daemon/daemon.go` zombie-slot-release: `onSnapshot` iterates `Snapshot.Items`, calls `dispatcher.releaseSlotIfZombie(itemID)` for any `Item.Session.Zombie == true` headless-tracked item — decrements concurrency accounting only, never kills the process, never releases the bd claim, never re-dispatches, per `design.md` § Zombie interaction [beads:if-qkov]
  - depends on: 2.1

## UI Batch

- [x] [3.1] Implement `internal/ui/headlessbar.go`: a new focus-ring pane (`Pane` interface — `Update(Snapshot) Pane`, `View() string`, `Focusable() bool`) rendering nothing when `Snapshot.HeadlessQueue == nil` or `!Paused`, and a banner + resume keybinding hint when `Paused`; the resume key calls `daemon.resume()` directly with no intermediate scheduling per `design.md` § Rate-limit backpressure / § Additive Snapshot field [beads:if-hpet]
  - depends on: 2.2
- [x] [3.2] Wire `cmd/wavetui/main.go`: instantiate `HeadlessDispatcher` + the daemon controller, append `headlessbar.go` to the root model's focus-ring pane slice per `wavetui-core`'s § Pane extensibility (no root-model rework), thread `Config.HeadlessConcurrencyCap` through construction [beads:if-huo2]
  - depends on: 2.1, 2.2, 2.3, 3.1

## E2E Batch

- [x] [4.1] Runtime-verify end-to-end (SAFETY-SUBSTITUTED per operator instruction: real `/bin/sh` processes running `sleep`/`exit`, never a real `claude` binary): enable headless dispatch with `HeadlessConcurrencyCap=2`, dispatch 3 items and confirm only 2 run concurrently (paste evidence of the third's `ErrConcurrencyCapReached` refusal and its later admission once a slot frees); simulate a `Snapshot.RateLimitBanner` transition and confirm new admission is refused while an in-flight real child is left running, then confirm an explicit `Controller.Resume()` call (not a timer) re-enables admission; force one real child to exit non-zero and confirm the failure surfaces immediately with no automatic re-dispatch observed over a shortened (documented) wait; runtime-verify what happens to a safe-substitute child's own subprocess when the parent daemon process is killed, per `design.md` § Child-process lifecycle on daemon exit — real `ps` evidence shows BOTH the direct child and its forked grandchild survive a SIGKILL of the daemon process, reparented to PID 1 (init) [beads:if-9rfm]
  - depends on: 3.2
- [x] [4.2] `go test` for `internal/daemon`: semaphore admission/refusal/slot-release fixtures, `composePrompt` fresh-vs-continue cases, pause/resume state transitions (including a fixture asserting no code path resumes without an explicit `resume()` call), zombie-slot-release accounting (confirms no process kill, no claim release call), and `HeadlessExitEvent` success/failure publishing [beads:if-drli]
  - depends on: 2.1, 2.2, 2.3
