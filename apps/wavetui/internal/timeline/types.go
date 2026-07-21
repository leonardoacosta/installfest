// Package timeline implements the per-item, on-demand history sources for
// wavetui-memory-timeline: BeadsHistorySource (this batch), OpenSpecArchiveSource
// (this batch), and MemoryHistorySource (a later batch), merged by a later
// batch's merge.Interleave into a date-grouped, source-tagged Entry list.
// See openspec/changes/wavetui-memory-timeline/design.md.
//
// Hard boundary (design.md § Hard boundary: render, never distill): every
// source in this package is strictly read-only. No Query method takes a
// mutation-capable parameter, and none of them ever open
// .beads/interactions.jsonl, a memory/journal file, or
// openspec/changes/archive/ in a write mode — they render existing content
// verbatim (or, for a SourceDistilled entry, a mechanical diff-header
// extraction), never summarizing or rewriting it.
package timeline

import "time"

// Source identifies which of the three timeline sources produced an Entry.
type Source string

const (
	// SourceBead is a bead-lifecycle Entry produced by BeadsHistorySource.
	SourceBead Source = "bead"
	// SourceArchive is an OpenSpec archive-milestone Entry produced by
	// OpenSpecArchiveSource.
	SourceArchive Source = "archive"
	// SourceJournal is a first-person dated journal.md Entry (a later
	// batch's MemoryHistorySource journal-preferred path).
	SourceJournal Source = "journal"
	// SourceDistilled is a git-log-reconstructed Entry (a later batch's
	// MemoryHistorySource git-log-fallback path) — a diff-header-derived
	// approximation, rendered visually distinct from a first-person
	// SourceJournal entry.
	SourceDistilled Source = "distilled"
)

// Precision distinguishes an Entry whose Time is known to full clock
// resolution from one that is only known to the day. See design.md §
// Interleaved rendering: the merge step sorts by date first, and within a
// single date never asserts an intra-day ordering between a
// PrecisionDateOnly entry and a PrecisionTimestamp entry — they render as
// one same-day group with no implied ordering between them.
type Precision int

const (
	PrecisionTimestamp Precision = iota
	PrecisionDateOnly
)

// MatchConfidence records how confidently an Entry has been associated
// with a specific bead ID — see design.md § Journal-to-bead matching.
//
// MatchConfidenceNone covers two distinct situations that render
// identically (no confidence marker, per design.md's "renders unmatched,
// bucketed by date only" wording): a SourceBead/SourceArchive Entry, which
// is already self-identified and was never fuzzy-matched to anything
// (BeadID is simply the bead the row already pertains to); and a
// SourceJournal/SourceDistilled Entry for which matching was attempted and
// found nothing.
type MatchConfidence int

const (
	MatchConfidenceNone MatchConfidence = iota
	// MatchConfidenceTentative is a timestamp-proximity fuzzy match —
	// design.md requires this render visually tentative (dimmed text plus
	// a "?" marker), never asserted as certain.
	MatchConfidenceTentative
	// MatchConfidenceConfident is an inline bead-ID reference match —
	// renders at full confidence, no visual hedge.
	MatchConfidenceConfident
)

// Entry is one row in the merged timeline. Shape is taken from design.md §
// Interleaved rendering and § Journal-to-bead matching.
type Entry struct {
	// Source identifies which of the three sources produced this entry.
	Source Source
	// Time is the entry's timestamp; Precision records whether it is known
	// to full clock resolution or only to the day.
	Time      time.Time
	Precision Precision
	// Text is the entry's rendered content — verbatim source content (a
	// bead's own close reason, an archive-landing label), or, for
	// Source == SourceDistilled, a mechanical diff-header extraction.
	// Never a generated summary — see the package doc's Hard boundary.
	Text string
	// BeadID is the bead this entry pertains to. For a SourceBead entry,
	// it is the bead the interactions.jsonl row was recorded against
	// (which, for an epic/feature query spanning child IDs, may differ
	// from the originally-queried item). For a SourceJournal/
	// SourceDistilled entry, it is the bead journal-to-bead matching
	// associated the entry with, if any. Empty when not applicable.
	BeadID string
	// Match records how confidently BeadID was matched to this entry.
	Match MatchConfidence
}

// Availability distinguishes an empty Result because a source's backing
// file/directory genuinely does not exist (Unavailable — the caller
// renders a per-lane "unavailable" badge) from an empty Result because the
// query ran fine and simply found nothing to report (Available with zero
// Entries — the caller renders an empty state, not a badge). This is the
// same badge-vs-empty distinction design.md draws per source in its
// "missing file/directory returns an unavailable badge state rather than
// an error" / "no match returns an empty result, not an error" wording.
type Availability int

const (
	Available Availability = iota
	Unavailable
)

// Result is what every source's Query method returns.
type Result struct {
	Entries      []Entry
	Availability Availability
}
