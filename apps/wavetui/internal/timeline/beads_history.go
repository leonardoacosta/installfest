// See types.go for the package-level contract. This file implements
// BeadsHistorySource — see openspec/changes/wavetui-memory-timeline/
// tasks.md [1.2] and design.md § Bead lifecycle source.
package timeline

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// BeadsHistorySource reads bd's own append-only .beads/interactions.jsonl
// audit log directly — design.md notes this file is a stable, documented
// export format upstream explicitly intends to be read for auditing
// ("why did the agent do that?"), unlike .beads/*.db (bd's unstable
// internal schema, which wavetui-core's BeadsSource correctly avoids
// parsing directly). BeadsHistorySource never watches the file (no
// fsnotify) — it is queried on-demand, once per selection change, per
// design.md § On-demand querying.
type BeadsHistorySource struct {
	root string // project root; .beads/interactions.jsonl is expected directly under it
}

// NewBeadsHistorySource constructs a BeadsHistorySource rooted at root (a
// project root — typically the cwd wavetui was launched from).
func NewBeadsHistorySource(root string) *BeadsHistorySource {
	return &BeadsHistorySource{root: root}
}

func (s *BeadsHistorySource) interactionsPath() string {
	return filepath.Join(s.root, ".beads", "interactions.jsonl")
}

// interactionRecord is the subset of one .beads/interactions.jsonl line's
// fields this source understands — tolerant-decode, matching
// wavetui-core's house style (sources/beads.go's beadRecord): unknown
// top-level fields are silently ignored by encoding/json, and a field this
// struct declares that a given line happens to omit just decodes to its
// zero value. Confirmed against this repo's own live interactions.jsonl
// (620 real lines, all kind=="field_change") plus `bd audit record --help`
// for the kind vocabulary bd's CLI itself documents (llm_call, tool_call,
// label — none of which this repo's history happens to contain, but which
// must not be dropped either, per the unrecognized-kind fallback below).
type interactionRecord struct {
	Kind      string           `json:"kind"`
	CreatedAt string           `json:"created_at"`
	IssueID   string           `json:"issue_id"`
	Extra     interactionExtra `json:"extra"`
}

// interactionExtra is the recognized subset of an interaction row's
// "extra" object. Field/OldValue/NewValue/Reason are the shape this repo's
// real field_change rows carry (a status or priority transition, with an
// optional close reason). Text/Comment are speculative fields for a
// future/differently-configured bd install's kind=="comment" rows — no
// real example of that shape exists in this repo's own history, so both
// are read tolerantly (missing -> zero value) and neither being present
// falls through to the generic-activity text, never an error.
type interactionExtra struct {
	Field    string `json:"field"`
	OldValue string `json:"old_value"`
	NewValue string `json:"new_value"`
	Reason   string `json:"reason"`
	Text     string `json:"text"`
	Comment  string `json:"comment"`
}

// Query reads .beads/interactions.jsonl line-by-line and returns one Entry
// per row whose issue_id matches itemID or any ID in childIDs (the
// selected item's children, for an epic/feature selection — the caller is
// expected to have already resolved that traversal via whatever
// parent/child mechanism wavetui-core's BeadsSource exposes for the
// current snapshot; this source does not re-derive bead hierarchy itself).
//
// A missing interactions.jsonl returns Result{Availability: Unavailable}
// rather than an error — design.md: "A missing .beads/interactions.jsonl
// ... degrades to an unavailable badge for the bead-lifecycle lane only."
// Any other read/scan failure is a genuine error.
func (s *BeadsHistorySource) Query(ctx context.Context, itemID string, childIDs []string) (Result, error) {
	path := s.interactionsPath()
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Result{Availability: Unavailable}, nil
		}
		return Result{}, fmt.Errorf("beads history: open %s: %w", path, err)
	}
	defer f.Close()

	want := make(map[string]bool, 1+len(childIDs))
	want[itemID] = true
	for _, id := range childIDs {
		want[id] = true
	}

	var entries []Entry
	scanner := bufio.NewScanner(f)
	// A close/field_change reason can be a long free-text sentence; the
	// default 64KB bufio.Scanner token limit is comfortably above any
	// real line seen in this repo's own interactions.jsonl, but this
	// raises the ceiling to 1MB defensively rather than have one
	// unusually long line silently truncate the whole scan.
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)

	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var rec interactionRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			// Tolerant-decode: one malformed line is skipped, never fails
			// the whole scan — the audit log is append-only and a partial
			// write mid-line is a plausible real-world shape.
			continue
		}
		if !want[rec.IssueID] {
			continue
		}
		entries = append(entries, beadEntryFrom(rec))
	}
	if err := scanner.Err(); err != nil {
		return Result{}, fmt.Errorf("beads history: scan %s: %w", path, err)
	}

	return Result{Entries: entries, Availability: Available}, nil
}

// beadEntryFrom maps one decoded interaction row to an Entry — design.md §
// Bead lifecycle source: "Recognized interaction kinds map to timeline
// entries: creation, claim, close (with reason text when present), and
// comment/decision-resolution rows. An unrecognized interaction kind value
// is rendered as a generic 'activity' entry rather than dropped."
func beadEntryFrom(rec interactionRecord) Entry {
	// Tolerant: an unparsable or absent CreatedAt just leaves the zero
	// time.Time — no error, no dropped entry (matches sources/beads.go's
	// toItem CreatedAt handling).
	t, _ := time.Parse(time.RFC3339Nano, rec.CreatedAt)
	return Entry{
		Source:    SourceBead,
		Time:      t,
		Precision: PrecisionTimestamp,
		Text:      beadEntryText(rec),
		BeadID:    rec.IssueID,
	}
}

// beadEntryText derives the recognized-kind text for one interaction row,
// falling back to a generic "activity" description for anything it does
// not recognize. This repo's real interactions.jsonl only ever carries
// kind=="field_change" — bd's documented alternative kinds (llm_call,
// tool_call, label, plus a speculative "created"/"comment") are handled
// here so a future/differently-configured bd install's rows are never
// silently dropped, only degraded to the generic fallback.
func beadEntryText(rec interactionRecord) string {
	switch rec.Kind {
	case "field_change":
		return fieldChangeText(rec.Extra)
	case "created":
		return "created"
	case "comment":
		return commentText(rec.Extra)
	default:
		return genericActivityText(rec)
	}
}

// fieldChangeText handles the one kind this repo's real audit log
// actually contains. Recognized transitions: any field=="status" row
// landing on "closed" is a close (with reason, when present); a
// field=="status" row moving open -> in_progress is a claim. Every other
// field_change (a different status transition such as a reopen or
// defer, or a non-status field like "priority") falls through to the
// generic activity description rather than being dropped.
func fieldChangeText(extra interactionExtra) string {
	if extra.Field != "status" {
		return activityText(extra)
	}
	switch {
	case extra.NewValue == "closed":
		if extra.Reason != "" {
			return "closed: " + extra.Reason
		}
		return "closed"
	case extra.OldValue == "open" && extra.NewValue == "in_progress":
		return "claimed"
	default:
		return activityText(extra)
	}
}

func commentText(extra interactionExtra) string {
	switch {
	case extra.Text != "":
		return "comment: " + extra.Text
	case extra.Comment != "":
		return "comment: " + extra.Comment
	default:
		return "comment"
	}
}

func activityText(extra interactionExtra) string {
	if extra.Field == "" {
		return "activity"
	}
	return fmt.Sprintf("%s: %s -> %s", extra.Field, displayOrUnknown(extra.OldValue), displayOrUnknown(extra.NewValue))
}

func genericActivityText(rec interactionRecord) string {
	if rec.Extra.Field != "" {
		return activityText(rec.Extra)
	}
	if rec.Kind != "" {
		return "activity: " + rec.Kind
	}
	return "activity"
}

func displayOrUnknown(v string) string {
	if v == "" {
		return "?"
	}
	return v
}
