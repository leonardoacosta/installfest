// See tmux.go for the TmuxDispatcher contract under test here — tasks.md
// [4.1]: candidate scoring, copy-mode/mid-turn-streaming refusals, and the
// bracketed-paste call sequence via a mock tmuxRunner.
package dispatch

import (
	"context"
	"errors"
	"testing"

	"github.com/leonardoacosta/installfest/apps/wavetui/internal/store"
)

// mockRunner is a hermetic stand-in for tmuxRunner — no real tmux/cc-tmux
// invocation ever happens through it. calls records every method invocation
// in order (method name only, or "method:arg" where a single distinguishing
// argument matters to a test) so sendPrompt's exact call sequence can be
// asserted.
type mockRunner struct {
	conductorListFn   func(ctx context.Context) ([]byte, error)
	conductorSwitchFn func(ctx context.Context, paneID string) error
	displayMessageFn  func(ctx context.Context, paneID, format string) (string, bool)
	showOptionFn      func(ctx context.Context, paneID, option string) (string, bool)
	loadBufferFn      func(ctx context.Context, bufName string, data []byte) error
	pasteBufferFn     func(ctx context.Context, bufName, paneID string) error
	deleteBufferFn    func(ctx context.Context, bufName string) error
	sendKeysEnterFn   func(ctx context.Context, paneID string) error

	calls []string

	// bufNames records the bufName argument seen by each of
	// LoadBuffer/PasteBuffer/DeleteBuffer, in call order, so a test can
	// assert the same generated buffer name flows through all three.
	bufNames []string
}

func (m *mockRunner) ConductorList(ctx context.Context) ([]byte, error) {
	m.calls = append(m.calls, "ConductorList")
	if m.conductorListFn != nil {
		return m.conductorListFn(ctx)
	}
	return nil, nil
}

func (m *mockRunner) ConductorSwitch(ctx context.Context, paneID string) error {
	m.calls = append(m.calls, "ConductorSwitch:"+paneID)
	if m.conductorSwitchFn != nil {
		return m.conductorSwitchFn(ctx, paneID)
	}
	return nil
}

func (m *mockRunner) DisplayMessage(ctx context.Context, paneID, format string) (string, bool) {
	m.calls = append(m.calls, "DisplayMessage:"+format)
	if m.displayMessageFn != nil {
		return m.displayMessageFn(ctx, paneID, format)
	}
	return "", false
}

func (m *mockRunner) ShowOption(ctx context.Context, paneID, option string) (string, bool) {
	m.calls = append(m.calls, "ShowOption:"+option)
	if m.showOptionFn != nil {
		return m.showOptionFn(ctx, paneID, option)
	}
	return "", false
}

func (m *mockRunner) LoadBuffer(ctx context.Context, bufName string, data []byte) error {
	m.calls = append(m.calls, "LoadBuffer")
	m.bufNames = append(m.bufNames, bufName)
	if m.loadBufferFn != nil {
		return m.loadBufferFn(ctx, bufName, data)
	}
	return nil
}

func (m *mockRunner) PasteBuffer(ctx context.Context, bufName, paneID string) error {
	m.calls = append(m.calls, "PasteBuffer:"+paneID)
	m.bufNames = append(m.bufNames, bufName)
	if m.pasteBufferFn != nil {
		return m.pasteBufferFn(ctx, bufName, paneID)
	}
	return nil
}

func (m *mockRunner) DeleteBuffer(ctx context.Context, bufName string) error {
	m.calls = append(m.calls, "DeleteBuffer")
	m.bufNames = append(m.bufNames, bufName)
	if m.deleteBufferFn != nil {
		return m.deleteBufferFn(ctx, bufName)
	}
	return nil
}

func (m *mockRunner) SendKeysEnter(ctx context.Context, paneID string) error {
	m.calls = append(m.calls, "SendKeysEnter:"+paneID)
	if m.sendKeysEnterFn != nil {
		return m.sendKeysEnterFn(ctx, paneID)
	}
	return nil
}

// --- candidate scoring ---------------------------------------------------

func TestCandidateScoreSameWindowBeatsSameSessionBeatsOther(t *testing.T) {
	self := selfContext{session: "s1", window: "3", project: "proj", branch: "main"}

	sameWindow := candidateScore(conductorPane{Session: "other-session", Window: "3"}, self)
	sameSession := candidateScore(conductorPane{Session: "s1", Window: "9"}, self)
	other := candidateScore(conductorPane{Session: "other-session", Window: "9"}, self)

	if !(sameWindow > sameSession) {
		t.Fatalf("want same-window score (%d) > same-session score (%d)", sameWindow, sameSession)
	}
	if !(sameSession > other) {
		t.Fatalf("want same-session score (%d) > other score (%d)", sameSession, other)
	}
}

func TestCandidateScoreProjectBranchAffinityBreaksTiesWithinTierOnly(t *testing.T) {
	self := selfContext{session: "s1", window: "3", project: "proj", branch: "main"}

	// Two same-session candidates: one also matches project+branch, the
	// other matches neither. Affinity must discriminate within the tier...
	sameSessionWithAffinity := candidateScore(conductorPane{Session: "s1", Window: "9", Project: "proj", Branch: "main"}, self)
	sameSessionNoAffinity := candidateScore(conductorPane{Session: "s1", Window: "9"}, self)
	if !(sameSessionWithAffinity > sameSessionNoAffinity) {
		t.Fatalf("want affinity to raise the score within a tier: %d vs %d", sameSessionWithAffinity, sameSessionNoAffinity)
	}

	// ...but never promote an "other" candidate (even with full affinity)
	// above a bare same-session candidate (no affinity at all) — the tier
	// gap (10) must exceed the largest possible affinity bonus (3).
	otherWithFullAffinity := candidateScore(conductorPane{Session: "elsewhere", Window: "9", Project: "proj", Branch: "main"}, self)
	if otherWithFullAffinity >= sameSessionNoAffinity {
		t.Fatalf("affinity must never cross a tier boundary: other-tier-with-affinity=%d >= same-session-tier=%d", otherWithFullAffinity, sameSessionNoAffinity)
	}
}

func TestScoreCandidatesTiePrompts(t *testing.T) {
	self := selfContext{} // fully unresolved: every pane scores 0
	panes := []conductorPane{
		{ID: "%1", Session: "a"},
		{ID: "%2", Session: "b"},
	}

	_, ambiguous := scoreCandidates(panes, self)
	if ambiguous == nil {
		t.Fatal("want an *AmbiguousTargetError for a tie, got nil")
	}
	if len(ambiguous.Candidates) != 2 {
		t.Fatalf("want 2 tied candidates, got %d: %+v", len(ambiguous.Candidates), ambiguous.Candidates)
	}
}

func TestScoreCandidatesSingleCandidateNeverAmbiguous(t *testing.T) {
	self := selfContext{}
	panes := []conductorPane{{ID: "%1", Session: "a"}}

	best, ambiguous := scoreCandidates(panes, self)
	if ambiguous != nil {
		t.Fatalf("want no ambiguity for a single candidate, got %+v", ambiguous)
	}
	if best.ID != "%1" {
		t.Fatalf("want the sole candidate picked, got %+v", best)
	}
}

func TestScoreCandidatesUniqueTopScoreWins(t *testing.T) {
	self := selfContext{session: "s1", window: "3"}
	panes := []conductorPane{
		{ID: "%1", Session: "s1", Window: "3"}, // same-window
		{ID: "%2", Session: "s1", Window: "9"}, // same-session
		{ID: "%3", Session: "other"},           // other
	}

	best, ambiguous := scoreCandidates(panes, self)
	if ambiguous != nil {
		t.Fatalf("want a unique winner, got ambiguity: %+v", ambiguous)
	}
	if best.ID != "%1" {
		t.Fatalf("want %%1 (same-window) to win, got %+v", best)
	}
}

// --- resolveTarget ---------------------------------------------------------

func TestResolveTargetPrefersLinkedPaneOverScoring(t *testing.T) {
	runner := &mockRunner{
		conductorListFn: func(ctx context.Context) ([]byte, error) {
			t.Fatal("ConductorList should never be called when item.Session.PaneID is already set")
			return nil, nil
		},
	}
	d := &TmuxDispatcher{runner: runner, selfPane: func() string { return "" }}

	item := store.Item{Session: &store.SessionLink{PaneID: "%42"}}
	pane, err := d.resolveTarget(context.Background(), item)
	if err != nil {
		t.Fatalf("resolveTarget error: %v", err)
	}
	if pane != "%42" {
		t.Fatalf("want linked pane %%42, got %q", pane)
	}
}

func TestResolveTargetErrNoDispatchTargetOnCLIError(t *testing.T) {
	runner := &mockRunner{
		conductorListFn: func(ctx context.Context) ([]byte, error) {
			return nil, errors.New("cc-tmux: not found")
		},
	}
	d := &TmuxDispatcher{runner: runner, selfPane: func() string { return "" }}

	_, err := d.resolveTarget(context.Background(), store.Item{})
	if !errors.Is(err, ErrNoDispatchTarget) {
		t.Fatalf("want ErrNoDispatchTarget on a CLI error, got %v", err)
	}
}

func TestResolveTargetErrNoDispatchTargetOnEmptyPanes(t *testing.T) {
	runner := &mockRunner{
		conductorListFn: func(ctx context.Context) ([]byte, error) {
			return []byte(`[]`), nil
		},
	}
	d := &TmuxDispatcher{runner: runner, selfPane: func() string { return "" }}

	_, err := d.resolveTarget(context.Background(), store.Item{})
	if !errors.Is(err, ErrNoDispatchTarget) {
		t.Fatalf("want ErrNoDispatchTarget on zero candidates, got %v", err)
	}
}

func TestResolveTargetErrNoDispatchTargetOnMalformedJSON(t *testing.T) {
	runner := &mockRunner{
		conductorListFn: func(ctx context.Context) ([]byte, error) {
			return []byte(`not json`), nil
		},
	}
	d := &TmuxDispatcher{runner: runner, selfPane: func() string { return "" }}

	_, err := d.resolveTarget(context.Background(), store.Item{})
	if !errors.Is(err, ErrNoDispatchTarget) {
		t.Fatalf("want ErrNoDispatchTarget on malformed JSON, got %v", err)
	}
}

func TestResolveTargetAmbiguousPropagates(t *testing.T) {
	runner := &mockRunner{
		conductorListFn: func(ctx context.Context) ([]byte, error) {
			return []byte(`[{"id":"%1","session":"a"},{"id":"%2","session":"b"}]`), nil
		},
	}
	d := &TmuxDispatcher{runner: runner, selfPane: func() string { return "" }}

	_, err := d.resolveTarget(context.Background(), store.Item{})
	var ambiguous *AmbiguousTargetError
	if !errors.As(err, &ambiguous) {
		t.Fatalf("want *AmbiguousTargetError, got %v", err)
	}
}

// --- checkRefusals ----------------------------------------------------------

func TestCheckRefusalsCopyMode(t *testing.T) {
	runner := &mockRunner{
		displayMessageFn: func(ctx context.Context, paneID, format string) (string, bool) {
			if format == "#{pane_in_mode}" {
				return "1", true
			}
			return "", false
		},
	}
	d := &TmuxDispatcher{runner: runner}

	err := d.checkRefusals(context.Background(), store.Item{}, "%1")
	if !errors.Is(err, ErrPaneInCopyMode) {
		t.Fatalf("want ErrPaneInCopyMode, got %v", err)
	}
}

func TestCheckRefusalsMidTurnStreaming(t *testing.T) {
	runner := &mockRunner{
		showOptionFn: func(ctx context.Context, paneID, option string) (string, bool) {
			if option == ccStateOpt {
				return "active", true
			}
			return "", false
		},
	}
	d := &TmuxDispatcher{runner: runner}

	item := store.Item{Session: &store.SessionLink{Zombie: false}}
	err := d.checkRefusals(context.Background(), item, "%1")
	if !errors.Is(err, ErrSessionStreaming) {
		t.Fatalf("want ErrSessionStreaming, got %v", err)
	}
}

func TestCheckRefusalsZombieSessionNeverRefusedAsStreaming(t *testing.T) {
	runner := &mockRunner{
		showOptionFn: func(ctx context.Context, paneID, option string) (string, bool) {
			if option == ccStateOpt {
				return "active", true // pane still reads active...
			}
			return "", false
		},
	}
	d := &TmuxDispatcher{runner: runner}

	item := store.Item{Session: &store.SessionLink{Zombie: true}} // ...but session is a known zombie
	if err := d.checkRefusals(context.Background(), item, "%1"); err != nil {
		t.Fatalf("want no refusal for a zombie session, got %v", err)
	}
}

func TestCheckRefusalsNoSessionNeverRefusedAsStreaming(t *testing.T) {
	runner := &mockRunner{
		showOptionFn: func(ctx context.Context, paneID, option string) (string, bool) {
			return "active", true
		},
	}
	d := &TmuxDispatcher{runner: runner}

	if err := d.checkRefusals(context.Background(), store.Item{}, "%1"); err != nil {
		t.Fatalf("want no refusal when item has no linked session, got %v", err)
	}
}

func TestCheckRefusalsFailOpenWhenNoData(t *testing.T) {
	runner := &mockRunner{} // every DisplayMessage/ShowOption call returns "", false by default
	d := &TmuxDispatcher{runner: runner}

	item := store.Item{Session: &store.SessionLink{Zombie: false}}
	if err := d.checkRefusals(context.Background(), item, "%1"); err != nil {
		t.Fatalf("want fail-open (no refusal) when tmux has no data for this pane, got %v", err)
	}
}

// --- sendPrompt bracketed-paste call sequence -------------------------------

func TestSendPromptExactlyThreeCallsNeverSendKeysDashL(t *testing.T) {
	runner := &mockRunner{}
	d := &TmuxDispatcher{runner: runner}

	if err := d.sendPrompt(context.Background(), "%7", "line one\nline two"); err != nil {
		t.Fatalf("sendPrompt error: %v", err)
	}

	// The three non-negotiable calls, in order, plus the deferred cleanup
	// DeleteBuffer call. tmuxRunner exposes no "send-keys -l" method at
	// all — see the interface's SendKeysEnter doc comment — so the
	// "never a single send-keys -l" invariant is structurally enforced by
	// the interface shape; this asserts the three (four, with cleanup)
	// calls that DO exist happened, in the documented order, exactly once
	// each.
	want := []string{"LoadBuffer", "PasteBuffer:%7", "SendKeysEnter:%7", "DeleteBuffer"}
	if len(runner.calls) != len(want) {
		t.Fatalf("want %d calls %v, got %d calls %v", len(want), want, len(runner.calls), runner.calls)
	}
	for i, c := range want {
		if runner.calls[i] != c {
			t.Fatalf("call %d: want %q, got %q (full sequence: %v)", i, c, runner.calls[i], runner.calls)
		}
	}

	// LoadBuffer/PasteBuffer/DeleteBuffer must all reference the SAME
	// generated buffer name.
	if len(runner.bufNames) != 3 {
		t.Fatalf("want 3 bufName-carrying calls, got %d: %v", len(runner.bufNames), runner.bufNames)
	}
	for i, name := range runner.bufNames {
		if name != runner.bufNames[0] || name == "" {
			t.Fatalf("bufNames must all match and be non-empty, got %v", runner.bufNames)
		}
		_ = i
	}
}

func TestSendPromptDeletesBufferEvenOnPasteFailure(t *testing.T) {
	runner := &mockRunner{
		pasteBufferFn: func(ctx context.Context, bufName, paneID string) error {
			return errors.New("paste failed")
		},
	}
	d := &TmuxDispatcher{runner: runner}

	err := d.sendPrompt(context.Background(), "%7", "hello")
	if err == nil {
		t.Fatal("want an error from a failing PasteBuffer, got nil")
	}
	// SendKeysEnter must never fire once paste failed.
	for _, c := range runner.calls {
		if c == "SendKeysEnter:%7" {
			t.Fatalf("SendKeysEnter must not fire after a PasteBuffer failure, got calls %v", runner.calls)
		}
	}
	// DeleteBuffer cleanup still runs via defer regardless of outcome.
	found := false
	for _, c := range runner.calls {
		if c == "DeleteBuffer" {
			found = true
		}
	}
	if !found {
		t.Fatalf("want DeleteBuffer cleanup to still run on failure, got calls %v", runner.calls)
	}
}

// --- full Dispatch integration ----------------------------------------------

func TestDispatchFullFlowLinkedPane(t *testing.T) {
	runner := &mockRunner{}
	d := &TmuxDispatcher{runner: runner, selfPane: func() string { return "" }}

	item := store.Item{Session: &store.SessionLink{PaneID: "%9", Zombie: false}}
	if err := d.Dispatch(context.Background(), item, "/apply if-p1ru"); err != nil {
		t.Fatalf("Dispatch error: %v", err)
	}

	want := []string{
		"DisplayMessage:#{pane_in_mode}",
		"ShowOption:" + ccStateOpt,
		"LoadBuffer",
		"PasteBuffer:%9",
		"SendKeysEnter:%9",
		"DeleteBuffer",
	}
	if len(runner.calls) != len(want) {
		t.Fatalf("want %d calls %v, got %d calls %v", len(want), want, len(runner.calls), runner.calls)
	}
	for i, c := range want {
		if runner.calls[i] != c {
			t.Fatalf("call %d: want %q, got %q (full sequence: %v)", i, c, runner.calls[i], runner.calls)
		}
	}
}

func TestDispatchRefusesInvalidPaneIDShape(t *testing.T) {
	runner := &mockRunner{}
	d := &TmuxDispatcher{runner: runner, selfPane: func() string { return "" }}

	// A linked session with a non-pane-ID-shaped PaneID must be refused
	// before any tmux invocation is attempted at all.
	item := store.Item{Session: &store.SessionLink{PaneID: "not-a-pane-id"}}
	if err := d.Dispatch(context.Background(), item, "prompt"); err == nil {
		t.Fatal("want an error for a non-pane-ID-shaped target, got nil")
	}
	if len(runner.calls) != 0 {
		t.Fatalf("want zero tmux calls before pane-ID validation fails, got %v", runner.calls)
	}
}

func TestDispatchPropagatesCopyModeRefusalBeforeSendPrompt(t *testing.T) {
	runner := &mockRunner{
		displayMessageFn: func(ctx context.Context, paneID, format string) (string, bool) {
			if format == "#{pane_in_mode}" {
				return "1", true
			}
			return "", false
		},
	}
	d := &TmuxDispatcher{runner: runner, selfPane: func() string { return "" }}

	item := store.Item{Session: &store.SessionLink{PaneID: "%3"}}
	err := d.Dispatch(context.Background(), item, "prompt")
	if !errors.Is(err, ErrPaneInCopyMode) {
		t.Fatalf("want ErrPaneInCopyMode, got %v", err)
	}
	for _, c := range runner.calls {
		if c == "LoadBuffer" || c == "PasteBuffer:%3" || c == "SendKeysEnter:%3" {
			t.Fatalf("no paste/send-keys call should happen after a copy-mode refusal, got %v", runner.calls)
		}
	}
}

func TestSwitchValidatesPaneIDBeforeInvoking(t *testing.T) {
	runner := &mockRunner{}
	d := &TmuxDispatcher{runner: runner}

	if err := d.Switch(context.Background(), "not-a-pane"); err == nil {
		t.Fatal("want an error for a non-pane-ID-shaped target, got nil")
	}
	if len(runner.calls) != 0 {
		t.Fatalf("want zero calls when validation fails, got %v", runner.calls)
	}

	if err := d.Switch(context.Background(), "%5"); err != nil {
		t.Fatalf("Switch error: %v", err)
	}
	if len(runner.calls) != 1 || runner.calls[0] != "ConductorSwitch:%5" {
		t.Fatalf("want a single ConductorSwitch:%%5 call, got %v", runner.calls)
	}
}
