// reward.go implements design.md's deferred "Victory-recap" mechanics
// (streak counter, combo multiplier, all-clear state, rare variable-reward
// celebration) ahead of any consumer — task [3.3]. design.md § Victory-recap
// data sourcing's anti-drift invariant requires a future wave-complete
// celebration's numbers to be computed by counting the SAME Diff()-produced
// EventItemClosed events this package already emits, never a second query
// against bd/openspec directly; RecordClose is exactly that counting path,
// ready for a future consumer to feed with events this package already
// produces.
//
// This file is deliberately NOT wired into cmd/wavetui this wave: the only
// consumer design.md names (a wave-complete celebration) has no emittable
// trigger anywhere in the pipeline yet (design.md § Event -> effect map's
// "Deferred" row — verified against wavetui-dispatch's own design.md during
// the API batch: no such event exists) — task [3.3]'s own text calls this
// "real functionality but does not block any task above or below it."
// RewardTracker is independent of FlairManager (it never touches
// active/toastQueue) so a future consumer can hold one on its own.
package flair

import (
	"math/rand"
	"time"
)

// comboWindow is how long after a close the NEXT close still counts as
// part of the same combo — a close arriving after a longer gap starts a
// fresh combo of 1 rather than extending the running one.
const comboWindow = 30 * time.Second

// maxComboMultiplier caps the combo multiplier so an extremely long combo
// doesn't grow without bound.
const maxComboMultiplier = 3.0

// comboMultiplierStep is how much the multiplier grows per additional close
// within the current combo, beyond the first.
const comboMultiplierStep = 0.25

// celebrationChance is the rare variable-reward celebration's odds per
// close — "rare" per design.md's Scope wording, independent of streak/combo
// length (a variable-reward schedule, not a milestone trigger).
const celebrationChance = 0.05

// RecordCloseResult is what RecordClose reports about the close it just
// processed.
type RecordCloseResult struct {
	Streak      int
	Combo       int
	Multiplier  float64
	Celebration bool
}

// RewardTracker accumulates streak/combo/all-clear state across a session.
// The zero value is not usable — construct with NewRewardTracker.
type RewardTracker struct {
	streak       int
	comboCount   int
	lastCloseAt  time.Time
	hasLastClose bool

	seenReady     bool
	wasReadyEmpty bool

	// rng is a struct field (not math/rand's package-level global) so tests
	// can inject a deterministic source to make Celebration assertions
	// exact instead of statistical.
	rng *rand.Rand
}

// NewRewardTracker constructs an idle RewardTracker (streak 0, no combo).
func NewRewardTracker() *RewardTracker {
	return &RewardTracker{rng: rand.New(rand.NewSource(time.Now().UnixNano()))}
}

// RecordClose records one EventItemClosed at now: streak is a plain
// monotonic per-session tally (this domain has no "failure" event that
// would ever reset it — every FlairEvent this package produces is either
// neutral or positive), and combo resets to 1 whenever the gap since the
// previous close exceeds comboWindow, otherwise extends the running combo.
func (t *RewardTracker) RecordClose(now time.Time) RecordCloseResult {
	t.streak++

	if t.hasLastClose && now.Sub(t.lastCloseAt) <= comboWindow {
		t.comboCount++
	} else {
		t.comboCount = 1
	}
	t.lastCloseAt = now
	t.hasLastClose = true

	return RecordCloseResult{
		Streak:      t.streak,
		Combo:       t.comboCount,
		Multiplier:  t.ComboMultiplier(),
		Celebration: t.rng.Float64() < celebrationChance,
	}
}

// ComboMultiplier returns the current combo's score multiplier, capped at
// maxComboMultiplier.
func (t *RewardTracker) ComboMultiplier() float64 {
	m := 1.0 + comboMultiplierStep*float64(t.comboCount-1)
	if m > maxComboMultiplier {
		return maxComboMultiplier
	}
	return m
}

// Streak returns the total number of closes recorded so far this session.
func (t *RewardTracker) Streak() int { return t.streak }

// Observe reports whether readyCount represents a genuine transition into
// an all-clear state: true only the first time readyCount reaches 0 after
// having previously been positive. It is false on a repeated call while the
// count stays at 0 (a caller invoking this once per Snapshot would
// otherwise re-fire the celebration on every render while the queue stays
// empty) and false on this tracker's very first-ever observation (a session
// that starts with an already-empty queue never had anything to clear).
func (t *RewardTracker) Observe(readyCount int) (allClear bool) {
	empty := readyCount == 0
	transitioned := t.seenReady && empty && !t.wasReadyEmpty
	t.wasReadyEmpty = empty
	t.seenReady = true
	return transitioned
}
