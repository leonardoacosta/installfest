package ui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/leonardoacosta/installfest/apps/wavetui/internal/store"
	"github.com/leonardoacosta/installfest/apps/wavetui/internal/timeline"
)

// --- stub queriers ---------------------------------------------------------
//
// These implement the three unexported querier interfaces (beadsHistoryQuerier/
// archiveQuerier/memoryQuerier) declared in timeline_dispatch.go, letting these
// tests inject a controllable delay/result/error instead of touching real
// git/bd processes — same rationale as sources/beads.go's beadsCLI and this
// package's own beadsHistoryQuerier doc comment.

type stubBeadsQuerier struct {
	delay  time.Duration
	result timeline.Result
	err    error
}

func (s *stubBeadsQuerier) Query(ctx context.Context, itemID string, childIDs []string) (timeline.Result, error) {
	if s.delay > 0 {
		time.Sleep(s.delay)
	}
	return s.result, s.err
}

type stubArchiveQuerier struct {
	delay  time.Duration
	result timeline.Result
	err    error
}

func (s *stubArchiveQuerier) Query(ctx context.Context, proposalSlug string) (timeline.Result, error) {
	if s.delay > 0 {
		time.Sleep(s.delay)
	}
	return s.result, s.err
}

type stubMemoryQuerier struct {
	delay  time.Duration
	result timeline.Result
	err    error
}

func (s *stubMemoryQuerier) Query(ctx context.Context) (timeline.Result, error) {
	if s.delay > 0 {
		time.Sleep(s.delay)
	}
	return s.result, s.err
}

var (
	_ beadsHistoryQuerier = (*stubBeadsQuerier)(nil)
	_ archiveQuerier      = (*stubArchiveQuerier)(nil)
	_ memoryQuerier       = (*stubMemoryQuerier)(nil)
)

func availableStubs() (*stubBeadsQuerier, *stubArchiveQuerier, *stubMemoryQuerier) {
	return &stubBeadsQuerier{result: timeline.Result{Availability: timeline.Available}},
		&stubArchiveQuerier{result: timeline.Result{Availability: timeline.Available}},
		&stubMemoryQuerier{result: timeline.Result{Availability: timeline.Available}}
}

// TestRunTimelineQueriesRunsSourcesConcurrently is design.md § On-demand
// querying's "fan out to all 3 sources concurrently" claim, exercised with a
// real (if short) artificial delay on each stub source: if the three Query
// calls actually ran sequentially, the total would be ~3x delay; concurrent
// dispatch keeps it close to 1x delay. A generous 2x-delay ceiling leaves
// plenty of scheduling-jitter margin while still failing hard on a
// regression to sequential dispatch.
func TestRunTimelineQueriesRunsSourcesConcurrently(t *testing.T) {
	const delay = 60 * time.Millisecond
	beads := &stubBeadsQuerier{delay: delay, result: timeline.Result{Availability: timeline.Available}}
	archive := &stubArchiveQuerier{delay: delay, result: timeline.Result{Availability: timeline.Available}}
	memory := &stubMemoryQuerier{delay: delay, result: timeline.Result{Availability: timeline.Available}}

	start := time.Now()
	runTimelineQueries(context.Background(), beads, archive, memory, store.Item{ID: "a"})
	elapsed := time.Since(start)

	if elapsed >= 2*delay {
		t.Fatalf("runTimelineQueries took %v for 3 sources each delayed %v — want well under %v (sequential dispatch would take ~%v); sources are not running concurrently", elapsed, delay, 2*delay, 3*delay)
	}
}

// TestRunTimelineQueriesMergesAllSourcesAndHandlesPartialFailure is the
// fan-in half of the same claim: the merged TimelineMsg must include every
// surviving source's entries even when one of the three errors out, and the
// per-lane Unavailable flags must reflect exactly the failing lane — not the
// other two, and not "the whole query failed."
func TestRunTimelineQueriesMergesAllSourcesAndHandlesPartialFailure(t *testing.T) {
	day := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	beadEntry := timeline.Entry{Source: timeline.SourceBead, Text: "claimed", Time: day}
	archiveEntry := timeline.Entry{Source: timeline.SourceArchive, Text: "archived milestone", Time: day.Add(time.Hour)}

	beads := &stubBeadsQuerier{result: timeline.Result{Entries: []timeline.Entry{beadEntry}, Availability: timeline.Available}}
	archive := &stubArchiveQuerier{result: timeline.Result{Entries: []timeline.Entry{archiveEntry}, Availability: timeline.Available}}
	memory := &stubMemoryQuerier{err: errors.New("memory source exploded")}

	msg := runTimelineQueries(context.Background(), beads, archive, memory, store.Item{ID: "a"})

	if !msg.HasSelection || msg.ItemID != "a" {
		t.Fatalf("want HasSelection=true ItemID=%q, got %+v", "a", msg)
	}
	if msg.BeadUnavailable {
		t.Errorf("bead lane succeeded — must not be marked BeadUnavailable")
	}
	if msg.ArchiveUnavailable {
		t.Errorf("archive lane succeeded — must not be marked ArchiveUnavailable")
	}
	if !msg.MemoryUnavailable {
		t.Errorf("memory lane errored — want MemoryUnavailable=true")
	}

	var gotTexts []string
	for _, g := range msg.Groups {
		for _, e := range g.Entries {
			gotTexts = append(gotTexts, e.Text)
		}
	}
	for _, want := range []string{"claimed", "archived milestone"} {
		found := false
		for _, got := range gotTexts {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("want %q merged into Groups despite the memory lane failing, got %v", want, gotTexts)
		}
	}
}

// TestRunTimelineQueriesUnavailabilityFromResultAlone asserts the
// Availability field (not just a returned error) drives the per-lane
// Unavailable flags — a source can report "genuinely missing" (e.g. no
// .beads/ dir) with a nil error, per timeline.Result's own Availability doc
// comment.
func TestRunTimelineQueriesUnavailabilityFromResultAlone(t *testing.T) {
	beads := &stubBeadsQuerier{result: timeline.Result{Availability: timeline.Unavailable}}
	archive := &stubArchiveQuerier{result: timeline.Result{Availability: timeline.Available}}
	memory := &stubMemoryQuerier{result: timeline.Result{Availability: timeline.Available}}

	msg := runTimelineQueries(context.Background(), beads, archive, memory, store.Item{ID: "a"})
	if !msg.BeadUnavailable {
		t.Fatal("a nil-error Result with Availability=Unavailable must still mark BeadUnavailable")
	}
	if msg.ArchiveUnavailable || msg.MemoryUnavailable {
		t.Fatalf("only the bead lane reported Unavailable — got %+v", msg)
	}
}

// TestUpdateDoesNotBlockOnTimelineQueries is design.md's structural claim
// ("No source or git/bd Query call ever runs synchronously inside Update()")
// exercised as a runtime timing assertion rather than a source-reading
// claim: each stub querier sleeps 2 seconds, which would make Update itself
// take ~2s if the queries ran synchronously on the render path. Update must
// instead return near-instantly with a dispatched tea.Cmd, deferring the
// actual querying to whenever bubbletea invokes that Cmd.
func TestUpdateDoesNotBlockOnTimelineQueries(t *testing.T) {
	const slow = 2 * time.Second

	r := NewRoot(NewQueuePane(), NewDetailPane())
	r.timelineCtx = context.Background()
	r.beadsQuerier = &stubBeadsQuerier{delay: slow, result: timeline.Result{Availability: timeline.Available}}
	r.archiveQuerier = &stubArchiveQuerier{delay: slow, result: timeline.Result{Availability: timeline.Available}}
	r.memoryQuerier = &stubMemoryQuerier{delay: slow, result: timeline.Result{Availability: timeline.Available}}

	start := time.Now()
	_, cmd := r.Update(SnapshotMsg{Snapshot: store.Snapshot{Items: []store.Item{
		{ID: "a", Kind: store.KindBead, Title: "Alpha"},
	}}})
	elapsed := time.Since(start)

	if elapsed > 200*time.Millisecond {
		t.Fatalf("Update took %v (queriers each sleep %v) — Update must not block on the actual queries, it must only dispatch a tea.Cmd", elapsed, slow)
	}
	if cmd == nil {
		t.Fatal("want a non-nil tea.Cmd dispatched for the new selection (the debounce timer), not a nil no-op")
	}
}

// TestArmTimelineDebounceNilWhenNoQuerierWired asserts a Root with no
// EnableMemoryTimeline call (every pre-existing test/session before this
// wave) dispatches zero timeline work — armTimelineDebounce's own
// documented nil-guard.
func TestArmTimelineDebounceNilWhenNoQuerierWired(t *testing.T) {
	r := NewRoot(NewQueuePane(), NewDetailPane())
	if cmd := r.armTimelineDebounce(store.Item{ID: "a"}, true); cmd != nil {
		t.Fatal("want nil tea.Cmd when no timeline querier has been wired in")
	}
}

// TestArmTimelineDebounceDelaysDispatchByDebounceWindow is design.md's
// "debounce ~200ms" claim, measured against the real returned tea.Cmd rather
// than asserted from the timelineDebounce constant alone: invoking the Cmd
// blocks until the timer fires (bubbletea's tea.Tick semantics), so timing
// the call proves the actual delay, not just that a constant with the right
// value exists somewhere in the source.
func TestArmTimelineDebounceDelaysDispatchByDebounceWindow(t *testing.T) {
	r := NewRoot(NewQueuePane(), NewDetailPane())
	r.timelineCtx = context.Background()
	r.beadsQuerier, r.archiveQuerier, r.memoryQuerier = availableStubs()

	cmd := r.armTimelineDebounce(store.Item{ID: "a"}, true)
	if cmd == nil {
		t.Fatal("want a non-nil debounce tea.Cmd once queriers are wired")
	}

	start := time.Now()
	msg := cmd()
	elapsed := time.Since(start)

	if elapsed < timelineDebounce-30*time.Millisecond {
		t.Fatalf("debounce fired after only %v, want at least ~%v", elapsed, timelineDebounce)
	}
	if elapsed > timelineDebounce+500*time.Millisecond {
		t.Fatalf("debounce took %v, far longer than the ~%v window — dispatch should not itself be delayed further", elapsed, timelineDebounce)
	}

	dm, ok := msg.(timelineDebounceMsg)
	if !ok {
		t.Fatalf("want a timelineDebounceMsg from the debounce Cmd, got %T", msg)
	}
	if dm.item.ID != "a" {
		t.Fatalf("want the debounce msg to carry the armed item, got %+v", dm)
	}
}

// TestRapidReselectionSupersedesPendingDebounce is the generation-guard
// behavior design.md requires for "avoid firing on rapid arrow-key
// scrolling": arming a second debounce before the first one has fired must
// make the FIRST one's eventual message a no-op (dropped by the generation
// check), while the second (latest) one still dispatches — never both, which
// would mean two Query rounds for what was really one settled selection.
func TestRapidReselectionSupersedesPendingDebounce(t *testing.T) {
	r := NewRoot(NewQueuePane(), NewDetailPane())
	r.timelineCtx = context.Background()
	r.beadsQuerier, r.archiveQuerier, r.memoryQuerier = availableStubs()

	cmd1 := r.armTimelineDebounce(store.Item{ID: "first"}, true)
	cmd2 := r.armTimelineDebounce(store.Item{ID: "second"}, true) // rapid reselection, same debounce window

	// Both timers were started back-to-back; blocking on cmd1 first (it
	// waits out the full ~200ms window) means cmd2's own, slightly-later
	// timer has also already elapsed by the time we call it next.
	msg1 := cmd1()
	msg2 := cmd2()

	dm1, ok := msg1.(timelineDebounceMsg)
	if !ok {
		t.Fatalf("want timelineDebounceMsg, got %T", msg1)
	}
	dm2, ok := msg2.(timelineDebounceMsg)
	if !ok {
		t.Fatalf("want timelineDebounceMsg, got %T", msg2)
	}
	if dm1.gen == dm2.gen {
		t.Fatalf("the two rapid selections must arm distinct generations, got both = %d", dm1.gen)
	}

	// Route both through the real Root.Update dispatch path (case
	// timelineDebounceMsg), exactly as bubbletea would deliver them.
	if _, gotCmd := r.Update(dm1); gotCmd != nil {
		t.Fatal("the superseded (first) debounce message produced a dispatch — generation guard failed to drop it")
	}
	_, gotCmd2 := r.Update(dm2)
	if gotCmd2 == nil {
		t.Fatal("the latest (second) debounce message must still produce a dispatch cmd")
	}

	result := gotCmd2()
	tm, ok := result.(TimelineMsg)
	if !ok {
		t.Fatalf("want a TimelineMsg from the surviving dispatch, got %T", result)
	}
	if tm.ItemID != "second" {
		t.Fatalf("want the surviving dispatch to resolve for the LATEST selection %q, got %q", "second", tm.ItemID)
	}
}

// TestHandleTimelineMsgRoutesOnlyToTimelineAwarePanes asserts
// handleTimelineMsg forwards a completed TimelineMsg to every appended
// TimelineAware pane and leaves non-TimelineAware panes (spyPane) untouched.
func TestHandleTimelineMsgRoutesOnlyToTimelineAwarePanes(t *testing.T) {
	q := NewQueuePane()
	d := NewDetailPane()
	spy := &spyPane{}
	mtp := NewMemoryTimelinePane()
	r := &Root{panes: []Pane{q, d, spy, mtp}, queue: q, detail: d, now: time.Now}

	mtp.SetSelected(store.Item{ID: "a"}, true)
	_, cmd := r.Update(TimelineMsg{ItemID: "a", HasSelection: true, Groups: []timeline.DateGroup{
		{Date: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), Entries: []timeline.Entry{
			{Source: timeline.SourceBead, Text: "routed entry"},
		}},
	}})
	if cmd != nil {
		t.Fatal("handleTimelineMsg should not itself dispatch a further command")
	}

	if !strings.Contains(mtp.View(), "routed entry") {
		t.Fatalf("want the TimelineMsg routed into the appended TimelineAware pane, got:\n%s", mtp.View())
	}
}
