package daemon

import (
	"github.com/leonardoacosta/installfest/apps/wavetui/internal/store"
)

// daemonController reacts to Store snapshots the same reactive pattern
// every pane in this fleet already uses — no new watcher, no second
// transcript parse (design.md § Rate-limit backpressure). Its caller
// (cmd/wavetui/main.go, tasks.md [3.2] — a later batch) is what feeds it
// each fresh Snapshot, the same place every pane's own Update(Snapshot)
// call already happens.
type daemonController struct {
	dispatcher *HeadlessDispatcher
}

// newDaemonController constructs a daemonController wrapping dispatcher.
func newDaemonController(dispatcher *HeadlessDispatcher) *daemonController {
	return &daemonController{dispatcher: dispatcher}
}

// onSnapshot is the single reactive entry point for both signals this
// batch consumes from Snapshot: rate-limit pause (design.md § Rate-limit
// backpressure) and zombie-slot-release (design.md § Zombie interaction).
// Both are pure reactions to the same Snapshot value, so neither needs its
// own watcher or a second copy of the fan-out over snap.Items.
func (c *daemonController) onSnapshot(snap store.Snapshot) {
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

// resume is the ONLY path back to admitting new headless dispatch after a
// rate-limit pause — exposed for internal/ui/headlessbar.go's resume
// keybinding (tasks.md [3.1], a later batch) to call directly in response
// to an explicit operator keypress. There is deliberately no timer, no
// backoff loop, and no other caller of HeadlessDispatcher.resume anywhere
// in this package: design.md § Rate-limit backpressure — "Resume is
// exclusively an operator keypress — never a timer." A timer-based
// auto-resume risks immediately re-triggering the same rate limit the
// moment it fires, since the underlying condition may not have actually
// cleared just because some wall-clock duration elapsed.
func (c *daemonController) resume() {
	c.dispatcher.resume()
}
