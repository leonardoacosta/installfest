package timeline

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// writeInteractions writes lines (already-JSON-encoded, one per line) to
// <root>/.beads/interactions.jsonl, creating .beads/ if needed. Fixture
// helper only — never touches this repo's own real .beads/.
func writeInteractions(t *testing.T, root string, lines []string) {
	t.Helper()
	dir := filepath.Join(root, ".beads")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	content := ""
	for _, l := range lines {
		content += l + "\n"
	}
	if err := os.WriteFile(filepath.Join(dir, "interactions.jsonl"), []byte(content), 0o644); err != nil {
		t.Fatalf("write interactions.jsonl: %v", err)
	}
}

func TestBeadsHistorySource_MissingFile_Unavailable(t *testing.T) {
	root := t.TempDir() // no .beads/ at all
	s := NewBeadsHistorySource(root)

	res, err := s.Query(context.Background(), "if-abc", nil)
	if err != nil {
		t.Fatalf("Query returned error: %v", err)
	}
	if res.Availability != Unavailable {
		t.Fatalf("Availability = %v, want Unavailable", res.Availability)
	}
	if len(res.Entries) != 0 {
		t.Fatalf("Entries = %v, want empty", res.Entries)
	}
}

func TestBeadsHistorySource_RecognizedKinds(t *testing.T) {
	root := t.TempDir()
	writeInteractions(t, root, []string{
		// Recognized: status -> in_progress (claim).
		`{"id":"int-1","kind":"field_change","created_at":"2026-04-09T21:00:00Z","actor":"leo","issue_id":"if-abc","extra":{"field":"status","old_value":"open","new_value":"in_progress"}}`,
		// Recognized: status -> closed, with reason.
		`{"id":"int-2","kind":"field_change","created_at":"2026-04-09T22:00:00Z","actor":"leo","issue_id":"if-abc","extra":{"field":"status","old_value":"in_progress","new_value":"closed","reason":"shipped it"}}`,
		// Recognized: status -> closed, no reason.
		`{"id":"int-3","kind":"field_change","created_at":"2026-04-09T23:00:00Z","actor":"leo","issue_id":"if-abc","extra":{"field":"status","old_value":"open","new_value":"closed"}}`,
		// field_change but not a status field (priority) -> generic activity, not dropped.
		`{"id":"int-4","kind":"field_change","created_at":"2026-04-10T00:00:00Z","actor":"leo","issue_id":"if-abc","extra":{"field":"priority","old_value":"2","new_value":"3"}}`,
		// field_change status, an unrecognized transition (reopen) -> generic activity.
		`{"id":"int-5","kind":"field_change","created_at":"2026-04-10T01:00:00Z","actor":"leo","issue_id":"if-abc","extra":{"field":"status","old_value":"closed","new_value":"open"}}`,
		// Wholly unrecognized kind -> generic activity, not dropped.
		`{"id":"int-6","kind":"llm_call","created_at":"2026-04-10T02:00:00Z","actor":"leo","issue_id":"if-abc","extra":{}}`,
		// A different bead entirely -> filtered out.
		`{"id":"int-7","kind":"field_change","created_at":"2026-04-10T03:00:00Z","actor":"leo","issue_id":"if-xyz","extra":{"field":"status","old_value":"open","new_value":"closed"}}`,
	})

	s := NewBeadsHistorySource(root)
	res, err := s.Query(context.Background(), "if-abc", nil)
	if err != nil {
		t.Fatalf("Query returned error: %v", err)
	}
	if res.Availability != Available {
		t.Fatalf("Availability = %v, want Available", res.Availability)
	}
	if len(res.Entries) != 6 {
		t.Fatalf("Entries = %d, want 6 (int-7 filtered out): %+v", len(res.Entries), res.Entries)
	}

	want := []string{
		"claimed",
		"closed: shipped it",
		"closed",
		"priority: 2 -> 3",
		"status: closed -> open",
		"activity: llm_call",
	}
	for i, e := range res.Entries {
		if e.Source != SourceBead {
			t.Errorf("Entries[%d].Source = %q, want %q", i, e.Source, SourceBead)
		}
		if e.BeadID != "if-abc" {
			t.Errorf("Entries[%d].BeadID = %q, want if-abc", i, e.BeadID)
		}
		if e.Precision != PrecisionTimestamp {
			t.Errorf("Entries[%d].Precision = %v, want PrecisionTimestamp", i, e.Precision)
		}
		if e.Time.IsZero() {
			t.Errorf("Entries[%d].Time is zero, want parsed", i)
		}
		if e.Text != want[i] {
			t.Errorf("Entries[%d].Text = %q, want %q", i, e.Text, want[i])
		}
	}
}

func TestBeadsHistorySource_ChildFiltering(t *testing.T) {
	root := t.TempDir()
	writeInteractions(t, root, []string{
		`{"id":"int-1","kind":"field_change","created_at":"2026-04-09T21:00:00Z","actor":"leo","issue_id":"if-epic","extra":{"field":"status","old_value":"open","new_value":"in_progress"}}`,
		`{"id":"int-2","kind":"field_change","created_at":"2026-04-09T22:00:00Z","actor":"leo","issue_id":"if-child1","extra":{"field":"status","old_value":"open","new_value":"closed"}}`,
		`{"id":"int-3","kind":"field_change","created_at":"2026-04-09T23:00:00Z","actor":"leo","issue_id":"if-child2","extra":{"field":"status","old_value":"open","new_value":"in_progress"}}`,
		`{"id":"int-4","kind":"field_change","created_at":"2026-04-10T00:00:00Z","actor":"leo","issue_id":"if-unrelated","extra":{"field":"status","old_value":"open","new_value":"closed"}}`,
	})

	s := NewBeadsHistorySource(root)
	res, err := s.Query(context.Background(), "if-epic", []string{"if-child1", "if-child2"})
	if err != nil {
		t.Fatalf("Query returned error: %v", err)
	}
	if len(res.Entries) != 3 {
		t.Fatalf("Entries = %d, want 3 (if-unrelated filtered out): %+v", len(res.Entries), res.Entries)
	}
	gotIDs := map[string]bool{}
	for _, e := range res.Entries {
		gotIDs[e.BeadID] = true
	}
	for _, want := range []string{"if-epic", "if-child1", "if-child2"} {
		if !gotIDs[want] {
			t.Errorf("missing entry for %s in %+v", want, res.Entries)
		}
	}
	if gotIDs["if-unrelated"] {
		t.Errorf("if-unrelated should have been filtered out: %+v", res.Entries)
	}
}

func TestBeadsHistorySource_MalformedLineSkipped(t *testing.T) {
	root := t.TempDir()
	writeInteractions(t, root, []string{
		`not-json-at-all`,
		``, // blank line, also tolerated
		`{"id":"int-1","kind":"field_change","created_at":"2026-04-09T21:00:00Z","actor":"leo","issue_id":"if-abc","extra":{"field":"status","old_value":"open","new_value":"closed"}}`,
	})

	s := NewBeadsHistorySource(root)
	res, err := s.Query(context.Background(), "if-abc", nil)
	if err != nil {
		t.Fatalf("Query returned error: %v", err)
	}
	if len(res.Entries) != 1 {
		t.Fatalf("Entries = %d, want 1 (malformed/blank lines skipped): %+v", len(res.Entries), res.Entries)
	}
}

func TestBeadsHistorySource_ContextCancelled(t *testing.T) {
	root := t.TempDir()
	writeInteractions(t, root, []string{
		`{"id":"int-1","kind":"field_change","created_at":"2026-04-09T21:00:00Z","actor":"leo","issue_id":"if-abc","extra":{"field":"status","old_value":"open","new_value":"closed"}}`,
	})

	s := NewBeadsHistorySource(root)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := s.Query(ctx, "if-abc", nil)
	if err == nil {
		t.Fatalf("Query with cancelled context returned nil error, want context.Canceled")
	}
}
