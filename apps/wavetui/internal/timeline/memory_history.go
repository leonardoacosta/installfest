// See types.go for the package-level contract. This file implements
// MemoryHistorySource — see openspec/changes/wavetui-memory-timeline/
// tasks.md [2.1]-[2.4] and design.md § Memory directory resolution, §
// Journal-to-bead matching.
package timeline

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// MemoryHistorySource resolves and reads a project's Claude Code memory
// directory (~/.claude/projects/<flattened-cwd>/memory/) per design.md §
// Memory directory resolution. Unlike BeadsHistorySource/
// OpenSpecArchiveSource, it has no per-item selector to take a Query
// parameter for — memory is scoped to the whole project, not to a
// currently-selected bead/proposal — so its Query signature takes only a
// context, matching the on-demand, no-fsnotify, run-once-per-selection-
// change discipline design.md establishes for all three sources.
type MemoryHistorySource struct {
	root string // target project root; used only to derive the flattened-cwd memory-directory path
	// claudeProjectsDir is normally ~/.claude/projects, resolved once at
	// construction time via os.UserHomeDir(). It is a field (not a
	// hardcoded path) so tests can point it at a hermetic fixture
	// directory instead of touching a real $HOME/.claude/projects/.
	claudeProjectsDir string
}

// NewMemoryHistorySource constructs a MemoryHistorySource for a project
// rooted at root (the project root wavetui is running against). If
// os.UserHomeDir() cannot resolve $HOME, claudeProjectsDir is left empty
// and every Query call degrades to Result{Availability: Unavailable} —
// the same badge-not-error precedent design.md establishes for a missing
// memory directory, applied to the rarer case of not being able to derive
// the memory directory's location at all.
func NewMemoryHistorySource(root string) *MemoryHistorySource {
	dir := ""
	if home, err := os.UserHomeDir(); err == nil {
		dir = filepath.Join(home, ".claude", "projects")
	}
	return &MemoryHistorySource{root: root, claudeProjectsDir: dir}
}

// memoryDirInfo is the resolved memory-directory state — the shared result
// of design.md § Memory directory resolution's two steps (symlink
// resolution, then independent git-root discovery), consumed by both the
// journal-preferred path and the git-log-fallback path.
type memoryDirInfo struct {
	// path is the memory directory's real, symlink-resolved absolute path.
	// Empty when the directory does not exist at all (or claudeProjectsDir
	// could not be derived).
	path string
	// gitRoot is the repo root git reports for path — independent of the
	// target project's own root (design.md: "the memory directory's git
	// root is independent of the target project's own git root... never
	// assume it equals the target project's own repo root"). Empty when
	// path is not inside any git repository, or path itself is empty.
	gitRoot string
}

// resolveMemoryDir implements design.md § Memory directory resolution's two
// verified-live steps: compute the flattened-cwd path, resolve it through
// any symlinks (this repo's own ~/.claude -> ~/dev/cc is exactly this
// case), then independently ask git for that resolved directory's own repo
// root rather than assuming it matches s.root's repo.
func (s *MemoryHistorySource) resolveMemoryDir(ctx context.Context) (memoryDirInfo, error) {
	if s.claudeProjectsDir == "" {
		return memoryDirInfo{}, nil
	}

	absRoot, err := filepath.Abs(s.root)
	if err != nil {
		return memoryDirInfo{}, fmt.Errorf("memory history: abs root %s: %w", s.root, err)
	}
	rawDir := filepath.Join(s.claudeProjectsDir, flattenCwd(absRoot), "memory")

	resolved, err := filepath.EvalSymlinks(rawDir)
	if err != nil {
		if os.IsNotExist(err) {
			return memoryDirInfo{}, nil
		}
		return memoryDirInfo{}, fmt.Errorf("memory history: resolve symlinks %s: %w", rawDir, err)
	}

	gitRoot, err := gitShowToplevel(ctx, resolved)
	if err != nil {
		return memoryDirInfo{}, err
	}
	return memoryDirInfo{path: resolved, gitRoot: gitRoot}, nil
}

// flattenCwd implements Claude Code's own memory-directory naming
// convention (design.md § Memory directory resolution, verified live
// against this repo's own memory directory): replace every path separator
// in an absolute path with "-".
func flattenCwd(absPath string) string {
	return strings.ReplaceAll(absPath, string(filepath.Separator), "-")
}

// gitShowToplevel runs `git -C dir rev-parse --show-toplevel`. A non-zero
// git exit (dir is not inside any git repository — the expected outcome
// for, e.g., a memory directory that has never been git-init'd) returns
// ("", nil), not an error: design.md's "present but not a git repo"
// branch is a normal outcome, never surfaced as a failure. Only a genuine
// invocation failure (git not installed, context cancelled/deadline
// exceeded before or during Start) is returned as an error — distinguished
// via *exec.ExitError, which is exactly what a non-zero git exit produces
// and nothing else does.
func gitShowToplevel(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--show-toplevel")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return "", nil
		}
		return "", fmt.Errorf("memory history: git rev-parse --show-toplevel %s: %w: %s", dir, err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

// Query implements design.md's three-way branch given the resolved memory
// directory: journal-preferred (dated journal.md entries, no git-log
// call), git-log-fallback (one source=distilled Entry per commit touching
// the directory), or unavailable (directory absent, or present but not a
// git repo with no dated journal.md either). It never runs a git-log call
// on the journal-preferred path, per design.md § Memory directory
// resolution.
func (s *MemoryHistorySource) Query(ctx context.Context) (Result, error) {
	info, err := s.resolveMemoryDir(ctx)
	if err != nil {
		return Result{}, err
	}
	if info.path == "" {
		return Result{Availability: Unavailable}, nil
	}

	journalEntries, hasDatedEntries, err := parseJournal(filepath.Join(info.path, "journal.md"))
	if err != nil {
		return Result{}, err
	}
	if hasDatedEntries {
		return Result{Entries: journalEntries, Availability: Available}, nil
	}

	if info.gitRoot == "" {
		return Result{Availability: Unavailable}, nil
	}

	entries, err := distilledEntries(ctx, info)
	if err != nil {
		return Result{}, err
	}
	return Result{Entries: entries, Availability: Available}, nil
}

// journalDateLineRe matches a journal.md line that begins a dated entry —
// design.md: "a journal.md with dated entries (a heading or line matching
// a date pattern)". Matches an optional markdown heading marker (0-6 "#"),
// then an ISO YYYY-MM-DD date, then an optional ":"/"-" separator, then any
// trailing same-line text becomes that entry's initial text.
var journalDateLineRe = regexp.MustCompile(`^\s*#{0,6}\s*(\d{4}-\d{2}-\d{2})\s*[:\-]?\s*(.*)$`)

// parseJournal reads path and splits it into dated entries per
// journalDateLineRe. A missing file returns (nil, false, nil) — not an
// error, since the caller falls through to the git-log-fallback path.
// hasDatedEntries is false whenever zero lines matched the date pattern
// (including when the file exists but has no dated content at all) —
// design.md requires journal.md to both exist AND contain dated entries
// before this path is preferred over git-log-fallback.
func parseJournal(path string) ([]Entry, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("memory history: read %s: %w", path, err)
	}

	var entries []Entry
	var cur *Entry
	var body []string
	flush := func() {
		if cur == nil {
			return
		}
		cur.Text = strings.TrimSpace(strings.Join(append([]string{cur.Text}, body...), "\n"))
		entries = append(entries, *cur)
	}

	for _, line := range strings.Split(string(data), "\n") {
		m := journalDateLineRe.FindStringSubmatch(line)
		if m == nil {
			if cur != nil {
				body = append(body, line)
			}
			continue
		}
		t, perr := time.Parse("2006-01-02", m[1])
		if perr != nil {
			// Cannot happen given the regex's \d{4}-\d{2}-\d{2} shape, but
			// tolerant-skip rather than fail the whole parse if it ever did.
			continue
		}
		flush()
		cur = &Entry{
			Source:    SourceJournal,
			Time:      t,
			Precision: PrecisionDateOnly,
			Text:      m[2],
		}
		body = nil
	}
	flush()

	return entries, len(entries) > 0, nil
}

// diffGitLineRe matches a unified-diff "diff --git a/<path> b/<path>"
// header line. Paths containing the literal substring " b/" would defeat
// this simple split-based extraction (a known, accepted simplification —
// real memory-directory content in this repo never does this).
var diffGitLineRe = regexp.MustCompile(`^diff --git a/(.*) b/(.*)$`)

// fileChange is one file's mechanically-derived change kind within a
// single commit's diff, extracted only from diff-header marker lines
// ("new file mode", "deleted file mode", "rename to ") — never from the
// hunk content itself, per design.md's "never generated prose" constraint.
type fileChange struct {
	path string
	kind string // "added" | "deleted" | "renamed" | "modified"
}

// parseDiffFileChanges scans one commit's raw `git log -p` body for
// diff --git sections and classifies each by its own header markers.
func parseDiffFileChanges(body string) []fileChange {
	var changes []fileChange
	var cur *fileChange
	flush := func() {
		if cur != nil {
			changes = append(changes, *cur)
		}
	}
	for _, line := range strings.Split(body, "\n") {
		if m := diffGitLineRe.FindStringSubmatch(line); m != nil {
			flush()
			cur = &fileChange{path: m[2], kind: "modified"}
			continue
		}
		if cur == nil {
			continue
		}
		switch {
		case strings.HasPrefix(line, "new file mode"):
			cur.kind = "added"
		case strings.HasPrefix(line, "deleted file mode"):
			cur.kind = "deleted"
		case strings.HasPrefix(line, "rename to "):
			cur.kind = "renamed"
			cur.path = strings.TrimPrefix(line, "rename to ")
		}
	}
	flush()
	return changes
}

// changeKindOrder fixes a deterministic rendering order for
// summarizeFileChanges — otherwise map iteration order would make the
// same commit's summary text vary between runs.
var changeKindOrder = []string{"added", "deleted", "renamed", "modified"}

// summarizeFileChanges builds one commit's source=distilled Entry.Text
// from mechanically-classified file changes — design.md: "a diff-header-
// derived summary, never generated prose."
func summarizeFileChanges(changes []fileChange) string {
	byKind := make(map[string][]string, len(changeKindOrder))
	for _, c := range changes {
		byKind[c.kind] = append(byKind[c.kind], filepath.Base(c.path))
	}
	var parts []string
	for _, kind := range changeKindOrder {
		names := byKind[kind]
		if len(names) == 0 {
			continue
		}
		parts = append(parts, kind+": "+strings.Join(names, ", "))
	}
	if len(parts) == 0 {
		return "modified"
	}
	return strings.Join(parts, "; ")
}

// commitRecordSep/commitFieldSep are control characters used to delimit
// `git log --format=...` output into unambiguous per-commit records —
// chosen because they cannot appear in normal diff/commit text, avoiding
// any need to parse around git's own "commit <hash>" boundary lines (which
// can also appear inside a diff body, e.g. a commit message quoting
// another commit).
const (
	commitRecordSep = "\x01"
	commitFieldSep  = "\x1f"
)

// distilledEntries runs `git log --follow -p -- <relPath>` from
// info.gitRoot and reconstructs one source=distilled Entry per commit,
// per design.md § Memory directory resolution's git-log-fallback path.
func distilledEntries(ctx context.Context, info memoryDirInfo) ([]Entry, error) {
	relPath, err := filepath.Rel(info.gitRoot, info.path)
	if err != nil {
		return nil, fmt.Errorf("memory history: rel path %s -> %s: %w", info.gitRoot, info.path, err)
	}

	cmd := exec.CommandContext(ctx, "git", "-C", info.gitRoot, "log", "--follow", "-p",
		"--format="+commitRecordSep+"%H"+commitFieldSep+"%aI", "--", relPath)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("memory history: git log --follow %s: %w: %s", relPath, err, strings.TrimSpace(stderr.String()))
	}

	var entries []Entry
	for _, chunk := range strings.Split(stdout.String(), commitRecordSep) {
		if chunk == "" {
			continue
		}
		header, body, _ := strings.Cut(chunk, "\n")
		_, dateStr, ok := strings.Cut(header, commitFieldSep)
		if !ok {
			continue // malformed header — tolerant-skip, matches package house style
		}
		t, perr := time.Parse(time.RFC3339, dateStr)
		if perr != nil {
			continue // tolerant-skip: unparsable commit date
		}
		entries = append(entries, Entry{
			Source:    SourceDistilled,
			Time:      t,
			Precision: PrecisionTimestamp,
			Text:      summarizeFileChanges(parseDiffFileChanges(body)),
		})
	}
	return entries, nil
}

// DefaultMatchWindow is the timestamp-proximity fuzzy-match fallback's
// default window (design.md § Journal-to-bead matching: "a configurable
// window (default 10 minutes)"), used by MatchToBeads whenever its window
// argument is <= 0.
const DefaultMatchWindow = 10 * time.Minute

// inlineBeadRefRe matches a bracketed inline bead-ID reference in journal
// text, e.g. "[if-tp05]" or "[if-w83ov.9]" — design.md § Journal-to-bead
// matching tier 1. The character class is this repo's canonical bead-ID
// grammar (rules/TOOLING.md's bead-id-grammar-canonical row; matches
// BEADS_ID_RE in scripts/lib/beads-helpers.sh: '[A-Za-z0-9][A-Za-z0-9._-]*'),
// applied twice around a required hyphen: every real bead ID observed in
// this repo has the "<prefix>-<slug>" shape, and requiring the hyphen
// keeps a bare bracketed word (e.g. "[DEFERRED]", the banned checkbox-
// deferral dialect rules/CORE.md documents) from false-positiving as a
// confident bead match.
var inlineBeadRefRe = regexp.MustCompile(`\[([A-Za-z0-9][A-Za-z0-9._-]*-[A-Za-z0-9][A-Za-z0-9._-]*)\]`)

// extractInlineBeadRef returns the first inline bead-ID reference found in
// text, if any.
func extractInlineBeadRef(text string) (string, bool) {
	m := inlineBeadRefRe.FindStringSubmatch(text)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// nearestBeadWithinWindow finds the beadEntries element (expected to be
// SourceBead entries produced by BeadsHistorySource.Query) whose Time is
// closest to t, returning its BeadID only if that closest distance is
// within window. Entries with a zero Time, or not carrying Source ==
// SourceBead, are ignored defensively — MatchToBeads's contract expects
// only bead entries in this slice, but a caller passing something else
// should degrade to "no match" rather than panic or mis-associate.
func nearestBeadWithinWindow(t time.Time, beadEntries []Entry, window time.Duration) (string, bool) {
	if t.IsZero() {
		return "", false
	}
	var bestID string
	found := false
	var bestDelta time.Duration
	for _, b := range beadEntries {
		if b.Source != SourceBead || b.Time.IsZero() {
			continue
		}
		delta := t.Sub(b.Time)
		if delta < 0 {
			delta = -delta
		}
		if delta > window {
			continue
		}
		if !found || delta < bestDelta {
			found = true
			bestDelta = delta
			bestID = b.BeadID
		}
	}
	return bestID, found
}

// MatchToBeads implements design.md § Journal-to-bead matching for a slice
// of source=journal/source=distilled entries (typically MemoryHistorySource
// Query's output), given the beadEntries a BeadsHistorySource.Query call
// already produced for the same selected item. It does not mutate entries
// in place; it returns a new slice in the same order.
//
// Two tiers, most-confident first:
//
//  1. Confident — an inline bracketed bead-ID reference in the entry's own
//     text. Checked only for Source == SourceJournal: a mechanically
//     generated SourceDistilled diff-header summary has no inline-ref
//     convention to look for (design.md), so it always goes straight to
//     tier 2.
//  2. Tentative — nearest-timestamp proximity to a beadEntries element,
//     gated by window (DefaultMatchWindow when window <= 0). Unlike
//     wavetui-sessions' session-linkage fallback match, which needs two
//     independent signals (cwd equality + timestamp proximity) because
//     cwd alone is too coarse when multiple sessions share a repo, a
//     journal/distilled entry carries no cwd-equivalent identity signal
//     at all — there is nothing here playing the role cwd plays there.
//     This reduces to the single timestamp-proximity signal design.md's
//     own § Journal-to-bead matching specifies, gated by a window for the
//     same reason wavetui-sessions' design.md gives for why its own
//     window exists: proximity alone, unbounded, is too coarse to assert
//     as a match — nothing stops an unrelated bead event from being the
//     "nearest" one if there is no bound on how near is near enough.
//
// An entry that matches neither tier is returned with BeadID cleared and
// Match == MatchConfidenceNone — design.md: "renders unmatched, bucketed
// by date only."
func MatchToBeads(entries []Entry, beadEntries []Entry, window time.Duration) []Entry {
	if window <= 0 {
		window = DefaultMatchWindow
	}
	out := make([]Entry, len(entries))
	for i, e := range entries {
		out[i] = matchOneToBeads(e, beadEntries, window)
	}
	return out
}

func matchOneToBeads(e Entry, beadEntries []Entry, window time.Duration) Entry {
	if e.Source == SourceJournal {
		if id, ok := extractInlineBeadRef(e.Text); ok {
			e.BeadID = id
			e.Match = MatchConfidenceConfident
			return e
		}
	}
	if id, ok := nearestBeadWithinWindow(e.Time, beadEntries, window); ok {
		e.BeadID = id
		e.Match = MatchConfidenceTentative
		return e
	}
	e.BeadID = ""
	e.Match = MatchConfidenceNone
	return e
}
