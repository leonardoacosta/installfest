// sprite.go implements design.md § Presence sprites (Clawd) — conditional,
// additive. wavetui-flair's original tasks.md [3.4] deferred this file
// entirely because, at authoring time, store.Item had no session-state
// accessor; wavetui-sessions has since shipped one (store.Item.Session
// *store.SessionLink, internal/store/store.go). This file is that
// follow-up (if-z7pm / if-u7ul.1).
//
// Art direction (design.md's own note, carried forward verbatim): "Clawd is
// Anthropic's mascot — render an original homage (a generic terminal
// crab-friend silhouette, not the actual mascot's likeness), since this is
// a personal tool, not an Anthropic-branded surface." The frame glyphs below
// are a claw-pinch motif — deliberately abstract (angle-bracket/asterisk
// glyphs, not a literal crab pictogram) rather than a recognizable mascot
// rendering.
package flair

import "github.com/leonardoacosta/installfest/apps/wavetui/internal/store"

// SpriteState is one presence-sprite state. See DeriveSpriteState's doc
// comment for exactly which of design.md's six named states
// (working/thinking/blocked-on-you/zombie/done/swarm) are genuinely
// reachable from the shipped store.SessionLink shape — only three are.
type SpriteState string

const (
	// SpriteStateActive covers both "working" and "thinking" from
	// design.md's vocabulary: SessionLink carries no signal that
	// distinguishes those two (no per-turn phase field), so both collapse
	// onto the one state a live, non-zombie, non-swarm session can report.
	SpriteStateActive SpriteState = "active"
	// SpriteStateSwarm maps to SessionLink.ExecutorLaneFlag — "this session
	// is a Task-dispatched subagent" (see store.go's doc comment: "isSidechain
	// is the chosen proxy for 'executor lane'"). That is the closest real
	// signal to design.md's "swarm" state — a session participating in a
	// multi-agent dispatch, not a solo session.
	SpriteStateSwarm SpriteState = "swarm"
	// SpriteStateZombie maps directly to SessionLink.Zombie — the exact
	// bool the field already is, no derivation needed.
	SpriteStateZombie SpriteState = "zombie"
)

// DeriveSpriteState maps an Item's linked session onto one of the three
// presence-sprite states genuinely derivable from store.SessionLink today.
// ok is false when session is nil — the common case for an unclaimed or
// claimed-but-sessionless item — meaning no sprite renders for that item at
// all, per design.md's "conditional, additive" framing.
//
// design.md § Presence sprites names a six-state vocabulary (working/
// thinking/blocked-on-you/zombie/done/swarm) "mapped directly to [a] state
// field — never a second state machine." wavetui-sessions' actual shipped
// SessionLink carries no such single state field — only Zombie/ZombieSince
// (a derived bool), ExecutorLaneFlag (the isSidechain proxy),
// ContextPct/ErrorCount/LastActivity/PaneID. Of the six named states, only
// three are reachable from that shape without inventing a second state
// machine or composing across OTHER Item fields (which "never a second
// state machine" forbids just as much as inventing new state does):
//
//   - zombie: Session.Zombie == true
//   - swarm:  Session.ExecutorLaneFlag == true
//   - active: a live, non-zombie, non-swarm session — covers "working" and
//     "thinking" both, since nothing on SessionLink distinguishes them
//
// "blocked-on-you" is NOT derivable from Session data alone — that signal
// lives on Item.Blocker, a sibling field, and reaching across to it would
// be exactly the cross-field composite "never a second state machine" rules
// out for this sprite. "done" is not derivable either: a closed item stops
// appearing in Snapshot.Items entirely (see manager.go's Diff /
// EventItemClosed) — there is no Session left to read a state from once
// that happens. Both omissions are a data-availability finding, not an
// oversight: this task was scoped explicitly to "map to whatever states ARE
// genuinely derivable," not to invent placeholder signals for the rest.
func DeriveSpriteState(session *store.SessionLink) (SpriteState, bool) {
	if session == nil {
		return "", false
	}
	switch {
	case session.Zombie:
		return SpriteStateZombie, true
	case session.ExecutorLaneFlag:
		return SpriteStateSwarm, true
	default:
		return SpriteStateActive, true
	}
}

// spriteFrames is the 2-4 frame animated cycle per state (design.md: "a
// small 2-4 frame cycle sprite"). Every entry is a single rune-width glyph,
// matching the existing HighlightState.Glyph convention (renderTitleCell
// prepends "<glyph> " ahead of the item title) so column widths tuned
// against that convention are unaffected.
var spriteFrames = map[SpriteState][]string{
	SpriteStateActive: {"⋋", "⋌"},      // claw pinch open/closed
	SpriteStateSwarm:  {"⋋", "✦", "⋌"}, // pinch + a flourish for "more than one"
	SpriteStateZombie: {"⋋", "×"},      // pinch + a broken/negative mark
}

// spriteStaticGlyphs is the single-frame fallback design.md's calm-mode
// gating point 2 requires: "the presence sprite renders its current
// state's single glyph with no frame cycling."
var spriteStaticGlyphs = map[SpriteState]string{
	SpriteStateActive: "⋋",
	SpriteStateSwarm:  "✦",
	SpriteStateZombie: "×",
}

// spriteGlyph resolves state/frame/calm to the glyph FlairManager.SpriteGlyphs
// hands QueuePane for one item this render. calm short-circuits straight to
// the static glyph (no frame-index dependency at all — design.md's calm-mode
// contract). frame wraps via modulo so any monotonically-increasing tick
// counter works regardless of a state's frame-count (2, 3, or 4).
func spriteGlyph(state SpriteState, frame int, calm bool) string {
	if calm {
		return spriteStaticGlyphs[state]
	}
	frames := spriteFrames[state]
	if len(frames) == 0 {
		return ""
	}
	return frames[frame%len(frames)]
}

// updateSpriteStates re-derives m.spriteStates from next.Items — called by
// OnSnapshot on every incoming Snapshot (gated by cfg.Enabled there, same
// disabled-equals-identical invariant every other flair mechanism follows).
// Unlike row-highlight animState, sprite state is NOT event-driven: it is a
// continuous per-item state re-derived fresh from the current Session on
// every Snapshot, never carried forward/diffed against a prior value — an
// item's sprite state is exactly what DeriveSpriteState says it is right
// now, nothing more.
func (m *FlairManager) updateSpriteStates(next store.Snapshot) {
	if len(next.Items) == 0 {
		m.spriteStates = nil
		return
	}
	states := make(map[string]SpriteState, len(next.Items))
	for _, it := range next.Items {
		if st, ok := DeriveSpriteState(it.Session); ok {
			states[it.ID] = st
		}
	}
	if len(states) == 0 {
		states = nil
	}
	m.spriteStates = states
}

// spritesAnimating reports whether any live sprite state currently needs
// frame-cycling ticks — the sprite half of NeedsTick's contract (design.md
// § Tick-loop lifecycle: "a tick is scheduled if and only if there is
// something left to animate"). False in calm mode: a static glyph never
// needs another tick to look right, matching gating point 2's "no frame
// cycling" contract exactly — ticking anyway would burn CPU for a frame
// nothing on screen would change.
func (m *FlairManager) spritesAnimating() bool {
	return !m.cfg.CalmMode && len(m.spriteStates) > 0
}

// advanceSpriteFrame steps the shared frame counter every AdvanceFrame call,
// but only while spritesAnimating (mirrors advanceToast/animState's own
// "only step what's actually live" discipline). One shared counter (rather
// than a per-item counter) is deliberate: every sprite of the same state
// cycles in lockstep, which is the simplest, cheapest implementation that
// still satisfies "2-4 frame cycle" — nothing in design.md asks for
// per-item phase offsets.
func (m *FlairManager) advanceSpriteFrame() {
	if !m.spritesAnimating() {
		return
	}
	m.spriteFrame++
}

// SpriteGlyphs returns the current frame's per-item presence-sprite glyph
// map for every item that currently has a derivable sprite state — nil when
// there are none (flair disabled, no item has a linked session, or
// spriteStates hasn't been populated by a Snapshot yet), so QueuePane.
// SetSpriteGlyphs(nil) renders identically to before this method existed.
func (m *FlairManager) SpriteGlyphs() map[string]string {
	if len(m.spriteStates) == 0 {
		return nil
	}
	glyphs := make(map[string]string, len(m.spriteStates))
	for id, st := range m.spriteStates {
		if g := spriteGlyph(st, m.spriteFrame, m.cfg.CalmMode); g != "" {
			glyphs[id] = g
		}
	}
	if len(glyphs) == 0 {
		return nil
	}
	return glyphs
}
