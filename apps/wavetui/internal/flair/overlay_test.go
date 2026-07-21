package flair

import (
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/lucasb-eyer/go-colorful"
)

// TestDetectColorProfileNoTTY confirms DetectColorProfile delegates to the
// real charmbracelet/colorprofile detection rather than hand-rolled TERM
// sniffing — writing to a non-terminal io.Writer (a plain bytes.Buffer via
// t.Output equivalent) with no color-related env vars should resolve to a
// no-color-support profile.
func TestDetectColorProfileNoTTY(t *testing.T) {
	var buf discardWriter
	got := DetectColorProfile(buf, nil)
	if got != colorprofile.NoTTY && got != colorprofile.Unknown && got != colorprofile.ASCII {
		t.Fatalf("want a no-color-support profile for a non-tty writer with empty env, got %v", got)
	}
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

// TestResolveColorTrueColorPassthrough confirms TrueColor never downsamples.
func TestResolveColorTrueColorPassthrough(t *testing.T) {
	c := colorful.Color{R: 0.2, G: 0.4, B: 0.9}
	got := ResolveColor(colorprofile.TrueColor, c)
	if got != c {
		t.Fatalf("want TrueColor passthrough %+v, got %+v", c, got)
	}
}

// TestResolveColorANSIDegradesToPaletteEntry confirms the ANSI (16-color)
// branch always resolves to one of the 16 known ansiPalette entries, never
// an arbitrary truecolor value the terminal couldn't render.
func TestResolveColorANSIDegradesToPaletteEntry(t *testing.T) {
	inputs := []colorful.Color{
		{R: 0.05, G: 0.9, B: 0.05},
		{R: 0.98, G: 0.02, B: 0.02},
		{R: 0.5, G: 0.5, B: 0.5},
		{R: 0.13, G: 0.55, B: 0.87},
	}
	for _, in := range inputs {
		got := ResolveColor(colorprofile.ANSI, in)
		found := false
		for _, p := range ansiPalette {
			if p == got {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("ResolveColor(ANSI, %+v) = %+v, not a member of ansiPalette", in, got)
		}
	}
}

// TestNearestANSIColorPicksClosest sanity-checks the distance search against
// two unambiguous cases: pure green should resolve nearer to bright green
// than to bright red, and vice versa.
func TestNearestANSIColorPicksClosest(t *testing.T) {
	green := NearestANSIColor(colorful.Color{R: 0, G: 1, B: 0})
	if green != ansiPalette[10] { // bright green
		t.Fatalf("want bright green palette entry for pure green input, got %+v", green)
	}

	red := NearestANSIColor(colorful.Color{R: 1, G: 0, B: 0})
	if red != ansiPalette[9] { // bright red
		t.Fatalf("want bright red palette entry for pure red input, got %+v", red)
	}
}

// TestToastOverlayComposeOffscreenIsNoop confirms Compose returns base
// unchanged when the toast is fully off-screen — the additive-overlay
// invariant this whole package is built around.
func TestToastOverlayComposeOffscreenIsNoop(t *testing.T) {
	o := &ToastOverlay{profile: colorprofile.TrueColor}
	base := "unchanged base view"
	got := o.Compose(base, "a toast message", colorful.Color{R: 0, G: 1, B: 0}, -10, 40, 10)
	if got != base {
		t.Fatalf("want base returned unchanged for an off-screen toast, got %q", got)
	}
}

// TestToastOverlayComposeOnscreenChangesOutput confirms an on-screen toast
// actually alters the composed output (the compositor is exercised, not a
// silent no-op).
func TestToastOverlayComposeOnscreenChangesOutput(t *testing.T) {
	o := &ToastOverlay{profile: colorprofile.TrueColor}
	base := "unchanged base view"
	got := o.Compose(base, "a toast message", colorful.Color{R: 0, G: 1, B: 0}, 0, 40, 10)
	if got == base {
		t.Fatal("want an on-screen toast to change the composed output")
	}
}
