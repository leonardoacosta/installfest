// See wave.go for the ConflictsFor contract under test here — tasks.md
// [4.3]: overlapping-path fixtures naming both item IDs, and the
// zero-overlap case. writer_test.go already exercises ConflictsFor
// indirectly through BuildFile's two-item overlap/no-overlap cases; this
// file adds direct, dedicated fixtures for ConflictsFor itself (3+ items,
// multiple independent conflicting paths, duplicate-path defense, and
// deterministic ID-sort-within-conflict), which BuildFile's projection
// layer does not need to exercise on its own.
//
// FanOutScore-descending ordering "in the caller's selection view" (the
// task's third named case) is QueuePane.SelectedForWave's own contract, not
// ConflictsFor's — ConflictsFor takes candidates in whatever order the
// caller passes and never reorders them. That ordering is already covered
// by internal/ui/queuepane_test.go's SelectedForWave tests (UI batch); no
// gap exists here to duplicate.
package wave

import (
	"reflect"
	"testing"

	"github.com/leonardoacosta/installfest/apps/wavetui/internal/store"
)

func TestConflictsForOverlappingPathNamesBothItemIDs(t *testing.T) {
	candidates := []store.Item{
		{ID: "b-item", TouchedFiles: []string{"pkg/x.go"}},
		{ID: "a-item", TouchedFiles: []string{"pkg/x.go"}},
	}

	got := ConflictsFor(candidates)
	want := map[string][]string{"pkg/x.go": {"a-item", "b-item"}} // sorted
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("want %+v, got %+v", want, got)
	}
}

func TestConflictsForZeroOverlapIsEmpty(t *testing.T) {
	candidates := []store.Item{
		{ID: "a", TouchedFiles: []string{"a.go"}},
		{ID: "b", TouchedFiles: []string{"b.go"}},
		{ID: "c", TouchedFiles: []string{}}, // a bead: no touches declared at all
	}

	got := ConflictsFor(candidates)
	if len(got) != 0 {
		t.Fatalf("want zero conflicts for disjoint paths, got %+v", got)
	}
}

func TestConflictsForSingleItemNeverConflicts(t *testing.T) {
	got := ConflictsFor([]store.Item{{ID: "solo", TouchedFiles: []string{"only.go"}}})
	if len(got) != 0 {
		t.Fatalf("want zero conflicts for a single candidate, got %+v", got)
	}
}

func TestConflictsForMultipleIndependentPaths(t *testing.T) {
	candidates := []store.Item{
		{ID: "a", TouchedFiles: []string{"shared1.go", "onlya.go"}},
		{ID: "b", TouchedFiles: []string{"shared1.go"}},
		{ID: "c", TouchedFiles: []string{"shared2.go"}},
		{ID: "d", TouchedFiles: []string{"shared2.go"}},
	}

	got := ConflictsFor(candidates)
	want := map[string][]string{
		"shared1.go": {"a", "b"},
		"shared2.go": {"c", "d"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("want %+v, got %+v", want, got)
	}
}

func TestConflictsForThreeWayOverlapNamesAllIDs(t *testing.T) {
	candidates := []store.Item{
		{ID: "z", TouchedFiles: []string{"hot.go"}},
		{ID: "a", TouchedFiles: []string{"hot.go"}},
		{ID: "m", TouchedFiles: []string{"hot.go"}},
	}

	got := ConflictsFor(candidates)
	want := map[string][]string{"hot.go": {"a", "m", "z"}} // sorted, not input order
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("want %+v, got %+v", want, got)
	}
}

func TestConflictsForEmptyPathEntriesIgnored(t *testing.T) {
	candidates := []store.Item{
		{ID: "a", TouchedFiles: []string{"", "real.go"}},
		{ID: "b", TouchedFiles: []string{""}},
	}

	got := ConflictsFor(candidates)
	if len(got) != 0 {
		t.Fatalf("want empty-string paths never treated as a conflict key, got %+v", got)
	}
}

func TestConflictsForDuplicatePathWithinSameItemDeduped(t *testing.T) {
	candidates := []store.Item{
		{ID: "a", TouchedFiles: []string{"dup.go", "dup.go"}}, // same item declares it twice
		{ID: "b", TouchedFiles: []string{"dup.go"}},
	}

	got := ConflictsFor(candidates)
	want := map[string][]string{"dup.go": {"a", "b"}} // "a" counted once, not twice
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("want %+v, got %+v", want, got)
	}
}

func TestConflictsForNilTouchedFilesNeverContributesKeys(t *testing.T) {
	candidates := []store.Item{
		{ID: "bead-1"}, // TouchedFiles nil — the common bead case
		{ID: "bead-2"},
	}

	got := ConflictsFor(candidates)
	if len(got) != 0 {
		t.Fatalf("want no conflicts from items with nil TouchedFiles, got %+v", got)
	}
}

func TestConflictsForEmptyCandidateSliceIsEmpty(t *testing.T) {
	got := ConflictsFor(nil)
	if len(got) != 0 {
		t.Fatalf("want empty map for nil candidates, got %+v", got)
	}
}
