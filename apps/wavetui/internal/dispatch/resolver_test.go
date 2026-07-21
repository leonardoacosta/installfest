// See resolver.go for the Resolver contract under test here. Not itself a
// named tasks.md [4.x] line item, but resolver.go's own doc comment points
// at "resolver_test.go (tasks.md [4.1]/[4.2] E2E batch)" as the intended
// home for this coverage, and Resolver's fallback logic (the ONE piece of
// dispatch-boundary behavior not exercised by tmux_test.go or
// clipboard_test.go in isolation) was a genuine gap — no test file existed
// for internal/dispatch at all before this batch.
package dispatch

import (
	"context"
	"errors"
	"testing"

	"github.com/leonardoacosta/installfest/apps/wavetui/internal/store"
)

// fakeDispatcher is a hermetic Dispatcher stand-in for exercising Resolver's
// fallback decision in isolation from any real Tmux/Clipboard logic.
type fakeDispatcher struct {
	err        error
	called     int
	lastItem   store.Item
	lastPrompt string
}

func (f *fakeDispatcher) Dispatch(ctx context.Context, item store.Item, promptText string) error {
	f.called++
	f.lastItem = item
	f.lastPrompt = promptText
	return f.err
}

func TestResolverFallsBackToClipboardOnErrNoDispatchTarget(t *testing.T) {
	tmux := &fakeDispatcher{err: ErrNoDispatchTarget}
	clipboard := &fakeDispatcher{err: nil}
	r := &Resolver{Tmux: tmux, Clipboard: clipboard}

	item := store.Item{ID: "if-p1ru"}
	if err := r.Dispatch(context.Background(), item, "prompt"); err != nil {
		t.Fatalf("Dispatch error: %v", err)
	}
	if tmux.called != 1 {
		t.Fatalf("want Tmux tried exactly once, got %d", tmux.called)
	}
	if clipboard.called != 1 {
		t.Fatalf("want Clipboard fallback tried exactly once, got %d", clipboard.called)
	}
	if clipboard.lastItem.ID != "if-p1ru" || clipboard.lastPrompt != "prompt" {
		t.Fatalf("want item/prompt forwarded unchanged to Clipboard, got item=%+v prompt=%q", clipboard.lastItem, clipboard.lastPrompt)
	}
}

func TestResolverSuccessNeverFallsBack(t *testing.T) {
	tmux := &fakeDispatcher{err: nil}
	clipboard := &fakeDispatcher{}
	r := &Resolver{Tmux: tmux, Clipboard: clipboard}

	if err := r.Dispatch(context.Background(), store.Item{}, "prompt"); err != nil {
		t.Fatalf("Dispatch error: %v", err)
	}
	if clipboard.called != 0 {
		t.Fatalf("want Clipboard never invoked on Tmux success, got called=%d", clipboard.called)
	}
}

func TestResolverAmbiguousErrorPropagatesWithoutFallback(t *testing.T) {
	ambiguous := &AmbiguousTargetError{Candidates: []Candidate{{PaneID: "%1"}, {PaneID: "%2"}}}
	tmux := &fakeDispatcher{err: ambiguous}
	clipboard := &fakeDispatcher{}
	r := &Resolver{Tmux: tmux, Clipboard: clipboard}

	err := r.Dispatch(context.Background(), store.Item{}, "prompt")
	var got *AmbiguousTargetError
	if !errors.As(err, &got) {
		t.Fatalf("want *AmbiguousTargetError propagated unchanged, got %v", err)
	}
	if clipboard.called != 0 {
		t.Fatalf("an ambiguous tie must never fall back to Clipboard, got called=%d", clipboard.called)
	}
}

func TestResolverRefusalErrorsPropagateWithoutFallback(t *testing.T) {
	for _, refusal := range []error{ErrPaneInCopyMode, ErrSessionStreaming} {
		tmux := &fakeDispatcher{err: refusal}
		clipboard := &fakeDispatcher{}
		r := &Resolver{Tmux: tmux, Clipboard: clipboard}

		err := r.Dispatch(context.Background(), store.Item{}, "prompt")
		if !errors.Is(err, refusal) {
			t.Fatalf("want %v propagated unchanged, got %v", refusal, err)
		}
		if clipboard.called != 0 {
			t.Fatalf("a %v refusal must never fall back to Clipboard, got called=%d", refusal, clipboard.called)
		}
	}
}

func TestResolverGenuineTmuxFailurePropagatesWithoutFallback(t *testing.T) {
	genuine := errors.New("tmux: server not running")
	tmux := &fakeDispatcher{err: genuine}
	clipboard := &fakeDispatcher{}
	r := &Resolver{Tmux: tmux, Clipboard: clipboard}

	err := r.Dispatch(context.Background(), store.Item{}, "prompt")
	if !errors.Is(err, genuine) {
		t.Fatalf("want the genuine tmux failure propagated unchanged, got %v", err)
	}
	if clipboard.called != 0 {
		t.Fatalf("a non-ErrNoDispatchTarget failure must never fall back to Clipboard, got called=%d", clipboard.called)
	}
}

func TestResolverPropagatesClipboardFailureAfterFallback(t *testing.T) {
	clipboardErr := errors.New("no clipboard mechanism available")
	tmux := &fakeDispatcher{err: ErrNoDispatchTarget}
	clipboard := &fakeDispatcher{err: clipboardErr}
	r := &Resolver{Tmux: tmux, Clipboard: clipboard}

	err := r.Dispatch(context.Background(), store.Item{}, "prompt")
	if !errors.Is(err, clipboardErr) {
		t.Fatalf("want the Clipboard fallback's own failure surfaced, got %v", err)
	}
}

func TestNewResolverWiresConcreteDispatchers(t *testing.T) {
	r := NewResolver(NewTmuxDispatcher(), NewClipboardDispatcher(false))
	if r.Tmux == nil || r.Clipboard == nil {
		t.Fatalf("want both Dispatchers wired, got Tmux=%v Clipboard=%v", r.Tmux, r.Clipboard)
	}
}
