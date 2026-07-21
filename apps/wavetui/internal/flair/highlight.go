// highlight.go bridges design.md § Snapshot diffing's FlairEvents and §
// Event -> effect map's per-effect physics primitives (effects.go) into the
// two outputs the root model actually renders: a row-scoped highlight map
// (task [3.1]'s QueuePane.SetHighlights) and the toast overlay (task
// [2.3]'s ToastOverlay.Compose). This is task [3.2]'s "wire FlairManager
// into the root model": OnSnapshot/AdvanceFrame below are everything
// cmd/wavetui's wiring needs beyond Process/EffectFor/NeedsTick (already
// implemented by the DB/API batches).
package flair

import (
	"time"

	"github.com/lucasb-eyer/go-colorful"

	"github.com/leonardoacosta/installfest/apps/wavetui/internal/store"
)

// HighlightState is the per-item, per-frame row-rendering state
// FlairManager hands to the root model for QueuePane.SetHighlights
// (design.md § Architecture's "row-scoped highlight map" box). It carries
// only what a bubbles/v2 table cell can actually render — a foreground
// color and an optional short glyph prefix — never a sub-cell pixel
// offset: bubbles/v2's table has no primitive for that, so
// EffectRowSlideIn's harmonica x-offset (see effects.go's SlideInEffect)
// degrades to a fixed accent color plus a directional glyph for as long as
// the slide is in flight, rather than faking sub-character motion the
// widget cannot render.
type HighlightState struct {
	Color colorful.Color
	Glyph string
}

// ToastRender is the current frame's toast-overlay render state, returned
// by AdvanceFrame — cmd/wavetui's wiring passes these fields straight into
// ToastOverlay.Compose (task [2.3]).
type ToastRender struct {
	Message string
	Accent  colorful.Color
	YOffset float64
}

// tickInterval is the fixed per-tick duration every active animState/toast
// advances by — see design.md § Tick-loop lifecycle: FlairManager's tick
// loop runs at effects.go's frameRate, the same rate every harmonica spring
// in this package was tuned against via harmonica.FPS(frameRate).
const tickInterval = time.Second / time.Duration(frameRate)

// --- row highlight adapters -------------------------------------------
//
// effects.go's four row/glyph effect types each have a distinctly-shaped
// Advance() (different return arity/types) since each was implemented
// against its own primitive (color lerp, spring position, or both). These
// adapters normalize every one of them to the single frameHighlighter shape
// AdvanceFrame's loop below drives generically.

// frameHighlighter is the common shape every row-highlight effect is
// adapted to, so AdvanceFrame can drive all of them through one loop
// despite their differing native Advance signatures.
type frameHighlighter interface {
	advance() HighlightState
	done() bool
}

// rowFlashHighlighter drives EffectRowFlash — design.md § Event -> effect
// map's EventItemClosed(KindBead) row: "Row flash green -> decayed fade,
// small particle burst." particle is composed alongside e (not a second,
// independently-triggered effect) so both fire together off the same
// closed-bead event, per that table row. bubbles/v2's table has no
// per-cell primitive for rendering n independent particle positions (the
// same rendering ceiling documented on HighlightState above for
// EffectRowSlideIn's x-offset), so the burst degrades to a short-lived
// glyph overlay on top of the flash color rather than faking particle
// motion the widget cannot render — cleared automatically once the
// particle's own fixed particleTTL lifetime (effects.go) elapses.
type rowFlashHighlighter struct {
	e        *RowFlashEffect
	particle *ParticleBurstEffect
}

func (a *rowFlashHighlighter) advance() HighlightState {
	hl := HighlightState{Color: a.e.Advance()}
	if a.particle != nil {
		a.particle.Advance(tickInterval)
		if !a.particle.Done() {
			hl.Glyph = particleBurstGlyph
		}
	}
	return hl
}

// done reports settled only once BOTH the flash color and the particle
// burst have finished — a still-bursting particle must not have its glyph
// pruned out from under it just because the (typically slower) color
// spring happened to settle first.
func (a *rowFlashHighlighter) done() bool {
	if a.particle != nil && !a.particle.Done() {
		return false
	}
	return a.e.Done()
}

type slideInHighlighter struct {
	e     *SlideInEffect
	color colorful.Color
}

func (a *slideInHighlighter) advance() HighlightState {
	a.e.Advance()
	return HighlightState{Color: a.color, Glyph: slideInGlyph}
}
func (a *slideInHighlighter) done() bool { return a.e.Done() }

type glyphPulseHighlighter struct{ e *GlyphPulseEffect }

func (a *glyphPulseHighlighter) advance() HighlightState {
	return HighlightState{Color: a.e.Advance(tickInterval)}
}
func (a *glyphPulseHighlighter) done() bool { return a.e.Done() }

type shakeRedPulseHighlighter struct{ e *ShakeRedPulseEffect }

func (a *shakeRedPulseHighlighter) advance() HighlightState {
	_, c := a.e.Advance()
	return HighlightState{Color: c, Glyph: shakeAlertGlyph}
}
func (a *shakeRedPulseHighlighter) done() bool { return a.e.Done() }

// staticDwell is how long a calm-mode static highlight lingers before it
// self-clears — design.md § Config + calm-mode + truecolor gating point 2
// describes calm mode as "a one-shot static color swap with no fade", which
// still needs SOME finite lifetime or it would never leave `active` and
// NeedsTick would report true forever for that item.
const staticDwell = 800 * time.Millisecond

// staticHighlighter is the calm-mode fallback adapter: no spring/fade math
// is ever evaluated, just a fixed HighlightState held for staticDwell.
type staticHighlighter struct {
	state   HighlightState
	elapsed time.Duration
}

func (a *staticHighlighter) advance() HighlightState {
	a.elapsed += tickInterval
	return a.state
}
func (a *staticHighlighter) done() bool { return a.elapsed >= staticDwell }

// --- toast adapter -------------------------------------------------------

// toastEffect is the shape both ToastSpringEffect (animated) and
// staticToastEffect (calm-mode) implement, so advanceToast can drive either
// through one code path. *ToastSpringEffect already satisfies this
// interface via its existing Advance/Y/Done methods (effects.go) — no
// adapter wrapper needed for the animated case.
type toastEffect interface {
	Advance(time.Duration)
	Y() float64
	Done() bool
}

// staticToastEffect is calm mode's toast fallback: appears immediately
// fully on-screen (Y always 0, no spring-in) and holds for a fixed dwell
// with no spring-out motion either.
type staticToastEffect struct{ elapsed time.Duration }

func (e *staticToastEffect) Advance(d time.Duration) { e.elapsed += d }
func (e *staticToastEffect) Y() float64              { return 0 }
func (e *staticToastEffect) Done() bool              { return e.elapsed >= toastDismissAfter }

// queuedToast is a toast-worthy event waiting for the currently-active
// toast (if any) to finish before it gets its turn — see advanceToast.
type queuedToast struct {
	message string
	accent  colorful.Color
	out     bool // true: spring/appear then immediately back off (archived proposal)
	static  bool // true: calm-mode fallback, no ToastSpringEffect at all
}

// --- default colors -------------------------------------------------------
//
// neutralBase approximates a terminal's default foreground — every "settle
// toward" target below decays to it rather than an arbitrary color, so a
// finished animation's last frame reads as "back to normal", not "stuck on
// some other color."
var (
	neutralBase = colorful.Hsv(0, 0.0, 0.85)

	rowFlashFrom = colorful.Hsv(120, 0.85, 0.85) // vivid green flash, a just-closed bead
	rowFlashTo   = neutralBase

	slideInColor = colorful.Hsv(210, 0.7, 0.9) // accent for a newly-appeared bead row

	glyphPulseFrom = colorful.Hsv(45, 0.9, 0.9) // amber pulse, a resolved blocker
	glyphPulseTo   = neutralBase

	toastNewAccent      = colorful.Hsv(210, 0.7, 0.9)
	toastArchivedAccent = colorful.Hsv(0, 0.0, 0.6) // neutral — an archive is informational, not alarming
)

const (
	slideInGlyph       = "→"
	staticRowGlyph     = "●"
	staticGlyphMark    = "◆"
	shakeAlertGlyph    = "!"
	particleBurstGlyph = "✱"
)

// particleBurstCount is how many particles EffectRowFlash's composed
// ParticleBurstEffect radiates per closed-bead event — "small" per
// design.md's table row, matched to effects_test.go's own
// TestParticleBurstEffectSettles fixture count.
const particleBurstCount = 6

// OnSnapshot diffs prev->next (via Process — gated exactly as before this
// task: a disabled manager never calls Diff at all) and starts a fresh
// row-highlight animState or toast queue entry for every resulting event.
// Both prev and next are threaded through so a newly-queued toast can look
// up the item's Title for its message (FlairEvent deliberately carries only
// ID/Kind — see FlairEvent's doc comment — so this is the one place that
// lookup happens, rather than duplicating it at every call site). titleByID
// is seeded from prev FIRST and next SECOND (next overwrites) so a
// still-present item's current title wins, but an EventItemClosed item —
// which by definition is absent from next — still resolves to its
// last-known title from prev instead of falling back to the raw item ID.
// Returns the events Diff produced, mainly so callers/tests can inspect
// what fired.
func (m *FlairManager) OnSnapshot(prev, next store.Snapshot) []FlairEvent {
	events := m.Process(prev, next)

	// Presence-sprite state (sprite.go) is re-derived fresh on every
	// Snapshot, independent of whether Diff produced any FlairEvent this
	// call — it is a continuous per-item state, not event-driven. Gated by
	// cfg.Enabled directly (not m.Process's return) since a disabled
	// manager must never derive or hold sprite state either — the same
	// "disabled-equals-identical" invariant Process already enforces for
	// Diff.
	if !m.cfg.Enabled {
		m.spriteStates = nil
	} else {
		m.updateSpriteStates(next)
	}

	if len(events) == 0 {
		return events
	}

	titleByID := make(map[string]string, len(prev.Items)+len(next.Items))
	for _, it := range prev.Items {
		titleByID[it.ID] = it.Title
	}
	for _, it := range next.Items {
		titleByID[it.ID] = it.Title
	}

	for _, ev := range events {
		m.start(ev, titleByID[ev.ItemID])
	}
	return events
}

// start installs the animState or queued toast for one FlairEvent, per the
// resolved EffectKind from m.EffectFor — the single source of truth for
// animated-vs-calm-mode routing (see effects.go's EffectFor doc comment),
// so this switch never re-derives that decision itself.
func (m *FlairManager) start(ev FlairEvent, title string) {
	switch effect := m.EffectFor(ev); effect {
	case EffectToastIn:
		m.enqueueToast(ev, title, false, false)
	case EffectToastOut:
		m.enqueueToast(ev, title, true, false)
	case EffectStaticToast:
		m.enqueueToast(ev, title, ev.Kind == EventItemClosed, true)
	case EffectRowFlash:
		m.active[ev.ItemID] = animState{Kind: effect, highlighter: &rowFlashHighlighter{
			e:        NewRowFlashEffect(rowFlashFrom, rowFlashTo),
			particle: NewParticleBurstEffect(particleBurstCount),
		}}
	case EffectRowSlideIn:
		m.active[ev.ItemID] = animState{Kind: effect, highlighter: &slideInHighlighter{e: NewSlideInEffect(), color: slideInColor}}
	case EffectGlyphPulse:
		m.active[ev.ItemID] = animState{Kind: effect, highlighter: &glyphPulseHighlighter{e: NewGlyphPulseEffect(glyphPulseFrom, glyphPulseTo)}}
	case EffectShakeRedPulse:
		m.active[ev.ItemID] = animState{Kind: effect, highlighter: &shakeRedPulseHighlighter{e: NewShakeRedPulseEffect(neutralBase)}}
	case EffectStaticRowMark:
		m.active[ev.ItemID] = animState{Kind: effect, highlighter: &staticHighlighter{state: HighlightState{Color: rowFlashTo, Glyph: staticRowGlyph}}}
	case EffectStaticGlyph:
		m.active[ev.ItemID] = animState{Kind: effect, highlighter: &staticHighlighter{state: HighlightState{Color: glyphPulseTo, Glyph: staticGlyphMark}}}
	case EffectStaticAlert:
		m.active[ev.ItemID] = animState{Kind: effect, highlighter: &staticHighlighter{state: HighlightState{Color: AlarmRed, Glyph: shakeAlertGlyph}}}
	}
}

// enqueueToast queues one toast-worthy event. out selects the
// spring-immediately-back-out variant (an archived proposal); static skips
// ToastSpringEffect construction entirely in favor of staticToastEffect
// (calm mode) — see advanceToast.
func (m *FlairManager) enqueueToast(ev FlairEvent, title string, out, static bool) {
	msg, accent := toastMessage(ev, title)
	m.toastQueue = append(m.toastQueue, queuedToast{message: msg, accent: accent, out: out, static: static})
}

// toastMessage renders ev/title into the toast's display text and accent
// color. A missing title (item removed from the Store before this lookup,
// or a bare ID with no Title source ever set) falls back to the raw ID
// rather than an empty banner.
func toastMessage(ev FlairEvent, title string) (string, colorful.Color) {
	if title == "" {
		title = ev.ItemID
	}
	if ev.Kind == EventItemClosed {
		return "archived: " + title, toastArchivedAccent
	}
	return "new proposal: " + title, toastNewAccent
}

// AdvanceFrame steps every currently-active row highlight and toast by one
// tick, prunes anything that settled (done()) this frame, and returns the
// resulting row highlight map (nil when nothing is animating — task
// [3.1]'s "render unchanged when nil/empty" contract) plus the current
// toast render, if any. The root model's wiring calls this once per
// tea.Tick and once immediately after every OnSnapshot call — see
// design.md § Tick-loop lifecycle.
func (m *FlairManager) AdvanceFrame() (map[string]HighlightState, *ToastRender) {
	m.advanceSpriteFrame()

	var highlights map[string]HighlightState
	for id, st := range m.active {
		if st.highlighter == nil {
			continue
		}
		hl := st.highlighter.advance()
		if st.highlighter.done() {
			delete(m.active, id)
			continue
		}
		if highlights == nil {
			highlights = make(map[string]HighlightState, len(m.active))
		}
		highlights[id] = hl
	}
	return highlights, m.advanceToast()
}

// advanceToast steps the current toast (promoting the next queued one if
// none is active) and returns its render state, or nil once the queue is
// fully drained.
func (m *FlairManager) advanceToast() *ToastRender {
	if m.toastEffect == nil {
		if len(m.toastQueue) == 0 {
			return nil
		}
		next := m.toastQueue[0]
		m.toastQueue = m.toastQueue[1:]
		m.toastMsg = next.message
		m.toastAccent = next.accent
		if next.static {
			m.toastEffect = &staticToastEffect{}
		} else {
			m.toastEffect = NewToastSpringEffect(next.out)
		}
	}

	m.toastEffect.Advance(tickInterval)
	render := &ToastRender{Message: m.toastMsg, Accent: m.toastAccent, YOffset: m.toastEffect.Y()}
	if m.toastEffect.Done() {
		m.toastEffect = nil
	}
	return render
}
