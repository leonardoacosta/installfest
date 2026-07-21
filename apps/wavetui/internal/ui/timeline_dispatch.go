// timeline_dispatch.go implements the on-demand timeline query dispatcher —
// see openspec/changes/wavetui-memory-timeline/tasks.md [3.2] and design.md
// § On-demand querying. Root (root.go) owns the small pieces of dispatch
// state (last-known selection, current debounce generation, the three
// querier handles) the same way it already owns SnapshotMsg's own
// coalescing state (pending/lastApply/flushTimer) — this file adds a second,
// symmetric coalescing concern to the same model rather than inventing a
// second selection-tracking path, per design.md's own instruction to reuse
// the existing selection-threading mechanism.
//
// design.md's architecture diagram describes the end of this pipeline as
// "Program.Send(TimelineMsg{...})". Package ui holds no *tea.Program handle
// (only cmd/wavetui/main.go does, for the Store-driven SnapshotMsg path,
// which originates OUTSIDE the tea model entirely from fs-watcher
// goroutines). A selection change, by contrast, originates INSIDE the tea
// model (a key press or an applied Snapshot moving QueuePane's cursor), so
// this dispatcher uses bubbletea's own sanctioned async-execution primitive
// instead: a returned tea.Cmd, whose eventual Msg the runtime delivers back
// into Update exactly as an external Program.Send() would — the same idiom
// root.go's own flushMsg/tea.Tick and cmd/wavetui/flair_wiring.go's
// flairTickMsg/tea.Tick already use for their own off-Update() async work.
// No source or git/bd Query call ever runs synchronously inside Update()
// either way: runTimelineQueries below only ever executes inside a tea.Cmd
// closure, which the bubbletea runtime invokes on its own goroutine.
package ui

import (
	"context"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/leonardoacosta/installfest/apps/wavetui/internal/store"
	"github.com/leonardoacosta/installfest/apps/wavetui/internal/timeline"
)

// timelineDebounce is the ~200ms debounce window design.md specifies:
// "debounce ~200ms (avoid firing on rapid arrow-key scrolling)".
const timelineDebounce = 200 * time.Millisecond

// beadsHistoryQuerier/archiveQuerier/memoryQuerier are the narrow interfaces
// the dispatcher depends on instead of the concrete timeline.*Source types —
// same rationale as sources/beads.go's beadsCLI: lets a test inject a stub
// instead of touching real git/bd processes.
type beadsHistoryQuerier interface {
	Query(ctx context.Context, itemID string, childIDs []string) (timeline.Result, error)
}

type archiveQuerier interface {
	Query(ctx context.Context, proposalSlug string) (timeline.Result, error)
}

type memoryQuerier interface {
	Query(ctx context.Context) (timeline.Result, error)
}

// TimelineMsg carries the merged, per-item history the dispatcher produced
// for whichever item was selected when the query was issued — see
// design.md § On-demand querying. ItemID lets a TimelineAware consumer
// discard a result superseded by a newer selection (the debounce generation
// guard in handleTimelineDebounce already discards a superseded in-flight
// request before it even runs the queries; ItemID is kept as a second, cheap
// guard at the render end against any ordering surprise in how bubbletea
// happens to deliver async Cmd results).
//
// Per-lane unavailability is three explicit named fields, not a single map
// keyed by timeline.Source, because design.md's three lanes don't line up
// 1:1 with the Source enum: the memory lane can produce either
// SourceJournal or SourceDistilled entries depending on which of
// MemoryHistorySource's two paths ran, but "is the memory lane itself
// unavailable" is one question regardless of which path would have answered
// it.
type TimelineMsg struct {
	ItemID       string
	HasSelection bool
	Groups       []timeline.DateGroup

	BeadUnavailable    bool
	ArchiveUnavailable bool
	MemoryUnavailable  bool
}

// timelineDebounceMsg is the dispatcher's own internal tick, fired
// timelineDebounce after a selection change, carrying the generation and the
// selection as they were at the moment the debounce timer was armed. If
// Root's current generation no longer matches gen when this msg is handled
// (a newer selection change armed a fresher timer meanwhile), the msg is
// discarded — the same generation-guard shape root.go's own
// pending/flushTimer pair already uses for SnapshotMsg coalescing, applied
// here to selection changes instead.
type timelineDebounceMsg struct {
	gen  int
	item store.Item
	ok   bool
}

// armTimelineDebounce arms (or re-arms, superseding any prior pending one)
// the debounce timer for a selection change. Returns nil when no timeline
// querier has been wired in at all (EnableMemoryTimeline was never called —
// e.g. every pre-existing test that constructs a bare Root) so a Root with
// no memory-timeline pane dispatches zero timeline work.
func (r *Root) armTimelineDebounce(item store.Item, ok bool) tea.Cmd {
	if r.beadsQuerier == nil {
		return nil
	}
	r.timelineGen++
	gen := r.timelineGen
	return tea.Tick(timelineDebounce, func(time.Time) tea.Msg {
		return timelineDebounceMsg{gen: gen, item: item, ok: ok}
	})
}

// handleTimelineDebounce fires when a debounce timer armed by
// armTimelineDebounce elapses. A stale generation (superseded by a newer
// selection change during the debounce window) is silently dropped — this
// is the debounce itself: only the LAST selection change in any 200ms burst
// ever reaches an actual Query call, per design.md's "avoid firing on rapid
// arrow-key scrolling."
func (r *Root) handleTimelineDebounce(m timelineDebounceMsg) tea.Cmd {
	if m.gen != r.timelineGen {
		return nil
	}
	if !m.ok {
		return func() tea.Msg { return TimelineMsg{HasSelection: false} }
	}

	item := m.item
	ctx := r.timelineCtx
	beads, archive, memory := r.beadsQuerier, r.archiveQuerier, r.memoryQuerier
	return func() tea.Msg {
		return runTimelineQueries(ctx, beads, archive, memory, item)
	}
}

// handleTimelineMsg routes a completed TimelineMsg to every appended
// TimelineAware pane (MemoryTimelinePane, today) — see the TimelineAware
// interface in root.go. Not part of the Pane interface itself: most panes
// have no use for a TimelineMsg, mirroring why HandleKey/SetSelected/SetSize
// already live outside Pane.
func (r *Root) handleTimelineMsg(m TimelineMsg) (tea.Model, tea.Cmd) {
	for _, p := range r.panes {
		if ta, ok := p.(TimelineAware); ok {
			ta.SetTimeline(m)
		}
	}
	return r, nil
}

// runTimelineQueries runs the three timeline sources' Query calls
// CONCURRENTLY (per design.md § On-demand querying) and merges the result.
// This function's body only ever executes inside the tea.Cmd closure
// handleTimelineDebounce returns above — never synchronously inside
// Root.Update — so "off the render path" holds regardless of which of the
// two async mechanisms (bubbletea's own tea.Cmd, or a raw
// goroutine+Program.Send) actually delivers the eventual TimelineMsg; see
// this file's top-of-file doc comment for why tea.Cmd is the one used here.
func runTimelineQueries(ctx context.Context, beads beadsHistoryQuerier, archive archiveQuerier, memory memoryQuerier, item store.Item) TimelineMsg {
	var beadRes, archiveRes, memRes timeline.Result
	var beadErr, archiveErr, memErr error

	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		// childIDs: wavetui-core's Snapshot/Item expose no parent/child bead
		// hierarchy to traverse — verified: store.Item carries no such
		// field, and BeadsSource's own `deps` map (store.go's
		// recomputeFanOutLocked) is a "depends-on" graph for FanOutScore,
		// not a bead parent-child edge. There is no existing traversal
		// mechanism for design.md's "reusing whatever parent/child
		// traversal wavetui-core's BeadsSource already exposes" language to
		// point at, so the query is scoped to exactly the selected item's
		// own ID until a future source publishes that edge type.
		beadRes, beadErr = beads.Query(ctx, item.ID, nil)
	}()
	go func() {
		defer wg.Done()
		archiveRes, archiveErr = archive.Query(ctx, proposalSlugFor(item))
	}()
	go func() {
		defer wg.Done()
		memRes, memErr = memory.Query(ctx)
	}()
	wg.Wait()

	matched := timeline.MatchToBeads(memRes.Entries, beadRes.Entries, 0)
	groups := timeline.Interleave(beadRes.Entries, archiveRes.Entries, matched)

	return TimelineMsg{
		ItemID:             item.ID,
		HasSelection:       true,
		Groups:             groups,
		BeadUnavailable:    beadErr != nil || beadRes.Availability == timeline.Unavailable,
		ArchiveUnavailable: archiveErr != nil || archiveRes.Availability == timeline.Unavailable,
		MemoryUnavailable:  memErr != nil || memRes.Availability == timeline.Unavailable,
	}
}

// proposalSlugFor returns the OpenSpec proposal slug OpenSpecArchiveSource
// should query for, or "" (design.md: "a bead-kind item with no associated
// proposal slug gets no archive entry at all — a normal, badge-free empty
// lane"). Restricted to !SecondClass proposals: a plans/advisor-plans item
// (sources/openspec.go's parseFlatMarkdownDir) is also store.KindProposal
// but carries an ID like "plan:name", not a real openspec/changes/ slug —
// there is no archive/ equivalent for that flat-markdown convention, so
// querying it would only ever glob-miss (harmlessly, per
// OpenSpecArchiveSource.Query's own empty-slug/no-match handling) rather
// than answer a real question.
func proposalSlugFor(item store.Item) string {
	if item.Kind == store.KindProposal && !item.SecondClass {
		return item.ID
	}
	return ""
}
