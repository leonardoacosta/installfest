package ui

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/leonardoacosta/installfest/apps/wavetui/internal/dispatch"
	"github.com/leonardoacosta/installfest/apps/wavetui/internal/flair"
	"github.com/leonardoacosta/installfest/apps/wavetui/internal/store"
	"github.com/leonardoacosta/installfest/apps/wavetui/internal/wave"
)

// queueColumns matches tasks.md [3.2]'s required column set: item / type /
// created-at / blocker-badge / fan-out-score.
// Blocker is 24 wide, not 18 — tasks.md [3.1]'s ErrSessionStreaming refusal
// renders the literal design.md phrase "queued — session busy" (21 runes)
// into this same column (see renderBlockerCell); 18 would silently
// ellipsis-truncate that mandated text mid-word, which is worse than a
// slightly wider column for every other row's shorter Blocker/Stale badges.
var queueColumns = []table.Column{
	{Title: "Item", Width: 32},
	{Title: "Type", Width: 10},
	{Title: "Created", Width: 12},
	{Title: "Blocker", Width: 24},
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

	// --- wavetui-dispatch (tasks.md [3.1]-[3.3]) additive state ---

	// ctx/dispatcher are nil until SetDispatcher is called (cmd/wavetui/
	// main.go, task [3.4]) — a QueuePane nobody wires a Dispatcher into
	// (e.g. every pre-existing test in this package) treats "enter" as a
	// silent no-op rather than a nil-pointer panic. dispatcher is typed as
	// the Dispatcher interface (Resolver's own production shape), not a
	// concrete *dispatch.Resolver, so tests can inject a fake — see
	// queuepane_test.go.
	ctx        context.Context
	dispatcher dispatch.Dispatcher

	// dispatchBadges is a transient, per-item overlay on the Blocker/Stale
	// column (see renderBlockerCell) recording the outcome of the most
	// recent Start dispatch for that item — "queued — session busy" on an
	// ErrSessionStreaming refusal, or "failed: <err>" on any other Dispatch
	// error, per design.md § No automatic retry: the operator re-triggers
	// Start manually (another "enter" press), nothing here retries on its
	// own. A successful dispatch clears any stale badge for that item
	// rather than leaving a prior failure visible after a later success.
	// Never populated by Update/a Snapshot — only startSelected touches it.
	dispatchBadges map[string]string

	// selected is the wave-builder's multi-select accumulator (task [3.2]),
	// keyed by item ID — toggled by "space" on the currently highlighted
	// row, independent of q.table's own single-row cursor. nil/empty is the
	// default (no select-mode activity yet), rendering identically to a
	// QueuePane that never had toggleSelected called — see renderTitleCell
	// and waveStatusLines.
	selected map[string]bool

	// waveWriter persists a finalized wave (task [3.3]'s JSON writer, see
	// internal/wave/writer.go) — nil until SetWaveWriter is called
	// (cmd/wavetui/main.go, task [3.4]). Injected as a function rather than
	// a *wave.File-writing dependency directly, so tests can assert
	// finalizeWave's call/no-call behavior without touching a real
	// filesystem path.
	waveWriter func(items []store.Item) error

	// lastWaveAction is a transient one-line status from the most recent
	// finalize attempt — the same single-line transient-status precedent
	// sessionspane.go's lastAction already establishes for a one-key,
	// never-automatic action.
	lastWaveAction string
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
	q.items = items
	q.rebuildRows()

	if idx := indexOfItemID(items, prevID); idx >= 0 {
		q.table.SetCursor(idx)
	} else if len(items) > 0 && q.table.Cursor() >= len(items) {
		q.table.SetCursor(len(items) - 1)
	}

	return q
}

// rebuildRows re-derives every table row from q.items using the CURRENT
// q.highlights/q.selected/q.dispatchBadges overlay state, without waiting
// for a fresh Snapshot. Update calls this after refreshing q.items; the
// wavetui-dispatch actions that mutate q.selected/q.dispatchBadges
// (startSelected/setDispatchBadge, toggleSelected, clearSelection,
// finalizeWave) ALSO call it directly, so a dispatch badge or a select
// marker is visible the instant that key is pressed — design.md § No
// automatic retry's "renders an immediate failure badge" would otherwise be
// violated by a stale row that only catches up on the next incoming
// store.Snapshot (which may be seconds away, or may never arrive again for
// an unchanged item).
func (q *QueuePane) rebuildRows() {
	rows := make([]table.Row, 0, len(q.items))
	for _, it := range q.items {
		rows = append(rows, table.Row{
			q.renderTitleCell(it),
			string(it.Kind),
			formatCreatedAt(it.CreatedAt),
			q.renderBlockerCell(it),
			fmt.Sprintf("%d", it.FanOutScore),
		})
	}
	q.table.SetRows(rows)
}

// View implements Pane. Beyond the underlying table, it appends the
// wave-builder's transient status lines (selection summary, ConflictsFor
// warnings, last finalize outcome) when there is anything to show — see
// waveStatusLines. A QueuePane with no selection and no wave activity yet
// renders byte-for-byte identically to before this task (waveStatusLines
// returns "" in that case, and the "\n"+"" append is skipped entirely).
func (q *QueuePane) View() string {
	view := q.table.View()
	if extra := q.waveStatusLines(); extra != "" {
		view += "\n" + extra
	}
	return view
}

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

// SetDispatcher wires the Resolver (or any Dispatcher, e.g. a test fake)
// the "enter" Start action calls, plus the ctx it dispatches under —
// cmd/wavetui/main.go's task [3.4] wiring. Until this is called, "enter" is
// a documented no-op (see startSelected) rather than a nil-pointer panic —
// every pre-existing test in this package never calls this and must keep
// working unchanged.
func (q *QueuePane) SetDispatcher(ctx context.Context, d dispatch.Dispatcher) {
	q.ctx = ctx
	q.dispatcher = d
}

// SetWaveWriter wires the wave finalization writer (internal/wave/writer.go,
// task [3.3]) the "w" finalize action calls — cmd/wavetui/main.go's task
// [3.4] wiring. fn receives SelectedForWave's already-FanOutScore-ordered
// slice; it is injected as a plain function (not a *wave.File-writing
// dependency directly) so tests can assert finalizeWave's call/no-call
// behavior without touching a real filesystem path. Until this is called,
// "w" reports "no wave writer configured" via lastWaveAction rather than
// panicking.
func (q *QueuePane) SetWaveWriter(fn func(items []store.Item) error) {
	q.waveWriter = fn
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

// HandleKey handles this pane's own wavetui-dispatch actions (tasks.md
// [3.1]/[3.2]) — "enter" (Start), "space" (toggle wave-builder selection),
// "w" (finalize the wave), "esc" (clear selection) — and otherwise forwards
// the key press to the underlying bubbles table (row up/down, paging, ...)
// when this pane is focused. Deliberately outside the Pane interface:
// design.md's Pane contract is Update/View/Focusable only, and a bare
// Snapshot carries no notion of "which key was pressed" — Root type-asserts
// to *QueuePane to reach this method, see root.go's handleKey. The four
// dispatch/wave keys are intercepted BEFORE reaching q.table.Update: bubbles'
// table has no built-in binding for any of them, so this ordering changes
// nothing about existing navigation, but keeps this pane the single place
// that decides what each of its own keys means.
func (q *QueuePane) HandleKey(msg tea.KeyPressMsg) {
	switch msg.String() {
	case "enter":
		q.startSelected()
		return
	case "space":
		q.toggleSelected()
		return
	case "w":
		q.finalizeWave()
		return
	case "esc":
		q.clearSelection()
		return
	}

	var cmd tea.Cmd
	q.table, cmd = q.table.Update(msg)
	_ = cmd // the table's own Update never returns a Cmd wavetui needs to run
}

// startSelected implements tasks.md [3.1]: dispatch the highlighted item via
// the wired Dispatcher (Resolver in production) in one action, recording the
// outcome as a transient per-item badge (see setDispatchBadge) rather than
// retrying automatically — design.md § No automatic retry: the operator
// re-triggers Start (another "enter" press) once a busy session goes idle,
// or once whatever caused a failure is fixed. A nil dispatcher (SetDispatcher
// never called — every pre-existing test, and any run before task [3.4]'s
// wiring lands) or no current selection is a silent no-op, never a panic.
func (q *QueuePane) startSelected() {
	item, ok := q.SelectedItem()
	if !ok || q.dispatcher == nil {
		return
	}
	ctx := q.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	err := q.dispatcher.Dispatch(ctx, item, renderPromptText(item))
	q.setDispatchBadge(item.ID, err)
}

// renderPromptText renders the prompt text startSelected dispatches for
// item. RESOLVED (task [3.1], no prior convention existed for this — design.md
// names the Dispatcher's promptText parameter but never how QueuePane
// derives one from an Item): every item this codebase surfaces is either a
// beads ID or an openspec proposal slug (store.Item.ID, this repo's one
// canonical id-shape per dispatch.go's idShapeRe), and this fleet's own
// `/apply <bead-id-or-spec-name>` command (see rules/BEADS.md, commands/
// apply.md) already accepts either interchangeably as its one entry point —
// so the dispatched prompt is simply "/apply <item.ID>", handing the target
// session the exact command an operator would otherwise alt-tab over and
// type by hand. This is deliberately NOT item.Title (free-form, not a valid
// slash-command argument) — see dispatch.go's own validateDispatchTarget
// doc comment for why a title must never be treated as an id-shaped value.
func renderPromptText(item store.Item) string {
	return "/apply " + item.ID
}

// setDispatchBadge records the outcome of a Start dispatch as a transient,
// per-item overlay on the Blocker/Stale column (see renderBlockerCell) — a
// nil err clears any stale badge for that item (a fresh success must not
// leave a prior failure visible), ErrSessionStreaming renders as "queued —
// session busy" per design.md § Mid-turn safety ("QueuePane renders this as
// 'queued — session busy' rather than silently discarding the dispatch"),
// and every other error renders its own message so the operator sees exactly
// what failed rather than a generic "dispatch failed."
func (q *QueuePane) setDispatchBadge(id string, err error) {
	if q.dispatchBadges == nil {
		q.dispatchBadges = make(map[string]string)
	}
	switch {
	case err == nil:
		delete(q.dispatchBadges, id)
	case errors.Is(err, dispatch.ErrSessionStreaming):
		q.dispatchBadges[id] = "queued — session busy"
	default:
		q.dispatchBadges[id] = "failed: " + err.Error()
	}
	q.rebuildRows()
}

// toggleSelected implements tasks.md [3.2]'s multi-select accumulation:
// "space" adds the currently highlighted item to the wave-builder's
// selection set, or removes it if already present. A no-op when nothing is
// currently selected in the table (an empty queue).
func (q *QueuePane) toggleSelected() {
	item, ok := q.SelectedItem()
	if !ok {
		return
	}
	if q.selected == nil {
		q.selected = make(map[string]bool)
	}
	if q.selected[item.ID] {
		delete(q.selected, item.ID)
	} else {
		q.selected[item.ID] = true
	}
	q.rebuildRows()
}

// clearSelection empties the wave-builder's selection set — "esc" — without
// touching any dispatch badge or the table's own single-row cursor.
func (q *QueuePane) clearSelection() {
	q.selected = nil
	q.rebuildRows()
}

// SelectedForWave returns the wave-builder's current selection as a slice of
// full store.Item values, ordered by Item.FanOutScore DESCENDING per
// tasks.md [3.2] ("multi-select accumulation ordered by Item.FanOutScore
// descending") — ties broken by ID for a deterministic, test-stable order.
// Deliberately outside the Pane interface, same rationale as every other
// QueuePane-only accessor in this file. nil when nothing is selected.
func (q *QueuePane) SelectedForWave() []store.Item {
	if len(q.selected) == 0 {
		return nil
	}
	items := make([]store.Item, 0, len(q.selected))
	for _, it := range q.items {
		if q.selected[it.ID] {
			items = append(items, it)
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].FanOutScore != items[j].FanOutScore {
			return items[i].FanOutScore > items[j].FanOutScore
		}
		return items[i].ID < items[j].ID
	})
	return items
}

// finalizeWave implements tasks.md [3.3]'s consumer side: "w" persists the
// current wave-builder selection via the wired waveWriter (SetWaveWriter,
// task [3.4]'s wiring of internal/wave/writer.go's WriteFile+BuildFile).
// Every outcome — nothing selected, no writer wired yet, a write failure, or
// success — is recorded in lastWaveAction (see waveStatusLines) rather than
// silently no-op'ing or panicking. A successful finalize clears the
// selection, matching design.md's framing of finalization as committing the
// wave, not merely previewing it.
func (q *QueuePane) finalizeWave() {
	items := q.SelectedForWave()
	if len(items) == 0 {
		q.lastWaveAction = "finalize: no items selected"
		return
	}
	if q.waveWriter == nil {
		q.lastWaveAction = "finalize: no wave writer configured"
		return
	}
	if err := q.waveWriter(items); err != nil {
		q.lastWaveAction = fmt.Sprintf("finalize failed: %v", err)
		return
	}
	q.lastWaveAction = fmt.Sprintf("wave finalized: %d item(s)", len(items))
	q.selected = nil
	q.rebuildRows()
}

// waveStatusLines renders the wave-builder's transient status: a selection
// summary line (count + ordered IDs + the keybinding hint) whenever at least
// one item is selected, one wave.ConflictsFor warning row per overlapping
// path (tasks.md [3.2]: "naming both item IDs, never silently dropping a
// candidate"), and the last finalize outcome if any. Returns "" when there
// is nothing to show (no selection ever made, no finalize attempted yet) —
// see View()'s doc comment for why that keeps pre-existing rendering
// byte-for-byte unchanged.
func (q *QueuePane) waveStatusLines() string {
	if len(q.selected) == 0 && q.lastWaveAction == "" {
		return ""
	}

	var lines []string

	if len(q.selected) > 0 {
		items := q.SelectedForWave()
		ids := make([]string, len(items))
		for i, it := range items {
			ids[i] = it.ID
		}
		lines = append(lines, fmt.Sprintf(
			"wave: %d selected (%s) — space: toggle, w: finalize, esc: clear",
			len(items), strings.Join(ids, ", "),
		))

		conflicts := wave.ConflictsFor(items)
		if len(conflicts) > 0 {
			paths := make([]string, 0, len(conflicts))
			for p := range conflicts {
				paths = append(paths, p)
			}
			sort.Strings(paths)
			for _, p := range paths {
				lines = append(lines, conflictWarningStyle.Render(
					fmt.Sprintf("conflict: %s touched by %s", p, strings.Join(conflicts[p], ", ")),
				))
			}
		}
	}

	if q.lastWaveAction != "" {
		lines = append(lines, lipgloss.NewStyle().Faint(true).Render(q.lastWaveAction))
	}

	return strings.Join(lines, "\n")
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

// waveSelectMarker prefixes a row's Item column when that item is currently
// in the wave-builder's selection set (task [3.2]) — plain ASCII, matching
// this file's existing glyph conventions (renderTitleCell's flair glyph
// prefix) rather than introducing a non-ASCII marker.
const waveSelectMarker = "[x] "

// renderItemTitle renders the queue's Item column: prefix (the wave-select
// marker, or "" when the item isn't selected — see renderTitleCell) plus the
// plain title for a real bead/proposal, or a dimmed rendering for a
// SecondClass item (see secondClassStyle). prefix is applied BEFORE the
// SecondClass dimming so a selected SecondClass item still renders dimmed
// with its marker visible, rather than the marker escaping the dim style.
func renderItemTitle(it store.Item, prefix string) string {
	text := prefix + it.Title
	if it.SecondClass {
		return secondClassStyle.Render(text)
	}
	return text
}

// renderTitleCell renders the queue's Item column, layering any highlight
// wavetui-flair computed for this item this frame, and the wave-select
// marker (task [3.2]) when this item is in q.selected, on top of the
// pre-existing SecondClass dimming (task [3.1]). Falls straight through to
// the unchanged renderItemTitle whenever q.highlights is nil/empty or has
// no entry for it.ID AND q.selected is nil/empty or has no entry for it.ID —
// the exact "render unchanged when both maps are nil or empty" contract
// task [3.1] originally required, extended to the second additive map.
func (q *QueuePane) renderTitleCell(it store.Item) string {
	prefix := ""
	if q.selected[it.ID] {
		prefix = waveSelectMarker
	}

	hl, highlighted := q.highlights[it.ID]
	if !highlighted {
		return renderItemTitle(it, prefix)
	}

	style := lipgloss.NewStyle().Foreground(lipgloss.Color(hl.Color.Hex()))
	if it.SecondClass {
		style = style.Faint(true)
	}
	text := prefix + it.Title
	if hl.Glyph != "" {
		text = hl.Glyph + " " + text
	}
	return style.Render(text)
}

// conflictWarningStyle renders a wave.ConflictsFor warning row (task [3.2]).
// Reuses the exact color ("203") root.go's unavailableBadges and
// sessionspane.go's sessionZombieColor already use for significant/error-
// weight state, rather than introducing a new arbitrary color for a state
// of comparable severity.
var conflictWarningStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("203"))

// renderBlockerCell renders the queue's Blocker/Stale column, layering
// task [3.1]'s transient per-item dispatch-outcome badge (q.dispatchBadges)
// on top of the pre-existing blockerBadge — a fresh Start-dispatch outcome
// takes precedence over a Blocker/Stale badge for the same row, since it is
// the more current, operator-triggered signal. Falls straight through to
// the unchanged blockerBadge whenever q.dispatchBadges has no entry for
// it.ID — the same "render unchanged when the map is empty" contract
// renderTitleCell already follows for its own additive overlay maps.
func (q *QueuePane) renderBlockerCell(it store.Item) string {
	if badge, ok := q.dispatchBadges[it.ID]; ok {
		return badge
	}
	return blockerBadge(it)
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
