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
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"sync"
	"time"

	"github.com/leonardoacosta/installfest/apps/wavetui/internal/bus"
	"github.com/leonardoacosta/installfest/apps/wavetui/internal/config"
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

// HeadlessPIDEntry is one persisted record of a live (or believed-live)
// headless child — enough for an operator to identify and reap it from the
// process table after wavetui itself has crashed and is no longer around to
// do that automatically (if-ugxa.1: "an operator has no way to discover or
// clean them up short of manually scanning the process table for stray
// claude -p invocations"). PID alone is not enough context to act on
// safely — a bare number is meaningless once you're staring at `ps aux` — so
// ItemID and StartedAt travel with it.
type HeadlessPIDEntry struct {
	ItemID    string    `json:"item_id"`
	PID       int       `json:"pid"`
	StartedAt time.Time `json:"started_at"`
}

// HeadlessDispatcherOption configures optional HeadlessDispatcher behavior
// at construction time. The only option today is WithPIDFile; the variadic
// signature on NewHeadlessDispatcher exists so adding a second option later
// never breaks an existing two-argument call site.
type HeadlessDispatcherOption func(*HeadlessDispatcher)

// WithPIDFile enables on-disk PID-file persistence at path: every headless
// child gains a HeadlessPIDEntry there the moment it starts, and loses it
// the moment awaitExit observes its real exit (see registerPID/
// unregisterPID). Omitting this option (the default for every existing
// test in this package) disables persistence entirely — no file is ever
// read or written, which is exactly why the many hermetic
// fakeRunner/fakeWaiter-based tests elsewhere in this package needed no
// changes to keep working. cmd/wavetui/main.go is the one production call
// site, passing a project-root-relative path beside .wavetui.toml — the
// same convention waveFileName already established for
// .wavetui-wave.json.
func WithPIDFile(path string) HeadlessDispatcherOption {
	return func(d *HeadlessDispatcher) { d.pidFilePath = path }
}

// LoadHeadlessPIDFile reads path (as written by writePIDFileLocked) and
// returns its entries. A missing file is not an error — it is the expected
// case on a fresh project or after a clean shutdown that left nothing
// behind — and returns (nil, nil), the same tolerant-missing-file
// convention config.Load already uses for .wavetui.toml. Exported so a
// future standalone cleanup path (e.g. a `wavetui --cleanup-orphans` flag,
// out of scope for this bead — see NewHeadlessDispatcher's doc comment on
// StalePIDsAtStartup) can read the file independently of ever constructing
// a HeadlessDispatcher, and so this package's own tests can assert on
// exactly what landed on disk.
func LoadHeadlessPIDFile(path string) ([]HeadlessPIDEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if len(data) == 0 {
		return nil, nil
	}
	var entries []HeadlessPIDEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	return entries, nil
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

	// pidFilePath, pidEntries, and stalePIDs back the WithPIDFile option
	// (if-ugxa.1). pidFilePath == "" (the default, unless WithPIDFile was
	// passed to NewHeadlessDispatcher) disables all of it — registerPID/
	// unregisterPID become no-ops and no file is ever touched.
	pidFilePath string
	// pidEntries mirrors `running` but is deliberately a SEPARATE map with
	// its own lifecycle: entries are added in Dispatch (same moment as
	// `running`) but removed ONLY by awaitExit's real Wait() completion —
	// never by releaseSlotIfZombie, which stops counting an item toward
	// ActiveCount/the semaphore without any claim the underlying OS process
	// has actually exited (see releaseSlotIfZombie's own doc comment: "the
	// underlying process may still be alive"). Deriving the PID file from
	// `running` directly would silently drop a genuinely-still-running
	// zombie-released child from the very file this bead exists to keep
	// accurate — exactly the discoverability gap being closed.
	pidEntries map[string]HeadlessPIDEntry
	// stalePIDs is populated once, at construction, from whatever
	// pidFilePath already contained (e.g. a prior crashed run) — see
	// StalePIDsAtStartup.
	stalePIDs []HeadlessPIDEntry
}

// NewHeadlessDispatcher constructs a HeadlessDispatcher bounded to cap
// concurrent children, publishing onto b. cap <= 0 is defensively floored
// to 1 rather than producing a permanently-zero-capacity semaphore that can
// never admit anything — the real default resolution (2, vs an operator
// override) happens one layer up, in config.Config's own
// EffectiveHeadlessConcurrencyCap; by the time cap reaches this
// constructor it is expected to already be that resolved, positive value.
//
// If a WithPIDFile option is passed and the named file already contains
// entries (a prior process's children that were never cleanly reaped —
// if-ugxa.1's whole reason for existing), they are loaded into
// StalePIDsAtStartup for the caller to surface, NOT merged into this
// instance's own live bookkeeping — this fresh dispatcher never dispatched
// them, so it has no basis to track or release them. The file is left
// on disk untouched until this instance's own first Dispatch/awaitExit
// rewrites it with its own (accurate, from-scratch) state.
func NewHeadlessDispatcher(cap int, b EventBus, opts ...HeadlessDispatcherOption) *HeadlessDispatcher {
	if cap <= 0 {
		cap = 1
	}
	d := &HeadlessDispatcher{
		sem:        make(chan struct{}, cap),
		bus:        b,
		runner:     execHeadlessRunner{},
		running:    make(map[string]*headlessProc),
		pidEntries: make(map[string]HeadlessPIDEntry),
	}
	for _, opt := range opts {
		opt(d)
	}
	if d.pidFilePath != "" {
		if stale, err := LoadHeadlessPIDFile(d.pidFilePath); err == nil {
			d.stalePIDs = stale
		}
		// A read error here (malformed JSON, permission denied) is not
		// fatal to constructing a working dispatcher — it just means no
		// stale-PID note can be surfaced this run, same
		// tolerant-toward-a-bad-file posture config.Load already takes
		// toward a malformed .wavetui.toml line.
	}
	return d
}

// StalePIDsAtStartup returns any headless-child PID entries this
// dispatcher found already recorded in its PID file at construction time —
// evidence of a prior process's children that were never cleanly reaped
// (most likely because the daemon itself crashed or was killed before
// awaitExit could run). Empty whenever WithPIDFile was not used, the file
// was absent, or it was empty.
//
// Scope decision for if-ugxa.1: this accessor plus a construction-time
// read is as far as this bead goes. cmd/wavetui/main.go uses it to print a
// one-line stderr note (matching main.go's existing "wavetui: ..."
// diagnostic convention) so a stale entry is never silently invisible.
// Actually killing/reaping those PIDs, or surfacing them as a dedicated UI
// banner/pane, is deliberately left to a future follow-up (the bead's own
// "not required now" `wavetui --cleanup-orphans` flag) — deciding whether
// a found-but-possibly-still-running process is safe to kill is an
// operator judgment call this package should not make unilaterally, the
// same caution releaseSlotIfZombie's doc comment already applies to a
// live-but-zombied item.
func (d *HeadlessDispatcher) StalePIDsAtStartup() []HeadlessPIDEntry {
	return d.stalePIDs
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
	d.registerPID(item.ID, extractPID(proc), hp.startedAt)
	d.publishState()

	go d.awaitExit(item.ID, hp)
	return nil
}

// extractPID best-effort recovers the OS PID of a just-started child for
// the PID-file entry. proc is a waiter — deliberately the narrowest
// interface Dispatch needs (just Wait() error) so hermetic tests can inject
// a fakeWaiter instead of a real process — so this cannot be a method on
// the interface itself; it type-switches on the one concrete type real
// runners actually produce (execHeadlessRunner and this package's own
// e2e-test realProcRunner/harnessRunner all return a bare *exec.Cmd as
// waiter). A fakeWaiter (every hermetic test in headless_dispatcher_test.go
// and daemon_test.go) falls through to 0 — there is no real OS process to
// record a PID for, and none of those tests pass WithPIDFile anyway, so the
// 0 is never written anywhere.
func extractPID(proc waiter) int {
	if cmd, ok := proc.(*exec.Cmd); ok && cmd.Process != nil {
		return cmd.Process.Pid
	}
	return 0
}

// registerPID adds itemID's PID-file entry and persists it — see
// pidEntries' doc comment for why this is a separate map from `running`,
// and WithPIDFile's doc comment for why pidFilePath == "" makes this a
// no-op (the overwhelming majority of this package's own tests).
func (d *HeadlessDispatcher) registerPID(itemID string, pid int, startedAt time.Time) {
	if d.pidFilePath == "" {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.pidEntries[itemID] = HeadlessPIDEntry{ItemID: itemID, PID: pid, StartedAt: startedAt}
	d.writePIDFileLocked()
}

// unregisterPID removes itemID's PID-file entry and persists it. Called
// ONLY from awaitExit's real Wait() completion — never from
// releaseSlotIfZombie, which does not (and must not) imply the underlying
// process has actually exited. See pidEntries' doc comment.
func (d *HeadlessDispatcher) unregisterPID(itemID string) {
	if d.pidFilePath == "" {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.pidEntries, itemID)
	d.writePIDFileLocked()
}

// writePIDFileLocked snapshots d.pidEntries and atomically rewrites
// d.pidFilePath via config.AtomicWriteFile — the same temp-file+rename
// helper the codebase already uses, not a bespoke write (per the reader/
// reuse gate). Caller MUST hold d.mu. Unlike publishState (which
// deliberately releases d.mu before its bus.Publish call, so a slow
// subscriber can never block on this dispatcher's own mutex — see
// publishState's doc comment), holding the lock across this write is safe
// and intentional: AtomicWriteFile is a local, fast, no-callback file
// write, and holding the lock is the simplest way to keep two concurrent
// register/unregister calls from racing each other into an out-of-order
// (older-overwrites-newer) file write.
//
// A marshal or write failure is logged to stderr rather than returned —
// Dispatch/awaitExit's own contracts (admission decision / exit
// notification) must never fail or block because a best-effort operator
// convenience file couldn't be written (e.g. a full disk or a read-only
// project directory); the child itself is unaffected either way. This
// mirrors cmd/wavetui/main.go's own top-level "wavetui: <err>" stderr
// convention for surfacing a non-fatal problem instead of swallowing it
// silently.
func (d *HeadlessDispatcher) writePIDFileLocked() {
	entries := make([]HeadlessPIDEntry, 0, len(d.pidEntries))
	for _, e := range d.pidEntries {
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ItemID < entries[j].ItemID })

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "wavetui: headless pid-file marshal failed: %v\n", err)
		return
	}
	if err := config.AtomicWriteFile(d.pidFilePath, data, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "wavetui: headless pid-file persist failed (%s): %v\n", d.pidFilePath, err)
	}
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

	// This is the ONLY place the PID-file entry is ever removed (if-ugxa.1)
	// — a real Wait() return is the one honest signal the OS process is
	// actually gone. Deliberately unconditional (unlike the semaphore
	// release below): even if releaseSlotIfZombie already stopped counting
	// this item toward ActiveCount, its PID-file entry stays until this
	// point proves the process itself is done.
	d.unregisterPID(itemID)

	// releaseSlotIfZombie may have already freed this slot early (design.md
	// § Zombie interaction) — only release it here if that hasn't already
	// happened, so a zombied-then-actually-exited child never frees two
	// slots for one admission.
	if !alreadyReleased {
		<-d.sem
	}
	d.publishState()

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

// publishState publishes the dispatcher's current pause/admission-count
// state as a store.HeadlessQueueStateEvent onto the bus — the ONLY path by
// which Snapshot.HeadlessQueue (store.go's Additive Snapshot field) ever
// reflects real dispatcher state; without this, Store.Apply never sees a
// HeadlessQueueStateEvent and Snapshot.HeadlessQueue stays permanently nil,
// which is exactly the bug this method fixes. Called after every operation
// that changes pause/admission-count state: pause(), resume(), Dispatch's
// successful admission, and both slot-release paths (awaitExit's normal
// completion and releaseSlotIfZombie's early release) — see each call site.
// Must be called WITHOUT d.mu held: it acquires the lock itself to read a
// consistent snapshot, then publishes after releasing it, so a slow/blocked
// subscriber can never hold this dispatcher's own mutex.
func (d *HeadlessDispatcher) publishState() {
	d.mu.Lock()
	st := store.HeadlessQueueState{
		Enabled:        true,
		ConcurrencyCap: cap(d.sem),
		ActiveCount:    len(d.running),
		Paused:         d.paused,
		PausedSince:    d.pausedSince,
		PauseSignal:    d.pauseSignal,
	}
	d.mu.Unlock()
	d.bus.Publish(store.HeadlessQueueStateEvent{State: st})
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
	if d.paused {
		d.mu.Unlock()
		return
	}
	d.paused = true
	d.pausedSince = time.Now()
	d.pauseSignal = signal
	d.mu.Unlock()
	d.publishState()
}

// resume is the ONLY path back to admitting new headless dispatch after a
// rate-limit pause. Deliberately the sole mutator of d.paused back to
// false anywhere in this package — see daemon.go's Controller.Resume
// doc comment for why no timer or automatic caller may ever exist.
func (d *HeadlessDispatcher) resume() {
	d.mu.Lock()
	d.paused = false
	d.pausedSince = time.Time{}
	d.pauseSignal = nil
	d.mu.Unlock()
	d.publishState()
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
//
// Deliberately never calls unregisterPID (if-ugxa.1): this method's whole
// contract is "the process may still be alive," so clearing its PID-file
// entry here would recreate the exact orphan-discoverability gap that
// mechanism exists to close. The entry is removed only by awaitExit's own
// real Wait() completion, whenever (if ever) that happens.
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
	d.publishState()
}
