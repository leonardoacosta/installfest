// See doc.go for the package-level contract. This file implements the
// session-linkage algorithm — see openspec/changes/wavetui-sessions/
// tasks.md [1.2] and design.md § Session linkage algorithm.
//
// SessionLinker is deliberately a pure matching helper: it never tails a
// file, never fsnotify-watches, and never touches the Store or bus
// directly (the "sources never touch Store internals" invariant extends
// here too, even though this file has no Run method of its own). The
// actual transcript tailing + JSONL decoding that produces its inputs is
// wavetui-sessions' TranscriptSource (tasks.md [2.1], a later batch); this
// file only implements the three-step priority algorithm design.md
// specifies, against already-decoded fields, so that algorithm has its own
// independently testable unit (tasks.md [4.3]) and TranscriptSource is not
// forced to re-derive it.
package sources

import (
	"regexp"
	"time"
)

// DefaultLinkWindow is the cwd+claim-timestamp-proximity fallback window
// used when a SessionLinker is constructed with window <= 0 — see
// design.md § Session linkage algorithm point 2 ("default 10 minutes").
//
// This is a struct-field default (mirroring BeadsSource's
// defaultBeadsDebounce / defaultBeadsPoll pattern), not a wired-up
// `internal/config` key: wavetui-sessions' proposal.md `- touches:` list
// does not name `internal/config/config.go`, and that file's config
// surface today is booleans only (ShowPlans/ShowAdvisorPlans/FlairConfig).
// If a future proposal wants this window operator-configurable via
// `.wavetui.toml`, NewSessionLinker's window parameter is the seam to wire
// a config value into — no change to the matching algorithm itself would
// be needed.
const DefaultLinkWindow = 10 * time.Minute

// applyDispatchRe matches the structural marker Claude Code's own CLI
// wraps around a genuinely interactively-dispatched `/apply <id>` slash
// command — `<command-name>/apply</command-name>` immediately followed by
// a `<command-args>` block whose leading token is the invoked id — rather
// than a bare literal-substring search for "/apply <id>" anywhere in a
// message's text.
//
// This replaces an earlier version of this match (a plain
// `(?:^|\s)/apply\s+(id)` substring regex) that produced a confirmed live
// false positive: it resolved to the FIRST occurrence of `/apply <word>`
// anywhere in a transcript, including incidental prose that merely
// mentions the string rather than a real dispatch — e.g. a CLAUDE.md-style
// context dump (loaded as a `<command-args>` block belonging to some other
// command, such as `/explore`) whose own prose says "... dispatches
// `/apply` waves via tmux ..." matched and won before the transcript's
// later, real, actually-dispatched `/apply <real-id>` command ever got a
// chance. Reproduced directly against this repo's own live session
// transcripts (`~/.claude/projects/<flattened-path>/*.jsonl`) while fixing
// this: a genuine interactive dispatch always looks structurally like
//
//	<command-message>apply</command-message>
//	<command-name>/apply</command-name>
//	<command-args>some-id</command-args>
//
// verbatim as the WHOLE message.content string (no other command's own
// `<command-args>` payload ever contains a nested `<command-name>/apply
// </command-name>` marker in real data) — this is the harness's own
// unambiguous "a real slash command was dispatched" signal, never
// something a passing prose mention can accidentally produce. A raw
// substring search cannot distinguish those two cases; the structural
// marker can. design.md's own edge-case philosophy ("ambiguous linkage
// gets a '?' variant, never a confident match") argues for exactly this
// conservative direction — under-matching a hypothetical raw-text mention
// of `/apply <id>` typed outside the slash-command mechanism (which would
// never earn this wrapper) is preferable to over-matching incidental
// prose, since design.md's fallback step (cwd+timestamp) still has a
// chance to resolve that session correctly.
//
// The id-shape charclass (`[A-Za-z0-9][A-Za-z0-9._-]*`) mirrors the
// canonical bead-ID grammar already used elsewhere in this codebase (see
// internal/timeline/memory_history.go's inlineBeadRefRe, and
// rules/TOOLING.md's BEADS_ID_RE in the wider cc fleet) and doubles as a
// valid openspec change-slug shape (e.g. "wavetui-sessions") — both are
// legal targets of `/apply <id>`. `\s*` (not `\s+`) between the closing
// `</command-name>` tag and `<command-args>` tolerates the harness never
// emitting anything but a single `\n` there in observed data, without
// hard-coding that exact byte.
var applyDispatchRe = regexp.MustCompile(`<command-name>/apply</command-name>\s*<command-args>\s*([A-Za-z0-9][A-Za-z0-9._-]*)`)

// SessionTranscript is the subset of one Claude Code session's transcript
// data a caller (TranscriptSource) must supply for linkage resolution —
// aggregated across that session's own lines, not a single decoded line.
// Field names/shapes mirror the real transcript fields confirmed during
// this proposal's authoring (design.md § Verified transcript fields) and
// re-confirmed against this session's own live transcript during this
// batch's implementation: `cwd`, `sessionId`, `isSidechain`, `parentUuid`,
// `timestamp` all exist under exactly these names on `user`-type lines.
type SessionTranscript struct {
	// SessionID is the transcript's own `sessionId` field.
	SessionID string
	// CWD is the transcript's own `cwd` field, trusted verbatim. This type
	// intentionally has no field for a flattened-directory-name path —
	// design.md's "cwd trusted over directory-name flattening" Requirement
	// is satisfied structurally here: there is nothing to derive a path
	// from except this field, so a caller cannot accidentally feed a
	// flattened-name guess into the fallback match by construction.
	CWD string
	// EarliestTimestamp is the earliest `timestamp` seen across this
	// session's lines — the value design.md's fallback step compares
	// against a claimed item's claim timestamp.
	EarliestTimestamp time.Time
	// IsSidechain and ParentUUID mirror a transcript line's own
	// `isSidechain`/`parentUuid` fields. A sidechain transcript's
	// SessionID is still its own real sessionId — ParentUUID is what
	// names the session it inherits linkage from.
	IsSidechain bool
	ParentUUID  string
	// UserMessages is the raw text content of every `user`-type line's
	// message in this session, in chronological order — scanned in that
	// order for an exact `/apply <id>` reference so that "first match
	// wins" (design.md point 1) resolves to the earliest occurrence.
	// Extracting text out of a transcript line's `message.content` (which
	// may be a plain string or a content-block array — both observed
	// shapes exist in real transcripts) is TranscriptSource's job
	// (tasks.md [2.1]), not this file's.
	UserMessages []string
}

// ClaimedItem is the subset of a claimed queue item's data the
// cwd+timestamp fallback step needs. RepoPath is the item's known repo
// path (matched against SessionTranscript.CWD) and ClaimedAt is the claim
// timestamp (e.g. from `bd show <id> --json` claim metadata, already
// available via BeadsSource per design.md's fallback-step citation).
type ClaimedItem struct {
	ItemID    string
	RepoPath  string
	ClaimedAt time.Time
}

// SessionLinker resolves which claimed item (if any) a Claude Code
// session's transcript belongs to, per design.md § Session linkage
// algorithm's three-step priority order: (1) exact `/apply <id>`
// reference, (2) cwd+claim-timestamp-proximity fallback, (3) subagent
// sidechain inheritance from a resolved parent session. It holds no
// bus/Store reference — TranscriptSource owns publishing whatever it does
// with a resolved link.
//
// Link is not safe to call concurrently with itself: it reads and writes
// the resolved cache without a lock, matching every other source's
// single-writer-goroutine convention (see BeadsSource.last's doc comment).
type SessionLinker struct {
	// Window is the cwd+claim-timestamp-proximity fallback window. Zero
	// (the zero value of a freshly-declared SessionLinker{}) is treated as
	// "use DefaultLinkWindow" by window(), so a caller that only needs the
	// default never has to think about this field; NewSessionLinker exists
	// for callers that want a different value made explicit.
	Window time.Duration

	// resolved caches SessionID -> linked ItemID for every session Link
	// has successfully resolved, so a later sidechain whose ParentUUID
	// names an already-resolved session can inherit it (step 3) even
	// though sidechain lines can arrive in the same or a later fsnotify
	// batch than their parent's own resolution.
	resolved map[string]string
}

// NewSessionLinker constructs a SessionLinker. window <= 0 selects
// DefaultLinkWindow.
func NewSessionLinker(window time.Duration) *SessionLinker {
	return &SessionLinker{
		Window:   window,
		resolved: make(map[string]string),
	}
}

func (l *SessionLinker) window() time.Duration {
	if l.Window <= 0 {
		return DefaultLinkWindow
	}
	return l.Window
}

// Link resolves sess to a claimed item's ID, or reports ok=false if none
// of the three steps produced a match. On a successful resolution, the
// result is cached under sess.SessionID (lazily initializing resolved if
// this SessionLinker was constructed as a bare SessionLinker{} rather than
// via NewSessionLinker) so a later sidechain can inherit it via step 3.
func (l *SessionLinker) Link(sess SessionTranscript, claims []ClaimedItem) (itemID string, ok bool) {
	if l.resolved == nil {
		l.resolved = make(map[string]string)
	}

	defer func() {
		if ok {
			l.resolved[sess.SessionID] = itemID
		}
	}()

	if sess.IsSidechain {
		// design.md point 3: "a sidechain's own session ID is never
		// matched independently; it always inherits its parent's item
		// linkage." Steps 1/2 are skipped entirely for a sidechain, even
		// if its own UserMessages/CWD would otherwise match something —
		// this is a priority order, not a set of independent signals to
		// OR together.
		parentItem, found := l.resolved[sess.ParentUUID]
		return parentItem, found
	}

	if id, found := matchExactApplyRef(sess.UserMessages); found {
		return id, true
	}

	return l.matchCWDTimestamp(sess.CWD, sess.EarliestTimestamp, claims)
}

// Resolved reports the item ID sessionID was last linked to via Link, if
// any. Exposed so a caller (or a test) can inspect sidechain-inheritance
// state without re-deriving it.
func (l *SessionLinker) Resolved(sessionID string) (itemID string, ok bool) {
	itemID, ok = l.resolved[sessionID]
	return itemID, ok
}

// matchExactApplyRef scans messages in order and returns the id captured
// by the first genuine `/apply <id>` slash-command dispatch found — "first
// match wins" (design.md point 1), which for messages supplied in
// chronological order means the earliest real dispatch in the session
// wins. See applyDispatchRe's doc comment for why this matches the
// harness's structural command-dispatch wrapper rather than a bare
// literal-substring search — the latter is what produced the false
// positive this function now fixes.
func matchExactApplyRef(messages []string) (string, bool) {
	for _, msg := range messages {
		if m := applyDispatchRe.FindStringSubmatch(msg); m != nil {
			return m[1], true
		}
	}
	return "", false
}

// matchCWDTimestamp implements design.md point 2: a match requires BOTH
// cwd equality AND claim-timestamp proximity within window — neither
// condition alone is sufficient (see spec.md's "cwd match alone... does
// not link" scenario). Among multiple claimed items sharing the same
// RepoPath and each within window, the closest-in-time match wins, since
// design.md's stated rationale for requiring both conditions ("cwd alone is
// too coarse... timestamp alone is too coarse") implies timestamp proximity
// is the tie-breaking signal, not an arbitrary one.
func (l *SessionLinker) matchCWDTimestamp(cwd string, earliest time.Time, claims []ClaimedItem) (string, bool) {
	if cwd == "" || earliest.IsZero() {
		return "", false
	}

	window := l.window()
	bestID := ""
	bestDiff := time.Duration(-1)

	for _, c := range claims {
		if c.RepoPath == "" || c.RepoPath != cwd || c.ClaimedAt.IsZero() {
			continue
		}
		diff := earliest.Sub(c.ClaimedAt)
		if diff < 0 {
			diff = -diff
		}
		if diff > window {
			continue
		}
		if bestDiff == -1 || diff < bestDiff {
			bestDiff = diff
			bestID = c.ItemID
		}
	}

	if bestDiff == -1 {
		return "", false
	}
	return bestID, true
}
