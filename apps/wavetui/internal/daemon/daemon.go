package daemon

import (
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
}

// NewController constructs a Controller wrapping dispatcher.
func NewController(dispatcher *HeadlessDispatcher) *Controller {
	return &Controller{dispatcher: dispatcher}
}

// OnSnapshot is the single reactive entry point for both signals this batch
// consumes from Snapshot: rate-limit pause (design.md § Rate-limit
// backpressure) and zombie-slot-release (design.md § Zombie interaction).
// Both are pure reactions to the same Snapshot value, so neither needs its
// own watcher or a second copy of the fan-out over snap.Items.
func (c *Controller) OnSnapshot(snap store.Snapshot) {
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
