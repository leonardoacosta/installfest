// See openspec/changes/wavetui-daemon/tasks.md [3.1] and design.md §
// Additive Snapshot field / § Rate-limit backpressure. HeadlessBar
// implements wavetui-core's Pane interface and is appended to Root's focus
// ring the same append-only way KPIBar/SessionsPane/MemoryTimelinePane
// already are (see kpibar.go, sessionspane.go, memorytimelinepane.go) — this
// is the sixth pane appended, following the exact same precedent.
//
// Unlike every other appended pane, HeadlessBar's View() returns the empty
// string in its common case (headless dispatch never enabled this run, or
// enabled but not currently paused) — root.go's extra-pane rendering loop
// skips drawing a border box around empty content, so this is genuinely
// invisible on screen, not an empty bordered box.
package ui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/leonardoacosta/installfest/apps/wavetui/internal/daemon"
	"github.com/leonardoacosta/installfest/apps/wavetui/internal/store"
)

// defaultHeadlessBarWidth sizes the bar before the first real
// tea.WindowSizeMsg arrives, mirroring every other appended pane's own
// default-width precedent (see kpibar.go's defaultKPIBarWidth).
const defaultHeadlessBarWidth = 96

// headlessBarPauseColor is the banner color for a paused headless queue —
// reuses the exact color ("203") root.go's unavailableBadges and
// sessionspane.go's zombie badge already use for significant/error-weight
// state, rather than introducing a new arbitrary color for a state of
// comparable severity.
var headlessBarPauseColor = lipgloss.Color("203")

// HeadlessBar renders a banner + resume keybinding hint when the headless
// dispatch queue is paused (design.md § Rate-limit backpressure), and
// nothing at all otherwise. Nothing-at-all is the common case: headless
// dispatch defaults to disabled (Snapshot.HeadlessQueue stays nil until
// something enables it), and even once enabled, a queue spends most of its
// life not paused.
type HeadlessBar struct {
	// ctrl is the daemon controller whose Resume method (see
	// internal/daemon/daemon.go) HandleKey below calls DIRECTLY on the
	// resume keypress — no intermediate scheduling, per design.md's hard
	// constraint that resume is exclusively an operator keypress, never a
	// timer.
	ctrl *daemon.Controller

	// queue mirrors the latest Snapshot's HeadlessQueue pointer verbatim —
	// nil means "headless dispatch has never been enabled this run" (see
	// store.HeadlessQueueState's own doc comment), the same nil/zero-means-
	// inactive convention store.RateLimitSignal already established.
	queue *store.HeadlessQueueState

	width int

	// lastAction mirrors sessionspane.go's own transient one-line status
	// field: set after a resume keypress, shown until the next Update call
	// observes the queue is no longer paused (see Update below), which
	// clears it — a stale "resumed" line has nothing left to describe once
	// the pause it resolved is gone.
	lastAction string
}

// NewHeadlessBar constructs a HeadlessBar bound to ctrl — see the ctrl
// field's doc comment for why the resume keybinding calls it directly.
func NewHeadlessBar(ctrl *daemon.Controller) *HeadlessBar {
	return &HeadlessBar{ctrl: ctrl, width: defaultHeadlessBarWidth}
}

// Update implements Pane.
func (h *HeadlessBar) Update(snap store.Snapshot) Pane {
	h.queue = snap.HeadlessQueue
	if h.queue == nil || !h.queue.Paused {
		h.lastAction = ""
	}
	return h
}

// View implements Pane. Returns "" — genuinely nothing, not an empty
// bordered box (see root.go's extra-pane rendering loop, which skips the
// border entirely for empty content) — when HeadlessQueue is nil or the
// queue is not currently paused. See the HeadlessBar doc comment for why
// that is the common case.
func (h *HeadlessBar) View() string {
	if h.queue == nil || !h.queue.Paused {
		return ""
	}

	width := h.width
	if width <= 0 {
		width = defaultHeadlessBarWidth
	}

	banner := fmt.Sprintf(
		"HEADLESS QUEUE PAUSED (rate-limit backpressure, active %d/%d) — press r to resume",
		h.queue.ActiveCount, h.queue.ConcurrencyCap,
	)
	line := lipgloss.NewStyle().Bold(true).Foreground(headlessBarPauseColor).Render(banner)

	if h.lastAction != "" {
		line += "\n" + lipgloss.NewStyle().Faint(true).Render(h.lastAction)
	}

	return lipgloss.NewStyle().Width(width).Render(line)
}

// Focusable implements Pane. HeadlessBar joins the focus ring (design.md §
// Pane implementation precedent every appended pane follows) — the resume
// action below requires this pane to be focused, the same
// focus-gates-the-one-key-action rationale sessionspane.go's own
// HandleKey/Focusable already established for its zombie-release action.
func (h *HeadlessBar) Focusable() bool { return true }

// SetSize implements the Sizeable optional interface (root.go). HeadlessBar
// is a single-banner-line pane with nothing to scroll, so only width affects
// rendering; height is accepted (matching the Sizeable signature every other
// appended pane implements) and otherwise unused — mirrors kpibar.go's own
// SetSize precedent.
func (h *HeadlessBar) SetSize(width, height int) {
	h.width = width
	_ = height
}

// HandleKey handles this pane's own one-key resume action. Deliberately
// outside the Pane interface, same rationale as sessionspane.go's own
// HandleKey: Root type-asserts to *HeadlessBar to reach this method (see
// root.go's handleKey) only when this pane currently has focus, so a
// keypress never reaches here unless the operator has actually navigated to
// this pane.
//
// Resume is called DIRECTLY on "r" — no scheduling, no timer, no debounce —
// matching design.md's hard constraint that resume is exclusively an
// operator keypress. A press while the queue is not paused (or headless
// dispatch was never enabled) is a silent no-op, not an error, since the
// keybinding has no meaning outside the paused case.
func (h *HeadlessBar) HandleKey(msg tea.KeyPressMsg) {
	if msg.String() != "r" {
		return
	}
	if h.queue == nil || !h.queue.Paused {
		return
	}
	h.ctrl.Resume()
	h.lastAction = "resumed headless dispatch"
}
