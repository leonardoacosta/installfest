package main

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/leonardoacosta/installfest/apps/wavetui/internal/config"
	"github.com/leonardoacosta/installfest/apps/wavetui/internal/flair"
	"github.com/leonardoacosta/installfest/apps/wavetui/internal/store"
	"github.com/leonardoacosta/installfest/apps/wavetui/internal/ui"
)

// collectTickMsgs unwraps a tea.Cmd that may be nil, a single Cmd, or a
// tea.Batch-produced tea.BatchMsg, and returns every flairTickMsg it finds
// by actually invoking the leaf commands — the same thing the real
// bubbletea runtime does when it executes a returned Cmd.
func collectTickMsgs(cmd tea.Cmd) []flairTickMsg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	switch m := msg.(type) {
	case tea.BatchMsg:
		var found []flairTickMsg
		for _, sub := range m {
			found = append(found, collectTickMsgs(sub)...)
		}
		return found
	case flairTickMsg:
		return []flairTickMsg{m}
	default:
		return nil
	}
}

// newTestWrapper builds a rootWithFlair the same way run() does, without
// needing a real terminal/program.
func newTestWrapper(cfg config.FlairConfig) *rootWithFlair {
	root := ui.NewRoot(ui.NewQueuePane(), ui.NewDetailPane())
	mgr := flair.NewFlairManager(cfg)
	return newRootWithFlair(root, mgr, nil)
}

// TestUpdateSchedulesNoTickWhenNeedsTickFalse is this batch's MANDATORY
// verification: when FlairManager.NeedsTick() is false, Update must not
// schedule a tea.Tick at all — not merely that the manager itself doesn't
// need one, but that the ROOT MODEL (rootWithFlair, the actual tea.Model
// handed to tea.NewProgram in main.go) never issues the tick command. A
// disabled manager (Enabled:false) never starts any animation or toast, so
// NeedsTick() stays false through every SnapshotMsg.
func TestUpdateSchedulesNoTickWhenNeedsTickFalse(t *testing.T) {
	w := newTestWrapper(config.FlairConfig{Enabled: false})

	_, cmd := w.Update(ui.SnapshotMsg{Snapshot: store.Snapshot{
		Items: []store.Item{{ID: "a", Kind: store.KindBead, Title: "Alpha"}},
	}})
	// First-ever snapshot never diffs (no prior baseline) — confirm no
	// tick here either, then feed a real transition.
	if ticks := collectTickMsgs(cmd); len(ticks) != 0 {
		t.Fatalf("want no flairTickMsg scheduled on the seed snapshot, got %d", len(ticks))
	}

	_, cmd = w.Update(ui.SnapshotMsg{Snapshot: store.Snapshot{}}) // item "a" closes
	if w.mgr.NeedsTick() {
		t.Fatal("setup invariant broken: a disabled manager must never report NeedsTick()==true")
	}
	if ticks := collectTickMsgs(cmd); len(ticks) != 0 {
		t.Fatalf("want NO flairTickMsg scheduled by Update when NeedsTick()==false, got %d", len(ticks))
	}

	// And the tick message itself must never self-perpetuate: feeding one
	// directly (as if the Program had delivered a stray one) still must not
	// re-schedule another while idle.
	_, cmd = w.Update(flairTickMsg{})
	if ticks := collectTickMsgs(cmd); len(ticks) != 0 {
		t.Fatalf("want no flairTickMsg re-scheduled from an idle flairTickMsg, got %d", len(ticks))
	}
}

// TestUpdateSchedulesTickWhenNeedsTickTrue is the other side of the same
// contract: once a real transition starts a live animation, Update DOES
// schedule exactly one flairTickMsg via tea.Tick, proving the root model
// wiring — not just FlairManager — respects NeedsTick().
func TestUpdateSchedulesTickWhenNeedsTickTrue(t *testing.T) {
	w := newTestWrapper(config.FlairConfig{Enabled: true})

	w.Update(ui.SnapshotMsg{Snapshot: store.Snapshot{
		Items: []store.Item{{ID: "a", Kind: store.KindBead, Title: "Alpha"}},
	}})

	_, cmd := w.Update(ui.SnapshotMsg{Snapshot: store.Snapshot{}}) // item "a" closes -> row flash starts
	if !w.mgr.NeedsTick() {
		t.Fatal("setup invariant broken: closing the item should have started a live animation")
	}
	ticks := collectTickMsgs(cmd)
	if len(ticks) != 1 {
		t.Fatalf("want exactly one flairTickMsg scheduled by Update when NeedsTick()==true, got %d", len(ticks))
	}

	// Drain the animation by repeatedly delivering flairTickMsg (as the
	// real Program would once the scheduled Cmd fires) until it settles,
	// then confirm the tick stops re-scheduling itself — the zero-idle-cost
	// invariant, exercised at the root-model level.
	const maxFrames = 10_000
	settledWithNoTick := false
	for i := 0; i < maxFrames; i++ {
		_, cmd = w.Update(flairTickMsg{})
		if !w.mgr.NeedsTick() {
			if len(collectTickMsgs(cmd)) != 0 {
				t.Fatal("want no flairTickMsg scheduled on the frame the animation settles")
			}
			settledWithNoTick = true
			break
		}
	}
	if !settledWithNoTick {
		t.Fatalf("animation never settled within %d frames", maxFrames)
	}
}

// TestViewCompositesToastOverlayOnlyWhenLive confirms View() returns root's
// output unchanged when there is no live toast (including whenever overlay
// is nil), and changes it when one is active — exercising task [3.2]'s
// "composite the overlay ... over the root View() output" wiring end to
// end through real ToastOverlay.
func TestViewCompositesToastOverlayOnlyWhenLive(t *testing.T) {
	root := ui.NewRoot(ui.NewQueuePane(), ui.NewDetailPane())
	mgr := flair.NewFlairManager(config.FlairConfig{Enabled: true})
	w := newRootWithFlair(root, mgr, flair.NewToastOverlay(discardWriter{}, nil))
	w.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	baseline := w.View().Content

	w.Update(ui.SnapshotMsg{Snapshot: store.Snapshot{}}) // seed
	w.Update(ui.SnapshotMsg{Snapshot: store.Snapshot{
		Items: []store.Item{{ID: "p1", Kind: store.KindProposal, Title: "New Thing"}},
	}}) // proposal appears -> toast queued, spring starts off-screen

	// The toast springs in from off-screen (ToastSpringEffect starts at
	// toastOffscreenY) — drive real ticks, as the Program would once
	// maybeTick's Cmd fires, until it has sprung far enough on-screen for
	// Compose to actually change the output.
	const maxFrames = 10_000
	changed := false
	for i := 0; i < maxFrames; i++ {
		w.Update(flairTickMsg{})
		if w.View().Content != baseline {
			changed = true
			break
		}
	}
	if !changed {
		t.Fatalf("want View() to change once the toast has sprung in, got identical output after %d frames", maxFrames)
	}
}

// TestStepFlairPropagatesSpriteGlyphsToQueuePane confirms the end-to-end
// wiring for design.md § Presence sprites (if-z7pm/if-u7ul.1): an item
// carrying a linked session with Session.Zombie==true renders its
// zombie-state sprite glyph in the real QueuePane row, driven entirely
// through rootWithFlair.Update -> stepFlair -> QueuePane.SetSpriteGlyphs —
// no direct FlairManager/QueuePane wiring bypassing this file.
func TestStepFlairPropagatesSpriteGlyphsToQueuePane(t *testing.T) {
	w := newTestWrapper(config.FlairConfig{Enabled: true})

	zombieSnap := ui.SnapshotMsg{Snapshot: store.Snapshot{
		Items: []store.Item{
			{ID: "a", Kind: store.KindBead, Title: "Stuck", Session: &store.SessionLink{Zombie: true}},
		},
	}}

	// ui.Root coalesces a SnapshotMsg arriving within renderInterval
	// (100ms, internal/ui/root.go) of the last applied one — real,
	// pre-existing behavior unrelated to sprites. Sleeping past it between
	// each call (rather than bypassing Root) keeps this test exercising
	// the actual production Update path end to end.
	//
	// Only ONE zombieSnap delivery is needed, not two: rootWithFlair.Update
	// now computes flair's diff/highlight/sprite state (m.applySnapshot)
	// BEFORE forwarding a SnapshotMsg to root.Update — the if-zts4 fix
	// (row_flash on a closed bead needs its highlight installed before
	// QueuePane.Update rebuilds rows, not after). QueuePane.SetSpriteGlyphs,
	// like SetHighlights, only stores the map; the rebuild that actually
	// consults it is root.Update's own queue.Update(snap) call, which now
	// runs AFTER applySnapshot on the SAME delivery — so the glyph installed
	// for this zombieSnap transition is already reflected in this same
	// delivery's rebuilt rows (previously it took a second delivery to see
	// it, since the rebuild ran a half-step ahead of the glyph it needed).
	// A second delivery here would advance the sprite's 2-frame cycle
	// (sprite.go's spriteFrames) to its OTHER glyph ("⋋" instead of "×"),
	// which is exactly why only one is used.
	w.Update(ui.SnapshotMsg{Snapshot: store.Snapshot{}}) // seed (no real prior state)
	time.Sleep(150 * time.Millisecond)
	w.Update(zombieSnap) // starts sprite state + installs this frame's glyph

	view := w.root.Queue().View()
	// The Item column now carries a kind glyph + MM-dd date prefix ahead of
	// the title (wavetui-table-detail-polish tasks.md [2.1]/[2.2]) — the
	// sprite glyph still prepends ahead of that whole label, not just the
	// title, so the zero-CreatedAt/KindBead item renders "× 🧿 ----- Stuck".
	if !strings.Contains(view, "× 🧿 ----- Stuck") {
		t.Fatalf("want the zombie sprite glyph prepended onto the linked item's row, got:\n%s", view)
	}
}

// TestRowFlashRendersOnClosedBeadRowBeforeDisappearing is if-zts4's
// MANDATORY regression test: it drives the REAL rootWithFlair/QueuePane
// pipeline (real FlairManager, real Diff/OnSnapshot/AdvanceFrame, real
// ui.Root/QueuePane row rebuild — no fakes, no direct FlairManager/QueuePane
// wiring bypassing this file's Update) through a real close-event Snapshot
// transition, and asserts the rendered View() output shows the closed row
// still present WITH its row_flash highlight applied for at least one frame
// before it disappears — not merely that FlairManager's internal animState
// exists (manager_test.go/highlight_test.go already proved that in
// isolation; the bug this closes is specifically that the highlight never
// reached the screen).
func TestRowFlashRendersOnClosedBeadRowBeforeDisappearing(t *testing.T) {
	w := newTestWrapper(config.FlairConfig{Enabled: true})

	openSnap := ui.SnapshotMsg{Snapshot: store.Snapshot{
		Items: []store.Item{{ID: "if-zts4", Kind: store.KindBead, Title: "Closeable Bead"}},
	}}

	// Seed (no real prior state to diff), then let the bead appear — both
	// spaced past ui.Root's 100ms coalescing window (same pattern
	// TestStepFlairPropagatesSpriteGlyphsToQueuePane already establishes)
	// so each delivery is actually applied, not coalesced away.
	w.Update(ui.SnapshotMsg{Snapshot: store.Snapshot{}})
	time.Sleep(150 * time.Millisecond)
	w.Update(openSnap)
	time.Sleep(150 * time.Millisecond)

	view := w.root.Queue().View()
	if !strings.Contains(view, "Closeable Bead") {
		t.Fatalf("want the bead's row present before it closes, got:\n%s", view)
	}

	// Close it — item "if-zts4" is absent from this Snapshot's Items.
	w.Update(ui.SnapshotMsg{Snapshot: store.Snapshot{}})

	// The row_flash effect always composes a ParticleBurstEffect alongside
	// the color spring (highlight.go's rowFlashHighlighter), which renders
	// its "✱" glyph for every frame until the burst's fixed particleTTL
	// (400ms) elapses — one 30fps tick in, nowhere close to done. Asserting
	// on the glyph (not the color) matches this package's own established
	// convention for proving a highlight actually rendered without
	// depending on ANSI color codes surviving a non-TTY test environment
	// (see queuepane_test.go's TestSetHighlightsAppliesColorAndGlyphToMatchingRow).
	view = w.root.Queue().View()
	if !strings.Contains(view, "✱") || !strings.Contains(view, "Closeable Bead") {
		t.Fatalf("want the closed bead's row still rendered with its row_flash glyph on the transition frame (if-zts4), got:\n%s", view)
	}

	// Drive the animation to completion via real flairTickMsg deliveries —
	// once the flash settles, FlairManager stops reporting a highlight for
	// this item at all.
	const maxFrames = 10_000
	settled := false
	for i := 0; i < maxFrames; i++ {
		_, cmd := w.Update(flairTickMsg{})
		if !w.mgr.NeedsTick() {
			settled = true
			_ = cmd
			break
		}
	}
	if !settled {
		t.Fatalf("row_flash animation never settled within %d frames", maxFrames)
	}

	// One more real Snapshot (same empty state) is what actually triggers
	// QueuePane's next row rebuild — ticks alone never rebuild rows (see
	// the comment above on TestStepFlairPropagatesSpriteGlyphsToQueuePane).
	// With the highlight now gone, withGhostRows must no longer resurrect
	// the closed row: it has to actually disappear once its flash is done.
	time.Sleep(150 * time.Millisecond)
	w.Update(ui.SnapshotMsg{Snapshot: store.Snapshot{}})
	view = w.root.Queue().View()
	if strings.Contains(view, "Closeable Bead") {
		t.Fatalf("want the closed bead's row gone once its row_flash has settled, got:\n%s", view)
	}
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }
