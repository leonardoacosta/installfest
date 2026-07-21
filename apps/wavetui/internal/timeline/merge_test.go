package timeline

import (
	"testing"
	"time"
)

func mergeMustParse(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("bad test timestamp %q: %v", s, err)
	}
	return ts
}

// TestInterleave_DateGrouping verifies entries recorded on different
// calendar days land in separate DateGroups, each keyed to that entry's own
// UTC calendar day.
func TestInterleave_DateGrouping(t *testing.T) {
	a := []Entry{{Source: SourceBead, Time: mergeMustParse(t, "2026-05-01T10:00:00Z"), Text: "day one"}}
	b := []Entry{{Source: SourceArchive, Time: mergeMustParse(t, "2026-05-02T10:00:00Z"), Text: "day two"}}

	groups := Interleave(a, b)
	if len(groups) != 2 {
		t.Fatalf("groups = %+v, want exactly 2", groups)
	}
	wantDates := []string{"2026-05-01", "2026-05-02"}
	for i, want := range wantDates {
		gotDate := groups[i].Date.Format("2006-01-02")
		if gotDate != want {
			t.Errorf("groups[%d].Date = %q, want %q", i, gotDate, want)
		}
		if len(groups[i].Entries) != 1 {
			t.Errorf("groups[%d].Entries = %+v, want exactly 1", i, groups[i].Entries)
		}
	}
}

// TestInterleave_ChronologicalOrderingWithinDateGroup verifies that
// full-Timestamp entries sharing one calendar day are sorted chronologically
// among themselves, regardless of which source slice they arrived in or
// their original relative order across slices.
func TestInterleave_ChronologicalOrderingWithinDateGroup(t *testing.T) {
	beadEntries := []Entry{
		{Source: SourceBead, Time: mergeMustParse(t, "2026-05-01T15:00:00Z"), Text: "bead-afternoon"},
		{Source: SourceBead, Time: mergeMustParse(t, "2026-05-01T09:00:00Z"), Text: "bead-morning"},
	}
	archiveEntries := []Entry{
		{Source: SourceArchive, Time: mergeMustParse(t, "2026-05-01T12:00:00Z"), Text: "archive-noon"},
	}

	groups := Interleave(beadEntries, archiveEntries)
	if len(groups) != 1 {
		t.Fatalf("groups = %+v, want exactly 1 date group", groups)
	}
	got := groups[0].Entries
	if len(got) != 3 {
		t.Fatalf("Entries = %+v, want exactly 3", got)
	}
	wantOrder := []string{"bead-morning", "archive-noon", "bead-afternoon"}
	for i, want := range wantOrder {
		if got[i].Text != want {
			t.Errorf("Entries[%d].Text = %q, want %q (chronological order across sources)", i, got[i].Text, want)
		}
	}
}

// TestInterleave_NoFabricatedIntraDayOrdering_MixedPrecision verifies the
// design.md invariant: a PrecisionDateOnly entry sharing a calendar day with
// PrecisionTimestamp entries is never interleaved among them by inferred
// position — it always renders trailing the full chronological timestamped
// block, in encounter order, never asserted as falling "between" two
// timestamped entries.
func TestInterleave_NoFabricatedIntraDayOrdering_MixedPrecision(t *testing.T) {
	sameDay := "2026-05-01"
	timestamped := []Entry{
		{Source: SourceBead, Time: mergeMustParse(t, sameDay+"T18:00:00Z"), Precision: PrecisionTimestamp, Text: "ts-evening"},
		{Source: SourceBead, Time: mergeMustParse(t, sameDay+"T06:00:00Z"), Precision: PrecisionTimestamp, Text: "ts-morning"},
	}
	// Two date-only entries, deliberately fed in a specific order — their
	// zero-time-of-day midnight instant would sort BEFORE both timestamped
	// entries above if a naive single chronological sort were used instead
	// of the trailing-block rule this test guards.
	dateOnly := []Entry{
		{Source: SourceJournal, Time: mergeMustParse(t, sameDay+"T00:00:00Z"), Precision: PrecisionDateOnly, Text: "journal-first"},
		{Source: SourceJournal, Time: mergeMustParse(t, sameDay+"T00:00:00Z"), Precision: PrecisionDateOnly, Text: "journal-second"},
	}

	groups := Interleave(timestamped, dateOnly)
	if len(groups) != 1 {
		t.Fatalf("groups = %+v, want exactly 1 date group", groups)
	}
	got := groups[0].Entries
	if len(got) != 4 {
		t.Fatalf("Entries = %+v, want exactly 4", got)
	}

	// The two timestamped entries come first, chronologically ordered
	// (never fabricating a position for the date-only entries between
	// them)...
	if got[0].Text != "ts-morning" || got[1].Text != "ts-evening" {
		t.Errorf("timestamped block = [%q, %q], want [ts-morning, ts-evening]", got[0].Text, got[1].Text)
	}
	// ...followed by the date-only entries as a trailing block, in the
	// order Interleave encountered them (not re-sorted against each other
	// or against the timestamped block).
	if got[2].Precision != PrecisionDateOnly || got[3].Precision != PrecisionDateOnly {
		t.Fatalf("Entries[2:] = %+v, want both PrecisionDateOnly (trailing block)", got[2:])
	}
	if got[2].Text != "journal-first" || got[3].Text != "journal-second" {
		t.Errorf("date-only block = [%q, %q], want [journal-first, journal-second] (encounter order preserved)", got[2].Text, got[3].Text)
	}
}

// TestInterleave_MultiDateChronologicalOrdering verifies DateGroups
// themselves are returned in ascending calendar-day order even when fed
// out-of-order across multiple source slices spanning more than two days.
func TestInterleave_MultiDateChronologicalOrdering(t *testing.T) {
	sourceA := []Entry{
		{Source: SourceBead, Time: mergeMustParse(t, "2026-05-03T10:00:00Z"), Text: "day3"},
		{Source: SourceBead, Time: mergeMustParse(t, "2026-05-01T10:00:00Z"), Text: "day1"},
	}
	sourceB := []Entry{
		{Source: SourceArchive, Time: mergeMustParse(t, "2026-05-02T10:00:00Z"), Text: "day2"},
	}

	groups := Interleave(sourceA, sourceB)
	if len(groups) != 3 {
		t.Fatalf("groups = %+v, want exactly 3 date groups", groups)
	}
	wantDates := []string{"2026-05-01", "2026-05-02", "2026-05-03"}
	for i, want := range wantDates {
		gotDate := groups[i].Date.Format("2006-01-02")
		if gotDate != want {
			t.Errorf("groups[%d].Date = %q, want %q (ascending order)", i, gotDate, want)
		}
	}
}

// TestInterleave_NoEntries verifies the degenerate empty-input case returns
// an empty (not nil-panicking) group slice.
func TestInterleave_NoEntries(t *testing.T) {
	groups := Interleave()
	if len(groups) != 0 {
		t.Fatalf("groups = %+v, want empty", groups)
	}
}
