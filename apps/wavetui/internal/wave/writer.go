// See wave.go for the package-level ConflictsFor contract. This file
// implements the wave finalization writer — openspec/changes/wavetui-
// dispatch/tasks.md [3.3] and design.md § Open Question: wave-file format.
//
// The format is JSON, not a bead — resolved during this run's Phase 0d
// preflight (see openspec/changes/wavetui-dispatch/decisions.jsonl: task
// "1.1", verdict "JSON, not a bead"), matching design.md's own
// recommendation (machine-artifact convention: a file consumed by tooling,
// never hand-edited, per documentation-writer's operational-docs canon).
package wave

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/leonardoacosta/installfest/apps/wavetui/internal/config"
	"github.com/leonardoacosta/installfest/apps/wavetui/internal/store"
)

// FileItem is one item's on-disk projection inside a finalized wave File — a
// deliberately narrow subset of store.Item (id/kind/title/fan-out score/
// touched files only), not a full Item serialization: a wave file is
// consumed by a future dispatcher (TmuxDispatcher today, HeadlessDispatcher
// later — see dispatch/dispatch.go's Dispatcher interface doc comment), so
// UI-only or session-derived fields (Blocker, TaskProgress, Session, Stale,
// SecondClass) are deliberately omitted rather than round-tripped.
type FileItem struct {
	ID           string   `json:"id"`
	Kind         string   `json:"kind"`
	Title        string   `json:"title"`
	FanOutScore  int      `json:"fan_out_score"`
	TouchedFiles []string `json:"touched_files,omitempty"`
}

// File is the on-disk JSON shape of a finalized wave.
type File struct {
	GeneratedAt time.Time  `json:"generated_at"`
	Items       []FileItem `json:"items"`
	// Conflicts mirrors QueuePane's select-mode in-UI warnings (tasks.md
	// [3.2]) so a conflict flagged before finalize is persisted alongside
	// the finalized wave, not silently dropped once the operator commits.
	Conflicts map[string][]string `json:"conflicts,omitempty"`
}

// BuildFile projects candidates — in the CALLER's order — into a File,
// deriving Conflicts via ConflictsFor over the same candidate set. Candidates
// are expected to already be ordered by the caller (QueuePane.SelectedForWave's
// FanOutScore-descending order); BuildFile does not re-sort them, since
// re-deriving an ordering the caller already computed would be a second,
// possibly-diverging source of truth for the same decision.
func BuildFile(candidates []store.Item, now time.Time) File {
	items := make([]FileItem, len(candidates))
	for i, it := range candidates {
		items[i] = FileItem{
			ID:           it.ID,
			Kind:         string(it.Kind),
			Title:        it.Title,
			FanOutScore:  it.FanOutScore,
			TouchedFiles: it.TouchedFiles,
		}
	}
	return File{
		GeneratedAt: now,
		Items:       items,
		Conflicts:   ConflictsFor(candidates),
	}
}

// WriteFile atomically writes f as indented JSON to path via
// config.AtomicWriteFile — wavetui-core's existing temp-file-in-the-same-
// dir-then-rename helper (internal/config/config.go's own doc comment:
// "any wavetui package that needs to persist state safely may call this
// directly"), reused as-is per the Reader Gate rather than re-implementing
// the identical .tmp+rename dance a second time in this package.
func WriteFile(path string, f File) error {
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("wave: marshal: %w", err)
	}
	if err := config.AtomicWriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("wave: write file: %w", err)
	}
	return nil
}
