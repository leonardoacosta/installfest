package flair

import (
	"strings"
	"testing"

	"github.com/leonardoacosta/installfest/apps/wavetui/internal/config"
	"github.com/leonardoacosta/installfest/apps/wavetui/internal/store"
)

// TestOnSnapshotStartsRowHighlightForClosedBead confirms task [3.2]'s core
// wiring contract: a bead closing between two snapshots produces a live
// row highlight for that item's ID once AdvanceFrame is called.
func TestOnSnapshotStartsRowHighlightForClosedBead(t *testing.T) {
	m := NewFlairManager(config.FlairConfig{Enabled: true})

	prev := mkSnapshot(store.Item{ID: "a", Kind: store.KindBead, Title: "Alpha"})
	next := mkSnapshot()

	events := m.OnSnapshot(prev, next)
	if len(events) != 1 || events[0].Kind != EventItemClosed {
		t.Fatalf("want one EventItemClosed, got %+v", events)
	}

	highlights, _ := m.AdvanceFrame()
	hl, ok := highlights["a"]
	if !ok {
		t.Fatalf("want a live highlight for item %q immediately after OnSnapshot, got %+v", "a", highlights)
	}
	if hl.Color == (colorfulZero) {
		t.Fatalf("want a non-zero color for a row-flash highlight, got zero value")
	}

	if !m.NeedsTick() {
		t.Fatal("want NeedsTick()==true immediately after starting a row-flash animation")
	}
}

// colorfulZero is the zero value of colorful.Color, used only as a sentinel
// comparison above.
var colorfulZero = HighlightState{}.Color

// TestOnSnapshotDisabledStartsNothing confirms the disabled-gate invariant
// extends through OnSnapshot: Process already guarantees Diff never runs,
// so start() is never reached and active/toast state stays empty.
func TestOnSnapshotDisabledStartsNothing(t *testing.T) {
	m := NewFlairManager(config.FlairConfig{Enabled: false})

	prev := mkSnapshot(store.Item{ID: "a", Kind: store.KindBead})
	next := mkSnapshot()

	events := m.OnSnapshot(prev, next)
	if events != nil {
		t.Fatalf("want no events when disabled, got %+v", events)
	}
	if m.NeedsTick() {
		t.Fatal("want NeedsTick()==false when disabled — nothing should have started")
	}

	highlights, toast := m.AdvanceFrame()
	if highlights != nil || toast != nil {
		t.Fatalf("want no highlights/toast when disabled, got highlights=%+v toast=%+v", highlights, toast)
	}
}

// TestOnSnapshotQueuesToastForAppearedProposal confirms a newly-appeared
// proposal queues a toast (not a row highlight — proposals are not rows in
// wavetui-flair's row-highlight map) whose message carries the item's real
// Title, looked up from `next` since FlairEvent itself only carries ID/Kind.
func TestOnSnapshotQueuesToastForAppearedProposal(t *testing.T) {
	m := NewFlairManager(config.FlairConfig{Enabled: true})

	prev := mkSnapshot()
	next := mkSnapshot(store.Item{ID: "p1", Kind: store.KindProposal, Title: "Add widgets"})

	m.OnSnapshot(prev, next)

	if !m.NeedsTick() {
		t.Fatal("want NeedsTick()==true once a toast is queued")
	}

	highlights, toast := m.AdvanceFrame()
	if len(highlights) != 0 {
		t.Fatalf("a proposal appearing must not produce a row highlight, got %+v", highlights)
	}
	if toast == nil {
		t.Fatal("want a live ToastRender once the queued toast is promoted")
	}
	if !strings.Contains(toast.Message, "Add widgets") {
		t.Fatalf("want the toast message to carry the item's real Title, got %q", toast.Message)
	}
}

// TestAdvanceFrameEventuallySettles confirms a started row animation
// eventually clears (Done()) rather than animating forever, and NeedsTick
// reflects that — the same "zero-idle-cost invariant" TestNeedsTickReflects
// ActiveState already covers via direct map mutation, exercised here
// through the real OnSnapshot/AdvanceFrame path instead.
func TestAdvanceFrameEventuallySettles(t *testing.T) {
	m := NewFlairManager(config.FlairConfig{Enabled: true})
	m.OnSnapshot(mkSnapshot(store.Item{ID: "a", Kind: store.KindBead}), mkSnapshot())

	const maxFrames = 10_000 // generous cap — a real settle happens in well under a second of ticks
	settled := false
	for i := 0; i < maxFrames; i++ {
		m.AdvanceFrame()
		if !m.NeedsTick() {
			settled = true
			break
		}
	}
	if !settled {
		t.Fatalf("row-flash animation never settled within %d frames", maxFrames)
	}
}

// TestCalmModeUsesStaticHighlighterAndStillSettles confirms calm mode
// (design.md § Config + calm-mode + truecolor gating point 2) still starts
// a highlight — state signals still update — but it is a fixed one-shot
// state that eventually clears on its own, never an animated spring.
func TestCalmModeUsesStaticHighlighterAndStillSettles(t *testing.T) {
	m := NewFlairManager(config.FlairConfig{Enabled: true, CalmMode: true})
	m.OnSnapshot(mkSnapshot(store.Item{ID: "a", Kind: store.KindBead, Blocker: &store.BlockerNote{Type: "x"}}), mkSnapshot(store.Item{ID: "a", Kind: store.KindBead}))

	highlights, _ := m.AdvanceFrame()
	if _, ok := highlights["a"]; !ok {
		t.Fatalf("want calm mode to still produce a highlight for a blocker-resolved item, got %+v", highlights)
	}

	const maxFrames = 10_000
	settled := false
	for i := 0; i < maxFrames; i++ {
		m.AdvanceFrame()
		if !m.NeedsTick() {
			settled = true
			break
		}
	}
	if !settled {
		t.Fatalf("calm-mode static highlight never settled within %d frames", maxFrames)
	}
}

// TestToastQueueOnlyOneActiveAtATime confirms two toast-worthy events queue
// rather than both animating at once — the second is only promoted once
// the first's effect Done()s.
func TestToastQueueOnlyOneActiveAtATime(t *testing.T) {
	m := NewFlairManager(config.FlairConfig{Enabled: true})
	prev := mkSnapshot()
	next := mkSnapshot(
		store.Item{ID: "p1", Kind: store.KindProposal, Title: "First"},
		store.Item{ID: "p2", Kind: store.KindProposal, Title: "Second"},
	)
	m.OnSnapshot(prev, next)

	_, toast := m.AdvanceFrame()
	if toast == nil {
		t.Fatal("want an active toast immediately")
	}
	firstMsg := toast.Message

	// Drain the first toast's full lifecycle (spring in, dwell, spring back
	// out) — generous cap since ToastSpringEffect's real dwell is seconds,
	// not frames.
	const maxFrames = 20_000
	sawSecond := false
	for i := 0; i < maxFrames; i++ {
		_, toast = m.AdvanceFrame()
		if toast != nil && toast.Message != firstMsg {
			sawSecond = true
			break
		}
	}
	if !sawSecond {
		t.Fatal("want the second queued toast to be promoted after the first finishes")
	}
}
