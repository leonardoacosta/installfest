// Package ui implements the bubbletea root model and Pane implementations
// (QueuePane, DetailPane) described in
// openspec/changes/wavetui-core/design.md § Architecture and
// § Pane extensibility.
package ui

import (
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
	case tea.WindowSizeMsg:
		r.width, r.height = m.Width, m.Height
		r.layout()
		return r, nil
	case tea.KeyPressMsg:
		return r.handleKey(m)
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
		r.applySnapshot(snap)
		r.lastApply = now
		return r, nil
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
	r.applySnapshot(snap)
	r.lastApply = r.now()
	return r, nil
}

// applySnapshot is the single place every pane actually gets Update(snap)
// called on it, and the single place DetailPane's selection is re-synced to
// QueuePane's current cursor (e.g. a snapshot may have removed the
// previously-selected item, or reordered rows).
func (r *Root) applySnapshot(snap store.Snapshot) {
	for i, p := range r.panes {
		r.panes[i] = p.Update(snap)
	}
	item, ok := r.queue.SelectedItem()
	r.detail.SetSelected(item, ok)
	r.errors = snap.Errors
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
	}

	if r.focus >= 0 && r.focus < len(r.panes) {
		if q, ok := r.panes[r.focus].(*QueuePane); ok {
			q.HandleKey(msg)
			item, sel := q.SelectedItem()
			r.detail.SetSelected(item, sel)
		}
	}
	return r, nil
}

// layout resizes QueuePane's table to fit the current terminal size, giving
// DetailPane its fixed detailWidth and splitting whatever width remains
// (minus each pane's own border+padding frame) to the queue. Called from
// Update's tea.WindowSizeMsg handling — see QueuePane.SetSize's doc comment
// for why this exists at all (bubbles/v2's viewport renders empty at
// Width()==0).
func (r *Root) layout() {
	if r.width <= 0 || r.height <= 0 {
		return
	}
	const paneFrame = 4 // lipgloss RoundedBorder (2) + Padding(0,1) (2), per pane

	detailBoxWidth := detailWidth + paneFrame
	queueWidth := r.width - detailBoxWidth - paneFrame
	if queueWidth < 20 {
		queueWidth = 20
	}

	const helpLines = 2 // the joined pane row's own newline + the help line below it
	tableHeight := r.height - paneFrame - helpLines
	if tableHeight < 3 {
		tableHeight = 3
	}

	r.queue.SetSize(queueWidth, tableHeight)
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
		if r.panes[idx].Focusable() {
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

	queueStyle := paneStyle(r.focus == indexOf(r.panes, r.queue))
	detailStyle := paneStyle(r.focus == indexOf(r.panes, r.detail))

	body := lipgloss.JoinHorizontal(
		lipgloss.Top,
		queueStyle.Render(r.queue.View()),
		detailStyle.Render(r.detail.View()),
	)

	help := lipgloss.NewStyle().Faint(true).Render("tab: switch pane  ↑/↓: select  q: quit")

	statusLine := help
	if badges := r.unavailableBadges(); badges != "" {
		statusLine = badges + "  " + help
	}

	v := tea.NewView(body + "\n" + statusLine)
	v.AltScreen = true
	return v
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
