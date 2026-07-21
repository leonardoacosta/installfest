package timeline

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// gitFixtureRoot creates a temp dir, git-inits it, and returns the root
// path. Fixture helper only — never touches this repo's own real git
// history. Author/committer identity and dates are injected via env vars
// so the fixture never depends on (or is affected by) any global git
// config present in the sandbox running the test.
func gitFixtureRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	runGit(t, root, nil, "init", "-q", "-b", "main")
	return root
}

// runGit runs a git subcommand rooted at dir, with extraEnv appended to
// the current environment (used to inject deterministic author/committer
// identity and dates for a commit).
func runGit(t *testing.T, dir string, extraEnv []string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), extraEnv...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// commitEnv returns the GIT_*_NAME/EMAIL/DATE env vars for a deterministic
// fixture commit at the given RFC3339 timestamp.
func commitEnv(ts string) []string {
	return []string{
		"GIT_AUTHOR_NAME=timeline-test",
		"GIT_AUTHOR_EMAIL=timeline-test@example.com",
		"GIT_COMMITTER_NAME=timeline-test",
		"GIT_COMMITTER_EMAIL=timeline-test@example.com",
		"GIT_AUTHOR_DATE=" + ts,
		"GIT_COMMITTER_DATE=" + ts,
	}
}

func TestOpenSpecArchiveSource_EmptySlug(t *testing.T) {
	root := gitFixtureRoot(t)
	s := NewOpenSpecArchiveSource(root)

	res, err := s.Query(context.Background(), "")
	if err != nil {
		t.Fatalf("Query returned error: %v", err)
	}
	if res.Availability != Available || len(res.Entries) != 0 {
		t.Fatalf("Result = %+v, want empty Available result", res)
	}
}

func TestOpenSpecArchiveSource_NoMatch_ReturnsEmptyNotError(t *testing.T) {
	root := gitFixtureRoot(t)
	// No openspec/changes/archive/ directory at all.
	s := NewOpenSpecArchiveSource(root)

	res, err := s.Query(context.Background(), "some-proposal")
	if err != nil {
		t.Fatalf("Query returned error: %v, want nil (no-match is not an error)", err)
	}
	if res.Availability != Available {
		t.Fatalf("Availability = %v, want Available (no-match is not Unavailable either)", res.Availability)
	}
	if len(res.Entries) != 0 {
		t.Fatalf("Entries = %+v, want empty", res.Entries)
	}
}

func TestOpenSpecArchiveSource_MatchResolvesArchiveTimestamp(t *testing.T) {
	root := gitFixtureRoot(t)
	archiveSubdir := filepath.Join("openspec", "changes", "archive", "2026-07-10-cc-tmux-plugin")
	if err := os.MkdirAll(filepath.Join(root, archiveSubdir), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, archiveSubdir, "proposal.md"), []byte("# archived\n"), 0o644); err != nil {
		t.Fatalf("write proposal.md: %v", err)
	}

	const wantTS = "2026-07-10T15:04:05-04:00"
	runGit(t, root, nil, "add", archiveSubdir)
	runGit(t, root, commitEnv(wantTS), "commit", "-q", "-m", "archive cc-tmux-plugin")

	s := NewOpenSpecArchiveSource(root)
	res, err := s.Query(context.Background(), "cc-tmux-plugin")
	if err != nil {
		t.Fatalf("Query returned error: %v", err)
	}
	if res.Availability != Available {
		t.Fatalf("Availability = %v, want Available", res.Availability)
	}
	if len(res.Entries) != 1 {
		t.Fatalf("Entries = %+v, want exactly 1", res.Entries)
	}

	e := res.Entries[0]
	if e.Source != SourceArchive {
		t.Errorf("Source = %q, want %q", e.Source, SourceArchive)
	}
	if e.Precision != PrecisionTimestamp {
		t.Errorf("Precision = %v, want PrecisionTimestamp", e.Precision)
	}
	wantTime, err := time.Parse(time.RFC3339, wantTS)
	if err != nil {
		t.Fatalf("bad test fixture timestamp: %v", err)
	}
	if !e.Time.Equal(wantTime) {
		t.Errorf("Time = %v, want %v", e.Time, wantTime)
	}
}

func TestOpenSpecArchiveSource_SlugMatchIsSuffixNotBareSubstring(t *testing.T) {
	root := gitFixtureRoot(t)
	archiveSubdir := filepath.Join("openspec", "changes", "archive", "2026-07-10-cc-tmux-plugin")
	if err := os.MkdirAll(filepath.Join(root, archiveSubdir), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, archiveSubdir, "proposal.md"), []byte("# archived\n"), 0o644); err != nil {
		t.Fatalf("write proposal.md: %v", err)
	}
	runGit(t, root, nil, "add", archiveSubdir)
	runGit(t, root, commitEnv("2026-07-10T00:00:00Z"), "commit", "-q", "-m", "archive")

	s := NewOpenSpecArchiveSource(root)

	// "cc-tmux" is a substring of the real archived dir's slug
	// ("cc-tmux-plugin") but is not itself an archived proposal — a bare
	// substring match would incorrectly resolve this to the same
	// directory. The glob pattern requires "*-<slug>" to match the whole
	// tail of the directory name, so this must return no match.
	res, err := s.Query(context.Background(), "cc-tmux")
	if err != nil {
		t.Fatalf("Query returned error: %v", err)
	}
	if len(res.Entries) != 0 {
		t.Fatalf("Entries = %+v, want empty (substring-only slug must not match)", res.Entries)
	}
}

func TestOpenSpecArchiveSource_NotAGitRepo_ReturnsError(t *testing.T) {
	root := t.TempDir() // deliberately not git-initialized
	archiveSubdir := filepath.Join("openspec", "changes", "archive", "2026-07-10-some-proposal")
	if err := os.MkdirAll(filepath.Join(root, archiveSubdir), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	s := NewOpenSpecArchiveSource(root)
	_, err := s.Query(context.Background(), "some-proposal")
	if err == nil {
		t.Fatalf("Query returned nil error for a non-git root, want a git-invocation error")
	}
}
