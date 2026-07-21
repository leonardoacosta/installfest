package sources

import (
	"context"
	"testing"

	"github.com/leonardoacosta/installfest/apps/wavetui/internal/bus"
)

// stubTmuxCLI is the test double for tmuxCLI — no real tmux/ps shellout
// ever happens in a unit test.
type stubTmuxCLI struct {
	panesRaw []byte
	panesErr error

	// options maps "paneID|option" -> (value, ok).
	options map[string]struct {
		value string
		ok    bool
	}

	psRaw []byte
	psErr error
}

func newStubTmuxCLI() *stubTmuxCLI {
	return &stubTmuxCLI{options: make(map[string]struct {
		value string
		ok    bool
	})}
}

func (s *stubTmuxCLI) setOption(paneID, option, value string, ok bool) {
	s.options[paneID+"|"+option] = struct {
		value string
		ok    bool
	}{value, ok}
}

func (s *stubTmuxCLI) ListPanes(context.Context) ([]byte, error) {
	if s.panesErr != nil {
		return nil, s.panesErr
	}
	return s.panesRaw, nil
}

func (s *stubTmuxCLI) ShowOption(_ context.Context, paneID, option string) (string, bool) {
	v, ok := s.options[paneID+"|"+option]
	if !ok {
		return "", false
	}
	return v.value, v.ok
}

func (s *stubTmuxCLI) ProcessTree(context.Context) ([]byte, error) {
	if s.psErr != nil {
		return nil, s.psErr
	}
	return s.psRaw, nil
}

// --- primary path: a tagged pane is read via @cc-state, never re-derived ---

func TestTmuxSourceTaggedPaneUsesPrimaryPathOnly(t *testing.T) {
	b := bus.New()
	src := NewTmuxSource(b)

	cli := newStubTmuxCLI()
	cli.panesRaw = []byte("%1\t100\n")
	cli.setOption("%1", ccStateOpt, "active", true)
	cli.setOption("%1", ccSessionIDOpt, "sess-a", true)
	// Deliberately no ProcessTree stub data configured: since %1 is fully
	// tagged, scanOnce's untaggedPIDs list stays empty and ProcessTree is
	// never invoked at all — a tagged pane must never re-derive state via
	// the process-tree fallback (design.md's explicit requirement).
	src.cli = cli

	src.scanOnce(context.Background())

	paneID, state, ok := src.StateForSession("sess-a")
	if !ok || paneID != "%1" || state != "active" {
		t.Fatalf("StateForSession(sess-a) = (%q, %q, %v), want (%%1, active, true)", paneID, state, ok)
	}
}

// --- fallback path: an untagged pane walks the process tree ---------------

func TestTmuxSourceUntaggedPaneFallsBackToProcessTree(t *testing.T) {
	b := bus.New()
	src := NewTmuxSource(b)

	cli := newStubTmuxCLI()
	cli.panesRaw = []byte("%2\t200\n")
	// No @cc-state/@cc-session-id set for %2 at all (untagged).
	cli.psRaw = []byte("PID PPID COMMAND\n200 1 zsh\n201 200 claude\n")
	src.cli = cli

	src.scanOnce(context.Background())

	// No session-id available for this pane (fallback found no
	// @cc-session-id, only process presence) — so it can't be looked up by
	// session, but its pane-id view should show a "running" state.
	src.mu.Lock()
	ps, ok := src.byPaneID["%2"]
	src.mu.Unlock()
	if !ok {
		t.Fatal("expected pane %2 to be present via the process-tree fallback")
	}
	if ps.Tagged {
		t.Fatal("expected pane %2 to be marked Tagged=false (fallback path, not primary)")
	}
	if ps.State != "running" {
		t.Fatalf("expected fallback state 'running', got %q", ps.State)
	}
}

func TestTmuxSourceUntaggedPaneWithNoClaudeProcessReportsNoResult(t *testing.T) {
	b := bus.New()
	src := NewTmuxSource(b)

	cli := newStubTmuxCLI()
	cli.panesRaw = []byte("%3\t300\n")
	cli.psRaw = []byte("PID PPID COMMAND\n300 1 zsh\n301 300 vim\n")
	src.cli = cli

	src.scanOnce(context.Background())

	src.mu.Lock()
	_, ok := src.byPaneID["%3"]
	src.mu.Unlock()
	if ok {
		t.Fatal("expected no result (not a guess) for a pane with no claude descendant")
	}
}

// --- no positional inference between panes ---------------------------------

func TestTmuxSourceNoPositionalInferenceBetweenPanes(t *testing.T) {
	b := bus.New()
	src := NewTmuxSource(b)

	cli := newStubTmuxCLI()
	// %10 is tagged active; %11 is untagged with no claude descendant.
	// %11 must NOT inherit %10's "active" state despite being adjacent.
	cli.panesRaw = []byte("%10\t400\n%11\t401\n")
	cli.setOption("%10", ccStateOpt, "active", true)
	cli.setOption("%10", ccSessionIDOpt, "sess-tagged", true)
	cli.psRaw = []byte("PID PPID COMMAND\n400 1 zsh\n401 1 zsh\n402 401 vim\n")
	src.cli = cli

	src.scanOnce(context.Background())

	src.mu.Lock()
	_, untaggedPresent := src.byPaneID["%11"]
	src.mu.Unlock()
	if untaggedPresent {
		t.Fatal("expected the untracked neighbor to have no inferred state")
	}
	if paneID, state, ok := src.StateForSession("sess-tagged"); !ok || paneID != "%10" || state != "active" {
		t.Fatalf("expected the tagged pane's own state to be unaffected, got (%q, %q, %v)", paneID, state, ok)
	}
}

// --- parsing helpers ---------------------------------------------------------

func TestParsePaneList(t *testing.T) {
	entries := parsePaneList([]byte("%1\t100\n%2\t200\n\nmalformed-line\n"))
	if len(entries) != 2 {
		t.Fatalf("expected 2 valid entries (malformed/blank lines skipped), got %d: %+v", len(entries), entries)
	}
	if entries[0].paneID != "%1" || entries[0].pid != 100 {
		t.Fatalf("unexpected first entry: %+v", entries[0])
	}
}

func TestFindClaudeDescendants(t *testing.T) {
	ps := []byte("PID PPID COMMAND\n1 0 systemd\n100 1 zsh\n101 100 claude\n200 1 zsh\n201 200 vim\n")
	found := findClaudeDescendants(ps, []int64{100, 200})
	if !found[100] {
		t.Fatal("expected pid 100 (has a claude child) to be found")
	}
	if found[200] {
		t.Fatal("expected pid 200 (no claude descendant) to NOT be found")
	}
}
