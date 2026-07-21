// See types.go for the package-level contract. This file implements
// OpenSpecArchiveSource — see openspec/changes/wavetui-memory-timeline/
// tasks.md [1.3] and design.md § OpenSpec archive milestone source.
package timeline

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// OpenSpecArchiveSource answers one question per selected item: did this
// item's proposal ever get archived, and when? It never watches
// openspec/changes/archive/ (no fsnotify) — every call is a point-in-time
// query, run only when an item is selected, per design.md.
type OpenSpecArchiveSource struct {
	root string // project root; openspec/changes/archive/ is expected directly under it
}

// NewOpenSpecArchiveSource constructs an OpenSpecArchiveSource rooted at
// root (a project root — typically the cwd wavetui was launched from, and
// expected to be a git repository so the git-log timestamp recovery below
// can run).
func NewOpenSpecArchiveSource(root string) *OpenSpecArchiveSource {
	return &OpenSpecArchiveSource{root: root}
}

func (s *OpenSpecArchiveSource) archiveDir() string {
	return filepath.Join(s.root, "openspec", "changes", "archive")
}

// Query looks for a directory under openspec/changes/archive/ whose name
// ends in "-<proposalSlug>" — archived proposals are prefixed with their
// archive date (e.g. "2026-07-10-cc-tmux-plugin" for slug
// "cc-tmux-plugin"), so a glob/suffix match is required, not an exact
// path, per design.md. If a match is found and git has a record of the
// commit that added it, Query returns that commit's author-date as a
// single Source == SourceArchive Entry.
//
// An empty proposalSlug (a bead-kind item with no associated proposal), no
// matching archive directory, or no git history for a matched directory's
// path are all treated as "this item was never archived" — an expected,
// non-error, badge-free empty Result, per design.md. Only a genuine git
// invocation failure (not a git repo, git not installed, glob pattern
// error) is returned as an error.
func (s *OpenSpecArchiveSource) Query(ctx context.Context, proposalSlug string) (Result, error) {
	if proposalSlug == "" {
		return Result{Availability: Available}, nil
	}

	matches, err := filepath.Glob(filepath.Join(s.archiveDir(), "*-"+proposalSlug))
	if err != nil {
		return Result{}, fmt.Errorf("openspec archive: glob: %w", err)
	}
	if len(matches) == 0 {
		return Result{Availability: Available}, nil
	}

	// The dated prefix ("YYYY-MM-DD-...") sorts lexicographically in
	// chronological order, so the last match is the most recently archived
	// directory of that name — the realistic tiebreak for the vanishingly
	// unlikely case of more than one match (a proposal slug is only ever
	// archived once in the normal workflow).
	sort.Strings(matches)
	matched := matches[len(matches)-1]

	relPath, err := filepath.Rel(s.root, matched)
	if err != nil {
		return Result{}, fmt.Errorf("openspec archive: rel path: %w", err)
	}

	ts, err := s.archiveTimestamp(ctx, relPath)
	if err != nil {
		return Result{}, err
	}
	if ts.IsZero() {
		// git ran fine but found no commit that added this path — treat
		// as "never archived", not an error.
		return Result{Availability: Available}, nil
	}

	return Result{
		Entries: []Entry{{
			Source:    SourceArchive,
			Time:      ts,
			Precision: PrecisionTimestamp,
			Text:      "archived: " + filepath.Base(matched),
		}},
		Availability: Available,
	}, nil
}

// archiveTimestamp runs `git log -1 --format=%aI --diff-filter=A --
// <relPath>` from s.root to recover the archive-landing commit's
// author-date. Empty stdout (git ran successfully, found nothing) returns
// the zero time.Time and a nil error; a non-zero git exit is returned as
// an error.
func (s *OpenSpecArchiveSource) archiveTimestamp(ctx context.Context, relPath string) (time.Time, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", s.root, "log", "-1", "--format=%aI", "--diff-filter=A", "--", relPath)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return time.Time{}, fmt.Errorf("openspec archive: git log %s: %w: %s", relPath, err, strings.TrimSpace(stderr.String()))
	}

	out := strings.TrimSpace(stdout.String())
	if out == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339, out)
	if err != nil {
		// Tolerant: an unparsable timestamp is treated the same as "no
		// history found" rather than surfaced as an error.
		return time.Time{}, nil
	}
	return t, nil
}
