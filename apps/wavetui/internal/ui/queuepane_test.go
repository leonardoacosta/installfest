package ui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/leonardoacosta/installfest/apps/wavetui/internal/store"
)

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
