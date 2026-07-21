// Package flair implements wavetui-flair's optional, view-only animation
// layer described in openspec/changes/wavetui-flair/design.md.
//
// FlairManager is a pure consumer of two consecutive store.Snapshot values
// (see design.md § Snapshot diffing, a later batch's Diff function) — it
// never touches the Store, never touches the bus, and never mutates a
// Snapshot. If FlairManager panicked, was nil, or were compiled out
// entirely, every existing pane would render identically minus the optional
// overlay layer this package adds; nothing here is load-bearing for
// wavetui-core's own rendering.
package flair

import (
	"sort"

	"github.com/lucasb-eyer/go-colorful"

	"github.com/leonardoacosta/installfest/apps/wavetui/internal/config"
	"github.com/leonardoacosta/installfest/apps/wavetui/internal/store"
)

// animState is one in-flight animation, keyed by item ID in
// FlairManager.active. highlighter is the adapter (see highlight.go, task
// [3.2]) that steps the underlying effects.go effect object by one tick and
// reports its current HighlightState; it is nil only for the zero-value
// case manager_test.go constructs directly to exercise NeedsTick's
// map-based liveness check in isolation from real effect construction.
type animState struct {
	// Kind identifies which effect is animating (see design.md § Event ->
	// effect map / effects.go's EffectFor).
	Kind EffectKind
	// highlighter is unexported by design: only this package may ever
	// mutate the physics/timing state behind a live animation.
	highlighter frameHighlighter
}

// FlairManager holds all currently-live per-item animation state. The zero
// value is not usable — construct with NewFlairManager.
type FlairManager struct {
	// active is keyed by item ID; an empty map means no row animation is
	// live — see design.md § Tick-loop lifecycle. NeedsTick also considers
	// toast state (below), so this map alone is not the whole liveness
	// story once task [3.2]'s toast queue is in play.
	active map[string]animState
	// cfg gates whether this manager's Diff/effect machinery runs at all
	// (Process, below), and whether it runs in calm-mode (EffectFor, below)
	// — see design.md § Config + calm-mode + truecolor gating.
	cfg config.FlairConfig
	// diff is the diffing function Process delegates to when enabled. It
	// defaults to the package-level Diff (set by NewFlairManager); this
	// package's own tests may replace it on a manager instance with a
	// counting wrapper to prove the disabled-gate invariant without adding
	// test-only instrumentation to Diff itself.
	diff func(prev, next store.Snapshot) []FlairEvent

	// --- toast state (task [3.2], see highlight.go) -----------------------
	// At most one toast animates at a time; additional toast-worthy events
	// queue up and are promoted once the current one's effect Done()s — see
	// highlight.go's advanceToast.
	toastQueue  []queuedToast
	toastEffect toastEffect
	toastMsg    string
	toastAccent colorful.Color

	// --- presence sprites (sprite.go, design.md § Presence sprites) -------
	// spriteStates is keyed by item ID, re-derived fresh from every incoming
	// Snapshot by updateSpriteStates (sprite.go) — nil/empty means no item
	// currently has a linked session with a derivable sprite state (the
	// common case: flair disabled, or no item claimed by a live session).
	spriteStates map[string]SpriteState
	// spriteFrame is the single shared frame-cycle counter every live
	// sprite advances through in lockstep — see advanceSpriteFrame's doc
	// comment for why one shared counter is enough.
	spriteFrame int
}

// NewFlairManager constructs a FlairManager gated by cfg.
func NewFlairManager(cfg config.FlairConfig) *FlairManager {
	return &FlairManager{
		active: make(map[string]animState),
		cfg:    cfg,
		diff:   Diff,
	}
}

// NeedsTick reports whether any animation, toast, OR presence sprite is
// currently live. The root model is expected to call this after every
// Update() and only re-issue tea.Tick when it returns true — see design.md
// § Tick-loop lifecycle: "a tick is scheduled if and only if there is
// something left to animate." FlairManager itself never schedules a
// tea.Tick — this file contains no unconditional tick loop, and NeedsTick is
// a pure query, not a side-effecting call.
func (m *FlairManager) NeedsTick() bool {
	return len(m.active) > 0 || m.toastEffect != nil || len(m.toastQueue) > 0 || m.spritesAnimating()
}

// --- Snapshot diffing (design.md § Snapshot diffing) ---------------------

// EventKind identifies what kind of transition Diff detected between two
// consecutive store.Snapshot values. Values are taken verbatim from
// design.md § Snapshot diffing.
type EventKind string

const (
	EventItemClosed      EventKind = "item_closed"      // present in prev, absent in next
	EventItemAppeared    EventKind = "item_appeared"    // absent in prev, present in next
	EventBlockerResolved EventKind = "blocker_resolved" // Item.Blocker: non-nil -> nil
	EventNegative        EventKind = "negative"         // Item.Stale: false -> true (zombie-adjacent)
)

// FlairEvent is one detected transition, produced by Diff. Kind/ItemID are
// taken verbatim from design.md's sketch of this struct; ItemKind is an
// additive field not in that sketch — it is needed because effects.go's
// event->effect map (design.md § Event -> effect map) branches on
// Item.Kind for EventItemClosed/EventItemAppeared ("row flash" for
// KindBead vs. "toast banner" for KindProposal), and Diff already has the
// item in hand while resolving each event — carrying it forward here means
// effects.go never needs a second lookup against a Snapshot it was never
// handed.
type FlairEvent struct {
	Kind     EventKind
	ItemID   string
	ItemKind store.ItemKind
}

// Diff compares two consecutive Snapshot values and returns the FlairEvents
// describing what changed between them, per design.md § Snapshot diffing.
// Diff is a pure function: it never mutates prev or next (every field read
// below is a plain read, no assignment through either argument), never
// touches the Store or bus, and produces identical output across repeated
// calls with identical input — the explicit sort at the end exists
// specifically for that last property, since Go map iteration order is
// randomized per-run and would otherwise reshuffle event order on every
// call even though the underlying diff never changed.
func Diff(prev, next store.Snapshot) []FlairEvent {
	prevByID := indexItemsByID(prev.Items)
	nextByID := indexItemsByID(next.Items)

	var events []FlairEvent

	for id, prevItem := range prevByID {
		nextItem, stillPresent := nextByID[id]
		if !stillPresent {
			events = append(events, FlairEvent{Kind: EventItemClosed, ItemID: id, ItemKind: prevItem.Kind})
			continue
		}
		// Present in both snapshots — check per-item field transitions.
		if prevItem.Blocker != nil && nextItem.Blocker == nil {
			events = append(events, FlairEvent{Kind: EventBlockerResolved, ItemID: id, ItemKind: nextItem.Kind})
		}
		if !prevItem.Stale && nextItem.Stale {
			events = append(events, FlairEvent{Kind: EventNegative, ItemID: id, ItemKind: nextItem.Kind})
		}
	}

	for id, nextItem := range nextByID {
		if _, existedBefore := prevByID[id]; !existedBefore {
			events = append(events, FlairEvent{Kind: EventItemAppeared, ItemID: id, ItemKind: nextItem.Kind})
		}
	}

	sort.Slice(events, func(i, j int) bool {
		if events[i].ItemID != events[j].ItemID {
			return events[i].ItemID < events[j].ItemID
		}
		return events[i].Kind < events[j].Kind
	})

	return events
}

// indexItemsByID builds a lookup map over items without mutating items
// itself or any Item value inside it — every map entry is a copy (Go's
// value semantics for the range loop below), same as the caller's own
// Snapshot.Items slice.
func indexItemsByID(items []store.Item) map[string]store.Item {
	m := make(map[string]store.Item, len(items))
	for _, it := range items {
		m[it.ID] = it
	}
	return m
}

// --- Config gating (design.md § Config + calm-mode + truecolor gating) ---

// Process is FlairManager's per-Snapshot entrypoint — the root model calls
// this (not the package-level Diff directly) on each SnapshotMsg. When
// cfg.Enabled is false, Diff is never invoked at all: this is gating point
// 1 from design.md § Config + calm-mode + truecolor gating, "the literal
// disabled-equals-identical path — flair code does not run at all, not
// merely 'runs but suppresses output.'" m.diff defaults to the
// package-level Diff (see NewFlairManager) but is swappable within this
// package's own tests to prove that invariant with a call-counting spy,
// without adding test-only instrumentation to Diff itself.
func (m *FlairManager) Process(prev, next store.Snapshot) []FlairEvent {
	if !m.cfg.Enabled {
		return nil
	}
	return m.diff(prev, next)
}

// EffectFor resolves ev to the effect flair should render, honoring this
// manager's own CalmMode setting (gating point 2 from design.md § Config +
// calm-mode + truecolor gating). This wrapper is what keeps calm-mode
// routing centralized in manager.go per task [2.4]: a caller reaches
// calm-mode behavior through the manager, never by reading cfg.CalmMode
// itself and threading it into effects.go's EffectFor by hand. See
// effects.go for the full event->effect map and the structural guarantee
// that EffectShakeRedPulse is reachable only from EventNegative.
func (m *FlairManager) EffectFor(ev FlairEvent) EffectKind {
	return EffectFor(ev, m.cfg.CalmMode)
}
