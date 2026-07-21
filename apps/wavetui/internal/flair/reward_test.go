package flair

import (
	"math/rand"
	"testing"
	"time"
)

func TestRecordCloseStreakIsMonotonic(t *testing.T) {
	tr := NewRewardTracker()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	for i := 1; i <= 5; i++ {
		res := tr.RecordClose(now)
		if res.Streak != i {
			t.Fatalf("close %d: want streak %d, got %d", i, i, res.Streak)
		}
		now = now.Add(time.Second)
	}
	if tr.Streak() != 5 {
		t.Fatalf("want Streak()==5, got %d", tr.Streak())
	}
}

// TestComboExtendsWithinWindowAndResetsAfterGap is the combo mechanic's core
// contract: closes arriving within comboWindow of each other extend the
// same combo (multiplier grows); a gap longer than comboWindow starts a
// fresh combo of 1 (multiplier back to baseline).
func TestComboExtendsWithinWindowAndResetsAfterGap(t *testing.T) {
	tr := NewRewardTracker()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	first := tr.RecordClose(now)
	if first.Combo != 1 {
		t.Fatalf("first close: want combo 1, got %d", first.Combo)
	}
	if first.Multiplier != 1.0 {
		t.Fatalf("first close: want multiplier 1.0, got %v", first.Multiplier)
	}

	now = now.Add(comboWindow / 2)
	second := tr.RecordClose(now)
	if second.Combo != 2 {
		t.Fatalf("close within window: want combo 2, got %d", second.Combo)
	}
	if second.Multiplier <= first.Multiplier {
		t.Fatalf("combo multiplier must grow: first=%v second=%v", first.Multiplier, second.Multiplier)
	}

	now = now.Add(comboWindow + time.Second)
	third := tr.RecordClose(now)
	if third.Combo != 1 {
		t.Fatalf("close after a gap exceeding comboWindow: want combo reset to 1, got %d", third.Combo)
	}
	if third.Multiplier != 1.0 {
		t.Fatalf("want multiplier reset to 1.0 after a combo reset, got %v", third.Multiplier)
	}
}

// TestComboMultiplierCapsAtMax confirms an arbitrarily long combo never
// exceeds maxComboMultiplier.
func TestComboMultiplierCapsAtMax(t *testing.T) {
	tr := NewRewardTracker()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	var last float64
	for i := 0; i < 200; i++ {
		res := tr.RecordClose(now)
		last = res.Multiplier
		if last > maxComboMultiplier {
			t.Fatalf("multiplier exceeded cap: got %v, want <= %v", last, maxComboMultiplier)
		}
		now = now.Add(time.Second)
	}
	if last != maxComboMultiplier {
		t.Fatalf("want a long combo to reach the cap %v, got %v", maxComboMultiplier, last)
	}
}

// TestObserveFiresOnlyOnGenuineTransitionToEmpty is the all-clear
// contract: Observe must report true exactly once per nonzero->zero
// transition, never on a repeated zero, and never on a tracker's very
// first-ever observation.
func TestObserveFiresOnlyOnGenuineTransitionToEmpty(t *testing.T) {
	tr := NewRewardTracker()

	if tr.Observe(0) {
		t.Fatal("want no all-clear on the very first observation, even if it starts at zero")
	}

	if tr.Observe(3) {
		t.Fatal("want no all-clear while the queue is non-empty")
	}

	if !tr.Observe(0) {
		t.Fatal("want an all-clear on the genuine nonzero->zero transition")
	}

	if tr.Observe(0) {
		t.Fatal("want no repeated all-clear while the queue stays empty")
	}

	if tr.Observe(2) {
		t.Fatal("want no all-clear when new items appear")
	}
	if !tr.Observe(0) {
		t.Fatal("want a second all-clear on the next genuine transition")
	}
}

// TestRecordCloseCelebrationRollsAcrossManyCloses confirms Celebration is
// actually wired to a real random roll (both true and false outcomes occur
// across enough closes) rather than being hardcoded to one value, using the
// tracker's own injected rng field so the test is reproducible.
func TestRecordCloseCelebrationRollsAcrossManyCloses(t *testing.T) {
	tr := &RewardTracker{rng: rand.New(rand.NewSource(2))}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	sawTrue, sawFalse := false, false
	for i := 0; i < 500; i++ {
		if tr.RecordClose(now).Celebration {
			sawTrue = true
		} else {
			sawFalse = true
		}
		now = now.Add(time.Millisecond)
	}
	if !sawFalse {
		t.Fatal("want at least one non-celebration roll across 500 closes")
	}
	// celebrationChance (5%) over 500 rolls makes at least one true
	// overwhelmingly likely; asserting it confirms Celebration is actually
	// wired to the roll rather than hardcoded false.
	if !sawTrue {
		t.Fatal("want at least one celebration roll across 500 closes at a 5% chance — Celebration may be hardcoded false")
	}
}
