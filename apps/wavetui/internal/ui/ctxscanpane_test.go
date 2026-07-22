package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/leonardoacosta/installfest/apps/wavetui/internal/store"
)

// key builds a tea.KeyPressMsg whose String() matches s, for the small set
// of keys ContextPane's HandleKey switches on ("j", "k", "r", "enter",
// "esc") — mirrors queuepane_test.go/headlessbar_test.go's own
// tea.KeyPressMsg{Text:...}/{Code: tea.KeyEnter}/{Code: tea.KeyEscape}
// construction precedent.
func key(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	default:
		return tea.KeyPressMsg{Text: s}
	}
}

// fixtureCtxScanReport builds a two-class, two-document-per-class report for
// drill-state tests.
func fixtureCtxScanReport() *store.CtxScanReport {
	return &store.CtxScanReport{
		SchemaVersion: 1,
		ProjectName:   "fixture-proj",
		ProjectPath:   "/tmp/fixture-proj",
		Classes: []store.CtxScanClass{
			{
				Class: "claude-md-chain", Label: "CLAUDE.md Chain", TotalTokens: 500, WorstBand: "RED",
				Documents: []store.CtxScanDocument{
					{Path: "/tmp/fixture-proj/CLAUDE.md", DisplayName: "CLAUDE.md", Tier: 1, Origin: "project", RawChars: 2000, EffectiveChars: 2000, EstTokens: 500, WorstBand: "RED",
						Bands: []store.CtxScanBand{{Rule: "A8", Band: "RED", Measured: 500, Limit: 200}}},
					{Path: "/tmp/fixture-proj/rules/CORE.md", DisplayName: "CORE.md", Tier: 1, Origin: "project", RawChars: 100, EffectiveChars: 100, EstTokens: 25, WorstBand: "GREEN"},
				},
			},
			{
				Class: "memory", Label: "Memory (MEMORY.md)", TotalTokens: 10, WorstBand: "GREEN",
				Documents: []store.CtxScanDocument{
					{Path: "/tmp/fixture-proj/MEMORY.md", DisplayName: "MEMORY.md", Tier: 1, Origin: "project", RawChars: 40, EffectiveChars: 40, EstTokens: 10, WorstBand: "GREEN"},
				},
			},
		},
	}
}

func TestContextPaneFocusable(t *testing.T) {
	p := NewContextPane(nil)
	if !p.Focusable() {
		t.Fatal("ContextPane must be Focusable — it joins Root's focus ring")
	}
}

func TestContextPaneScanningStateBeforeFirstReport(t *testing.T) {
	p := NewContextPane(nil)
	if got := p.View(); !strings.Contains(got, "scanning") {
		t.Fatalf("want a scanning placeholder before any report/error arrives, got %q", got)
	}
}

func TestContextPaneUnavailableBadgeNoLastGood(t *testing.T) {
	p := NewContextPane(nil)
	p.Update(store.Snapshot{CtxScanErr: "ctx-scan view-model: exec: \"ctx-scan\": executable file not found in $PATH"})

	got := p.View()
	if !strings.Contains(got, "unavailable") {
		t.Fatalf("want an unavailable badge, got %q", got)
	}
	// badgeMessageMaxLen (root.go) truncates long messages, so assert on a
	// prefix that survives truncation rather than the tail-end wording.
	if !strings.Contains(got, "ctx-scan view-model") {
		t.Fatalf("want the underlying error surfaced, got %q", got)
	}
	if strings.Contains(got, "fixture-proj") {
		t.Fatalf("want no report content with no last-good report, got %q", got)
	}
}

func TestContextPaneUnavailableBadgeWithLastGoodStillShowsStaleReport(t *testing.T) {
	p := NewContextPane(nil)
	// A successful scan first...
	p.Update(store.Snapshot{CtxScan: fixtureCtxScanReport()})
	// ...then a failure, per spec.md: "if a last-good report exists it
	// remains visible, marked stale."
	p.Update(store.Snapshot{CtxScan: fixtureCtxScanReport(), CtxScanErr: "ctx-scan view-model: timeout"})

	got := p.View()
	if !strings.Contains(got, "unavailable") {
		t.Fatalf("want an unavailable badge, got %q", got)
	}
	if !strings.Contains(got, "fixture-proj") {
		t.Fatalf("want last-good report content still rendered, got %q", got)
	}
	if !strings.Contains(strings.ToLower(got), "stale") {
		t.Fatalf("want the last-good content marked stale, got %q", got)
	}
}

func TestContextPaneClassBreakdownRendersBandsAndTokens(t *testing.T) {
	p := NewContextPane(nil)
	p.Update(store.Snapshot{CtxScan: fixtureCtxScanReport()})

	got := p.View()
	if !strings.Contains(got, "CLAUDE.md Chain") || !strings.Contains(got, "Memory (MEMORY.md)") {
		t.Fatalf("want both class labels rendered, got %q", got)
	}
	if !strings.Contains(got, "RED") || !strings.Contains(got, "GREEN") {
		t.Fatalf("want both band verdicts rendered, got %q", got)
	}
	if !strings.Contains(got, "~500") {
		t.Fatalf("want the ~-prefixed estimated-token convention, got %q", got)
	}
}

// TestContextPaneDrillDownAndBackPreservesCursor is the ADDED "drill-down
// reaches document detail and walks back" scenario: select a class, enter,
// select a document, enter, esc twice — ends back at the class breakdown
// with the cursor exactly where it started.
func TestContextPaneDrillDownAndBackPreservesCursor(t *testing.T) {
	p := NewContextPane(nil)
	p.Update(store.Snapshot{CtxScan: fixtureCtxScanReport()})

	// Move to the second class ("memory") before drilling in.
	p.HandleKey(key("j"))
	if p.level != ctxScanLevelClasses || p.classCursor != 1 {
		t.Fatalf("want classCursor=1 at classes level after one 'j', got level=%v cursor=%d", p.level, p.classCursor)
	}

	p.HandleKey(key("enter"))
	if p.level != ctxScanLevelDocuments {
		t.Fatalf("want documents level after enter, got %v", p.level)
	}
	if got := p.View(); !strings.Contains(got, "MEMORY.md") {
		t.Fatalf("want the memory class's own document listed, got %q", got)
	}

	p.HandleKey(key("enter")) // only one document in this class — descend to detail
	if p.level != ctxScanLevelDetail {
		t.Fatalf("want detail level after a second enter, got %v", p.level)
	}
	if got := p.View(); !strings.Contains(got, "tier: 1") || !strings.Contains(got, "origin: project") {
		t.Fatalf("want tier/origin in detail view, got %q", got)
	}

	p.HandleKey(key("esc"))
	if p.level != ctxScanLevelDocuments {
		t.Fatalf("want documents level after first esc, got %v", p.level)
	}

	p.HandleKey(key("esc"))
	if p.level != ctxScanLevelClasses {
		t.Fatalf("want classes level after second esc, got %v", p.level)
	}
	if p.classCursor != 1 {
		t.Fatalf("classCursor = %d, want 1 (preserved across the round trip)", p.classCursor)
	}
}

func TestContextPaneDetailViolationsAndCleanCases(t *testing.T) {
	p := NewContextPane(nil)
	p.Update(store.Snapshot{CtxScan: fixtureCtxScanReport()})

	// classCursor=0 ("claude-md-chain"), docCursor=0 ("CLAUDE.md", RED/A8).
	p.HandleKey(key("enter"))
	p.HandleKey(key("enter"))
	if got := p.View(); !strings.Contains(got, "violations:") || !strings.Contains(got, "A8") {
		t.Fatalf("want the A8 violation listed for the RED-banded document, got %q", got)
	}

	p.HandleKey(key("esc"))
	p.HandleKey(key("j")) // docCursor -> 1 ("CORE.md", GREEN, no bands)
	p.HandleKey(key("enter"))
	if got := p.View(); !strings.Contains(got, "no rubric violations") {
		t.Fatalf("want a clean document to say so explicitly, got %q", got)
	}
}

func TestContextPaneRefreshKeyInvokesInjectedFunc(t *testing.T) {
	called := 0
	p := NewContextPane(func() { called++ })
	p.Update(store.Snapshot{CtxScan: fixtureCtxScanReport()})

	p.HandleKey(key("r"))
	if called != 1 {
		t.Fatalf("want refresh invoked exactly once, got %d", called)
	}

	// "r" must work identically regardless of drill level.
	p.HandleKey(key("enter"))
	p.HandleKey(key("r"))
	if called != 2 {
		t.Fatalf("want refresh invoked from a non-classes drill level too, got %d calls", called)
	}
}

func TestContextPaneNilRefreshIsSilentNoOp(t *testing.T) {
	p := NewContextPane(nil)
	p.Update(store.Snapshot{CtxScan: fixtureCtxScanReport()})
	// Must not panic.
	p.HandleKey(key("r"))
}

func TestContextPaneCursorClampsWhenReportShrinks(t *testing.T) {
	p := NewContextPane(nil)
	p.Update(store.Snapshot{CtxScan: fixtureCtxScanReport()})
	p.HandleKey(key("j")) // classCursor -> 1

	shrunk := fixtureCtxScanReport()
	shrunk.Classes = shrunk.Classes[:1] // only "claude-md-chain" survives
	p.Update(store.Snapshot{CtxScan: shrunk})

	if p.classCursor != 0 {
		t.Fatalf("classCursor = %d, want clamped to 0 after the report shrank", p.classCursor)
	}
}
