// Package daemon implements wavetui-daemon's headless-dispatch queue:
// HeadlessDispatcher (a Dispatcher that spawns bounded `claude -p` child
// processes instead of delivering into a tmux pane or the clipboard) and
// the daemon controller that reacts to rate-limit and zombie signals
// already carried on Snapshot. See openspec/changes/wavetui-daemon/
// design.md § Architecture: "HeadlessDispatcher is the only new writer of
// process state. It never touches Store fields directly... its only output
// is a typed event onto the existing bus."
package daemon

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"time"

	"github.com/leonardoacosta/installfest/apps/wavetui/internal/bus"
	"github.com/leonardoacosta/installfest/apps/wavetui/internal/dispatch"
	"github.com/leonardoacosta/installfest/apps/wavetui/internal/store"
)

// var _ dispatch.Dispatcher = (*HeadlessDispatcher)(nil) is the compile-time
// assertion for spec.md's "HeadlessDispatcher satisfies the existing
// Dispatcher interface" scenario — HeadlessDispatcher implements
// wavetui-dispatch's Dispatcher with zero deviation and no interface
// change in internal/dispatch.
var _ dispatch.Dispatcher = (*HeadlessDispatcher)(nil)

// ErrConcurrencyCapReached is returned by Dispatch when every concurrency
// slot is already occupied. This is admission throttling, not a retry of a
// failed dispatch — Dispatch never started, so the "no automatic retry"
// invariant elsewhere in this package does not apply here: the caller (the
// queue controller) re-offers this item on a later tick once a slot frees.
// See design.md § Dispatcher interface.
var ErrConcurrencyCapReached = errors.New("headless dispatch concurrency cap reached, refusing new admission")

// ErrQueuePaused is returned by Dispatch while the queue is paused due to a
// rate-limit signal (see daemon.go's onSnapshot / HeadlessDispatcher.pause).
// Same synchronous-refusal shape as internal/dispatch's
// ErrPaneInCopyMode/ErrSessionStreaming — no spawn is attempted.
var ErrQueuePaused = errors.New("headless dispatch queue is paused (rate-limit backpressure), refusing new admission")

// EventBus is the narrow publish-only surface HeadlessDispatcher depends on
// — satisfied verbatim by *bus.Bus (wavetui-core's existing bus, reused not
// rebuilt). Declared as a local interface rather than a direct *bus.Bus
// field purely so tests can inject a fake instead of a real Bus — the same
// hermetic-testing rationale as internal/dispatch/tmux.go's tmuxRunner.
type EventBus interface {
	Publish(ev bus.Event)
}

// HeadlessExitEvent is published on the bus once a headless-dispatched
// child exits, success or failure — the ONLY output HeadlessDispatcher ever
// produces about a running child (design.md § Architecture). No retry
// decision is made here or anywhere else in this package: a Failed event is
// a terminal notification, not the start of a backoff loop. See design.md's
// "No retry anywhere in this path."
type HeadlessExitEvent struct {
	ItemID   string
	ExitCode int
	Err      error
	Failed   bool
}

// EventName implements bus.Event.
func (HeadlessExitEvent) EventName() string { return "headless.exit" }

// waiter is the subset of *exec.Cmd's surface HeadlessDispatcher needs once
// a child has started: just enough to block for its exit. *exec.Cmd
// satisfies this trivially (its own Wait() error method). Exists so tests
// can inject a fake child instead of ever invoking a real `claude` binary —
// tasks.md [4.2]'s "hermetic... not real claude -p invocations" contract.
type waiter interface {
	Wait() error
}

// headlessRunner is the subprocess-start boundary HeadlessDispatcher
// depends on, so tests can inject a fake instead of actually spawning
// `claude -p` — the same hermetic-testing rationale as
// internal/dispatch/tmux.go's tmuxRunner. execHeadlessRunner (below) is the
// only implementation that ever touches os/exec.
type headlessRunner interface {
	// Start spawns promptText as a headless `claude -p` invocation. A
	// non-nil error means the spawn itself failed (Dispatch's synchronous
	// spawn-failure case) — no process was started and no slot accounting
	// happens for it.
	Start(ctx context.Context, promptText string) (waiter, error)
}

// execHeadlessRunner is the real headlessRunner, backed by os/exec. Mirrors
// design.md § Dispatcher interface's literal
// `exec.CommandContext(ctx, "claude", "-p", promptText)` call.
type execHeadlessRunner struct{}

func (execHeadlessRunner) Start(ctx context.Context, promptText string) (waiter, error) {
	cmd := exec.CommandContext(ctx, "claude", "-p", promptText)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd, nil
}

// headlessProc is one child HeadlessDispatcher is tracking for slot
// accounting — never a handle used to kill or signal it. See
// releaseSlotIfZombie's doc comment for why this proposal never kills a
// process under any circumstance.
type headlessProc struct {
	proc      waiter
	startedAt time.Time
	// slotReleased guards the semaphore's single release: either
	// awaitExit's normal-completion path or releaseSlotIfZombie's early
	// release sets this exactly once, whichever happens first, so the
	// other path never attempts a second `<-d.sem` receive for the same
	// child (which would either double-count freed capacity or, on an
	// already-empty channel, deadlock the releasing goroutine).
	slotReleased bool
}

// HeadlessDispatcher satisfies wavetui-dispatch's exact Dispatcher
// interface (Dispatch(ctx, item, promptText) error), spawning promptText as
// a bounded headless `claude -p` child process instead of delivering into
// an interactive tmux pane or the clipboard. See design.md § Dispatcher
// interface / § Concurrency cap default.
//
// HeadlessDispatcher never touches Store fields directly and never calls
// bd — its only output is HeadlessExitEvent published on bus.
type HeadlessDispatcher struct {
	sem    chan struct{}
	bus    EventBus
	runner headlessRunner

	mu          sync.Mutex
	running     map[string]*headlessProc
	paused      bool
	pausedSince time.Time
	pauseSignal *store.RateLimitSignal
}

// NewHeadlessDispatcher constructs a HeadlessDispatcher bounded to cap
// concurrent children, publishing onto b. cap <= 0 is defensively floored
// to 1 rather than producing a permanently-zero-capacity semaphore that can
// never admit anything — the real default resolution (2, vs an operator
// override) happens one layer up, in config.Config's own
// EffectiveHeadlessConcurrencyCap; by the time cap reaches this
// constructor it is expected to already be that resolved, positive value.
func NewHeadlessDispatcher(cap int, b EventBus) *HeadlessDispatcher {
	if cap <= 0 {
		cap = 1
	}
	return &HeadlessDispatcher{
		sem:     make(chan struct{}, cap),
		bus:     b,
		runner:  execHeadlessRunner{},
		running: make(map[string]*headlessProc),
	}
}

// composePrompt builds the `claude -p` prompt for item: a plain
// `/apply <id>` for a fresh item, `/apply <id> --continue` once work has
// already started — reusing wavetui-sessions' exact-match `/apply <id>`
// session-linkage algorithm unmodified (design.md § Prompt composition).
// This proposal tracks no resume state of its own; every actual
// resume-point calculation happens inside the spawned `/apply` process.
//
// RESOLVED: design.md's own literal snippet checks
// `item.TaskProgress.Started`, but store.TaskProgress (shipped verbatim by
// the DB batch, commit f05e056) carries only Done/Total — no Started field
// exists, matching wavetui-core's original "checkbox tally" doc comment on
// the type. "Work already started" is read here as `TaskProgress.Done > 0`:
// that is the only observable signal this shape provides for "started," and
// TaskProgress's own doc comment ("nil on an Item that has no sub-tasks")
// makes Done == 0 the correct proxy for "not yet started" either way —
// zero tasks done reads the same as brand new regardless of whether the
// item happens to carry a non-nil TaskProgress pointer.
func composePrompt(item store.Item) string {
	if item.TaskProgress != nil && item.TaskProgress.Done > 0 {
		return fmt.Sprintf("/apply %s --continue", item.ID)
	}
	return fmt.Sprintf("/apply %s", item.ID)
}

// Dispatch implements dispatch.Dispatcher. It is a synchronous ADMISSION
// decision, not a synchronous completion wait: a headless child can run for
// minutes, and Dispatch returns as soon as the spawn attempt itself
// succeeds or fails. See design.md § Dispatcher interface.
func (d *HeadlessDispatcher) Dispatch(ctx context.Context, item store.Item, promptText string) error {
	d.mu.Lock()
	paused := d.paused
	d.mu.Unlock()
	if paused {
		return ErrQueuePaused
	}

	select {
	case d.sem <- struct{}{}:
		// slot acquired
	default:
		return ErrConcurrencyCapReached
	}

	proc, err := d.runner.Start(ctx, promptText)
	if err != nil {
		<-d.sem // release the slot this failed spawn never used
		return err
	}

	hp := &headlessProc{proc: proc, startedAt: time.Now()}
	d.mu.Lock()
	d.running[item.ID] = hp
	d.mu.Unlock()

	go d.awaitExit(item.ID, hp)
	return nil
}

// awaitExit blocks for the child's exit and publishes HeadlessExitEvent
// unconditionally — success or failure, the event always fires, and no
// code path here ever issues a new Dispatch call in response. See
// design.md's "No retry anywhere in this path."
func (d *HeadlessDispatcher) awaitExit(itemID string, hp *headlessProc) {
	err := hp.proc.Wait()

	d.mu.Lock()
	alreadyReleased := hp.slotReleased
	hp.slotReleased = true
	delete(d.running, itemID)
	d.mu.Unlock()

	// releaseSlotIfZombie may have already freed this slot early (design.md
	// § Zombie interaction) — only release it here if that hasn't already
	// happened, so a zombied-then-actually-exited child never frees two
	// slots for one admission.
	if !alreadyReleased {
		<-d.sem
	}

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

// pause stops new admission (Dispatch returns ErrQueuePaused) without
// touching already-running children — design.md § Rate-limit backpressure:
// "Pausing stops NEW admission only... already-running children are never
// killed." Idempotent: calling pause while already paused does not stomp
// pausedSince/pauseSignal with a later observation, which is what lets
// daemon.go's onSnapshot call this unconditionally whenever
// Snapshot.RateLimitBanner is non-nil rather than needing its own
// check-then-set (avoiding a TOCTOU gap between reading d.paused and
// setting it).
func (d *HeadlessDispatcher) pause(signal *store.RateLimitSignal) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.paused {
		return
	}
	d.paused = true
	d.pausedSince = time.Now()
	d.pauseSignal = signal
}

// resume is the ONLY path back to admitting new headless dispatch after a
// rate-limit pause. Deliberately the sole mutator of d.paused back to
// false anywhere in this package — see daemon.go's Controller.Resume
// doc comment for why no timer or automatic caller may ever exist.
func (d *HeadlessDispatcher) resume() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.paused = false
	d.pausedSince = time.Time{}
	d.pauseSignal = nil
}

// releaseSlotIfZombie stops counting itemID against ActiveCount/the
// semaphore's logical capacity so a new item can be admitted — it does
// NOT call cmd.Process.Kill() (the underlying process may still be alive;
// killing it is a decision this proposal does not make unilaterally,
// matching the "never either signal alone, never automatic" caution
// wavetui-sessions already established for the zombie badge itself), it
// does NOT release the item's bd claim (the only release path for that is
// wavetui-sessions' existing one-key SessionsPane action), and it does NOT
// re-dispatch. This is a COMPLETELY SEPARATE mechanism from
// wavetui-sessions' `sources.ReleaseClaim` — that action clears the Store's
// SessionLink via a manual, one-key operator gesture and is the only way a
// bd claim is ever released; this method only ever decrements this
// package's own internal semaphore/map bookkeeping and is structurally
// incapable of calling bd, killing a process, or touching Item.Session —
// it takes only an itemID string, nothing else. See design.md § Zombie
// interaction.
//
// A no-op when itemID is not currently tracked (never dispatched headless,
// or its slot was already released by a prior call / its own natural
// exit) — safe to call on every Snapshot for every zombie-flagged item
// without first checking whether this dispatcher even knows about it.
func (d *HeadlessDispatcher) releaseSlotIfZombie(itemID string) {
	d.mu.Lock()
	hp, ok := d.running[itemID]
	if !ok || hp.slotReleased {
		d.mu.Unlock()
		return
	}
	hp.slotReleased = true
	delete(d.running, itemID) // stop counting toward ActiveCount immediately
	d.mu.Unlock()

	<-d.sem // free the semaphore slot for a new admission; the process
	// itself is left running untouched.
}
