package flair

import (
	"testing"

	"github.com/leonardoacosta/installfest/apps/wavetui/internal/config"
)

// TestNeedsTickReflectsActiveState is the core testable invariant for this
// whole proposal (see design.md § Tick-loop lifecycle): NeedsTick must
// report false while `active` is empty, true as soon as it isn't, and false
// again once every entry drains back out — so the root model can genuinely
// idle at zero scheduling cost when nothing is animating.
func TestNeedsTickReflectsActiveState(t *testing.T) {
	m := NewFlairManager(config.FlairConfig{Enabled: true})

	if m.NeedsTick() {
		t.Fatal("want NeedsTick()==false on a freshly constructed manager with no active animations")
	}

	m.active["item-1"] = animState{}
	if !m.NeedsTick() {
		t.Fatal("want NeedsTick()==true once active is non-empty")
	}

	m.active["item-2"] = animState{}
	if !m.NeedsTick() {
		t.Fatal("want NeedsTick()==true with multiple active entries")
	}

	delete(m.active, "item-1")
	if !m.NeedsTick() {
		t.Fatal("want NeedsTick()==true while at least one entry remains active")
	}

	delete(m.active, "item-2")
	if m.NeedsTick() {
		t.Fatal("want NeedsTick()==false again once active drains back to empty")
	}
}

func TestNewFlairManagerStartsIdle(t *testing.T) {
	m := NewFlairManager(config.FlairConfig{})
	if m.NeedsTick() {
		t.Fatal("want a newly constructed FlairManager to start idle regardless of cfg")
	}
}
