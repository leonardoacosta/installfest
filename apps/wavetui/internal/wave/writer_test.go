package wave

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/leonardoacosta/installfest/apps/wavetui/internal/store"
)

func TestBuildFileProjectsItemsAndConflicts(t *testing.T) {
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	items := []store.Item{
		{ID: "a", Kind: store.KindBead, Title: "Alpha", FanOutScore: 3, TouchedFiles: []string{"x.go"}},
		{ID: "b", Kind: store.KindProposal, Title: "Beta", FanOutScore: 1, TouchedFiles: []string{"x.go"}},
	}

	f := BuildFile(items, now)

	if !f.GeneratedAt.Equal(now) {
		t.Fatalf("want GeneratedAt %v, got %v", now, f.GeneratedAt)
	}
	if len(f.Items) != 2 || f.Items[0].ID != "a" || f.Items[1].ID != "b" {
		t.Fatalf("want items preserved in caller order, got %+v", f.Items)
	}
	if f.Items[0].FanOutScore != 3 || f.Items[0].Kind != "bead" {
		t.Fatalf("want projected FanOutScore/Kind preserved, got %+v", f.Items[0])
	}
	ids, ok := f.Conflicts["x.go"]
	if !ok || len(ids) != 2 {
		t.Fatalf("want a conflict on x.go naming both item IDs, got %+v", f.Conflicts)
	}
}

func TestBuildFileNoConflictsOmitted(t *testing.T) {
	items := []store.Item{{ID: "a", TouchedFiles: []string{"x.go"}}}
	f := BuildFile(items, time.Now())
	if len(f.Conflicts) != 0 {
		t.Fatalf("want zero conflicts for a single item, got %+v", f.Conflicts)
	}
}

func TestWriteFileRoundTrips(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wave.json")
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	f := BuildFile([]store.Item{{ID: "a", Title: "Alpha", FanOutScore: 2}}, now)

	if err := WriteFile(path, f); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	var got File
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if len(got.Items) != 1 || got.Items[0].ID != "a" || got.Items[0].FanOutScore != 2 {
		t.Fatalf("round-tripped file mismatch: %+v", got)
	}

	// No leftover temp file from the atomic-write helper.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("want exactly 1 entry (wave.json), got %d: %v", len(entries), entries)
	}
}

func TestWriteFileOverwritesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wave.json")

	if err := WriteFile(path, BuildFile([]store.Item{{ID: "a"}}, time.Now())); err != nil {
		t.Fatalf("first write failed: %v", err)
	}
	if err := WriteFile(path, BuildFile([]store.Item{{ID: "b"}}, time.Now())); err != nil {
		t.Fatalf("second write failed: %v", err)
	}

	var got File
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if len(got.Items) != 1 || got.Items[0].ID != "b" {
		t.Fatalf("want the second write's content, got %+v", got)
	}
}
