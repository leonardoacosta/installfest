// Package ui implements the bubbletea root model and Pane implementations
// (QueuePane, DetailPane) described in
// openspec/changes/wavetui-core/design.md § Architecture and
// § Pane extensibility.
package ui

import (
	"context"
	"math"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/leonardoacosta/installfest/apps/wavetui/internal/store"
)

// renderInterval caps coalesced pane updates to ~10fps — design.md §
// Architecture: "coalesced to ~10fps so an fs-event burst does not spam
// renders."
const renderInterval = 100 * time.Millisecond

// SnapshotMsg is the ONLY message Root's Update reacts to for state changes.
// design.md's invariant is that the root model's Update contains NO
// watcher/polling logic — something outside this package (main.go's bus
// subscriber, wired in the UI batch's task 3.4) turns Store mutations into
// Program.Send(SnapshotMsg{...}) calls. Update itself never watches or polls
// anything; it only ever reacts to this message.
type SnapshotMsg struct {
	Snapshot store.Snapshot
}

// flushMsg is Root's own internal tick used only to drain a coalesced
// pending snapshot once renderInterval has elapsed since the last applied
// one. Nothing outside this package ever sends it.
type flushMsg struct{}

// Pane is implemented by every panel Root hosts (QueuePane, DetailPane, and
// any sibling-proposal pane appended later — see design.md § Pane
// extensibility). Update returns a (possibly new) Pane rather than requiring
// in-place mutation, so a future Pane implementation can favor an immutable
// update style without an interface change; QueuePane/DetailPane themselves
// mutate in place and return their own pointer, since introducing real
// immutability for two thin wrapper structs would be complexity with no
// payback here.
type Pane interface {
	// Update applies the latest Snapshot and returns the (possibly new)
	// resulting Pane.
	Update(store.Snapshot) Pane
	// View renders the pane's current content.
	View() string
	// Focusable reports whether this pane ever participates in the focus
	// ring (Tab/Shift+Tab cycling).
	Focusable() bool
}

// SelectionAware is an optional interface an appended Pane may implement to
// react to QueuePane's cursor changing — the exact selection-threading
// mechanism DetailPane.SetSelected already relies on. Deliberately a plain
// type assertion (see notifySelection below), not part of the Pane
// interface itself, so appending a SelectionAware pane (e.g.
// wavetui-memory-timeline's MemoryTimelinePane) never requires editing
// Root's own applySnapshot/handleKey call sites — design.md's own directive
// to reuse this mechanism "rather than inventing a second selection-tracking
// path."
type SelectionAware interface {
	SetSelected(item store.Item, ok bool)
}

// TimelineAware is an optional interface an appended Pane may implement to
// receive a completed TimelineMsg (see timeline_dispatch.go) — kept separate
// from SelectionAware and from the Pane interface's own Update(Snapshot),
// since a TimelineMsg is not a Snapshot and most panes have no use for it.
type TimelineAware interface {
	SetTimeline(TimelineMsg)
}

// Sizeable is an optional interface an appended Pane may implement to
// receive Root's own tea.WindowSizeMsg-driven layout pass (see layout()
// below) — mirrors QueuePane.SetSize's existing precedent, generalized so a
// future appended pane doesn't need a typed Root field to be sized.
type Sizeable interface {
	SetSize(width, height int)
}

// Root is the bubbletea root model. It holds an ordered slice of Pane plus a
// focus index (the "focus ring" from design.md § Pane extensibility) for
// generic View composition and Tab-cycling, and separately keeps typed
// references to the two concrete panes it wires together directly: the Pane
// interface deliberately carries only Update/View/Focusable (no key-event
// plumbing, no cross-pane selection hook), so wiring QueuePane's row
// selection into DetailPane, and forwarding navigation keys to whichever
// pane is focused, happens via type assertions against the concrete types
// Root itself constructed — see queuepane.go's HandleKey/SelectedItem and
// detailpane.go's SetSelected for the extra, Pane-interface-external methods
// this wiring calls.
type Root struct {
	panes []Pane
	focus int // index into panes of the currently focused pane; -1 if none focusable

	queue  *QueuePane
	detail *DetailPane

	pending    *store.Snapshot
	lastApply  time.Time
	flushTimer bool // true while a flush tick is already scheduled

	// errors mirrors the latest applied Snapshot's Errors field — spec.md's
	// "A missing .beads/ or openspec/ directory degrades to an unavailable
	// badge, never a crash" Requirement. Store/BeadsSource/OpenSpecSource
	// already produce this data correctly (see internal/store, internal/
	// sources); this field plus its rendering in View() is what makes it
	// visible instead of silently tracked-but-unrendered.
	errors []store.SourceError

	// now is injected so the coalescing window can be driven deterministically
	// in tests, without depending on real timers or wall-clock sleeps.
	now func() time.Time

	quitting      bool
	width, height int

	// activeTab selects which of the two mutually-exclusive "main content"
	// panes renders: tabItems (queue+detail, the historical always-on
	// layout) or tabMemories (the memory timeline, full width/height
	// instead of a cramped stacked row below the queue). Sessions/KPIBar/
	// HeadlessBar are NOT tab-gated — they stay in the always-visible
	// bottom strip regardless of activeTab; only queue/detail vs. memory
	// are mutually exclusive. Zero value is tabItems, matching every
	// pre-existing session's default view before tabs existed.
	activeTab int

	// memory is a typed reference to the pane EnableMemoryTimeline appends,
	// mirroring the queue/detail fields above — needed so View() can render
	// it as the Memories tab's full-width content instead of via the
	// generic extraPanes() stacking loop, and so paneVisible/switchTab can
	// gate its focus-ring membership by activeTab. nil until
	// EnableMemoryTimeline is called, same lifecycle as timelineCtx et al.
	memory *MemoryTimelinePane

	// --- wavetui-memory-timeline on-demand dispatch state (task 3.2) ---
	// lastSelID/lastSelOK mirror the most recently notified QueuePane
	// selection, so notifySelection can tell a genuine selection CHANGE
	// (arms the debounce timer) apart from a Snapshot refresh that leaves
	// the cursor on the same row (must not re-fire the dispatcher).
	lastSelID string
	lastSelOK bool
	// timelineGen is the current debounce generation — see
	// timelineDebounceMsg's doc comment for the generation-guard this
	// implements, symmetric with pending/flushTimer's own guard above but
	// for selection changes instead of Store snapshots.
	timelineGen int
	// timelineCtx/beadsQuerier/archiveQuerier/memoryQuerier are nil until
	// EnableMemoryTimeline is called (main.go, task 3.4) — a Root nobody
	// ever calls that on (e.g. every pre-existing test in this package)
	// dispatches zero timeline queries; see armTimelineDebounce's nil-guard.
	timelineCtx    context.Context
	beadsQuerier   beadsHistoryQuerier
	archiveQuerier archiveQuerier
	memoryQuerier  memoryQuerier
}

// NewRoot constructs a Root wired to the given QueuePane and DetailPane. The
// focus ring order is [queue, detail] — detail is not Focusable (see
// detailpane.go), so it never actually receives focus today, but design.md §
// Pane extensibility expects sibling proposals to append further Focusable
// panes to this same ring without a Root rework.
func NewRoot(queue *QueuePane, detail *DetailPane) *Root {
	panes := []Pane{queue, detail}
	r := &Root{
		panes:  panes,
		queue:  queue,
		detail: detail,
		now:    time.Now,
	}
	r.focus = firstFocusable(panes)
	return r
}

// Queue exposes the root model's QueuePane so external wiring (wavetui-
// flair's cmd/wavetui integration, task [3.2]) can call QueuePane-only
// methods like SetHighlights without Root itself needing to know anything
// about wavetui-flair. Purely an additive accessor — Root's own Update/View
// above, and its panes slice, are unmodified by this task.
func (r *Root) Queue() *QueuePane { return r.queue }

// AppendPane adds p to the end of the focus ring — design.md § Pane
// extensibility: "Root model exposes a pluggable pane and focus-ring
// architecture for future sibling panes." Append-only: it never reorders or
// removes any existing pane. If nothing was previously focusable (an
// unreachable state today, since QueuePane is always Focusable, but kept
// correct for a hypothetical future caller) and p is Focusable, focus moves
// to it.
func (r *Root) AppendPane(p Pane) {
	r.panes = append(r.panes, p)
	if r.focus < 0 && p.Focusable() {
		r.focus = len(r.panes) - 1
	}
}

// EnableMemoryTimeline wires the three timeline sources into Root's
// selection-change dispatch (task 3.2) and appends pane to the focus ring
// via AppendPane (task 3.3/3.4) — a single call site so cmd/wavetui/main.go's
// wiring stays a one-liner, matching flair_wiring.go's own additive-wiring
// precedent. ctx is the same run-scoped context main.go derives everything
// else from — cancelling it stops any in-flight timeline query the same way
// it stops the two live sources.
func (r *Root) EnableMemoryTimeline(ctx context.Context, beads beadsHistoryQuerier, archive archiveQuerier, memory memoryQuerier, pane *MemoryTimelinePane) {
	r.timelineCtx = ctx
	r.beadsQuerier = beads
	r.archiveQuerier = archive
	r.memoryQuerier = memory
	r.memory = pane
	r.AppendPane(pane)
}

// Tab indices for Root.activeTab.
const (
	tabItems    = 0
	tabMemories = 1
)

// tabLabels are the tab bar's display strings (also the click-target text
// tabAtX measures) — the bracketed digit doubles as the keybinding hint and
// the "1"/"2" handleKey case below.
var tabLabels = []string{"[1] Items", "[2] Memories"}

// tabBarSeparator joins tabLabels in both renderTabBar and tabAtX — declared
// once so the two stay in sync (a click's X math must match what was
// actually rendered).
const tabBarSeparator = "   "

// switchTab sets activeTab and moves focus to that tab's primary pane
// (queue for Items, memory for Memories) so a manual tab switch never
// leaves focus stranded on a pane paneVisible is about to hide — see
// paneVisible's doc comment for why a hidden pane must not keep focus.
// A nil r.memory (EnableMemoryTimeline never called) makes switching to
// tabMemories a no-op — nothing to show, nothing to focus.
func (r *Root) switchTab(tab int) {
	if tab == tabMemories && r.memory == nil {
		return
	}
	r.activeTab = tab
	if tab == tabMemories {
		r.focus = indexOf(r.panes, Pane(r.memory))
	} else {
		r.focus = indexOf(r.panes, r.queue)
	}
}

// paneVisible reports whether p is currently rendered, given r.activeTab —
// queue and memory are mutually exclusive tab content (see the Root.
// activeTab doc comment); every other pane (detail, sessions, kpi,
// headlessbar) is always visible. Consulted by nextFocusable so Tab/
// Shift+Tab never cycles focus onto a pane that isn't on screen — a
// keypress reaching a hidden pane would silently mutate state the operator
// can't see (e.g. the queue's cursor moving while the Memories tab is
// showing), which is worse than simply skipping it in the ring.
func (r *Root) paneVisible(p Pane) bool {
	if r.memory != nil && p == Pane(r.memory) {
		return r.activeTab == tabMemories
	}
	if p == Pane(r.queue) {
		return r.activeTab == tabItems
	}
	return true
}

// bodyLeftPad returns how many columns of left padding View()'s
// lipgloss.PlaceHorizontal(r.width, lipgloss.Center, body) actually put in
// front of every body line — replicated from lipgloss v2.0.5's own
// PlaceHorizontal (position.go): for Center, split =
// round(gap*0.5), left = gap-split. Every body line renders at exactly
// r.contentWidth() (renderTabBar pads itself to match — see its doc
// comment — and every other row is sized to contentWidth by layout()), so
// gap is the same single terminal-wide value for the whole body and this
// one calculation is valid for every line, not just the tab bar's.
// Duplicated here (rather than calling lipgloss.PlaceHorizontal itself)
// because there is no exported way to ask it "where would you have put
// this" without re-rendering the whole body just to measure it.
func (r *Root) bodyLeftPad() int {
	gap := r.width - r.contentWidth()
	if gap <= 0 {
		return 0
	}
	split := int(math.Round(float64(gap) * 0.5))
	return gap - split
}

// tabAtX returns which tab index (tabItems/tabMemories) contains local
// x — an X coordinate already relative to the tab bar's own left edge
// (i.e. with bodyLeftPad already subtracted by the caller) — or -1 if x
// falls in the separator gap or past the last tab. Mirrors renderTabBar's
// exact join (tabLabels + tabBarSeparator between each) so a click always
// maps to the same boundary the bar was actually drawn with.
func tabAtX(x int) int {
	if x < 0 {
		return -1
	}
	pos := 0
	for i, label := range tabLabels {
		w := lipgloss.Width(label)
		if x < pos+w {
			return i
		}
		pos += w + lipgloss.Width(tabBarSeparator)
	}
	return -1
}

// handleMouseClick routes a left-click by screen position: row 0 is
// always the tab bar (renderTabBar is the first line View() joins into
// body), so a click there switches tabs the same way pressing "1"/"2"
// does. Every other row is a no-op today — see the wavetui-build-hook-
// style follow-up note in this change's summary for why queue-row
// click-to-select isn't included: bubbles/v2's table.Model exposes no
// viewport-scroll-offset accessor, so an absolute screen row can't be
// reliably mapped back to an absolute item index once the table has
// scrolled past its first page.
func (r *Root) handleMouseClick(m tea.Mouse) {
	if m.Button != tea.MouseLeft {
		return
	}
	if m.Y != 0 {
		return
	}
	if tab := tabAtX(m.X - r.bodyLeftPad()); tab >= 0 {
		r.switchTab(tab)
	}
}

// handleMouseWheel forwards a wheel event to the queue table's own
// cursor movement — only meaningful (and only wired) when the Items tab
// is showing the queue at all; MemoryTimelinePane exposes no scroll-
// offset API to forward a wheel event to. Returns notifySelection's Cmd
// so a wheel-driven cursor move re-syncs DetailPane/MemoryTimelinePane's
// selection exactly like an up/down keypress already does (see
// QueuePane's HandleKey case in handleKey).
func (r *Root) handleMouseWheel(m tea.Mouse) tea.Cmd {
	if r.activeTab != tabItems {
		return nil
	}
	switch m.Button {
	case tea.MouseWheelUp:
		r.queue.MoveCursor(-1)
	case tea.MouseWheelDown:
		r.queue.MoveCursor(1)
	default:
		return nil
	}
	item, ok := r.queue.SelectedItem()
	return r.notifySelection(item, ok)
}

func firstFocusable(panes []Pane) int {
	for i, p := range panes {
		if p.Focusable() {
			return i
		}
	}
	return -1
}

// Init satisfies tea.Model. Root has nothing to kick off on its own — all
// state arrives via SnapshotMsg sent from outside (main.go).
func (r *Root) Init() tea.Cmd { return nil }

// Update satisfies tea.Model. Per design.md, this method contains NO
// watcher/polling logic of its own — SnapshotMsg is pushed in from outside
// via Program.Send(), and flushMsg is this package's own coalescing tick.
func (r *Root) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case SnapshotMsg:
		return r.handleSnapshot(m.Snapshot)
	case flushMsg:
		return r.handleFlush()
	case timelineDebounceMsg:
		return r, r.handleTimelineDebounce(m)
	case TimelineMsg:
		return r.handleTimelineMsg(m)
	case tea.WindowSizeMsg:
		r.width, r.height = m.Width, m.Height
		r.layout()
		return r, nil
	case tea.KeyPressMsg:
		return r.handleKey(m)
	case tea.MouseClickMsg:
		r.handleMouseClick(m.Mouse())
		return r, nil
	case tea.MouseWheelMsg:
		return r, r.handleMouseWheel(m.Mouse())
	}
	return r, nil
}

// handleSnapshot implements the ~10fps render-coalescing cap: the first
// SnapshotMsg after an idle period (or after renderInterval has elapsed
// since the last applied one) is applied immediately; any SnapshotMsg
// arriving inside that window is coalesced — only the latest one is kept as
// pending, and at most one flush timer is ever scheduled to drain it once
// the window closes. A burst of arbitrarily many SnapshotMsgs therefore
// never applies more than one pane update per renderInterval.
func (r *Root) handleSnapshot(snap store.Snapshot) (tea.Model, tea.Cmd) {
	now := r.now()
	if r.lastApply.IsZero() || now.Sub(r.lastApply) >= renderInterval {
		cmd := r.applySnapshot(snap)
		r.lastApply = now
		return r, cmd
	}

	r.pending = &snap
	if r.flushTimer {
		return r, nil
	}
	r.flushTimer = true
	wait := renderInterval - now.Sub(r.lastApply)
	return r, tea.Tick(wait, func(time.Time) tea.Msg { return flushMsg{} })
}

// handleFlush drains a coalesced pending snapshot, if any arrived during the
// window that just closed.
func (r *Root) handleFlush() (tea.Model, tea.Cmd) {
	r.flushTimer = false
	if r.pending == nil {
		return r, nil
	}
	snap := *r.pending
	r.pending = nil
	cmd := r.applySnapshot(snap)
	r.lastApply = r.now()
	return r, cmd
}

// applySnapshot is the single place every pane actually gets Update(snap)
// called on it, and the single place every SelectionAware pane's selection
// is re-synced to QueuePane's current cursor (e.g. a snapshot may have
// removed the previously-selected item, or reordered rows). Returns
// whatever tea.Cmd notifySelection produces — non-nil only when the
// selection genuinely changed as a result (see notifySelection).
func (r *Root) applySnapshot(snap store.Snapshot) tea.Cmd {
	for i, p := range r.panes {
		r.panes[i] = p.Update(snap)
	}
	item, ok := r.queue.SelectedItem()
	r.errors = snap.Errors
	return r.notifySelection(item, ok)
}

// notifySelection propagates the current QueuePane selection to every
// SelectionAware pane (DetailPane today; any appended SelectionAware pane —
// e.g. MemoryTimelinePane — going forward) per design.md § On-demand
// querying's directive to reuse the existing selection-threading mechanism
// rather than inventing a second one. When the selection has genuinely
// changed since the last call (a different item ID, or a transition to/from
// "nothing selected"), it also arms the timeline debounce timer and returns
// the resulting tea.Cmd; an unchanged selection (e.g. a Snapshot refresh
// that leaves the cursor on the same row) returns nil — the on-demand
// dispatcher must never re-fire for a selection that didn't actually move.
func (r *Root) notifySelection(item store.Item, ok bool) tea.Cmd {
	for _, p := range r.panes {
		if sa, isSA := p.(SelectionAware); isSA {
			sa.SetSelected(item, ok)
		}
	}

	changed := ok != r.lastSelOK || (ok && item.ID != r.lastSelID)
	r.lastSelOK = ok
	r.lastSelID = item.ID
	if !changed {
		return nil
	}
	return r.armTimelineDebounce(item, ok)
}

// handleKey handles global keybindings (quit, focus-cycling) and otherwise
// forwards the key to whichever pane currently has focus, re-syncing
// DetailPane's selection afterward — see the Root doc comment for why this
// goes through a concrete-type assertion rather than the Pane interface.
func (r *Root) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		r.quitting = true
		return r, tea.Quit
	case "tab":
		r.focusNext()
		return r, nil
	case "shift+tab":
		r.focusPrev()
		return r, nil
	case "1":
		r.switchTab(tabItems)
		return r, nil
	case "2":
		r.switchTab(tabMemories)
		return r, nil
	}

	if r.focus >= 0 && r.focus < len(r.panes) {
		switch p := r.panes[r.focus].(type) {
		case *QueuePane:
			p.HandleKey(msg)
			item, sel := p.SelectedItem()
			return r, r.notifySelection(item, sel)
		case *SessionsPane:
			// SessionsPane (wavetui-sessions, tasks.md [3.1]) owns its own
			// row navigation and one-key zombie-release action — same
			// concrete-type dispatch precedent as QueuePane above, added
			// for the second pane in this package that needs key input.
			p.HandleKey(msg)
			return r, nil
		case *HeadlessBar:
			// HeadlessBar (wavetui-daemon, tasks.md [3.1]) owns its own
			// one-key resume action — same concrete-type dispatch
			// precedent as SessionsPane above.
			p.HandleKey(msg)
			return r, nil
		}
	}
	return r, nil
}

// maxContentWidth caps how wide the body ever renders, regardless of actual
// terminal width — an ultrawide terminal (200+ cols) otherwise stretched
// every pane edge-to-edge with mostly dead space in between, which is worse
// for reading a table + a 44-wide detail pane than a capped, centered
// column. View() centers the composed body within the real terminal width
// via lipgloss.PlaceHorizontal once layout() has sized everything against
// this cap; a terminal narrower than the cap is completely unaffected
// (contentWidth just equals r.width, PlaceHorizontal is then a no-op).
const maxContentWidth = 130

// tabBarLines is the tab bar's own row budget — one line, no border (see
// renderTabBar) — that layout() must reserve from the queue table's height
// the same way helpLines already does for the trailing help line below.
const tabBarLines = 1

// contentWidth is the single source of truth for "how wide does the body
// actually render" — layout()'s size math, renderTabBar's padding, and
// handleMouseClick's hit-testing ALL call this rather than each
// re-deriving the maxContentWidth cap independently, so a click can never
// land against a different width than what was actually drawn.
func (r *Root) contentWidth() int {
	if r.width > maxContentWidth {
		return maxContentWidth
	}
	return r.width
}

// layout resizes QueuePane's table to fit the current terminal size (capped
// at maxContentWidth), giving DetailPane its fixed detailWidth and
// splitting whatever width remains (minus each pane's own border+padding
// frame) to the queue. Called from Update's tea.WindowSizeMsg handling —
// see QueuePane.SetSize's doc comment for why this exists at all (bubbles/
// v2's viewport renders empty at Width()==0).
func (r *Root) layout() {
	if r.width <= 0 || r.height <= 0 {
		return
	}
	const paneFrame = 4 // lipgloss RoundedBorder (2) + Padding(0,1) (2), per pane

	contentWidth := r.contentWidth()

	detailBoxWidth := detailWidth + paneFrame
	queueWidth := contentWidth - detailBoxWidth - paneFrame
	if queueWidth < 20 {
		queueWidth = 20
	}

	const helpLines = 2 // the joined pane row's own newline + the help line below it

	// Only the ALWAYS-VISIBLE extras (sessions/kpi/headlessbar) reserve
	// height here — the memory timeline is now tab-exclusive content (see
	// Root.activeTab), rendered full-height in place of the queue/detail
	// row rather than stacked below it, so it must NOT also claim a slice
	// of the queue's budget the way it did before tabs existed (that would
	// double-reserve: once implicitly by not being part of tableHeight, and
	// once explicitly here).
	extras := r.persistentExtras()
	reserved := len(extras) * extraPaneReservedRows

	tableHeight := r.height - paneFrame - helpLines - tabBarLines - reserved
	if tableHeight < 3 {
		tableHeight = 3
	}

	r.queue.SetSize(queueWidth, tableHeight)

	// The memory timeline gets the SAME full height budget the queue/
	// detail row would otherwise use — when it's the active tab, nothing
	// else occupies that space, so it should fill it, not settle for the
	// old cramped extraPaneReservedRows box. Sized unconditionally
	// (regardless of r.activeTab) so switching tabs via "1"/"2" or a mouse
	// click never needs a fresh tea.WindowSizeMsg to render at full size.
	if r.memory != nil {
		r.memory.SetSize(contentWidth-paneFrame, tableHeight)
	}

	// Size any Sizeable persistent extra pane to the same full content
	// width the queue/detail row uses, and to its reserved row budget. A
	// pane that doesn't implement Sizeable (most won't need to) is simply
	// left at whatever default width/height it constructed itself with.
	for _, extra := range extras {
		if s, ok := extra.(Sizeable); ok {
			s.SetSize(contentWidth-paneFrame, extraPaneReservedRows-paneFrame)
		}
	}
}

// extraPaneReservedRows is the fixed row budget layout() reserves per
// appended extra pane (frame + content) — generous enough for
// MemoryTimelinePane's header line, an unavailable-badge line, and a
// handful of date-grouped entries without needing real scrolling machinery,
// which is out of scope for this task.
const extraPaneReservedRows = 12

// extraPanes returns every pane beyond the fixed queue/detail pair — i.e.
// any pane attached via AppendPane. A helper rather than inline slicing so
// the "queue/detail are always panes[0:2]" invariant NewRoot establishes is
// documented in exactly one place.
func (r *Root) extraPanes() []Pane {
	if len(r.panes) <= 2 {
		return nil
	}
	return r.panes[2:]
}

// persistentExtras returns extraPanes() minus the memory timeline pane —
// the subset that still renders in the always-visible bottom strip (see
// View()) now that memory is tab-exclusive content instead of a third
// stacked row. Used by both layout()'s reserved-height math and View()'s
// rendering loop so the two stay in lockstep (a pane sized here but not
// rendered there, or vice versa, would silently desync the layout).
func (r *Root) persistentExtras() []Pane {
	extras := r.extraPanes()
	if r.memory == nil {
		return extras
	}
	out := make([]Pane, 0, len(extras))
	for _, p := range extras {
		if p == Pane(r.memory) {
			continue
		}
		out = append(out, p)
	}
	return out
}

func (r *Root) focusNext() { r.focus = r.nextFocusable(1) }
func (r *Root) focusPrev() { r.focus = r.nextFocusable(-1) }

// nextFocusable walks the focus ring in dir (+1 or -1) starting just past
// the current focus, wrapping around, and returns the index of the next
// Focusable pane. Returns the current focus unchanged if no pane (other
// than possibly the current one) is Focusable.
func (r *Root) nextFocusable(dir int) int {
	n := len(r.panes)
	if n == 0 {
		return r.focus
	}
	idx := r.focus
	for i := 0; i < n; i++ {
		idx = ((idx+dir)%n + n) % n
		if r.panes[idx].Focusable() && r.paneVisible(r.panes[idx]) {
			return idx
		}
	}
	return r.focus
}

// View satisfies tea.Model. Per the wavetui-core constraint, this uses plain
// lipgloss style composition (JoinHorizontal) for the two-pane split — the
// Layer/Canvas primitives lipgloss v2 also offers are wavetui-flair's
// territory, not needed here.
func (r *Root) View() tea.View {
	if r.quitting {
		v := tea.NewView("")
		return v
	}

	body := lipgloss.JoinVertical(lipgloss.Left, r.renderTabBar(), r.renderTabContent())

	// Persistent extras (sessions/kpi/headlessbar — NOT the memory
	// timeline, which is tab-exclusive content rendered by
	// renderTabContent above) still stack below as their own full-width
	// row each. A pane whose View() returns "" (wavetui-daemon's
	// HeadlessBar, in its common not-paused/never-enabled case —
	// design.md § Additive Snapshot field: "renders nothing") is skipped
	// entirely rather than wrapped in an empty bordered box — an empty box
	// is still a visible box, which would contradict "renders nothing."
	for _, extra := range r.persistentExtras() {
		content := extra.View()
		if content == "" {
			continue
		}
		style := paneStyle(r.focus == indexOf(r.panes, extra))
		body = lipgloss.JoinVertical(lipgloss.Left, body, style.Render(content))
	}

	help := lipgloss.NewStyle().Faint(true).Render("1/2 or click: tabs  tab: switch pane  ↑/↓ or wheel: select  q: quit")

	statusLine := help
	if badges := r.unavailableBadges(); badges != "" {
		statusLine = badges + "  " + help
	}

	body = body + "\n" + statusLine

	// maxContentWidth (see layout()) caps how wide every pane was actually
	// sized; centering the composed body within the real terminal width
	// here is what makes that cap visible instead of just leaving dead
	// space flush to the left edge. A no-op whenever r.width is already
	// <= maxContentWidth.
	v := tea.NewView(lipgloss.PlaceHorizontal(r.width, lipgloss.Center, body))
	v.AltScreen = true
	// MouseModeCellMotion enables click/release/wheel events (not full
	// motion tracking, which would flood Update with every pixel move) —
	// see handleMouseClick/handleMouseWheel for what this actually wires
	// up to. Previously unset entirely, which is why NO mouse interaction
	// of any kind worked before this.
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

// renderTabBar renders the "[1] Items   [2] Memories" tab strip — the
// active tab bold+underlined+accented, the inactive one faint. tabAtX's
// hit-testing math (handleMouseClick) must stay in lockstep with the
// exact labels/separator rendered here; both read from the same
// tabLabels/tabBarSeparator so they can't drift independently.
//
// Padded (left-aligned) to the full contentWidth rather than left at its
// own short natural width: View()'s final lipgloss.PlaceHorizontal centers
// the WHOLE body by measuring each line's width and centering the
// shortfall independently per line — a short tab-bar line would get
// re-centered on its OWN, different from the wider queue/detail row below
// it, making the two lines' left edges land at different absolute X
// coordinates. Padding every line to the identical contentWidth first
// means PlaceHorizontal's per-line "short" compensation is always zero, so
// the single outer gap/2 split (handleMouseClick's leftPad) applies
// uniformly to every line — including this one.
func (r *Root) renderTabBar() string {
	parts := make([]string, len(tabLabels))
	for i, label := range tabLabels {
		style := lipgloss.NewStyle().Faint(true)
		if i == r.activeTab {
			style = lipgloss.NewStyle().Bold(true).Underline(true).Foreground(lipgloss.Color("212"))
		}
		parts[i] = style.Render(label)
	}
	bar := strings.Join(parts, tabBarSeparator)
	return lipgloss.NewStyle().Width(r.contentWidth()).Render(bar)
}

// renderTabContent renders whichever tab is active: the historical queue+
// detail row (tabItems), or the memory timeline full-width (tabMemories).
// A nil r.memory (EnableMemoryTimeline never called, e.g. every
// pre-existing test in this package) can never actually show tabMemories —
// switchTab already refuses to switch there — so this only ever hits the
// tabItems branch in that case, rendering byte-for-byte as it always has.
func (r *Root) renderTabContent() string {
	if r.activeTab == tabMemories && r.memory != nil {
		style := paneStyle(r.focus == indexOf(r.panes, Pane(r.memory)))
		return style.Render(r.memory.View())
	}

	queueStyle := paneStyle(r.focus == indexOf(r.panes, r.queue))
	detailStyle := paneStyle(r.focus == indexOf(r.panes, r.detail))
	return lipgloss.JoinHorizontal(
		lipgloss.Top,
		queueStyle.Render(r.queue.View()),
		detailStyle.Render(r.detail.View()),
	)
}

// unavailableBadges renders one status badge per active Snapshot.Errors
// entry — spec.md's "A missing .beads/ or openspec/ directory degrades to
// an unavailable badge, never a crash" Requirement. Returns "" when there
// are no active source errors, so callers can decide whether to add a
// separator without an empty-string special case.
func (r *Root) unavailableBadges() string {
	if len(r.errors) == 0 {
		return ""
	}
	parts := make([]string, 0, len(r.errors))
	for _, e := range r.errors {
		parts = append(parts, badgeText(e))
	}
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("203")).Render(strings.Join(parts, "  "))
}

// unavailablePrefix is the exact prefix sources/beads.go's and
// sources/openspec.go's publishUnavailable use — the ONLY case that means
// "the directory itself is genuinely missing." Every other SourceError
// (published by markStale, both sources) means the directory/source is
// still there and a re-query attempt failed — an operator seeing "beads
// unavailable" for that case would go looking for a missing .beads/ that
// was never actually gone.
const unavailablePrefix = "unavailable: "

// badgeMessageMaxLen caps how much of a transient failure's own
// error message is surfaced in the status line, so one long shell/parse
// error can't blow out the whole line.
const badgeMessageMaxLen = 40

// badgeText renders one SourceError as a status-line badge, distinguishing
// a genuinely-missing source directory from a transient CLI/parse failure
// (see unavailablePrefix) rather than collapsing both into the same generic
// "<source> unavailable" text and discarding the real SourceError.Message.
func badgeText(e store.SourceError) string {
	if strings.HasPrefix(e.Message, unavailablePrefix) {
		return e.Source + " unavailable"
	}
	return e.Source + " stale (retrying): " + truncateBadgeMessage(e.Message)
}

// truncateBadgeMessage rune-truncates msg to badgeMessageMaxLen, appending
// "…" when truncated, so a long error message can't be cut mid multi-byte
// rune.
func truncateBadgeMessage(msg string) string {
	r := []rune(msg)
	if len(r) <= badgeMessageMaxLen {
		return msg
	}
	return string(r[:badgeMessageMaxLen-1]) + "…"
}

func indexOf(panes []Pane, p Pane) int {
	for i, cand := range panes {
		if cand == p {
			return i
		}
	}
	return -1
}

func paneStyle(focused bool) lipgloss.Style {
	s := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	if focused {
		s = s.BorderForeground(lipgloss.Color("212"))
	}
	return s
}
