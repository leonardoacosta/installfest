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
		items = append(items, item)
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

// BlockerNote fields intentionally mirror internal/blocker.Note's shape
// (Type/Reason/Ref) so a future source can construct one directly from a
// parsed note without an adapter — see internal/blocker for the grammar.
