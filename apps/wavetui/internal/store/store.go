// Package store implements the single-writer Store described in
// openspec/changes/wavetui-core/design.md § Store data model. The exported
// Item/Snapshot/SourceError shapes are taken verbatim from that section —
// do not add fields here without a corresponding design.md update, since
// wavetui-dispatch depends on this shape staying stable.
package store

import (
	"sort"
	"sync"
	"time"

	"github.com/leonardoacosta/installfest/apps/wavetui/internal/bus"
)

// ItemKind identifies what kind of thing an Item represents. It is a plain
// string type specifically so a future sibling proposal (wavetui-dispatch's
// wave-file source) can introduce a new kind without touching this file's
// exported API. See design.md § Store data model.
type ItemKind string

const (
	KindBead     ItemKind = "bead"
	KindProposal ItemKind = "proposal"
	// KindWaveFile is intentionally NOT added here — wavetui-dispatch's
	// concern, deferred per design.md.
)

// BlockerNote is the parsed form of a "blocked: <type> - <reason> (see
// <ref>)" line — see internal/blocker for the grammar and parser. Ref is
// empty when the optional "(see ...)" suffix was not present.
type BlockerNote struct {
	Type   string
	Reason string
	Ref    string
}

// TaskProgress is a checkbox tally (e.g. tasks.md "[x]" counts, or a bead's
// sub-task completion). Nil on an Item that has no sub-tasks.
type TaskProgress struct {
	Done  int
	Total int
}

// Item is one row in the queue — a bead or an OpenSpec proposal. Shape is
// taken verbatim from design.md § Store data model.
type Item struct {
	ID           string
	Kind         ItemKind
	Title        string
	CreatedAt    time.Time
	Blocker      *BlockerNote  // nil when unblocked
	FanOutScore  int           // count of transitive dependents this item unblocks
	TaskProgress *TaskProgress // nil when not applicable (e.g. a bead with no sub-tasks)
	Stale        bool          // true when the backing CLI call failed and this is last-good data
	// SecondClass marks an item sourced from a directory that spec.md's
	// visibility-gate Requirement designates as "visually second-class when
	// enabled" — currently plans/ and advisor-plans/ (see
	// sources/openspec.go's parseFlatMarkdownDir). False for every
	// openspec/changes/ proposal and every bead; the UI (queuepane.go) reads
	// this to dim the row rather than rendering it identically to a real
	// proposal.
	SecondClass bool
	// Session is the linked Claude Code session's derived state — see
	// wavetui-sessions' design.md § Store additive fields. nil when no
	// Claude Code session has been linked to this item (the common case for
	// an unclaimed or claimed-but-sessionless item). Additive field: every
	// existing wavetui-core source leaves this nil, which is exactly the
	// zero value, so no existing caller needs to change.
	Session *SessionLink
}

// SessionLink is one claimed item's linked-session state, derived by
// wavetui-sessions' TranscriptSource (openspec/changes/wavetui-sessions/
// internal/sources/transcript.go) from a Claude Code transcript. Shape is
// taken verbatim from that proposal's design.md § Store additive fields —
// do not add fields here without a corresponding design.md update, same
// convention as Item's own doc comment above.
type SessionLink struct {
	SessionID string
	// PaneID is "" when TmuxSource has no @cc-state match for this
	// session's pane (not every session runs inside a cc-tmux-tracked
	// pane) — absence here is expected, not a failure.
	PaneID string
	// ContextPct is 0-100, derived from cumulative input+cache-read tokens
	// against an approximate model context-window size.
	ContextPct   float64
	LastActivity time.Time
	Zombie       bool
	ZombieSince  time.Time
	ErrorCount   int
	// TokensByModel is output tokens, keyed by model name.
	TokensByModel map[string]int64
}

// RateLimitSignal is a rate-limit backpressure indicator observed in a
// Claude Code transcript stream — see wavetui-sessions' design.md §
// Rate-limit backpressure: emit only, never consume. This proposal only
// renders a banner from it (KPIBar); no dispatch/scheduling component
// reads or acts on it. The exact shape is not dictated by design.md's
// Store-additive-fields Go block (that section only names the field,
// `Snapshot.RateLimitBanner *RateLimitSignal`) — Detected/Message is the
// minimal banner-rendering shape KPIBar needs, additively extensible by a
// later batch (design.md/tasks.md [2.4]) that actually populates it.
type RateLimitSignal struct {
	Detected time.Time
	// Message is human-readable banner text — typically the raw indicator
	// text observed in the transcript stream.
	Message string
}

// SourceError is per-source badge state — a source is never allowed to
// panic the Store, so a failed fetch surfaces here instead.
type SourceError struct {
	Source    string
	Message   string
	Timestamp time.Time
}

// Snapshot is an immutable, copy-on-write view of Store state at one point
// in time. Snapshot is returned by value and owns its own backing slices —
// a later Store mutation can never retroactively change a Snapshot already
// handed to a caller. See design.md § Store data model.
type Snapshot struct {
	Items     []Item
	Errors    []SourceError
	Generated time.Time
	// RateLimitBanner is nil when no active rate-limit signal exists. See
	// wavetui-sessions' design.md § Rate-limit backpressure. Independent of
	// any single Item — it is a whole-snapshot signal, not per-row state.
	RateLimitBanner *RateLimitSignal
}

// --- Events consumed by Store.Apply -----------------------------------

// ItemUpsertEvent publishes a new or updated Item. Deps lists the IDs this
// item depends on (blocked on) — used only to derive FanOutScore for the
// items named in Deps, and is not itself part of the exported Item shape
// (see design.md: Item deliberately carries no Deps field yet, since no
// source in this batch produces dependency edges — this is the internal
// scaffolding a later source publishes into).
type ItemUpsertEvent struct {
	Item Item
	Deps []string
}

func (ItemUpsertEvent) EventName() string { return "item.upsert" }

// ItemRemoveEvent removes an item (and its dep edges) from the Store.
type ItemRemoveEvent struct {
	ID string
}

func (ItemRemoveEvent) EventName() string { return "item.remove" }

// SourceErrorEvent records a per-source failure. It never causes Apply to
// panic or return an error — it is state, not a control-flow signal.
type SourceErrorEvent struct {
	Error SourceError
}

func (SourceErrorEvent) EventName() string { return "source.error" }

// SourceOKEvent clears a previously-recorded SourceError for Source, once
// that source recovers.
type SourceOKEvent struct {
	Source string
}

func (SourceOKEvent) EventName() string { return "source.ok" }

// --- Store --------------------------------------------------------------

// Store is the single writer for all derived wavetui state. Apply is the
// ONLY mutation path — nothing else may mutate items/deps/errors. Apply is
// intended to be called exclusively from the bus-delivery goroutine that
// subscribes this Store to the event bus (see design.md § Architecture),
// which is what makes it a "single writer" in practice; the mutex below
// exists so Snapshot() may still be called concurrently (e.g. from the
// bubbletea goroutine) without racing that writer.
type Store struct {
	mu     sync.Mutex
	items  map[string]Item
	deps   map[string][]string // itemID -> IDs it depends on
	errors map[string]SourceError
}

// New constructs an empty Store.
func New() *Store {
	return &Store{
		items:  make(map[string]Item),
		deps:   make(map[string][]string),
		errors: make(map[string]SourceError),
	}
}

// Apply mutates Store state in response to a single bus.Event. Unrecognized
// event types are ignored (tolerant — a future event type introduced by a
// sibling proposal is not a crash here, just a no-op until Store learns it).
func (s *Store) Apply(ev bus.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch e := ev.(type) {
	case ItemUpsertEvent:
		s.items[e.Item.ID] = e.Item
		if e.Deps != nil {
			s.deps[e.Item.ID] = append([]string(nil), e.Deps...)
		}
	case ItemRemoveEvent:
		delete(s.items, e.ID)
		delete(s.deps, e.ID)
	case SourceErrorEvent:
		s.errors[e.Error.Source] = e.Error
	case SourceOKEvent:
		delete(s.errors, e.Source)
	default:
		// Unknown event type: no-op, not an error.
		return
	}

	s.recomputeFanOutLocked()
}

// recomputeFanOutLocked derives each item's FanOutScore — the count of
// transitive dependents it unblocks — from the current dep graph. Must be
// called with mu held.
func (s *Store) recomputeFanOutLocked() {
	// dependents[X] = items that declared X in their Deps (i.e. items that
	// depend ON X, so resolving X unblocks them).
	dependents := make(map[string][]string, len(s.deps))
	for id, dependsOn := range s.deps {
		for _, dep := range dependsOn {
			dependents[dep] = append(dependents[dep], id)
		}
	}

	for id, item := range s.items {
		item.FanOutScore = countTransitive(id, dependents)
		s.items[id] = item
	}
}

// countTransitive counts the number of distinct nodes reachable from start
// via the dependents adjacency map, not counting start itself.
func countTransitive(start string, dependents map[string][]string) int {
	seen := map[string]bool{start: true}
	queue := append([]string(nil), dependents[start]...)
	for len(queue) > 0 {
		next := queue[0]
		queue = queue[1:]
		if seen[next] {
			continue
		}
		seen[next] = true
		queue = append(queue, dependents[next]...)
	}
	return len(seen) - 1
}

// Snapshot returns an immutable, copy-on-write view of current Store state.
// The returned value owns its own Items/Errors backing slices — mutating
// them, or a later Apply call, can never retroactively change this
// Snapshot.
func (s *Store) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()

	items := make([]Item, 0, len(s.items))
	for _, item := range s.items {
		items = append(items, cloneItem(item))
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })

	errs := make([]SourceError, 0, len(s.errors))
	for _, e := range s.errors {
		errs = append(errs, e)
	}
	sort.Slice(errs, func(i, j int) bool { return errs[i].Source < errs[j].Source })

	return Snapshot{
		Items:     items,
		Errors:    errs,
		Generated: time.Now(),
	}
}

// cloneItem returns a copy of item whose pointer fields (Blocker,
// TaskProgress, Session) are independently allocated, not shared with
// item's own pointers. Snapshot's doc comment claims "a later Store
// mutation can never retroactively change a Snapshot already handed to a
// caller" — a plain struct copy (Go's default `item` value semantics in the
// range loop above) already satisfies that for Item's primitive fields, but
// Blocker, TaskProgress, and (added by wavetui-sessions) Session are
// pointers, so without this, every Snapshot's Items would still share the
// exact *BlockerNote/*TaskProgress/*SessionLink the Store's internal map
// holds. Nothing in this codebase currently mutates through those pointers
// post-construction (every source always assigns a freshly allocated one —
// see sources/beads.go's toItem and sources/openspec.go's parseOneProposal),
// so this is defensive rather than fixing an observed corruption, but it is
// exactly the kind of latent bug that bites the first future caller that
// does mutate in place, so it's cheap to close now.
func cloneItem(item Item) Item {
	if item.Blocker != nil {
		b := *item.Blocker
		item.Blocker = &b
	}
	if item.TaskProgress != nil {
		tp := *item.TaskProgress
		item.TaskProgress = &tp
	}
	if item.Session != nil {
		sl := *item.Session
		if item.Session.TokensByModel != nil {
			sl.TokensByModel = make(map[string]int64, len(item.Session.TokensByModel))
			for k, v := range item.Session.TokensByModel {
				sl.TokensByModel[k] = v
			}
		}
		item.Session = &sl
	}
	return item
}

// BlockerNote fields intentionally mirror internal/blocker.Note's shape
// (Type/Reason/Ref) so a future source can construct one directly from a
// parsed note without an adapter — see internal/blocker for the grammar.
