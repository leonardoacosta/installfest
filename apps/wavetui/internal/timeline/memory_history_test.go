package timeline

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// newTestSource builds a MemoryHistorySource whose "~/.claude/projects"
// equivalent is a hermetic temp directory instead of the real $HOME —
// never touches this sandbox's actual ~/.claude/projects/.
func newTestSource(root, claudeProjectsDir string) *MemoryHistorySource {
	return &MemoryHistorySource{root: root, claudeProjectsDir: claudeProjectsDir}
}

// flattenedMemoryDir mirrors flattenCwd + the "<flattened>/memory" suffix,
// for building fixture paths in tests without duplicating the production
// path-join logic inline in every test.
func flattenedMemoryDir(t *testing.T, claudeProjectsDir, projectRoot string) string {
	t.Helper()
	absRoot, err := filepath.Abs(projectRoot)
	if err != nil {
		t.Fatalf("abs %s: %v", projectRoot, err)
	}
	return filepath.Join(claudeProjectsDir, flattenCwd(absRoot), "memory")
}

func TestMemoryHistorySource_DirectoryAbsent_Unavailable(t *testing.T) {
	projectRoot := t.TempDir()
	claudeProjectsDir := t.TempDir() // exists, but no <flattened>/memory subdir inside it

	s := newTestSource(projectRoot, claudeProjectsDir)
	res, err := s.Query(context.Background())
	if err != nil {
		t.Fatalf("Query returned error: %v", err)
	}
	if res.Availability != Unavailable {
		t.Fatalf("Availability = %v, want Unavailable", res.Availability)
	}
	if len(res.Entries) != 0 {
		t.Fatalf("Entries = %+v, want empty", res.Entries)
	}
}

func TestMemoryHistorySource_NoHomeResolved_Unavailable(t *testing.T) {
	// claudeProjectsDir == "" mirrors NewMemoryHistorySource's degrade path
	// when os.UserHomeDir() fails — never an error, per design.md's
	// badge-not-crash precedent.
	s := newTestSource(t.TempDir(), "")
	res, err := s.Query(context.Background())
	if err != nil {
		t.Fatalf("Query returned error: %v", err)
	}
	if res.Availability != Unavailable {
		t.Fatalf("Availability = %v, want Unavailable", res.Availability)
	}
}

func TestMemoryHistorySource_PresentButNotGitRepo_NoJournal_Unavailable(t *testing.T) {
	projectRoot := t.TempDir()
	claudeProjectsDir := t.TempDir()
	memDir := flattenedMemoryDir(t, claudeProjectsDir, projectRoot)
	if err := os.MkdirAll(memDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// memDir exists, contains no journal.md, and is not inside any git repo.

	s := newTestSource(projectRoot, claudeProjectsDir)
	res, err := s.Query(context.Background())
	if err != nil {
		t.Fatalf("Query returned error: %v", err)
	}
	if res.Availability != Unavailable {
		t.Fatalf("Availability = %v, want Unavailable", res.Availability)
	}
}

func TestMemoryHistorySource_SymlinkedMemoryDir_ResolvesRealPath(t *testing.T) {
	projectRoot := t.TempDir()
	realHome := t.TempDir()

	// claudeProjectsDir/../.claude is a symlink pointing at realHome/dotclaude
	// (mirroring this machine's real ~/.claude -> ~/dev/cc symlink) — the
	// projects dir passed to the source sits behind that symlink.
	realClaudeDir := filepath.Join(realHome, "dotclaude")
	if err := os.MkdirAll(filepath.Join(realClaudeDir, "projects"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	linkedClaudeDir := filepath.Join(t.TempDir(), "claude-link")
	if err := os.Symlink(realClaudeDir, linkedClaudeDir); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	claudeProjectsDir := filepath.Join(linkedClaudeDir, "projects")

	memDir := flattenedMemoryDir(t, claudeProjectsDir, projectRoot)
	if err := os.MkdirAll(memDir, 0o755); err != nil {
		t.Fatalf("mkdir memDir: %v", err)
	}
	journal := "2026-07-15: symlink-resolved entry\n"
	if err := os.WriteFile(filepath.Join(memDir, "journal.md"), []byte(journal), 0o644); err != nil {
		t.Fatalf("write journal.md: %v", err)
	}

	s := newTestSource(projectRoot, claudeProjectsDir)
	res, err := s.Query(context.Background())
	if err != nil {
		t.Fatalf("Query returned error: %v", err)
	}
	if res.Availability != Available {
		t.Fatalf("Availability = %v, want Available", res.Availability)
	}
	if len(res.Entries) != 1 {
		t.Fatalf("Entries = %+v, want exactly 1", res.Entries)
	}
	if res.Entries[0].Text != "symlink-resolved entry" {
		t.Errorf("Text = %q, want %q", res.Entries[0].Text, "symlink-resolved entry")
	}
}

func TestMemoryHistorySource_GitRootIndependentOfProjectRoot(t *testing.T) {
	// Two entirely separate git repos: the "target project" root, and the
	// memory directory's own (unrelated) git root — mirrors this
	// rollout's real live finding (design.md § Memory directory
	// resolution): ~/.claude/projects/<flattened>/memory's git root is
	// /home/nyaptor/dev/cc, NOT the target project's own repo root.
	projectRoot := gitFixtureRoot(t)
	runGit(t, projectRoot, commitEnv("2026-01-01T00:00:00Z"), "commit", "--allow-empty", "-q", "-m", "target project has its own unrelated history")

	// claudeProjectsDir is nested inside its OWN git fixture root
	// (memoryGitRoot), separate from projectRoot's repo — so the memory
	// directory's resolved git root is memoryGitRoot, never projectRoot.
	memoryGitRoot := gitFixtureRoot(t)
	claudeProjectsDir := filepath.Join(memoryGitRoot, "dotclaude", "projects")
	memDir := flattenedMemoryDir(t, claudeProjectsDir, projectRoot)
	if err := os.MkdirAll(memDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(memDir, "notes.md"), []byte("distilled fixture"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	runGit(t, memoryGitRoot, nil, "add", "-A")
	runGit(t, memoryGitRoot, commitEnv("2026-02-01T09:00:00Z"), "commit", "-q", "-m", "add notes.md")

	s := newTestSource(projectRoot, claudeProjectsDir)
	res, err := s.Query(context.Background())
	if err != nil {
		t.Fatalf("Query returned error: %v", err)
	}
	if res.Availability != Available {
		t.Fatalf("Availability = %v, want Available", res.Availability)
	}
	if len(res.Entries) != 1 {
		t.Fatalf("Entries = %+v, want exactly 1 (from memoryGitRoot's history, not projectRoot's)", res.Entries)
	}
	if res.Entries[0].Source != SourceDistilled {
		t.Errorf("Source = %q, want %q", res.Entries[0].Source, SourceDistilled)
	}
	wantTime, _ := time.Parse(time.RFC3339, "2026-02-01T09:00:00Z")
	if !res.Entries[0].Time.Equal(wantTime) {
		t.Errorf("Time = %v, want %v (memoryGitRoot's own commit date, unrelated to projectRoot's history)", res.Entries[0].Time, wantTime)
	}
}

func TestMemoryHistorySource_JournalPreferred_HeadingAndBareLineForms(t *testing.T) {
	projectRoot := t.TempDir()
	claudeProjectsDir := t.TempDir()
	memDir := flattenedMemoryDir(t, claudeProjectsDir, projectRoot)
	if err := os.MkdirAll(memDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	journal := "" +
		"## 2026-07-10 fixed the flaky test\n" +
		"body line one\n" +
		"body line two\n" +
		"\n" +
		"2026-07-12: shipped the feature\n" +
		"more detail here\n"
	if err := os.WriteFile(filepath.Join(memDir, "journal.md"), []byte(journal), 0o644); err != nil {
		t.Fatalf("write journal.md: %v", err)
	}

	s := newTestSource(projectRoot, claudeProjectsDir)
	res, err := s.Query(context.Background())
	if err != nil {
		t.Fatalf("Query returned error: %v", err)
	}
	if res.Availability != Available {
		t.Fatalf("Availability = %v, want Available", res.Availability)
	}
	if len(res.Entries) != 2 {
		t.Fatalf("Entries = %+v, want exactly 2", res.Entries)
	}

	e0 := res.Entries[0]
	if e0.Source != SourceJournal {
		t.Errorf("Entries[0].Source = %q, want %q", e0.Source, SourceJournal)
	}
	if e0.Precision != PrecisionDateOnly {
		t.Errorf("Entries[0].Precision = %v, want PrecisionDateOnly", e0.Precision)
	}
	wantT0, _ := time.Parse("2006-01-02", "2026-07-10")
	if !e0.Time.Equal(wantT0) {
		t.Errorf("Entries[0].Time = %v, want %v", e0.Time, wantT0)
	}
	wantText0 := "fixed the flaky test\nbody line one\nbody line two"
	if e0.Text != wantText0 {
		t.Errorf("Entries[0].Text = %q, want %q", e0.Text, wantText0)
	}

	e1 := res.Entries[1]
	wantText1 := "shipped the feature\nmore detail here"
	if e1.Text != wantText1 {
		t.Errorf("Entries[1].Text = %q, want %q", e1.Text, wantText1)
	}
}

func TestMemoryHistorySource_JournalPresentButNoDatedEntries_FallsBackToGit(t *testing.T) {
	projectRoot := t.TempDir()
	memoryGitRoot := gitFixtureRoot(t)
	claudeProjectsDir := filepath.Join(memoryGitRoot, "dotclaude", "projects")
	memDir := flattenedMemoryDir(t, claudeProjectsDir, projectRoot)
	if err := os.MkdirAll(memDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// journal.md exists but has no dated-entry lines at all.
	if err := os.WriteFile(filepath.Join(memDir, "journal.md"), []byte("just some prose, no dates here\n"), 0o644); err != nil {
		t.Fatalf("write journal.md: %v", err)
	}
	runGit(t, memoryGitRoot, nil, "add", "-A")
	runGit(t, memoryGitRoot, commitEnv("2026-03-01T00:00:00Z"), "commit", "-q", "-m", "add memory dir")

	s := newTestSource(projectRoot, claudeProjectsDir)
	res, err := s.Query(context.Background())
	if err != nil {
		t.Fatalf("Query returned error: %v", err)
	}
	if res.Availability != Available {
		t.Fatalf("Availability = %v, want Available", res.Availability)
	}
	if len(res.Entries) != 1 {
		t.Fatalf("Entries = %+v, want exactly 1 (git-log-fallback, since journal.md had no dated entries)", res.Entries)
	}
	if res.Entries[0].Source != SourceDistilled {
		t.Errorf("Source = %q, want %q (fell back to git-log path, not journal-preferred)", res.Entries[0].Source, SourceDistilled)
	}
}

func TestMemoryHistorySource_GitLogFallback_ReconstructsPerCommitEntries(t *testing.T) {
	projectRoot := t.TempDir()
	memoryGitRoot := gitFixtureRoot(t)
	claudeProjectsDir := filepath.Join(memoryGitRoot, "dotclaude", "projects")
	memDir := flattenedMemoryDir(t, claudeProjectsDir, projectRoot)
	if err := os.MkdirAll(memDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if err := os.WriteFile(filepath.Join(memDir, "a.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	runGit(t, memoryGitRoot, nil, "add", "-A")
	runGit(t, memoryGitRoot, commitEnv("2026-04-01T10:00:00Z"), "commit", "-q", "-m", "add a.md")

	if err := os.WriteFile(filepath.Join(memDir, "a.md"), []byte("hello again\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	runGit(t, memoryGitRoot, nil, "add", "-A")
	runGit(t, memoryGitRoot, commitEnv("2026-04-02T11:00:00Z"), "commit", "-q", "-m", "update a.md")

	if err := os.Remove(filepath.Join(memDir, "a.md")); err != nil {
		t.Fatalf("remove: %v", err)
	}
	runGit(t, memoryGitRoot, nil, "add", "-A")
	runGit(t, memoryGitRoot, commitEnv("2026-04-03T12:00:00Z"), "commit", "-q", "-m", "delete a.md")

	s := newTestSource(projectRoot, claudeProjectsDir)
	res, err := s.Query(context.Background())
	if err != nil {
		t.Fatalf("Query returned error: %v", err)
	}
	if len(res.Entries) != 3 {
		t.Fatalf("Entries = %+v, want exactly 3 (one per commit)", res.Entries)
	}

	// git log without --reverse orders newest-first.
	wantOrder := []struct {
		ts   string
		text string
	}{
		{"2026-04-03T12:00:00Z", "deleted: a.md"},
		{"2026-04-02T11:00:00Z", "modified: a.md"},
		{"2026-04-01T10:00:00Z", "added: a.md"},
	}
	for i, w := range wantOrder {
		e := res.Entries[i]
		if e.Source != SourceDistilled {
			t.Errorf("Entries[%d].Source = %q, want %q", i, e.Source, SourceDistilled)
		}
		if e.Precision != PrecisionTimestamp {
			t.Errorf("Entries[%d].Precision = %v, want PrecisionTimestamp", i, e.Precision)
		}
		wantT, _ := time.Parse(time.RFC3339, w.ts)
		if !e.Time.Equal(wantT) {
			t.Errorf("Entries[%d].Time = %v, want %v", i, e.Time, wantT)
		}
		if e.Text != w.text {
			t.Errorf("Entries[%d].Text = %q, want %q", i, e.Text, w.text)
		}
	}
}

func TestMemoryHistorySource_ContextCancelled_ReturnsError(t *testing.T) {
	projectRoot := t.TempDir()
	memoryGitRoot := gitFixtureRoot(t)
	claudeProjectsDir := filepath.Join(memoryGitRoot, "dotclaude", "projects")
	memDir := flattenedMemoryDir(t, claudeProjectsDir, projectRoot)
	if err := os.MkdirAll(memDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	runGit(t, memoryGitRoot, nil, "add", "-A")
	runGit(t, memoryGitRoot, commitEnv("2026-01-01T00:00:00Z"), "commit", "--allow-empty", "-q", "-m", "init")

	s := newTestSource(projectRoot, claudeProjectsDir)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := s.Query(ctx)
	if err == nil {
		t.Fatalf("Query with cancelled context returned nil error, want a real error")
	}
}

func TestParseJournal_NoDatedEntries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "journal.md")
	if err := os.WriteFile(path, []byte("no dates in here at all\njust prose\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	entries, has, err := parseJournal(path)
	if err != nil {
		t.Fatalf("parseJournal returned error: %v", err)
	}
	if has {
		t.Fatalf("has = true, want false (no dated lines)")
	}
	if len(entries) != 0 {
		t.Fatalf("entries = %+v, want empty", entries)
	}
}

func TestParseJournal_MissingFile(t *testing.T) {
	entries, has, err := parseJournal(filepath.Join(t.TempDir(), "nope.md"))
	if err != nil {
		t.Fatalf("parseJournal returned error: %v, want nil (missing file is not an error)", err)
	}
	if has || len(entries) != 0 {
		t.Fatalf("got has=%v entries=%+v, want false/empty", has, entries)
	}
}

func TestExtractInlineBeadRef(t *testing.T) {
	cases := []struct {
		text   string
		wantID string
		wantOK bool
	}{
		{"fixed the bug, see [if-tp05] for details", "if-tp05", true},
		{"dotted subtask ref [if-w83ov.9] landed", "if-w83ov.9", true},
		{"no ref here at all", "", false},
		{"a bare bracketed word [DEFERRED] is not a bead ref", "", false},
	}
	for _, c := range cases {
		id, ok := extractInlineBeadRef(c.text)
		if ok != c.wantOK || id != c.wantID {
			t.Errorf("extractInlineBeadRef(%q) = (%q, %v), want (%q, %v)", c.text, id, ok, c.wantID, c.wantOK)
		}
	}
}

func TestMatchToBeads_ConfidentInlineRef(t *testing.T) {
	entries := []Entry{
		{Source: SourceJournal, Time: mustParse(t, "2026-05-01T00:00:00Z"), Text: "closed out [if-abc] today"},
	}
	// No bead entries needed at all — an inline ref is confident regardless
	// of proximity to any bead lifecycle event.
	got := MatchToBeads(entries, nil, 0)
	if len(got) != 1 {
		t.Fatalf("got %+v, want 1 entry", got)
	}
	if got[0].BeadID != "if-abc" || got[0].Match != MatchConfidenceConfident {
		t.Errorf("got BeadID=%q Match=%v, want if-abc/Confident", got[0].BeadID, got[0].Match)
	}
}

func TestMatchToBeads_TentativeProximityWithinWindow(t *testing.T) {
	entries := []Entry{
		{Source: SourceJournal, Time: mustParse(t, "2026-05-01T10:00:00Z"), Text: "no inline ref here"},
	}
	beadEntries := []Entry{
		{Source: SourceBead, BeadID: "if-near", Time: mustParse(t, "2026-05-01T10:04:00Z")},
		{Source: SourceBead, BeadID: "if-far", Time: mustParse(t, "2026-05-01T09:00:00Z")},
	}
	got := MatchToBeads(entries, beadEntries, 10*time.Minute)
	if got[0].BeadID != "if-near" || got[0].Match != MatchConfidenceTentative {
		t.Errorf("got BeadID=%q Match=%v, want if-near/Tentative (nearest within window)", got[0].BeadID, got[0].Match)
	}
}

func TestMatchToBeads_OutsideWindow_Unmatched(t *testing.T) {
	entries := []Entry{
		{Source: SourceDistilled, Time: mustParse(t, "2026-05-01T10:00:00Z"), Text: "modified: notes.md"},
	}
	beadEntries := []Entry{
		{Source: SourceBead, BeadID: "if-toofar", Time: mustParse(t, "2026-05-01T10:30:00Z")},
	}
	got := MatchToBeads(entries, beadEntries, 10*time.Minute)
	if got[0].BeadID != "" || got[0].Match != MatchConfidenceNone {
		t.Errorf("got BeadID=%q Match=%v, want unmatched (30min > 10min window)", got[0].BeadID, got[0].Match)
	}
}

func TestMatchToBeads_DistilledNeverInlineMatched(t *testing.T) {
	// A source=distilled entry's mechanically-generated text happens to
	// contain a bracket-and-hyphen shape that would match the inline-ref
	// regex if checked — design.md says distilled entries never get the
	// inline-ref tier checked at all, so this must still only be reachable
	// via proximity matching, never treated as a confident inline match.
	entries := []Entry{
		{Source: SourceDistilled, Time: mustParse(t, "2026-05-01T10:00:00Z"), Text: "modified: [if-decoy].md"},
	}
	beadEntries := []Entry{
		{Source: SourceBead, BeadID: "if-real", Time: mustParse(t, "2026-05-01T10:01:00Z")},
	}
	got := MatchToBeads(entries, beadEntries, 10*time.Minute)
	if got[0].BeadID != "if-real" || got[0].Match != MatchConfidenceTentative {
		t.Errorf("got BeadID=%q Match=%v, want if-real/Tentative (distilled entries skip the inline-ref tier)", got[0].BeadID, got[0].Match)
	}
}

func TestMatchToBeads_DefaultWindowAppliedWhenNonPositive(t *testing.T) {
	entries := []Entry{
		{Source: SourceJournal, Time: mustParse(t, "2026-05-01T10:00:00Z"), Text: "no ref"},
	}
	beadEntries := []Entry{
		{Source: SourceBead, BeadID: "if-x", Time: mustParse(t, "2026-05-01T10:09:00Z")}, // 9min, within default 10min
	}
	got := MatchToBeads(entries, beadEntries, 0)
	if got[0].BeadID != "if-x" || got[0].Match != MatchConfidenceTentative {
		t.Errorf("got BeadID=%q Match=%v, want if-x/Tentative under DefaultMatchWindow", got[0].BeadID, got[0].Match)
	}
}

func TestMatchToBeads_NoCandidates_Unmatched(t *testing.T) {
	// Genuinely zero candidates (nil beadEntries, no inline ref) — distinct
	// from TestMatchToBeads_OutsideWindow_Unmatched, which exercises
	// rejection of a real-but-too-far candidate. This exercises
	// nearestBeadWithinWindow's loop body never running at all (found stays
	// false from its zero value), not a per-candidate rejection.
	entries := []Entry{
		{Source: SourceJournal, Time: mustParse(t, "2026-05-01T10:00:00Z"), Text: "no ref, no candidates"},
	}
	got := MatchToBeads(entries, nil, 10*time.Minute)
	if got[0].BeadID != "" || got[0].Match != MatchConfidenceNone {
		t.Errorf("got BeadID=%q Match=%v, want unmatched (zero candidates)", got[0].BeadID, got[0].Match)
	}
}

func mustParse(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("bad test timestamp %q: %v", s, err)
	}
	return ts
}
