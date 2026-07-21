// Package bus implements the typed publish/subscribe event bus that sits
// between the wavetui sources (BeadsSource, OpenSpecSource, ...) and the
// Store. See openspec/changes/wavetui-core/design.md § Architecture.
//
// Each subscriber is delivered events on its own goroutine, derived from the
// context.Context passed to Subscribe. Publish never blocks on a slow or
// stuck subscriber, and no mutable state is shared between the goroutine
// that calls Publish and the goroutines that deliver to each subscriber —
// the only shared thing is the per-subscriber channel itself, which is the
// synchronization primitive, not an object either side mutates directly.
package bus

import (
	"context"
	"sync"
)

// Event is the marker interface every published event implements. Concrete
// event types live alongside their producer/consumer package (e.g. the
// store package defines the events its Apply method understands) — bus
// itself stays generic and has no knowledge of any concrete event type.
type Event interface {
	// EventName returns a short, stable, human-readable identifier for the
	// event's type — used for logging/debugging, never for dispatch.
	EventName() string
}

// Handler processes one delivered event. It runs on the subscriber's own
// goroutine, so a slow handler only delays that subscriber's own queue, not
// other subscribers and not Publish.
type Handler func(Event)

// subscriberBuffer is the per-subscriber channel capacity. A subscriber that
// falls this far behind has events dropped for it (Publish never blocks) —
// generous enough that no realistic wavetui event burst hits it in practice.
const subscriberBuffer = 64

type subscriber struct {
	ch     chan Event
	cancel context.CancelFunc
}

// Bus is a typed event bus. The zero value is not usable — construct with
// New.
type Bus struct {
	mu   sync.Mutex
	subs map[int]*subscriber
	next int
}

// New constructs an empty Bus.
func New() *Bus {
	return &Bus{subs: make(map[int]*subscriber)}
}

// Subscribe registers handler to receive every event Published after this
// call returns, for as long as ctx is not Done. Delivery to this subscriber
// runs on a single dedicated goroutine that is torn down when ctx is
// cancelled — Subscribe itself does not block.
func (b *Bus) Subscribe(ctx context.Context, handler Handler) {
	ch := make(chan Event, subscriberBuffer)
	sctx, cancel := context.WithCancel(ctx)

	b.mu.Lock()
	id := b.next
	b.next++
	b.subs[id] = &subscriber{ch: ch, cancel: cancel}
	b.mu.Unlock()

	go func() {
		defer func() {
			b.mu.Lock()
			delete(b.subs, id)
			b.mu.Unlock()
		}()
		for {
			select {
			case <-sctx.Done():
				return
			case ev := <-ch:
				handler(ev)
			}
		}
	}()
}

// Publish delivers ev to every currently-subscribed handler. Delivery is
// best-effort per subscriber: a subscriber whose buffer is full has this
// event dropped for it rather than blocking Publish or any other
// subscriber. Publish returns as soon as every send has been attempted; it
// does not wait for handlers to finish running.
func (b *Bus) Publish(ev Event) {
	b.mu.Lock()
	chans := make([]chan Event, 0, len(b.subs))
	for _, s := range b.subs {
		chans = append(chans, s.ch)
	}
	b.mu.Unlock()

	for _, ch := range chans {
		select {
		case ch <- ev:
		default:
		}
	}
}

// Len reports the current number of live subscribers. Exposed for tests and
// diagnostics only — production code should never branch on it.
func (b *Bus) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.subs)
}
