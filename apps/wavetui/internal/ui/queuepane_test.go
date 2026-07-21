package ui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/lucasb-eyer/go-colorful"

	"github.com/leonardoacosta/installfest/apps/wavetui/internal/dispatch"
	"github.com/leonardoacosta/installfest/apps/wavetui/internal/flair"
	"github.com/leonardoacosta/installfest/apps/wavetui/internal/store"
)

// fakeDispatcher is a test-only dispatch.Dispatcher recording every call and
// returning a caller-configured error — the same fake-injection rationale
// resolver.go's own doc comment cites for typing Resolver.Tmux/Clipboard as
// the Dispatcher interface rather than a concrete type.
type fakeDispatcher struct {
	err        error
	calls      int
	lastItem   store.Item
	lastPrompt string
}

func (f *fakeDispatcher) Dispatch(_ context.Context, item store.Item, promptText string) error {
	f.calls++
	f.lastItem = item
	f.lastPrompt = promptText
	return f.err
}

func TestQueuePaneFocusable(t *testing.T) {
	q := NewQueuePane()
	if !q.Focusable() {
		t.Fatal("QueuePane must be Focusable")
	}
}

// TestQueuePaneUpdateBuildsSelection asserts Update populates items in
// Snapshot order and that SelectedItem/SelectedID reflect the table's
// initial cursor (row 0).
func TestQueuePaneUpdateBuildsSelection(t *testing.T) {
	q := NewQueuePane()
	snap := store.Snapshot{Items: []store.Item{
		{ID: "a", Kind: store.KindBead, Title: "Alpha"},
		{ID: "b", Kind: store.KindProposal, Title: "Beta"},
	}}

	pane := q.Update(snap)
	if pane != Pane(q) {
		t.Fatal("Update must return the same *QueuePane, not a new Pane value")
	}

	item, ok := q.SelectedItem()
	if !ok {
		t.Fatal("want a selection after Update with a non-empty snapshot")
	}
	if item.ID != "a" {
		t.Fatalf("want initial selection %q, got %q", "a", item.ID)
	}
}

// TestQueuePaneHandleKeyMovesSelection asserts a down-arrow key press
// (forwarded via HandleKey, the Pane-interface-external navigation hook
// Root uses) moves the selection to the next row.
func TestQueuePaneHandleKeyMovesSelection(t *testing.T) {
	q := NewQueuePane()
	q.Update(store.Snapshot{Items: []store.Item{
		{ID: "a", Title: "Alpha"},
		{ID: "b", Title: "Beta"},
	}})

	q.HandleKey(tea.KeyPressMsg{Code: tea.KeyDown})

	if got := q.SelectedID(); got != "b" {
		t.Fatalf("want selection %q after down-arrow, got %q", "b", got)
	}
}

// TestQueuePanePreservesSelectionAcrossRebuild is the regression this
// pane's Update doc comment calls out: a snapshot refresh that keeps the
// same item IDs (just updated fields) must not silently reset the cursor
// back to row 0.
func TestQueuePanePreservesSelectionAcrossRebuild(t *testing.T) {
	q := NewQueuePane()
	q.Update(store.Snapshot{Items: []store.Item{
		{ID: "a", Title: "Alpha"},
		{ID: "b", Title: "Beta"},
		{ID: "c", Title: "Gamma"},
	}})
	q.HandleKey(tea.KeyPressMsg{Code: tea.KeyDown}) // select "b"
	if got := q.SelectedID(); got != "b" {
		t.Fatalf("setup: want selection %q, got %q", "b", got)
	}

	// A fresh snapshot with the same IDs but different field values (e.g.
	// FanOutScore bumped) must keep the selection on "b", not reset to "a".
	q.Update(store.Snapshot{Items: []store.Item{
		{ID: "a", Title: "Alpha"},
		{ID: "b", Title: "Beta", FanOutScore: 3},
		{ID: "c", Title: "Gamma"},
	}})

	if got := q.SelectedID(); got != "b" {
		t.Fatalf("selection was not preserved across rebuild: got %q, want %q", got, "b")
	}
}

// TestQueuePaneViewRendersRows is a regression test for a real bug caught
// during task 3.4's runtime-evidence run: bubbles/v2's viewport.View()
// returns "" whenever the viewport's Width() is 0, so a table with no width
// ever set renders its header with ZERO visible body rows — even though
// Update had populated real rows and a real selection existed. NewQueuePane
// now sets a default width/height for exactly this reason (see
// defaultQueueWidth/Height's doc comment) — this test asserts the row text
// actually appears in View(), not just that SelectedItem() succeeds (which
// alone would have passed even with this bug present).
func TestQueuePaneViewRendersRows(t *testing.T) {
	q := NewQueuePane()
	q.Update(store.Snapshot{Items: []store.Item{
		{ID: "a", Kind: store.KindBead, Title: "Alpha item"},
	}})

	view := q.View()
	if !strings.Contains(view, "Alpha item") {
		t.Fatalf("QueuePane.View() did not render the row's title — want %q in:\n%s", "Alpha item", view)
	}
}

func TestFormatCreatedAt(t *testing.T) {
	if got := formatCreatedAt(time.Time{}); got != "-" {
		t.Fatalf("zero time: want %q, got %q", "-", got)
	}
	tm := time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC)
	if got := formatCreatedAt(tm); got != "2026-07-04" {
		t.Fatalf("want %q, got %q", "2026-07-04", got)
	}
}

// TestQueuePaneSecondClassItemRendersDistinctFromProposal is the post-wave
// gate finding's regression test: spec.md's OpenSpecSource Requirement says
// a plans/advisor-plans item ("SecondClass") SHALL render "visually
// second-class" — not identically to a real openspec/changes/ proposal.
// Renders two otherwise-identical items (same Kind, same Title) that differ
// ONLY in SecondClass, and asserts their rendered queue rows differ — not
// just the underlying field, the actual output QueuePane.View() produces.
func TestQueuePaneSecondClassItemRendersDistinctFromProposal(t *testing.T) {
	proposal := NewQueuePane()
	proposal.Update(store.Snapshot{Items: []store.Item{
		{ID: "a", Kind: store.KindProposal, Title: "Same Title"},
	}})
	proposalRow := firstDataRow(proposal.View())

	plansItem := NewQueuePane()
	plansItem.Update(store.Snapshot{Items: []store.Item{
		{ID: "a", Kind: store.KindProposal, Title: "Same Title", SecondClass: true},
	}})
	plansRow := firstDataRow(plansItem.View())

	// Both rows must still show the title text — this proves the difference
	// below is styling, not a content/data difference.
	if !strings.Contains(proposalRow, "Same Title") || !strings.Contains(plansRow, "Same Title") {
		t.Fatalf("both rows must render the title text; proposal=%q plans=%q", proposalRow, plansRow)
	}
	if proposalRow == plansRow {
		t.Fatalf("a SecondClass (plans/advisor-plans) item rendered IDENTICALLY to a real proposal row — want distinct styling:\n%q", proposalRow)
	}
}

// firstDataRow returns the first row of a QueuePane.View() rendering below
// the header line.
func firstDataRow(view string) string {
	lines := strings.Split(view, "\n")
	if len(lines) < 2 {
		return ""
	}
	return lines[1]
}

// TestSetHighlightsNilOrEmptyRendersUnchanged is task [3.1]'s core contract:
// a nil map (never called) and an explicitly-empty map must both render
// byte-for-byte identically to a QueuePane that never had SetHighlights
// touched at all.
func TestSetHighlightsNilOrEmptyRendersUnchanged(t *testing.T) {
	snap := store.Snapshot{Items: []store.Item{
		{ID: "a", Kind: store.KindBead, Title: "Alpha item"},
	}}

	baseline := NewQueuePane()
	baseline.Update(snap)
	want := baseline.View()

	untouched := NewQueuePane()
	untouched.Update(snap)
	if got := untouched.View(); got != want {
		t.Fatalf("never calling SetHighlights changed rendering:\nwant %q\ngot  %q", want, got)
	}

	nilMap := NewQueuePane()
	nilMap.SetHighlights(nil)
	nilMap.Update(snap)
	if got := nilMap.View(); got != want {
		t.Fatalf("SetHighlights(nil) changed rendering:\nwant %q\ngot  %q", want, got)
	}

	emptyMap := NewQueuePane()
	emptyMap.SetHighlights(map[string]flair.HighlightState{})
	emptyMap.Update(snap)
	if got := emptyMap.View(); got != want {
		t.Fatalf("SetHighlights(empty map) changed rendering:\nwant %q\ngot  %q", want, got)
	}
}

// TestSetHighlightsAppliesColorAndGlyphToMatchingRow confirms the additive
// path actually changes output for a highlighted item, and leaves an
// unhighlighted sibling row untouched.
func TestSetHighlightsAppliesColorAndGlyphToMatchingRow(t *testing.T) {
	q := NewQueuePane()
	q.SetHighlights(map[string]flair.HighlightState{
		"a": {Color: colorful.Color{R: 0, G: 1, B: 0}, Glyph: "!"},
	})
	q.Update(store.Snapshot{Items: []store.Item{
		{ID: "a", Kind: store.KindBead, Title: "Alpha item"},
		{ID: "b", Kind: store.KindBead, Title: "Beta item"},
	}})

	view := q.View()
	if !strings.Contains(view, "! Alpha item") {
		t.Fatalf("want highlighted row to carry its glyph prefix, got:\n%s", view)
	}
	if !strings.Contains(view, "Beta item") || strings.Contains(view, "! Beta item") {
		t.Fatalf("unhighlighted sibling row must render its plain title, got:\n%s", view)
	}
}

func TestBlockerBadge(t *testing.T) {
	cases := []struct {
		name string
		item store.Item
		want string
	}{
		{"unblocked", store.Item{}, ""},
		{"blocked", store.Item{Blocker: &store.BlockerNote{Type: "dependency"}}, "blocked:dependency"},
		{"stale", store.Item{Stale: true}, "stale"},
		// Blocker takes precedence over stale when both happen to be set —
		// matches sources/beads.go's markStale, which republishes a blocked
		// item's existing Blocker unchanged alongside Stale=true.
		{"blocked and stale", store.Item{Stale: true, Blocker: &store.BlockerNote{Type: "review"}}, "blocked:review"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := blockerBadge(c.item); got != c.want {
				t.Fatalf("want %q, got %q", c.want, got)
			}
		})
	}
}

// --- wavetui-dispatch (tasks.md [3.1]) Start action ---------------------

func TestQueuePaneStartDispatchesHighlightedItemViaResolver(t *testing.T) {
	q := NewQueuePane()
	q.Update(store.Snapshot{Items: []store.Item{
		{ID: "if-1234", Title: "Alpha"},
	}})

	fake := &fakeDispatcher{}
	q.SetDispatcher(context.Background(), fake)

	q.HandleKey(tea.KeyPressMsg{Code: tea.KeyEnter})

	if fake.calls != 1 {
		t.Fatalf("want Dispatch called exactly once, got %d", fake.calls)
	}
	if fake.lastItem.ID != "if-1234" {
		t.Fatalf("want the highlighted item dispatched, got %q", fake.lastItem.ID)
	}
	if fake.lastPrompt != "/apply if-1234" {
		t.Fatalf("want promptText %q, got %q", "/apply if-1234", fake.lastPrompt)
	}
}

func TestQueuePaneStartNoDispatcherIsNoop(t *testing.T) {
	q := NewQueuePane()
	q.Update(store.Snapshot{Items: []store.Item{{ID: "a", Title: "Alpha"}}})

	// SetDispatcher never called — must not panic.
	q.HandleKey(tea.KeyPressMsg{Code: tea.KeyEnter})

	if strings.Contains(q.View(), "failed") {
		t.Fatalf("want no badge rendered when no dispatcher is wired, got:\n%s", q.View())
	}
}

func TestQueuePaneStartRendersFailureBadgeOnDispatchError(t *testing.T) {
	q := NewQueuePane()
	q.Update(store.Snapshot{Items: []store.Item{{ID: "a", Title: "Alpha"}}})
	fake := &fakeDispatcher{err: errors.New("tmux: boom")}
	q.SetDispatcher(context.Background(), fake)

	q.HandleKey(tea.KeyPressMsg{Code: tea.KeyEnter})

	if got := firstDataRow(q.View()); !strings.Contains(got, "failed: tmux: boom") {
		t.Fatalf("want failure badge in row, got:\n%s", got)
	}
}

func TestQueuePaneStartRendersQueuedBadgeOnSessionStreamingRefusal(t *testing.T) {
	q := NewQueuePane()
	q.Update(store.Snapshot{Items: []store.Item{{ID: "a", Title: "Alpha"}}})
	fake := &fakeDispatcher{err: dispatch.ErrSessionStreaming}
	q.SetDispatcher(context.Background(), fake)

	q.HandleKey(tea.KeyPressMsg{Code: tea.KeyEnter})

	if got := firstDataRow(q.View()); !strings.Contains(got, "queued — session busy") {
		t.Fatalf("want queued-busy badge in row, got:\n%s", got)
	}
}

func TestQueuePaneStartSuccessClearsPriorFailureBadge(t *testing.T) {
	q := NewQueuePane()
	q.Update(store.Snapshot{Items: []store.Item{{ID: "a", Title: "Alpha"}}})
	fake := &fakeDispatcher{err: errors.New("boom")}
	q.SetDispatcher(context.Background(), fake)
	q.HandleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !strings.Contains(firstDataRow(q.View()), "failed") {
		t.Fatalf("setup: want a failure badge present before the retry")
	}

	fake.err = nil
	q.HandleKey(tea.KeyPressMsg{Code: tea.KeyEnter})

	if strings.Contains(firstDataRow(q.View()), "failed") {
		t.Fatalf("want the stale failure badge cleared after a successful retry, got:\n%s", firstDataRow(q.View()))
	}
}

// explodingDispatcher is a dispatch.Dispatcher stand-in that fails the test
// immediately if Dispatch is ever called on it — a spy, not a stub that
// happens to succeed. Used below to prove a non-id-shaped item.ID never
// reaches a real tmux/clipboard call.
type explodingDispatcher struct {
	t *testing.T
}

func (e explodingDispatcher) Dispatch(context.Context, store.Item, string) error {
	e.t.Fatal("dispatcher invoked: a non-id-shaped item.ID must be refused before any tmux/clipboard dispatch call")
	return nil
}

// TestQueuePaneStartRefusesNonIDShapedItemBeforeAnyDispatchAttempt is the
// regression test for the post-wave gate finding that validateDispatchTarget
// (dispatch.go) was fully implemented and unit-tested but had no real
// caller anywhere in the dispatch path — a non-id-shaped item.ID (e.g.
// "plan:foo", the exact shape internal/sources/openspec.go's
// parseFlatMarkdownDir mints for every plans/ and advisor-plans/ item via
// idPrefix+":"+name) would have sailed straight through QueuePane's Start
// action into a real TmuxDispatcher/ClipboardDispatcher call, pasting an
// unvalidated string into a real tmux pane.
//
// This exercises the REAL production chain, not a stand-in for it: a real
// *dispatch.Resolver (the exact type cmd/wavetui/main.go wires via
// SetDispatcher — see resolver.go's NewResolver doc comment), wired with
// explodingDispatcher spies in place of Tmux/Clipboard, driven through
// QueuePane's actual "enter" HandleKey path (startSelected -> Resolver.
// Dispatch). Calling validateDispatchTarget directly (dispatch_test.go)
// would prove nothing about whether the real dispatch path ever invokes it
// — this test fails loudly (via explodingDispatcher.t.Fatal) if
// Resolver.Dispatch ever again forgets to validate item.ID before either
// concrete Dispatcher runs.
func TestQueuePaneStartRefusesNonIDShapedItemBeforeAnyDispatchAttempt(t *testing.T) {
	q := NewQueuePane()
	q.Update(store.Snapshot{Items: []store.Item{
		{ID: "plan:foo", Title: "A plans/ item", SecondClass: true},
	}})

	resolver := &dispatch.Resolver{
		Tmux:      explodingDispatcher{t: t},
		Clipboard: explodingDispatcher{t: t},
	}
	q.SetDispatcher(context.Background(), resolver)

	q.HandleKey(tea.KeyPressMsg{Code: tea.KeyEnter})

	// The Blocker column is a fixed 24-wide table cell (queueColumns), so the
	// full validateDispatchTarget message ("...is not id-shaped, refusing to
	// cross the dispatch boundary") renders truncated with an ellipsis —
	// asserting on the message's own leading text ("dispatch target"), not
	// the tail that the column width cuts off.
	if got := firstDataRow(q.View()); !strings.Contains(got, "failed:") || !strings.Contains(got, "dispatch target") {
		t.Fatalf("want a refusal badge naming the id-shape failure, got:\n%s", got)
	}
}

// --- wavetui-dispatch (tasks.md [3.2]) select mode -----------------------

func TestQueuePaneToggleSelectedOrdersByFanOutScoreDescending(t *testing.T) {
	q := NewQueuePane()
	q.Update(store.Snapshot{Items: []store.Item{
		{ID: "a", Title: "Alpha", FanOutScore: 1},
		{ID: "b", Title: "Beta", FanOutScore: 5},
		{ID: "c", Title: "Gamma", FanOutScore: 3},
	}})

	// Select all three in table order (a, b, c) — SelectedForWave must
	// still return them fan-out-descending (b, c, a), not selection order.
	q.HandleKey(tea.KeyPressMsg{Code: tea.KeySpace}) // select "a"
	q.HandleKey(tea.KeyPressMsg{Code: tea.KeyDown})
	q.HandleKey(tea.KeyPressMsg{Code: tea.KeySpace}) // select "b"
	q.HandleKey(tea.KeyPressMsg{Code: tea.KeyDown})
	q.HandleKey(tea.KeyPressMsg{Code: tea.KeySpace}) // select "c"

	got := q.SelectedForWave()
	if len(got) != 3 {
		t.Fatalf("want 3 selected items, got %d: %+v", len(got), got)
	}
	wantOrder := []string{"b", "c", "a"}
	for i, want := range wantOrder {
		if got[i].ID != want {
			t.Fatalf("want order %v, got %v", wantOrder, idsOf(got))
		}
	}
}

func idsOf(items []store.Item) []string {
	ids := make([]string, len(items))
	for i, it := range items {
		ids[i] = it.ID
	}
	return ids
}

func TestQueuePaneToggleSelectedTwiceDeselects(t *testing.T) {
	q := NewQueuePane()
	q.Update(store.Snapshot{Items: []store.Item{{ID: "a", Title: "Alpha"}}})

	q.HandleKey(tea.KeyPressMsg{Code: tea.KeySpace})
	if got := q.SelectedForWave(); len(got) != 1 {
		t.Fatalf("want 1 selected after first toggle, got %d", len(got))
	}

	q.HandleKey(tea.KeyPressMsg{Code: tea.KeySpace})
	if got := q.SelectedForWave(); len(got) != 0 {
		t.Fatalf("want 0 selected after second toggle (deselect), got %d", len(got))
	}
}

func TestQueuePaneSelectMarkerRendersOnSelectedRow(t *testing.T) {
	q := NewQueuePane()
	q.Update(store.Snapshot{Items: []store.Item{{ID: "a", Title: "Alpha item"}}})

	q.HandleKey(tea.KeyPressMsg{Code: tea.KeySpace})

	if got := firstDataRow(q.View()); !strings.Contains(got, waveSelectMarker+"Alpha item") {
		t.Fatalf("want select marker prefixing the title, got:\n%s", got)
	}
}

func TestQueuePaneSelectModeRendersConflictWarningNamingBothIDs(t *testing.T) {
	q := NewQueuePane()
	q.Update(store.Snapshot{Items: []store.Item{
		{ID: "a", Title: "Alpha", TouchedFiles: []string{"shared.go"}},
		{ID: "b", Title: "Beta", TouchedFiles: []string{"shared.go"}},
	}})

	q.HandleKey(tea.KeyPressMsg{Code: tea.KeySpace}) // select "a"
	q.HandleKey(tea.KeyPressMsg{Code: tea.KeyDown})
	q.HandleKey(tea.KeyPressMsg{Code: tea.KeySpace}) // select "b"

	view := q.View()
	if !strings.Contains(view, "conflict: shared.go") || !strings.Contains(view, "a") || !strings.Contains(view, "b") {
		t.Fatalf("want a conflict warning naming both item IDs, got:\n%s", view)
	}
}

func TestQueuePaneNoConflictWarningWithoutOverlap(t *testing.T) {
	q := NewQueuePane()
	q.Update(store.Snapshot{Items: []store.Item{
		{ID: "a", Title: "Alpha", TouchedFiles: []string{"one.go"}},
		{ID: "b", Title: "Beta", TouchedFiles: []string{"two.go"}},
	}})

	q.HandleKey(tea.KeyPressMsg{Code: tea.KeySpace})
	q.HandleKey(tea.KeyPressMsg{Code: tea.KeyDown})
	q.HandleKey(tea.KeyPressMsg{Code: tea.KeySpace})

	if strings.Contains(q.View(), "conflict:") {
		t.Fatalf("want no conflict warning for non-overlapping files, got:\n%s", q.View())
	}
}

func TestQueuePaneEscClearsSelection(t *testing.T) {
	q := NewQueuePane()
	q.Update(store.Snapshot{Items: []store.Item{{ID: "a", Title: "Alpha"}}})
	q.HandleKey(tea.KeyPressMsg{Code: tea.KeySpace})
	if len(q.SelectedForWave()) != 1 {
		t.Fatalf("setup: want 1 selected")
	}

	q.HandleKey(tea.KeyPressMsg{Code: tea.KeyEscape})

	if got := q.SelectedForWave(); len(got) != 0 {
		t.Fatalf("want selection cleared after esc, got %d", len(got))
	}
}

// --- wavetui-dispatch (tasks.md [3.3]) wave finalization -----------------

func TestQueuePaneFinalizeWaveCallsWriterAndClearsSelection(t *testing.T) {
	q := NewQueuePane()
	q.Update(store.Snapshot{Items: []store.Item{
		{ID: "a", Title: "Alpha", FanOutScore: 2},
		{ID: "b", Title: "Beta", FanOutScore: 5},
	}})
	q.HandleKey(tea.KeyPressMsg{Code: tea.KeySpace})
	q.HandleKey(tea.KeyPressMsg{Code: tea.KeyDown})
	q.HandleKey(tea.KeyPressMsg{Code: tea.KeySpace})

	var gotItems []store.Item
	q.SetWaveWriter(func(items []store.Item) error {
		gotItems = items
		return nil
	})

	q.HandleKey(tea.KeyPressMsg{Text: "w"})

	if len(gotItems) != 2 || gotItems[0].ID != "b" || gotItems[1].ID != "a" {
		t.Fatalf("want writer called with FanOutScore-descending items, got %+v", gotItems)
	}
	if got := q.SelectedForWave(); len(got) != 0 {
		t.Fatalf("want selection cleared after a successful finalize, got %d", len(got))
	}
	if !strings.Contains(q.View(), "wave finalized: 2 item(s)") {
		t.Fatalf("want a success status line, got:\n%s", q.View())
	}
}

func TestQueuePaneFinalizeWaveNoSelectionReportsStatus(t *testing.T) {
	q := NewQueuePane()
	q.Update(store.Snapshot{Items: []store.Item{{ID: "a", Title: "Alpha"}}})
	called := false
	q.SetWaveWriter(func(items []store.Item) error {
		called = true
		return nil
	})

	q.HandleKey(tea.KeyPressMsg{Text: "w"})

	if called {
		t.Fatalf("want writer never called with an empty selection")
	}
	if !strings.Contains(q.View(), "no items selected") {
		t.Fatalf("want a status line explaining nothing was selected, got:\n%s", q.View())
	}
}

func TestQueuePaneFinalizeWaveNoWriterConfiguredIsSafe(t *testing.T) {
	q := NewQueuePane()
	q.Update(store.Snapshot{Items: []store.Item{{ID: "a", Title: "Alpha"}}})
	q.HandleKey(tea.KeyPressMsg{Code: tea.KeySpace})

	// SetWaveWriter never called — must not panic.
	q.HandleKey(tea.KeyPressMsg{Text: "w"})

	if !strings.Contains(q.View(), "no wave writer configured") {
		t.Fatalf("want a status line explaining no writer is wired, got:\n%s", q.View())
	}
}

func TestQueuePaneFinalizeWaveSurfacesWriterError(t *testing.T) {
	q := NewQueuePane()
	q.Update(store.Snapshot{Items: []store.Item{{ID: "a", Title: "Alpha"}}})
	q.HandleKey(tea.KeyPressMsg{Code: tea.KeySpace})
	q.SetWaveWriter(func(items []store.Item) error {
		return errors.New("disk full")
	})

	q.HandleKey(tea.KeyPressMsg{Text: "w"})

	if !strings.Contains(q.View(), "finalize failed: disk full") {
		t.Fatalf("want the writer's error surfaced, got:\n%s", q.View())
	}
	// A failed finalize must not silently clear the selection the operator
	// would otherwise need to retry.
	if got := q.SelectedForWave(); len(got) != 1 {
		t.Fatalf("want selection preserved after a failed finalize, got %d", len(got))
	}
}
