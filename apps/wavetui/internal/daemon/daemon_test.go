// Tests for Controller — tasks.md [4.2]: pause/resume state
// transitions (including a fixture asserting no code path resumes without
// an explicit Resume() call) and zombie-slot-release accounting (confirms
// no process kill, no claim release call — both structurally impossible
// here, since releaseSlotIfZombie takes only an itemID and this package
// never imports anything bd-related or process-kill-capable).
package daemon

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/leonardoacosta/installfest/apps/wavetui/internal/bus"
	"github.com/leonardoacosta/installfest/apps/wavetui/internal/store"
)

func TestOnSnapshotPausesOnRateLimitTransition(t *testing.T) {
	d, _, _ := newTestDispatcher(2)
	c := NewController(d)
	ctx := context.Background()

	if err := d.Dispatch(ctx, store.Item{ID: "a"}, "/apply a"); err != nil {
		t.Fatalf("Dispatch before any rate-limit signal: %v", err)
	}

	c.OnSnapshot(store.Snapshot{
		RateLimitBanner: &store.RateLimitSignal{Message: "rate limited"},
	})

	err := d.Dispatch(ctx, store.Item{ID: "b"}, "/apply b")
	if !errors.Is(err, ErrQueuePaused) {
		t.Fatalf("Dispatch after rate-limit snapshot = %v, want ErrQueuePaused", err)
	}
}

func TestOnSnapshotPauseIsIdempotent(t *testing.T) {
	d, _, _ := newTestDispatcher(2)
	c := NewController(d)
	banner := &store.RateLimitSignal{Message: "rate limited"}

	c.OnSnapshot(store.Snapshot{RateLimitBanner: banner})
	d.mu.Lock()
	firstPausedSince := d.pausedSince
	d.mu.Unlock()

	time.Sleep(5 * time.Millisecond)
	c.OnSnapshot(store.Snapshot{RateLimitBanner: banner})
	d.mu.Lock()
	secondPausedSince := d.pausedSince
	d.mu.Unlock()

	if !firstPausedSince.Equal(secondPausedSince) {
		t.Fatalf("pausedSince changed across repeated onSnapshot calls: %v -> %v", firstPausedSince, secondPausedSince)
	}
}

func TestResumeRequiresExplicitCall(t *testing.T) {
	d, _, _ := newTestDispatcher(2)
	c := NewController(d)
	ctx := context.Background()

	c.OnSnapshot(store.Snapshot{RateLimitBanner: &store.RateLimitSignal{Message: "rate limited"}})

	// Simulate many Snapshots arriving over "wall-clock time" with the
	// signal cleared (RateLimitBanner nil again) and no resume() call —
	// admission must remain paused throughout. This is the fixture for
	// spec.md's "resume requires an explicit keypress, never a timer"
	// scenario: nothing about the passage of time or snapshot churn alone
	// may clear d.paused.
	for range 10 {
		c.OnSnapshot(store.Snapshot{})
	}
	if err := d.Dispatch(ctx, store.Item{ID: "a"}, "/apply a"); !errors.Is(err, ErrQueuePaused) {
		t.Fatalf("Dispatch still paused after banner cleared with no resume() = %v, want ErrQueuePaused", err)
	}

	c.Resume()
	if err := d.Dispatch(ctx, store.Item{ID: "a"}, "/apply a"); err != nil {
		t.Fatalf("Dispatch after explicit resume() = %v, want nil", err)
	}
}

func TestOnSnapshotReleasesZombieSlotWithoutKillingProcess(t *testing.T) {
	d, fr, _ := newTestDispatcher(2)
	c := NewController(d)
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

	c.OnSnapshot(store.Snapshot{
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
	c := NewController(d)
	ctx := context.Background()

	if err := d.Dispatch(ctx, store.Item{ID: "a"}, "/apply a"); err != nil {
		t.Fatalf("Dispatch a: %v", err)
	}

	snap := store.Snapshot{Items: []store.Item{{ID: "a", Session: &store.SessionLink{Zombie: true}}}}
	// Call onSnapshot twice with the same zombie-flagged item: the second
	// call must be a no-op (releaseSlotIfZombie's ok/slotReleased guard),
	// not a second `<-d.sem` receive on an already-drained channel (which
	// would deadlock this goroutine forever).
	c.OnSnapshot(snap)
	c.OnSnapshot(snap)

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

// TestControllerPublishesHeadlessQueueStateToStore is the real end-to-end
// proof that a pause/resume triggered through Controller actually reaches
// store.Store.Snapshot().HeadlessQueue — the field HeadlessBar.Update (and
// its resume keybinding guard) reads. Every other test in this file uses
// fakeBus, a recording double that never forwards to a real Store, so none
// of them could have caught the post-wave gate finding: HeadlessDispatcher
// flipped its own internal paused/running state correctly, but never
// published anything, so Snapshot.HeadlessQueue stayed permanently nil and
// HeadlessBar's resume keypress (internal/ui/headlessbar.go's HandleKey
// guard `h.queue == nil || !h.queue.Paused`) was unreachable from a real
// running app. This test wires a real bus.Bus to a real store.Store the
// same way cmd/wavetui/main.go does, so only a genuine end-to-end fix can
// pass it.
func TestControllerPublishesHeadlessQueueStateToStore(t *testing.T) {
	b := bus.New()
	st := store.New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b.Subscribe(ctx, func(ev bus.Event) { st.Apply(ev) })

	d := NewHeadlessDispatcher(2, b)
	c := NewController(d)

	// Trigger a pause via the Controller — the same call
	// cmd/wavetui/main.go's bus subscriber makes on every fresh Snapshot
	// (design.md § Rate-limit backpressure) — by simulating a
	// RateLimitBanner snapshot, exactly what a real TranscriptSource-detected
	// rate limit looks like from the Controller's point of view.
	c.OnSnapshot(store.Snapshot{
		RateLimitBanner: &store.RateLimitSignal{Message: "rate limited (test)"},
	})

	waitFor(t, time.Second, func() bool {
		snap := st.Snapshot()
		return snap.HeadlessQueue != nil && snap.HeadlessQueue.Paused
	})
	snap := st.Snapshot()
	if snap.HeadlessQueue == nil || !snap.HeadlessQueue.Paused {
		t.Fatalf("Store.Snapshot().HeadlessQueue after Controller-triggered pause = %+v, want non-nil Paused=true", snap.HeadlessQueue)
	}
	t.Logf("EVIDENCE: real Store.Snapshot().HeadlessQueue = %+v after Controller.OnSnapshot(rate-limit banner) — this is what HeadlessBar.Update actually reads", snap.HeadlessQueue)

	// Resume via the exact call HeadlessBar.HandleKey makes on a real "r"
	// keypress (internal/ui/headlessbar.go) — no timer, no scheduling.
	c.Resume()

	waitFor(t, time.Second, func() bool {
		snap := st.Snapshot()
		return snap.HeadlessQueue != nil && !snap.HeadlessQueue.Paused
	})
	snap = st.Snapshot()
	if snap.HeadlessQueue == nil || snap.HeadlessQueue.Paused {
		t.Fatalf("Store.Snapshot().HeadlessQueue after Controller.Resume() = %+v, want Paused=false", snap.HeadlessQueue)
	}
	t.Logf("EVIDENCE: real Store.Snapshot().HeadlessQueue = %+v after Controller.Resume() — the real keypress path updates the Store, not just the dispatcher's own internal field", snap.HeadlessQueue)
}
