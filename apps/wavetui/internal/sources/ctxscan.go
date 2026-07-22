// See doc.go for the package-level contract. This file implements
// CtxScanSource — see openspec/changes/wavetui-context-pane/tasks.md [2.2]
// and its specs/wavetui/spec.md "CtxScanSource polls the current project
// and publishes a context report" Requirement.
//
// Unlike BeadsSource/OpenSpecSource, this source has NO fsnotify watcher —
// proposal.md's Scope explicitly puts "fsnotify-driven re-scans" OUT (poll +
// manual refresh chosen instead), so the only two triggers are a poll
// ticker and the operator's "r" key (ContextPane's injected refresh func,
// wired to TriggerRefresh below). Both feed the SAME requeryLoop (loop.go)
// used by every other source, which is what gives "at most one shellout in
// flight, a burst of triggers coalesces into one follow-up" for free rather
// than reimplementing that guarantee here.
package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/leonardoacosta/installfest/apps/wavetui/internal/bus"
	"github.com/leonardoacosta/installfest/apps/wavetui/internal/store"
)

// SourceNameCtxScan identifies this source — see beads.go's SourceNameBeads
// for the sibling precedent. CtxScanEvent carries its own dedicated
// Snapshot fields (store.Snapshot.CtxScan/CtxScanErr) rather than the
// generic SourceError registry, so this constant is not published via
// store.SourceErrorEvent/SourceOKEvent the way the other sources' names
// are — it exists purely for log/diagnostic consistency with those sources.
const SourceNameCtxScan = "ctxscan"

// defaultCtxScanPollSeconds is CtxScanSource's poll interval when the
// project's ctx_scan_poll_seconds config knob is unset or non-positive —
// see config.Config's doc comment and wavetui-context-pane's spec.md
// ("interval from the ctx_scan_poll_seconds config knob, default 60").
const defaultCtxScanPollSeconds = 60

// ctxScanSchemaVersion is the envelope schemaVersion `ctx-scan view-model`
// emits (apps/ctx-scan/src/cli.ts's ViewModelEnvelope) — bump alongside any
// breaking change to that envelope's own shape. A mismatch is a decode
// failure per the ADDED "a schemaVersion mismatch is an error, not a
// garbage render" scenario, never a best-effort partial decode.
const ctxScanSchemaVersion = 1

// ctxScanCLI is the shell-out boundary CtxScanSource depends on, so tests
// can inject a stub instead of actually invoking the ctx-scan binary —
// mirrors beadsCLI's precedent (beads.go).
type ctxScanCLI interface {
	ViewModel(ctx context.Context, root string) ([]byte, error)
}

// execCtxScanCLI shells out to the real `ctx-scan` binary. A missing/
// unreachable binary (e.g. never `bun link`ed into PATH) surfaces through
// runJSON's own error wrapping exactly like beadsCLI's execBeadsCLI does —
// see this source's requeryOnce for how that becomes a degrade-to-badge
// CtxScanEvent rather than a panic.
type execCtxScanCLI struct{}

func (execCtxScanCLI) ViewModel(ctx context.Context, root string) ([]byte, error) {
	return runJSON(ctx, "ctx-scan", "view-model", "--root", root)
}

// --- ctx-scan JSON envelope decode shapes (unexported — mirrors
// apps/ctx-scan/src/render/view-model.ts's RenderViewModel and
// apps/ctx-scan/src/model.ts's Band/Truncation; toReport below maps the
// decoded shape into store.CtxScanReport, the exported, wavetui-native
// type ContextPane actually reads) ---

type ctxScanEnvelopeJSON struct {
	SchemaVersion int                  `json:"schemaVersion"`
	ViewModel     ctxScanViewModelJSON `json:"viewModel"`
}

type ctxScanViewModelJSON struct {
	SchemaVersion int                  `json:"schemaVersion"`
	Root          string               `json:"root"`
	GeneratedAt   string               `json:"generatedAt"`
	Projects      []ctxScanProjectJSON `json:"projects"`
}

type ctxScanProjectJSON struct {
	Name    string             `json:"name"`
	Path    string             `json:"path"`
	Classes []ctxScanClassJSON `json:"classes"`
}

type ctxScanClassJSON struct {
	Cls         string                `json:"cls"`
	Label       string                `json:"label"`
	Documents   []ctxScanDocumentJSON `json:"documents"`
	TotalTokens float64               `json:"totalTokens"`
	WorstBand   string                `json:"worstBand"`
}

type ctxScanDocumentJSON struct {
	Path           string             `json:"path"`
	DisplayName    string             `json:"displayName"`
	Cls            string             `json:"cls"`
	Tier           int                `json:"tier"`
	Origin         string             `json:"origin"`
	RawChars       float64            `json:"rawChars"`
	EffectiveChars float64            `json:"effectiveChars"`
	EstTokens      float64            `json:"estTokens"`
	Truncations    []ctxScanTruncJSON `json:"truncations"`
	Bands          []ctxScanBandJSON  `json:"bands"`
	WorstBand      string             `json:"worstBand"`
}

type ctxScanBandJSON struct {
	Rule     string  `json:"rule"`
	Band     string  `json:"band"`
	Measured float64 `json:"measured"`
	Limit    float64 `json:"limit"`
}

type ctxScanTruncJSON struct {
	Raw       float64 `json:"raw"`
	Effective float64 `json:"effective"`
	Cap       string  `json:"cap"`
}

// toReport maps a decoded ctxScanEnvelopeJSON into the exported
// store.CtxScanReport ContextPane renders — per proposal.md's "Scan root
// decision," wavetui always passes its own repo root as `--root`, which
// ctx-scan's own discovery collapses to at most one project (see
// discovery.ts: the root itself qualifies as a project when it carries a
// marker file). Taking projects[0] here — rather than modeling the full
// fleet — is what encodes that single-project scope on the Go side; a root
// that discovered zero projects (no CLAUDE.md/.claude/.mcp.json marker at
// that path) yields an empty-but-valid report (ProjectName/ProjectPath ""),
// never an error.
func toReport(env ctxScanEnvelopeJSON) *store.CtxScanReport {
	vm := env.ViewModel
	report := &store.CtxScanReport{
		SchemaVersion: env.SchemaVersion,
		Root:          vm.Root,
	}
	if t, err := time.Parse(time.RFC3339, vm.GeneratedAt); err == nil {
		report.GeneratedAt = t
	}
	if len(vm.Projects) == 0 {
		return report
	}
	p := vm.Projects[0]
	report.ProjectName = p.Name
	report.ProjectPath = p.Path
	report.Classes = make([]store.CtxScanClass, len(p.Classes))
	for i, c := range p.Classes {
		report.Classes[i] = store.CtxScanClass{
			Class:       c.Cls,
			Label:       c.Label,
			TotalTokens: c.TotalTokens,
			WorstBand:   c.WorstBand,
			Documents:   toDocuments(c.Documents),
		}
	}
	return report
}

func toDocuments(docs []ctxScanDocumentJSON) []store.CtxScanDocument {
	out := make([]store.CtxScanDocument, len(docs))
	for i, d := range docs {
		bands := make([]store.CtxScanBand, len(d.Bands))
		for j, b := range d.Bands {
			bands[j] = store.CtxScanBand{Rule: b.Rule, Band: b.Band, Measured: b.Measured, Limit: b.Limit}
		}
		truncs := make([]store.CtxScanTruncation, len(d.Truncations))
		for j, t := range d.Truncations {
			truncs[j] = store.CtxScanTruncation{Raw: t.Raw, Effective: t.Effective, Cap: t.Cap}
		}
		out[i] = store.CtxScanDocument{
			Path:           d.Path,
			DisplayName:    d.DisplayName,
			Class:          d.Cls,
			Tier:           d.Tier,
			Origin:         d.Origin,
			RawChars:       d.RawChars,
			EffectiveChars: d.EffectiveChars,
			EstTokens:      d.EstTokens,
			WorstBand:      d.WorstBand,
			Bands:          bands,
			Truncations:    truncs,
		}
	}
	return out
}

// CtxScanSource shells out to `ctx-scan view-model --root <repo-root>` on a
// poll ticker plus a coalesced manual-refresh trigger, publishing the
// decoded result (or a failure) as a store.CtxScanEvent. See this file's
// header doc for why there is no fsnotify watcher here, unlike
// BeadsSource/OpenSpecSource.
type CtxScanSource struct {
	root string
	bus  *bus.Bus
	cli  ctxScanCLI
	poll time.Duration

	// loop is both the coalescing trigger CtxScanSource.TriggerRefresh
	// signals and the serializer Run drives — see requeryLoop's own doc
	// comment (loop.go) for the "at most one shellout in flight, a burst of
	// Trigger calls collapses to one follow-up" guarantee this reuses
	// verbatim. Constructed in NewCtxScanSource (not lazily inside Run) so
	// TriggerRefresh is safe to call — and simply queues a request — even
	// before Run's goroutine has started.
	loop *requeryLoop

	// failCount/backoff mirror BeadsSource's own retry-backoff fields
	// (beads.go) — same backoffDelay helper, same "cap the delay at the
	// poll interval" convention.
	failCount int

	// afterQuery is a test-only hook, see BeadsSource.afterQuery.
	afterQuery func()
}

// NewCtxScanSource constructs a CtxScanSource rooted at root (wavetui's own
// repo root — never a fleet-wide root, per proposal.md's Scan root
// decision) that publishes onto b. pollSeconds is the project's
// ctx_scan_poll_seconds config value; <= 0 falls back to
// defaultCtxScanPollSeconds.
func NewCtxScanSource(root string, b *bus.Bus, pollSeconds int) *CtxScanSource {
	poll := time.Duration(pollSeconds) * time.Second
	if pollSeconds <= 0 {
		poll = defaultCtxScanPollSeconds * time.Second
	}
	return &CtxScanSource{
		root: root,
		bus:  b,
		cli:  execCtxScanCLI{},
		poll: poll,
		loop: newRequeryLoop(),
	}
}

// TriggerRefresh requests an immediate re-scan — the "r" key's injected
// refresh func (ContextPane, ui/ctxscanpane.go). Non-blocking, and coalesced
// with any concurrently in-flight or already-queued scan (requeryLoop.
// Trigger) — per spec.md's "the manual-refresh trigger SHALL be a
// non-blocking signal into the source's goroutine; no UI code invokes the
// CLI directly." Safe to call before Run has started (see s.loop's doc
// comment) — the request is simply served once Run's loop.Run goroutine is
// up.
func (s *CtxScanSource) TriggerRefresh() {
	s.loop.Trigger()
}

// Run drives the poll ticker and the coalesced requeryLoop until ctx is
// cancelled. Every goroutine here is derived from ctx (the same "no
// goroutine without a cancellable context" audit requirement
// beads.go/openspec.go's Run already document).
func (s *CtxScanSource) Run(ctx context.Context) error {
	go s.loop.Run(ctx, s.requeryWithBackoff)
	s.loop.Trigger() // initial scan

	ticker := time.NewTicker(s.poll)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			s.loop.Trigger()
		}
	}
}

// requeryWithBackoff wraps requeryOnce with BeadsSource's own retry-backoff
// convention (beads.go's Run): a failure schedules a delayed re-Trigger
// rather than looping tight, and any success resets failCount to 0.
func (s *CtxScanSource) requeryWithBackoff(ctx context.Context) {
	if s.requeryOnce(ctx) {
		s.failCount = 0
		return
	}
	s.failCount++
	delay := backoffDelay(s.failCount, s.poll)
	go func() {
		select {
		case <-ctx.Done():
		case <-time.After(delay):
			s.loop.Trigger()
		}
	}()
}

// requeryOnce runs `ctx-scan view-model`, decodes and schema-checks the
// envelope, and publishes the result. Returns true on success. Any
// exec/decode/schema-version failure publishes a CtxScanEvent carrying only
// Err (nil Report) — Store.Apply's CtxScanEvent case is what actually
// retains the last-good report; this source never re-publishes stale data
// itself, unlike BeadsSource/OpenSpecSource's markStale (which republishes
// each item with Stale=true) — CtxScanReport has no per-field staleness
// concept, so "retain what Store already has" is Store's job alone here.
func (s *CtxScanSource) requeryOnce(ctx context.Context) bool {
	if s.afterQuery != nil {
		defer s.afterQuery()
	}

	raw, err := s.cli.ViewModel(ctx, s.root)
	if err != nil {
		s.publishError(fmt.Errorf("ctx-scan view-model: %w", err))
		return false
	}

	var env ctxScanEnvelopeJSON
	if err := json.Unmarshal(raw, &env); err != nil {
		s.publishError(fmt.Errorf("ctx-scan view-model: malformed json: %w", err))
		return false
	}

	if env.SchemaVersion != ctxScanSchemaVersion {
		s.publishError(fmt.Errorf(
			"ctx-scan view-model: schemaVersion mismatch: got %d, want %d",
			env.SchemaVersion, ctxScanSchemaVersion,
		))
		return false
	}

	s.bus.Publish(store.CtxScanEvent{Report: toReport(env)})
	return true
}

func (s *CtxScanSource) publishError(err error) {
	s.bus.Publish(store.CtxScanEvent{Err: err.Error()})
}
