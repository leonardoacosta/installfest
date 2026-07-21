package flair

import (
	"testing"

	"github.com/leonardoacosta/installfest/apps/wavetui/internal/config"
	"github.com/leonardoacosta/installfest/apps/wavetui/internal/store"
)

// TestDeriveSpriteStateNilSessionMeansNoSprite confirms design.md's
// "conditional, additive" contract: an item with no linked session gets no
// sprite state at all, not a fallback state.
func TestDeriveSpriteStateNilSessionMeansNoSprite(t *testing.T) {
	if _, ok := DeriveSpriteState(nil); ok {
		t.Fatal("want ok==false for a nil Session — no sprite at all")
	}
}

// TestDeriveSpriteStatePriority exercises every genuinely-derivable state
// (see DeriveSpriteState's doc comment for why only these three of
// design.md's six named states are reachable from store.SessionLink), and
// confirms Zombie wins over ExecutorLaneFlag when a session somehow carries
// both signals (a zombie session is still worth surfacing as zombie, not
// swarm, since "no longer active" is the more actionable fact).
func TestDeriveSpriteStatePriority(t *testing.T) {
	cases := []struct {
		name    string
		session *store.SessionLink
		want    SpriteState
	}{
		{"zombie", &store.SessionLink{Zombie: true}, SpriteStateZombie},
		{"swarm", &store.SessionLink{ExecutorLaneFlag: true}, SpriteStateSwarm},
		{"active", &store.SessionLink{}, SpriteStateActive},
		{"zombie wins over swarm", &store.SessionLink{Zombie: true, ExecutorLaneFlag: true}, SpriteStateZombie},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := DeriveSpriteState(tc.session)
			if !ok {
				t.Fatalf("want ok==true for a non-nil session")
			}
			if got != tc.want {
				t.Fatalf("want state %q, got %q", tc.want, got)
			}
		})
	}
}

// TestUpdateSpriteStatesSkipsSessionlessItems confirms updateSpriteStates
// (driven by OnSnapshot) only records an entry for items that actually have
// a derivable sprite state — a claimed-but-sessionless item contributes
// nothing to SpriteGlyphs.
func TestUpdateSpriteStatesSkipsSessionlessItems(t *testing.T) {
	m := NewFlairManager(config.FlairConfig{Enabled: true})

	next := mkSnapshot(
		store.Item{ID: "no-session", Kind: store.KindBead, Title: "Plain"},
		store.Item{ID: "zombie-session", Kind: store.KindBead, Title: "Stuck", Session: &store.SessionLink{Zombie: true}},
	)
	m.OnSnapshot(store.Snapshot{}, next)

	glyphs := m.SpriteGlyphs()
	if _, ok := glyphs["no-session"]; ok {
		t.Fatal("want no sprite glyph for an item with a nil Session")
	}
	if g, ok := glyphs["zombie-session"]; !ok || g == "" {
		t.Fatalf("want a non-empty sprite glyph for the zombie-session item, got %q (ok=%v)", g, ok)
	}
}

// TestOnSnapshotClearsSpriteStatesWhenDisabled confirms the
// disabled-equals-identical invariant applies to sprite state exactly like
// every other flair mechanism: a disabled manager never derives or holds
// sprite state, even when the snapshot has sessions that would otherwise
// qualify.
func TestOnSnapshotClearsSpriteStatesWhenDisabled(t *testing.T) {
	m := NewFlairManager(config.FlairConfig{Enabled: false})

	next := mkSnapshot(store.Item{ID: "a", Kind: store.KindBead, Session: &store.SessionLink{Zombie: true}})
	m.OnSnapshot(store.Snapshot{}, next)

	if glyphs := m.SpriteGlyphs(); glyphs != nil {
		t.Fatalf("want nil sprite glyphs when flair is disabled, got %+v", glyphs)
	}
	if m.NeedsTick() {
		t.Fatal("want NeedsTick()==false when flair is disabled, regardless of session data")
	}
}

// TestSpriteFrameCyclesOverTicks confirms the 2-4 frame cycle actually
// advances across AdvanceFrame calls (design.md: "a small 2-4 frame cycle
// sprite"), and that it eventually repeats (proving it wraps rather than
// growing unbounded or freezing on one frame).
func TestSpriteFrameCyclesOverTicks(t *testing.T) {
	m := NewFlairManager(config.FlairConfig{Enabled: true})
	next := mkSnapshot(store.Item{ID: "a", Kind: store.KindBead, Session: &store.SessionLink{}})
	m.OnSnapshot(store.Snapshot{}, next)

	seen := map[string]bool{}
	first := ""
	repeated := false
	for i := 0; i < 10; i++ {
		m.AdvanceFrame()
		g := m.SpriteGlyphs()["a"]
		if g == "" {
			t.Fatalf("want a non-empty glyph on frame %d, got empty", i)
		}
		if i == 0 {
			first = g
		}
		if seen[g] && g == first {
			repeated = true
		}
		seen[g] = true
	}
	if len(seen) < 2 {
		t.Fatalf("want at least 2 distinct frames across 10 ticks for an active sprite, got %d: %v", len(seen), seen)
	}
	if !repeated {
		t.Fatal("want the frame cycle to repeat (wrap) within 10 ticks")
	}
}

// TestSpriteCalmModeIsStaticSingleGlyph confirms gating point 2 from
// design.md § Config + calm-mode + truecolor gating: in calm mode, the
// presence sprite renders its current state's single static glyph with no
// frame cycling, and NeedsTick must not report true purely because of a
// live sprite (nothing would change frame-to-frame, so no tick is needed).
func TestSpriteCalmModeIsStaticSingleGlyph(t *testing.T) {
	m := NewFlairManager(config.FlairConfig{Enabled: true, CalmMode: true})
	// Item "a" is present in BOTH prev and next (only its Session field
	// differs) so Diff produces no FlairEvent — this isolates the sprite
	// mechanism from the unrelated row-appear/close animations OnSnapshot
	// also drives, which would otherwise keep NeedsTick()==true for a
	// reason that has nothing to do with sprites.
	prev := mkSnapshot(store.Item{ID: "a", Kind: store.KindBead})
	next := mkSnapshot(store.Item{ID: "a", Kind: store.KindBead, Session: &store.SessionLink{}})
	m.OnSnapshot(prev, next)

	if m.NeedsTick() {
		t.Fatal("want NeedsTick()==false in calm mode when only a sprite is live (static glyph never needs another tick)")
	}

	first := m.SpriteGlyphs()["a"]
	for i := 0; i < 5; i++ {
		m.AdvanceFrame()
		if g := m.SpriteGlyphs()["a"]; g != first {
			t.Fatalf("want the calm-mode glyph to stay fixed at %q, got %q on tick %d", first, g, i)
		}
	}
}

// TestSpriteGlyphsNilWhenNoLiveSessions confirms SpriteGlyphs returns nil
// (not an empty-but-non-nil map) once no item has a derivable sprite state
// — the same "nil means render unchanged" contract SetHighlights already
// established for QueuePane.
func TestSpriteGlyphsNilWhenNoLiveSessions(t *testing.T) {
	m := NewFlairManager(config.FlairConfig{Enabled: true})
	m.OnSnapshot(store.Snapshot{}, mkSnapshot(store.Item{ID: "a", Kind: store.KindBead, Title: "Plain"}))

	if glyphs := m.SpriteGlyphs(); glyphs != nil {
		t.Fatalf("want nil sprite glyphs when no item has a linked session, got %+v", glyphs)
	}
}
