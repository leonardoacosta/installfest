// See openspec/changes/wavetui-context-pane/tasks.md [3.1] and
// specs/wavetui/spec.md's "ContextPane renders the context breakdown as a
// drill-down tab" Requirement. ContextPane implements wavetui-core's Pane
// interface and additionally implements Sizeable (see root.go) — the same
// extra, Pane-interface-external hook MemoryTimelinePane already
// establishes (memorytimelinepane.go). Unlike MemoryTimelinePane, this
// pane's content DOES come from the generic Snapshot (Snapshot.CtxScan/
// CtxScanErr, populated by CtxScanSource) — it does not implement
// SelectionAware/TimelineAware, since it has no on-demand per-item query;
// it renders the single current project's whole report regardless of
// QueuePane's own selection.
package ui

import (
	"fmt"
	"math"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/leonardoacosta/installfest/apps/wavetui/internal/store"
)

// defaultContextPaneWidth sizes the pane before the first real
// tea.WindowSizeMsg arrives — Root.layout() calls SetSize once it does,
// mirroring MemoryTimelinePane's defaultMemoryTimelineWidth precedent.
const defaultContextPaneWidth = 96

// ctxScanDrillLevel is ContextPane's three-level drill state — spec.md:
// "class breakdown ... documents within a selected class ... single-
// document detail."
type ctxScanDrillLevel int

const (
	ctxScanLevelClasses ctxScanDrillLevel = iota
	ctxScanLevelDocuments
	ctxScanLevelDetail
)

// ContextPane renders the current project's ctx-scan context report as the
// full-screen "[3] Context" tab. classCursor/docCursor are kept as separate
// fields (rather than a single stack) specifically so "esc" walking back up
// restores the exact row the operator was on — spec.md's "returns to the
// class breakdown with cursor position preserved" scenario — without any
// extra bookkeeping: descending never resets the level ABOVE the one being
// entered, only initializes the level being entered (see descend).
type ContextPane struct {
	report *store.CtxScanReport
	errMsg string

	level       ctxScanDrillLevel
	classCursor int
	docCursor   int

	width, height int

	// refresh is the injected "r" action — CtxScanSource.TriggerRefresh in
	// production (wired by cmd/wavetui/main.go, task 3.3). Per spec.md's
	// "the pane's update path never calls a source or CLI" invariant, this
	// is the ONLY thing "r" ever calls; the shellout itself stays inside
	// CtxScanSource's own goroutine. nil is tolerated as a silent no-op —
	// same convention QueuePane.SetDispatcher/SetSpawner establish for their
	// own nil-until-wired dependencies (queuepane.go) — so a bare
	// NewContextPane(nil) in a test never panics on "r".
	refresh func()
}

// NewContextPane constructs an empty ContextPane (no report yet) whose "r"
// key invokes refresh — pass nil in a test that never presses "r".
func NewContextPane(refresh func()) *ContextPane {
	return &ContextPane{width: defaultContextPaneWidth, refresh: refresh}
}

// Update implements Pane. Unlike MemoryTimelinePane, ContextPane's content
// comes straight from the Snapshot's dedicated fields (no on-demand query) —
// see this file's header doc for why SelectionAware/TimelineAware are not
// implemented here.
func (p *ContextPane) Update(snap store.Snapshot) Pane {
	p.report = snap.CtxScan
	p.errMsg = snap.CtxScanErr
	p.clampCursors()
	return p
}

// clampCursors keeps classCursor/docCursor in bounds after a Snapshot
// refreshes p.report to a differently-sized report (e.g. a class gained or
// lost documents on rescan) — never resets the drill level itself except
// when the report goes fully nil (nothing left to drill into).
func (p *ContextPane) clampCursors() {
	if p.report == nil {
		p.level = ctxScanLevelClasses
		p.classCursor = 0
		p.docCursor = 0
		return
	}
	p.classCursor = clampIndex(p.classCursor, len(p.report.Classes))
	if cls := p.currentClass(); cls != nil {
		p.docCursor = clampIndex(p.docCursor, len(cls.Documents))
	} else {
		p.docCursor = 0
	}
}

func clampIndex(idx, length int) int {
	if length == 0 {
		return 0
	}
	if idx < 0 {
		return 0
	}
	if idx >= length {
		return length - 1
	}
	return idx
}

// currentClass returns the class under classCursor, or nil when the report
// is nil or has no classes.
func (p *ContextPane) currentClass() *store.CtxScanClass {
	if p.report == nil || p.classCursor < 0 || p.classCursor >= len(p.report.Classes) {
		return nil
	}
	return &p.report.Classes[p.classCursor]
}

// currentDocument returns the document under docCursor within the current
// class, or nil when there is no current class or no documents.
func (p *ContextPane) currentDocument() *store.CtxScanDocument {
	cls := p.currentClass()
	if cls == nil || p.docCursor < 0 || p.docCursor >= len(cls.Documents) {
		return nil
	}
	return &cls.Documents[p.docCursor]
}

// Focusable implements Pane — the Context tab joins the focus ring, same as
// MemoryTimelinePane.
func (p *ContextPane) Focusable() bool { return true }

// SetSize implements the Sizeable optional interface (root.go), mirroring
// MemoryTimelinePane.SetSize's precedent.
func (p *ContextPane) SetSize(width, height int) {
	p.width = width
	p.height = height
}

// HandleKey handles this pane's own keys — deliberately outside the Pane
// interface, the same rationale QueuePane.HandleKey's doc comment already
// establishes (root.go type-asserts to *ContextPane to reach this, see
// root.go's handleKey). "r" ALWAYS invokes refresh regardless of drill
// level, per spec.md: "The r key SHALL invoke the injected refresh trigger
// only ... on whatever drill level is currently shown."
func (p *ContextPane) HandleKey(msg tea.KeyPressMsg) {
	switch msg.String() {
	case "r":
		if p.refresh != nil {
			p.refresh()
		}
	case "j", "down":
		p.moveCursor(1)
	case "k", "up":
		p.moveCursor(-1)
	case "enter":
		p.descend()
	case "esc":
		p.ascend()
	}
}

// moveCursor moves the cursor for whichever level is active by delta rows,
// clamped (never wrapping) — the detail level has no list to move within,
// so it's a no-op there.
func (p *ContextPane) moveCursor(delta int) {
	if p.report == nil {
		return
	}
	switch p.level {
	case ctxScanLevelClasses:
		p.classCursor = clampIndex(p.classCursor+delta, len(p.report.Classes))
	case ctxScanLevelDocuments:
		if cls := p.currentClass(); cls != nil {
			p.docCursor = clampIndex(p.docCursor+delta, len(cls.Documents))
		}
	}
}

// descend implements "enter": classes -> documents -> detail. A no-op at
// the deepest level, or when the level being entered has nothing to show
// (e.g. a class with zero documents) — the row-level content itself
// (renderDocuments/renderDetail's own "no documents"/"no longer available"
// text) is what communicates that, not a refusal to descend.
func (p *ContextPane) descend() {
	switch p.level {
	case ctxScanLevelClasses:
		if p.currentClass() == nil {
			return
		}
		p.level = ctxScanLevelDocuments
		p.docCursor = 0
	case ctxScanLevelDocuments:
		if p.currentDocument() == nil {
			return
		}
		p.level = ctxScanLevelDetail
	}
}

// ascend implements "esc": detail -> documents -> classes. classCursor/
// docCursor are untouched here — see the ContextPane doc comment for why
// that alone is what gives "cursor position preserved" on the way back up.
func (p *ContextPane) ascend() {
	switch p.level {
	case ctxScanLevelDetail:
		p.level = ctxScanLevelDocuments
	case ctxScanLevelDocuments:
		p.level = ctxScanLevelClasses
	}
}

// View implements Pane.
func (p *ContextPane) View() string {
	width := p.width
	if width <= 0 {
		width = defaultContextPaneWidth
	}

	var lines []string
	lines = append(lines, lipgloss.NewStyle().Bold(true).Render("Context"))

	if p.errMsg != "" {
		lines = append(lines, contextUnavailableStyle.Render("ctx-scan unavailable: "+truncateBadgeMessage(p.errMsg)))
	}

	if p.report == nil {
		if p.errMsg == "" {
			lines = append(lines, lipgloss.NewStyle().Faint(true).Render("scanning…"))
		}
		return lipgloss.NewStyle().Width(width).Render(strings.Join(lines, "\n"))
	}

	if p.errMsg != "" {
		lines = append(lines, lipgloss.NewStyle().Faint(true).Render("showing last-good scan (stale)"))
	}

	switch p.level {
	case ctxScanLevelDocuments:
		lines = append(lines, p.renderDocuments()...)
	case ctxScanLevelDetail:
		lines = append(lines, p.renderDetail()...)
	default:
		lines = append(lines, p.renderClasses()...)
	}

	lines = clipContextLines(lines, p.height)
	return lipgloss.NewStyle().Width(width).Render(strings.Join(lines, "\n"))
}

// contextUnavailableStyle mirrors root.go's unavailableBadges color (203) —
// the same error/significant-weight convention used fleet-wide in this
// package, rather than introducing a new arbitrary color.
var contextUnavailableStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("203"))

func (p *ContextPane) renderClasses() []string {
	header := lipgloss.NewStyle().Faint(true).Render(
		fmt.Sprintf("%s — j/k: move, enter: drill in, r: refresh", contextHeaderText(p.report)),
	)
	lines := []string{header}

	classes := p.report.Classes
	if len(classes) == 0 {
		return append(lines, lipgloss.NewStyle().Faint(true).Render("no context classes found"))
	}
	for i, c := range classes {
		row := fmt.Sprintf("%s%-24s %8s  %s", cursorMarker(i == p.classCursor), c.Label, fmtEstTokens(c.TotalTokens), c.WorstBand)
		lines = append(lines, ctxScanRowStyle(c.WorstBand, i == p.classCursor).Render(row))
	}
	return lines
}

func (p *ContextPane) renderDocuments() []string {
	cls := p.currentClass()
	if cls == nil {
		return []string{lipgloss.NewStyle().Faint(true).Render("class no longer available — esc: back")}
	}

	header := lipgloss.NewStyle().Faint(true).Render(
		fmt.Sprintf("%s — j/k: move, enter: detail, esc: back, r: refresh", cls.Label),
	)
	lines := []string{header}

	if len(cls.Documents) == 0 {
		return append(lines, lipgloss.NewStyle().Faint(true).Render("no documents in this class"))
	}
	for i, d := range cls.Documents {
		trunc := ""
		if len(d.Truncations) > 0 {
			trunc = " [truncated]"
		}
		row := fmt.Sprintf("%s%-32s %8s  %s%s", cursorMarker(i == p.docCursor), d.DisplayName, fmtEstTokens(d.EstTokens), d.WorstBand, trunc)
		lines = append(lines, ctxScanRowStyle(d.WorstBand, i == p.docCursor).Render(row))
	}
	return lines
}

func (p *ContextPane) renderDetail() []string {
	doc := p.currentDocument()
	if doc == nil {
		return []string{lipgloss.NewStyle().Faint(true).Render("document no longer available — esc: back")}
	}

	lines := []string{
		lipgloss.NewStyle().Bold(true).Render(doc.DisplayName),
		lipgloss.NewStyle().Faint(true).Render("esc: back  r: refresh"),
		"path: " + doc.Path,
		fmt.Sprintf("tier: %d   origin: %s", doc.Tier, doc.Origin),
		fmt.Sprintf("raw chars: %s   effective chars: %s   est tokens: %s", fmtCount(doc.RawChars), fmtCount(doc.EffectiveChars), fmtEstTokens(doc.EstTokens)),
	}

	if len(doc.Truncations) > 0 {
		lines = append(lines, lipgloss.NewStyle().Bold(true).Render("truncations:"))
		for _, t := range doc.Truncations {
			lines = append(lines, fmt.Sprintf("  %s: %s -> %s chars", t.Cap, fmtCount(t.Raw), fmtCount(t.Effective)))
		}
	}

	violations := ctxScanViolations(doc.Bands)
	if len(violations) == 0 {
		lines = append(lines, lipgloss.NewStyle().Faint(true).Render("no rubric violations"))
	} else {
		lines = append(lines, lipgloss.NewStyle().Bold(true).Render("violations:"))
		for _, b := range violations {
			row := fmt.Sprintf("  %s: %s (measured %s, limit %s)", b.Rule, b.Band, fmtCount(b.Measured), fmtCount(b.Limit))
			lines = append(lines, ctxScanBandStyle(b.Band).Render(row))
		}
	}
	return lines
}

// ctxScanViolations returns every band whose verdict is not GREEN — see
// store.CtxScanDocument.Bands' own doc comment for why this derives
// "violations" from Bands rather than a separate field.
func ctxScanViolations(bands []store.CtxScanBand) []store.CtxScanBand {
	var out []store.CtxScanBand
	for _, b := range bands {
		if b.Band != "GREEN" {
			out = append(out, b)
		}
	}
	return out
}

func contextHeaderText(r *store.CtxScanReport) string {
	if r.ProjectName == "" {
		return "Context: no project discovered at scan root"
	}
	return "Context: " + r.ProjectName
}

func cursorMarker(selected bool) string {
	if selected {
		return "> "
	}
	return "  "
}

// ctxScanBandStyle colors GREEN/AMBER/RED consistently across the classes,
// documents, and detail-violations views. RED reuses color "203" — the same
// error-weight color root.go's unavailableBadges/conflictWarningStyle
// already use fleet-wide — rather than introducing a fourth arbitrary
// color for the same "worst" severity.
func ctxScanBandStyle(band string) lipgloss.Style {
	switch band {
	case "GREEN":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("70"))
	case "AMBER":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	case "RED":
		return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("203"))
	default:
		return lipgloss.NewStyle().Faint(true)
	}
}

// ctxScanRowStyle layers the cursor-selected weight (Bold) on top of
// ctxScanBandStyle's color, mirroring queuepane.go's ambiguousCandidateCursorStyle
// precedent of bolding the currently-selected row.
func ctxScanRowStyle(band string, selected bool) lipgloss.Style {
	style := ctxScanBandStyle(band)
	if selected {
		style = style.Bold(true)
	}
	return style
}

// fmtEstTokens mirrors ctx-scan's own render/view-model.ts fmtEstTokens
// convention ("~" prefix — Table A token figures are always estimates, per
// docs/context-budget-rubric.md Part 0's "Token estimate convention").
func fmtEstTokens(n float64) string {
	return fmt.Sprintf("~%d", int(math.Round(n)))
}

// fmtCount mirrors ctx-scan's own render/view-model.ts fmtCount convention
// — a plain rounded integer for an exact (non-estimated) measurement (raw/
// effective char counts, truncation before/after values).
func fmtCount(n float64) string {
	return fmt.Sprintf("%d", int(math.Round(n)))
}

// clipContextLines caps rendered content to height (Root.layout's SetSize
// budget), reserving the last line for a "N more…" indicator when clipped —
// mirrors memorytimelinepane.go's clipLines precedent verbatim (same
// off-screen-overflow concern, different pane). height <= 0 (no SetSize
// call yet) means unbounded.
func clipContextLines(lines []string, height int) []string {
	if height <= 0 || len(lines) <= height {
		return lines
	}
	kept := lines[:height-1]
	hidden := len(lines) - len(kept)
	return append(kept, lipgloss.NewStyle().Faint(true).Render(fmt.Sprintf("… %d more line(s)", hidden)))
}
