// Tests for daemonController — tasks.md [4.2]: pause/resume state
// transitions (including a fixture asserting no code path resumes without
// an explicit resume() call) and zombie-slot-release accounting (confirms
// no process kill, no claim release call — both structurally impossible
// here, since releaseSlotIfZombie takes only an itemID and this package
// never imports anything bd-related or process-kill-capable).
package daemon

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/leonardoacosta/installfest/apps/wavetui/internal/store"
)

func TestOnSnapshotPausesOnRateLimitTransition(t *testing.T) {
	d, _, _ := newTestDispatcher(2)
	c := newDaemonController(d)
	ctx := context.Background()

	if err := d.Dispatch(ctx, store.Item{ID: "a"}, "/apply a"); err != nil {
		t.Fatalf("Dispatch before any rate-limit signal: %v", err)
	}

	c.onSnapshot(store.Snapshot{
		RateLimitBanner: &store.RateLimitSignal{Message: "rate limited"},
	})

	err := d.Dispatch(ctx, store.Item{ID: "b"}, "/apply b")
	if !errors.Is(err, ErrQueuePaused) {
		t.Fatalf("Dispatch after rate-limit snapshot = %v, want ErrQueuePaused", err)
	}
}

func TestOnSnapshotPauseIsIdempotent(t *testing.T) {
	d, _, _ := newTestDispatcher(2)
	c := newDaemonController(d)
	banner := &store.RateLimitSignal{Message: "rate limited"}

	c.onSnapshot(store.Snapshot{RateLimitBanner: banner})
	d.mu.Lock()
	firstPausedSince := d.pausedSince
	d.mu.Unlock()

	time.Sleep(5 * time.Millisecond)
	c.onSnapshot(store.Snapshot{RateLimitBanner: banner})
	d.mu.Lock()
	secondPausedSince := d.pausedSince
	d.mu.Unlock()

	if !firstPausedSince.Equal(secondPausedSince) {
		t.Fatalf("pausedSince changed across repeated onSnapshot calls: %v -> %v", firstPausedSince, secondPausedSince)
	}
}

func TestResumeRequiresExplicitCall(t *testing.T) {
	d, _, _ := newTestDispatcher(2)
	c := newDaemonController(d)
	ctx := context.Background()

	c.onSnapshot(store.Snapshot{RateLimitBanner: &store.RateLimitSignal{Message: "rate limited"}})

	// Simulate many Snapshots arriving over "wall-clock time" with the
	// signal cleared (RateLimitBanner nil again) and no resume() call —
	// admission must remain paused throughout. This is the fixture for
	// spec.md's "resume requires an explicit keypress, never a timer"
	// scenario: nothing about the passage of time or snapshot churn alone
	// may clear d.paused.
	for range 10 {
		c.onSnapshot(store.Snapshot{})
	}
	if err := d.Dispatch(ctx, store.Item{ID: "a"}, "/apply a"); !errors.Is(err, ErrQueuePaused) {
		t.Fatalf("Dispatch still paused after banner cleared with no resume() = %v, want ErrQueuePaused", err)
	}

	c.resume()
	if err := d.Dispatch(ctx, store.Item{ID: "a"}, "/apply a"); err != nil {
		t.Fatalf("Dispatch after explicit resume() = %v, want nil", err)
	}
}

func TestOnSnapshotReleasesZombieSlotWithoutKillingProcess(t *testing.T) {
	d, fr, _ := newTestDispatcher(2)
	c := newDaemonController(d)
	ctx := context.Background()

	if err := d.Dispatch(ctx, store.Item{ID: "a"}, "/apply a"); err != nil {
		t.Fatalf("Dispatch a: %v", err)
	}
	if err := d.Dispatch(ctx, store.Item{ID: "b"}, "/apply b"); err != nil {
		t.Fatalf("Dispatch b: %v", err)
	}
	if err := d.Dispatch(ctx, store.Item{ID: "c"}, "/apply c"); !errors.Is(err, ErrConcurrencyCapReached) {
		t.Fatalf("Dispatch c before zombie release = %v, want ErrConcurrencyCapReached", err)
	}

	c.onSnapshot(store.Snapshot{
		Items: []store.Item{
			{ID: "a", Session: &store.SessionLink{Zombie: true}},
			{ID: "b", Session: &store.SessionLink{Zombie: false}},
		},
	})

	// The zombied item's slot is freed, so a new admission now succeeds.
	if err := d.Dispatch(ctx, store.Item{ID: "c"}, "/apply c"); err != nil {
		t.Fatalf("Dispatch c after zombie release = %v, want nil", err)
	}

	// The underlying process is never killed: item "a"'s fakeWaiter is
	// still blocked in Wait() (finish was never called) — if
	// releaseSlotIfZombie had killed anything, there would be no live
	// waiter left to finish. Finishing it now must still work and still
	// publish an exit event, proving the "process" was left running.
	fr.waiterAt(0).finish(nil)
	waitFor(t, time.Second, func() bool {
		d.mu.Lock()
		defer d.mu.Unlock()
		_, stillRunning := d.running["a"]
		return !stillRunning
	})
}

func TestReleaseSlotIfZombieDoesNotDoubleRelease(t *testing.T) {
	d, fr, fb := newTestDispatcher(1)
	c := newDaemonController(d)
	ctx := context.Background()

	if err := d.Dispatch(ctx, store.Item{ID: "a"}, "/apply a"); err != nil {
		t.Fatalf("Dispatch a: %v", err)
	}

	snap := store.Snapshot{Items: []store.Item{{ID: "a", Session: &store.SessionLink{Zombie: true}}}}
	// Call onSnapshot twice with the same zombie-flagged item: the second
	// call must be a no-op (releaseSlotIfZombie's ok/slotReleased guard),
	// not a second `<-d.sem` receive on an already-drained channel (which
	// would deadlock this goroutine forever).
	c.onSnapshot(snap)
	c.onSnapshot(snap)

	// The slot was freed by the (single) zombie release, so a new
	// admission succeeds despite the original child never having exited.
	if err := d.Dispatch(ctx, store.Item{ID: "b"}, "/apply b"); err != nil {
		t.Fatalf("Dispatch b after zombie release = %v, want nil", err)
	}

	// Now let item "a"'s child actually finish. awaitExit must see
	// slotReleased already true and must NOT attempt a second `<-d.sem` —
	// if it did, this goroutine would deadlock and the waitFor below would
	// time out.
	fr.waiterAt(0).finish(nil)
	waitFor(t, time.Second, func() bool { return len(fb.exitEvents()) == 1 })
}
