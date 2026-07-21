package flair

import (
	"testing"
	"time"

	"github.com/lucasb-eyer/go-colorful"

	"github.com/leonardoacosta/installfest/apps/wavetui/internal/store"
)

// TestShakeRedPulseOnlyFromNegative is the code-level assertion task [2.2]
// requires: exhaustively check every other known EventKind (including a
// hypothetical future one), both ItemKinds, and both calmMode states never
// resolve to EffectShakeRedPulse or its calm-mode sibling EffectStaticAlert.
func TestShakeRedPulseOnlyFromNegative(t *testing.T) {
	otherKinds := []EventKind{
		EventItemAppeared,
		EventItemClosed,
		EventBlockerResolved,
		EventKind("unknown_future_kind"),
		EventKind(""),
	}
	itemKinds := []store.ItemKind{store.KindBead, store.KindProposal}

	for _, k := range otherKinds {
		for _, ik := range itemKinds {
			for _, calm := range []bool{false, true} {
				eff := EffectFor(FlairEvent{Kind: k, ItemKind: ik}, calm)
				if eff == EffectShakeRedPulse || eff == EffectStaticAlert {
					t.Fatalf("EventKind %q (item %q, calm=%v) resolved to the negative-only effect %q", k, ik, calm, eff)
				}
			}
		}
	}
}

// TestEffectForNegativeAlwaysResolvesToAlarm confirms the one case that
// SHOULD reach the reserved effect actually does, in both calm and animated
// modes.
func TestEffectForNegativeAlwaysResolvesToAlarm(t *testing.T) {
	if got := EffectFor(FlairEvent{Kind: EventNegative}, false); got != EffectShakeRedPulse {
		t.Fatalf("want EffectShakeRedPulse, got %q", got)
	}
	if got := EffectFor(FlairEvent{Kind: EventNegative}, true); got != EffectStaticAlert {
		t.Fatalf("want EffectStaticAlert in calm mode, got %q", got)
	}
}

// TestEffectForCalmModeRoutesToStaticFallback checks gating point 2 from
// design.md § Config + calm-mode + truecolor gating: every non-negative
// event kind also has a static fallback, and calmMode=true always reaches
// it.
func TestEffectForCalmModeRoutesToStaticFallback(t *testing.T) {
	cases := []struct {
		ev   FlairEvent
		want EffectKind
	}{
		{FlairEvent{Kind: EventItemClosed, ItemKind: store.KindBead}, EffectStaticRowMark},
		{FlairEvent{Kind: EventItemClosed, ItemKind: store.KindProposal}, EffectStaticToast},
		{FlairEvent{Kind: EventItemAppeared, ItemKind: store.KindProposal}, EffectStaticToast},
		{FlairEvent{Kind: EventItemAppeared, ItemKind: store.KindBead}, EffectStaticRowMark},
		{FlairEvent{Kind: EventBlockerResolved}, EffectStaticGlyph},
	}
	for _, c := range cases {
		if got := EffectFor(c.ev, true); got != c.want {
			t.Fatalf("event %+v: calm mode want %q, got %q", c.ev, c.want, got)
		}
	}
}

// TestEffectForAnimatedModeResolvesDistinctEffects checks the non-calm-mode
// side of design.md's event->effect map resolves every combination to the
// documented effect (including the ItemKind branch for closed/appeared).
func TestEffectForAnimatedModeResolvesDistinctEffects(t *testing.T) {
	cases := []struct {
		ev   FlairEvent
		want EffectKind
	}{
		{FlairEvent{Kind: EventItemClosed, ItemKind: store.KindBead}, EffectRowFlash},
		{FlairEvent{Kind: EventItemClosed, ItemKind: store.KindProposal}, EffectToastOut},
		{FlairEvent{Kind: EventItemAppeared, ItemKind: store.KindProposal}, EffectToastIn},
		{FlairEvent{Kind: EventItemAppeared, ItemKind: store.KindBead}, EffectRowSlideIn},
		{FlairEvent{Kind: EventBlockerResolved}, EffectGlyphPulse},
		{FlairEvent{Kind: EventNegative}, EffectShakeRedPulse},
	}
	for _, c := range cases {
		if got := EffectFor(c.ev, false); got != c.want {
			t.Fatalf("event %+v: animated want %q, got %q", c.ev, c.want, got)
		}
	}
}

// TestRowFlashEffectSettles exercises the harmonica-decay + go-colorful-lerp
// primitive: it should eventually settle (Done()) rather than animate
// forever.
func TestRowFlashEffectSettles(t *testing.T) {
	e := NewRowFlashEffect(colorful.Color{R: 0, G: 1, B: 0}, colorful.Color{R: 0, G: 0, B: 0})
	settled := false
	for i := 0; i < 1000; i++ {
		e.Advance()
		if e.Done() {
			settled = true
			break
		}
	}
	if !settled {
		t.Fatal("want RowFlashEffect to settle within 1000 frames")
	}
}

// TestShakeRedPulseEffectSettles mirrors the above for the shake+pulse
// effect — the reserved negative effect must also converge, not oscillate
// indefinitely.
func TestShakeRedPulseEffectSettles(t *testing.T) {
	e := NewShakeRedPulseEffect(colorful.Color{R: 1, G: 1, B: 1})
	settled := false
	for i := 0; i < 1000; i++ {
		e.Advance()
		if e.Done() {
			settled = true
			break
		}
	}
	if !settled {
		t.Fatal("want ShakeRedPulseEffect to settle within 1000 frames")
	}
}

// TestToastSpringEffectAutoDismisses confirms the auto-dismiss timer
// actually flips the spring's target back off-screen and eventually
// reports Done() — the "auto-dismiss" half of task [2.3].
func TestToastSpringEffectAutoDismisses(t *testing.T) {
	e := NewToastSpringEffect(false)
	frameDuration := time.Second / time.Duration(frameRate)

	done := false
	for i := 0; i < 100000; i++ {
		e.Advance(frameDuration)
		if e.Done() {
			done = true
			break
		}
	}
	if !done {
		t.Fatal("want ToastSpringEffect to auto-dismiss (spring in, dwell, spring out, settle)")
	}
}
