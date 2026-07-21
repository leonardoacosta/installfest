// See spawn.go for the Spawner/TmuxSpawner contract under test here —
// tasks.md [2.1]/[2.2]: the before/after conductor-list diff that discovers
// a spawned pane's ID, and the prompt-template render. All calls go through
// mockSpawnRunner — no real tmux/cc-tmux invocation ever happens through it.
package dispatch

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/leonardoacosta/installfest/apps/wavetui/internal/store"
)

// mockSpawnRunner is a hermetic stand-in for spawnRunner.
type mockSpawnRunner struct {
	conductorListFn    func(ctx context.Context) ([]byte, error)
	conductorSpawnTask func(ctx context.Context, promptText string) error
	calls              []string
	promptTextSeen     string
}

func (m *mockSpawnRunner) ConductorList(ctx context.Context) ([]byte, error) {
	m.calls = append(m.calls, "ConductorList")
	if m.conductorListFn != nil {
		return m.conductorListFn(ctx)
	}
	return []byte(`[]`), nil
}

func (m *mockSpawnRunner) ConductorSpawnTask(ctx context.Context, promptText string) error {
	m.calls = append(m.calls, "ConductorSpawnTask")
	m.promptTextSeen = promptText
	if m.conductorSpawnTask != nil {
		return m.conductorSpawnTask(ctx, promptText)
	}
	return nil
}

// --- Spawn: diff-based pane ID discovery ------------------------------------

func TestSpawnReturnsExactlyOneNewPaneID(t *testing.T) {
	call := 0
	runner := &mockSpawnRunner{
		conductorListFn: func(ctx context.Context) ([]byte, error) {
			call++
			if call == 1 {
				return []byte(`[{"id":"%1"},{"id":"%2"}]`), nil
			}
			return []byte(`[{"id":"%1"},{"id":"%2"},{"id":"%3"}]`), nil
		},
	}
	s := &TmuxSpawner{runner: runner}

	pane, err := s.Spawn(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Spawn error: %v", err)
	}
	if pane != "%3" {
		t.Fatalf("want new pane %%3, got %q", pane)
	}

	want := []string{"ConductorList", "ConductorSpawnTask", "ConductorList"}
	if len(runner.calls) != len(want) {
		t.Fatalf("want calls %v, got %v", want, runner.calls)
	}
	for i, c := range want {
		if runner.calls[i] != c {
			t.Fatalf("call %d: want %q, got %q (full: %v)", i, c, runner.calls[i], runner.calls)
		}
	}
	if runner.promptTextSeen != "hello" {
		t.Fatalf("want promptText %q passed through, got %q", "hello", runner.promptTextSeen)
	}
}

func TestSpawnErrorsWhenNoNewPaneAppears(t *testing.T) {
	runner := &mockSpawnRunner{
		conductorListFn: func(ctx context.Context) ([]byte, error) {
			return []byte(`[{"id":"%1"}]`), nil // identical before and after
		},
	}
	s := &TmuxSpawner{runner: runner}

	_, err := s.Spawn(context.Background(), "hello")
	if !errors.Is(err, ErrSpawnNoNewPane) {
		t.Fatalf("want ErrSpawnNoNewPane, got %v", err)
	}
}

func TestSpawnErrorsWhenMultipleNewPanesAppear(t *testing.T) {
	call := 0
	runner := &mockSpawnRunner{
		conductorListFn: func(ctx context.Context) ([]byte, error) {
			call++
			if call == 1 {
				return []byte(`[]`), nil
			}
			// A racing second spawn from elsewhere landed between our two
			// list calls -- ambiguous, must not guess.
			return []byte(`[{"id":"%1"},{"id":"%2"}]`), nil
		},
	}
	s := &TmuxSpawner{runner: runner}

	_, err := s.Spawn(context.Background(), "hello")
	if err == nil {
		t.Fatal("want an error for an ambiguous (2+) new-pane diff, got nil")
	}
}

func TestSpawnPropagatesDispatchFailureWithoutSecondList(t *testing.T) {
	runner := &mockSpawnRunner{
		conductorSpawnTask: func(ctx context.Context, promptText string) error {
			return errors.New("cc-tmux: no claude binary on PATH")
		},
	}
	s := &TmuxSpawner{runner: runner}

	_, err := s.Spawn(context.Background(), "hello")
	if err == nil {
		t.Fatal("want an error when ConductorSpawnTask fails, got nil")
	}
	// Exactly one ConductorList (the "before" snapshot) then the failed
	// dispatch -- no "after" list call once dispatch itself errored.
	want := []string{"ConductorList", "ConductorSpawnTask"}
	if len(runner.calls) != len(want) {
		t.Fatalf("want calls %v, got %v", want, runner.calls)
	}
}

func TestSpawnPropagatesBeforeListFailure(t *testing.T) {
	runner := &mockSpawnRunner{
		conductorListFn: func(ctx context.Context) ([]byte, error) {
			return nil, errors.New("cc-tmux: not found")
		},
	}
	s := &TmuxSpawner{runner: runner}

	_, err := s.Spawn(context.Background(), "hello")
	if err == nil {
		t.Fatal("want an error when the before-list call fails, got nil")
	}
	if len(runner.calls) != 1 || runner.calls[0] != "ConductorList" {
		t.Fatalf("want a single ConductorList call before failing, got %v", runner.calls)
	}
}

func TestSpawnPropagatesMalformedJSON(t *testing.T) {
	runner := &mockSpawnRunner{
		conductorListFn: func(ctx context.Context) ([]byte, error) {
			return []byte(`not json`), nil
		},
	}
	s := &TmuxSpawner{runner: runner}

	if _, err := s.Spawn(context.Background(), "hello"); err == nil {
		t.Fatal("want an error on malformed conductor list --json output, got nil")
	}
}

func TestTmuxSpawnerSatisfiesSpawnerInterface(t *testing.T) {
	var _ Spawner = NewTmuxSpawner()
}

// --- RenderSpawnPrompt -------------------------------------------------------

func TestRenderSpawnPromptContainsApplyReferenceAndPersistenceMandate(t *testing.T) {
	item := store.Item{
		ID:    "if-abcd",
		Kind:  store.KindBead,
		Title: "Decide on X",
		Blocker: &store.BlockerNote{
			Type:   "decision",
			Reason: "need Leo's call",
			Ref:    "if-xyz",
		},
	}

	prompt := RenderSpawnPrompt(item)

	if !strings.Contains(prompt, "Reference: /apply if-abcd") {
		t.Fatalf("want the /apply <item.ID> reference line, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "MUST persist the resolution") {
		t.Fatalf("want the persistence-mandate text, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "decision") || !strings.Contains(prompt, "need Leo's call") {
		t.Fatalf("want blocker type/reason rendered, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "bd comment if-abcd") {
		t.Fatalf("want the bead persistence instruction naming item.ID, got:\n%s", prompt)
	}
}

func TestRenderSpawnPromptOpenSpecItemNamesProposalPath(t *testing.T) {
	item := store.Item{
		ID:      "wavetui-decision-lanes",
		Kind:    store.KindProposal,
		Title:   "Decision lanes",
		Blocker: &store.BlockerNote{Type: "review", Reason: "needs a second look"},
	}

	prompt := RenderSpawnPrompt(item)

	wantPath := "openspec/changes/wavetui-decision-lanes/proposal.md"
	if !strings.Contains(prompt, wantPath) {
		t.Fatalf("want derived proposal path %q, got:\n%s", wantPath, prompt)
	}
	if !strings.Contains(prompt, "Reference: /apply wavetui-decision-lanes") {
		t.Fatalf("want the /apply <item.ID> reference line, got:\n%s", prompt)
	}
}

func TestRenderSpawnPromptHandlesNilBlocker(t *testing.T) {
	item := store.Item{ID: "if-nilb", Kind: store.KindBead, Title: "no blocker set"}

	// Must not panic on a nil Blocker -- defensive per RenderSpawnPrompt's
	// doc comment (a lane action should only ever be offered for a blocked
	// item, but the render function itself does not assume that invariant).
	prompt := RenderSpawnPrompt(item)
	if !strings.Contains(prompt, "Reference: /apply if-nilb") {
		t.Fatalf("want the /apply <item.ID> reference line even with a nil Blocker, got:\n%s", prompt)
	}
}
