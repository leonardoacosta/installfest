package sources

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/leonardoacosta/installfest/apps/wavetui/internal/bus"
	"github.com/leonardoacosta/installfest/apps/wavetui/internal/config"
	"github.com/leonardoacosta/installfest/apps/wavetui/internal/store"
)

// stubOpenspecParser is the test double for openspecParser — no real
// filesystem walk happens in a unit test that uses this.
type stubOpenspecParser struct {
	items []store.Item
	err   error
	calls atomic.Int32
}

func (s *stubOpenspecParser) Parse(context.Context, string, config.Config) ([]store.Item, error) {
	s.calls.Add(1)
	if s.err != nil {
		return nil, s.err
	}
	return s.items, nil
}

func makeProposal(t *testing.T, changesDir, slug, body, tasks string) {
	t.Helper()
	dir := filepath.Join(changesDir, slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "proposal.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if tasks != "" {
		if err := os.WriteFile(filepath.Join(dir, "tasks.md"), []byte(tasks), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// --- (a) debounce coalescing --------------------------------------------

func TestOpenSpecSourceDebounceCoalescesBurst(t *testing.T) {
	dir := t.TempDir()
	changesDir := filepath.Join(dir, "openspec", "changes")
	makeProposal(t, changesDir, "my-proposal",
		"# Proposal: my-proposal — a thing\n\n## Context\n- nothing blocking\n",
		"- [x] [1.1] done\n- [ ] [1.2] todo\n")

	b := bus.New()
	src := NewOpenSpecSource(dir, b, config.Config{})
	src.debounce = 200 * time.Millisecond
	src.poll = time.Hour

	var calls atomic.Int32
	src.afterQuery = func() { calls.Add(1) }

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		if err := src.Run(ctx); err != nil {
			t.Errorf("Run: %v", err)
		}
		close(done)
	}()

	eventually(t, time.Second, func() bool { return calls.Load() >= 1 })
	time.Sleep(50 * time.Millisecond)
	calls.Store(0)

	tasksPath := filepath.Join(changesDir, "my-proposal", "tasks.md")
	for i := 0; i < 15; i++ {
		content := "- [x] [1.1] done\n- [ ] [1.2] todo\n- [ ] iteration " + string(rune('a'+i)) + "\n"
		if err := os.WriteFile(tasksPath, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		time.Sleep(2 * time.Millisecond)
	}

	time.Sleep(600 * time.Millisecond)
	cancel()
	<-done

	if got := calls.Load(); got != 1 {
		t.Fatalf("Parse called %d times after burst, want exactly 1", got)
	}
}

// --- (b) failure keeps last-good snapshot + marks SourceError -----------

func TestOpenSpecSourceFailureKeepsLastGoodAndMarksError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b, st := newWiredStore(t, ctx)

	stub := &stubOpenspecParser{items: []store.Item{{ID: "prop-1", Kind: store.KindProposal, Title: "one"}}}
	src := NewOpenSpecSource(t.TempDir(), b, config.Config{})
	src.parser = stub

	src.requery(ctx)
	eventually(t, time.Second, func() bool {
		snap := st.Snapshot()
		return len(snap.Items) == 1 && snap.Items[0].ID == "prop-1"
	})

	stub.err = errors.New("read openspec/changes: permission denied")
	src.requery(ctx)

	eventually(t, time.Second, func() bool {
		snap := st.Snapshot()
		if len(snap.Items) != 1 || snap.Items[0].ID != "prop-1" || !snap.Items[0].Stale {
			return false
		}
		for _, e := range snap.Errors {
			if e.Source == SourceNameOpenSpec {
				return true
			}
		}
		return false
	})
}

// --- (c) missing-directory startup degradation ---------------------------

func TestOpenSpecSourceMissingChangesDirPublishesUnavailableNoPanic(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	b, st := newWiredStore(t, ctx)

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "openspec"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Deliberately no openspec/changes/ subdirectory.

	src := NewOpenSpecSource(dir, b, config.Config{})
	src.poll = time.Hour

	errCh := make(chan error, 1)
	go func() { errCh <- src.Run(ctx) }()

	eventually(t, time.Second, func() bool {
		for _, e := range st.Snapshot().Errors {
			if e.Source == SourceNameOpenSpec {
				return true
			}
		}
		return false
	})

	// Create changes/ with a proposal and confirm live transition to
	// available, no restart (task 2.3).
	changesDir := filepath.Join(dir, "openspec", "changes")
	makeProposal(t, changesDir, "late-arrival", "# Proposal: late-arrival — appeared later\n", "")

	eventually(t, time.Second, func() bool {
		snap := st.Snapshot()
		if len(snap.Errors) != 0 {
			return false
		}
		for _, item := range snap.Items {
			if item.ID == "late-arrival" {
				return true
			}
		}
		return false
	})

	cancel()
	if err := <-errCh; err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
}

// --- also missing when openspec/ itself is absent (deeper missing case) --

func TestOpenSpecSourceMissingOpenspecDirPublishesUnavailableNoPanic(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b, st := newWiredStore(t, ctx)

	dir := t.TempDir() // no openspec/ at all
	src := NewOpenSpecSource(dir, b, config.Config{})
	src.poll = time.Hour

	errCh := make(chan error, 1)
	go func() { errCh <- src.Run(ctx) }()

	eventually(t, time.Second, func() bool {
		for _, e := range st.Snapshot().Errors {
			if e.Source == SourceNameOpenSpec {
				return true
			}
		}
		return false
	})

	cancel()
	if err := <-errCh; err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
}

// --- real parse correctness (fsOpenspecParser / parseOneProposal) -------

func TestParseOneProposalRealFiles(t *testing.T) {
	dir := t.TempDir()
	changesDir := filepath.Join(dir, "openspec", "changes")
	makeProposal(t, changesDir, "wavetui-core",
		"# Proposal: wavetui-core — event bus, Store\n\n"+
			"## Context\n- blocked: decision - waiting on Leo's call (see if-9999)\n- unrelated bullet\n",
		"- [x] [1.1] one\n- [x] [1.2] two\n- [ ] [2.1] three\n")

	item := parseOneProposal(changesDir, "wavetui-core")

	if item.ID != "wavetui-core" || item.Kind != store.KindProposal {
		t.Fatalf("unexpected ID/Kind: %+v", item)
	}
	if item.Title != "wavetui-core — event bus, Store" {
		t.Fatalf("Title = %q, want the parsed proposal.md H1 text", item.Title)
	}
	if item.Blocker == nil || item.Blocker.Type != "decision" || item.Blocker.Ref != "if-9999" {
		t.Fatalf("want blocker parsed from ## Context, got %+v", item.Blocker)
	}
	if item.TaskProgress == nil || item.TaskProgress.Done != 2 || item.TaskProgress.Total != 3 {
		t.Fatalf("TaskProgress = %+v, want {Done:2 Total:3}", item.TaskProgress)
	}
	if item.CreatedAt.IsZero() {
		t.Fatal("want CreatedAt populated from proposal.md mtime")
	}
}

func TestParseOneProposalToleratesMissingFiles(t *testing.T) {
	dir := t.TempDir()
	changesDir := filepath.Join(dir, "openspec", "changes")
	if err := os.MkdirAll(filepath.Join(changesDir, "empty-shell"), 0o755); err != nil {
		t.Fatal(err)
	}

	item := parseOneProposal(changesDir, "empty-shell")
	if item.ID != "empty-shell" || item.Title != "empty-shell" {
		t.Fatalf("want slug-derived fallback title, got %+v", item)
	}
	if item.Blocker != nil || item.TaskProgress != nil {
		t.Fatalf("want no blocker/task progress for a shell dir, got %+v", item)
	}
}

func TestParseProposalsSkipsArchiveAndDotfiles(t *testing.T) {
	dir := t.TempDir()
	changesDir := filepath.Join(dir, "openspec", "changes")
	makeProposal(t, changesDir, "real-one", "# Proposal: real-one — x\n", "")
	if err := os.MkdirAll(filepath.Join(changesDir, "archive", "2026-01-01-old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(changesDir, ".hidden"), 0o755); err != nil {
		t.Fatal(err)
	}

	items, err := parseProposals(changesDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != "real-one" {
		t.Fatalf("want exactly [real-one], got %+v", items)
	}
}
