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

	// height is the rendered content height threaded in from Root.layout()'s
	// already-computed tableHeight (task 2.5, spec.md's "the detail pane
	// matches the queue table's height" scenario) — see SetSize. 0 (the
	// zero value, and every pre-existing test's default) means "no explicit
	// height set yet," in which case View() renders at its natural content
	// height exactly as it always has, matching QueuePane's own
	// defaultQueueWidth/Height precedent of "an unset size must not break
	// existing behavior before layout() has ever run."
	height int
}

// NewDetailPane constructs an empty DetailPane (no selection yet).
func NewDetailPane() *DetailPane { return &DetailPane{} }

// SetSize sets DetailPane's rendered content height to match QueuePane's
// real table height (task 2.5) — mirrors QueuePane.SetSize's existing
// precedent (root.go's layout() calls both from the same already-computed
// tableHeight). width is accepted for signature symmetry with
// QueuePane.SetSize and the Sizeable interface (layout() type-asserts any
// appended pane against Sizeable), but DetailPane keeps its own fixed
// detailWidth — see that constant's doc comment for why a concrete width is
// load-bearing for predictable wrapping — so width is intentionally unused
// here.
func (d *DetailPane) SetSize(_ int, height int) {
	d.height = height
}

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

// View implements Pane. Its bordered box (root.go's paneStyle wraps this
// content, not View() itself) is sized to detailWidth x d.height whenever
// SetSize has been called (d.height > 0) — task 2.5, matching QueuePane's
// real table height so the two panes' bordered boxes line up exactly. A
// DetailPane that predates SetSize ever being called (d.height == 0, every
// pre-existing test in this package) renders at its natural content height,
// byte-for-byte identical to before this field existed.
func (d *DetailPane) View() string {
	style := lipgloss.NewStyle().Width(detailWidth)
	if d.height > 0 {
		style = style.Height(d.height)
	}

	if !d.hasSel {
		return style.Faint(true).Render("No item selected.")
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", lipgloss.NewStyle().Bold(true).Render(d.selected.Title))
	fmt.Fprintf(&b, "ID: %s   Kind: %s\n", d.selected.ID, d.selected.Kind)

	if d.selected.Description != "" {
		b.WriteString("\n")
		b.WriteString(d.selected.Description)
		b.WriteString("\n")
	}

	// A nil Blocker renders nothing here at all — the absence of a blocker
	// line IS the unblocked signal (spec.md's "an unblocked item's detail
	// pane shows no blocker line" scenario), not a "\nUnblocked.\n" line
	// occupying a wasted line on every unblocked item's detail view.
	if d.selected.Blocker != nil {
		b.WriteString("\n")
		b.WriteString(lipgloss.NewStyle().Bold(true).Render("Blocked: " + d.selected.Blocker.Type))
		b.WriteString("\n")
		b.WriteString(d.selected.Blocker.Reason)
		if d.selected.Blocker.Ref != "" {
			fmt.Fprintf(&b, " (see %s)", d.selected.Blocker.Ref)
		}
		b.WriteString("\n")
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

	return style.Render(b.String())
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
