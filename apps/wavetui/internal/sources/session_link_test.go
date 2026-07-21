package sources

import (
	"testing"
	"time"
)

// --- exact match: genuine dispatch wrapper --------------------------------

func TestMatchExactApplyRefMatchesGenuineDispatch(t *testing.T) {
	msg := "<command-message>apply</command-message>\n<command-name>/apply</command-name>\n<command-args>if-abc12</command-args>"
	id, ok := matchExactApplyRef([]string{msg})
	if !ok || id != "if-abc12" {
		t.Fatalf("matchExactApplyRef() = (%q, %v), want (if-abc12, true)", id, ok)
	}
}

// TestMatchExactApplyRefIgnoresIncidentalProseMention is the MANDATORY
// regression test for the false positive the UI-phase agent found live in
// this session's own transcript: applyRefRe (the prior version of this
// match, a bare `(?:^|\s)/apply\s+(id)` substring regex) resolved to the
// FIRST occurrence of "/apply <word>" anywhere in ANY user-type message —
// including a CLAUDE.md-style context dump's own prose ("... dispatches
// `/apply` waves via tmux ...") injected as a `<command-args>` payload
// belonging to an unrelated command (`/explore`), which won before the
// transcript's later, real, actually-dispatched `/apply <real-id>` command
// ever got a chance.
//
// This fixture reproduces that exact shape: an EARLIER message is a real
// `/explore` dispatch whose own `<command-args>` block is a large prose
// blob that incidentally contains the substring "/apply waves" (mirroring
// the real CLAUDE.md prose that triggered the live bug), followed by a
// LATER message that is a genuine `/apply <id>` dispatch. The fixed
// algorithm must resolve to the real, later id — never the incidental
// earlier prose mention.
func TestMatchExactApplyRefIgnoresIncidentalProseMention(t *testing.T) {
	incidentalProse := "<command-message>explore</command-message>\n" +
		"<command-name>/explore</command-name>\n" +
		"<command-args># Explore: Wave-Orchestration TUI\n\n" +
		"This TUI links items to live Claude Code sessions, and dispatches " +
		"`/apply` waves via tmux, clipboard, or headless sessions — with all " +
		"features working end to end across every source.</command-args>"
	genuineDispatch := "<command-message>apply</command-message>\n" +
		"<command-name>/apply</command-name>\n" +
		"<command-args>if-real99</command-args>"

	messages := []string{incidentalProse, genuineDispatch}

	id, ok := matchExactApplyRef(messages)
	if !ok {
		t.Fatal("expected the genuine later dispatch to resolve, got no match at all")
	}
	if id != "if-real99" {
		t.Fatalf("matchExactApplyRef() = %q, want %q (the real dispatch, not the incidental %q prose mention)",
			id, "if-real99", "waves")
	}
}

// TestMatchExactApplyRefIgnoresBareTextMention covers the simpler case of
// the same failure class: a plain user-typed chat message that merely
// mentions "/apply <word>" as prose (never went through the slash-command
// mechanism, so it never earns the <command-name>/<command-args> wrapper)
// must not be mistaken for a real dispatch either.
func TestMatchExactApplyRefIgnoresBareTextMention(t *testing.T) {
	messages := []string{
		"can you explain what /apply waves does before I run it for real?",
	}
	if id, ok := matchExactApplyRef(messages); ok {
		t.Fatalf("expected no match for a bare prose mention with no dispatch wrapper, got (%q, %v)", id, ok)
	}
}

// TestMatchExactApplyRefFirstGenuineDispatchWins covers "first match wins"
// (design.md point 1) restated for the fixed algorithm: among two genuine
// dispatches, the earliest (first in chronological order) wins.
func TestMatchExactApplyRefFirstGenuineDispatchWins(t *testing.T) {
	first := "<command-message>apply</command-message>\n<command-name>/apply</command-name>\n<command-args>if-first</command-args>"
	second := "<command-message>apply</command-message>\n<command-name>/apply</command-name>\n<command-args>if-second</command-args>"

	id, ok := matchExactApplyRef([]string{first, second})
	if !ok || id != "if-first" {
		t.Fatalf("matchExactApplyRef() = (%q, %v), want (if-first, true)", id, ok)
	}
}

// TestMatchExactApplyRefIgnoresUnrelatedCommandDispatch covers a genuine
// dispatch of a DIFFERENT command (e.g. /apply:all, which is a distinct
// command name, not "/apply" with args) — it must never be mistaken for a
// literal `/apply <id>` dispatch just because it shares a prefix.
func TestMatchExactApplyRefIgnoresUnrelatedCommandDispatch(t *testing.T) {
	msg := "<command-message>apply:all</command-message>\n<command-name>/apply:all</command-name>"
	if id, ok := matchExactApplyRef([]string{msg}); ok {
		t.Fatalf("expected /apply:all (no args, different command name) to not match, got (%q, %v)", id, ok)
	}
}

func TestMatchExactApplyRefNoMatchReturnsFalse(t *testing.T) {
	if id, ok := matchExactApplyRef([]string{"just chatting about the project"}); ok {
		t.Fatalf("expected no match, got (%q, %v)", id, ok)
	}
	if id, ok := matchExactApplyRef(nil); ok {
		t.Fatalf("expected no match for nil messages, got (%q, %v)", id, ok)
	}
}

// --- cwd + timestamp fallback ------------------------------------------

func TestLinkCWDTimestampFallbackRequiresBothConditions(t *testing.T) {
	base := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	linker := NewSessionLinker(10 * time.Minute)

	claims := []ClaimedItem{
		{ItemID: "if-a", RepoPath: "/repo/a", ClaimedAt: base},
	}

	// Both cwd and timestamp match, within window: links.
	sess := SessionTranscript{SessionID: "s1", CWD: "/repo/a", EarliestTimestamp: base.Add(2 * time.Minute)}
	id, ok := linker.Link(sess, claims)
	if !ok || id != "if-a" {
		t.Fatalf("expected fallback match (%q, %v), want (if-a, true)", id, ok)
	}
}

func TestLinkCWDAloneWithoutTimestampProximityDoesNotLink(t *testing.T) {
	base := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	linker := NewSessionLinker(10 * time.Minute)

	claims := []ClaimedItem{
		{ItemID: "if-a", RepoPath: "/repo/a", ClaimedAt: base},
	}

	// cwd matches, but timestamp is far outside the window.
	sess := SessionTranscript{SessionID: "s2", CWD: "/repo/a", EarliestTimestamp: base.Add(2 * time.Hour)}
	if id, ok := linker.Link(sess, claims); ok {
		t.Fatalf("expected cwd-alone (timestamp out of window) to NOT link, got (%q, %v)", id, ok)
	}
}

func TestLinkTimestampAloneWithoutCWDMatchDoesNotLink(t *testing.T) {
	base := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	linker := NewSessionLinker(10 * time.Minute)

	claims := []ClaimedItem{
		{ItemID: "if-a", RepoPath: "/repo/a", ClaimedAt: base},
	}

	// timestamp is within window, but cwd does not match.
	sess := SessionTranscript{SessionID: "s3", CWD: "/repo/other", EarliestTimestamp: base.Add(1 * time.Minute)}
	if id, ok := linker.Link(sess, claims); ok {
		t.Fatalf("expected timestamp-alone (cwd mismatch) to NOT link, got (%q, %v)", id, ok)
	}
}

func TestLinkCWDTimestampFallbackClosestMatchWins(t *testing.T) {
	base := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	linker := NewSessionLinker(30 * time.Minute)

	claims := []ClaimedItem{
		{ItemID: "if-far", RepoPath: "/repo/a", ClaimedAt: base},
		{ItemID: "if-near", RepoPath: "/repo/a", ClaimedAt: base.Add(20 * time.Minute)},
	}

	sess := SessionTranscript{SessionID: "s4", CWD: "/repo/a", EarliestTimestamp: base.Add(22 * time.Minute)}
	id, ok := linker.Link(sess, claims)
	if !ok || id != "if-near" {
		t.Fatalf("expected the closest-in-time claim to win, got (%q, %v), want (if-near, true)", id, ok)
	}
}

func TestLinkDefaultWindowIsTenMinutes(t *testing.T) {
	if DefaultLinkWindow != 10*time.Minute {
		t.Fatalf("DefaultLinkWindow = %v, want 10m", DefaultLinkWindow)
	}

	base := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	linker := NewSessionLinker(0) // window <= 0 selects DefaultLinkWindow

	claims := []ClaimedItem{{ItemID: "if-a", RepoPath: "/repo/a", ClaimedAt: base}}

	// 11 minutes: just outside the 10-minute default window.
	sess := SessionTranscript{SessionID: "s5", CWD: "/repo/a", EarliestTimestamp: base.Add(11 * time.Minute)}
	if _, ok := linker.Link(sess, claims); ok {
		t.Fatal("expected 11m to fall outside the default 10m window")
	}

	// 9 minutes: within it.
	sess2 := SessionTranscript{SessionID: "s6", CWD: "/repo/a", EarliestTimestamp: base.Add(9 * time.Minute)}
	if _, ok := linker.Link(sess2, claims); !ok {
		t.Fatal("expected 9m to fall within the default 10m window")
	}
}

// --- sidechain inheritance ------------------------------------------------

func TestLinkSidechainInheritsResolvedParent(t *testing.T) {
	linker := NewSessionLinker(0)

	parent := SessionTranscript{
		SessionID:    "parent-1",
		UserMessages: []string{"<command-message>apply</command-message>\n<command-name>/apply</command-name>\n<command-args>if-parent</command-args>"},
	}
	parentID, ok := linker.Link(parent, nil)
	if !ok || parentID != "if-parent" {
		t.Fatalf("setup: expected parent to link to if-parent, got (%q, %v)", parentID, ok)
	}

	sidechain := SessionTranscript{
		SessionID:   "side-1",
		IsSidechain: true,
		ParentUUID:  "parent-1",
		// Deliberately populated with fields that WOULD match if the
		// sidechain were evaluated independently — proves step 3 is a
		// priority order, not an OR of independent signals.
		CWD:          "/some/other/repo",
		UserMessages: []string{"<command-message>apply</command-message>\n<command-name>/apply</command-name>\n<command-args>if-decoy</command-args>"},
	}
	id, ok := linker.Link(sidechain, nil)
	if !ok || id != "if-parent" {
		t.Fatalf("expected sidechain to inherit parent's linkage (if-parent), got (%q, %v)", id, ok)
	}
}

func TestLinkSidechainWithUnresolvedParentDoesNotLink(t *testing.T) {
	linker := NewSessionLinker(0)

	sidechain := SessionTranscript{
		SessionID:   "side-2",
		IsSidechain: true,
		ParentUUID:  "never-resolved",
	}
	if id, ok := linker.Link(sidechain, nil); ok {
		t.Fatalf("expected no link when the named parent was never resolved, got (%q, %v)", id, ok)
	}
}

func TestResolvedReportsCachedLinkage(t *testing.T) {
	linker := NewSessionLinker(0)
	sess := SessionTranscript{
		SessionID:    "s7",
		UserMessages: []string{"<command-message>apply</command-message>\n<command-name>/apply</command-name>\n<command-args>if-x</command-args>"},
	}
	linker.Link(sess, nil)

	id, ok := linker.Resolved("s7")
	if !ok || id != "if-x" {
		t.Fatalf("Resolved(s7) = (%q, %v), want (if-x, true)", id, ok)
	}
	if _, ok := linker.Resolved("never-linked"); ok {
		t.Fatal("expected Resolved to report false for a session never linked")
	}
}

// --- cwd trusted over directory-name flattening ---------------------------
//
// SessionTranscript intentionally has no field for a flattened-directory-
// name path (see its own doc comment) — this test proves the fallback
// match is driven entirely by the verbatim CWD field, so a caller cannot
// accidentally feed a flattening-derived guess into it. Two repo paths
// that would collide if flattened (a real hyphen vs a flattened slash)
// must resolve to their own distinct, correct claim.
func TestLinkFallbackUsesVerbatimCWDNeverFlattening(t *testing.T) {
	base := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	linker := NewSessionLinker(10 * time.Minute)

	claims := []ClaimedItem{
		{ItemID: "if-real-path", RepoPath: "/home/user/dev/my-project", ClaimedAt: base},
		{ItemID: "if-other-path", RepoPath: "/home/user/dev/my/project", ClaimedAt: base},
	}

	// Both repo paths flatten to the identical string
	// "-home-user-dev-my-project" — only the verbatim CWD field
	// distinguishes them.
	sess := SessionTranscript{SessionID: "s8", CWD: "/home/user/dev/my-project", EarliestTimestamp: base.Add(time.Minute)}
	id, ok := linker.Link(sess, claims)
	if !ok || id != "if-real-path" {
		t.Fatalf("expected the verbatim-cwd match if-real-path, got (%q, %v)", id, ok)
	}
}
