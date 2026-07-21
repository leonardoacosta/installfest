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
	"github.com/leonardoacosta/installfest/apps/wavetui/internal/config"
)

// animState is one in-flight animation, keyed by item ID in
// FlairManager.active. Its fields are populated by later batches (Diff's
// event dispatch and effects.go's spring/fade state) — this DB-batch
// skeleton only needs enough shape for NeedsTick's map-based liveness check
// to compile and be exercised by tests; it deliberately does not import
// harmonica/go-colorful yet, since nothing in this file constructs a
// non-empty animState.
type animState struct {
	// Kind identifies which effect is animating (see design.md § Event ->
	// effect map, implemented by a later batch's effects.go). Zero value is
	// fine today since this file never dispatches on it.
	Kind string
}

// FlairManager holds all currently-live per-item animation state. The zero
// value is not usable — construct with NewFlairManager.
type FlairManager struct {
	// active is keyed by item ID; an empty map means no animation is live
	// and NeedsTick reports false — see design.md § Tick-loop lifecycle.
	active map[string]animState
	// cfg gates whether this manager's (future) Diff/effect machinery runs
	// at all, and whether it runs in calm-mode — see design.md § Config +
	// calm-mode + truecolor gating. Not yet read by this file; a later
	// batch's gating logic consumes it.
	cfg config.FlairConfig
}

// NewFlairManager constructs a FlairManager gated by cfg.
func NewFlairManager(cfg config.FlairConfig) *FlairManager {
	return &FlairManager{
		active: make(map[string]animState),
		cfg:    cfg,
	}
}

// NeedsTick reports whether any animation is currently live. The root model
// is expected to call this after every Update() and only re-issue
// tea.Tick when it returns true — see design.md § Tick-loop lifecycle:
// "a tick is scheduled if and only if there is something left to animate."
// FlairManager itself never schedules a tea.Tick — this file contains no
// unconditional tick loop, and NeedsTick is a pure query over `active`, not
// a side-effecting call.
func (m *FlairManager) NeedsTick() bool {
	return len(m.active) > 0
}
