# Design: wavetui-daemon

## Architecture

```
                       claude -p "/apply <id>[...--continue]"
QueuePane / daemon controller ──► HeadlessDispatcher.Dispatch(ctx, item, promptText)
        │                              │
        │                       semaphore (cap N, config-driven, default 2)
        │                              │
        │                       exec.CommandContext ──► child process (async)
        │                              │
        │                       goroutine: cmd.Wait() ──► typed HeadlessExitEvent
        │                              │                   (ItemID, ExitCode, Err, Duration)
        │                              ▼
        │                    wavetui-core's event bus (reused, not rebuilt)
        │                              ▼
        │                    wavetui-core's Store (single writer, additive fields)
        │                              ▼
        │                     immutable Snapshot (+HeadlessQueue)
        │                              ▼
        └── reads Snapshot.RateLimitBanner ◄── wavetui-sessions' TranscriptSource (unchanged, emit-only)
        └── reads Item.Session.Zombie      ◄── wavetui-sessions' zombie detection (unchanged, emit-only)
                                              ▼
                                bubbletea Program.Send() (reused, not rebuilt)
                                              ▼
                                headlessbar.go (new focus-ring pane: banner + resume key)
```

`HeadlessDispatcher` is the only new writer of process state. It never touches Store fields
directly — like every other source in this fleet, its only output is a typed event onto the
existing bus; the Store remains the single mutator.

## Dispatcher interface — implemented verbatim, not re-derived

Verified by reading `wavetui-dispatch`'s shipped `design.md` § Dispatcher interface and
`specs/wavetui/spec.md`'s "Dispatcher interface abstracts prompt delivery behind
Dispatch(item, promptText)" requirement directly, per the operator's explicit instruction not to
guess this signature:

```go
type Dispatcher interface {
    Dispatch(ctx context.Context, item store.Item, promptText string) error
}
```

`HeadlessDispatcher` implements this with zero deviation:

```go
type HeadlessDispatcher struct {
    cap     int              // semaphore size — config.HeadlessConcurrencyCap, default 2
    sem     chan struct{}
    mu      sync.Mutex
    running map[string]*headlessProc // item ID -> running child, for slot accounting + zombie interaction
    paused  bool
    pausedSince time.Time
    bus     EventBus // wavetui-core's existing bus — same interface every other source publishes on
}

type headlessProc struct {
    cmd       *exec.Cmd
    itemID    string
    startedAt time.Time
}
```

`Dispatch` is a synchronous ADMISSION decision, not a synchronous completion wait — this matters
because a headless child can run for minutes, and `Dispatch`'s contract (per `wavetui-dispatch`'s
own `design.md`: "Returns immediately on failure") is about the dispatch attempt itself, not the
work it starts:

```go
func (d *HeadlessDispatcher) Dispatch(ctx context.Context, item store.Item, promptText string) error {
    d.mu.Lock()
    if d.paused {
        d.mu.Unlock()
        return ErrQueuePaused // synchronous refusal — no spawn attempted, same shape as
                               // TmuxDispatcher's ErrPaneInCopyMode/ErrSessionStreaming refusals
    }
    d.mu.Unlock()

    select {
    case d.sem <- struct{}{}:
        // slot acquired
    default:
        return ErrConcurrencyCapReached // synchronous refusal — this is admission throttling,
                                          // NOT a retry of a failed dispatch; Dispatch never
                                          // started, so "no automatic retry" does not apply —
                                          // the caller (queue controller) re-offers this item
                                          // on the next tick once a slot frees
    }

    cmd := exec.CommandContext(ctx, "claude", "-p", promptText)
    if err := cmd.Start(); err != nil {
        <-d.sem // release slot
        return err // spawn failure — surfaces synchronously, same as any other Dispatcher error
    }

    d.mu.Lock()
    d.running[item.ID] = &headlessProc{cmd: cmd, itemID: item.ID, startedAt: time.Now()}
    d.mu.Unlock()

    go d.awaitExit(item.ID, cmd) // async — publishes HeadlessExitEvent on completion
    return nil
}

func (d *HeadlessDispatcher) awaitExit(itemID string, cmd *exec.Cmd) {
    err := cmd.Wait()
    d.mu.Lock()
    delete(d.running, itemID)
    d.mu.Unlock()
    <-d.sem // release slot unconditionally — success or failure, the slot is free either way

    exitCode := 0
    if exitErr, ok := err.(*exec.ExitError); ok {
        exitCode = exitErr.ExitCode()
    }
    d.bus.Publish(HeadlessExitEvent{
        ItemID:   itemID,
        ExitCode: exitCode,
        Err:      err,
        Failed:   err != nil,
    })
}
```

No retry anywhere in this path — a failed `Dispatch` (spawn error) and a failed exit
(`HeadlessExitEvent.Failed`) both terminate at "publish, surface, stop." This directly extends
`wavetui-dispatch`'s own "No automatic retry" invariant (`design.md` § No automatic retry:
"Retry storms against tmux or against a downstream `claude` session are a real failure mode... the
smarter, rate-limit-aware retry policy that WOULD be safe belongs to `wavetui-daemon`") — this
proposal is the one that inherits that citation, and the answer it lands on is: there is no
retry policy, smarter or otherwise. A retry loop against a rate limit is not a safer version of
this proposal's job; it is the exact failure this proposal exists to prevent.

## Concurrency cap default: 2, config-driven, never unbounded

`internal/config/config.go` (shipped by `wavetui-core`, already the per-project TOML loader every
sibling extends additively — `wavetui-dispatch`'s `ClipboardDispatcher.ForceOSC52` and
`wavetui-flair`'s `Flair.Enabled`/`Flair.CalmMode` both cite this same file as their config home)
gains one more additive field:

```go
type Config struct {
    // ... existing wavetui-core + wavetui-flair fields unchanged ...

    HeadlessConcurrencyCap int // default 2 when unset or <= 0
}
```

**Default is 2, not unbounded, and not 1.** Justification: a single Claude Code API account has a
shared rate-limit budget across all concurrent sessions; running headless dispatch at
"unbounded" would let this proposal itself trigger the exact backpressure condition it is built
to respect (see the design.md self-consistency note below). 1 is over-conservative — it forecloses
using headless dispatch to work through a queue while a second item builds context, with no
throughput benefit over doing it manually. 2 gives real parallelism headroom while keeping the
daemon's own aggregate request rate a small, bounded multiple of a single interactive session's —
consistent with the "false-positive pause is cheap, false-negative lockout is not" conservative
posture the exploration set for rate-limit handling itself. Operators with a higher-tier rate
limit can raise it via config; the shipped default never assumes that headroom exists.

## Prompt composition: reuse the exact-match session-linkage algorithm, add zero new code

`wavetui-sessions`' `design.md` § Session linkage algorithm step 1 ("Exact match... scan
`user`-type transcript lines' `message` text for a literal `/apply <id>` substring... First match
wins") is the mechanism this proposal needs for the spawned headless child's transcript to link
back to the queue item — and it already exists, unmodified, waiting for exactly this input:

```go
func composePrompt(item store.Item) string {
    if item.TaskProgress != nil && item.TaskProgress.Started {
        return fmt.Sprintf("/apply %s --continue", item.ID)
    }
    return fmt.Sprintf("/apply %s", item.ID)
}
```

`item.TaskProgress` is `wavetui-core`'s own field (`design.md` § Store data model:
`TaskProgress *TaskProgress // nil when not applicable`) — this proposal does not track its own
resume state; it reads the field the Store already derives from `bd show`/`tasks.md` and defers
entirely to `/apply`'s own `--continue` detection (`commands/apply.md` § "--continue: resume an
interrupted single-spec /apply" — "auto-detects where to resume rather than starting from the
top... via `scripts/bin/apply-resume-detect`"). The daemon's ONLY resume-adjacent decision is
"start fresh vs. pass `--continue`"; every actual resume-point calculation happens inside the
child process, in code this proposal never touches.

## "One spec per session, then stop" — delegated, not reimplemented

The exploration specified a bounded-unit-of-work rule for headless sessions. This proposal does
NOT build a new mechanism for it. `/apply`'s own execution model already IS that mechanism: each
invocation works one spec's phases/waves to a gate or to completion, then the process exits
cleanly — `--continue` exists specifically because that exit is expected and resumable, not a
crash. `HeadlessDispatcher` simply invokes `claude -p "/apply <id>"` and lets that child's own
exit (clean or otherwise) be the unit boundary; `awaitExit` above treats every exit the same way
regardless of cause. If a future need arises for a *different* bounding rule (e.g., a wall-clock
timeout independent of `/apply`'s own gates), that is a `context.Context` deadline passed into
`exec.CommandContext` — a one-line addition to `Dispatch`, not a new subsystem — and is left
undecided here since nothing in the Done Means requires it.

## Rate-limit backpressure: consume only, never re-detect

`wavetui-sessions`' `design.md` § Rate-limit backpressure states its own scope precisely: "emit
only, never consume... Building the headless-dispatch queue that would PAUSE on this signal is
explicitly out of scope — a sibling proposal's concern." This proposal is that sibling, and its
job is exactly as narrow as that sentence implies:

```go
// Called from the daemon controller's Update(Snapshot) — the same reactive pattern every
// pane in this fleet already uses; no new watcher, no second transcript parse.
func (d *daemonController) onSnapshot(snap store.Snapshot) {
    if snap.RateLimitBanner != nil && !d.dispatcher.paused {
        d.dispatcher.pause(snap.RateLimitBanner)
    }
}
```

`pause()` sets `d.paused = true` and records `pausedSince` and the banner's own signal metadata
(whatever `RateLimitSignal` already carries — this proposal does not need to know its internal
shape beyond "non-nil means paused," since `wavetui-sessions` owns that type). Pausing stops NEW
admission only (`Dispatch` returns `ErrQueuePaused` per above) — already-running children are
never killed, since a hard-killed `claude -p` mid-turn is a worse outcome than letting it finish
naturally while new work waits.

**Resume is exclusively an operator keypress — never a timer.** This is a hard design constraint
from the exploration, restated here because it is easy to get subtly wrong: a timer-based
auto-resume risks immediately re-triggering the same rate limit the moment it fires, because the
underlying condition (too many requests in the window) may not have actually cleared just because
some wall-clock duration elapsed. `headlessbar.go` binds a resume key that calls
`d.dispatcher.resume()` directly — no scheduled retry, no backoff timer, nothing that fires
without a human pressing a key.

## Additive Snapshot field

`Snapshot` (already extended once, additively, by `wavetui-sessions`' `RateLimitBanner *RateLimitSignal`)
gains one more field, following the same "nil/zero means inactive" convention:

```go
type Snapshot struct {
    // ... existing wavetui-core + wavetui-sessions fields unchanged ...

    HeadlessQueue *HeadlessQueueState // nil when headless dispatch has never been enabled this run
}

type HeadlessQueueState struct {
    Enabled       bool
    ConcurrencyCap int
    ActiveCount   int      // len(HeadlessDispatcher.running) at last Snapshot
    Paused        bool
    PausedSince   time.Time
    PauseSignal   *RateLimitSignal // the exact signal that caused the pause — nil when not paused
}
```

`headlessbar.go` implements `wavetui-core`'s `Pane` interface (`Update(Snapshot) Pane`,
`View() string`, `Focusable() bool`) and appends to the existing focus-ring slice, per
`wavetui-core`'s own § Pane extensibility ("Sibling proposals attach by appending to that
slice — no root-model rework needed"). It renders nothing when `HeadlessQueue == nil` or
`!Paused`, and a single banner line with the resume keybinding hint when `Paused`.

## Zombie interaction: consume the badge, free the slot, do nothing else

`wavetui-sessions`' zombie detection (`design.md` § Zombie detection: two independent signals)
already flags `Item.Session.Zombie = true` for a stale claim, rendered by `SessionsPane` with a
"one-key, never-automatic" release action. A headless-dispatched child that goes zombie is still
a running OS process from `HeadlessDispatcher`'s point of view (its `cmd.Wait()` goroutine has not
returned) — without consuming this signal, a zombied headless session would hold its concurrency
slot forever, silently shrinking the effective cap over the daemon's lifetime.

```go
func (d *daemonController) onSnapshot(snap store.Snapshot) {
    // ... rate-limit pause check above ...
    for _, item := range snap.Items {
        if item.Session != nil && item.Session.Zombie {
            d.dispatcher.releaseSlotIfZombie(item.ID) // decrements accounting only —
                                                          // never kills the process, never
                                                          // releases the bd claim, never re-dispatches
        }
    }
}
```

`releaseSlotIfZombie` stops COUNTING the item against `ActiveCount`/the semaphore's logical
capacity so new work can be admitted — it does not call `cmd.Process.Kill()` (the underlying
process may still be alive; killing it is a decision this proposal does not make unilaterally,
matching the "never either signal alone, never automatic" caution `wavetui-sessions` already
established for the badge itself) and it does not touch the bd claim. The existing zombie badge
in `SessionsPane` is the ONLY UI surface for this state — `headlessbar.go` does not render a
second one, per the operator's explicit instruction to reuse rather than duplicate.

## Child-process lifecycle on daemon exit — verified: children survive, reparented to PID 1

`exec.CommandContext`'s cancellation on `ctx.Done()` sends `SIGKILL` to the direct child only, on
Linux and Darwin, by default — it does NOT propagate to a process group unless one is explicitly
created (`cmd.SysProcAttr.Setpgid`).

**Verified** (`tasks.md [4.1]`, `TestE2EChildProcessSurvivesWhenDaemonKilled` in
`apps/wavetui/internal/daemon/e2e_process_lifecycle_test.go`): a headless-dispatched `claude -p`
child, and any of its own grandchildren, **survives a SIGKILL of the daemon parent process,
reparented to PID 1 (init)**. Real `ps` evidence from the test (a separate re-exec'd "daemon"
harness process SIGKILLed out from under a real child + grandchild pair): both remained alive
post-kill with `PPID=1`, e.g. `2737467       1 S    sh -c sleep 20 & echo GRANDCHILD_PID=$!; wait`
and `2737468 2737467 S    sleep 20`. This is not a hypothetical — orphaning IS observed, not
merely possible. Follow-up remediation (registering running children's PIDs somewhere an operator
can discover and reap them after a crash) is tracked as `if-ugxa.1`, since building that mechanism
is out of scope for this proposal's Done Means but the gap it addresses is now confirmed real, not
speculative.

## Alternatives considered

**Considered and rejected: a bounded worker-pool with a work queue owned by `HeadlessDispatcher`
itself** (pull model — dispatcher asks the Store for the next ready item). Rejected because
`wavetui-core`'s own architecture is push-based: sources publish events, the Store derives state,
panes react to snapshots. A dispatcher that reaches back into the Store to pull work would be the
first component in this fleet to invert that flow. The chosen shape keeps `HeadlessDispatcher` a
pure `Dispatcher` implementation — something else (the daemon controller, itself reacting to
`Snapshot.Items` the same way any pane does) decides WHICH item to dispatch next and calls
`Dispatch`. This mirrors `wavetui-dispatch`'s own separation of `Resolver` (decides target) from
`Dispatcher.Dispatch` (executes) — decision and execution stay separate components here too.

**Considered and rejected: killing a paused-queue's in-flight children immediately on a rate-limit
signal.** Rejected per the exploration's own framing — a rate-limit signal means "stop starting
NEW work," not "abort work already in flight." An in-flight child is not itself necessarily the
cause of the limit (it may have started before the limit was hit), and killing it discards
partial progress for no benefit over letting it finish.
