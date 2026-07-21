// See lanes.go for the LaneState/DetectLane/IsStale contract under test
// here — tasks.md [1.2]/[2.3]: DetectLane's nil/preserved-across-snapshots
// cases and IsStale's idle-window boundary cases.
package lanes

import (
	"testing"
	"time"
)

func TestDetectLaneNilWhenNoBlocker(t *testing.T) {
	if ls := DetectLane("", nil); ls != nil {
		t.Fatalf("want nil for an empty blockerType, got %+v", ls)
	}
}

func TestDetectLanePreservesPriorAcrossSnapshotsWhenTypeUnchanged(t *testing.T) {
	prior := &LaneState{Type: "decision", Since: time.Now().Add(-time.Hour), PaneID: "%3", SpawnedAt: time.Now().Add(-time.Minute)}

	got := DetectLane("decision", prior)
	if got != prior {
		t.Fatalf("want the exact prior pointer preserved (same Type), got a different value: %+v", got)
	}
}

func TestDetectLaneFreshWhenTypeChanges(t *testing.T) {
	prior := &LaneState{Type: "decision", PaneID: "%3", SpawnedAt: time.Now()}

	got := DetectLane("dependency", prior)
	if got == prior {
		t.Fatal("want a fresh LaneState when the type changes, got the same pointer")
	}
	if got.Type != "dependency" {
		t.Fatalf("want Type %q, got %q", "dependency", got.Type)
	}
	if got.PaneID != "" || !got.SpawnedAt.IsZero() {
		t.Fatalf("want PaneID/SpawnedAt reset (not carried forward) on a type change, got %+v", got)
	}
}

// --- IsStale -----------------------------------------------------------

func TestIsStaleNeverSpawnedIsNeverStale(t *testing.T) {
	ls := &LaneState{Type: "decision"} // SpawnedAt zero value
	if ls.IsStale(false, time.Minute) {
		t.Fatal("want a never-spawned lane to never be stale, regardless of hasLiveSession")
	}
}

func TestIsStaleLiveSessionShortCircuitsEvenPastIdleWindow(t *testing.T) {
	ls := &LaneState{Type: "decision", SpawnedAt: time.Now().Add(-time.Hour)}
	if ls.IsStale(true, time.Minute) {
		t.Fatal("want a live session to never be reported stale, regardless of elapsed time")
	}
}

func TestIsStaleJustUnderIdleWindowIsNotStale(t *testing.T) {
	ls := &LaneState{Type: "decision", SpawnedAt: time.Now().Add(-30 * time.Second)}
	if ls.IsStale(false, time.Minute) {
		t.Fatal("want not-stale when elapsed time is under the idle window")
	}
}

func TestIsStaleJustOverIdleWindowIsStale(t *testing.T) {
	ls := &LaneState{Type: "decision", SpawnedAt: time.Now().Add(-2 * time.Minute)}
	if !ls.IsStale(false, time.Minute) {
		t.Fatal("want stale when elapsed time exceeds the idle window with no live session")
	}
}

func TestIsStaleNoLiveSessionTreatedSameAsZombie(t *testing.T) {
	// hasLiveSession == false covers both "item.Session == nil" and
	// "item.Session.Zombie == true" at the call site -- IsStale itself only
	// ever sees the collapsed bool, so this asserts the collapsed case
	// crosses the idle window correctly (the two-source collapse itself is
	// the caller's responsibility, documented in IsStale's doc comment).
	ls := &LaneState{Type: "decision", SpawnedAt: time.Now().Add(-2 * time.Minute)}
	if !ls.IsStale(false, time.Minute) {
		t.Fatal("want stale when hasLiveSession is false past the idle window")
	}
}
