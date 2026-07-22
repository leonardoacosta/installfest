package sources

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// stubCtxScanCLI is the test double for ctxScanCLI — no real ctx-scan
// shellout ever happens in a unit test that uses this. Mirrors
// stubBeadsCLI's precedent (beads_test.go).
type stubCtxScanCLI struct {
	json  string
	err   error
	calls atomic.Int32
	// gate, when non-nil, is read from before ViewModel returns — lets a
	// test hold a call "in flight" to exercise trigger coalescing (see
	// TestCtxScanSourceTriggerCoalescing). nil means "return immediately",
	// the common case for every other test in this file.
	gate chan struct{}
}

func (s *stubCtxScanCLI) ViewModel(ctx context.Context, _ string) ([]byte, error) {
	s.calls.Add(1)
	if s.gate != nil {
		select {
		case <-s.gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if s.err != nil {
		return nil, s.err
	}
	return []byte(s.json), nil
}

// validEnvelopeFixture is a decodable ctx-scan `view-model --json` envelope
// (schemaVersion 1) carrying one project, one class, and one document with
// a RED-banded A8 verdict — enough surface to assert every field toReport
// maps.
const validEnvelopeFixture = `{
  "schemaVersion": 1,
  "viewModel": {
    "schemaVersion": 1,
    "root": "/tmp/fixture-root",
    "generatedAt": "2026-01-01T00:00:00.000Z",
    "fleet": {"bars": [], "maxTotalTokens": 0},
    "projects": [
      {
        "name": "fixture-proj",
        "path": "/tmp/fixture-root",
        "classes": [
          {
            "cls": "claude-md-chain",
            "label": "CLAUDE.md Chain",
            "totalTokens": 25,
            "worstBand": "RED",
            "documents": [
              {
                "path": "/tmp/fixture-root/CLAUDE.md",
                "displayName": "CLAUDE.md",
                "cls": "claude-md-chain",
                "tier": 1,
                "origin": "project",
                "rawChars": 2000,
                "effectiveChars": 2000,
                "estTokens": 500,
                "truncations": [],
                "bands": [{"rule": "A8", "band": "RED", "measured": 500, "limit": 200}],
                "worstBand": "RED"
              }
            ]
          }
        ],
        "totalTokens": 25
      }
    ],
    "contentByPath": {}
  }
}`

// schemaMismatchEnvelopeFixture is byte-identical to validEnvelopeFixture
// except its envelope-level schemaVersion is 2 (unsupported).
const schemaMismatchEnvelopeFixture = `{"schemaVersion": 2, "viewModel": {"schemaVersion": 2, "root": "/tmp/fixture-root", "generatedAt": "", "projects": []}}`

// --- (a) decode correctness ------------------------------------------------

func TestCtxScanSourceDecodesValidFixture(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b, st := newWiredStore(t, ctx)

	src := NewCtxScanSource("/tmp/fixture-root", b, 60)
	src.cli = &stubCtxScanCLI{json: validEnvelopeFixture}

	if !src.requeryOnce(ctx) {
		t.Fatal("requeryOnce should succeed decoding a valid fixture")
	}

	eventually(t, time.Second, func() bool {
		return st.Snapshot().CtxScan != nil
	})

	snap := st.Snapshot()
	if snap.CtxScanErr != "" {
		t.Fatalf("want empty CtxScanErr on success, got %q", snap.CtxScanErr)
	}
	report := snap.CtxScan
	if report == nil {
		t.Fatal("want non-nil CtxScan report")
	}
	if report.SchemaVersion != 1 {
		t.Fatalf("SchemaVersion = %d, want 1", report.SchemaVersion)
	}
	if report.ProjectName != "fixture-proj" || report.ProjectPath != "/tmp/fixture-root" {
		t.Fatalf("Project = %q/%q, want fixture-proj//tmp/fixture-root", report.ProjectName, report.ProjectPath)
	}
	if len(report.Classes) != 1 || report.Classes[0].Class != "claude-md-chain" {
		t.Fatalf("Classes = %+v, want one claude-md-chain class", report.Classes)
	}
	cls := report.Classes[0]
	if len(cls.Documents) != 1 {
		t.Fatalf("Documents = %+v, want exactly 1", cls.Documents)
	}
	doc := cls.Documents[0]
	if doc.WorstBand != "RED" || len(doc.Bands) != 1 || doc.Bands[0].Rule != "A8" {
		t.Fatalf("doc = %+v, want a single RED A8 band", doc)
	}
}

// --- (b) schemaVersion mismatch is an error, last-good retained -----------

func TestCtxScanSourceSchemaVersionMismatchKeepsLastGoodAndPublishesError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b, st := newWiredStore(t, ctx)

	src := NewCtxScanSource("/tmp/fixture-root", b, 60)
	src.cli = &stubCtxScanCLI{json: validEnvelopeFixture}
	if !src.requeryOnce(ctx) {
		t.Fatal("setup: first requeryOnce should succeed")
	}
	eventually(t, time.Second, func() bool { return st.Snapshot().CtxScan != nil })
	firstGood := st.Snapshot().CtxScan

	src.cli = &stubCtxScanCLI{json: schemaMismatchEnvelopeFixture}
	if src.requeryOnce(ctx) {
		t.Fatal("requeryOnce should report failure on a schemaVersion mismatch")
	}

	eventually(t, time.Second, func() bool { return st.Snapshot().CtxScanErr != "" })
	snap := st.Snapshot()
	if !strings.Contains(snap.CtxScanErr, "schemaVersion") {
		t.Fatalf("CtxScanErr = %q, want it to mention schemaVersion", snap.CtxScanErr)
	}
	if snap.CtxScan == nil || snap.CtxScan.ProjectName != firstGood.ProjectName {
		t.Fatalf("last-good report lost after a schemaVersion mismatch: got %+v, want %+v", snap.CtxScan, firstGood)
	}
}

// --- (c) exec failure is an error, last-good retained ----------------------

func TestCtxScanSourceExecFailureKeepsLastGoodAndPublishesError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b, st := newWiredStore(t, ctx)

	src := NewCtxScanSource("/tmp/fixture-root", b, 60)
	src.cli = &stubCtxScanCLI{json: validEnvelopeFixture}
	if !src.requeryOnce(ctx) {
		t.Fatal("setup: first requeryOnce should succeed")
	}
	eventually(t, time.Second, func() bool { return st.Snapshot().CtxScan != nil })
	firstGood := st.Snapshot().CtxScan

	src.cli = &stubCtxScanCLI{err: errors.New("exec: \"ctx-scan\": executable file not found in $PATH")}
	if src.requeryOnce(ctx) {
		t.Fatal("requeryOnce should report failure on an exec error")
	}

	eventually(t, time.Second, func() bool { return st.Snapshot().CtxScanErr != "" })
	snap := st.Snapshot()
	if snap.CtxScan == nil || snap.CtxScan.ProjectName != firstGood.ProjectName {
		t.Fatalf("last-good report lost after an exec failure: got %+v, want %+v", snap.CtxScan, firstGood)
	}
	if !strings.Contains(snap.CtxScanErr, "executable file not found") {
		t.Fatalf("CtxScanErr = %q, want it to surface the underlying exec error", snap.CtxScanErr)
	}
}

// --- (d) malformed JSON is a decode error, never a panic -------------------

func TestCtxScanSourceMalformedJSONIsError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b, st := newWiredStore(t, ctx)

	src := NewCtxScanSource("/tmp/fixture-root", b, 60)
	src.cli = &stubCtxScanCLI{json: "not json"}
	if src.requeryOnce(ctx) {
		t.Fatal("requeryOnce should report failure on malformed JSON")
	}
	eventually(t, time.Second, func() bool { return st.Snapshot().CtxScanErr != "" })
	if snap := st.Snapshot(); snap.CtxScan != nil {
		t.Fatalf("want nil CtxScan when no prior success ever landed, got %+v", snap.CtxScan)
	}
}

// --- (e) N rapid triggers coalesce into exactly one follow-up invocation --

func TestCtxScanSourceTriggerCoalescing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b, _ := newWiredStore(t, ctx)

	gate := make(chan struct{})
	cli := &stubCtxScanCLI{json: validEnvelopeFixture, gate: gate}

	// Poll interval set far longer than this test's runtime so only manual
	// triggers (plus the initial scan) drive requeryOnce — isolates trigger
	// coalescing from the ticker.
	src := NewCtxScanSource("/tmp/fixture-root", b, 3600)
	src.cli = cli

	done := make(chan struct{})
	go func() {
		_ = src.Run(ctx)
		close(done)
	}()

	// Wait for the initial scan to start (and block on gate).
	eventually(t, time.Second, func() bool { return cli.calls.Load() == 1 })

	// A burst of manual refreshes while the first call is still in flight —
	// per spec.md's "rapid manual refreshes coalesce into one invocation"
	// scenario, this must produce exactly one follow-up call, not one per
	// Trigger.
	for i := 0; i < 20; i++ {
		src.TriggerRefresh()
	}

	close(gate) // release the in-flight call (and every call after it)

	eventually(t, time.Second, func() bool { return cli.calls.Load() == 2 })
	time.Sleep(100 * time.Millisecond) // let any runaway extra calls surface
	if got := cli.calls.Load(); got != 2 {
		t.Fatalf("ViewModel called %d times after a 20-trigger burst, want exactly 2 (initial + 1 coalesced follow-up)", got)
	}

	cancel()
	<-done
}
