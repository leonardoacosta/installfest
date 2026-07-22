package ui

import (
	"strings"
	"testing"

	"github.com/leonardoacosta/installfest/apps/wavetui/internal/store"
)

func TestDetailPaneNotFocusable(t *testing.T) {
	d := NewDetailPane()
	if d.Focusable() {
		t.Fatal("DetailPane must not be Focusable — it's a read-only reflection of QueuePane's selection")
	}
}

func TestDetailPaneNoSelectionView(t *testing.T) {
	d := NewDetailPane()
	if got := d.View(); !strings.Contains(got, "No item selected") {
		t.Fatalf("want a placeholder view with no selection, got %q", got)
	}
}

// TestDetailPaneRendersBlockerAndTaskProgress asserts tasks.md [3.3]'s
// required content — notes/blocker reason and task progress — actually
// appears in View() once SetSelected wires an item in.
func TestDetailPaneRendersBlockerAndTaskProgress(t *testing.T) {
	d := NewDetailPane()
	d.SetSelected(store.Item{
		ID:    "if-xj6u",
		Title: "Wire cmd/wavetui/main.go end-to-end",
		Kind:  store.KindBead,
		Blocker: &store.BlockerNote{
			Type:   "dependency",
			Reason: "waiting on 3.2/3.3",
			Ref:    "if-7c4o",
		},
		TaskProgress: &store.TaskProgress{Done: 2, Total: 4},
		FanOutScore:  1,
	}, true)

	view := d.View()
	for _, want := range []string{
		"Wire cmd/wavetui/main.go end-to-end",
		"if-xj6u",
		"dependency",
		"waiting on 3.2/3.3",
		"if-7c4o",
		"2/4",
		"Fan-out score: 1",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("DetailPane.View() missing %q\nfull view:\n%s", want, view)
		}
	}
}

// TestDetailPaneRendersDescription covers wavetui-item-description's spec.md
// "an item with a description shows it in the detail pane" scenario.
func TestDetailPaneRendersDescription(t *testing.T) {
	d := NewDetailPane()
	d.SetSelected(store.Item{
		ID:          "if-1",
		Title:       "Add thing",
		Description: "This is what the item is about.",
	}, true)

	if got := d.View(); !strings.Contains(got, "This is what the item is about.") {
		t.Fatalf("DetailPane.View() missing Description text, got:\n%s", got)
	}
}

// TestDetailPaneNoDescriptionShowsNoExtraSection covers spec.md's "an item
// with no description shows no extra section" scenario — absence renders as
// nothing, not a blank placeholder label.
func TestDetailPaneNoDescriptionShowsNoExtraSection(t *testing.T) {
	d := NewDetailPane()
	d.SetSelected(store.Item{ID: "a", Title: "Clean item"}, true)

	if got := d.View(); strings.Contains(strings.ToLower(got), "description") {
		t.Fatalf("want no description label/placeholder for an item with empty Description, got:\n%s", got)
	}
}

func TestDetailPaneUnblockedRendersUnblocked(t *testing.T) {
	d := NewDetailPane()
	d.SetSelected(store.Item{ID: "a", Title: "Clean item"}, true)

	if got := d.View(); !strings.Contains(got, "Unblocked") {
		t.Fatalf("want %q in view for an item with no Blocker, got:\n%s", "Unblocked", got)
	}
}

func TestDetailPaneStaleBadge(t *testing.T) {
	d := NewDetailPane()
	d.SetSelected(store.Item{ID: "a", Title: "Flaky source", Stale: true}, true)

	if got := d.View(); !strings.Contains(got, "stale") {
		t.Fatalf("want a stale indicator in view, got:\n%s", got)
	}
}

// TestDetailPaneUpdateSyncsFieldsWithoutChangingSelection asserts the
// Pane-interface Update re-syncs the displayed item's own fields (e.g. a
// blocker resolving) without needing SetSelected to be called again.
func TestDetailPaneUpdateSyncsFieldsWithoutChangingSelection(t *testing.T) {
	d := NewDetailPane()
	d.SetSelected(store.Item{ID: "a", Title: "Item A", FanOutScore: 0}, true)

	d.Update(store.Snapshot{Items: []store.Item{
		{ID: "a", Title: "Item A", FanOutScore: 5},
		{ID: "b", Title: "Item B"},
	}})

	if got := d.View(); !strings.Contains(got, "Fan-out score: 5") {
		t.Fatalf("want FanOutScore refreshed to 5 via Update, got:\n%s", got)
	}
}

// TestDetailPaneUpdateClearsSelectionWhenItemRemoved asserts a Snapshot that
// no longer contains the selected item's ID clears the selection rather
// than continuing to show stale data for a resolved/closed/archived item.
func TestDetailPaneUpdateClearsSelectionWhenItemRemoved(t *testing.T) {
	d := NewDetailPane()
	d.SetSelected(store.Item{ID: "a", Title: "Item A"}, true)

	d.Update(store.Snapshot{Items: []store.Item{
		{ID: "b", Title: "Item B"},
	}})

	if got := d.View(); !strings.Contains(got, "No item selected") {
		t.Fatalf("want selection cleared once the item disappears from the snapshot, got:\n%s", got)
	}
}
