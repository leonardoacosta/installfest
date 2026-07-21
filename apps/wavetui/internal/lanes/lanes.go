// Package lanes implements wavetui-decision-lanes' pure derivation over an
// item's blocker note — see openspec/changes/wavetui-decision-lanes/
// design.md § Lane detection. Like every prior wavetui derivation package,
// internal/lanes never mutates Store state directly; it only derives a
// LaneState value from data the caller already has (an item's blocker-note
// Type, sourced from wavetui-core's internal/blocker grammar).
package lanes

import "time"

// LaneState is one item's decision-lane presentation-tracking state — a
// blocker note surfaced as an actionable badge/spawn-target. Shape is taken
// verbatim from design.md § Lane detection: do not add fields here without
// a corresponding design.md update, same convention internal/store's Item
// and SessionLink doc comments already establish for this codebase.
type LaneState struct {
	// Type is wavetui-core's BlockerNote.Type, copied verbatim — decision,
	// dependency, external, review, or any other token (unknown types are
	// accepted and render with a generic badge; see internal/blocker).
	Type string
	// Since is the first time this item's blocker note was observed in
	// this exact form (i.e. the same Type as the immediately prior
	// snapshot) — preserved across snapshots, not reset on every render.
	Since time.Time
	// PaneID is "" until a spawn has happened for this item (tasks.md
	// [2.1]/[3.2]'s Spawner wiring — out of scope for this batch).
	PaneID string
	// SpawnedAt is zero until spawned.
	SpawnedAt time.Time
}

// DetectLane derives the lane state for one item from its current
// blocker-note Type, preserving prior's Since/PaneID/SpawnedAt across
// snapshots when the type is unchanged. Callers key their prior-state
// lookup by item ID (not by note text), so identity is by item ID — the
// same LaneState pointer is returned, untouched, whenever blockerType
// still matches prior.Type; a changed or newly-blank type always produces
// a fresh (or nil) result and never reuses prior's Since/PaneID/SpawnedAt.
//
// blockerType is the item's current store.BlockerNote.Type, or "" when the
// item has no blocker note (item.Blocker == nil) — those two conditions
// collapse to the same "no lane" case per design.md's DetectLane doc
// comment ("returns nil when item.Blocker is nil or item.Blocker.Type ==
// \"\""), and internal/blocker.Parse's grammar (`[\w-]+` for the type
// capture group) never produces an empty Type for an actual match, so ""
// unambiguously means "no blocker."
//
// DetectLane deliberately does NOT take a store.Item (or any other
// internal/store type) as a parameter. internal/store additively holds a
// *lanes.LaneState field on Item (tasks.md [1.1]) — so a DetectLane
// signature accepting store.Item, or even *store.BlockerNote, would make
// internal/store and internal/lanes import each other, a compile-time
// import cycle Go's build simply refuses. Accepting only the one string
// value this function actually reads (item.Blocker.Type, already unwrapped
// by the caller) avoids the cycle entirely while deriving the identical
// result design.md specifies.
func DetectLane(blockerType string, prior *LaneState) *LaneState {
	if blockerType == "" {
		return nil
	}
	if prior != nil && prior.Type == blockerType {
		return prior
	}
	return &LaneState{Type: blockerType, Since: time.Now()}
}
