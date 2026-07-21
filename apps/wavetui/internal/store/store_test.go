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

// TestSnapshotDeepCopiesPointerFields is the post-wave gate finding's
// regression test: TestSnapshotIsCopyOnWrite above only proves shallow
// copy-on-write for a primitive field (Title). Item also carries two
// pointer fields (Blocker, TaskProgress) — a plain per-Item struct copy
// (what the range loop in Snapshot did before cloneItem existed) copies the
// pointer VALUE, not the pointed-to struct, so every Snapshot's Items would
// still share the exact *BlockerNote/*TaskProgress the Store's internal map
// holds. This asserts mutating those pointed-to structs through a returned
// Snapshot cannot leak back into the Store (or into a separately-taken
// Snapshot), matching this file's doc-comment claim that "a later Store
// mutation can never retroactively change a Snapshot already handed to a
// caller" for pointer fields too, not just primitive ones.
func TestSnapshotDeepCopiesPointerFields(t *testing.T) {
	s := New()
	s.Apply(ItemUpsertEvent{Item: Item{
		ID:           "a",
		Kind:         KindBead,
		Title:        "A",
		Blocker:      &BlockerNote{Type: "dependency", Reason: "before"},
		TaskProgress: &TaskProgress{Done: 1, Total: 4},
	}})

	snap := s.Snapshot()
	if snap.Items[0].Blocker == nil || snap.Items[0].TaskProgress == nil {
		t.Fatalf("setup: want Blocker and TaskProgress populated, got %+v", snap.Items[0])
	}

	// Mutate the pointed-to structs through the Snapshot's own copy.
	snap.Items[0].Blocker.Reason = "CORRUPTED"
	snap.Items[0].TaskProgress.Done = 999

	fresh := s.Snapshot()
	if fresh.Items[0].Blocker.Reason == "CORRUPTED" {
		t.Fatalf("mutating a Snapshot's Blocker pointer leaked into the Store's internal item: %+v", fresh.Items[0].Blocker)
	}
	if fresh.Items[0].TaskProgress.Done == 999 {
		t.Fatalf("mutating a Snapshot's TaskProgress pointer leaked into the Store's internal item: %+v", fresh.Items[0].TaskProgress)
	}

	// Two independently-taken Snapshots must not share pointer identity
	// either — each Snapshot call is its own deep copy, not a cache of the
	// same clone.
	again := s.Snapshot()
	if again.Items[0].Blocker == fresh.Items[0].Blocker {
		t.Fatal("two Snapshots share the same *BlockerNote pointer — not a deep copy")
	}
	if again.Items[0].TaskProgress == fresh.Items[0].TaskProgress {
		t.Fatal("two Snapshots share the same *TaskProgress pointer — not a deep copy")
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
