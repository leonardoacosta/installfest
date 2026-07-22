// See openspec/changes/wavetui-memory-timeline/tasks.md [3.3] and design.md
// § Pane implementation. MemoryTimelinePane implements wavetui-core's Pane
// interface and additionally implements SelectionAware/TimelineAware/
// Sizeable (see root.go) — the extra, Pane-interface-external hooks Root
// wires it through, the same pattern QueuePane.HandleKey/SelectedItem and
// DetailPane.SetSelected already established.
package ui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/leonardoacosta/installfest/apps/wavetui/internal/store"
	"github.com/leonardoacosta/installfest/apps/wavetui/internal/timeline"
)

// defaultMemoryTimelineWidth sizes the pane before the first real
// tea.WindowSizeMsg arrives — Root.layout() calls SetSize once it does (see
// root.go), mirroring QueuePane's own defaultQueueWidth/Height precedent.
const defaultMemoryTimelineWidth = 96

// MemoryTimelinePane renders the merged, date-grouped timeline for
// whichever item QueuePane currently has selected — see design.md §
// On-demand querying and § Pane implementation. It does NOT react to
// store.Snapshot for its own content (design.md: "does not react to
// SnapshotMsg for its own content... queries on-demand"); Update is a
// required-by-interface no-op, and real content arrives exclusively via
// SetSelected (clearing stale content the instant the selection moves) and
// SetTimeline (the debounced query result for the CURRENT selection).
type MemoryTimelinePane struct {
	hasSelection bool
	itemID       string

	groups             []timeline.DateGroup
	beadUnavailable    bool
	archiveUnavailable bool
	memoryUnavailable  bool

	width  int
	height int // 0 means unbounded — see View()'s clipping
}

// NewMemoryTimelinePane constructs an empty MemoryTimelinePane (no
// selection, no timeline yet).
func NewMemoryTimelinePane() *MemoryTimelinePane {
	return &MemoryTimelinePane{width: defaultMemoryTimelineWidth}
}

// Update implements Pane. MemoryTimelinePane has no use for a generic
// Snapshot — its content is on-demand, per-item history delivered via
// SetTimeline, never derived from the queue's current item set — so this is
// a pure no-op that satisfies the interface without pretending a Snapshot
// carries information this pane needs.
func (m *MemoryTimelinePane) Update(store.Snapshot) Pane { return m }

// View implements Pane.
func (m *MemoryTimelinePane) View() string {
	width := m.width
	if width <= 0 {
		width = defaultMemoryTimelineWidth
	}

	if !m.hasSelection {
		return lipgloss.NewStyle().Width(width).Faint(true).Render("Memory timeline: no item selected.")
	}

	var lines []string
	lines = append(lines, lipgloss.NewStyle().Bold(true).Render("Memory timeline"))

	if badges := m.unavailableBadges(); badges != "" {
		lines = append(lines, badges)
	}

	// Empty state (design.md § Pane implementation: "when no item is
	// selected, or the selected item has no history in any of the three
	// lanes yet, it renders an empty state rather than a badge — empty is
	// not an error"). Deliberately independent of the unavailable badges
	// above: a lane can be genuinely unavailable AND the other lanes can
	// still have zero entries, in which case both the badge and this empty
	// state render together.
	if len(m.groups) == 0 {
		lines = append(lines, lipgloss.NewStyle().Faint(true).Render("No history for this item yet."))
		return lipgloss.NewStyle().Width(width).Render(strings.Join(lines, "\n"))
	}

	for _, g := range m.groups {
		lines = append(lines, lipgloss.NewStyle().Bold(true).Render(g.Date.Format("2006-01-02")))
		for _, e := range g.Entries {
			lines = append(lines, renderTimelineEntry(e))
		}
	}

	lines = m.clipLines(lines)
	return lipgloss.NewStyle().Width(width).Render(strings.Join(lines, "\n"))
}

// clipLines caps rendered content to m.height (Root.layout's SetSize
// budget), reserving the last line for a "N more…" indicator when clipped —
// closes the real overflow bug found live (task 3.4's evidence run): a busy
// item's full entry list otherwise renders past the fixed row budget
// layout() reserves for this pane, pushing content off the bottom of a real
// terminal instead of merely truncating it visibly. m.height <= 0 (no
// SetSize call yet — e.g. before the first tea.WindowSizeMsg, or a bare
// NewMemoryTimelinePane in a test) means unbounded, unchanged.
func (m *MemoryTimelinePane) clipLines(lines []string) []string {
	if m.height <= 0 || len(lines) <= m.height {
		return lines
	}
	kept := lines[:m.height-1]
	hidden := len(lines) - len(kept)
	return append(kept, lipgloss.NewStyle().Faint(true).Render(fmt.Sprintf("… %d more line(s)", hidden)))
}

// Focusable implements Pane. MemoryTimelinePane joins the focus ring
// (design.md § Pane implementation: "attaches to the existing focus ring by
// appending to the root model's pane slice").
func (m *MemoryTimelinePane) Focusable() bool { return true }

// SetSize implements the Sizeable optional interface (root.go) — mirrors
// QueuePane.SetSize's precedent for Root's own tea.WindowSizeMsg-driven
// layout pass. height feeds View()'s clipLines budget (see clipLines) —
// this is what actually closes the real off-screen-overflow bug found
// running this pane against a real pty, not merely cosmetic.
func (m *MemoryTimelinePane) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// SetSelected implements the SelectionAware optional interface (root.go).
// Root's notifySelection calls this on EVERY applied Snapshot, not only on
// an actual cursor move (design.md's own selection-threading mechanism is
// reused as-is, and DetailPane's existing use of it re-syncs unconditionally
// on every call since DetailPane's own fields are cheap to recompute from
// the live item every time). A real selection CHANGE (different item ID, or
// a transition to/from "nothing selected") clears the previously-rendered
// timeline immediately, rather than leaving the PREVIOUS item's history on
// screen while the debounced query for the new item is still in flight —
// showing stale content under the wrong item's selection would be
// misleading, not merely momentarily out of date. A repeat call for the
// SAME item (e.g. a Snapshot refresh from unrelated bd/openspec churn that
// leaves QueuePane's cursor exactly where it was) is a no-op: found live
// during task 3.4's pty verification run against this repo's own actively
// churning .beads/ state — without this guard, every routine re-snapshot
// wiped the just-rendered timeline back to "no history yet" even though the
// selection never moved, which is a real, user-visible flicker bug, not a
// hypothetical one.
func (m *MemoryTimelinePane) SetSelected(item store.Item, ok bool) {
	newID := ""
	if ok {
		newID = item.ID
	}
	if ok == m.hasSelection && newID == m.itemID {
		return
	}

	m.hasSelection = ok
	m.itemID = newID
	m.groups = nil
	m.beadUnavailable = false
	m.archiveUnavailable = false
	m.memoryUnavailable = false
}

// SetTimeline implements the TimelineAware optional interface (root.go). A
// result whose ItemID no longer matches the currently-selected item is
// discarded: the selection moved on again before this (already-superseded)
// query finished, and rendering it would flash stale content for the wrong
// item — see timeline_dispatch.go's TimelineMsg doc comment for the
// debounce-generation guard this is a second, cheap line of defense against.
func (m *MemoryTimelinePane) SetTimeline(msg TimelineMsg) {
	if !msg.HasSelection || !m.hasSelection || msg.ItemID != m.itemID {
		return
	}
	m.groups = msg.Groups
	m.beadUnavailable = msg.BeadUnavailable
	m.archiveUnavailable = msg.ArchiveUnavailable
	m.memoryUnavailable = msg.MemoryUnavailable
}

// unavailableBadges renders one badge per genuinely-unavailable lane,
// independent of the other two — design.md § Pane implementation /
// tasks.md [3.3]: "per-lane 'unavailable' badges independent of the other
// two lanes." Returns "" when every lane is fine, so View() can decide
// whether to add a separating newline without an empty-string special case.
func (m *MemoryTimelinePane) unavailableBadges() string {
	var parts []string
	if m.beadUnavailable {
		parts = append(parts, "bead lifecycle unavailable")
	}
	if m.archiveUnavailable {
		parts = append(parts, "archive unavailable")
	}
	if m.memoryUnavailable {
		parts = append(parts, "memory unavailable")
	}
	if len(parts) == 0 {
		return ""
	}
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("203")).Render(strings.Join(parts, "  "))
}

// renderTimelineEntry renders one Entry: an optional time-of-day prefix, its
// source tag (source=distilled visually labeled "distilled change", per
// tasks.md [3.3]), its text, its matched BeadID when present, an optional
// trailing actor, and — for a MatchConfidenceTentative match — a dimmed
// rendering with a trailing "?" marker (design.md § Journal-to-bead
// matching: "renders visually tentative — dimmed text plus a '?' marker,
// never asserted as certain").
//
// The time-of-day prefix only appears for e.Precision == PrecisionTimestamp
// (wavetui-memory-timeline-richness proposal: an entry precise only to the
// day has no time to show). The actor suffix only appears when e.Actor is
// non-empty (archive/journal-sourced entries and bead records with no
// recorded actor carry no placeholder).
func renderTimelineEntry(e timeline.Entry) string {
	line := fmt.Sprintf("[%s] %s", sourceTag(e.Source), e.Text)
	if e.BeadID != "" {
		line += " (" + e.BeadID + ")"
	}
	if e.Actor != "" {
		line += " — " + e.Actor
	}
	if e.Precision == timeline.PrecisionTimestamp {
		line = e.Time.Format("15:04") + " " + line
	}

	if e.Match == timeline.MatchConfidenceTentative {
		return lipgloss.NewStyle().Faint(true).Render(line + " ?")
	}
	return line
}

// sourceTag renders a Source as its display tag. SourceDistilled gets the
// full "distilled change" label design.md/tasks.md [3.3] specifically
// require ("source=distilled visually labeled as a distilled change"),
// rather than the bare enum value.
func sourceTag(s timeline.Source) string {
	switch s {
	case timeline.SourceBead:
		return "bead"
	case timeline.SourceArchive:
		return "archive"
	case timeline.SourceJournal:
		return "journal"
	case timeline.SourceDistilled:
		return "distilled change"
	default:
		return string(s)
	}
}
