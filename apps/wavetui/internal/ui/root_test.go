package ui

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/leonardoacosta/installfest/apps/wavetui/internal/store"
)

// spyPane counts Update calls — used only to observe how many times Root
// actually pushed a Snapshot into the panes, independent of QueuePane's own
// internal bubbles table state.
type spyPane struct {
	updates int
}

func (s *spyPane) Update(store.Snapshot) Pane { s.updates++; return s }
func (s *spyPane) View() string               { return "" }
func (s *spyPane) Focusable() bool            { return false }

var (
	_ Pane = (*QueuePane)(nil)
	_ Pane = (*DetailPane)(nil)
	_ Pane = (*spyPane)(nil)
)

// newTestRoot builds a Root the same way NewRoot does, but with an injected
// clock and an extra spyPane appended to the focus-ring/View-composition
// slice so tests can count pane-apply calls without depending on QueuePane's
// bubbles-table internals. queue/detail are still real, since Root's
// applySnapshot unconditionally calls r.queue.SelectedItem() and
// r.detail.SetSelected(...).
func newTestRoot(now func() time.Time) (*Root, *spyPane) {
	q := NewQueuePane()
	d := NewDetailPane()
	spy := &spyPane{}
	r := &Root{
		panes:  []Pane{q, d, spy},
		queue:  q,
		detail: d,
		now:    now,
	}
	r.focus = firstFocusable(r.panes)
	return r, spy
}

// TestRenderCoalescing is tasks.md [3.1]'s "roughly a 10fps cap" acceptance
// test: a burst of SnapshotMsgs arriving faster than renderInterval must
// collapse into at most one extra pane-apply, not one per message. The
// clock is injected and driven by hand rather than real timers, so the test
// is deterministic and fast regardless of renderInterval's actual value.
func TestRenderCoalescing(t *testing.T) {
	clockNow := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r, spy := newTestRoot(func() time.Time { return clockNow })

	snap := store.Snapshot{}

	// First SnapshotMsg after an idle Root applies immediately, no flush
	// command scheduled.
	if _, cmd := r.Update(SnapshotMsg{Snapshot: snap}); cmd != nil {
		t.Fatal("first snapshot after idle should apply immediately with no scheduled command")
	}
	if spy.updates != 1 {
		t.Fatalf("want 1 update after first snapshot, got %d", spy.updates)
	}

	// A burst of 50 more SnapshotMsgs arriving while the clock is frozen
	// (i.e. well inside the coalescing window) must not apply more than
	// once more, and must schedule exactly one flush command for the whole
	// burst — not one per message.
	var flushCmd tea.Cmd
	for i := 0; i < 50; i++ {
		_, cmd := r.Update(SnapshotMsg{Snapshot: snap})
		if cmd != nil {
			if flushCmd != nil {
				t.Fatal("more than one flush command scheduled during a single coalescing burst")
			}
			flushCmd = cmd
		}
	}
	if spy.updates != 1 {
		t.Fatalf("burst applied panes %d times inside the coalescing window, want still 1", spy.updates)
	}
	if flushCmd == nil {
		t.Fatal("want exactly one flush command scheduled for the burst")
	}

	// Draining the flush (simulating the coalescing window closing) applies
	// the coalesced latest snapshot exactly once — never once per burst
	// message.
	if _, cmd := r.Update(flushMsg{}); cmd != nil {
		t.Fatal("flush should not itself schedule another command")
	}
	if spy.updates != 2 {
		t.Fatalf("want 2 updates after flush drains the burst, got %d", spy.updates)
	}

	// A second flush with nothing pending is a no-op.
	r.Update(flushMsg{})
	if spy.updates != 2 {
		t.Fatalf("flushing with nothing pending must not re-apply, got %d updates", spy.updates)
	}

	// Once the clock advances past renderInterval, the next SnapshotMsg is
	// outside the coalescing window and applies immediately again.
	clockNow = clockNow.Add(renderInterval + time.Millisecond)
	if _, cmd := r.Update(SnapshotMsg{Snapshot: snap}); cmd != nil {
		t.Fatal("a snapshot outside the coalescing window should apply immediately with no scheduled command")
	}
	if spy.updates != 3 {
		t.Fatalf("want 3 updates once outside the coalescing window, got %d", spy.updates)
	}
}

// TestFocusRingCyclesOnlyFocusablePanes asserts Tab/Shift+Tab only ever land
// on a Focusable pane (DetailPane never gets focus — see design.md § Pane
// extensibility: today's ring is [queue(focusable), detail(not)], so tabbing
// must stay on queue).
func TestFocusRingCyclesOnlyFocusablePanes(t *testing.T) {
	r := NewRoot(NewQueuePane(), NewDetailPane())

	if !r.panes[r.focus].Focusable() {
		t.Fatalf("initial focus (index %d) is not on a Focusable pane", r.focus)
	}

	r.focusNext()
	if !r.panes[r.focus].Focusable() {
		t.Fatalf("focusNext landed on a non-Focusable pane (index %d)", r.focus)
	}

	r.focusPrev()
	if !r.panes[r.focus].Focusable() {
		t.Fatalf("focusPrev landed on a non-Focusable pane (index %d)", r.focus)
	}
}

// TestQuitKeySendsTeaQuit asserts the documented "q"/"ctrl+c" keybinding
// actually produces a tea.Quit command, since that's what main.go depends
// on to unwind the sources via cancel() once Program.Run() returns.
func TestQuitKeySendsTeaQuit(t *testing.T) {
	r := NewRoot(NewQueuePane(), NewDetailPane())

	_, cmd := r.Update(tea.KeyPressMsg{Text: "q", Code: 'q'})
	if cmd == nil {
		t.Fatal("want a non-nil command from the quit key")
	}
	if !r.quitting {
		t.Fatal("want r.quitting set true after the quit key")
	}
}
