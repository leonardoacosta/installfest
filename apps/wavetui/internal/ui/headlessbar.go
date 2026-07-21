// See openspec/changes/wavetui-daemon/tasks.md [3.1],
// openspec/changes/wavetui-headless-admission/tasks.md [2.1], and design.md
// § Additive Snapshot field / § Rate-limit backpressure. HeadlessBar
// implements wavetui-core's Pane interface and is appended to Root's focus
// ring the same append-only way KPIBar/SessionsPane/MemoryTimelinePane
// already are (see kpibar.go, sessionspane.go, memorytimelinepane.go) — this
// is the sixth pane appended, following the exact same precedent.
//
// Unlike every other appended pane, HeadlessBar's View() returns the empty
// string in its common case (headless admission never toggled on this run,
// and the queue is not currently paused) — root.go's extra-pane rendering
// loop skips drawing a border box around empty content, so this is
// genuinely invisible on screen, not an empty bordered box. Once admission
// is toggled on, the bar becomes visible for the rest of the run to show
// the always-relevant enabled/in-flight indicator (spec.md's "An operator
// keybinding enables/disables headless admission" Requirement: the toggle
// state "is visibly indicated on HeadlessBar").
package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/leonardoacosta/installfest/apps/wavetui/internal/daemon"
	"github.com/leonardoacosta/installfest/apps/wavetui/internal/store"
)

// defaultHeadlessBarWidth sizes the bar before the first real
// tea.WindowSizeMsg arrives, mirroring every other appended pane's own
// default-width precedent (see kpibar.go's defaultKPIBarWidth).
const defaultHeadlessBarWidth = 96

// admissionToggleKey enables/disables headless admission (tasks.md [2.1]).
// "a" (mnemonic for "admission") is unused anywhere else in this package —
// checked HandleKey in this file (only "r", the resume action below) and
// every other pane's HandleKey (queuepane.go: enter/space/w/esc/s/x/up/k/
// down/j; sessionspane.go: up/k/down/j/r; root.go's global keys:
// ctrl+c/q/tab/shift+tab) before picking it, per spec.md's requirement that
// this be a single, unambiguous operator keypress.
const admissionToggleKey = "a"

// headlessBarAdmissionOnColor is the indicator color while admission is
// enabled — reuses "212", the same accent color root.go's focused-pane
// border and queuepane.go's ambiguous-candidate cursor already use for
// "notable/active" state, rather than introducing a new arbitrary color.
var headlessBarAdmissionOnColor = lipgloss.Color("212")

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
// border entirely for empty content) — only when admission has never been
// toggled on, the queue is not currently paused, and there is no pending
// lastAction line. See the HeadlessBar doc comment for why that is the
// common case (admission defaults to disabled every run).
func (h *HeadlessBar) View() string {
	var lines []string

	if h.ctrl.AdmissionEnabled() {
		status := fmt.Sprintf("HEADLESS ADMISSION ON — press %s to disable", admissionToggleKey)
		// ActiveCount/ConcurrencyCap are already on h.queue for the paused
		// banner below — reused here rather than adding new plumbing for
		// the "roughly how many slots are in-flight vs. the cap" nice-to-
		// have tasks.md [2.1] calls out (cheaply available, no new field).
		if h.queue != nil {
			status = fmt.Sprintf(
				"HEADLESS ADMISSION ON (in-flight %d/%d) — press %s to disable",
				h.queue.ActiveCount, h.queue.ConcurrencyCap, admissionToggleKey,
			)
		}
		lines = append(lines, lipgloss.NewStyle().Bold(true).Foreground(headlessBarAdmissionOnColor).Render(status))
	}

	if h.queue != nil && h.queue.Paused {
		banner := fmt.Sprintf(
			"HEADLESS QUEUE PAUSED (rate-limit backpressure, active %d/%d) — press r to resume",
			h.queue.ActiveCount, h.queue.ConcurrencyCap,
		)
		lines = append(lines, lipgloss.NewStyle().Bold(true).Foreground(headlessBarPauseColor).Render(banner))
	}

	if h.lastAction != "" {
		lines = append(lines, lipgloss.NewStyle().Faint(true).Render(h.lastAction))
	}

	if len(lines) == 0 {
		return ""
	}

	width := h.width
	if width <= 0 {
		width = defaultHeadlessBarWidth
	}
	return lipgloss.NewStyle().Width(width).Render(strings.Join(lines, "\n"))
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

// HandleKey handles this pane's own resume and admission-toggle actions.
// Deliberately outside the Pane interface, same rationale as
// sessionspane.go's own HandleKey: Root type-asserts to *HeadlessBar to
// reach this method (see root.go's handleKey) only when this pane currently
// has focus, so a keypress never reaches here unless the operator has
// actually navigated to this pane.
//
// Resume is called DIRECTLY on "r" — no scheduling, no timer, no debounce —
// matching design.md's hard constraint that resume is exclusively an
// operator keypress. A press while the queue is not paused (or headless
// dispatch was never enabled) is a silent no-op, not an error, since the
// keybinding has no meaning outside the paused case.
//
// ToggleAdmission is called DIRECTLY on admissionToggleKey ("a") — same
// single-explicit-keypress precedent, no config flag, no timer (spec.md's
// "An operator keybinding enables/disables headless admission" Requirement).
// View()'s indicator reads Controller.AdmissionEnabled() directly (no local
// mirror) so it can never drift from the real toggle state.
func (h *HeadlessBar) HandleKey(msg tea.KeyPressMsg) {
	switch msg.String() {
	case "r":
		if h.queue == nil || !h.queue.Paused {
			return
		}
		h.ctrl.Resume()
		h.lastAction = "resumed headless dispatch"
	case admissionToggleKey:
		h.ctrl.ToggleAdmission()
	}
}
