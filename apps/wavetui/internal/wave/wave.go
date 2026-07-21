// Package wave implements wavetui's own file-overlap conflict detection
// for a candidate set of items being assembled into a wave — see
// openspec/changes/wavetui-dispatch/design.md § Store additive field.
//
// This is a SEPARATE Go implementation of the same idea
// `scripts/bin/wave-plan-build`'s parse_proposal_paths already solves for
// a one-shot CLI wave build — not a call-out to that Python script. It
// exists because wavetui's wave assembly happens interactively inside a
// TUI session (QueuePane's select-mode), not as a batch step.
//
// ConflictsFor is pure: no I/O, no filesystem access, no shell-out. The
// wave finalization writer (persisting a confirmed wave to disk, per the
// format decided at tasks.md [1.1]) is a separate concern that lands in
// the UI batch — see tasks.md [3.3].
package wave

import (
	"sort"

	"github.com/leonardoacosta/installfest/apps/wavetui/internal/store"
)

// ConflictsFor scans candidates' TouchedFiles and returns every path
// touched by two or more distinct candidates, keyed by that path, with the
// value being the sorted, deduplicated list of item IDs that declared it.
// A path touched by only one candidate is not a conflict and is omitted
// from the result entirely — QueuePane's select-mode renders one warning
// row per key in this map, naming both (or more) item IDs, never silently
// dropping a candidate from the wave.
//
// An item with empty TouchedFiles (the common case for a bead, which has
// no `- touches:` declaration) never contributes a key and can never
// itself be reported as conflicting.
func ConflictsFor(candidates []store.Item) map[string][]string {
	byPath := make(map[string][]string)

	for _, item := range candidates {
		for _, path := range item.TouchedFiles {
			if path == "" {
				continue
			}
			if !containsID(byPath[path], item.ID) {
				byPath[path] = append(byPath[path], item.ID)
			}
		}
	}

	conflicts := make(map[string][]string)
	for path, ids := range byPath {
		if len(ids) < 2 {
			continue
		}
		sorted := append([]string(nil), ids...)
		sort.Strings(sorted)
		conflicts[path] = sorted
	}

	return conflicts
}

// containsID reports whether ids already contains id — used to guard
// against double-counting the same item ID if a caller's TouchedFiles ever
// contains a duplicate path for the same item (defensive; no known source
// produces this today).
func containsID(ids []string, id string) bool {
	for _, existing := range ids {
		if existing == id {
			return true
		}
	}
	return false
}
