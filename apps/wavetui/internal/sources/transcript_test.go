package sources

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/leonardoacosta/installfest/apps/wavetui/internal/bus"
	"github.com/leonardoacosta/installfest/apps/wavetui/internal/store"
)

// stubTranscriptCLI is the test double for transcriptCLI — no real `bd`
// shellout ever happens in a unit test.
type stubTranscriptCLI struct {
	claimsJSON string
	err        error
}

func (s *stubTranscriptCLI) ClaimedItems(context.Context) ([]byte, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.claimsJSON == "" {
		return []byte("[]"), nil
	}
	return []byte(s.claimsJSON), nil
}

// stubPaneStateSource is the test double for PaneStateSource.
type stubPaneStateSource struct {
	states map[string]struct {
		pane, state string
	}
}

func newStubPaneStateSource() *stubPaneStateSource {
	return &stubPaneStateSource{states: make(map[string]struct{ pane, state string })}
}

func (s *stubPaneStateSource) set(sessionID, paneID, state string) {
	s.states[sessionID] = struct{ pane, state string }{paneID, state}
}

func (s *stubPaneStateSource) StateForSession(sessionID string) (string, string, bool) {
	v, ok := s.states[sessionID]
	if !ok {
		return "", "", false
	}
	return v.pane, v.state, true
}

// newTestTranscriptSource builds a TranscriptSource rooted at a temp
// "project root" with its transcript directory pre-created under a
// separate temp "projects dir" — never touches a real home directory or a
// real transcript file.
func newTestTranscriptSource(t *testing.T, b *bus.Bus) (*TranscriptSource, string) {
	t.Helper()
	root := t.TempDir()
	projectsDir := t.TempDir()

	src := NewTranscriptSource(root, b)
	src.projectsDir = projectsDir
	src.cli = &stubTranscriptCLI{}
	src.zombieAfter = 0 // tests control "quiet" via lastActivity directly

	dir := src.transcriptDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return src, dir
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func appendFile(t *testing.T, path, content string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
}

// --- offset tracking -------------------------------------------------------

func TestTranscriptSourceOffsetTracksAcrossReads(t *testing.T) {
	b := bus.New()
	src, dir := newTestTranscriptSource(t, b)
	path := filepath.Join(dir, "sess-1.jsonl")

	writeFile(t, path, `{"type":"user","sessionId":"sess-1","cwd":"/proj","timestamp":"2026-01-01T00:00:00Z","message":{"role":"user","content":"/apply if-abc"}}`+"\n")
	src.tailAll(context.Background())

	agg := src.sessions["sess-1"]
	if agg == nil {
		t.Fatal("expected session sess-1 to be tracked after first tail")
	}
	if src.files[path].offset == 0 {
		t.Fatalf("expected non-zero offset after first tail, got %d", src.files[path].offset)
	}
	firstOffset := src.files[path].offset

	// A second tail with no new bytes must not reprocess anything (offset
	// unchanged, no duplicate user message appended).
	src.tailAll(context.Background())
	if src.files[path].offset != firstOffset {
		t.Fatalf("offset changed with no new bytes: %d -> %d", firstOffset, src.files[path].offset)
	}
	if got := len(agg.userMessages); got != 1 {
		t.Fatalf("expected exactly 1 user message after re-tail with no new bytes, got %d", got)
	}

	// Append a second line — only the NEW bytes should be read.
	appendFile(t, path, `{"type":"user","sessionId":"sess-1","message":{"role":"user","content":"second"}}`+"\n")
	src.tailAll(context.Background())
	if got := len(agg.userMessages); got != 2 {
		t.Fatalf("expected 2 user messages after appending a second line, got %d", got)
	}
	if src.files[path].offset <= firstOffset {
		t.Fatalf("expected offset to advance past %d, got %d", firstOffset, src.files[path].offset)
	}
}

// --- partial-line buffering --------------------------------------------

func TestTranscriptSourcePartialLineBuffered(t *testing.T) {
	b := bus.New()
	src, dir := newTestTranscriptSource(t, b)
	path := filepath.Join(dir, "sess-2.jsonl")

	// No trailing newline: an incomplete line.
	writeFile(t, path, `{"type":"user","sessionId":"sess-2","message":{"role":"user","content":"partial`)
	src.tailAll(context.Background())

	if _, ok := src.sessions["sess-2"]; ok {
		t.Fatal("expected no session to be recorded from an unterminated partial line")
	}
	if len(src.files[path].remainder) == 0 {
		t.Fatal("expected the partial line to be held in the remainder buffer")
	}

	// Complete the line.
	appendFile(t, path, `"}}`+"\n")
	src.tailAll(context.Background())

	agg, ok := src.sessions["sess-2"]
	if !ok {
		t.Fatal("expected session sess-2 to be recorded once the line is completed")
	}
	if len(agg.userMessages) != 1 || agg.userMessages[0] != "partial" {
		t.Fatalf("expected the completed message to be processed, got %+v", agg.userMessages)
	}
}

// --- truncation resets the offset --------------------------------------

func TestTranscriptSourceTruncationResetsOffset(t *testing.T) {
	b := bus.New()
	src, dir := newTestTranscriptSource(t, b)
	path := filepath.Join(dir, "sess-3.jsonl")

	writeFile(t, path, `{"type":"user","sessionId":"sess-3","message":{"role":"user","content":"first-generation content padding"}}`+"\n")
	src.tailAll(context.Background())
	if src.files[path].offset == 0 {
		t.Fatal("expected a non-zero offset after the first tail")
	}

	// Replace with a shorter file — simulates truncation/replacement.
	writeFile(t, path, `{"type":"user","sessionId":"sess-3","message":{"role":"user","content":"new"}}`+"\n")
	src.tailAll(context.Background())

	agg := src.sessions["sess-3"]
	if agg == nil {
		t.Fatal("expected session sess-3 to still be tracked")
	}
	last := agg.userMessages[len(agg.userMessages)-1]
	if last != "new" {
		t.Fatalf("expected the re-read-from-scratch content 'new', got %q", last)
	}
}

// --- tolerant decode -----------------------------------------------------

func TestTranscriptSourceTolerantDecodeIgnoresUnknownTypes(t *testing.T) {
	b := bus.New()
	src, dir := newTestTranscriptSource(t, b)
	path := filepath.Join(dir, "sess-4.jsonl")

	// All ten observed real type values (design.md's dump) plus one
	// synthetic unknown type, plus one malformed JSON line — none of these
	// should crash or error, and the malformed line must not stop
	// subsequent well-formed lines from being processed.
	lines := []string{
		`{"type":"last-prompt","sessionId":"sess-4"}`,
		`{"type":"custom-title","sessionId":"sess-4"}`,
		`{"type":"agent-name","sessionId":"sess-4"}`,
		`{"type":"mode","sessionId":"sess-4"}`,
		`{"type":"permission-mode","sessionId":"sess-4"}`,
		`{"type":"attachment","sessionId":"sess-4"}`,
		`{"type":"file-history-snapshot","sessionId":"sess-4"}`,
		`{"type":"file-history-delta","sessionId":"sess-4"}`,
		`{"type":"system","sessionId":"sess-4"}`,
		`{this is not valid json`,
		`{"type":"totally-unheard-of-future-type","sessionId":"sess-4"}`,
		`{"type":"user","sessionId":"sess-4","message":{"role":"user","content":"still alive"}}`,
	}
	content := ""
	for _, l := range lines {
		content += l + "\n"
	}
	writeFile(t, path, content)

	src.tailAll(context.Background()) // must not panic

	agg, ok := src.sessions["sess-4"]
	if !ok {
		t.Fatal("expected session sess-4 to be tracked despite unknown/malformed lines")
	}
	if len(agg.userMessages) != 1 || agg.userMessages[0] != "still alive" {
		t.Fatalf("expected the well-formed user line after the malformed one to still be processed, got %+v", agg.userMessages)
	}
}

// --- context gauge --------------------------------------------------------

func TestSessionAggContextPctAndThreshold(t *testing.T) {
	agg := newSessionAgg("s")
	agg.contextTokens = 69
	if pct := agg.contextPct(100); IsHandoffThreshold(pct) {
		t.Fatalf("69/100 = %.1f%% should be below threshold", pct)
	}
	agg.contextTokens = 70
	if pct := agg.contextPct(100); !IsHandoffThreshold(pct) {
		t.Fatalf("70/100 = %.1f%% should cross the threshold", pct)
	}
	// Never exceeds 100%.
	agg.contextTokens = 1000
	if pct := agg.contextPct(100); pct != 100 {
		t.Fatalf("expected clamp to 100, got %.1f", pct)
	}
	// Zero/negative window: no divide-by-zero panic.
	if pct := agg.contextPct(0); pct != 0 {
		t.Fatalf("expected 0 for a zero window, got %.1f", pct)
	}
}

// --- zombie detection: two independent signals -----------------------------

func TestUpdateZombieInactivityAloneBadgesWithNoPaneData(t *testing.T) {
	b := bus.New()
	src, _ := newTestTranscriptSource(t, b)
	src.zombieAfter = 15 * time.Minute
	// No PaneStateSource wired at all — must fail open.

	agg := newSessionAgg("s")
	agg.lastActivity = time.Now().Add(-20 * time.Minute)
	src.updateZombie(agg)

	if !agg.zombie {
		t.Fatal("expected inactivity alone to badge zombie when no tmux data exists")
	}
}

func TestUpdateZombieActiveTmuxSuppressesBadge(t *testing.T) {
	b := bus.New()
	src, _ := newTestTranscriptSource(t, b)
	src.zombieAfter = 15 * time.Minute

	panes := newStubPaneStateSource()
	panes.set("s", "%1", "active")
	src.SetPaneStateSource(panes)

	agg := newSessionAgg("s")
	agg.lastActivity = time.Now().Add(-20 * time.Minute)
	src.updateZombie(agg)

	if agg.zombie {
		t.Fatal("expected an active tmux pane to suppress the zombie badge despite transcript inactivity")
	}
}

func TestUpdateZombieInactiveTmuxStillBadges(t *testing.T) {
	b := bus.New()
	src, _ := newTestTranscriptSource(t, b)
	src.zombieAfter = 15 * time.Minute

	panes := newStubPaneStateSource()
	panes.set("s", "%1", "idle")
	src.SetPaneStateSource(panes)

	agg := newSessionAgg("s")
	agg.lastActivity = time.Now().Add(-20 * time.Minute)
	src.updateZombie(agg)

	if !agg.zombie {
		t.Fatal("expected a non-active tmux pane state to still badge zombie (cross-check confirms inactivity)")
	}
}

func TestUpdateZombieNotYetInactiveNeverBadges(t *testing.T) {
	b := bus.New()
	src, _ := newTestTranscriptSource(t, b)
	src.zombieAfter = 15 * time.Minute

	agg := newSessionAgg("s")
	agg.lastActivity = time.Now().Add(-1 * time.Minute)
	src.updateZombie(agg)

	if agg.zombie {
		t.Fatal("expected no zombie badge before the inactivity threshold is reached")
	}
}

// --- error classification --------------------------------------------------

func TestClassifyToolErrorRealObservedShapes(t *testing.T) {
	cases := []struct {
		name string
		text string
		want string
	}{
		{
			name: "read-first violation",
			text: "<tool_use_error>File has not been read yet. Read it first before writing to it.</tool_use_error>",
			want: errorClassReadFirst,
		},
		{
			name: "edit string-not-found",
			text: "<tool_use_error>String to replace not found in file.\nString:   some context</tool_use_error>",
			want: errorClassEditNotFound,
		},
		{
			name: "gate.sh BLOCKED",
			text: "PreToolUse:Bash hook error: [~/.claude/scripts/hooks/gate.sh]: BLOCKED: full .env dump (cat/less/head/etc. with no filter).\n",
			want: errorClassGateBlocked,
		},
		{
			name: "unrecognized shape falls back to unclassified, not dropped",
			text: "Exit code 1\nsome random tool failure nobody has a rule for",
			want: errorClassUnclassified,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyToolError(tc.text); got != tc.want {
				t.Fatalf("classifyToolError(%q) = %q, want %q", tc.text, got, tc.want)
			}
		})
	}
}

func TestHandleToolResultRecordsErrorFeedEntry(t *testing.T) {
	b := bus.New()
	src, _ := newTestTranscriptSource(t, b)
	agg := newSessionAgg("s")

	src.handleToolResult(agg, rawContentBlock{
		Type:    "tool_result",
		IsError: true,
		Content: rawJSONString(t, "<tool_use_error>File has not been read yet. Read it first before writing to it.</tool_use_error>"),
	})

	if len(agg.errors) != 1 {
		t.Fatalf("expected 1 error-feed entry, got %d", len(agg.errors))
	}
	if agg.errors[0].Class != errorClassReadFirst {
		t.Fatalf("expected class %q, got %q", errorClassReadFirst, agg.errors[0].Class)
	}

	// A non-error tool_result must not be recorded.
	src.handleToolResult(agg, rawContentBlock{Type: "tool_result", IsError: false, Content: rawJSONString(t, "ok")})
	if len(agg.errors) != 1 {
		t.Fatalf("expected a successful tool_result to add nothing, still want 1, got %d", len(agg.errors))
	}
}

// rawJSONString encodes s as a JSON string literal, for constructing a
// rawContentBlock.Content value (which is json.RawMessage — the raw bytes
// of a JSON-encoded string or block array, exactly as it would appear
// embedded in a real transcript line).
func rawJSONString(t *testing.T, s string) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// --- token meter ------------------------------------------------------------

func TestHandleAssistantLineAccumulatesTokensPerModel(t *testing.T) {
	b := bus.New()
	src, _ := newTestTranscriptSource(t, b)
	agg := newSessionAgg("s")

	src.handleAssistantLine(agg, rawTranscriptLine{
		Type: "assistant",
		Message: &rawMessage{
			Model: "claude-sonnet-5",
			Usage: &rawUsage{InputTokens: 10, CacheReadInputTokens: 5, OutputTokens: 100},
		},
	})
	src.handleAssistantLine(agg, rawTranscriptLine{
		Type: "assistant",
		Message: &rawMessage{
			Model: "claude-haiku-4-5",
			Usage: &rawUsage{InputTokens: 1, CacheReadInputTokens: 0, OutputTokens: 20},
		},
	})

	if got := agg.tokensByModel["claude-sonnet-5"]; got != 100 {
		t.Fatalf("expected 100 sonnet output tokens, got %d", got)
	}
	if got := agg.tokensByModel["claude-haiku-4-5"]; got != 20 {
		t.Fatalf("expected 20 haiku output tokens, got %d", got)
	}
	if got := agg.contextTokens; got != 16 {
		t.Fatalf("expected cumulative input+cache-read tokens 16, got %d", got)
	}
}

func TestHandleAssistantLineFlagsOpusInSidechainExecutorLane(t *testing.T) {
	b := bus.New()
	src, _ := newTestTranscriptSource(t, b)

	agg := newSessionAgg("s")
	agg.isSidechain = true
	src.handleAssistantLine(agg, rawTranscriptLine{
		Message: &rawMessage{Model: "claude-opus-4-1", Usage: &rawUsage{}},
	})
	if !agg.executorLaneFlag {
		t.Fatal("expected opus in a sidechain (executor-lane proxy) session to raise the flag")
	}

	nonSidechain := newSessionAgg("s2")
	src.handleAssistantLine(nonSidechain, rawTranscriptLine{
		Message: &rawMessage{Model: "claude-opus-4-1", Usage: &rawUsage{}},
	})
	if nonSidechain.executorLaneFlag {
		t.Fatal("expected opus in a non-sidechain (orchestrator) session to NOT raise the flag")
	}
}

// --- rate-limit signal emission ---------------------------------------------

func TestRateLimitSignalEmittedOnErrorKeywordMatch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b, st := newWiredStore(t, ctx)
	src, _ := newTestTranscriptSource(t, b)
	agg := newSessionAgg("s")

	src.handleToolResult(agg, rawContentBlock{
		Type:    "tool_result",
		IsError: true,
		Content: rawJSONString(t, "upstream connect error: rate limit exceeded (429)"),
	})

	eventually(t, time.Second, func() bool {
		return st.Snapshot().RateLimitBanner != nil
	})
}

func TestRateLimitSignalNotEmittedForUnrelatedError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b, st := newWiredStore(t, ctx)
	src, _ := newTestTranscriptSource(t, b)
	agg := newSessionAgg("s")

	src.handleToolResult(agg, rawContentBlock{
		Type:    "tool_result",
		IsError: true,
		Content: rawJSONString(t, "Exit code 1\nfile not found"),
	})

	time.Sleep(50 * time.Millisecond) // give the bus a chance to (not) deliver
	if st.Snapshot().RateLimitBanner != nil {
		t.Fatal("expected no rate-limit banner for an unrelated error")
	}
}

// --- session linkage end-to-end (exact /apply match) ------------------------

func TestTranscriptSourceLinksViaExactApplyReference(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b, st := newWiredStore(t, ctx)
	src, dir := newTestTranscriptSource(t, b)

	// Publish the base item first (as BeadsSource would).
	b.Publish(store.ItemUpsertEvent{Item: store.Item{ID: "if-abc12", Kind: store.KindBead, Title: "thing"}})

	path := filepath.Join(dir, "sess-5.jsonl")
	writeFile(t, path, `{"type":"user","sessionId":"sess-5","message":{"role":"user","content":"/apply if-abc12"}}`+"\n"+
		`{"type":"assistant","sessionId":"sess-5","message":{"model":"claude-sonnet-5","usage":{"input_tokens":5,"cache_read_input_tokens":0,"output_tokens":50}}}`+"\n")

	src.tailAll(ctx)

	eventually(t, time.Second, func() bool {
		for _, item := range st.Snapshot().Items {
			if item.ID == "if-abc12" && item.Session != nil && item.Session.SessionID == "sess-5" {
				return true
			}
		}
		return false
	})
}

// --- flattenProjectDir ------------------------------------------------------

func TestFlattenProjectDir(t *testing.T) {
	got := flattenProjectDir("/home/nyaptor/dev/personal/installfest")
	want := "-home-nyaptor-dev-personal-installfest"
	if got != want {
		t.Fatalf("flattenProjectDir() = %q, want %q", got, want)
	}
}
