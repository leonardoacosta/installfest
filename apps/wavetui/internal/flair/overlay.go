// overlay.go implements design.md § Architecture's "lipgloss v2 Layer/Canvas
// overlay (toast, celebration) composited over root View() output" and §
// Config + calm-mode + truecolor gating's terminal color-profile detection +
// go-colorful nearest-ANSI-equivalent fallback.
//
// This is deliberately the ONLY file in this package (and in wavetui) that
// imports lipgloss/v2's Layer/Canvas/Compositor types. This is NOT a
// lipgloss-version split — every sibling pane (queuepane.go, detailpane.go,
// ...) already imports "charm.land/lipgloss/v2" too, same as this file, since
// wavetui-core's UI phase. The real reason overlay.go stands alone is
// narrower: it is the only file anywhere in wavetui that needs v2's
// Layer/Canvas/Compositor layered-rendering primitives (see design.md's
// Alternatives section on why those are a genuine new capability, not a
// version upgrade) — every sibling pane still renders single-layer strings
// via lipgloss/v2's ordinary Style/Join* API and never touches compositing.
// Per task [2.3]'s scope, this file does not touch queuepane.go or
// detailpane.go at all.
package flair

import (
	"io"
	"math"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/lucasb-eyer/go-colorful"
)

// DetectColorProfile resolves the terminal's real color-support level using
// the exact detection mechanism lipgloss/v2 itself documents and depends on
// (github.com/charmbracelet/colorprofile — see lipgloss/v2's color.go
// Complete()/CompleteFunc doc comment: "p := colorprofile.Detect(os.Stderr,
// os.Environ())"), rather than hand-rolled TERM/COLORTERM sniffing, per
// design.md § Config + calm-mode + truecolor gating point 3.
func DetectColorProfile(output io.Writer, environ []string) colorprofile.Profile {
	return colorprofile.Detect(output, environ)
}

// ansiPalette is the 16 standard ANSI colors (xterm's classic palette,
// normalized to go-colorful's 0-1 RGB range), used only as the candidate
// set NearestANSIColor searches below.
var ansiPalette = [16]colorful.Color{
	{R: 0, G: 0, B: 0},                               // 0  black
	{R: 128.0 / 255, G: 0, B: 0},                     // 1  red
	{R: 0, G: 128.0 / 255, B: 0},                     // 2  green
	{R: 128.0 / 255, G: 128.0 / 255, B: 0},           // 3  yellow
	{R: 0, G: 0, B: 128.0 / 255},                     // 4  blue
	{R: 128.0 / 255, G: 0, B: 128.0 / 255},           // 5  magenta
	{R: 0, G: 128.0 / 255, B: 128.0 / 255},           // 6  cyan
	{R: 192.0 / 255, G: 192.0 / 255, B: 192.0 / 255}, // 7  white
	{R: 128.0 / 255, G: 128.0 / 255, B: 128.0 / 255}, // 8  bright black
	{R: 1, G: 0, B: 0},                               // 9  bright red
	{R: 0, G: 1, B: 0},                               // 10 bright green
	{R: 1, G: 1, B: 0},                               // 11 bright yellow
	{R: 0, G: 0, B: 1},                               // 12 bright blue
	{R: 1, G: 0, B: 1},                               // 13 bright magenta
	{R: 0, G: 1, B: 1},                               // 14 bright cyan
	{R: 1, G: 1, B: 1},                               // 15 bright white
}

// NearestANSIColor returns the ansiPalette entry closest to c by Euclidean
// RGB distance, using go-colorful's own DistanceRgb — the "go-colorful ...
// distance-based nearest-color helpers" fallback design.md's gating point 3
// calls for on a 16-color (or lower) terminal, so a "flash green -> fade"
// sequence degrades to a plain ANSI color change instead of emitting
// truecolor escape codes the terminal cannot interpret.
func NearestANSIColor(c colorful.Color) colorful.Color {
	best := ansiPalette[0]
	bestDist := c.DistanceRgb(best)
	for _, cand := range ansiPalette[1:] {
		if d := c.DistanceRgb(cand); d < bestDist {
			best, bestDist = cand, d
		}
	}
	return best
}

// ResolveColor applies profile's gating to c: TrueColor passes c through
// unchanged; ANSI256 defers to colorprofile's own Convert (an existing,
// already-correct 256-color downsample — no need to reinvent that table);
// ANSI (16-color), ASCII, and NoTTY all fall back to NearestANSIColor's
// go-colorful distance search, since Profile.Convert itself returns nil for
// ASCII/NoTTY (no color support at all) and design.md's fallback is
// specifically framed as an ANSI (16-color) degrade, not a 256-color one.
func ResolveColor(profile colorprofile.Profile, c colorful.Color) colorful.Color {
	switch profile {
	case colorprofile.TrueColor:
		return c
	case colorprofile.ANSI256:
		if converted := profile.Convert(c); converted != nil {
			if cc, ok := colorful.MakeColor(converted); ok {
				return cc
			}
		}
		return NearestANSIColor(c)
	default: // ANSI, ASCII, NoTTY, Unknown
		return NearestANSIColor(c)
	}
}

// ToastOverlay composes wavetui-flair's one additive rendering surface — a
// toast banner springing in/out over the root view — using lipgloss/v2's
// Layer/Canvas compositor (design.md's Alternatives section: "lipgloss v1
// has no layered/z-indexed compositing primitive at all... v2's Layer/
// Canvas types are a genuinely new capability").
type ToastOverlay struct {
	profile colorprofile.Profile
}

// NewToastOverlay detects the real terminal color profile once (via
// DetectColorProfile) and caches it for every Compose call — detection
// isn't free, and a toast overlay's available color range can't change
// mid-terminal-session anyway.
func NewToastOverlay(output io.Writer, environ []string) *ToastOverlay {
	return &ToastOverlay{profile: DetectColorProfile(output, environ)}
}

// Compose renders msg as a toast banner positioned at yOffset (from a
// ToastSpringEffect's Y(), see effects.go) inside a width x height root
// view, and layers it over base via lipgloss/v2's Compositor + Canvas. The
// banner's accent color is resolved against this overlay's detected color
// profile (ResolveColor) before rendering, so a non-truecolor terminal gets
// the ANSI-degraded color rather than a raw truecolor escape sequence.
//
// When the toast is fully off-screen (yOffset at or above its own negative
// height — not yet sprung in, or already dismissed), Compose returns base
// unchanged: this package's whole additive-overlay invariant (design.md's
// Architecture section — "if FlairManager panicked, was nil, or was
// compiled out entirely, every existing pane would render identically")
// means an invisible toast must never alter base, not even by drawing an
// empty layer over it.
func (o *ToastOverlay) Compose(base, msg string, accent colorful.Color, yOffset float64, width, height int) string {
	if width <= 0 || height <= 0 {
		return base
	}

	bannerHeight := lipgloss.Height(msg)
	if yOffset <= float64(-bannerHeight) {
		return base
	}

	style := lipgloss.NewStyle().
		Foreground(ResolveColor(o.profile, accent)).
		Bold(true).
		Padding(0, 1)
	banner := style.Render(msg)

	baseLayer := lipgloss.NewLayer(base).ID("base").X(0).Y(0).Z(0)
	toastLayer := lipgloss.NewLayer(banner).ID("toast").X(0).Y(int(math.Round(yOffset))).Z(1)

	canvas := lipgloss.NewCanvas(width, height)
	canvas.Compose(lipgloss.NewCompositor(baseLayer, toastLayer))
	return canvas.Render()
}
