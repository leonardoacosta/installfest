// See openspec/changes/wavetui-sessions/tasks.md [3.2] and
// specs/wavetui/spec.md's "KPIBar renders continue-count, rate-limit
// incidents, and stale-claim minutes" Requirement. KPIBar implements
// wavetui-core's Pane interface and is appended to Root's focus ring the
// same append-only way SessionsPane and MemoryTimelinePane are — see
// sessionspane.go and memorytimelinepane.go for the established precedent
// this file follows.
package ui

import (
	"fmt"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/leonardoacosta/installfest/apps/wavetui/internal/sources"
	"github.com/leonardoacosta/installfest/apps/wavetui/internal/store"
)

// defaultKPIBarWidth sizes the bar before the first real tea.WindowSizeMsg
// arrives, mirroring every other appended pane's own default-width
// precedent (see sessionspane.go's defaultSessionsWidth).
const defaultKPIBarWidth = 96

// KPIBar renders three run-scoped metrics as a single status row:
//
//   - a continue-count PROXY: the number of currently-linked sessions whose
//     context gauge has crossed sources.ContextGaugeThresholdPct (the same
//     "would need a handoff/continue soon" signal transcript.go's own
//     IsHandoffThreshold derives for the context gauge's own badge). This is
//     a proxy, not a real continuation-event log — the Store carries no
//     historical "session N was continued" event, only each session's
//     current ContextPct — documented as a heuristic rather than a verified
//     count, the same honesty standard design.md's ExecutorLaneFlag
//     addendum already set for a comparably-derived signal.
//   - a rate-limit-incident counter: incremented once per DISTINCT
//     RateLimitSignal observed across Update calls. Snapshot.RateLimitBanner
//     is the CURRENT signal only (store.RateLimitSignalEvent's own doc
//     comment: applying it "overwrit[es] any prior one"), not a running
//     tally, so KPIBar tracks the last-seen Detected timestamp itself to
//     avoid re-counting the same still-active banner on every Snapshot.
//   - stale-claim minutes: elapsed time since the OLDEST currently
//     zombie-badged claim went stale, computed against Snapshot.Generated
//     (never time.Now()) so this stays deterministic under test.
type KPIBar struct {
	width int

	rateLimitCount  int
	lastSeenRateSig time.Time
	haveSeenRateSig bool

	continueCount int
	haveZombie    bool
	staleMinutes  int
}

// NewKPIBar constructs an empty KPIBar (zero counts, no signal seen yet).
func NewKPIBar() *KPIBar {
	return &KPIBar{width: defaultKPIBarWidth}
}

// Update implements Pane.
func (k *KPIBar) Update(snap store.Snapshot) Pane {
	k.observeRateLimit(snap.RateLimitBanner)
	k.observeSessions(snap)
	return k
}

// observeRateLimit increments rateLimitCount exactly once per distinct
// RateLimitSignal — see the KPIBar doc comment above for why a plain
// "banner present" check would over-count a still-active signal on every
// subsequent Snapshot.
func (k *KPIBar) observeRateLimit(banner *store.RateLimitSignal) {
	if banner == nil {
		return
	}
	if k.haveSeenRateSig && banner.Detected.Equal(k.lastSeenRateSig) {
		return
	}
	k.rateLimitCount++
	k.lastSeenRateSig = banner.Detected
	k.haveSeenRateSig = true
}

// observeSessions derives the continue-count proxy and the stale-claim
// minutes from the snapshot's linked sessions — see the KPIBar doc comment
// for both metrics' exact definitions.
func (k *KPIBar) observeSessions(snap store.Snapshot) {
	continueCount := 0
	haveZombie := false
	var oldestZombieSince time.Time

	for _, it := range snap.Items {
		sl := it.Session
		if sl == nil {
			continue
		}
		if sources.IsHandoffThreshold(sl.ContextPct) {
			continueCount++
		}
		if sl.Zombie && (!haveZombie || sl.ZombieSince.Before(oldestZombieSince)) {
			oldestZombieSince = sl.ZombieSince
			haveZombie = true
		}
	}

	k.continueCount = continueCount
	k.haveZombie = haveZombie
	k.staleMinutes = 0
	if haveZombie {
		if elapsed := int(snap.Generated.Sub(oldestZombieSince).Minutes()); elapsed > 0 {
			k.staleMinutes = elapsed
		}
	}
}

// View implements Pane.
func (k *KPIBar) View() string {
	width := k.width
	if width <= 0 {
		width = defaultKPIBarWidth
	}

	stale := "-"
	if k.haveZombie {
		stale = fmt.Sprintf("%dm", k.staleMinutes)
	}

	line := fmt.Sprintf(
		"Continue: %d   Rate-limit incidents: %d   Oldest stale claim: %s",
		k.continueCount, k.rateLimitCount, stale,
	)

	return lipgloss.NewStyle().Width(width).Bold(true).Render(line)
}

// Focusable implements Pane. KPIBar joins the focus ring like every other
// appended pane in this package (design.md § Pane implementation) — tasks.md
// [3.3] requires the focus ring to cycle through all five panes.
func (k *KPIBar) Focusable() bool { return true }

// SetSize implements the Sizeable optional interface (root.go). KPIBar is a
// single-row bar with nothing to scroll, so only width affects rendering;
// height is accepted (matching the Sizeable signature every other appended
// pane implements) and otherwise unused.
func (k *KPIBar) SetSize(width, height int) {
	k.width = width
	_ = height
}
