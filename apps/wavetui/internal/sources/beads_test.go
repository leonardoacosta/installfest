package sources

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/leonardoacosta/installfest/apps/wavetui/internal/bus"
	"github.com/leonardoacosta/installfest/apps/wavetui/internal/store"
)

// stubBeadsCLI is the test double for beadsCLI — no real bd shellout ever
// happens in a unit test.
type stubBeadsCLI struct {
	listJSON, readyJSON string
	listErr, readyErr   error
	calls               atomic.Int32
}

func (s *stubBeadsCLI) List(context.Context) ([]byte, error) {
	s.calls.Add(1)
	if s.listErr != nil {
		return nil, s.listErr
	}
	return []byte(s.listJSON), nil
}

func (s *stubBeadsCLI) Ready(context.Context) ([]byte, error) {
	if s.readyErr != nil {
		return nil, s.readyErr
	}
	return []byte(s.readyJSON), nil
}

// eventually polls cond until it's true or timeout elapses — the bus
// delivers to Store asynchronously (its own per-subscriber goroutine), so
// tests that assert post-publish Store state need to wait for delivery
// rather than snapshot immediately after Publish returns.
func eventually(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("condition not met within %s", timeout)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func newWiredStore(t *testing.T, ctx context.Context) (*bus.Bus, *store.Store) {
	t.Helper()
	b := bus.New()
	st := store.New()
	b.Subscribe(ctx, func(ev bus.Event) { st.Apply(ev) })
	return b, st
}

// --- (a) debounce coalescing --------------------------------------------

func TestBeadsSourceDebounceCoalescesBurst(t *testing.T) {
	dir := t.TempDir()
	beadsDir := filepath.Join(dir, ".beads")
	if err := os.Mkdir(beadsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	walPath := filepath.Join(beadsDir, "beads.db-wal")
	if err := os.WriteFile(walPath, []byte("0"), 0o644); err != nil {
		t.Fatal(err)
	}

	b := bus.New()
	src := NewBeadsSource(dir, b)
	src.debounce = 200 * time.Millisecond
	src.poll = time.Hour // isolate debounce behavior from the poll fallback
	src.cli = &stubBeadsCLI{listJSON: "[]", readyJSON: "[]"}

	var calls atomic.Int32
	src.afterQuery = func() { calls.Add(1) }

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		if err := src.Run(ctx); err != nil {
			t.Errorf("Run: %v", err)
		}
		close(done)
	}()

	// Let the startup-triggered initial query (dirExists==true) fire and
	// settle before isolating the burst.
	eventually(t, time.Second, func() bool { return calls.Load() >= 1 })
	time.Sleep(50 * time.Millisecond)
	calls.Store(0)

	// A rapid burst of real writes, well inside the 200ms debounce window,
	// must coalesce into exactly one re-query (trailing-edge debounce).
	for i := 0; i < 15; i++ {
		if err := os.WriteFile(walPath, []byte{byte(i)}, 0o644); err != nil {
			t.Fatal(err)
		}
		time.Sleep(2 * time.Millisecond)
	}

	time.Sleep(600 * time.Millisecond) // let the trailing debounce fire
	cancel()
	<-done

	if got := calls.Load(); got != 1 {
		t.Fatalf("requeryOnce called %d times after burst, want exactly 1", got)
	}
}

// --- (b) failure keeps last-good snapshot + marks SourceError -----------

func TestBeadsSourceFailureModesKeepLastGoodAndMarkError(t *testing.T) {
	cases := []struct {
		name string
		cli  *stubBeadsCLI
	}{
		{name: "non-zero exit", cli: &stubBeadsCLI{listErr: errors.New("bd: exit status 1")}},
		{name: "malformed json", cli: &stubBeadsCLI{listJSON: "not json", readyJSON: "[]"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			b, st := newWiredStore(t, ctx)

			src := NewBeadsSource(t.TempDir(), b)
			src.cli = &stubBeadsCLI{
				listJSON:  `[{"id":"if-1","title":"one","status":"open","created_at":"2026-01-01T00:00:00Z"}]`,
				readyJSON: `[{"id":"if-1"}]`,
			}

			if !src.requeryOnce(ctx) {
				t.Fatal("setup: first requeryOnce should succeed")
			}
			eventually(t, time.Second, func() bool {
				snap := st.Snapshot()
				return len(snap.Items) == 1 && snap.Items[0].ID == "if-1"
			})

			// Swap in the failing CLI and requery again.
			src.cli = tc.cli
			if src.requeryOnce(ctx) {
				t.Fatal("requeryOnce should report failure")
			}

			eventually(t, time.Second, func() bool {
				snap := st.Snapshot()
				if len(snap.Items) != 1 || snap.Items[0].ID != "if-1" {
					return false
				}
				if !snap.Items[0].Stale {
					return false
				}
				for _, e := range snap.Errors {
					if e.Source == SourceNameBeads {
						return true
					}
				}
				return false
			})

			snap := st.Snapshot()
			if len(snap.Items) != 1 || snap.Items[0].ID != "if-1" {
				t.Fatalf("prior item lost after failed requery: %+v", snap.Items)
			}
			if !snap.Items[0].Stale {
				t.Fatalf("surviving item should be badged Stale after a failed requery: %+v", snap.Items[0])
			}
		})
	}
}

// --- (c) missing-directory startup degradation ---------------------------

func TestBeadsSourceMissingDirPublishesUnavailableNoPanic(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	b, st := newWiredStore(t, ctx)

	dir := t.TempDir() // deliberately no .beads/ subdirectory
	src := NewBeadsSource(dir, b)
	src.poll = time.Hour
	src.cli = &stubBeadsCLI{listJSON: "[]", readyJSON: "[]"}

	errCh := make(chan error, 1)
	go func() { errCh <- src.Run(ctx) }()

	eventually(t, time.Second, func() bool {
		for _, e := range st.Snapshot().Errors {
			if e.Source == SourceNameBeads {
				return true
			}
		}
		return false
	})

	// Now create .beads/ and confirm the badge clears live, without a
	// restart (task 2.3).
	if err := os.Mkdir(filepath.Join(dir, ".beads"), 0o755); err != nil {
		t.Fatal(err)
	}
	eventually(t, time.Second, func() bool {
		return len(st.Snapshot().Errors) == 0
	})

	cancel()
	if err := <-errCh; err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
}

// --- toItem / decode correctness -----------------------------------------

func TestToItemBlockerPrecedenceAndTolerantDecode(t *testing.T) {
	ready := map[string]bool{"if-ready": true}

	// Explicit note wins over the synthetic not-ready fallback.
	explicit := toItem(beadRecord{
		ID:     "if-1",
		Title:  "one",
		Status: "open",
		Notes:  "some preamble\nblocked: decision - waiting on Leo (see if-2)\nmore text",
	}, ready)
	if explicit.Blocker == nil || explicit.Blocker.Type != "decision" || explicit.Blocker.Ref != "if-2" {
		t.Fatalf("want explicit blocker note, got %+v", explicit.Blocker)
	}

	// Open + absent from ready + no explicit note -> synthetic blocker.
	synthetic := toItem(beadRecord{ID: "if-3", Title: "three", Status: "open"}, ready)
	if synthetic.Blocker == nil || synthetic.Blocker.Type != "dependency" {
		t.Fatalf("want synthetic dependency blocker, got %+v", synthetic.Blocker)
	}

	// In the ready set -> no blocker at all.
	clean := toItem(beadRecord{ID: "if-ready", Title: "ready", Status: "open"}, ready)
	if clean.Blocker != nil {
		t.Fatalf("want no blocker for a ready item, got %+v", clean.Blocker)
	}

	// in_progress, absent from ready, no note -> must NOT be mislabeled
	// blocked (it's being worked, not stuck).
	inProgress := toItem(beadRecord{ID: "if-4", Title: "four", Status: "in_progress"}, ready)
	if inProgress.Blocker != nil {
		t.Fatalf("in_progress item should never get a synthetic blocker, got %+v", inProgress.Blocker)
	}

	// Missing/unparsable created_at tolerantly zero-values rather than
	// erroring.
	noDate := toItem(beadRecord{ID: "if-5", Title: "five", CreatedAt: "not-a-date"}, ready)
	if !noDate.CreatedAt.IsZero() {
		t.Fatalf("want zero CreatedAt for unparsable input, got %v", noDate.CreatedAt)
	}
}

// --- if-yijz: Description threading -------------------------------------

func TestToItemThreadsDescriptionVerbatim(t *testing.T) {
	ready := map[string]bool{}

	withDesc := toItem(beadRecord{
		ID:          "if-1",
		Title:       "one",
		Description: "This bead tracks the thing that needs doing.",
	}, ready)
	if withDesc.Description != "This bead tracks the thing that needs doing." {
		t.Fatalf("Description = %q, want the beadRecord's description verbatim", withDesc.Description)
	}

	noDesc := toItem(beadRecord{ID: "if-2", Title: "two"}, ready)
	if noDesc.Description != "" {
		t.Fatalf("Description = %q, want empty string for a bead with no description", noDesc.Description)
	}
}

func TestBeadsSourceUnknownJSONFieldsIgnored(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b, st := newWiredStore(t, ctx)

	src := NewBeadsSource(t.TempDir(), b)
	// dependency_count/labels/etc. are real bd fields beadRecord does not
	// declare — encoding/json.Unmarshal must ignore them, not error.
	src.cli = &stubBeadsCLI{
		listJSON:  `[{"id":"if-1","title":"one","status":"open","dependency_count":3,"labels":["x"],"comment_count":0}]`,
		readyJSON: `[]`,
	}

	if !src.requeryOnce(ctx) {
		t.Fatal("requeryOnce should tolerate unknown JSON fields")
	}
	eventually(t, time.Second, func() bool {
		snap := st.Snapshot()
		return len(snap.Items) == 1 && snap.Items[0].ID == "if-1"
	})
}
