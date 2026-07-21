package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/leonardoacosta/installfest/apps/wavetui/internal/store"
	"github.com/leonardoacosta/installfest/apps/wavetui/internal/timeline"
)

func TestMemoryTimelinePaneFocusable(t *testing.T) {
	m := NewMemoryTimelinePane()
	if !m.Focusable() {
		t.Fatal("MemoryTimelinePane must be Focusable — it joins Root's focus ring")
	}
}

func TestMemoryTimelinePaneNoSelectionView(t *testing.T) {
	m := NewMemoryTimelinePane()
	if got := m.View(); !strings.Contains(got, "no item selected") {
		t.Fatalf("want a no-selection placeholder, got %q", got)
	}
}

// TestSetSelectedSameItemPreservesGroups is the REGRESSION test for the
// flicker bug found live during task 3.4's pty verification run: a repeat
// SetSelected call for the SAME item (e.g. a Snapshot refresh from unrelated
// .beads/ churn that leaves QueuePane's cursor exactly where it was) must NOT
// wipe an already-loaded timeline back to empty. Reverting the same-item
// guard in SetSelected (the `if ok == m.hasSelection && newID == m.itemID {
// return }` early-return) must make this test fail.
func TestSetSelectedSameItemPreservesGroups(t *testing.T) {
	m := NewMemoryTimelinePane()
	m.SetSelected(store.Item{ID: "a"}, true)
	m.SetTimeline(TimelineMsg{
		ItemID:       "a",
		HasSelection: true,
		Groups: []timeline.DateGroup{
			{Date: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), Entries: []timeline.Entry{
				{Source: timeline.SourceBead, Text: "claimed"},
			}},
		},
	})

	if len(m.groups) == 0 {
		t.Fatal("setup failed: want non-empty groups after SetTimeline, got none")
	}
	if !strings.Contains(m.View(), "claimed") {
		t.Fatalf("setup failed: want loaded content in View(), got:\n%s", m.View())
	}

	// A Snapshot refresh that leaves the selection on the SAME item calls
	// SetSelected again with an identical item/ok pair — this must be a
	// no-op for already-loaded content.
	m.SetSelected(store.Item{ID: "a"}, true)

	if len(m.groups) == 0 {
		t.Fatal("SetSelected called twice with the identical item ID cleared already-loaded groups — this is the flicker regression")
	}
	if !strings.Contains(m.View(), "claimed") {
		t.Fatalf("want loaded content to survive a same-item SetSelected call, got:\n%s", m.View())
	}
}

// TestSetSelectedDifferentItemClearsGroups is the contrasting case: the
// same-item guard above must not be so broad that it also swallows a REAL
// selection change. A genuinely different item ID (or a transition to/from
// "nothing selected") must still clear the previous item's stale timeline
// immediately, per SetSelected's own doc comment ("showing stale content
// under the wrong item's selection would be misleading").
func TestSetSelectedDifferentItemClearsGroups(t *testing.T) {
	m := NewMemoryTimelinePane()
	m.SetSelected(store.Item{ID: "a"}, true)
	m.SetTimeline(TimelineMsg{
		ItemID:       "a",
		HasSelection: true,
		Groups: []timeline.DateGroup{
			{Date: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), Entries: []timeline.Entry{
				{Source: timeline.SourceBead, Text: "claimed"},
			}},
		},
	})

	m.SetSelected(store.Item{ID: "b"}, true)

	if len(m.groups) != 0 {
		t.Fatalf("selecting a DIFFERENT item must clear the previous item's groups, got %d groups still loaded", len(m.groups))
	}
	if strings.Contains(m.View(), "claimed") {
		t.Fatalf("want the previous item's content gone from View() after a real selection change, got:\n%s", m.View())
	}
}

// TestSetTimelineDiscardsResultForSupersededSelection asserts SetTimeline's
// own second line of defense (see its doc comment): a TimelineMsg whose
// ItemID no longer matches the currently-selected item is discarded rather
// than rendered.
func TestSetTimelineDiscardsResultForSupersededSelection(t *testing.T) {
	m := NewMemoryTimelinePane()
	m.SetSelected(store.Item{ID: "b"}, true) // selection already moved on

	m.SetTimeline(TimelineMsg{
		ItemID:       "a", // stale result for the PREVIOUS selection
		HasSelection: true,
		Groups: []timeline.DateGroup{
			{Date: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), Entries: []timeline.Entry{
				{Source: timeline.SourceBead, Text: "stale entry for a"},
			}},
		},
	})

	if strings.Contains(m.View(), "stale entry for a") {
		t.Fatalf("a TimelineMsg for a superseded ItemID must be discarded, got:\n%s", m.View())
	}
}

// TestRenderTimelineEntryTentativeMatchIsDimmedWithQuestionMark is design.md
// § Journal-to-bead matching's requirement: a MatchConfidenceTentative entry
// "renders visually tentative — dimmed text plus a '?' marker, never
// asserted as certain." Compares against an otherwise-identical
// MatchConfidenceConfident entry so the assertion is about the RENDERED
// STRING differing (this codebase's Faint-style dimming convention, per
// queuepane.go's SecondClass precedent), not just the underlying field.
func TestRenderTimelineEntryTentativeMatchIsDimmedWithQuestionMark(t *testing.T) {
	base := timeline.Entry{Source: timeline.SourceJournal, Text: "did the thing", BeadID: "if-abc"}

	confident := base
	confident.Match = timeline.MatchConfidenceConfident
	confidentLine := renderTimelineEntry(confident)

	tentative := base
	tentative.Match = timeline.MatchConfidenceTentative
	tentativeLine := renderTimelineEntry(tentative)

	// tentativeLine is lipgloss-styled (ANSI escape codes wrap the whole
	// string, including a trailing reset code), so the "?" marker sits just
	// before that reset rather than at the literal end of the string — check
	// containment, not a raw suffix.
	if !strings.Contains(tentativeLine, "?") {
		t.Fatalf("want a %q marker on a tentative match, got %q", "?", tentativeLine)
	}
	if strings.Contains(confidentLine, "?") {
		t.Fatalf("a confident match must not carry the tentative %q marker, got %q", "?", confidentLine)
	}
	if tentativeLine == confidentLine {
		t.Fatalf("a tentative match rendered IDENTICALLY to a confident one — want a visually distinct (dimmed) rendering:\n%q", tentativeLine)
	}

	// Confirm the same distinction survives through the real Pane pipeline
	// (View()), not just the bare helper.
	m := NewMemoryTimelinePane()
	m.SetSelected(store.Item{ID: "a"}, true)
	m.SetTimeline(TimelineMsg{
		ItemID:       "a",
		HasSelection: true,
		Groups: []timeline.DateGroup{
			{Date: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), Entries: []timeline.Entry{tentative}},
		},
	})
	view := m.View()
	if !strings.Contains(view, "did the thing") {
		t.Fatalf("want the entry text rendered, got:\n%s", view)
	}
	if !strings.Contains(view, "?") {
		t.Fatalf("want the tentative-match marker present in the rendered view, got:\n%s", view)
	}
}

// TestSourceDistilledRendersDistilledChangeLabel is tasks.md [3.3]'s
// requirement: "source=distilled visually labeled as a distilled change" —
// not the bare enum value ("distilled").
func TestSourceDistilledRendersDistilledChangeLabel(t *testing.T) {
	m := NewMemoryTimelinePane()
	m.SetSelected(store.Item{ID: "a"}, true)
	m.SetTimeline(TimelineMsg{
		ItemID:       "a",
		HasSelection: true,
		Groups: []timeline.DateGroup{
			{Date: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), Entries: []timeline.Entry{
				{Source: timeline.SourceDistilled, Text: "reworked the merge logic"},
			}},
		},
	})

	view := m.View()
	if !strings.Contains(view, "distilled change") {
		t.Fatalf("want the %q label for a source=distilled entry, got:\n%s", "distilled change", view)
	}
	if strings.Contains(view, "[distilled]") {
		t.Fatalf("want the labeled form, not the bare enum value, got:\n%s", view)
	}
}

// TestUnavailableBadgesPerLaneIndependent is tasks.md [3.3]'s "per-lane
// 'unavailable' badges independent of the other two lanes" requirement: with
// exactly one lane (bead) unavailable and the other two (archive, memory)
// carrying real data, the rendered view must show exactly one badge and
// real content for the other two lanes.
func TestUnavailableBadgesPerLaneIndependent(t *testing.T) {
	m := NewMemoryTimelinePane()
	m.SetSelected(store.Item{ID: "a"}, true)
	m.SetTimeline(TimelineMsg{
		ItemID:             "a",
		HasSelection:       true,
		BeadUnavailable:    true,
		ArchiveUnavailable: false,
		MemoryUnavailable:  false,
		Groups: []timeline.DateGroup{
			{Date: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), Entries: []timeline.Entry{
				{Source: timeline.SourceArchive, Text: "archive lane content"},
				{Source: timeline.SourceJournal, Text: "memory lane content"},
			}},
		},
	})

	view := m.View()
	if !strings.Contains(view, "bead lifecycle unavailable") {
		t.Fatalf("want the bead-lane badge, got:\n%s", view)
	}
	if strings.Contains(view, "archive unavailable") {
		t.Fatalf("archive lane has data — must not show its unavailable badge, got:\n%s", view)
	}
	if strings.Contains(view, "memory unavailable") {
		t.Fatalf("memory lane has data — must not show its unavailable badge, got:\n%s", view)
	}
	if !strings.Contains(view, "archive lane content") || !strings.Contains(view, "memory lane content") {
		t.Fatalf("want real content rendered for the two available lanes, got:\n%s", view)
	}
}

// TestEmptyStateDistinctFromUnavailableBadges is design.md § Pane
// implementation's "when ... the selected item has no history in any of the
// three lanes yet, it renders an empty state rather than a badge — empty is
// not an error." Asserts the rendered string for a genuinely empty (but
// available) timeline differs from every unavailable-badge string.
func TestEmptyStateDistinctFromUnavailableBadges(t *testing.T) {
	m := NewMemoryTimelinePane()
	m.SetSelected(store.Item{ID: "a"}, true)
	m.SetTimeline(TimelineMsg{ItemID: "a", HasSelection: true}) // zero entries, all lanes available

	view := m.View()
	if !strings.Contains(view, "No history for this item yet.") {
		t.Fatalf("want the empty-state message, got:\n%s", view)
	}
	for _, badge := range []string{"bead lifecycle unavailable", "archive unavailable", "memory unavailable"} {
		if strings.Contains(view, badge) {
			t.Fatalf("an empty-but-available timeline must not render any unavailable badge (%q), got:\n%s", badge, view)
		}
	}
}

// TestClipLinesWithinBudgetLeavesLinesUnchanged is clipLines' no-op boundary:
// content that already fits the height budget must pass through untouched,
// with no truncation indicator appended.
func TestClipLinesWithinBudgetLeavesLinesUnchanged(t *testing.T) {
	m := &MemoryTimelinePane{height: 5}
	lines := []string{"1", "2", "3"}

	got := m.clipLines(lines)
	if len(got) != 3 {
		t.Fatalf("want 3 lines unchanged (within budget), got %d: %v", len(got), got)
	}
	for i, want := range lines {
		if got[i] != want {
			t.Fatalf("clipLines mutated an in-budget line at index %d: got %q, want %q", i, got[i], want)
		}
	}
}

// TestClipLinesUnboundedWhenHeightUnset asserts m.height<=0 (no SetSize call
// yet, e.g. a bare NewMemoryTimelinePane) means unbounded — clipLines must
// leave arbitrarily long content untouched.
func TestClipLinesUnboundedWhenHeightUnset(t *testing.T) {
	m := &MemoryTimelinePane{} // height == 0
	lines := make([]string, 100)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d", i)
	}

	got := m.clipLines(lines)
	if len(got) != 100 {
		t.Fatalf("want all 100 lines when height is unset (unbounded), got %d", len(got))
	}
}

// TestClipLinesClipsWhenExceedingBudget is the REGRESSION test for the real
// off-screen-overflow bug found running this pane against a real pty (see
// clipLines' doc comment): content longer than the height budget must be
// clipped to exactly that budget, with the final line replaced by a
// "N more line(s)" indicator — not silently allowed to overflow past
// Root.layout()'s reserved row budget.
func TestClipLinesClipsWhenExceedingBudget(t *testing.T) {
	m := &MemoryTimelinePane{height: 5}
	lines := []string{"1", "2", "3", "4", "5", "6", "7"} // 7 lines, budget is 5

	got := m.clipLines(lines)
	if len(got) != 5 {
		t.Fatalf("content longer than the height budget was not clipped to it: got %d lines, want exactly 5:\n%v", len(got), got)
	}
	for i, want := range []string{"1", "2", "3", "4"} {
		if got[i] != want {
			t.Fatalf("want the first 4 lines kept verbatim, got %q at index %d", got[i], i)
		}
	}
	if !strings.Contains(got[4], "3 more line(s)") {
		t.Fatalf("want a hidden-count indicator for the 3 clipped lines, got %q", got[4])
	}
}

// TestViewClipsContentExceedingHeightBudget is the same regression exercised
// through the full Pane pipeline (SetSize + SetTimeline + View()), proving
// clipLines is actually wired into View() and not just correct in
// isolation: a busy item's rendered view must never exceed the configured
// height.
func TestViewClipsContentExceedingHeightBudget(t *testing.T) {
	m := NewMemoryTimelinePane()
	m.SetSize(80, 5)
	m.SetSelected(store.Item{ID: "a"}, true)

	var entries []timeline.Entry
	for i := 0; i < 20; i++ {
		entries = append(entries, timeline.Entry{
			Source: timeline.SourceBead,
			Text:   fmt.Sprintf("entry number %d", i),
			Time:   time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC),
		})
	}
	m.SetTimeline(TimelineMsg{
		ItemID:       "a",
		HasSelection: true,
		Groups: []timeline.DateGroup{
			{Date: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), Entries: entries},
		},
	})

	view := m.View()
	lines := strings.Split(view, "\n")
	if len(lines) > 5 {
		t.Fatalf("View() rendered %d lines, want clipped to the configured height budget (5):\n%s", len(lines), view)
	}
	if !strings.Contains(view, "more line(s)") {
		t.Fatalf("want a clipped-content indicator when the busy item's entries exceed the height budget, got:\n%s", view)
	}
}
