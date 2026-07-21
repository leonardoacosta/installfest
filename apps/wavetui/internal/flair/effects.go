// effects.go implements design.md § Event -> effect map: the pure
// EventKind[+ItemKind]->EffectKind dispatch table, plus the harmonica- and
// go-colorful-backed physics/color primitives each resolved effect actually
// animates with. See manager.go's EffectFor wrapper for how calm-mode
// routes through this file's EffectFor.
package flair

import (
	"math"
	"time"

	"github.com/charmbracelet/harmonica"
	"github.com/lucasb-eyer/go-colorful"

	"github.com/leonardoacosta/installfest/apps/wavetui/internal/store"
)

// EffectKind identifies which per-event visual effect flair renders. Values
// are deliberately distinct from EventKind — one EventKind (crossed with
// ItemKind) resolves to exactly one EffectKind via EffectFor below.
type EffectKind string

const (
	// EffectNone is returned for any EventKind EffectFor does not
	// recognize (see EffectFor's doc comment on why this can never be the
	// negative-only effect below).
	EffectNone EffectKind = ""

	// Animated effects (design.md § Event -> effect map table).
	EffectRowFlash      EffectKind = "row_flash"       // EventItemClosed, KindBead
	EffectToastOut      EffectKind = "toast_fade_out"  // EventItemClosed, KindProposal (archived)
	EffectToastIn       EffectKind = "toast_spring_in" // EventItemAppeared, KindProposal
	EffectRowSlideIn    EffectKind = "row_slide_in"    // EventItemAppeared, KindBead
	EffectGlyphPulse    EffectKind = "glyph_pulse"     // EventBlockerResolved
	EffectShakeRedPulse EffectKind = "shake_red_pulse" // EventNegative — reserved, see EffectFor

	// Calm-mode static-glyph fallbacks (design.md § Config + calm-mode +
	// truecolor gating, point 2) — one per animated effect above, no frame
	// cycling, no spring/fade math ever evaluated for these.
	EffectStaticRowMark EffectKind = "static_row_mark"
	EffectStaticToast   EffectKind = "static_toast"
	EffectStaticGlyph   EffectKind = "static_glyph"
	EffectStaticAlert   EffectKind = "static_alert" // EventNegative calm-mode variant
)

// EffectFor resolves ev to the EffectKind flair should render, per
// design.md § Event -> effect map. When calmMode is true, every resolution
// routes to that effect's static-glyph fallback instead (design.md § Config
// + calm-mode + truecolor gating, point 2) — this is the ONLY place that
// routing decision is made, so a caller can never bypass calm-mode by
// picking an effect some other way.
//
// EventItemClosed/EventItemAppeared additionally branch on ev.ItemKind:
// design.md's table gives an explicit row for KindBead in both directions
// and for KindProposal appearing (a genuinely new proposal, toast
// spring-in); it does not give a distinct row for KindProposal closing
// (archived), stating only that "a 'proposal archived' event is a
// KindProposal item disappearing" reusing the same appear/disappear diff.
// EffectToastOut fills that one gap as the toast family's mirror-image
// (fade/spring back out) rather than inventing an unrelated primitive —
// same Layer/Canvas + harmonica-spring toast mechanism, reversed direction.
//
// Structural guarantee for task [2.2]: EffectShakeRedPulse/EffectStaticAlert
// are returned from exactly one branch, gated on ev.Kind == EventNegative.
// This switch has no default case and no fallthrough — every EventKind not
// explicitly matched (including any future EventKind a later change adds)
// falls through to EffectNone below the switch, never to the negative
// effect. Reaching EffectShakeRedPulse/EffectStaticAlert therefore requires
// explicitly writing `case EventNegative`, not merely omitting a case for a
// new event kind. TestShakeRedPulseOnlyFromNegative in effects_test.go
// checks this exhaustively against every other known EventKind, both
// ItemKinds, and both calmMode states.
func EffectFor(ev FlairEvent, calmMode bool) EffectKind {
	switch ev.Kind {
	case EventItemClosed:
		if ev.ItemKind == store.KindProposal {
			return pickEffect(EffectToastOut, EffectStaticToast, calmMode)
		}
		return pickEffect(EffectRowFlash, EffectStaticRowMark, calmMode)
	case EventItemAppeared:
		if ev.ItemKind == store.KindProposal {
			return pickEffect(EffectToastIn, EffectStaticToast, calmMode)
		}
		return pickEffect(EffectRowSlideIn, EffectStaticRowMark, calmMode)
	case EventBlockerResolved:
		return pickEffect(EffectGlyphPulse, EffectStaticGlyph, calmMode)
	case EventNegative:
		return pickEffect(EffectShakeRedPulse, EffectStaticAlert, calmMode)
	}
	return EffectNone
}

// pickEffect returns static when calmMode is set, animated otherwise. The
// single call site per EventKind branch in EffectFor is what makes calm-mode
// routing structurally impossible to skip for any resolved effect.
func pickEffect(animated, static EffectKind, calmMode bool) EffectKind {
	if calmMode {
		return static
	}
	return animated
}

// --- Physics/color primitives ---------------------------------------------
//
// Tuning constants below are harmonica's own (angularFrequency, damping)
// pair — see harmonica.NewSpring's doc comment for the over/critically/
// under-damped taxonomy this file relies on: shake wants strong
// under-damping (oscillation = the shake itself), flash/slide/toast want
// mild under-damping (quick settle, small overshoot).
const (
	frameRate = 30 // assumed tick rate once a flair effect is actively animating

	flashAngularFreq = 4.0
	flashDamping     = 0.6

	slideAngularFreq = 6.0
	slideDamping     = 0.8

	toastAngularFreq = 5.0
	toastDamping     = 0.7

	shakeAngularFreq = 18.0
	shakeDamping     = 0.15 // strongly under-damped: this oscillation IS the shake

	settleThreshold = 0.01 // |value| below this counts as "settled" for Done()

	shakeKickMagnitude = 1.0  // initial x-offset kick that decays into the shake
	toastOffscreenY    = -3.0 // toast's off-screen Y position (above the root view)

	particleSpeed = 6.0
	particleTTL   = 400 * time.Millisecond
)

// AlarmRed is the pulse target color for EffectShakeRedPulse — the one
// color this package reserves for EventNegative's alarm, per design.md's
// "reserved exclusively for this event kind" note. HSV rather than a raw
// hex/RGB literal so the color stays a clearly "pure alarm red" independent
// of gamma-correction quirks in a plain RGB guess.
var AlarmRed = colorful.Hsv(0, 0.9, 0.9)

func clamp01(v float64) float64 {
	return math.Max(0, math.Min(1, v))
}

// RowFlashEffect animates EffectRowFlash: a decaying color flash on a
// closed bead's row, combining harmonica's decay-spring settle fraction
// with a go-colorful RGB lerp from `from` to `to` — the two primitives task
// [2.2] requires ("harmonica-driven spring/decay for row flash ...
// go-colorful-driven lerp for fade") driven by one shared settle value
// rather than two disconnected timers.
type RowFlashEffect struct {
	spring   harmonica.Spring
	pos, vel float64
	from, to colorful.Color
}

// NewRowFlashEffect constructs a RowFlashEffect that lerps from from to to
// as its internal spring settles toward equilibrium (1.0).
func NewRowFlashEffect(from, to colorful.Color) *RowFlashEffect {
	return &RowFlashEffect{
		spring: harmonica.NewSpring(harmonica.FPS(frameRate), flashAngularFreq, flashDamping),
		from:   from,
		to:     to,
	}
}

// Advance steps the spring by one frame and returns this frame's lerped
// color.
func (e *RowFlashEffect) Advance() colorful.Color {
	e.pos, e.vel = e.spring.Update(e.pos, e.vel, 1.0)
	return e.from.BlendRgb(e.to, clamp01(e.pos))
}

// Done reports whether the spring has settled close enough to equilibrium
// that further frames would be visually imperceptible.
func (e *RowFlashEffect) Done() bool {
	return math.Abs(1.0-e.pos) < settleThreshold && math.Abs(e.vel) < settleThreshold
}

// SlideInEffect animates EffectRowSlideIn: a new bead's row sliding in from
// a horizontal offset toward its resting position (0), via a mild
// under-damped harmonica spring.
type SlideInEffect struct {
	spring   harmonica.Spring
	pos, vel float64
}

// slideStartOffset is how far off-screen (in cells) a sliding-in row starts.
const slideStartOffset = 6.0

// NewSlideInEffect constructs a SlideInEffect starting at slideStartOffset.
func NewSlideInEffect() *SlideInEffect {
	return &SlideInEffect{
		spring: harmonica.NewSpring(harmonica.FPS(frameRate), slideAngularFreq, slideDamping),
		pos:    slideStartOffset,
	}
}

// Advance steps the spring by one frame toward 0 and returns the current
// x-offset.
func (e *SlideInEffect) Advance() float64 {
	e.pos, e.vel = e.spring.Update(e.pos, e.vel, 0)
	return e.pos
}

// Done reports whether the row has settled at its resting position.
func (e *SlideInEffect) Done() bool {
	return math.Abs(e.pos) < settleThreshold && math.Abs(e.vel) < settleThreshold
}

// ShakeRedPulseEffect animates EffectShakeRedPulse — EventNegative's
// reserved effect. It combines a strongly under-damped harmonica spring
// (the horizontal shake, oscillating back to center) with a go-colorful
// lerp toward AlarmRed (the pulse). Exclusivity to EventNegative is
// enforced structurally by EffectFor's switch (see its doc comment), not by
// anything in this type — ShakeRedPulseEffect is simply never constructed
// from any code path that isn't already dispatching on EventNegative.
type ShakeRedPulseEffect struct {
	spring   harmonica.Spring
	pos, vel float64
	base     colorful.Color
}

// NewShakeRedPulseEffect constructs a ShakeRedPulseEffect that shakes
// around base's row and pulses its color toward AlarmRed.
func NewShakeRedPulseEffect(base colorful.Color) *ShakeRedPulseEffect {
	return &ShakeRedPulseEffect{
		spring: harmonica.NewSpring(harmonica.FPS(frameRate), shakeAngularFreq, shakeDamping),
		pos:    shakeKickMagnitude,
		base:   base,
	}
}

// Advance steps the spring by one frame and returns the current x-offset
// (the shake) and this frame's color (the pulse toward AlarmRed, strongest
// at the peak of the shake's displacement).
func (e *ShakeRedPulseEffect) Advance() (xOffset float64, c colorful.Color) {
	e.pos, e.vel = e.spring.Update(e.pos, e.vel, 0)
	t := clamp01(math.Abs(e.pos))
	return e.pos, e.base.BlendRgb(AlarmRed, t)
}

// Done reports whether the shake has settled back to center.
func (e *ShakeRedPulseEffect) Done() bool {
	return math.Abs(e.pos) < settleThreshold && math.Abs(e.vel) < settleThreshold
}

// GlyphPulseEffect animates EffectGlyphPulse: a plain go-colorful color
// lerp with no harmonica spring — design.md's primitive column for
// EventBlockerResolved is explicitly "go-colorful lerp, no particle", i.e.
// a time-driven glyph-color morph, not a physics-driven one.
type GlyphPulseEffect struct {
	from, to          colorful.Color
	elapsed, duration time.Duration
}

// glyphPulseDuration is how long EffectGlyphPulse's color morph takes.
const glyphPulseDuration = 500 * time.Millisecond

// NewGlyphPulseEffect constructs a GlyphPulseEffect lerping from from to to
// over glyphPulseDuration.
func NewGlyphPulseEffect(from, to colorful.Color) *GlyphPulseEffect {
	return &GlyphPulseEffect{from: from, to: to, duration: glyphPulseDuration}
}

// Advance accumulates frameDuration and returns this frame's lerped color.
func (e *GlyphPulseEffect) Advance(frameDuration time.Duration) colorful.Color {
	e.elapsed += frameDuration
	t := clamp01(float64(e.elapsed) / float64(e.duration))
	return e.from.BlendRgb(e.to, t)
}

// Done reports whether the morph has finished.
func (e *GlyphPulseEffect) Done() bool {
	return e.elapsed >= e.duration
}

// ParticleBurstEffect models EventItemClosed(KindBead)'s "small particle
// burst" sub-effect using harmonica's own Projectile simulator (harmonica's
// package doc: "a projectile simulator well suited for projectiles and
// particles") — n particles kicked outward at even angles under terminal
// gravity.
type ParticleBurstEffect struct {
	particles []*harmonica.Projectile
	age       time.Duration
}

// NewParticleBurstEffect constructs a burst of n particles radiating
// outward from the origin.
func NewParticleBurstEffect(n int) *ParticleBurstEffect {
	particles := make([]*harmonica.Projectile, n)
	for i := range particles {
		angle := 2 * math.Pi * float64(i) / float64(n)
		vel := harmonica.Vector{X: math.Cos(angle) * particleSpeed, Y: math.Sin(angle) * particleSpeed}
		particles[i] = harmonica.NewProjectile(harmonica.FPS(frameRate), harmonica.Point{}, vel, harmonica.TerminalGravity)
	}
	return &ParticleBurstEffect{particles: particles}
}

// Advance steps every particle by one frame and returns their new
// positions.
func (e *ParticleBurstEffect) Advance(frameDuration time.Duration) []harmonica.Point {
	e.age += frameDuration
	pts := make([]harmonica.Point, len(e.particles))
	for i, p := range e.particles {
		pts[i] = p.Update()
	}
	return pts
}

// Done reports whether the burst's fixed lifetime has elapsed.
func (e *ParticleBurstEffect) Done() bool {
	return e.age >= particleTTL
}

// ToastSpringEffect drives EffectToastIn/EffectToastOut's Y-offset spring
// per design.md § Event -> effect map ("lipgloss/v2 Layer/Canvas +
// harmonica spring (Y-offset)"). overlay.go's compositor (task [2.3])
// positions its toast Layer using this effect's current Y() each frame and
// calls Done() to know when to drop the layer — this file owns the
// physics, overlay.go owns compositing that physics into a rendered Layer.
type ToastSpringEffect struct {
	spring       harmonica.Spring
	y, vel       float64
	target       float64
	dismissAfter time.Duration
	elapsed      time.Duration
	dismissing   bool
}

// toastDismissAfter is how long a fully-sprung-in toast lingers before it
// starts springing back out on its own.
const toastDismissAfter = 3 * time.Second

// NewToastSpringEffect constructs a ToastSpringEffect. springingOut=false
// (EffectToastIn) starts off-screen and springs down to on-screen;
// springingOut=true (EffectToastOut, the archived-proposal case) starts
// on-screen and springs immediately back off-screen with no lingering
// dwell time.
func NewToastSpringEffect(springingOut bool) *ToastSpringEffect {
	e := &ToastSpringEffect{
		spring:       harmonica.NewSpring(harmonica.FPS(frameRate), toastAngularFreq, toastDamping),
		dismissAfter: toastDismissAfter,
	}
	if springingOut {
		e.dismissing = true
		e.target = toastOffscreenY
	} else {
		e.y = toastOffscreenY
	}
	return e
}

// Advance steps the spring by one frame. Once a spring-IN toast has settled
// on-screen (target == 0), elapsed on-screen dwell time accumulates toward
// dismissAfter; crossing that deadline flips the spring's target back
// off-screen so it auto-dismisses without any external caller having to
// track a separate timer.
func (e *ToastSpringEffect) Advance(frameDuration time.Duration) {
	e.y, e.vel = e.spring.Update(e.y, e.vel, e.target)
	if e.target == 0 && e.settled() {
		e.elapsed += frameDuration
		if e.elapsed >= e.dismissAfter && !e.dismissing {
			e.dismissing = true
			e.target = toastOffscreenY
		}
	}
}

// Y returns the toast layer's current Y-offset (0 == fully on-screen).
func (e *ToastSpringEffect) Y() float64 { return e.y }

func (e *ToastSpringEffect) settled() bool {
	return math.Abs(e.y-e.target) < settleThreshold && math.Abs(e.vel) < settleThreshold
}

// Done reports whether the toast has finished springing back off-screen and
// may be dropped entirely.
func (e *ToastSpringEffect) Done() bool {
	return e.dismissing && e.target == toastOffscreenY && e.settled()
}
