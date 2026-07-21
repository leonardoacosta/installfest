// flair_wiring.go wires wavetui-flair's FlairManager/ToastOverlay into the
// bubbletea Program (tasks.md [3.2]) via a decorator tea.Model wrapping
// *ui.Root — this keeps internal/ui's own Update/View and its panes slice
// completely untouched by flair's presence; every tea.Msg the Program
// delivers is forwarded to root.Update unconditionally (see Update below),
// and this file adds flair-specific handling alongside that forwarding,
// never inside it. Matches design.md § Architecture: FlairManager sits
// beside the root model, never inside its pane slice.
package main

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/leonardoacosta/installfest/apps/wavetui/internal/flair"
	"github.com/leonardoacosta/installfest/apps/wavetui/internal/store"
	"github.com/leonardoacosta/installfest/apps/wavetui/internal/ui"
)

// flairFrameInterval is the fixed tick rate flair's animations advance at
// once any of them is live — matches effects.go's frameRate (30fps), the
// rate every harmonica spring in that package was tuned against via
// harmonica.FPS(frameRate). It is only ever scheduled while
// FlairManager.NeedsTick() is true (design.md § Tick-loop lifecycle) — see
// maybeTick below, this file's one and only tea.Tick call site.
const flairFrameInterval = time.Second / 30

// flairTickMsg is this wrapper's own tick payload, distinct from
// internal/ui's unexported flushMsg and from any message a sibling
// proposal's wiring might add later.
type flairTickMsg struct{}

// rootWithFlair wraps *ui.Root, additively layering wavetui-flair's
// diff/overlay/tick machinery around it. Every tea.Msg is forwarded to
// root.Update unconditionally — this file never reorders, removes, or
// bypasses any existing root behavior; it only adds flair-specific handling
// alongside that forwarding.
type rootWithFlair struct {
	root    *ui.Root
	mgr     *flair.FlairManager
	overlay *flair.ToastOverlay

	prevSnapshot store.Snapshot
	havePrev     bool

	lastToast     *flair.ToastRender
	width, height int
}

// newRootWithFlair constructs the wrapper. overlay may be nil (a nil
// overlay degrades View() to root's own output unchanged, same as a
// disabled FlairManager producing no toast).
func newRootWithFlair(root *ui.Root, mgr *flair.FlairManager, overlay *flair.ToastOverlay) *rootWithFlair {
	return &rootWithFlair{root: root, mgr: mgr, overlay: overlay}
}

// Init satisfies tea.Model.
func (m *rootWithFlair) Init() tea.Cmd { return m.root.Init() }

// Update forwards every message to root.Update first — Root always returns
// itself as the tea.Model it hands back (see root.go's doc comments), so
// that returned value is discarded and this wrapper (m) is returned
// instead, keeping the Program's model identity stable. ui.SnapshotMsg and
// this file's own flairTickMsg additionally drive FlairManager and
// conditionally re-schedule the next tick — every other message passes
// through with flair playing no part at all.
func (m *rootWithFlair) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	_, rootCmd := m.root.Update(msg)

	switch t := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = t.Width, t.Height
	case ui.SnapshotMsg:
		m.applySnapshot(t.Snapshot)
		return m, tea.Batch(rootCmd, m.maybeTick())
	case flairTickMsg:
		m.stepFlair()
		return m, tea.Batch(rootCmd, m.maybeTick())
	}
	return m, rootCmd
}

// applySnapshot diffs the transition from the previously-seen snapshot to
// next (via FlairManager.OnSnapshot, itself gated by Process's existing
// disabled-check) and steps the animation state one frame so the very next
// render already reflects any freshly-started effect.
//
// The FIRST snapshot this wrapper ever sees has no real prior state to diff
// against — treating a zero-value store.Snapshot{} as "prev" would report
// every already-existing item as EventItemAppeared and animate the entire
// queue on startup, which is not a real transition. So the first call only
// seeds prevSnapshot and returns.
func (m *rootWithFlair) applySnapshot(next store.Snapshot) {
	if !m.havePrev {
		m.prevSnapshot = next
		m.havePrev = true
		return
	}
	m.mgr.OnSnapshot(m.prevSnapshot, next)
	m.prevSnapshot = next
	m.stepFlair()
}

// stepFlair advances every currently-active animation/toast by one tick and
// pushes the resulting highlight map into QueuePane — the only place this
// file ever calls SetHighlights.
func (m *rootWithFlair) stepFlair() {
	highlights, toast := m.mgr.AdvanceFrame()
	m.root.Queue().SetHighlights(highlights)
	m.root.Queue().SetSpriteGlyphs(m.mgr.SpriteGlyphs())
	m.lastToast = toast
}

// maybeTick schedules the next flairTickMsg if and only if FlairManager
// still has something animating — design.md § Tick-loop lifecycle's
// zero-idle-cost invariant: no unconditional tea.Tick is ever issued here.
func (m *rootWithFlair) maybeTick() tea.Cmd {
	if !m.mgr.NeedsTick() {
		return nil
	}
	return tea.Tick(flairFrameInterval, func(time.Time) tea.Msg { return flairTickMsg{} })
}

// View renders root's own View() and, only when a toast is currently live,
// composites it on top via ToastOverlay.Compose (task [2.3]'s additive
// overlay layer). A nil overlay or nil lastToast (flair disabled, or simply
// nothing to show right now) returns root's output byte-for-byte unchanged.
func (m *rootWithFlair) View() tea.View {
	v := m.root.View()
	if m.overlay == nil || m.lastToast == nil {
		return v
	}
	v.Content = m.overlay.Compose(v.Content, m.lastToast.Message, m.lastToast.Accent, m.lastToast.YOffset, m.width, m.height)
	return v
}
