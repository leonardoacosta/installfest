package ui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/leonardoacosta/installfest/apps/wavetui/internal/bus"
	"github.com/leonardoacosta/installfest/apps/wavetui/internal/daemon"
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

// TestUnavailableBadgeRendersFromSnapshotErrors is spec.md's "A missing
// .beads/ or openspec/ directory degrades to an unavailable badge, never a
// crash" Requirement, exercised at the Root level: a Snapshot carrying a
// SourceError for "beads" must produce a visible "beads unavailable" badge
// in View(), and that badge must clear once a later Snapshot reports no
// errors (the live "unavailable -> available" transition from task 2.3).
func TestUnavailableBadgeRendersFromSnapshotErrors(t *testing.T) {
	clockNow := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r := NewRoot(NewQueuePane(), NewDetailPane())
	r.now = func() time.Time { return clockNow }
	r.width, r.height = 100, 30
	r.layout()

	r.Update(SnapshotMsg{Snapshot: store.Snapshot{
		Errors: []store.SourceError{{Source: "beads", Message: "unavailable: .beads/ not found"}},
	}})

	view := r.View().Content
	if !strings.Contains(view, "beads unavailable") {
		t.Fatalf("want a %q badge in the rendered view, got:\n%s", "beads unavailable", view)
	}

	// The transition back to available (task 2.3: no restart needed) must
	// clear the badge on the very next snapshot. Advance the injected clock
	// past renderInterval first so this Update applies immediately rather
	// than coalescing with the prior one (same clock-driving pattern as
	// TestRenderCoalescing).
	clockNow = clockNow.Add(renderInterval + time.Millisecond)
	r.Update(SnapshotMsg{Snapshot: store.Snapshot{}})
	view = r.View().Content
	if strings.Contains(view, "unavailable") {
		t.Fatalf("badge did not clear after a snapshot with no errors, got:\n%s", view)
	}
}

// TestUnavailableBadgeDistinguishesMissingDirFromTransientFailure is the
// post-wave gate finding's regression test: a genuinely-missing source
// directory (sources/*.go's publishUnavailable, "unavailable: ..." message)
// must render as "<source> unavailable", while a transient CLI/parse
// failure (markStale, an arbitrary error message with no such prefix) must
// render distinctly and surface the real SourceError.Message instead of
// claiming the directory is gone.
func TestUnavailableBadgeDistinguishesMissingDirFromTransientFailure(t *testing.T) {
	missing := badgeText(store.SourceError{Source: "beads", Message: "unavailable: .beads/ not found"})
	if missing != "beads unavailable" {
		t.Fatalf("want %q for a genuinely-missing directory, got %q", "beads unavailable", missing)
	}

	transient := badgeText(store.SourceError{Source: "beads", Message: "bd list: connection refused"})
	if transient == missing {
		t.Fatalf("a transient CLI failure rendered identically to the missing-directory badge: %q", transient)
	}
	if strings.Contains(transient, "beads unavailable") {
		t.Fatalf("a transient CLI failure must not claim the directory is unavailable, got %q", transient)
	}
	if !strings.Contains(transient, "beads") || !strings.Contains(transient, "connection refused") {
		t.Fatalf("want the real failure message surfaced, got %q", transient)
	}
}

// --- wavetui-headless-discoverability (tasks.md [2.1]) admission hint -----

// TestAdmissionHintEmptyWithoutHeadlessBar asserts admissionHint() returns ""
// when no HeadlessBar has ever been appended (e.g. every pre-existing test
// in this package, which constructs a bare Root via NewRoot) — the "render
// unchanged when the wiring doesn't exist" convention this method's own doc
// comment documents.
func TestAdmissionHintEmptyWithoutHeadlessBar(t *testing.T) {
	r := NewRoot(NewQueuePane(), NewDetailPane())
	if got := r.admissionHint(); got != "" {
		t.Fatalf("admissionHint() with no HeadlessBar appended = %q, want empty string", got)
	}
}

// TestAdmissionHintAppearsInViewAndFlipsOnToggle is the E2E-level assertion
// behind tasks.md [1.1]/[1.2]: once a HeadlessBar is appended, its
// AdmissionHint() text is always present in Root.View()'s rendered content
// (regardless of HeadlessBar.View()'s own empty-common-case contract), and
// that text flips immediately after the admissionToggleKey is pressed on the
// focused HeadlessBar — the same live-flip behavior
// TestHeadlessBarAdmissionHintFlipsOnToggle asserts at the HeadlessBar level,
// exercised here through Root's own View()/handleKey wiring.
func TestAdmissionHintAppearsInViewAndFlipsOnToggle(t *testing.T) {
	r := NewRoot(NewQueuePane(), NewDetailPane())
	r.width, r.height = 100, 30
	r.layout()

	ctrl := daemon.NewController(daemon.NewHeadlessDispatcher(2, bus.New()))
	hb := NewHeadlessBar(ctrl)
	r.AppendPane(hb)

	view := r.View().Content
	if !strings.Contains(view, "a: headless dispatch (off)") {
		t.Fatalf("want the off-state admission hint in a fresh Root's View(), got:\n%s", view)
	}

	// Focus the HeadlessBar pane so handleKey's *HeadlessBar type-assertion
	// branch (root.go) routes the keypress to it, then press the toggle key
	// through Root's own Update — not HeadlessBar.HandleKey directly — so
	// this exercises the real dispatch path an operator's keypress travels.
	r.focus = indexOf(r.panes, Pane(hb))
	r.Update(tea.KeyPressMsg{Text: admissionToggleKey})

	view = r.View().Content
	if !strings.Contains(view, "a: headless dispatch (on)") {
		t.Fatalf("want the on-state admission hint after pressing %q, got:\n%s", admissionToggleKey, view)
	}
	if strings.Contains(view, "a: headless dispatch (off)") {
		t.Fatalf("off-state hint text must not still be present after toggling on, got:\n%s", view)
	}
}

// --- wavetui-context-pane (tasks.md [3.2]) tab wiring ----------------------

// TestSwitchToContextTabNoopWithoutEnableContextPane mirrors switchTab's
// existing tabMemories guard: pressing "3" before EnableContextPane has ever
// been called (e.g. every pre-existing test in this package) must be a
// silent no-op, never a nil-pointer panic.
func TestSwitchToContextTabNoopWithoutEnableContextPane(t *testing.T) {
	r := NewRoot(NewQueuePane(), NewDetailPane())
	r.Update(tea.KeyPressMsg{Text: "3"})
	if r.activeTab != tabItems {
		t.Fatalf("activeTab = %d, want unchanged tabItems (0) when no ContextPane is wired", r.activeTab)
	}
}

// TestContextTabSwitchesContentAndRoutesKeys is the E2E-level assertion
// behind tasks.md [3.2]: pressing "3" renders the Context tab's own content
// in place of the queue/detail row, and a subsequent keypress routes to
// *ContextPane via Root's handleKey type-assertion (mirroring
// TestAdmissionHintAppearsInViewAndFlipsOnToggle's real-dispatch-path
// precedent for *HeadlessBar).
func TestContextTabSwitchesContentAndRoutesKeys(t *testing.T) {
	r := NewRoot(NewQueuePane(), NewDetailPane())
	r.width, r.height = 100, 30
	r.layout()

	refreshed := 0
	pane := NewContextPane(func() { refreshed++ })
	r.EnableContextPane(pane)

	r.Update(SnapshotMsg{Snapshot: store.Snapshot{
		CtxScan: &store.CtxScanReport{
			ProjectName: "fixture-proj",
			Classes: []store.CtxScanClass{
				{Class: "memory", Label: "Memory (MEMORY.md)", TotalTokens: 10, WorstBand: "GREEN"},
			},
		},
	}})

	r.Update(tea.KeyPressMsg{Text: "3"})
	if r.activeTab != tabScan {
		t.Fatalf("activeTab = %d, want tabScan (%d) after pressing 3", r.activeTab, tabScan)
	}

	view := r.View().Content
	if !strings.Contains(view, "Memory (MEMORY.md)") {
		t.Fatalf("want the Context tab's own class content in View(), got:\n%s", view)
	}

	// Focus is already on the ContextPane (switchTab moves it there) — a
	// further keypress must route through Root.Update -> handleKey's
	// *ContextPane case, not silently no-op.
	r.Update(key("r"))
	if refreshed != 1 {
		t.Fatalf("want the ContextPane's injected refresh invoked once via Root's real key-dispatch path, got %d calls", refreshed)
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
