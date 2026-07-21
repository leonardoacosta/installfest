package daemon

import (
	"context"
	"errors"
	"sort"
	"sync"

	"github.com/leonardoacosta/installfest/apps/wavetui/internal/store"
)

// Controller reacts to Store snapshots the same reactive pattern every pane
// in this fleet already uses — no new watcher, no second transcript parse
// (design.md § Rate-limit backpressure). Its caller (cmd/wavetui/main.go,
// tasks.md [3.2], the UI batch) instantiates it alongside HeadlessDispatcher
// and feeds it each fresh Snapshot from the same bus subscriber that already
// pushes SnapshotMsg to the running Program — the same place every pane's
// own Update(Snapshot) call already happens.
//
// Exported (not daemonController/newDaemonController/onSnapshot/resume, as
// design.md's own pseudocode names it) because cmd/wavetui/main.go (package
// main) and internal/ui/headlessbar.go (package ui) both need to reach it —
// an unexported type in this package would be unreachable from either.
type Controller struct {
	dispatcher *HeadlessDispatcher

	// mu guards enabled. ToggleAdmission is called directly from
	// internal/ui/headlessbar.go's HandleKey on the bubbletea Update
	// goroutine (the exact same call shape Resume() below already uses),
	// while OnSnapshot runs on the bus subscriber's own goroutine (see
	// internal/bus/bus.go's Subscribe doc comment: "each subscriber is
	// delivered events on its own goroutine"). Those two goroutines read
	// and write enabled concurrently in the real running app, the same
	// reason HeadlessDispatcher itself guards paused/running with its own
	// mu rather than leaving them as bare fields.
	mu      sync.Mutex
	enabled bool
}

// NewController constructs a Controller wrapping dispatcher. enabled starts
// false (the zero value) — headless admission is opt-in, never on by
// default (spec.md: "no configuration flag or timer may enable admission").
func NewController(dispatcher *HeadlessDispatcher) *Controller {
	return &Controller{dispatcher: dispatcher}
}

// ToggleAdmission flips the admission toggle in response to a single
// explicit operator keypress (internal/ui/headlessbar.go's new keybinding,
// tasks.md [2.1]) — the same "single explicit-action method" precedent
// Resume() below already establishes: no config-only auto-start, no timer,
// just a direct flip called from the keybinding handler.
func (c *Controller) ToggleAdmission() {
	c.mu.Lock()
	c.enabled = !c.enabled
	c.mu.Unlock()
}

// OnSnapshot is the single reactive entry point for every signal this
// package reacts to on a fresh Snapshot: rate-limit pause (design.md §
// Rate-limit backpressure), zombie-slot-release (design.md § Zombie
// interaction), and — once admission is enabled — the headless admission
// loop (spec.md's "Controller.OnSnapshot dispatches eligible ready items
// when admission is enabled" Requirement). All three are pure reactions to
// the same Snapshot value, so none needs its own watcher or a second copy
// of the fan-out over snap.Items. ctx is threaded through from
// cmd/wavetui/main.go's own top-level context (the same one already passed
// to bus.Subscribe and tea.WithContext) — the admission loop's Dispatch
// calls need a context and this reuses the one call-site-adjacent source
// already in scope rather than manufacturing a fresh context.Background()
// here.
func (c *Controller) OnSnapshot(ctx context.Context, snap store.Snapshot) {
	// Rate-limit pause: HeadlessDispatcher.pause is itself idempotent (see
	// its doc comment), so this call site does not need its own
	// nil-transition guard beyond "a signal is currently present" — calling
	// pause repeatedly while snap.RateLimitBanner stays non-nil across
	// several Snapshots is a harmless no-op past the first.
	if snap.RateLimitBanner != nil {
		c.dispatcher.pause(snap.RateLimitBanner)
	}

	// Zombie-slot-release: consume the badge, free the slot, do nothing
	// else. releaseSlotIfZombie is a safe no-op for any item this
	// dispatcher never dispatched or has already released — see its own
	// doc comment for the "never kill, never touch the bd claim, never
	// re-dispatch" invariant this must never violate.
	for _, item := range snap.Items {
		if item.Session != nil && item.Session.Zombie {
			c.dispatcher.releaseSlotIfZombie(item.ID)
		}
	}

	c.mu.Lock()
	enabled := c.enabled
	c.mu.Unlock()
	if !enabled {
		return
	}
	c.admit(ctx, snap.Items)
}

// admit implements the admission loop: filter items for eligibility
// (Blocker == nil && Session == nil — the Store's existing
// unblocked/unclaimed definitions, no new readiness field), order eligible
// items by FanOutScore descending, and call dispatcher.Dispatch for each in
// that order until Dispatch reports the queue can't take any more this
// snapshot.
//
// Builds its own filtered/sorted COPY of items rather than sorting in
// place: snap (items' owning Snapshot) is the exact value
// cmd/wavetui/main.go forwards on to every pane via
// program.Send(ui.SnapshotMsg{Snapshot: snap}) immediately after
// OnSnapshot returns — sorting snap.Items itself would reorder what
// QueuePane renders as a side effect of this admission loop, which is not
// this loop's concern to change.
func (c *Controller) admit(ctx context.Context, items []store.Item) {
	eligible := make([]store.Item, 0, len(items))
	for _, item := range items {
		if item.Blocker == nil && item.Session == nil {
			eligible = append(eligible, item)
		}
	}
	sort.SliceStable(eligible, func(i, j int) bool {
		return eligible[i].FanOutScore > eligible[j].FanOutScore
	})

	for _, item := range eligible {
		err := c.dispatcher.Dispatch(ctx, item, composePrompt(item))
		if err == nil {
			continue
		}
		// ErrConcurrencyCapReached (spec.md's named stop condition) and
		// ErrQueuePaused are both refusal-shaped, not-a-failure sentinels
		// — see their own doc comments in headless_dispatcher.go ("this is
		// admission throttling, not a retry of a failed dispatch" /
		// "no spawn is attempted"). ErrQueuePaused reaches here whenever
		// this same OnSnapshot call just paused the dispatcher above (a
		// RateLimitBanner on this very Snapshot) — without this branch,
		// every remaining eligible item in the loop would otherwise
		// publish a spurious failure event for a condition that isn't a
		// per-item failure at all. Stop this snapshot's admission
		// silently: no failure event, no retry, no log noise. The next
		// Snapshot (not a timer, not a retry loop) is the next admission
		// opportunity.
		if errors.Is(err, ErrConcurrencyCapReached) || errors.Is(err, ErrQueuePaused) {
			return
		}
		// A genuine Dispatch error (e.g. the runner itself failed to
		// spawn) surfaces the same way a manual dispatch failure already
		// does for the interactive Start action
		// (internal/ui/queuepane.go's startSelected -> setDispatchBadge):
		// it does not vanish silently. HeadlessDispatcher has no per-row
		// UI badge of its own to render into, so this reuses the ONE
		// existing bus event HeadlessDispatcher already publishes about a
		// dispatched item's outcome — HeadlessExitEvent (see its own doc
		// comment: "the ONLY output HeadlessDispatcher ever produces
		// about a running child") — rather than inventing a second event
		// type for the same concept. The loop continues to the next
		// eligible item: unlike the two refusal sentinels above, a
		// synchronous spawn error is scoped to this one item's attempt,
		// not a systemic reason every other item would also fail.
		c.dispatcher.bus.Publish(HeadlessExitEvent{ItemID: item.ID, Err: err, Failed: true})
	}
}

// Resume is the ONLY path back to admitting new headless dispatch after a
// rate-limit pause — called directly by internal/ui/headlessbar.go's resume
// keybinding (tasks.md [3.1]) in response to an explicit operator keypress,
// with no intermediate scheduling. There is deliberately no timer, no
// backoff loop, and no other caller of HeadlessDispatcher.resume anywhere
// in this package: design.md § Rate-limit backpressure — "Resume is
// exclusively an operator keypress — never a timer." A timer-based
// auto-resume risks immediately re-triggering the same rate limit the
// moment it fires, since the underlying condition may not have actually
// cleared just because some wall-clock duration elapsed.
func (c *Controller) Resume() {
	c.dispatcher.resume()
}
