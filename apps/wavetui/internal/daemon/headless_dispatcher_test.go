// Tests for HeadlessDispatcher — tasks.md [4.2]: semaphore admission/
// refusal/slot-release, composePrompt fresh-vs-continue, and
// HeadlessExitEvent success/failure publishing. Hermetic throughout: no
// call in this file ever spawns a real `claude` binary — fakeRunner/
// fakeWaiter stand in for os/exec.
package daemon

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/leonardoacosta/installfest/apps/wavetui/internal/bus"
	"github.com/leonardoacosta/installfest/apps/wavetui/internal/store"
)

// fakeWaiter implements waiter for hermetic tests: Wait blocks until
// finish is called, then returns whatever error finish was given.
type fakeWaiter struct {
	done chan struct{}
	err  error
}

func newFakeWaiter() *fakeWaiter {
	return &fakeWaiter{done: make(chan struct{})}
}

func (w *fakeWaiter) Wait() error {
	<-w.done
	return w.err
}

func (w *fakeWaiter) finish(err error) {
	w.err = err
	close(w.done)
}

// fakeRunner implements headlessRunner for hermetic tests — never invokes a
// real claude binary. When startErr is non-nil every Start call fails
// (Dispatch's synchronous spawn-failure case); otherwise each Start call
// returns a fresh fakeWaiter, recorded in waiters in call order so a test
// can control exactly when/how each spawned "child" exits.
type fakeRunner struct {
	mu       sync.Mutex
	startErr error
	prompts  []string
	waiters  []*fakeWaiter
}

func (r *fakeRunner) Start(_ context.Context, promptText string) (waiter, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.prompts = append(r.prompts, promptText)
	if r.startErr != nil {
		return nil, r.startErr
	}
	w := newFakeWaiter()
	r.waiters = append(r.waiters, w)
	return w, nil
}

func (r *fakeRunner) promptCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.prompts)
}

func (r *fakeRunner) waiterAt(i int) *fakeWaiter {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.waiters[i]
}

// fakeBus implements EventBus for hermetic tests — records every published
// event instead of fanning out to real subscriber goroutines.
type fakeBus struct {
	mu     sync.Mutex
	events []bus.Event
}

func (b *fakeBus) Publish(ev bus.Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = append(b.events, ev)
}

func (b *fakeBus) exitEvents() []HeadlessExitEvent {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []HeadlessExitEvent
	for _, ev := range b.events {
		if e, ok := ev.(HeadlessExitEvent); ok {
			out = append(out, e)
		}
	}
	return out
}

// waitFor polls cond until true or the timeout elapses, failing the test
// otherwise — awaitExit runs on its own goroutine, so tests observing its
// side effects (map/semaphore/bus state) must not assert immediately.
func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	if !cond() {
		t.Fatalf("condition not met within %s", timeout)
	}
}

func newTestDispatcher(cap int) (*HeadlessDispatcher, *fakeRunner, *fakeBus) {
	fb := &fakeBus{}
	d := NewHeadlessDispatcher(cap, fb)
	fr := &fakeRunner{}
	d.runner = fr
	return d, fr, fb
}

func TestComposePrompt(t *testing.T) {
	cases := []struct {
		name string
		item store.Item
		want string
	}{
		{
			name: "nil TaskProgress dispatches fresh",
			item: store.Item{ID: "if-abcd"},
			want: "/apply if-abcd",
		},
		{
			name: "zero Done dispatches fresh",
			item: store.Item{ID: "if-abcd", TaskProgress: &store.TaskProgress{Done: 0, Total: 5}},
			want: "/apply if-abcd",
		},
		{
			name: "Done > 0 dispatches --continue",
			item: store.Item{ID: "if-abcd", TaskProgress: &store.TaskProgress{Done: 2, Total: 5}},
			want: "/apply if-abcd --continue",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := composePrompt(tc.item); got != tc.want {
				t.Fatalf("composePrompt() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDispatchAdmitsUpToCapThenRefuses(t *testing.T) {
	d, fr, _ := newTestDispatcher(2)
	ctx := context.Background()

	if err := d.Dispatch(ctx, store.Item{ID: "a"}, "/apply a"); err != nil {
		t.Fatalf("first Dispatch: %v", err)
	}
	if err := d.Dispatch(ctx, store.Item{ID: "b"}, "/apply b"); err != nil {
		t.Fatalf("second Dispatch: %v", err)
	}
	err := d.Dispatch(ctx, store.Item{ID: "c"}, "/apply c")
	if !errors.Is(err, ErrConcurrencyCapReached) {
		t.Fatalf("third Dispatch = %v, want ErrConcurrencyCapReached", err)
	}
	if got := fr.promptCount(); got != 2 {
		t.Fatalf("runner.Start called %d times, want 2 (third dispatch must not spawn)", got)
	}
}

func TestSlotFreesOnChildExit(t *testing.T) {
	d, fr, _ := newTestDispatcher(2)
	ctx := context.Background()

	if err := d.Dispatch(ctx, store.Item{ID: "a"}, "/apply a"); err != nil {
		t.Fatalf("Dispatch a: %v", err)
	}
	if err := d.Dispatch(ctx, store.Item{ID: "b"}, "/apply b"); err != nil {
		t.Fatalf("Dispatch b: %v", err)
	}
	if err := d.Dispatch(ctx, store.Item{ID: "c"}, "/apply c"); !errors.Is(err, ErrConcurrencyCapReached) {
		t.Fatalf("Dispatch c before any exit = %v, want ErrConcurrencyCapReached", err)
	}

	// Finish item "a"'s child successfully — its slot must free.
	fr.waiterAt(0).finish(nil)

	waitFor(t, time.Second, func() bool {
		return d.Dispatch(ctx, store.Item{ID: "c"}, "/apply c") == nil
	})
}

func TestDispatchRefusedWhilePaused(t *testing.T) {
	d, fr, _ := newTestDispatcher(2)
	ctx := context.Background()
	d.pause(&store.RateLimitSignal{Message: "rate limited"})

	err := d.Dispatch(ctx, store.Item{ID: "a"}, "/apply a")
	if !errors.Is(err, ErrQueuePaused) {
		t.Fatalf("Dispatch while paused = %v, want ErrQueuePaused", err)
	}
	if got := fr.promptCount(); got != 0 {
		t.Fatalf("runner.Start called %d times while paused, want 0 (no spawn attempt)", got)
	}
}

func TestSpawnFailureReleasesSlot(t *testing.T) {
	d, fr, _ := newTestDispatcher(1)
	fr.startErr = errors.New("exec: fork/exec claude: no such file or directory")
	ctx := context.Background()

	if err := d.Dispatch(ctx, store.Item{ID: "a"}, "/apply a"); err == nil {
		t.Fatal("Dispatch with a failing runner returned nil error, want the spawn error")
	}

	// The failed spawn must not have permanently consumed the only slot —
	// clear startErr and confirm a subsequent Dispatch can still admit.
	fr.startErr = nil
	if err := d.Dispatch(ctx, store.Item{ID: "b"}, "/apply b"); err != nil {
		t.Fatalf("Dispatch after a spawn failure = %v, want nil (slot must have been released)", err)
	}
}

func TestAwaitExitPublishesSuccessEvent(t *testing.T) {
	d, fr, fb := newTestDispatcher(1)
	ctx := context.Background()
	if err := d.Dispatch(ctx, store.Item{ID: "a"}, "/apply a"); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	fr.waiterAt(0).finish(nil)

	waitFor(t, time.Second, func() bool { return len(fb.exitEvents()) == 1 })
	ev := fb.exitEvents()[0]
	if ev.ItemID != "a" || ev.Failed || ev.ExitCode != 0 || ev.Err != nil {
		t.Fatalf("unexpected success event: %+v", ev)
	}
}

func TestAwaitExitPublishesFailureEvent(t *testing.T) {
	d, fr, fb := newTestDispatcher(1)
	ctx := context.Background()
	if err := d.Dispatch(ctx, store.Item{ID: "a"}, "/apply a"); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	fr.waiterAt(0).finish(errors.New("boom"))

	waitFor(t, time.Second, func() bool { return len(fb.exitEvents()) == 1 })
	ev := fb.exitEvents()[0]
	if ev.ItemID != "a" || !ev.Failed || ev.Err == nil {
		t.Fatalf("unexpected failure event: %+v", ev)
	}

	// No automatic retry: nothing in this package re-dispatches item "a" in
	// response to its own failure event. Give any latent (incorrect) retry
	// goroutine a generous window to fire, then assert Start was still only
	// called once.
	time.Sleep(50 * time.Millisecond)
	if got := fr.promptCount(); got != 1 {
		t.Fatalf("runner.Start called %d times after a failed exit, want 1 (no automatic retry)", got)
	}
}
