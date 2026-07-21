// See types.go for the package-level contract. This file implements
// Interleave — see openspec/changes/wavetui-memory-timeline/tasks.md [3.1]
// and design.md § Interleaved rendering and precision-aware ordering.
package timeline

import (
	"sort"
	"time"
)

// DateGroup is one calendar day's worth of merged Entry values, ordered per
// design.md § Interleaved rendering: chronological among full-Timestamp
// entries, with every PrecisionDateOnly entry rendered as a separate
// trailing block rather than interleaved among them — see orderWithinDate
// below for why a trailing block (not some other position) is what keeps
// "never asserts a specific intra-day position... relative to a timestamped
// one" true.
type DateGroup struct {
	// Date is the UTC calendar day this group represents (year/month/day,
	// zero time-of-day, UTC). Grouping is done in UTC — not the host
	// machine's local zone — so Interleave's output is deterministic
	// regardless of where wavetui runs, even though the three sources feed
	// it entries recorded in a mix of original zones (bd's
	// interactions.jsonl timestamps, git's %aI author-zone dates,
	// journal.md's zone-less dated headings).
	Date    time.Time
	Entries []Entry
}

// Interleave merges any number of sources' Entry slices (typically
// BeadsHistorySource/OpenSpecArchiveSource/MemoryHistorySource's Query
// results, though Interleave itself is source-agnostic — it only ever reads
// Entry.Time/Precision) into one chronologically ordered, date-grouped list.
//
// Per design.md § Interleaved rendering: entries are sorted by date first;
// within a single date, full-Timestamp entries sort chronologically among
// themselves, but the merge NEVER interleaves a PrecisionDateOnly entry into
// that chronological sequence, and never asserts a specific intra-day
// position for it relative to a timestamped entry sharing the same date —
// both precisions render together as one same-day DateGroup with no implied
// cross-precision ordering.
func Interleave(entries ...[]Entry) []DateGroup {
	byDate := make(map[time.Time][]Entry)
	var dates []time.Time

	for _, es := range entries {
		for _, e := range es {
			key := dateKey(e.Time)
			if _, seen := byDate[key]; !seen {
				dates = append(dates, key)
			}
			byDate[key] = append(byDate[key], e)
		}
	}

	sort.Slice(dates, func(i, j int) bool { return dates[i].Before(dates[j]) })

	groups := make([]DateGroup, 0, len(dates))
	for _, d := range dates {
		groups = append(groups, DateGroup{Date: d, Entries: orderWithinDate(byDate[d])})
	}
	return groups
}

// dateKey truncates t to its UTC calendar day — the grouping key Interleave
// sorts and buckets entries on.
func dateKey(t time.Time) time.Time {
	u := t.UTC()
	y, m, d := u.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// orderWithinDate implements design.md's precision-aware ordering for one
// date's entries: every PrecisionTimestamp entry, stably sorted
// chronologically, followed by every PrecisionDateOnly entry in the order
// Interleave encountered them. There is no meaningful intra-day ordering
// among date-only entries to sort by, and trailing them as their own
// unordered block — rather than interleaving them by, say, comparing against
// midnight — is what keeps the "never asserts a specific intra-day
// position... relative to a timestamped one" invariant true: a date-only
// entry never ends up positioned between two timestamped entries as if it
// were known to fall chronologically between them.
//
// The timestamped block uses a stable sort: two entries at the exact same
// instant (e.g. two SourceBead rows sharing one interactions.jsonl batch
// write) keep their original relative order rather than being shuffled.
func orderWithinDate(entries []Entry) []Entry {
	timestamped := make([]Entry, 0, len(entries))
	dateOnly := make([]Entry, 0, len(entries))
	for _, e := range entries {
		if e.Precision == PrecisionDateOnly {
			dateOnly = append(dateOnly, e)
		} else {
			timestamped = append(timestamped, e)
		}
	}
	sort.SliceStable(timestamped, func(i, j int) bool { return timestamped[i].Time.Before(timestamped[j].Time) })

	out := make([]Entry, 0, len(entries))
	out = append(out, timestamped...)
	out = append(out, dateOnly...)
	return out
}
