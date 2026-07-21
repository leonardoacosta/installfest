package bus

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type testEvent struct{ n int }

func (testEvent) EventName() string { return "test.event" }

// waitFor polls cond until it's true or the timeout elapses, failing the
// test otherwise. Delivery happens on a separate goroutine from Publish, so
// tests must not assert immediately after Publish returns.
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

func TestPublishReachesAllSubscribers(t *testing.T) {
	b := New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	const subscriberCount = 5
	var mu sync.Mutex
	received := make([]int, subscriberCount)

	for i := 0; i < subscriberCount; i++ {
		idx := i
		b.Subscribe(ctx, func(ev Event) {
			mu.Lock()
			received[idx]++
			mu.Unlock()
		})
	}

	waitFor(t, time.Second, func() bool { return b.Len() == subscriberCount })

	b.Publish(testEvent{n: 1})

	waitFor(t, time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		for _, c := range received {
			if c != 1 {
				return false
			}
		}
		return true
	})

	mu.Lock()
	defer mu.Unlock()
	for i, c := range received {
		if c != 1 {
			t.Errorf("subscriber %d received %d events, want exactly 1", i, c)
		}
	}
}

func TestPublishNoDoubleDelivery(t *testing.T) {
	b := New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var count int64
	b.Subscribe(ctx, func(ev Event) {
		atomic.AddInt64(&count, 1)
	})

	waitFor(t, time.Second, func() bool { return b.Len() == 1 })

	const publishCount = 10
	for i := 0; i < publishCount; i++ {
		b.Publish(testEvent{n: i})
	}

	waitFor(t, time.Second, func() bool { return atomic.LoadInt64(&count) == publishCount })

	if got := atomic.LoadInt64(&count); got != publishCount {
		t.Fatalf("subscriber received %d events, want exactly %d (no double-delivery)", got, publishCount)
	}
}

func TestSubscribeTornDownOnContextCancel(t *testing.T) {
	b := New()
	ctx, cancel := context.WithCancel(context.Background())

	b.Subscribe(ctx, func(Event) {})
	waitFor(t, time.Second, func() bool { return b.Len() == 1 })

	cancel()
	waitFor(t, time.Second, func() bool { return b.Len() == 0 })
}

func TestPublishWithNoSubscribersDoesNotBlock(t *testing.T) {
	b := New()
	done := make(chan struct{})
	go func() {
		b.Publish(testEvent{n: 1})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Publish blocked with zero subscribers")
	}
}
