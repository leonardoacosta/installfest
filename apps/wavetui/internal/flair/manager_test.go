package flair

import (
	"reflect"
	"testing"

	"github.com/leonardoacosta/installfest/apps/wavetui/internal/config"
	"github.com/leonardoacosta/installfest/apps/wavetui/internal/store"
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

// --- Diff (design.md § Snapshot diffing) ----------------------------------

func mkSnapshot(items ...store.Item) store.Snapshot {
	return store.Snapshot{Items: items}
}

// TestDiffDetectsAllTransitionKinds exercises Diff against one snapshot
// pair carrying every transition kind at once: an appeared bead, an
// appeared proposal, a closed bead, a closed proposal (archived), a
// blocker-resolved item, and a stale-went-true (negative) item.
func TestDiffDetectsAllTransitionKinds(t *testing.T) {
	prev := mkSnapshot(
		store.Item{ID: "closed-bead", Kind: store.KindBead},
		store.Item{ID: "closed-proposal", Kind: store.KindProposal},
		store.Item{ID: "blocker-item", Kind: store.KindBead, Blocker: &store.BlockerNote{Type: "waiting"}},
		store.Item{ID: "stale-item", Kind: store.KindBead, Stale: false},
	)
	next := mkSnapshot(
		store.Item{ID: "appeared-bead", Kind: store.KindBead},
		store.Item{ID: "appeared-proposal", Kind: store.KindProposal},
		store.Item{ID: "blocker-item", Kind: store.KindBead, Blocker: nil},
		store.Item{ID: "stale-item", Kind: store.KindBead, Stale: true},
	)

	events := Diff(prev, next)

	want := map[string]EventKind{
		"closed-bead":       EventItemClosed,
		"closed-proposal":   EventItemClosed,
		"appeared-bead":     EventItemAppeared,
		"appeared-proposal": EventItemAppeared,
		"blocker-item":      EventBlockerResolved,
		"stale-item":        EventNegative,
	}
	got := make(map[string]EventKind, len(events))
	for _, ev := range events {
		got[ev.ItemID] = ev.Kind
	}

	if len(got) != len(want) {
		t.Fatalf("want %d distinct events, got %d: %+v", len(want), len(got), events)
	}
	for id, kind := range want {
		if got[id] != kind {
			t.Errorf("item %q: want event %q, got %q", id, kind, got[id])
		}
	}
}

// TestDiffItemKindCarriesThrough confirms FlairEvent.ItemKind reflects the
// item's real Kind (KindBead vs KindProposal) — effects.go's EffectFor
// depends on this field.
func TestDiffItemKindCarriesThrough(t *testing.T) {
	prev := mkSnapshot()
	next := mkSnapshot(store.Item{ID: "p1", Kind: store.KindProposal})

	events := Diff(prev, next)
	if len(events) != 1 || events[0].ItemKind != store.KindProposal {
		t.Fatalf("want one EventItemAppeared with ItemKind=KindProposal, got %+v", events)
	}
}

// TestDiffNeverMutatesInputs confirms Diff leaves prev and next byte-for-byte
// identical to how they were before the call, including through their
// pointer fields (Blocker/TaskProgress) — the hard "no mutation" invariant
// design.md's package doc and task [2.1] both call out explicitly.
func TestDiffNeverMutatesInputs(t *testing.T) {
	prev := mkSnapshot(
		store.Item{ID: "a", Kind: store.KindBead, Blocker: &store.BlockerNote{Type: "t", Reason: "r"}},
		store.Item{ID: "shared", Kind: store.KindBead},
	)
	next := mkSnapshot(
		store.Item{ID: "shared", Kind: store.KindBead, Stale: true},
		store.Item{ID: "b", Kind: store.KindProposal},
	)

	prevBefore := deepCopySnapshot(prev)
	nextBefore := deepCopySnapshot(next)

	_ = Diff(prev, next)

	if !reflect.DeepEqual(prev, prevBefore) {
		t.Fatalf("Diff mutated prev: before=%+v after=%+v", prevBefore, prev)
	}
	if !reflect.DeepEqual(next, nextBefore) {
		t.Fatalf("Diff mutated next: before=%+v after=%+v", nextBefore, next)
	}
}

// TestDiffIsDeterministicAcrossRepeatedCalls confirms calling Diff twice
// with identical inputs produces identical output — the property the
// explicit sort inside Diff exists for, since Go map iteration order would
// otherwise reshuffle event order non-deterministically across calls.
func TestDiffIsDeterministicAcrossRepeatedCalls(t *testing.T) {
	prev := mkSnapshot(
		store.Item{ID: "z", Kind: store.KindBead},
		store.Item{ID: "a", Kind: store.KindProposal},
		store.Item{ID: "m", Kind: store.KindBead},
	)
	next := mkSnapshot(
		store.Item{ID: "n1", Kind: store.KindBead},
		store.Item{ID: "n2", Kind: store.KindProposal},
	)

	first := Diff(prev, next)
	for i := 0; i < 20; i++ {
		again := Diff(prev, next)
		if !reflect.DeepEqual(first, again) {
			t.Fatalf("Diff produced different output on repeated call %d:\nfirst=%+v\nagain=%+v", i, first, again)
		}
	}
}

// deepCopySnapshot copies s, including through its Items' pointer fields,
// preserving nil-vs-empty-slice distinctions on Items/Errors — reflect.
// DeepEqual treats a nil slice and a non-nil empty slice as unequal, so a
// copy that always allocates via make() would produce a spurious mismatch
// against a snapshot whose Items/Errors were never populated at all.
func deepCopySnapshot(s store.Snapshot) store.Snapshot {
	var items []store.Item
	if s.Items != nil {
		items = make([]store.Item, len(s.Items))
		for i, it := range s.Items {
			items[i] = it
			if it.Blocker != nil {
				b := *it.Blocker
				items[i].Blocker = &b
			}
			if it.TaskProgress != nil {
				tp := *it.TaskProgress
				items[i].TaskProgress = &tp
			}
		}
	}
	var errs []store.SourceError
	if s.Errors != nil {
		errs = make([]store.SourceError, len(s.Errors))
		copy(errs, s.Errors)
	}
	return store.Snapshot{Items: items, Errors: errs, Generated: s.Generated}
}

// --- Process gating (design.md § Config + calm-mode + truecolor gating) --

// TestProcessSkipsDiffWhenDisabled proves task [2.4]'s disabled-gate
// invariant with a call-counting spy substituted for m.diff: when
// cfg.Enabled is false, Diff must never be invoked at all, not merely have
// its output discarded.
func TestProcessSkipsDiffWhenDisabled(t *testing.T) {
	m := NewFlairManager(config.FlairConfig{Enabled: false})
	var calls int
	m.diff = func(prev, next store.Snapshot) []FlairEvent {
		calls++
		return nil
	}

	prev := mkSnapshot(store.Item{ID: "a"})
	next := mkSnapshot(store.Item{ID: "b"})
	events := m.Process(prev, next)

	if calls != 0 {
		t.Fatalf("want Diff invoked 0 times when disabled, got %d", calls)
	}
	if events != nil {
		t.Fatalf("want Process to return nil when disabled, got %+v", events)
	}
}

// TestProcessCallsDiffWhenEnabled confirms the gate's other side: enabled
// managers do delegate to Diff.
func TestProcessCallsDiffWhenEnabled(t *testing.T) {
	m := NewFlairManager(config.FlairConfig{Enabled: true})
	var calls int
	m.diff = func(prev, next store.Snapshot) []FlairEvent {
		calls++
		return []FlairEvent{{Kind: EventItemAppeared, ItemID: "x"}}
	}

	events := m.Process(mkSnapshot(), mkSnapshot())

	if calls != 1 {
		t.Fatalf("want Diff invoked exactly once when enabled, got %d", calls)
	}
	if len(events) != 1 || events[0].ItemID != "x" {
		t.Fatalf("want Process to return Diff's output unchanged, got %+v", events)
	}
}

// TestManagerEffectForHonorsCalmMode confirms manager.go's EffectFor
// wrapper actually threads cfg.CalmMode through to effects.go's EffectFor —
// the centralization point task [2.4] requires.
func TestManagerEffectForHonorsCalmMode(t *testing.T) {
	animated := NewFlairManager(config.FlairConfig{Enabled: true, CalmMode: false})
	calm := NewFlairManager(config.FlairConfig{Enabled: true, CalmMode: true})

	ev := FlairEvent{Kind: EventBlockerResolved}
	if got := animated.EffectFor(ev); got != EffectGlyphPulse {
		t.Fatalf("want EffectGlyphPulse in animated mode, got %q", got)
	}
	if got := calm.EffectFor(ev); got != EffectStaticGlyph {
		t.Fatalf("want EffectStaticGlyph in calm mode, got %q", got)
	}
}
