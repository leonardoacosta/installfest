package ui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/lucasb-eyer/go-colorful"

	"github.com/leonardoacosta/installfest/apps/wavetui/internal/dispatch"
	"github.com/leonardoacosta/installfest/apps/wavetui/internal/flair"
	"github.com/leonardoacosta/installfest/apps/wavetui/internal/lanes"
	"github.com/leonardoacosta/installfest/apps/wavetui/internal/store"
)

// fakeDispatcher is a test-only dispatch.Dispatcher recording every call and
// returning a caller-configured error — the same fake-injection rationale
// resolver.go's own doc comment cites for typing Resolver.Tmux/Clipboard as
// the Dispatcher interface rather than a concrete type.
type fakeDispatcher struct {
	err        error
	calls      int
	lastItem   store.Item
	lastPrompt string
}

func (f *fakeDispatcher) Dispatch(_ context.Context, item store.Item, promptText string) error {
	f.calls++
	f.lastItem = item
	f.lastPrompt = promptText
	return f.err
}

func TestQueuePaneFocusable(t *testing.T) {
	q := NewQueuePane()
	if !q.Focusable() {
		t.Fatal("QueuePane must be Focusable")
	}
}

// TestQueuePaneUpdateBuildsSelection asserts Update populates items in
// Snapshot order and that SelectedItem/SelectedID reflect the table's
// initial cursor (row 0).
func TestQueuePaneUpdateBuildsSelection(t *testing.T) {
	q := NewQueuePane()
	snap := store.Snapshot{Items: []store.Item{
		{ID: "a", Kind: store.KindBead, Title: "Alpha"},
		{ID: "b", Kind: store.KindProposal, Title: "Beta"},
	}}

	pane := q.Update(snap)
	if pane != Pane(q) {
		t.Fatal("Update must return the same *QueuePane, not a new Pane value")
	}

	item, ok := q.SelectedItem()
	if !ok {
		t.Fatal("want a selection after Update with a non-empty snapshot")
	}
	if item.ID != "a" {
		t.Fatalf("want initial selection %q, got %q", "a", item.ID)
	}
}

// TestQueuePaneHandleKeyMovesSelection asserts a down-arrow key press
// (forwarded via HandleKey, the Pane-interface-external navigation hook
// Root uses) moves the selection to the next row.
func TestQueuePaneHandleKeyMovesSelection(t *testing.T) {
	q := NewQueuePane()
	q.Update(store.Snapshot{Items: []store.Item{
		{ID: "a", Title: "Alpha"},
		{ID: "b", Title: "Beta"},
	}})

	q.HandleKey(tea.KeyPressMsg{Code: tea.KeyDown})

	if got := q.SelectedID(); got != "b" {
		t.Fatalf("want selection %q after down-arrow, got %q", "b", got)
	}
}

// TestQueuePanePreservesSelectionAcrossRebuild is the regression this
// pane's Update doc comment calls out: a snapshot refresh that keeps the
// same item IDs (just updated fields) must not silently reset the cursor
// back to row 0.
func TestQueuePanePreservesSelectionAcrossRebuild(t *testing.T) {
	q := NewQueuePane()
	q.Update(store.Snapshot{Items: []store.Item{
		{ID: "a", Title: "Alpha"},
		{ID: "b", Title: "Beta"},
		{ID: "c", Title: "Gamma"},
	}})
	q.HandleKey(tea.KeyPressMsg{Code: tea.KeyDown}) // select "b"
	if got := q.SelectedID(); got != "b" {
		t.Fatalf("setup: want selection %q, got %q", "b", got)
	}

	// A fresh snapshot with the same IDs but different field values (e.g.
	// FanOutScore bumped) must keep the selection on "b", not reset to "a".
	q.Update(store.Snapshot{Items: []store.Item{
		{ID: "a", Title: "Alpha"},
		{ID: "b", Title: "Beta", FanOutScore: 3},
		{ID: "c", Title: "Gamma"},
	}})

	if got := q.SelectedID(); got != "b" {
		t.Fatalf("selection was not preserved across rebuild: got %q, want %q", got, "b")
	}
}

// TestQueuePaneViewRendersRows is a regression test for a real bug caught
// during task 3.4's runtime-evidence run: bubbles/v2's viewport.View()
// returns "" whenever the viewport's Width() is 0, so a table with no width
// ever set renders its header with ZERO visible body rows — even though
// Update had populated real rows and a real selection existed. NewQueuePane
// now sets a default width/height for exactly this reason (see
// defaultQueueWidth/Height's doc comment) — this test asserts the row text
// actually appears in View(), not just that SelectedItem() succeeds (which
// alone would have passed even with this bug present).
func TestQueuePaneViewRendersRows(t *testing.T) {
	q := NewQueuePane()
	q.Update(store.Snapshot{Items: []store.Item{
		{ID: "a", Kind: store.KindBead, Title: "Alpha item"},
	}})

	view := q.View()
	if !strings.Contains(view, "Alpha item") {
		t.Fatalf("QueuePane.View() did not render the row's title — want %q in:\n%s", "Alpha item", view)
	}
}

func TestFormatCreatedAtShort(t *testing.T) {
	if got := formatCreatedAtShort(time.Time{}); got != "-----" {
		t.Fatalf("zero time: want %q, got %q", "-----", got)
	}
	tm := time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC)
	if got := formatCreatedAtShort(tm); got != "07-04" {
		t.Fatalf("want %q, got %q", "07-04", got)
	}
}

// TestQueuePaneSecondClassItemRendersDistinctFromProposal is the post-wave
// gate finding's regression test: spec.md's OpenSpecSource Requirement says
// a plans/advisor-plans item ("SecondClass") SHALL render "visually
// second-class" — not identically to a real openspec/changes/ proposal.
// Renders two otherwise-identical items (same Kind, same Title) that differ
// ONLY in SecondClass, and asserts their rendered queue rows differ — not
// just the underlying field, the actual output QueuePane.View() produces.
func TestQueuePaneSecondClassItemRendersDistinctFromProposal(t *testing.T) {
	proposal := NewQueuePane()
	proposal.Update(store.Snapshot{Items: []store.Item{
		{ID: "a", Kind: store.KindProposal, Title: "Same Title"},
	}})
	proposalRow := firstDataRow(proposal.View())

	plansItem := NewQueuePane()
	plansItem.Update(store.Snapshot{Items: []store.Item{
		{ID: "a", Kind: store.KindProposal, Title: "Same Title", SecondClass: true},
	}})
	plansRow := firstDataRow(plansItem.View())

	// Both rows must still show the title text — this proves the difference
	// below is styling, not a content/data difference.
	if !strings.Contains(proposalRow, "Same Title") || !strings.Contains(plansRow, "Same Title") {
		t.Fatalf("both rows must render the title text; proposal=%q plans=%q", proposalRow, plansRow)
	}
	if proposalRow == plansRow {
		t.Fatalf("a SecondClass (plans/advisor-plans) item rendered IDENTICALLY to a real proposal row — want distinct styling:\n%q", proposalRow)
	}
}

// firstDataRow returns the first row of a QueuePane.View() rendering below
// the header line.
func firstDataRow(view string) string {
	lines := strings.Split(view, "\n")
	if len(lines) < 2 {
		return ""
	}
	return lines[1]
}

// TestSetHighlightsNilOrEmptyRendersUnchanged is task [3.1]'s core contract:
// a nil map (never called) and an explicitly-empty map must both render
// byte-for-byte identically to a QueuePane that never had SetHighlights
// touched at all.
func TestSetHighlightsNilOrEmptyRendersUnchanged(t *testing.T) {
	snap := store.Snapshot{Items: []store.Item{
		{ID: "a", Kind: store.KindBead, Title: "Alpha item"},
	}}

	baseline := NewQueuePane()
	baseline.Update(snap)
	want := baseline.View()

	untouched := NewQueuePane()
	untouched.Update(snap)
	if got := untouched.View(); got != want {
		t.Fatalf("never calling SetHighlights changed rendering:\nwant %q\ngot  %q", want, got)
	}

	nilMap := NewQueuePane()
	nilMap.SetHighlights(nil)
	nilMap.Update(snap)
	if got := nilMap.View(); got != want {
		t.Fatalf("SetHighlights(nil) changed rendering:\nwant %q\ngot  %q", want, got)
	}

	emptyMap := NewQueuePane()
	emptyMap.SetHighlights(map[string]flair.HighlightState{})
	emptyMap.Update(snap)
	if got := emptyMap.View(); got != want {
		t.Fatalf("SetHighlights(empty map) changed rendering:\nwant %q\ngot  %q", want, got)
	}
}

// TestSetHighlightsAppliesColorAndGlyphToMatchingRow confirms the additive
// path actually changes output for a highlighted item, and leaves an
// unhighlighted sibling row untouched.
func TestSetHighlightsAppliesColorAndGlyphToMatchingRow(t *testing.T) {
	q := NewQueuePane()
	q.SetHighlights(map[string]flair.HighlightState{
		"a": {Color: colorful.Color{R: 0, G: 1, B: 0}, Glyph: "!"},
	})
	alpha := store.Item{ID: "a", Kind: store.KindBead, Title: "Alpha item"}
	beta := store.Item{ID: "b", Kind: store.KindBead, Title: "Beta item"}
	q.Update(store.Snapshot{Items: []store.Item{alpha, beta}})

	view := q.View()
	// The Item column now carries a kind glyph + MM-dd date prefix ahead of
	// the title (task 2.1/2.2, renderItemLabel) — the flair highlight glyph
	// still prepends ahead of that whole label, not just the title.
	if !strings.Contains(view, "! "+renderItemLabel(alpha)) {
		t.Fatalf("want highlighted row to carry its glyph prefix, got:\n%s", view)
	}
	plainBeta := renderItemLabel(beta)
	if !strings.Contains(view, plainBeta) || strings.Contains(view, "! "+plainBeta) {
		t.Fatalf("unhighlighted sibling row must render its plain title, got:\n%s", view)
	}
}

// TestSetSpriteGlyphsNilOrEmptyRendersUnchanged mirrors
// TestSetHighlightsNilOrEmptyRendersUnchanged for wavetui-flair's presence
// sprite (design.md § Presence sprites, if-z7pm/if-u7ul.1): a nil map
// (never called) and an explicitly-empty map must both render
// byte-for-byte identically to a QueuePane that never had SetSpriteGlyphs
// touched at all.
func TestSetSpriteGlyphsNilOrEmptyRendersUnchanged(t *testing.T) {
	snap := store.Snapshot{Items: []store.Item{
		{ID: "a", Kind: store.KindBead, Title: "Alpha item"},
	}}

	baseline := NewQueuePane()
	baseline.Update(snap)
	want := baseline.View()

	untouched := NewQueuePane()
	untouched.Update(snap)
	if got := untouched.View(); got != want {
		t.Fatalf("never calling SetSpriteGlyphs changed rendering:\nwant %q\ngot  %q", want, got)
	}

	nilMap := NewQueuePane()
	nilMap.SetSpriteGlyphs(nil)
	nilMap.Update(snap)
	if got := nilMap.View(); got != want {
		t.Fatalf("SetSpriteGlyphs(nil) changed rendering:\nwant %q\ngot  %q", want, got)
	}

	emptyMap := NewQueuePane()
	emptyMap.SetSpriteGlyphs(map[string]string{})
	emptyMap.Update(snap)
	if got := emptyMap.View(); got != want {
		t.Fatalf("SetSpriteGlyphs(empty map) changed rendering:\nwant %q\ngot  %q", want, got)
	}
}

// TestSetSpriteGlyphsComposesWithTitleAndHighlight confirms the sprite
// glyph prepends onto the title (composing with, not replacing, the
// wave-select marker and any live flair highlight on the same row), and
// that an unrelated sibling row without a sprite glyph is untouched.
func TestSetSpriteGlyphsComposesWithTitleAndHighlight(t *testing.T) {
	q := NewQueuePane()
	q.SetSpriteGlyphs(map[string]string{"a": "⋋"})
	q.SetHighlights(map[string]flair.HighlightState{
		"a": {Color: colorful.Color{R: 0, G: 1, B: 0}, Glyph: "!"},
	})
	alpha := store.Item{ID: "a", Kind: store.KindBead, Title: "Alpha item"}
	beta := store.Item{ID: "b", Kind: store.KindBead, Title: "Beta item"}
	q.Update(store.Snapshot{Items: []store.Item{alpha, beta}})

	view := q.View()
	// Same composition-order note as TestSetHighlightsAppliesColorAndGlyphToMatchingRow
	// above: the sprite glyph and highlight glyph both prepend ahead of the
	// whole glyph+date+title label (renderItemLabel), not just the title.
	if !strings.Contains(view, "! ⋋ "+renderItemLabel(alpha)) {
		t.Fatalf("want sprite glyph to prepend ahead of the existing highlight glyph+title, got:\n%s", view)
	}
	plainBeta := renderItemLabel(beta)
	if !strings.Contains(view, plainBeta) || strings.Contains(view, "⋋ "+plainBeta) {
		t.Fatalf("sibling row with no sprite glyph must render its plain title, got:\n%s", view)
	}
}

func TestBlockerBadge(t *testing.T) {
	cases := []struct {
		name string
		item store.Item
		want string
	}{
		{"unblocked", store.Item{}, ""},
		{"blocked", store.Item{Blocker: &store.BlockerNote{Type: "dependency"}}, "blocked:dependency"},
		{"stale", store.Item{Stale: true}, "stale"},
		// Blocker takes precedence over stale when both happen to be set —
		// matches sources/beads.go's markStale, which republishes a blocked
		// item's existing Blocker unchanged alongside Stale=true.
		{"blocked and stale", store.Item{Stale: true, Blocker: &store.BlockerNote{Type: "review"}}, "blocked:review"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := blockerBadge(c.item); got != c.want {
				t.Fatalf("want %q, got %q", c.want, got)
			}
		})
	}
}

// --- wavetui-headless-discoverability (tasks.md [1.3]/[2.1]) mechanism tag -

// TestDispatchMechanismTag asserts the fallback tag names "tmux" only when
// the item has a linked pane (item.Session.PaneID != ""), and "clipboard"
// otherwise — including the no-Session and empty-PaneID-with-a-Session
// cases, per dispatchMechanismTag's own doc comment.
func TestDispatchMechanismTag(t *testing.T) {
	cases := []struct {
		name string
		item store.Item
		want string
	}{
		{"no session at all", store.Item{}, "clipboard"},
		{"session with no linked pane", store.Item{Session: &store.SessionLink{}}, "clipboard"},
		{"session with a linked pane", store.Item{Session: &store.SessionLink{PaneID: "%3"}}, "tmux"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := dispatchMechanismTag(c.item); got != c.want {
				t.Fatalf("want %q, got %q", c.want, got)
			}
		})
	}
}

// TestRenderBlockerCellMechanismTagOnlyWhenNoOtherBadgeApplies is
// renderBlockerCell's own documented precedence (dispatch badge > lane badge
// > blocker/stale badge > mechanism tag): the mechanism tag must render for
// an unblocked, fresh item with no dispatch/lane badge, but must NOT
// override any of the three existing badges — even when that same item's
// Session would otherwise qualify it for the "tmux" tag.
func TestRenderBlockerCellMechanismTagOnlyWhenNoOtherBadgeApplies(t *testing.T) {
	tmuxItem := store.Item{ID: "a", Session: &store.SessionLink{PaneID: "%1"}}
	clipboardItem := store.Item{ID: "b"}

	t.Run("unblocked with linked pane falls through to tmux tag", func(t *testing.T) {
		q := NewQueuePane()
		if got := q.renderBlockerCell(tmuxItem); got != "tmux" {
			t.Fatalf("want %q, got %q", "tmux", got)
		}
	})

	t.Run("unblocked with no linked pane falls through to clipboard tag", func(t *testing.T) {
		q := NewQueuePane()
		if got := q.renderBlockerCell(clipboardItem); got != "clipboard" {
			t.Fatalf("want %q, got %q", "clipboard", got)
		}
	})

	t.Run("dispatch badge wins over the mechanism tag", func(t *testing.T) {
		q := NewQueuePane()
		q.dispatchBadges = map[string]string{"a": "failed: boom"}
		if got := q.renderBlockerCell(tmuxItem); got != "failed: boom" {
			t.Fatalf("want the dispatch badge to win, got %q", got)
		}
	})

	t.Run("lane badge wins over the mechanism tag", func(t *testing.T) {
		q := NewQueuePane()
		q.lanes = map[string]*lanes.LaneState{"a": {Type: "decision"}}
		if got := q.renderBlockerCell(tmuxItem); got != "lane:decision (s)" {
			t.Fatalf("want the lane badge to win, got %q", got)
		}
	})

	t.Run("blocker badge wins over the mechanism tag", func(t *testing.T) {
		q := NewQueuePane()
		blocked := store.Item{ID: "a", Blocker: &store.BlockerNote{Type: "dependency"}, Session: &store.SessionLink{PaneID: "%1"}}
		if got := q.renderBlockerCell(blocked); got != "blocked:dependency" {
			t.Fatalf("want the blocker badge to win, got %q", got)
		}
	})

	t.Run("stale badge wins over the mechanism tag", func(t *testing.T) {
		q := NewQueuePane()
		stale := store.Item{ID: "a", Stale: true, Session: &store.SessionLink{PaneID: "%1"}}
		if got := q.renderBlockerCell(stale); got != "stale" {
			t.Fatalf("want the stale badge to win, got %q", got)
		}
	})
}

// --- wavetui-dispatch (tasks.md [3.1]) Start action ---------------------

func TestQueuePaneStartDispatchesHighlightedItemViaResolver(t *testing.T) {
	q := NewQueuePane()
	q.Update(store.Snapshot{Items: []store.Item{
		{ID: "if-1234", Title: "Alpha"},
	}})

	fake := &fakeDispatcher{}
	q.SetDispatcher(context.Background(), fake)

	q.HandleKey(tea.KeyPressMsg{Code: tea.KeyEnter})

	if fake.calls != 1 {
		t.Fatalf("want Dispatch called exactly once, got %d", fake.calls)
	}
	if fake.lastItem.ID != "if-1234" {
		t.Fatalf("want the highlighted item dispatched, got %q", fake.lastItem.ID)
	}
	if fake.lastPrompt != "/apply if-1234" {
		t.Fatalf("want promptText %q, got %q", "/apply if-1234", fake.lastPrompt)
	}
}

func TestQueuePaneStartNoDispatcherIsNoop(t *testing.T) {
	q := NewQueuePane()
	q.Update(store.Snapshot{Items: []store.Item{{ID: "a", Title: "Alpha"}}})

	// SetDispatcher never called — must not panic.
	q.HandleKey(tea.KeyPressMsg{Code: tea.KeyEnter})

	if strings.Contains(q.View(), "failed") {
		t.Fatalf("want no badge rendered when no dispatcher is wired, got:\n%s", q.View())
	}
}

func TestQueuePaneStartRendersFailureBadgeOnDispatchError(t *testing.T) {
	q := NewQueuePane()
	q.Update(store.Snapshot{Items: []store.Item{{ID: "a", Title: "Alpha"}}})
	fake := &fakeDispatcher{err: errors.New("tmux: boom")}
	q.SetDispatcher(context.Background(), fake)

	q.HandleKey(tea.KeyPressMsg{Code: tea.KeyEnter})

	if got := firstDataRow(q.View()); !strings.Contains(got, "failed: tmux: boom") {
		t.Fatalf("want failure badge in row, got:\n%s", got)
	}
}

func TestQueuePaneStartRendersQueuedBadgeOnSessionStreamingRefusal(t *testing.T) {
	q := NewQueuePane()
	q.Update(store.Snapshot{Items: []store.Item{{ID: "a", Title: "Alpha"}}})
	fake := &fakeDispatcher{err: dispatch.ErrSessionStreaming}
	q.SetDispatcher(context.Background(), fake)

	q.HandleKey(tea.KeyPressMsg{Code: tea.KeyEnter})

	if got := firstDataRow(q.View()); !strings.Contains(got, "queued — session busy") {
		t.Fatalf("want queued-busy badge in row, got:\n%s", got)
	}
}

func TestQueuePaneStartSuccessClearsPriorFailureBadge(t *testing.T) {
	q := NewQueuePane()
	q.Update(store.Snapshot{Items: []store.Item{{ID: "a", Title: "Alpha"}}})
	fake := &fakeDispatcher{err: errors.New("boom")}
	q.SetDispatcher(context.Background(), fake)
	q.HandleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !strings.Contains(firstDataRow(q.View()), "failed") {
		t.Fatalf("setup: want a failure badge present before the retry")
	}

	fake.err = nil
	q.HandleKey(tea.KeyPressMsg{Code: tea.KeyEnter})

	if strings.Contains(firstDataRow(q.View()), "failed") {
		t.Fatalf("want the stale failure badge cleared after a successful retry, got:\n%s", firstDataRow(q.View()))
	}
}

// explodingDispatcher is a dispatch.Dispatcher stand-in that fails the test
// immediately if Dispatch is ever called on it — a spy, not a stub that
// happens to succeed. Used below to prove a non-id-shaped item.ID never
// reaches a real tmux/clipboard call.
type explodingDispatcher struct {
	t *testing.T
}

func (e explodingDispatcher) Dispatch(context.Context, store.Item, string) error {
	e.t.Fatal("dispatcher invoked: a non-id-shaped item.ID must be refused before any tmux/clipboard dispatch call")
	return nil
}

// TestQueuePaneStartRefusesNonIDShapedItemBeforeAnyDispatchAttempt is the
// regression test for the post-wave gate finding that validateDispatchTarget
// (dispatch.go) was fully implemented and unit-tested but had no real
// caller anywhere in the dispatch path — a non-id-shaped item.ID (e.g.
// "plan:foo", the exact shape internal/sources/openspec.go's
// parseFlatMarkdownDir mints for every plans/ and advisor-plans/ item via
// idPrefix+":"+name) would have sailed straight through QueuePane's Start
// action into a real TmuxDispatcher/ClipboardDispatcher call, pasting an
// unvalidated string into a real tmux pane.
//
// This exercises the REAL production chain, not a stand-in for it: a real
// *dispatch.Resolver (the exact type cmd/wavetui/main.go wires via
// SetDispatcher — see resolver.go's NewResolver doc comment), wired with
// explodingDispatcher spies in place of Tmux/Clipboard, driven through
// QueuePane's actual "enter" HandleKey path (startSelected -> Resolver.
// Dispatch). Calling validateDispatchTarget directly (dispatch_test.go)
// would prove nothing about whether the real dispatch path ever invokes it
// — this test fails loudly (via explodingDispatcher.t.Fatal) if
// Resolver.Dispatch ever again forgets to validate item.ID before either
// concrete Dispatcher runs.
func TestQueuePaneStartRefusesNonIDShapedItemBeforeAnyDispatchAttempt(t *testing.T) {
	q := NewQueuePane()
	q.Update(store.Snapshot{Items: []store.Item{
		{ID: "plan:foo", Title: "A plans/ item", SecondClass: true},
	}})

	resolver := &dispatch.Resolver{
		Tmux:      explodingDispatcher{t: t},
		Clipboard: explodingDispatcher{t: t},
	}
	q.SetDispatcher(context.Background(), resolver)

	q.HandleKey(tea.KeyPressMsg{Code: tea.KeyEnter})

	// The Blocker column is a fixed 24-wide table cell (queueColumns), so the
	// full validateDispatchTarget message ("...is not id-shaped, refusing to
	// cross the dispatch boundary") renders truncated with an ellipsis —
	// asserting on the message's own leading text ("dispatch target"), not
	// the tail that the column width cuts off.
	if got := firstDataRow(q.View()); !strings.Contains(got, "failed:") || !strings.Contains(got, "dispatch target") {
		t.Fatalf("want a refusal badge naming the id-shape failure, got:\n%s", got)
	}
}

// --- if-7mq2.1: inline AmbiguousTargetError candidate picker -------------

// fakeOverrideDispatcher is a dispatch.TargetOverrideDispatcher stand-in
// (dispatch.go) recording both Dispatch and DispatchToPane calls — the
// picker's confirm action (confirmAmbiguousPicker) needs a dispatcher that
// implements the override capability, which the plain fakeDispatcher above
// deliberately does not, so this is a separate fake rather than an
// extension of that one.
type fakeOverrideDispatcher struct {
	dispatchErr error
	overrideErr error

	dispatchCalls int
	overrideCalls int

	lastOverrideItem   store.Item
	lastOverridePane   string
	lastOverridePrompt string
}

func (f *fakeOverrideDispatcher) Dispatch(_ context.Context, _ store.Item, _ string) error {
	f.dispatchCalls++
	return f.dispatchErr
}

func (f *fakeOverrideDispatcher) DispatchToPane(_ context.Context, item store.Item, promptText, paneID string) error {
	f.overrideCalls++
	f.lastOverrideItem = item
	f.lastOverridePane = paneID
	f.lastOverridePrompt = promptText
	return f.overrideErr
}

// ambiguousErr is a small fixture shared by the picker tests below: two
// tied candidates, mirroring resolver_test.go's own
// TestResolverAmbiguousErrorPropagatesWithoutFallback fixture shape.
func ambiguousErr() *dispatch.AmbiguousTargetError {
	return &dispatch.AmbiguousTargetError{Candidates: []dispatch.Candidate{
		{PaneID: "%1", Session: "main", Window: "1", Project: "installfest"},
		{PaneID: "%2", Session: "main", Window: "2", Project: "installfest"},
	}}
}

// TestQueuePaneStartAmbiguousErrorOpensInlinePicker asserts a tied dispatch
// installs q.ambiguousPicker (rather than falling through to the plain
// failure badge), renders a compact row badge, AND renders every tied
// candidate's pane ID inline via View().
func TestQueuePaneStartAmbiguousErrorOpensInlinePicker(t *testing.T) {
	q := NewQueuePane()
	q.Update(store.Snapshot{Items: []store.Item{{ID: "if-1234", Title: "Alpha"}}})
	fake := &fakeOverrideDispatcher{dispatchErr: ambiguousErr()}
	q.SetDispatcher(context.Background(), fake)

	q.HandleKey(tea.KeyPressMsg{Code: tea.KeyEnter})

	if q.ambiguousPicker == nil {
		t.Fatal("want an ambiguous picker opened after a tied dispatch")
	}
	if len(q.ambiguousPicker.candidates) != 2 {
		t.Fatalf("want 2 candidates carried into the picker, got %d", len(q.ambiguousPicker.candidates))
	}
	if q.ambiguousPicker.cursor != 0 {
		t.Fatalf("want the picker cursor to start at 0, got %d", q.ambiguousPicker.cursor)
	}

	view := q.View()
	if !strings.Contains(view, "%1") || !strings.Contains(view, "%2") {
		t.Fatalf("want both candidate pane IDs rendered inline, got:\n%s", view)
	}
	if !strings.Contains(firstDataRow(view), "ambiguous") {
		t.Fatalf("want a compact ambiguous badge on the row, got:\n%s", firstDataRow(view))
	}
}

// TestQueuePaneAmbiguousPickerNavigatesAndClampsCursor asserts up/down and
// k/j both move the picker's own cursor (independent of q.table's row
// cursor), clamped at both ends rather than wrapping.
func TestQueuePaneAmbiguousPickerNavigatesAndClampsCursor(t *testing.T) {
	q := NewQueuePane()
	q.Update(store.Snapshot{Items: []store.Item{{ID: "if-1234", Title: "Alpha"}}})
	err := &dispatch.AmbiguousTargetError{Candidates: []dispatch.Candidate{
		{PaneID: "%1"}, {PaneID: "%2"}, {PaneID: "%3"},
	}}
	q.SetDispatcher(context.Background(), &fakeOverrideDispatcher{dispatchErr: err})
	q.HandleKey(tea.KeyPressMsg{Code: tea.KeyEnter}) // open the picker

	// Cannot go above the top.
	q.HandleKey(tea.KeyPressMsg{Code: tea.KeyUp})
	if q.ambiguousPicker.cursor != 0 {
		t.Fatalf("want cursor clamped at 0, got %d", q.ambiguousPicker.cursor)
	}

	q.HandleKey(tea.KeyPressMsg{Code: tea.KeyDown})
	if q.ambiguousPicker.cursor != 1 {
		t.Fatalf("want cursor at 1 after down, got %d", q.ambiguousPicker.cursor)
	}
	q.HandleKey(tea.KeyPressMsg{Text: "j"})
	if q.ambiguousPicker.cursor != 2 {
		t.Fatalf("want cursor at 2 after j, got %d", q.ambiguousPicker.cursor)
	}
	// Cannot go past the last candidate.
	q.HandleKey(tea.KeyPressMsg{Code: tea.KeyDown})
	if q.ambiguousPicker.cursor != 2 {
		t.Fatalf("want cursor clamped at 2, got %d", q.ambiguousPicker.cursor)
	}
	q.HandleKey(tea.KeyPressMsg{Text: "k"})
	if q.ambiguousPicker.cursor != 1 {
		t.Fatalf("want cursor at 1 after k, got %d", q.ambiguousPicker.cursor)
	}
}

// TestQueuePaneAmbiguousPickerConfirmDispatchesToChosenCandidate asserts
// "enter" re-dispatches via DispatchToPane (not the plain Dispatch) with
// the candidate under the picker's cursor at confirm time, the original
// item and prompt text unchanged, and dismisses the picker + clears the
// row badge on success.
func TestQueuePaneAmbiguousPickerConfirmDispatchesToChosenCandidate(t *testing.T) {
	q := NewQueuePane()
	q.Update(store.Snapshot{Items: []store.Item{{ID: "if-1234", Title: "Alpha"}}})
	fake := &fakeOverrideDispatcher{dispatchErr: ambiguousErr()}
	q.SetDispatcher(context.Background(), fake)

	q.HandleKey(tea.KeyPressMsg{Code: tea.KeyEnter}) // open the picker
	q.HandleKey(tea.KeyPressMsg{Code: tea.KeyDown})  // move onto "%2"
	q.HandleKey(tea.KeyPressMsg{Code: tea.KeyEnter}) // confirm

	if fake.overrideCalls != 1 {
		t.Fatalf("want DispatchToPane called exactly once, got %d", fake.overrideCalls)
	}
	if fake.lastOverridePane != "%2" {
		t.Fatalf("want the operator-chosen pane %q dispatched to, got %q", "%2", fake.lastOverridePane)
	}
	if fake.lastOverrideItem.ID != "if-1234" {
		t.Fatalf("want the original item re-dispatched, got %q", fake.lastOverrideItem.ID)
	}
	if fake.lastOverridePrompt != "/apply if-1234" {
		t.Fatalf("want the original prompt text reused unchanged, got %q", fake.lastOverridePrompt)
	}
	if q.ambiguousPicker != nil {
		t.Fatal("want the picker dismissed after a confirm")
	}
	if strings.Contains(firstDataRow(q.View()), "ambiguous") {
		t.Fatalf("want the ambiguous badge cleared after a successful override dispatch, got:\n%s", firstDataRow(q.View()))
	}
}

// TestQueuePaneAmbiguousPickerEscapeCancelsToFailureBadge asserts "esc"
// dismisses the picker WITHOUT ever calling DispatchToPane, falling back to
// the plain generic failure badge the original AmbiguousTargetError would
// have rendered had the picker never existed.
func TestQueuePaneAmbiguousPickerEscapeCancelsToFailureBadge(t *testing.T) {
	q := NewQueuePane()
	q.Update(store.Snapshot{Items: []store.Item{{ID: "if-1234", Title: "Alpha"}}})
	fake := &fakeOverrideDispatcher{dispatchErr: ambiguousErr()}
	q.SetDispatcher(context.Background(), fake)
	q.HandleKey(tea.KeyPressMsg{Code: tea.KeyEnter})

	q.HandleKey(tea.KeyPressMsg{Code: tea.KeyEscape})

	if q.ambiguousPicker != nil {
		t.Fatal("want the picker dismissed after esc")
	}
	if fake.overrideCalls != 0 {
		t.Fatalf("want DispatchToPane never called on cancel, got %d", fake.overrideCalls)
	}
	// The Blocker column is a fixed 24-wide table cell (queueColumns), so the
	// full AmbiguousTargetError message ("...ambiguous: 2 candidates tied for
	// the top score") renders truncated with an ellipsis — the same
	// leading-text-only assertion
	// TestQueuePaneStartRefusesNonIDShapedItemBeforeAnyDispatchAttempt already
	// uses for an equally long refusal message in this same column.
	row := firstDataRow(q.View())
	if !strings.Contains(row, "failed:") || !strings.Contains(row, "dispatch target") {
		t.Fatalf("want a plain ambiguous failure badge after cancel, got:\n%s", row)
	}
}

// TestQueuePaneAmbiguousPickerConfirmWithoutOverrideSupportRendersFailure
// asserts confirming against a Dispatcher that does NOT implement
// dispatch.TargetOverrideDispatcher (the plain fakeDispatcher, same as
// production code would see from a hypothetical future Dispatcher that
// never grows tmux-pane targeting) dismisses the picker and renders a
// failure badge rather than panicking or silently no-op'ing.
func TestQueuePaneAmbiguousPickerConfirmWithoutOverrideSupportRendersFailure(t *testing.T) {
	q := NewQueuePane()
	q.Update(store.Snapshot{Items: []store.Item{{ID: "if-1234", Title: "Alpha"}}})
	fake := &fakeDispatcher{err: ambiguousErr()}
	q.SetDispatcher(context.Background(), fake)
	q.HandleKey(tea.KeyPressMsg{Code: tea.KeyEnter})

	q.HandleKey(tea.KeyPressMsg{Code: tea.KeyEnter}) // confirm attempt

	if q.ambiguousPicker != nil {
		t.Fatal("want the picker dismissed even without override support")
	}
	if !strings.Contains(firstDataRow(q.View()), "failed:") {
		t.Fatalf("want a failure badge when the wired dispatcher can't act on the operator's pick, got:\n%s", firstDataRow(q.View()))
	}
}

// TestQueuePaneAmbiguousPickerSuppressesOtherKeysWhileOpen asserts the
// picker owns the keyboard entirely: "space" (wave-builder select) must not
// fire while a tie is unresolved, and the picker must remain open.
func TestQueuePaneAmbiguousPickerSuppressesOtherKeysWhileOpen(t *testing.T) {
	q := NewQueuePane()
	q.Update(store.Snapshot{Items: []store.Item{{ID: "if-1234", Title: "Alpha"}}})
	q.SetDispatcher(context.Background(), &fakeOverrideDispatcher{dispatchErr: ambiguousErr()})
	q.HandleKey(tea.KeyPressMsg{Code: tea.KeyEnter})

	q.HandleKey(tea.KeyPressMsg{Code: tea.KeySpace})

	if len(q.SelectedForWave()) != 0 {
		t.Fatal("want space suppressed while the ambiguous picker is active")
	}
	if q.ambiguousPicker == nil {
		t.Fatal("want the picker to remain open")
	}
}

// --- wavetui-dispatch (tasks.md [3.2]) select mode -----------------------

func TestQueuePaneToggleSelectedOrdersByFanOutScoreDescending(t *testing.T) {
	q := NewQueuePane()
	q.Update(store.Snapshot{Items: []store.Item{
		{ID: "a", Title: "Alpha", FanOutScore: 1},
		{ID: "b", Title: "Beta", FanOutScore: 5},
		{ID: "c", Title: "Gamma", FanOutScore: 3},
	}})

	// Select all three in table order (a, b, c) — SelectedForWave must
	// still return them fan-out-descending (b, c, a), not selection order.
	q.HandleKey(tea.KeyPressMsg{Code: tea.KeySpace}) // select "a"
	q.HandleKey(tea.KeyPressMsg{Code: tea.KeyDown})
	q.HandleKey(tea.KeyPressMsg{Code: tea.KeySpace}) // select "b"
	q.HandleKey(tea.KeyPressMsg{Code: tea.KeyDown})
	q.HandleKey(tea.KeyPressMsg{Code: tea.KeySpace}) // select "c"

	got := q.SelectedForWave()
	if len(got) != 3 {
		t.Fatalf("want 3 selected items, got %d: %+v", len(got), got)
	}
	wantOrder := []string{"b", "c", "a"}
	for i, want := range wantOrder {
		if got[i].ID != want {
			t.Fatalf("want order %v, got %v", wantOrder, idsOf(got))
		}
	}
}

func idsOf(items []store.Item) []string {
	ids := make([]string, len(items))
	for i, it := range items {
		ids[i] = it.ID
	}
	return ids
}

func TestQueuePaneToggleSelectedTwiceDeselects(t *testing.T) {
	q := NewQueuePane()
	q.Update(store.Snapshot{Items: []store.Item{{ID: "a", Title: "Alpha"}}})

	q.HandleKey(tea.KeyPressMsg{Code: tea.KeySpace})
	if got := q.SelectedForWave(); len(got) != 1 {
		t.Fatalf("want 1 selected after first toggle, got %d", len(got))
	}

	q.HandleKey(tea.KeyPressMsg{Code: tea.KeySpace})
	if got := q.SelectedForWave(); len(got) != 0 {
		t.Fatalf("want 0 selected after second toggle (deselect), got %d", len(got))
	}
}

func TestQueuePaneSelectMarkerRendersOnSelectedRow(t *testing.T) {
	q := NewQueuePane()
	item := store.Item{ID: "a", Title: "Alpha item"}
	q.Update(store.Snapshot{Items: []store.Item{item}})

	q.HandleKey(tea.KeyPressMsg{Code: tea.KeySpace})

	// The select marker prefixes the whole glyph+date+title label
	// (renderItemLabel, task 2.1/2.2), not just the bare title.
	if got := firstDataRow(q.View()); !strings.Contains(got, waveSelectMarker+renderItemLabel(item)) {
		t.Fatalf("want select marker prefixing the title, got:\n%s", got)
	}
}

func TestQueuePaneSelectModeRendersConflictWarningNamingBothIDs(t *testing.T) {
	q := NewQueuePane()
	q.Update(store.Snapshot{Items: []store.Item{
		{ID: "a", Title: "Alpha", TouchedFiles: []string{"shared.go"}},
		{ID: "b", Title: "Beta", TouchedFiles: []string{"shared.go"}},
	}})

	q.HandleKey(tea.KeyPressMsg{Code: tea.KeySpace}) // select "a"
	q.HandleKey(tea.KeyPressMsg{Code: tea.KeyDown})
	q.HandleKey(tea.KeyPressMsg{Code: tea.KeySpace}) // select "b"

	view := q.View()
	if !strings.Contains(view, "conflict: shared.go") || !strings.Contains(view, "a") || !strings.Contains(view, "b") {
		t.Fatalf("want a conflict warning naming both item IDs, got:\n%s", view)
	}
}

func TestQueuePaneNoConflictWarningWithoutOverlap(t *testing.T) {
	q := NewQueuePane()
	q.Update(store.Snapshot{Items: []store.Item{
		{ID: "a", Title: "Alpha", TouchedFiles: []string{"one.go"}},
		{ID: "b", Title: "Beta", TouchedFiles: []string{"two.go"}},
	}})

	q.HandleKey(tea.KeyPressMsg{Code: tea.KeySpace})
	q.HandleKey(tea.KeyPressMsg{Code: tea.KeyDown})
	q.HandleKey(tea.KeyPressMsg{Code: tea.KeySpace})

	if strings.Contains(q.View(), "conflict:") {
		t.Fatalf("want no conflict warning for non-overlapping files, got:\n%s", q.View())
	}
}

func TestQueuePaneEscClearsSelection(t *testing.T) {
	q := NewQueuePane()
	q.Update(store.Snapshot{Items: []store.Item{{ID: "a", Title: "Alpha"}}})
	q.HandleKey(tea.KeyPressMsg{Code: tea.KeySpace})
	if len(q.SelectedForWave()) != 1 {
		t.Fatalf("setup: want 1 selected")
	}

	q.HandleKey(tea.KeyPressMsg{Code: tea.KeyEscape})

	if got := q.SelectedForWave(); len(got) != 0 {
		t.Fatalf("want selection cleared after esc, got %d", len(got))
	}
}

// --- wavetui-dispatch (tasks.md [3.3]) wave finalization -----------------

func TestQueuePaneFinalizeWaveCallsWriterAndClearsSelection(t *testing.T) {
	q := NewQueuePane()
	q.Update(store.Snapshot{Items: []store.Item{
		{ID: "a", Title: "Alpha", FanOutScore: 2},
		{ID: "b", Title: "Beta", FanOutScore: 5},
	}})
	q.HandleKey(tea.KeyPressMsg{Code: tea.KeySpace})
	q.HandleKey(tea.KeyPressMsg{Code: tea.KeyDown})
	q.HandleKey(tea.KeyPressMsg{Code: tea.KeySpace})

	var gotItems []store.Item
	q.SetWaveWriter(func(items []store.Item) error {
		gotItems = items
		return nil
	})

	q.HandleKey(tea.KeyPressMsg{Text: "w"})

	if len(gotItems) != 2 || gotItems[0].ID != "b" || gotItems[1].ID != "a" {
		t.Fatalf("want writer called with FanOutScore-descending items, got %+v", gotItems)
	}
	if got := q.SelectedForWave(); len(got) != 0 {
		t.Fatalf("want selection cleared after a successful finalize, got %d", len(got))
	}
	if !strings.Contains(q.View(), "wave finalized: 2 item(s)") {
		t.Fatalf("want a success status line, got:\n%s", q.View())
	}
}

func TestQueuePaneFinalizeWaveNoSelectionReportsStatus(t *testing.T) {
	q := NewQueuePane()
	q.Update(store.Snapshot{Items: []store.Item{{ID: "a", Title: "Alpha"}}})
	called := false
	q.SetWaveWriter(func(items []store.Item) error {
		called = true
		return nil
	})

	q.HandleKey(tea.KeyPressMsg{Text: "w"})

	if called {
		t.Fatalf("want writer never called with an empty selection")
	}
	if !strings.Contains(q.View(), "no items selected") {
		t.Fatalf("want a status line explaining nothing was selected, got:\n%s", q.View())
	}
}

func TestQueuePaneFinalizeWaveNoWriterConfiguredIsSafe(t *testing.T) {
	q := NewQueuePane()
	q.Update(store.Snapshot{Items: []store.Item{{ID: "a", Title: "Alpha"}}})
	q.HandleKey(tea.KeyPressMsg{Code: tea.KeySpace})

	// SetWaveWriter never called — must not panic.
	q.HandleKey(tea.KeyPressMsg{Text: "w"})

	if !strings.Contains(q.View(), "no wave writer configured") {
		t.Fatalf("want a status line explaining no writer is wired, got:\n%s", q.View())
	}
}

func TestQueuePaneFinalizeWaveSurfacesWriterError(t *testing.T) {
	q := NewQueuePane()
	q.Update(store.Snapshot{Items: []store.Item{{ID: "a", Title: "Alpha"}}})
	q.HandleKey(tea.KeyPressMsg{Code: tea.KeySpace})
	q.SetWaveWriter(func(items []store.Item) error {
		return errors.New("disk full")
	})

	q.HandleKey(tea.KeyPressMsg{Text: "w"})

	if !strings.Contains(q.View(), "finalize failed: disk full") {
		t.Fatalf("want the writer's error surfaced, got:\n%s", q.View())
	}
	// A failed finalize must not silently clear the selection the operator
	// would otherwise need to retry.
	if got := q.SelectedForWave(); len(got) != 1 {
		t.Fatalf("want selection preserved after a failed finalize, got %d", len(got))
	}
}
