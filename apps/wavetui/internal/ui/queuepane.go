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
	"github.com/leonardoacosta/installfest/apps/wavetui/internal/lanes"
	"github.com/leonardoacosta/installfest/apps/wavetui/internal/sources"
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
// wavetui-decision-lanes' own badges (renderLaneBadge, e.g. "lane:decision
// (spawned)" at 23 runes, "stale — x:cleanup" at 17) were sized to fit this
// same 24-wide budget rather than widening the column further.
var queueColumns = []table.Column{
	{Title: "Item", Width: 32},
	{Title: "Type", Width: 10},
	{Title: "Created", Width: 12},
	{Title: "Blocker", Width: 24},
	{Title: "Fan-out", Width: 8},
}

// ambiguousPicker is QueuePane.ambiguousPicker's payload (if-7mq2.1): the
// item/prompt the operator originally tried to dispatch, the tied
// candidates a *dispatch.AmbiguousTargetError reported, and the picker's
// own cursor into that slice — independent of q.table's own single-row
// cursor, since the two navigate different lists. cursor's zero value (0)
// is exactly "the first tied candidate" — an arbitrary but deterministic
// starting point, since scoreCandidates (tmux.go) makes no ordering
// promise beyond "all tied."
type ambiguousPicker struct {
	item       store.Item
	promptText string
	candidates []dispatch.Candidate
	cursor     int
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

	// spriteGlyphs is wavetui-flair's optional per-item presence-sprite
	// glyph map (design.md § Presence sprites, if-z7pm/if-u7ul.1) — nil/
	// empty (the default, and the whole state whenever flair is disabled,
	// compiled out, or simply no item has a linked session right now)
	// leaves QueuePane's rendering byte-for-byte identical to how it
	// rendered before this field existed. See renderTitleCell.
	spriteGlyphs map[string]string

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

	// ambiguousPicker holds a pending target-disambiguation prompt
	// (if-7mq2.1): populated when a Start dispatch's Dispatcher call
	// returns *dispatch.AmbiguousTargetError (a same-window/same-session/
	// other scoring tie among tmux panes — design.md § Target resolution
	// point 2's own "never a silent pick" invariant), so the operator can
	// resolve the tie with an inline, arrow-key-navigable candidate list —
	// the "AskUserQuestion-shaped inline pane list" design.md's own prose
	// names — instead of stepping outside wavetui to manually disambiguate
	// (e.g. closing a duplicate tmux pane) before retrying. nil is the
	// default (no tie pending) and the whole state whenever no dispatch has
	// ever hit this error, or the operator already confirmed/cancelled a
	// prior one — every pre-existing test in this package never triggers
	// this and must keep rendering exactly as before. See
	// handleAmbiguousPickerKey, which intercepts ALL keys ahead of HandleKey's
	// normal switch whenever this is non-nil.
	ambiguousPicker *ambiguousPicker

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

	// --- wavetui-decision-lanes (tasks.md [3.1]-[3.3]) additive state ---

	// lanes mirrors wavetui-decision-lanes' per-item LaneState
	// (internal/lanes), keyed by item ID — rebuilt by rebuildLanes on every
	// Update(Snapshot) call via lanes.DetectLane, which preserves
	// PaneID/SpawnedAt/Since across snapshots for an item whose blocker
	// Type is unchanged (design.md § Lane detection). An item with no lane
	// (item.Blocker == nil, or the type just changed) has no entry here —
	// that absence IS the badge-clear signal design.md specifies; there is
	// no separate "resolved" flag anywhere. nil/empty is the default,
	// matching every pre-existing test in this package (none of which
	// populate a Blocker note, so none ever get a lane).
	lanes map[string]*lanes.LaneState

	// spawner is nil until SetSpawner is called (cmd/wavetui/main.go, task
	// [3.4]) — a QueuePane nobody wires a Spawner into (every pre-existing
	// test) treats the lane "s" key as a silent no-op rather than a
	// nil-pointer panic, the same convention SetDispatcher/dispatcher
	// already establish above for the unrelated Start action.
	spawner dispatch.Spawner

	// spawnBadges is a transient, per-item overlay recording the outcome of
	// the most recent lane-spawn action for that item — "spawn failed:
	// <err>" on a Spawn error, per design.md's explicit "no automatic
	// retry" instruction (dispatchBadges' own doc comment above documents
	// the identical precedent for the unrelated Start action): the operator
	// re-triggers "s" manually, nothing here retries on its own.
	// rebuildLanes clears an entry the moment its item's lane itself
	// disappears (blocker resolved/edited away), so a stale failure badge
	// never survives past the blocker note that caused it.
	spawnBadges map[string]string

	// ghostIDs marks which entries in q.items are "ghost rows" (if-zts4
	// fix) — an item that Diff'd as EventItemClosed (absent from the
	// Snapshot Update just applied) but was appended back into q.items by
	// withGhostRows because it still has a live entry in q.highlights,
	// giving wavetui-flair's row_flash at least one real row to paint
	// before the item disappears for good. Recomputed from scratch on
	// every Update call — never mutated by any other method — and
	// consulted only by rebuildLanes, so a ghost's stale-by-definition
	// Blocker note can't spuriously create or resurrect a decision-lane
	// entry for an item that no longer exists in the actual data source.
	// nil/empty (the default, and the whole state whenever nothing was
	// ever ghosted) behaves identically to every pre-existing QueuePane.
	ghostIDs map[string]bool
}

// laneIdleWindow is the staleness threshold LaneState.IsStale checks a
// spawned-but-no-longer-live lane against — design.md § Lane liveness:
// "default matches wavetui-sessions' 15min zombie default." Reuses
// sources.DefaultZombieInactivity verbatim rather than declaring a second
// 15-minute constant, since it is the exact same threshold design.md cites.
var laneIdleWindow = sources.DefaultZombieInactivity

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
//
// Before overwriting q.items, withGhostRows compares the OUTGOING q.items
// (still carrying whatever just closed) against the incoming snap.Items and
// against q.highlights, appending back any item that closed this transition
// but still has a live wavetui-flair highlight (if-zts4) — see that
// function's doc comment for the full rationale and its dependency on
// q.highlights already reflecting this transition by the time Update runs.
func (q *QueuePane) Update(snap store.Snapshot) Pane {
	prevID := q.SelectedID()
	prevItems := q.items

	items := make([]store.Item, len(snap.Items))
	copy(items, snap.Items)
	items, ghosts := withGhostRows(prevItems, items, q.highlights)
	q.items = items
	q.ghostIDs = ghosts
	q.rebuildLanes()
	q.rebuildRows()

	if idx := indexOfItemID(items, prevID); idx >= 0 {
		q.table.SetCursor(idx)
	} else if len(items) > 0 && q.table.Cursor() >= len(items) {
		q.table.SetCursor(len(items) - 1)
	}

	return q
}

// withGhostRows appends any item present in prevItems but missing from
// items (a just-closed item — FlairManager's EventItemClosed) that STILL has
// an active row-anchored highlight in highlights, so a closed bead's
// row_flash/static_row_mark gets at least one real row to paint before the
// item disappears from the table for good (if-zts4: the highlight was
// starting correctly, but the row it was meant to paint had already been
// dropped from the table by the time it was ever consulted).
//
// This depends on highlights already reflecting THIS transition's
// freshly-started animation by the time Update calls this — see
// cmd/wavetui's rootWithFlair.Update, which computes flair's diff/highlight
// step before forwarding the triggering SnapshotMsg to root.Update (the
// call that reaches here). A highlight map still keyed to the PRIOR
// transition would never carry the newly-closed item's ID, and this
// function would correctly do nothing for it.
//
// EventItemClosed+KindProposal (an overlay toast, not a row highlight) never
// populates highlights for the closing item, so this never manufactures a
// ghost row for a closed proposal — only for a closed bead, matching
// row_flash/static_row_mark's row-anchored effects. A ghosted item is
// appended at the end of items (not reinserted at its prior index) — simpler
// than re-deriving its original position, and immaterial given the row is
// gone again the moment the highlight itself settles and a further Snapshot
// arrives. Returns items unchanged (and a nil ghosts set) whenever there is
// nothing highlighted or nothing to compare against, so the common case (no
// flair, or nothing closed this transition) allocates nothing.
func withGhostRows(prevItems, items []store.Item, highlights map[string]flair.HighlightState) ([]store.Item, map[string]bool) {
	if len(highlights) == 0 || len(prevItems) == 0 {
		return items, nil
	}
	present := make(map[string]bool, len(items))
	for _, it := range items {
		present[it.ID] = true
	}
	var ghosts map[string]bool
	for _, it := range prevItems {
		if present[it.ID] {
			continue
		}
		if _, highlighted := highlights[it.ID]; !highlighted {
			continue
		}
		items = append(items, it)
		if ghosts == nil {
			ghosts = make(map[string]bool, 1)
		}
		ghosts[it.ID] = true
	}
	return items, ghosts
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

// rebuildLanes re-derives q.lanes from q.items' current blocker notes via
// lanes.DetectLane, preserving each item's own PaneID/SpawnedAt/Since
// (design.md § Lane detection's prior-state-carry-forward contract)
// whenever its blocker Type is unchanged from the previous call. Called by
// Update immediately after q.items is refreshed, and ONLY there — unlike
// rebuildRows (also called after every dispatch/wave/lane action so a
// transient badge shows immediately), lane identity must only ever change
// in response to a genuine incoming Snapshot, never as a side effect of a
// spawn/cleanup keypress rebuilding rows.
//
// An item whose Blocker becomes nil, or whose Type changes, gets no entry
// here — dropping the old lane IS the badge-clear signal design.md
// specifies ("The moment item.Blocker becomes nil or its Type changes, the
// lane entry is dropped from the map"); there is no separate "resolved"
// flag anywhere in this codebase. Any q.spawnBadges entry for an item that
// no longer has a lane is dropped in the same pass, so a stale
// spawn-failure badge never survives past the blocker note that caused it.
//
// A ghost row (q.ghostIDs, if-zts4) is skipped entirely here: it is a
// last-known-good snapshot of an item the actual data source has already
// dropped, kept alive only long enough to render its flash highlight — its
// stale Blocker note must not spawn or resurrect a decision-lane entry for
// an item that no longer exists.
func (q *QueuePane) rebuildLanes() {
	next := make(map[string]*lanes.LaneState, len(q.items))
	for _, it := range q.items {
		if q.ghostIDs[it.ID] {
			continue
		}
		var blockerType string
		if it.Blocker != nil {
			blockerType = it.Blocker.Type
		}
		if ls := lanes.DetectLane(blockerType, q.lanes[it.ID]); ls != nil {
			next[it.ID] = ls
		}
	}
	q.lanes = next

	for id := range q.spawnBadges {
		if _, ok := q.lanes[id]; !ok {
			delete(q.spawnBadges, id)
		}
	}
}

// View implements Pane. Beyond the underlying table, it appends the
// ambiguous-target picker (if-7mq2.1, see renderAmbiguousPicker) when a Start
// dispatch is currently tied on a candidate pane, then the wave-builder's
// transient status lines (selection summary, ConflictsFor warnings, last
// finalize outcome) when there is anything to show — see waveStatusLines. A
// QueuePane with no picker open, no selection, and no wave activity yet
// renders byte-for-byte identically to before either feature existed
// (renderAmbiguousPicker/waveStatusLines both return "" in that case, and
// each "\n"+"" append is skipped entirely).
func (q *QueuePane) View() string {
	view := q.table.View()
	if picker := q.renderAmbiguousPicker(); picker != "" {
		view += "\n" + picker
	}
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

// SetSpriteGlyphs installs the current per-item presence-sprite glyph map
// wavetui-flair computed for this frame (design.md § Presence sprites). A
// nil or empty map — flair disabled, compiled out, calm mode with no live
// session, or simply no item with a linked Claude Code session right now —
// leaves QueuePane's rendering byte-for-byte identical to how it rendered
// before this method existed, the exact same convention SetHighlights
// already establishes above.
func (q *QueuePane) SetSpriteGlyphs(glyphs map[string]string) {
	q.spriteGlyphs = glyphs
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

// SetSpawner wires the Spawner (TmuxSpawner in production) the lane "s"
// action calls — cmd/wavetui/main.go's task [3.4] wiring. Until this is
// called, "s" is a silent no-op (see spawnSelectedLane) rather than a
// nil-pointer panic — every pre-existing test in this package never calls
// this and must keep working unchanged, the same convention SetDispatcher
// already establishes above for the unrelated Start action.
func (q *QueuePane) SetSpawner(spawner dispatch.Spawner) {
	q.spawner = spawner
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

// HandleKey handles this pane's own wavetui-dispatch AND wavetui-
// decision-lanes actions (tasks.md [3.1]/[3.2]) — "enter" (Start), "space"
// (toggle wave-builder selection), "w" (finalize the wave), "esc" (clear
// selection), "s" (spawn a decision-lane session for the highlighted row's
// blocker, tasks.md [3.2]), "x" (manual stale-lane cleanup, tasks.md [3.3])
// — and otherwise forwards the key press to the underlying bubbles table
// (row up/down, paging, ...) when this pane is focused. Deliberately
// outside the Pane interface: design.md's Pane contract is
// Update/View/Focusable only, and a bare Snapshot carries no notion of
// "which key was pressed" — Root type-asserts
// to *QueuePane to reach this method, see root.go's handleKey. All six
// dispatch/wave/lane keys are intercepted BEFORE reaching q.table.Update:
// bubbles' table has no built-in binding for any of them, so this ordering
// changes nothing about existing navigation, but keeps this pane the single
// place that decides what each of its own keys means.
//
// When q.ambiguousPicker is non-nil (if-7mq2.1: a Start dispatch is
// currently tied on a candidate pane), EVERY key is instead routed to
// handleAmbiguousPickerKey, ahead of this switch and ahead of q.table.Update
// — none of the six actions above, and none of the table's own row
// navigation, may fire while a tie is still unresolved. This mirrors the
// "intercepted before reaching the table" ordering the six keys above
// already establish, just at the whole-method level rather than per-key.
func (q *QueuePane) HandleKey(msg tea.KeyPressMsg) {
	if q.ambiguousPicker != nil {
		q.handleAmbiguousPickerKey(msg)
		return
	}

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
	case "s":
		q.spawnSelectedLane()
		return
	case "x":
		q.cleanupSelectedLane()
		return
	}

	var cmd tea.Cmd
	q.table, cmd = q.table.Update(msg)
	_ = cmd // the table's own Update never returns a Cmd wavetui needs to run
}

// handleAmbiguousPickerKey handles every key while q.ambiguousPicker is
// active (if-7mq2.1): "up"/"k" and "down"/"j" move the candidate cursor —
// the same up/down+k/j convention sessionspane.go's own HandleKey already
// establishes for a different pane's row cursor — clamped to the candidate
// slice's bounds (never wrapping); "enter" confirms the highlighted
// candidate (confirmAmbiguousPicker); "esc" cancels back to a plain failure
// badge (cancelAmbiguousPicker). Any other key is a silent no-op: the
// picker owns the keyboard entirely until the operator confirms or cancels,
// per HandleKey's own doc comment above.
func (q *QueuePane) handleAmbiguousPickerKey(msg tea.KeyPressMsg) {
	switch msg.String() {
	case "up", "k":
		if q.ambiguousPicker.cursor > 0 {
			q.ambiguousPicker.cursor--
		}
	case "down", "j":
		if q.ambiguousPicker.cursor < len(q.ambiguousPicker.candidates)-1 {
			q.ambiguousPicker.cursor++
		}
	case "enter":
		q.confirmAmbiguousPicker()
	case "esc":
		q.cancelAmbiguousPicker()
	}
}

// startSelected implements tasks.md [3.1]: dispatch the highlighted item via
// the wired Dispatcher (Resolver in production) in one action, recording the
// outcome as a transient per-item badge (see setDispatchBadge) rather than
// retrying automatically — design.md § No automatic retry: the operator
// re-triggers Start (another "enter" press) once a busy session goes idle,
// or once whatever caused a failure is fixed. A nil dispatcher (SetDispatcher
// never called — every pre-existing test, and any run before task [3.4]'s
// wiring lands) or no current selection is a silent no-op, never a panic.
//
// A *dispatch.AmbiguousTargetError (if-7mq2.1) is the one Dispatch outcome
// that does NOT fall through to the plain failure badge: openAmbiguousPicker
// installs the inline candidate-disambiguation prompt instead, so the
// operator can resolve the tie in-app rather than stepping outside wavetui.
func (q *QueuePane) startSelected() {
	item, ok := q.SelectedItem()
	if !ok || q.dispatcher == nil {
		return
	}
	ctx := q.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	prompt := renderPromptText(item)
	err := q.dispatcher.Dispatch(ctx, item, prompt)

	var ambigErr *dispatch.AmbiguousTargetError
	if errors.As(err, &ambigErr) {
		q.openAmbiguousPicker(item, prompt, ambigErr.Candidates)
		return
	}
	q.setDispatchBadge(item.ID, err)
}

// openAmbiguousPicker installs the inline candidate-disambiguation prompt
// (if-7mq2.1) startSelected opens on a *dispatch.AmbiguousTargetError,
// replacing the plain "failed: ..." badge that error would otherwise render
// via setDispatchBadge's default branch with a compact "ambiguous — pick
// below" row badge (renderBlockerCell's existing dispatchBadges precedence
// already applies unchanged) plus the full candidate list rendered by
// renderAmbiguousPicker in View(). cursor starts at 0 (the first tied
// candidate) — see ambiguousPicker's own doc comment for why any starting
// index is equally arbitrary.
func (q *QueuePane) openAmbiguousPicker(item store.Item, promptText string, candidates []dispatch.Candidate) {
	q.ambiguousPicker = &ambiguousPicker{
		item:       item,
		promptText: promptText,
		candidates: candidates,
	}
	if q.dispatchBadges == nil {
		q.dispatchBadges = make(map[string]string)
	}
	q.dispatchBadges[item.ID] = "ambiguous — pick below"
	q.rebuildRows()
}

// confirmAmbiguousPicker implements the picker's "enter" action
// (if-7mq2.1): re-dispatch the item to the candidate currently highlighted
// by q.ambiguousPicker.cursor. Requires q.dispatcher to implement
// dispatch.TargetOverrideDispatcher (Resolver does, in production) — a
// dispatcher that doesn't (e.g. a bare test fake wired directly) renders a
// "no override support" failure badge instead of silently no-op'ing or
// panicking, the same "never a silent pick" invariant this whole feature
// exists to uphold, now applied to a caller that can't act on the
// operator's actual choice either. Always dismisses the picker first
// (single-shot: confirming re-dispatches exactly once, never loops), then
// records the override dispatch's own outcome via the existing
// setDispatchBadge, so a failed override still renders its own error text
// rather than silently leaving the stale "ambiguous — pick below" badge.
func (q *QueuePane) confirmAmbiguousPicker() {
	p := q.ambiguousPicker
	q.ambiguousPicker = nil
	if p == nil || len(p.candidates) == 0 {
		q.rebuildRows()
		return
	}
	chosen := p.candidates[p.cursor]

	overrider, ok := q.dispatcher.(dispatch.TargetOverrideDispatcher)
	if !ok {
		q.setDispatchBadge(p.item.ID, errors.New(
			"dispatch target picked, but the wired dispatcher does not support an explicit target override",
		))
		return
	}

	ctx := q.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	err := overrider.DispatchToPane(ctx, p.item, p.promptText, chosen.PaneID)
	q.setDispatchBadge(p.item.ID, err)
}

// cancelAmbiguousPicker implements the picker's "esc" action (if-7mq2.1):
// dismiss the inline candidate list and fall back to the plain generic
// failure badge the original *dispatch.AmbiguousTargetError's own Error()
// text would have rendered had this picker never existed (setDispatchBadge's
// default branch) — "cancel back to the failure-badge state," not a silent
// dismiss that leaves the row looking unblocked.
func (q *QueuePane) cancelAmbiguousPicker() {
	p := q.ambiguousPicker
	q.ambiguousPicker = nil
	if p == nil {
		return
	}
	q.setDispatchBadge(p.item.ID, &dispatch.AmbiguousTargetError{Candidates: p.candidates})
}

// renderAmbiguousPicker renders q.ambiguousPicker's inline candidate list
// (if-7mq2.1) for View() — a header line naming the tied item and the
// keybinding hints, then one line per candidate (renderAmbiguousCandidate)
// with a "> " cursor marker on the currently highlighted one, the same
// marker convention sessionspane.go's own renderRow already establishes for
// a different pane's row cursor. Returns "" when no picker is open, so
// View()'s "\n"+"" append is skipped entirely — the same "render nothing
// extra when there's nothing to show" contract waveStatusLines already
// follows.
func (q *QueuePane) renderAmbiguousPicker() string {
	p := q.ambiguousPicker
	if p == nil {
		return ""
	}

	lines := make([]string, 0, len(p.candidates)+1)
	lines = append(lines, ambiguousPickerHeaderStyle.Render(fmt.Sprintf(
		"dispatch target ambiguous for %s — %d candidates tied (up/down: select, enter: confirm, esc: cancel)",
		p.item.ID, len(p.candidates),
	)))
	for i, c := range p.candidates {
		lines = append(lines, renderAmbiguousCandidate(i == p.cursor, c))
	}
	return strings.Join(lines, "\n")
}

// ambiguousPickerHeaderStyle bolds the picker's header line (if-7mq2.1) —
// plain Bold(true), matching this file's existing convention of reserving
// color for error/significant-weight state (conflictWarningStyle) and
// keeping merely-informational emphasis to weight alone.
var ambiguousPickerHeaderStyle = lipgloss.NewStyle().Bold(true)

// ambiguousCandidateCursorStyle highlights the picker's currently selected
// candidate line (if-7mq2.1). Reuses color "212" — root.go's paneStyle uses
// the exact same color for a focused pane's border — rather than
// introducing a new arbitrary color for the same "this is the current
// focus" semantic.
var ambiguousCandidateCursorStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))

// renderAmbiguousCandidate renders one candidate line of the picker
// (if-7mq2.1): a "> "/"  " cursor marker, the candidate's pane ID, and its
// session/window locality plus project/branch affinity (candidateScore's
// own ranking inputs, tmux.go) so the operator can see WHY each candidate
// tied, not just that it did.
func renderAmbiguousCandidate(selected bool, c dispatch.Candidate) string {
	marker := "  "
	if selected {
		marker = "> "
	}
	line := fmt.Sprintf("%s%s  session=%s window=%s", marker, c.PaneID, c.Session, c.Window)
	if c.Project != "" {
		line += "  project=" + c.Project
	}
	if c.Branch != "" {
		line += " branch=" + c.Branch
	}
	if selected {
		return ambiguousCandidateCursorStyle.Render(line)
	}
	return line
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

// spawnSelectedLane implements tasks.md [3.2]: pressing "s" on the currently
// highlighted row calls the wired Spawner (TmuxSpawner in production) with
// the rendered spawn-prompt template (dispatch.RenderSpawnPrompt), storing
// the returned paneID/SpawnedAt onto that item's own LaneState in q.lanes so
// subsequent renders/staleness checks see it immediately — never waiting for
// a fresh Snapshot, the same "renders an immediate ... badge" requirement
// startSelected's own doc comment already establishes for the unrelated
// Start action. A Spawn error renders as an immediate failure badge (see
// renderLaneBadge) with NO automatic retry — design.md's explicit
// instruction, mirroring startSelected/setDispatchBadge's own no-retry
// precedent for the unrelated wavetui-dispatch Start action: the operator
// re-triggers "s" manually. A nil spawner (SetSpawner never called), no
// current selection, or a selection with no active lane is a silent no-op,
// never a panic — the same convention startSelected already establishes for
// a nil dispatcher.
func (q *QueuePane) spawnSelectedLane() {
	item, ok := q.SelectedItem()
	if !ok || q.spawner == nil {
		return
	}
	ls, ok := q.lanes[item.ID]
	if !ok {
		return
	}

	ctx := q.ctx
	if ctx == nil {
		ctx = context.Background()
	}

	paneID, err := q.spawner.Spawn(ctx, dispatch.RenderSpawnPrompt(item))
	if q.spawnBadges == nil {
		q.spawnBadges = make(map[string]string)
	}
	if err != nil {
		q.spawnBadges[item.ID] = "spawn failed: " + err.Error()
		q.rebuildRows()
		return
	}
	delete(q.spawnBadges, item.ID)
	ls.PaneID = paneID
	ls.SpawnedAt = time.Now()
	q.rebuildRows()
}

// cleanupSelectedLane implements tasks.md [3.3]'s manual-cleanup action:
// pressing "x" on a currently stale-badged row drops ONLY that item's
// q.lanes/q.spawnBadges entries — per design.md § Manual-cleanup prompt,
// this NEVER touches the underlying bead note, openspec delta, or bd claim;
// if the operator wants the blocker itself resolved, they edit the note
// directly. A no-op when nothing is selected, the selection has no lane, or
// the lane is not currently stale — the same "meaningless outside its one
// triggering condition" convention releaseSelected (sessionspane.go)
// already establishes for a non-zombie session.
func (q *QueuePane) cleanupSelectedLane() {
	item, ok := q.SelectedItem()
	if !ok {
		return
	}
	ls, ok := q.lanes[item.ID]
	if !ok {
		return
	}
	hasLiveSession := item.Session != nil && !item.Session.Zombie
	if !ls.IsStale(hasLiveSession, laneIdleWindow) {
		return
	}
	delete(q.lanes, item.ID)
	delete(q.spawnBadges, item.ID)
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
	// Presence-sprite glyph (design.md § Presence sprites) composes ahead
	// of the title, after the wave-select marker, regardless of whether
	// this row also has a highlight in flight this frame — additive, same
	// as SetHighlights: q.spriteGlyphs nil/empty (the default) leaves
	// prefix exactly as it was before this field existed.
	if sprite := q.spriteGlyphs[it.ID]; sprite != "" {
		prefix += sprite + " "
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
// and (wavetui-decision-lanes, tasks.md [3.1]/[3.2]/[3.3]) the current lane
// badge on top of the pre-existing blockerBadge. Precedence: a fresh
// Start-dispatch outcome (q.dispatchBadges) wins over everything else, since
// it is the more current, operator-triggered signal for the SAME column
// (the pre-existing rationale this doc comment already documented); next,
// an active lane (q.lanes) renders its own badge instead of the plain
// blockerBadge, since a lane is strictly more informative than the raw
// "blocked:<type>" text once decision-lanes is wired in. Falls straight
// through to the unchanged blockerBadge whenever neither map has an entry
// for it.ID — the same "render unchanged when the map is empty" contract
// renderTitleCell already follows for its own additive overlay maps.
func (q *QueuePane) renderBlockerCell(it store.Item) string {
	if badge, ok := q.dispatchBadges[it.ID]; ok {
		return badge
	}
	if ls, ok := q.lanes[it.ID]; ok {
		return q.renderLaneBadge(it, ls)
	}
	return blockerBadge(it)
}

// renderLaneBadge renders the Blocker/Stale column's lane-specific state for
// an item with a live entry in q.lanes (tasks.md [3.1]/[3.2]/[3.3]): a stale
// badge — IsStale is computed fresh on every render, never cached, so it
// reflects "now" rather than the moment of the last Snapshot — takes
// precedence over a transient spawn-failure badge, which takes precedence
// over the base "spawned"/"not yet spawned" state naming the blocker type
// (design.md § Lane detection: "naming the blocker type"). Kept as plain,
// unstyled text (no lipgloss color/bold), matching this file's existing
// dispatchBadges convention (setDispatchBadge's own "failed: <err>" text is
// likewise unstyled) rather than introducing a new visual-weight decision
// design.md does not specify. Text is kept compact to fit the 24-wide
// Blocker column (see queueColumns' doc comment) without truncating.
func (q *QueuePane) renderLaneBadge(it store.Item, ls *lanes.LaneState) string {
	hasLiveSession := it.Session != nil && !it.Session.Zombie
	if ls.IsStale(hasLiveSession, laneIdleWindow) {
		return "stale — x:cleanup"
	}
	if msg, ok := q.spawnBadges[it.ID]; ok {
		return msg
	}
	if ls.PaneID != "" {
		return fmt.Sprintf("lane:%s (spawned)", ls.Type)
	}
	return fmt.Sprintf("lane:%s (s)", ls.Type)
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
