package store

import (
	"testing"
	"time"
)

func TestApplyUpsertAndRemove(t *testing.T) {
	s := New()
	s.Apply(ItemUpsertEvent{Item: Item{ID: "a", Kind: KindBead, Title: "A"}})

	snap := s.Snapshot()
	if len(snap.Items) != 1 || snap.Items[0].ID != "a" {
		t.Fatalf("want 1 item %q, got %+v", "a", snap.Items)
	}

	s.Apply(ItemRemoveEvent{ID: "a"})
	snap = s.Snapshot()
	if len(snap.Items) != 0 {
		t.Fatalf("want 0 items after remove, got %+v", snap.Items)
	}
}

func TestApplySourceErrorAndOK(t *testing.T) {
	s := New()
	s.Apply(SourceErrorEvent{Error: SourceError{Source: "beads", Message: "bd list failed", Timestamp: time.Now()}})

	snap := s.Snapshot()
	if len(snap.Errors) != 1 || snap.Errors[0].Source != "beads" {
		t.Fatalf("want 1 error for beads, got %+v", snap.Errors)
	}

	s.Apply(SourceOKEvent{Source: "beads"})
	snap = s.Snapshot()
	if len(snap.Errors) != 0 {
		t.Fatalf("want 0 errors after recovery, got %+v", snap.Errors)
	}
}

// TestSnapshotIsCopyOnWrite is the core immutability contract from
// design.md: "the UI holds its own copy and the Store's next mutation
// cannot retroactively change a snapshot the UI already rendered." It
// asserts both directions: a Snapshot taken before a mutation must not see
// that mutation, and mutating a Snapshot's own returned slice must not
// corrupt the Store's internal state.
func TestSnapshotIsCopyOnWrite(t *testing.T) {
	s := New()
	s.Apply(ItemUpsertEvent{Item: Item{ID: "a", Kind: KindBead, Title: "before"}})

	before := s.Snapshot()
	if len(before.Items) != 1 || before.Items[0].Title != "before" {
		t.Fatalf("setup: want 1 item titled %q, got %+v", "before", before.Items)
	}

	// Mutate the Store after taking `before`.
	s.Apply(ItemUpsertEvent{Item: Item{ID: "a", Kind: KindBead, Title: "after"}})
	s.Apply(ItemUpsertEvent{Item: Item{ID: "b", Kind: KindBead, Title: "new"}})

	// `before` must be completely unaffected by the later Apply calls.
	if len(before.Items) != 1 || before.Items[0].Title != "before" {
		t.Fatalf("snapshot taken before mutation changed retroactively: %+v", before.Items)
	}

	after := s.Snapshot()
	if len(after.Items) != 2 {
		t.Fatalf("want 2 items after upserts, got %+v", after.Items)
	}

	// Mutating the caller's copy of the slice/backing array must not leak
	// into the Store's own state — prove it by corrupting `after` and then
	// taking a fresh snapshot.
	after.Items[0].Title = "CORRUPTED"
	fresh := s.Snapshot()
	for _, item := range fresh.Items {
		if item.Title == "CORRUPTED" {
			t.Fatalf("mutating a returned Snapshot leaked into Store state: %+v", fresh.Items)
		}
	}
}

func TestFanOutScoreTransitiveDependents(t *testing.T) {
	s := New()
	// c depends on b, b depends on a: resolving a unblocks b and (transitively) c.
	s.Apply(ItemUpsertEvent{Item: Item{ID: "a", Kind: KindBead, Title: "A"}})
	s.Apply(ItemUpsertEvent{Item: Item{ID: "b", Kind: KindBead, Title: "B"}, Deps: []string{"a"}})
	s.Apply(ItemUpsertEvent{Item: Item{ID: "c", Kind: KindBead, Title: "C"}, Deps: []string{"b"}})

	snap := s.Snapshot()
	scores := map[string]int{}
	for _, item := range snap.Items {
		scores[item.ID] = item.FanOutScore
	}

	if scores["a"] != 2 {
		t.Errorf("a.FanOutScore = %d, want 2 (unblocks b and c)", scores["a"])
	}
	if scores["b"] != 1 {
		t.Errorf("b.FanOutScore = %d, want 1 (unblocks c)", scores["b"])
	}
	if scores["c"] != 0 {
		t.Errorf("c.FanOutScore = %d, want 0 (nothing depends on c)", scores["c"])
	}
}

func TestFanOutScoreIgnoresCycleInfiniteLoop(t *testing.T) {
	s := New()
	// a <-> b cyclic dependency must not hang recomputeFanOutLocked.
	s.Apply(ItemUpsertEvent{Item: Item{ID: "a", Kind: KindBead, Title: "A"}, Deps: []string{"b"}})
	s.Apply(ItemUpsertEvent{Item: Item{ID: "b", Kind: KindBead, Title: "B"}, Deps: []string{"a"}})

	done := make(chan struct{})
	go func() {
		_ = s.Snapshot()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("recomputeFanOutLocked hung on a dependency cycle")
	}
}
