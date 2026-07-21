package main

import (
	"testing"

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

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }
