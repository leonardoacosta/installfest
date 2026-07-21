// See openspec/changes/wavetui-sessions/tasks.md [3.1] and design.md § Store
// additive fields / § Zombie detection. SessionsPane implements wavetui-
// core's Pane interface and additionally implements Sizeable (see root.go),
// the same append-only extra-pane pattern wavetui-memory-timeline's
// MemoryTimelinePane already established — it is appended to Root's focus
// ring via AppendPane, with no reordering or removal of any existing pane.
//
// Unlike MemoryTimelinePane, SessionsPane needs its own key handling (the
// one-key zombie-release action below), so it also exposes a HandleKey
// method outside the Pane interface — the exact precedent queuepane.go's own
// HandleKey/SelectedItem already set for a pane Root forwards keys to via a
// concrete-type assertion (see root.go's handleKey).
package ui

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/leonardoacosta/installfest/apps/wavetui/internal/bus"
	"github.com/leonardoacosta/installfest/apps/wavetui/internal/sources"
	"github.com/leonardoacosta/installfest/apps/wavetui/internal/store"
)

// defaultSessionsWidth sizes the pane before the first real
// tea.WindowSizeMsg arrives — Root.layout() calls SetSize once it does (see
// root.go), mirroring MemoryTimelinePane's own defaultMemoryTimelineWidth
// precedent.
const defaultSessionsWidth = 96

// sessionZombieColor is the badge color for a zombie-badged session's row —
// reuses the exact color ("203") root.go's unavailableBadges and
// memorytimelinepane.go's unavailableBadges already use for
// significant/error-weight state, rather than introducing a new arbitrary
// color for a state of comparable severity.
var sessionZombieColor = lipgloss.Color("203")

// SessionsPane renders one row per Claude Code session currently linked to
// an Item (Item.Session != nil) — pane identity when TmuxSource resolved
// one, the context-percent gauge, and a zombie badge with its one-key
// release action. See design.md § Store additive fields and § Zombie
// detection.
type SessionsPane struct {
	// ctx is the same run-scoped context cmd/wavetui/main.go derives every
	// other source from — releaseSelected's sources.ReleaseClaim call is
	// cancelled the same way those sources are when the program quits.
	ctx context.Context

	// bus is the same event bus every source publishes onto — threaded
	// through so releaseSelected's sources.ReleaseClaim call can publish the
	// session-link-cleared event that tells the Store the release happened
	// (see ReleaseClaim's doc comment). This is the one UI-triggered Store
	// mutation in this codebase; it goes through the bus rather than
	// calling store.Store directly because Store.Apply is documented as
	// callable exclusively from the bus-delivery goroutine (design.md §
	// Architecture) — the UI never holds a *store.Store reference at all.
	bus *bus.Bus

	// sessions mirrors the subset of the latest Snapshot's Items that carry
	// a non-nil Session, in the same order Store.Snapshot already returns
	// (sorted by ID) — cursor indexes into this slice, not the full item
	// set, since a row with no linked session has nothing this pane renders.
	sessions []store.Item
	// cursor is this pane's own selection, independent of QueuePane's own
	// cursor (design.md/tasks.md [3.1]: the release action is gated to
	// "focused/selected in that pane", i.e. this pane's own selection, not
	// whatever QueuePane happens to have selected). -1 means no selection
	// (the empty-sessions case).
	cursor int

	width  int
	height int

	// lastAction is a transient one-line status from the most recent
	// release attempt ("released <id>" / "release <id> failed: <err>"),
	// shown until the next release attempt overwrites it. Empty means
	// nothing to show — no release has been attempted yet this run.
	lastAction string
}

// NewSessionsPane constructs an empty SessionsPane (no linked sessions yet).
// ctx and b are threaded through to releaseSelected's sources.ReleaseClaim
// call — see the ctx/bus fields' doc comments.
func NewSessionsPane(ctx context.Context, b *bus.Bus) *SessionsPane {
	return &SessionsPane{ctx: ctx, bus: b, width: defaultSessionsWidth, cursor: -1}
}

// Update implements Pane. It rebuilds the session list from the snapshot's
// Items (filtering to Session != nil) and, where possible, preserves the
// operator's current row selection across the rebuild by re-locating the
// previously-selected item's ID in the new set — the same cursor-
// preservation rationale queuepane.go's own Update already documents.
func (s *SessionsPane) Update(snap store.Snapshot) Pane {
	prevID := s.selectedID()

	sessions := make([]store.Item, 0, len(snap.Items))
	for _, it := range snap.Items {
		if it.Session != nil {
			sessions = append(sessions, it)
		}
	}
	s.sessions = sessions

	if idx := indexOfItemID(sessions, prevID); idx >= 0 {
		s.cursor = idx
	} else if len(sessions) == 0 {
		s.cursor = -1
	} else if s.cursor < 0 || s.cursor >= len(sessions) {
		s.cursor = 0
	}

	return s
}

// View implements Pane.
func (s *SessionsPane) View() string {
	width := s.width
	if width <= 0 {
		width = defaultSessionsWidth
	}

	var lines []string
	lines = append(lines, lipgloss.NewStyle().Bold(true).Render("Sessions"))

	if len(s.sessions) == 0 {
		lines = append(lines, lipgloss.NewStyle().Faint(true).Render("No linked Claude Code sessions."))
	} else {
		for i, it := range s.sessions {
			lines = append(lines, s.renderRow(i, it))
		}
	}

	if s.lastAction != "" {
		lines = append(lines, "", lipgloss.NewStyle().Faint(true).Render(s.lastAction))
	}

	return lipgloss.NewStyle().Width(width).Render(strings.Join(lines, "\n"))
}

// renderRow renders one session: a cursor marker, the item's title, its
// tmux pane identity when TmuxSource resolved one (design.md's PaneID: ""
// means "not every session runs inside a cc-tmux-tracked pane" — rendered
// as "no pane", not a blank/misleading cell), the context-percent gauge
// (bold+colored once it crosses sources.IsHandoffThreshold, the same 70%
// constant transcript.go's own handoff badge uses — never re-declared
// here), and the zombie badge plus its release keybinding hint.
func (s *SessionsPane) renderRow(i int, it store.Item) string {
	marker := "  "
	if i == s.cursor {
		marker = "> "
	}

	sl := it.Session

	pane := "no pane"
	if sl.PaneID != "" {
		pane = "pane " + sl.PaneID
	}

	ctxPct := fmt.Sprintf("%.0f%% ctx", sl.ContextPct)
	if sources.IsHandoffThreshold(sl.ContextPct) {
		ctxPct = lipgloss.NewStyle().Bold(true).Foreground(sessionZombieColor).Render(ctxPct)
	}

	row := fmt.Sprintf("%s%s  %s  %s", marker, it.Title, pane, ctxPct)

	if sl.Zombie {
		row += "  " + lipgloss.NewStyle().Bold(true).Foreground(sessionZombieColor).
			Render("ZOMBIE — press r to release")
	}

	return row
}

// Focusable implements Pane. SessionsPane joins the focus ring (design.md §
// Pane implementation: "attaches to the existing focus ring by appending to
// the root model's pane slice") — the release action requires this pane to
// be focused/selected, which requires it to participate in Tab-cycling.
func (s *SessionsPane) Focusable() bool { return true }

// SetSize implements the Sizeable optional interface (root.go) — mirrors
// MemoryTimelinePane.SetSize's precedent for Root's own
// tea.WindowSizeMsg-driven layout pass.
func (s *SessionsPane) SetSize(width, height int) {
	s.width = width
	s.height = height
}

// HandleKey handles this pane's own row navigation and the one-key zombie-
// release action. Deliberately outside the Pane interface, same rationale
// as queuepane.go's HandleKey: Root type-asserts to *SessionsPane to reach
// this method (see root.go's handleKey) only when this pane currently has
// focus, so a keypress never reaches here unless the operator has actually
// navigated to this pane.
func (s *SessionsPane) HandleKey(msg tea.KeyPressMsg) {
	switch msg.String() {
	case "up", "k":
		if s.cursor > 0 {
			s.cursor--
		}
	case "down", "j":
		if s.cursor >= 0 && s.cursor < len(s.sessions)-1 {
			s.cursor++
		}
	case "r":
		s.releaseSelected()
	}
}

// releaseSelected is the one-key, NEVER-AUTOMATIC release action spec.md's
// Zombie-detection Requirement requires: it calls sources.ReleaseClaim ONLY
// when the currently focused/selected row is zombie-badged, and only in
// direct response to the "r" keypress above — never on a timer, never as a
// side effect of rendering or of a Snapshot refresh. A non-zombie selection
// (or no selection at all) is a silent no-op, not an error, since the
// keybinding has no meaning outside the zombie-badged case.
func (s *SessionsPane) releaseSelected() {
	if s.cursor < 0 || s.cursor >= len(s.sessions) {
		return
	}
	item := s.sessions[s.cursor]
	if item.Session == nil || !item.Session.Zombie {
		return
	}

	if err := sources.ReleaseClaim(s.ctx, s.bus, item.ID); err != nil {
		s.lastAction = fmt.Sprintf("release %s failed: %v", item.ID, err)
		return
	}
	s.lastAction = fmt.Sprintf("released claim on %s", item.ID)
}

// selectedID returns the ID of the currently selected row, or "" when there
// is no selection — used by Update to re-locate the row across a rebuild
// (see indexOfItemID in queuepane.go, reused as-is).
func (s *SessionsPane) selectedID() string {
	if s.cursor < 0 || s.cursor >= len(s.sessions) {
		return ""
	}
	return s.sessions[s.cursor].ID
}
