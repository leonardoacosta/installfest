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
	// TouchedFiles is the set of file paths this item's own author declared
	// it touches — see wavetui-dispatch's design.md § Store additive field.
	// Populated by OpenSpecSource from a proposal's `- touches:` line (the
	// same author-declared, authoritative contract
	// `scripts/bin/wave-plan-build`'s parse_proposal_paths already treats as
	// the override for noisy text extraction); empty (not nil) for a bead,
	// which has no such declaration. Additive field: every existing source
	// leaves this nil, which is exactly the zero value, so no existing
	// caller needs to change. internal/wave's ConflictsFor is the consumer.
	TouchedFiles []string
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
	// Errors is the classified tool_result error feed for this session, most
	// recent last — see wavetui-sessions' design.md § Store additive fields
	// (API-batch addendum). ErrorCount above stays a cheap rolling total;
	// this is the richer per-entry record. No pane in this proposal renders
	// the feed itself — forward-compat scaffolding for a later proposal.
	Errors []ErrorEntry
	// ExecutorLaneFlag is true when this session is a Task-dispatched
	// subagent (isSidechain: true) whose assistant lines used an opus-tier
	// model — see design.md's addendum for why isSidechain is the chosen
	// proxy for "executor lane" (no real agent-role field exists on Item).
	ExecutorLaneFlag bool
}

// ErrorEntry is one classified tool_result error attributed to a linked
// session — see wavetui-sessions' design.md § Store additive fields
// (API-batch addendum) and spec.md's "Error feed attributes tool-result
// error classes" Requirement.
type ErrorEntry struct {
	Timestamp time.Time
	// Class is one of "read_first_violation", "edit_string_not_found",
	// "gate_blocked", or "unclassified" (the generic fallback — spec.md:
	// "still recorded in the error feed under a generic/unclassified class
	// rather than being discarded").
	Class string
	// Agent is "" when not determinable from transcript agent metadata.
	Agent   string
	Message string
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

// SessionLinkEvent updates ONLY the Session sub-field of item ItemID — unlike
// ItemUpsertEvent, it never touches Title/Kind/Blocker/etc. This exists
// because TranscriptSource (wavetui-sessions) does not own the base item —
// BeadsSource/OpenSpecSource do — so it must not republish a full Item it
// only partially knows about. Session == nil clears a previously-linked
// session (e.g. the transcript source lost track of it). If ItemID names an
// item that has not been published yet (a plausible race between sources),
// the Session value is cached and applied the moment that item's first
// ItemUpsertEvent arrives — see Store.Apply's ItemUpsertEvent case.
type SessionLinkEvent struct {
	ItemID  string
	Session *SessionLink
}

func (SessionLinkEvent) EventName() string { return "session.link" }

// RateLimitSignalEvent publishes a rate-limit backpressure indicator observed
// in a Claude Code transcript — see wavetui-sessions' design.md §
// Rate-limit backpressure: emit only, never consume. Store.Apply sets this as
// the Snapshot's current RateLimitBanner (overwriting any prior one); nothing
// in this proposal consumes or clears it beyond that.
type RateLimitSignalEvent struct {
	Signal RateLimitSignal
}

func (RateLimitSignalEvent) EventName() string { return "ratelimit.signal" }

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

	// pendingSessions holds a SessionLinkEvent's Session value for an
	// itemID that has not been published via ItemUpsertEvent yet — applied
	// as soon as that item arrives (see Apply's ItemUpsertEvent case).
	pendingSessions map[string]*SessionLink

	// rateLimit is the most recently published RateLimitSignal, or nil.
	// Independent of any single item — see Snapshot.RateLimitBanner.
	rateLimit *RateLimitSignal
}

// New constructs an empty Store.
func New() *Store {
	return &Store{
		items:           make(map[string]Item),
		deps:            make(map[string][]string),
		errors:          make(map[string]SourceError),
		pendingSessions: make(map[string]*SessionLink),
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
		item := e.Item
		// A source that doesn't know about sessions (BeadsSource,
		// OpenSpecSource) always publishes Item.Session == nil — and it
		// republishes on every requery cycle (poll or fsnotify-triggered),
		// not just once, since neither source tracks "have I already sent
		// this item" beyond its own last-good cache. Two distinct cases
		// need the incoming nil Session filled in rather than trusted
		// verbatim:
		//
		//  1. TranscriptSource resolved a link for this item BEFORE its
		//     base Item ever arrived — apply the cached pendingSessions
		//     value now rather than losing it to this upsert's zero value.
		//  2. This item already exists AND is already linked (a normal,
		//     repeated BeadsSource/OpenSpecSource republish of an item
		//     TranscriptSource previously attached a Session to via its own
		//     SessionLinkEvent). Without this branch, every such republish
		//     silently wiped the Session back to nil — a real regression
		//     caught live during tasks.md [4.5]'s runtime verification: a
		//     linked, zombie-badged SessionsPane row vanished the moment
		//     BeadsSource's own periodic requery re-published the same
		//     item, since case 1's pendingSessions lookup is empty once a
		//     link has already been applied (SessionLinkEvent's own case
		//     below deletes that pending entry the moment it fires). Only
		//     SessionLinkEvent (Session == nil, explicitly) is allowed to
		//     clear an existing link — an ItemUpsertEvent from a
		//     session-unaware source must never do so as a side effect of
		//     republishing the item it DOES own.
		if item.Session == nil {
			if pending, ok := s.pendingSessions[item.ID]; ok {
				item.Session = pending
			} else if existing, ok := s.items[item.ID]; ok {
				item.Session = existing.Session
			}
		}
		s.items[item.ID] = item
		if e.Deps != nil {
			s.deps[item.ID] = append([]string(nil), e.Deps...)
		}
	case ItemRemoveEvent:
		delete(s.items, e.ID)
		delete(s.deps, e.ID)
		delete(s.pendingSessions, e.ID)
	case SourceErrorEvent:
		s.errors[e.Error.Source] = e.Error
	case SourceOKEvent:
		delete(s.errors, e.Source)
	case SessionLinkEvent:
		if item, ok := s.items[e.ItemID]; ok {
			item.Session = e.Session
			s.items[e.ItemID] = item
			delete(s.pendingSessions, e.ItemID)
		} else if e.Session != nil {
			s.pendingSessions[e.ItemID] = e.Session
		} else {
			delete(s.pendingSessions, e.ItemID)
		}
	case RateLimitSignalEvent:
		sig := e.Signal
		s.rateLimit = &sig
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

	var banner *RateLimitSignal
	if s.rateLimit != nil {
		b := *s.rateLimit
		banner = &b
	}

	return Snapshot{
		Items:           items,
		Errors:          errs,
		Generated:       time.Now(),
		RateLimitBanner: banner,
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
	if item.TouchedFiles != nil {
		item.TouchedFiles = append([]string(nil), item.TouchedFiles...)
	}
	if item.Session != nil {
		sl := *item.Session
		if item.Session.TokensByModel != nil {
			sl.TokensByModel = make(map[string]int64, len(item.Session.TokensByModel))
			for k, v := range item.Session.TokensByModel {
				sl.TokensByModel[k] = v
			}
		}
		if item.Session.Errors != nil {
			sl.Errors = append([]ErrorEntry(nil), item.Session.Errors...)
		}
		item.Session = &sl
	}
	return item
}

// BlockerNote fields intentionally mirror internal/blocker.Note's shape
// (Type/Reason/Ref) so a future source can construct one directly from a
// parsed note without an adapter — see internal/blocker for the grammar.
