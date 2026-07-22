package ui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/leonardoacosta/installfest/apps/wavetui/internal/store"
)

// detailWidth is the fixed content width DetailPane renders at — the split
// with QueuePane is otherwise just two lipgloss-bordered blocks joined
// horizontally (root.go's View), so a concrete width keeps wrapping
// predictable regardless of terminal size.
const detailWidth = 44

// DetailPane renders notes, blocker reason, task progress, and description
// for whichever item is currently selected in QueuePane — see design.md §
// Architecture, tasks.md [3.3], and wavetui-item-description's spec.md
// MODIFIED "DetailPane renders full detail..." Requirement. Selection itself
// is driven by QueuePane's cursor, not by
// this pane's own Update: SetSelected (outside the Pane interface, see
// root.go's Root doc comment) is Root's wiring hook, called every time
// QueuePane's cursor moves or a fresh Snapshot lands.
type DetailPane struct {
	selected store.Item
	hasSel   bool
}

// NewDetailPane constructs an empty DetailPane (no selection yet).
func NewDetailPane() *DetailPane { return &DetailPane{} }

// Update implements Pane. It does NOT decide which item is selected — that
// is SetSelected's job — it only re-syncs the currently-displayed item's own
// fields (blocker note, task progress, staleness) against the latest
// Snapshot, since those can change without QueuePane's cursor moving at all.
// If the previously-selected item is no longer present in the Snapshot (it
// was resolved/closed/archived), selection is cleared.
func (d *DetailPane) Update(snap store.Snapshot) Pane {
	if !d.hasSel {
		return d
	}
	for _, it := range snap.Items {
		if it.ID == d.selected.ID {
			d.selected = it
			return d
		}
	}
	d.hasSel = false
	d.selected = store.Item{}
	return d
}

// View implements Pane.
func (d *DetailPane) View() string {
	if !d.hasSel {
		return lipgloss.NewStyle().Width(detailWidth).Faint(true).Render("No item selected.")
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", lipgloss.NewStyle().Bold(true).Render(d.selected.Title))
	fmt.Fprintf(&b, "ID: %s   Kind: %s\n", d.selected.ID, d.selected.Kind)

	if d.selected.Description != "" {
		b.WriteString("\n")
		b.WriteString(d.selected.Description)
		b.WriteString("\n")
	}

	if d.selected.Blocker != nil {
		b.WriteString("\n")
		b.WriteString(lipgloss.NewStyle().Bold(true).Render("Blocked: " + d.selected.Blocker.Type))
		b.WriteString("\n")
		b.WriteString(d.selected.Blocker.Reason)
		if d.selected.Blocker.Ref != "" {
			fmt.Fprintf(&b, " (see %s)", d.selected.Blocker.Ref)
		}
		b.WriteString("\n")
	} else {
		b.WriteString("\nUnblocked.\n")
	}

	if d.selected.TaskProgress != nil {
		tp := d.selected.TaskProgress
		fmt.Fprintf(&b, "\nTasks: %d/%d done\n", tp.Done, tp.Total)
	}

	fmt.Fprintf(&b, "\nFan-out score: %d\n", d.selected.FanOutScore)

	if d.selected.Stale {
		b.WriteString("\n")
		b.WriteString(lipgloss.NewStyle().Faint(true).Render("(stale — last-good data, source is currently erroring)"))
		b.WriteString("\n")
	}

	return lipgloss.NewStyle().Width(detailWidth).Render(b.String())
}

// Focusable implements Pane. DetailPane is a read-only reflection of
// QueuePane's selection — there is nothing on it to navigate independently,
// so it never joins the focus ring.
func (d *DetailPane) Focusable() bool { return false }

// SetSelected is Root's wiring hook, called whenever QueuePane's cursor
// moves (via a key press) or a fresh Snapshot is applied (root.go's
// applySnapshot). Deliberately outside the Pane interface — see the Root doc
// comment in root.go for why.
func (d *DetailPane) SetSelected(item store.Item, ok bool) {
	d.hasSel = ok
	if ok {
		d.selected = item
	} else {
		d.selected = store.Item{}
	}
}
