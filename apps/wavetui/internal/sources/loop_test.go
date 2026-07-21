package sources

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// TestRequeryLoopCoalescesTriggers proves the serialize+coalesce contract
// requeryLoop exists for: a burst of Trigger() calls that arrive while a
// call is already in flight must produce exactly one follow-up call, never
// one per Trigger.
func TestRequeryLoopCoalescesTriggers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	loop := newRequeryLoop()
	var calls atomic.Int32
	block := make(chan struct{})
	entered := make(chan struct{}, 1)

	go loop.Run(ctx, func(context.Context) {
		calls.Add(1)
		select {
		case entered <- struct{}{}:
		default:
		}
		<-block
	})

	loop.Trigger()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("first call never started")
	}

	// Fire a burst of triggers while the first call is still in flight.
	for i := 0; i < 50; i++ {
		loop.Trigger()
	}

	close(block) // let the first call return; the coalesced follow-up runs next

	deadline := time.Now().Add(time.Second)
	for calls.Load() != 2 && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}

	// Settle a little longer to make sure it does NOT keep climbing past 2.
	time.Sleep(50 * time.Millisecond)
	if got := calls.Load(); got != 2 {
		t.Fatalf("calls = %d, want exactly 2 (1 in-flight + 1 coalesced follow-up)", got)
	}
}

// TestDebounceFiresOnceAfterTrailingQuiet proves the trailing-edge debounce
// contract: a burst of signals well inside the debounce window collapses
// into exactly one fire, once the burst goes quiet.
func TestDebounceFiresOnceAfterTrailingQuiet(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	in := make(chan struct{}, 1)
	var fires atomic.Int32
	go debounce(ctx, in, 60*time.Millisecond, func() { fires.Add(1) })

	for i := 0; i < 10; i++ {
		select {
		case in <- struct{}{}:
		default:
		}
		time.Sleep(3 * time.Millisecond)
	}

	time.Sleep(300 * time.Millisecond)
	if got := fires.Load(); got != 1 {
		t.Fatalf("fires = %d, want exactly 1 for a burst inside the debounce window", got)
	}
}

// TestDebounceStopsOnContextCancel proves the debounce goroutine actually
// exits when ctx is done, rather than leaking (task 2.4's audit concern).
func TestDebounceStopsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	in := make(chan struct{}, 1)
	done := make(chan struct{})

	go func() {
		debounce(ctx, in, time.Hour, func() {})
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("debounce did not return after ctx cancellation")
	}
}

func TestBackoffDelay(t *testing.T) {
	cap := 15 * time.Second
	cases := []struct {
		failCount int
		want      time.Duration
	}{
		{0, 0},
		{1, time.Second},
		{2, 2 * time.Second},
		{3, 4 * time.Second},
		{5, cap}, // 16s would exceed cap
		{100, cap},
	}
	for _, tc := range cases {
		if got := backoffDelay(tc.failCount, cap); got != tc.want {
			t.Errorf("backoffDelay(%d, %s) = %s, want %s", tc.failCount, cap, got, tc.want)
		}
	}
}
