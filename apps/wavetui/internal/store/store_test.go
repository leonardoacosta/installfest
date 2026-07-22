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

// --- wavetui-sessions additive fields (tasks.md [4.4]) ----------------------
//
// These tests cover the Session/RateLimitBanner fields wavetui-sessions
// added additively to Item/Snapshot (design.md § Store additive fields).
// Everything above this point is the unmodified wavetui-core test suite —
// its continued passing (verified by running this whole file) is itself
// the "existing tests unmodified/passing" half of tasks.md [4.4]'s
// coverage requirement.

func TestSessionLinkEventUpdatesOnlySessionSubField(t *testing.T) {
	s := New()
	s.Apply(ItemUpsertEvent{Item: Item{ID: "a", Kind: KindBead, Title: "A"}})

	s.Apply(SessionLinkEvent{ItemID: "a", Session: &SessionLink{
		SessionID:     "sess-1",
		ContextPct:    42,
		TokensByModel: map[string]int64{"claude-sonnet-5": 100},
	}})

	snap := s.Snapshot()
	if len(snap.Items) != 1 {
		t.Fatalf("want 1 item, got %+v", snap.Items)
	}
	if snap.Items[0].Title != "A" || snap.Items[0].Kind != KindBead {
		t.Fatalf("SessionLinkEvent must not touch Title/Kind, got %+v", snap.Items[0])
	}
	if snap.Items[0].Session == nil || snap.Items[0].Session.SessionID != "sess-1" {
		t.Fatalf("expected Session.SessionID sess-1, got %+v", snap.Items[0].Session)
	}
}

// TestSessionLinkEventCachesForNotYetPublishedItem covers the documented
// race (store.go's SessionLinkEvent doc comment): TranscriptSource may
// resolve a link before BeadsSource/OpenSpecSource ever publishes the base
// Item. The Session value must be cached and applied the moment that
// item's first ItemUpsertEvent arrives, not dropped.
func TestSessionLinkEventCachesForNotYetPublishedItem(t *testing.T) {
	s := New()
	s.Apply(SessionLinkEvent{ItemID: "b", Session: &SessionLink{SessionID: "sess-2"}})

	snap := s.Snapshot()
	if len(snap.Items) != 0 {
		t.Fatalf("expected no items before the base item is published, got %+v", snap.Items)
	}

	s.Apply(ItemUpsertEvent{Item: Item{ID: "b", Kind: KindBead, Title: "B"}})
	snap = s.Snapshot()
	if len(snap.Items) != 1 || snap.Items[0].Session == nil || snap.Items[0].Session.SessionID != "sess-2" {
		t.Fatalf("expected the pending session link to be applied on first upsert, got %+v", snap.Items)
	}
}

// TestSessionLinkEventNilClearsLink covers "Session == nil clears a
// previously-linked session" from SessionLinkEvent's doc comment.
func TestSessionLinkEventNilClearsLink(t *testing.T) {
	s := New()
	s.Apply(ItemUpsertEvent{Item: Item{ID: "a", Kind: KindBead, Title: "A"}})
	s.Apply(SessionLinkEvent{ItemID: "a", Session: &SessionLink{SessionID: "sess-1"}})
	s.Apply(SessionLinkEvent{ItemID: "a", Session: nil})

	snap := s.Snapshot()
	if snap.Items[0].Session != nil {
		t.Fatalf("expected nil Session to clear a previously-linked session, got %+v", snap.Items[0].Session)
	}
}

// TestSnapshotDeepCopiesSessionLinkMapAndSlice extends
// TestSnapshotDeepCopiesPointerFields to Session's own nested reference
// types (TokensByModel map, Errors slice) — cloneItem's doc comment claims
// these are independently allocated per Snapshot, not shared with the
// Store's internal *SessionLink.
func TestSnapshotDeepCopiesSessionLinkMapAndSlice(t *testing.T) {
	s := New()
	s.Apply(ItemUpsertEvent{Item: Item{ID: "a", Kind: KindBead, Title: "A"}})
	s.Apply(SessionLinkEvent{ItemID: "a", Session: &SessionLink{
		SessionID:     "sess-1",
		TokensByModel: map[string]int64{"claude-sonnet-5": 100},
		Errors:        []ErrorEntry{{Class: "unclassified", Message: "boom"}},
	}})

	snap := s.Snapshot()
	sess := snap.Items[0].Session
	sess.TokensByModel["claude-sonnet-5"] = 999999
	sess.TokensByModel["new-model"] = 1
	sess.Errors[0].Message = "CORRUPTED"
	sess.Errors = append(sess.Errors, ErrorEntry{Class: "injected"})

	fresh := s.Snapshot()
	freshSess := fresh.Items[0].Session
	if freshSess.TokensByModel["claude-sonnet-5"] != 100 {
		t.Fatalf("mutating a Snapshot's TokensByModel map leaked into Store state: %+v", freshSess.TokensByModel)
	}
	if _, ok := freshSess.TokensByModel["new-model"]; ok {
		t.Fatal("adding a key to a Snapshot's TokensByModel map leaked into Store state")
	}
	if freshSess.Errors[0].Message == "CORRUPTED" {
		t.Fatalf("mutating a Snapshot's Errors slice leaked into Store state: %+v", freshSess.Errors)
	}
	if len(freshSess.Errors) != 1 {
		t.Fatalf("appending to a Snapshot's Errors slice leaked into Store state: %+v", freshSess.Errors)
	}

	again := s.Snapshot()
	if again.Items[0].Session == fresh.Items[0].Session {
		t.Fatal("two Snapshots share the same *SessionLink pointer — not a deep copy")
	}
}

// TestSnapshotRoundTripsSessionLinkCWD is wavetui-session-cwd's tasks.md
// [4.1]: SessionLink.CWD (additive field, see this proposal's spec.md "the
// matched cwd is available on the linked item's SessionLink" scenario) must
// survive a Snapshot round-trip unchanged, and — following
// TestSnapshotDeepCopiesSessionLinkMapAndSlice's precedent immediately
// above — a plain string field needs no deep-copy proof beyond cloneItem's
// existing `sl := *item.Session` value copy, but it still must actually
// reach the Snapshot's Items, which is the regression this test guards.
func TestSnapshotRoundTripsSessionLinkCWD(t *testing.T) {
	s := New()
	s.Apply(ItemUpsertEvent{Item: Item{ID: "a", Kind: KindBead, Title: "A"}})
	s.Apply(SessionLinkEvent{ItemID: "a", Session: &SessionLink{
		SessionID: "sess-1",
		CWD:       "/home/nyaptor/dev/personal/installfest",
	}})

	snap := s.Snapshot()
	if snap.Items[0].Session == nil || snap.Items[0].Session.CWD != "/home/nyaptor/dev/personal/installfest" {
		t.Fatalf("expected Session.CWD to round-trip through Snapshot, got %+v", snap.Items[0].Session)
	}
}

// TestSnapshotDeepCopiesRateLimitBanner covers Snapshot.RateLimitBanner —
// a whole-snapshot pointer field independent of any single Item, so it
// needs its own copy-on-write proof distinct from cloneItem's coverage.
func TestSnapshotDeepCopiesRateLimitBanner(t *testing.T) {
	s := New()
	s.Apply(RateLimitSignalEvent{Signal: RateLimitSignal{Message: "rate limited"}})

	snap := s.Snapshot()
	if snap.RateLimitBanner == nil || snap.RateLimitBanner.Message != "rate limited" {
		t.Fatalf("expected a RateLimitBanner, got %+v", snap.RateLimitBanner)
	}

	snap.RateLimitBanner.Message = "CORRUPTED"

	fresh := s.Snapshot()
	if fresh.RateLimitBanner.Message == "CORRUPTED" {
		t.Fatalf("mutating a Snapshot's RateLimitBanner leaked into Store state: %+v", fresh.RateLimitBanner)
	}
	if fresh.RateLimitBanner == snap.RateLimitBanner {
		t.Fatal("two Snapshots share the same *RateLimitSignal pointer — not a deep copy")
	}
}

func TestRateLimitSignalEventOverwritesPrevious(t *testing.T) {
	s := New()
	s.Apply(RateLimitSignalEvent{Signal: RateLimitSignal{Message: "first"}})
	s.Apply(RateLimitSignalEvent{Signal: RateLimitSignal{Message: "second"}})

	snap := s.Snapshot()
	if snap.RateLimitBanner == nil || snap.RateLimitBanner.Message != "second" {
		t.Fatalf("expected the banner to be overwritten by the latest signal, got %+v", snap.RateLimitBanner)
	}
}

// TestItemUpsertRepublishPreservesExistingSessionLink is the regression
// test for a real bug caught live during tasks.md [4.5]'s runtime
// verification (not a hypothetical): BeadsSource/OpenSpecSource republish
// their known items on every requery cycle (poll or fsnotify-triggered),
// always with Item.Session == nil (they don't know about sessions at
// all). Before this fix, Apply's ItemUpsertEvent case only restored a
// cached pendingSessions value (the "session resolved before the item
// existed" race) — it never preserved an EXISTING item's already-attached
// Session across a later, ordinary republish. In a live pty run against
// this repo's real bd data, a SessionsPane row (linked + zombie-badged)
// visibly vanished the moment BeadsSource's own periodic requery
// re-published the same already-linked item, confirmed independently via
// a standalone repro before this fix landed.
func TestItemUpsertRepublishPreservesExistingSessionLink(t *testing.T) {
	s := New()
	s.Apply(ItemUpsertEvent{Item: Item{ID: "a", Kind: KindBead, Title: "A"}})
	s.Apply(SessionLinkEvent{ItemID: "a", Session: &SessionLink{SessionID: "sess-1", ContextPct: 42}})

	snap := s.Snapshot()
	if snap.Items[0].Session == nil || snap.Items[0].Session.SessionID != "sess-1" {
		t.Fatalf("setup: expected item a linked to sess-1, got %+v", snap.Items[0].Session)
	}

	// Simulate a session-unaware source (BeadsSource/OpenSpecSource)
	// republishing the SAME item on a later requery cycle — its own
	// ItemUpsertEvent always carries Session == nil.
	s.Apply(ItemUpsertEvent{Item: Item{ID: "a", Kind: KindBead, Title: "A", CreatedAt: time.Time{}}})

	snap = s.Snapshot()
	if snap.Items[0].Session == nil {
		t.Fatal("a session-unaware republish wiped the existing Session link")
	}
	if snap.Items[0].Session.SessionID != "sess-1" || snap.Items[0].Session.ContextPct != 42 {
		t.Fatalf("expected the preserved Session to be unchanged, got %+v", snap.Items[0].Session)
	}

	// A REAL SessionLinkEvent with Session == nil must still be able to
	// clear the link — this fix must not make session state permanently
	// sticky.
	s.Apply(SessionLinkEvent{ItemID: "a", Session: nil})
	snap = s.Snapshot()
	if snap.Items[0].Session != nil {
		t.Fatal("expected an explicit SessionLinkEvent(nil) to still clear the link after the fix")
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
