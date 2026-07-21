package ui

import (
	"fmt"
	"time"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/leonardoacosta/installfest/apps/wavetui/internal/flair"
	"github.com/leonardoacosta/installfest/apps/wavetui/internal/store"
)

// queueColumns matches tasks.md [3.2]'s required column set: item / type /
// created-at / blocker-badge / fan-out-score.
var queueColumns = []table.Column{
	{Title: "Item", Width: 32},
	{Title: "Type", Width: 10},
	{Title: "Created", Width: 12},
	{Title: "Blocker", Width: 18},
	{Title: "Fan-out", Width: 8},
}

// QueuePane is a bubbles table implementing the Pane interface — see
// design.md § Architecture and tasks.md [3.2].
type QueuePane struct {
	table table.Model
	// items mirrors the table's rows in the same order, so SelectedItem can
	// map the table's cursor index back to the full store.Item (blocker
	// detail, task progress, ...) that DetailPane needs but that doesn't fit
	// in a single table cell.
	items []store.Item
	// highlights is wavetui-flair's optional per-item row highlight map
	// (task [3.1], design.md § Architecture's "row-scoped highlight map"
	// box) — nil/empty (the default, and the whole state whenever flair is
	// disabled or compiled out) means every row renders exactly as it did
	// before this field existed. See renderTitleCell.
	highlights map[string]flair.HighlightState
}

// defaultQueueWidth/Height give the table a usable size before the first
// real tea.WindowSizeMsg arrives (Root.layout calls SetSize once it does —
// see root.go). bubbles/v2's viewport.View() returns "" whenever its Width()
// is 0 (the table's zero-value default), so without an initial width the
// queue would render its header with zero body rows even once real data
// exists — a real bug this default and SetSize both guard against, not
// cosmetic tuning.
const (
	defaultQueueWidth  = 90
	defaultQueueHeight = 15
)

// NewQueuePane constructs an empty, focused QueuePane.
func NewQueuePane() *QueuePane {
	t := table.New(
		table.WithColumns(queueColumns),
		table.WithFocused(true),
		table.WithWidth(defaultQueueWidth),
		table.WithHeight(defaultQueueHeight),
	)
	return &QueuePane{table: t}
}

// Update implements Pane. It rebuilds the table's rows from the snapshot's
// items and, where possible, preserves the operator's current row selection
// across the rebuild by re-locating the previously-selected item's ID in the
// new row set (a plain SetRows call would otherwise always reset the cursor
// to 0, disorienting anyone mid-navigation when an unrelated item's fields
// merely refresh).
func (q *QueuePane) Update(snap store.Snapshot) Pane {
	prevID := q.SelectedID()

	items := make([]store.Item, len(snap.Items))
	copy(items, snap.Items)

	rows := make([]table.Row, 0, len(items))
	for _, it := range items {
		rows = append(rows, table.Row{
			q.renderTitleCell(it),
			string(it.Kind),
			formatCreatedAt(it.CreatedAt),
			blockerBadge(it),
			fmt.Sprintf("%d", it.FanOutScore),
		})
	}

	q.items = items
	q.table.SetRows(rows)

	if idx := indexOfItemID(items, prevID); idx >= 0 {
		q.table.SetCursor(idx)
	} else if len(items) > 0 && q.table.Cursor() >= len(items) {
		q.table.SetCursor(len(items) - 1)
	}

	return q
}

// View implements Pane.
func (q *QueuePane) View() string { return q.table.View() }

// SetHighlights installs the current per-item highlight map wavetui-flair
// computed for this frame (task [3.1]). A nil or empty map — flair
// disabled, compiled out, or simply no event mid-flight for any visible row
// right now — leaves QueuePane's rendering byte-for-byte identical to how it
// rendered before this method existed: Update's row-building loop only
// consults q.highlights through renderTitleCell below, never inline in the
// pre-existing renderItemTitle/blockerBadge/formatCreatedAt path, which this
// task does not touch.
func (q *QueuePane) SetHighlights(highlights map[string]flair.HighlightState) {
	q.highlights = highlights
}

// Focusable implements Pane — the queue is always a candidate for focus.
func (q *QueuePane) Focusable() bool { return true }

// SetSize resizes the underlying table. Deliberately outside the Pane
// interface (same rationale as HandleKey) — Root calls this from its own
// tea.WindowSizeMsg handling (root.go's layout), since a Snapshot carries no
// terminal-size information either.
func (q *QueuePane) SetSize(width, height int) {
	q.table.SetWidth(width)
	q.table.SetHeight(height)
}

// HandleKey forwards a key press to the underlying bubbles table (row
// up/down, paging, ...) when this pane is focused. Deliberately outside the
// Pane interface: design.md's Pane contract is Update/View/Focusable only,
// and a bare Snapshot carries no notion of "which key was pressed" — Root
// type-asserts to *QueuePane to reach this method, see root.go's handleKey.
func (q *QueuePane) HandleKey(msg tea.KeyPressMsg) {
	var cmd tea.Cmd
	q.table, cmd = q.table.Update(msg)
	_ = cmd // the table's own Update never returns a Cmd wavetui needs to run
}

// SelectedItem returns the store.Item under the table's current cursor.
// Deliberately outside the Pane interface, same rationale as HandleKey —
// Root and DetailPane need the full Item (blocker/task-progress), not just
// the row strings the table itself holds.
func (q *QueuePane) SelectedItem() (store.Item, bool) {
	idx := q.table.Cursor()
	if idx < 0 || idx >= len(q.items) {
		return store.Item{}, false
	}
	return q.items[idx], true
}

// SelectedID is a convenience wrapper over SelectedItem used to re-locate
// the previously-selected row after a rebuild (see Update above).
func (q *QueuePane) SelectedID() string {
	if it, ok := q.SelectedItem(); ok {
		return it.ID
	}
	return ""
}

func indexOfItemID(items []store.Item, id string) int {
	if id == "" {
		return -1
	}
	for i, it := range items {
		if it.ID == id {
			return i
		}
	}
	return -1
}

// formatCreatedAt renders Item.CreatedAt for the table's Created column. A
// zero time.Time (source didn't have/couldn't parse a created-at value — see
// sources/beads.go's toItem and sources/openspec.go's parseOneProposal) is
// tolerated per wavetui's decode-everywhere convention: it renders as "-"
// rather than the zero-value's misleading "0001-01-01".
func formatCreatedAt(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Format("2006-01-02")
}

// secondClassStyle dims an Item.SecondClass row's title — spec.md's
// visibility-gate Requirement: a plans/advisor-plans-sourced item "SHALL...
// be rendered as visually second-class when enabled", never identically to
// a real openspec/changes/ proposal. Faint(true) mirrors the muted-text
// convention this package already uses for de-emphasized content (root.go's
// help line, detailpane.go's stale caveat) — there is no pre-existing
// "dim row" style in this file to reuse, since QueuePane's Stale handling
// today is text-only (blockerBadge's "stale" cell), not a row-level style.
var secondClassStyle = lipgloss.NewStyle().Faint(true)

// renderItemTitle renders the queue's Item column: the plain title for a
// real bead/proposal, or a dimmed rendering for a SecondClass item (see
// secondClassStyle).
func renderItemTitle(it store.Item) string {
	if it.SecondClass {
		return secondClassStyle.Render(it.Title)
	}
	return it.Title
}

// renderTitleCell renders the queue's Item column, layering any highlight
// wavetui-flair computed for this item this frame on top of the
// pre-existing SecondClass dimming (task [3.1]). Falls straight through to
// the unchanged renderItemTitle whenever q.highlights is nil/empty or has
// no entry for it.ID — the exact "render unchanged when the map is nil or
// empty" contract this task requires.
func (q *QueuePane) renderTitleCell(it store.Item) string {
	hl, highlighted := q.highlights[it.ID]
	if !highlighted {
		return renderItemTitle(it)
	}

	style := lipgloss.NewStyle().Foreground(lipgloss.Color(hl.Color.Hex()))
	if it.SecondClass {
		style = style.Faint(true)
	}
	text := it.Title
	if hl.Glyph != "" {
		text = hl.Glyph + " " + text
	}
	return style.Render(text)
}

// blockerBadge renders the queue's blocker-badge column: a short type tag
// when a BlockerNote is present (design.md's blocker-note grammar), "stale"
// when the item's backing source failed and this is last-good data, or a
// blank cell when the item is unblocked and fresh.
func blockerBadge(it store.Item) string {
	switch {
	case it.Blocker != nil:
		return "blocked:" + it.Blocker.Type
	case it.Stale:
		return "stale"
	default:
		return ""
	}
}
