// See doc.go for the package-level contract. This file implements
// TmuxSource — see openspec/changes/wavetui-sessions/tasks.md [2.5] and
// design.md § Alternatives / Related Work.
//
// TmuxSource reads cc-tmux's existing `@cc-state` pane option (the same
// primitive `cc_tmux.tmux.get_pane_option()` wraps, confirmed by inspecting
// apps/cc-tmux/src/cc_tmux/tmux.py and cli.py during this batch's
// authoring — no dedicated per-pane JSON query subcommand exists in cc_tmux's
// CLI) as its PRIMARY and preferred data path for every pane cc-tmux has
// tagged. A process-tree walk is used ONLY as a fallback for panes cc-tmux
// has not tagged (not installed, or outside its hook coverage) — never to
// re-derive state for an already-tagged pane, and never by assuming any
// positional relationship between panes.
//
// cc-tmux also writes `@cc-session-id` (apps/cc-tmux/src/cc_tmux/tmux.py's
// OPT_SESSION_ID, captured from the SessionStart hook payload) alongside
// `@cc-state` — this is the real mechanism that lets TmuxSource answer "what
// is THIS Claude Code session's pane state", which is what
// TranscriptSource's zombie cross-check (design.md § Zombie detection)
// needs. Verified live against this environment's own tmux server during
// authoring: real panes carried real `@cc-state`/`@cc-session-id` values
// (e.g. `active` / a live session UUID), and an untagged pane's
// `show-options` call exits 1 with stderr "invalid option: @cc-state" and
// empty stdout — treated as "no data for this pane", never a parse target.
package sources

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/leonardoacosta/installfest/apps/wavetui/internal/bus"
	"github.com/leonardoacosta/installfest/apps/wavetui/internal/store"
)

// SourceNameTmux identifies this source in store.SourceError.Source and
// store.SourceOKEvent.Source.
const SourceNameTmux = "tmux"

// defaultTmuxPoll is how often TmuxSource re-scans panes. Unlike
// BeadsSource/OpenSpecSource there is no filesystem to fsnotify-watch here —
// pane options are tmux server state, not files — so a plain poll loop is
// the whole mechanism (mirrors design.md's own framing: cc-tmux's hooks fire
// synchronously on real transitions, so a poll interval is "strictly more
// current than any poll interval could be" only in the sense that reading
// @cc-state fresh each tick is always at least as current as the last real
// hook-driven write; TmuxSource does not itself need push delivery to stay
// close to that ground truth for a background-refresh-rate UI).
const defaultTmuxPoll = 2 * time.Second

// cc-tmux's own pane-option names (apps/cc-tmux/src/cc_tmux/tmux.py). Only
// the two this source needs are declared here — TmuxSource never writes any
// cc-tmux option, only reads.
const (
	ccStateOpt     = "@cc-state"
	ccSessionIDOpt = "@cc-session-id"
)

// PaneState is one pane's resolved state, however it was obtained (the
// primary @cc-state read, or the process-tree fallback).
type PaneState struct {
	PaneID    string
	SessionID string // "" when unknown (untagged pane, or fallback path found no session id)
	State     string // "active" | "idle" | "waiting" (cc-tmux's own vocabulary) or e.g. "running" for a fallback-detected claude process; "" means "found a claude process but no state signal"
	// Tagged is true when this pane carried a real @cc-state option (the
	// primary path). False means this pane's state (if any) came from the
	// process-tree fallback.
	Tagged bool
}

// PaneStateSource is the minimal interface TranscriptSource depends on for
// its zombie-detection cross-check (design.md § Zombie detection) — kept
// separate from TmuxSource's concrete type so transcript.go never has a
// hard dependency on this file, matching the "sources never touch each
// other directly" invariant (each source only ever publishes to the bus;
// wiring one source's read helper into another is done at the composition
// root, cmd/wavetui/main.go, tasks.md [3.3] — not inside either source).
// A nil PaneStateSource is valid and must be treated as "no data for any
// pane" (fail-open) by every caller.
type PaneStateSource interface {
	// StateForSession reports the last-known pane state for the pane whose
	// @cc-session-id matches sessionID. ok=false means no tracked pane
	// claims this session — the fail-open case design.md requires
	// ("inactivity alone still badges, since not every session runs inside
	// a cc-tmux-tracked pane").
	StateForSession(sessionID string) (paneID, state string, ok bool)
}

// tmuxCLI is the shell-out boundary TmuxSource depends on, so tests can
// inject a stub instead of actually invoking tmux/ps. execTmuxCLI (below) is
// the only implementation that ever touches os/exec.
type tmuxCLI interface {
	// ListPanes returns one line per pane: "<pane_id>\t<pane_pid>".
	ListPanes(ctx context.Context) ([]byte, error)
	// ShowOption reads a single pane option's value. ok=false means the pane
	// has no such option set (or does not exist) — tmux's own fail-open
	// shape (exit 1, stderr "invalid option: ..." or "no such pane: ...").
	// This is never treated as a SourceError — an untagged pane is the
	// expected, common case (design.md's whole reason for the fallback
	// path to exist).
	ShowOption(ctx context.Context, paneID, option string) (value string, ok bool)
	// ProcessTree returns `ps -axo pid,ppid,comm` output verbatim.
	ProcessTree(ctx context.Context) ([]byte, error)
}

type execTmuxCLI struct{}

func (execTmuxCLI) ListPanes(ctx context.Context) ([]byte, error) {
	return runJSON(ctx, "tmux", "list-panes", "-a", "-F", "#{pane_id}\t#{pane_pid}")
}

func (execTmuxCLI) ShowOption(ctx context.Context, paneID, option string) (string, bool) {
	out, err := runJSON(ctx, "tmux", "show-options", "-p", "-v", "-t", paneID, option)
	if err != nil {
		// Fail-open: an unset option or a pane that has since closed both
		// land here (tmux exits 1 in both cases) — never surfaced as a
		// source error, since "this pane isn't tagged" is the expected,
		// common case the process-tree fallback exists for.
		return "", false
	}
	return strings.TrimSpace(string(out)), true
}

func (execTmuxCLI) ProcessTree(ctx context.Context) ([]byte, error) {
	return runJSON(ctx, "ps", "-axo", "pid,ppid,comm")
}

// TmuxSource polls cc-tmux's `@cc-state`/`@cc-session-id` pane options (the
// primary path, for every pane that carries them) and falls back to a
// process-tree walk (only for panes that carry neither) on each tick,
// republishing a SourceOK/SourceError badge and caching the resolved
// sessionID -> PaneState map so it can serve PaneStateSource lookups for
// TranscriptSource's zombie cross-check.
type TmuxSource struct {
	bus  *bus.Bus
	cli  tmuxCLI
	poll time.Duration

	// byPaneID and bySessionID are both derived from the same scan each
	// tick — bySessionID is what StateForSession reads; byPaneID exists
	// only so a caller/test can inspect the raw per-pane view. Guarded by
	// mu since StateForSession may be called concurrently from the
	// bubbletea/UI goroutine while scanOnce runs on this source's own Run
	// goroutine (same rationale as store.Store's own mutex).
	mu          sync.Mutex
	byPaneID    map[string]PaneState
	bySessionID map[string]PaneState

	afterScan func() // test-only hook, called after every scan
}

// NewTmuxSource constructs a TmuxSource that publishes onto b.
func NewTmuxSource(b *bus.Bus) *TmuxSource {
	return &TmuxSource{
		bus:         b,
		cli:         execTmuxCLI{},
		poll:        defaultTmuxPoll,
		byPaneID:    make(map[string]PaneState),
		bySessionID: make(map[string]PaneState),
	}
}

// StateForSession implements PaneStateSource.
func (s *TmuxSource) StateForSession(sessionID string) (paneID, state string, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, found := s.bySessionID[sessionID]
	if !found {
		return "", "", false
	}
	return p.PaneID, p.State, true
}

// Run polls tmux pane state until ctx is cancelled. Every goroutine Run
// starts is derived from ctx (task 2.4's audit requirement, carried forward
// from wavetui-core). Run itself returns once ctx is done.
func (s *TmuxSource) Run(ctx context.Context) error {
	s.scanOnce(ctx)

	ticker := time.NewTicker(s.poll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			s.scanOnce(ctx)
		}
	}
}

// scanOnce lists every pane, resolves each one's state (primary @cc-state
// path, process-tree fallback for panes with neither), and republishes the
// full map plus a SourceOK/SourceError badge. A tmux-unavailable failure
// (no tmux binary, no panes reachable) degrades to SourceErrorEvent and
// leaves the last-good map in place — never a panic, matching every other
// source's tolerant-degrade convention.
func (s *TmuxSource) scanOnce(ctx context.Context) {
	if s.afterScan != nil {
		defer s.afterScan()
	}

	raw, err := s.cli.ListPanes(ctx)
	if err != nil {
		s.bus.Publish(store.SourceErrorEvent{Error: store.SourceError{
			Source:    SourceNameTmux,
			Message:   fmt.Sprintf("tmux list-panes: %v", err),
			Timestamp: time.Now(),
		}})
		return
	}

	panes := parsePaneList(raw)

	byPaneID := make(map[string]PaneState, len(panes))
	bySessionID := make(map[string]PaneState, len(panes))

	var untaggedPIDs []int64
	untaggedByPID := make(map[int64]string) // pid -> paneID, for the fallback pass below

	for _, p := range panes {
		state, stateOK := s.cli.ShowOption(ctx, p.paneID, ccStateOpt)
		sessionID, sidOK := s.cli.ShowOption(ctx, p.paneID, ccSessionIDOpt)

		if stateOK && state != "" {
			// Primary path: this pane is cc-tmux-tagged. Never re-derive
			// via process-tree walk for a tagged pane (design.md's explicit
			// requirement) — take @cc-state as-is, even if @cc-session-id
			// happened to come back empty (sessionID just stays "" and this
			// pane simply isn't indexable by StateForSession).
			ps := PaneState{PaneID: p.paneID, State: state, Tagged: true}
			if sidOK && sessionID != "" {
				ps.SessionID = sessionID
				bySessionID[sessionID] = ps
			}
			byPaneID[p.paneID] = ps
			continue
		}

		// Untagged: candidate for the process-tree fallback. Deferred to a
		// single batched ps scan below rather than one `ps` call per pane.
		if p.pid > 0 {
			untaggedPIDs = append(untaggedPIDs, p.pid)
			untaggedByPID[p.pid] = p.paneID
		}
	}

	if len(untaggedPIDs) > 0 {
		if psRaw, err := s.cli.ProcessTree(ctx); err == nil {
			foundClaudePID := findClaudeDescendants(psRaw, untaggedPIDs)
			for _, pid := range untaggedPIDs {
				paneID := untaggedByPID[pid]
				if foundClaudePID[pid] {
					// A claude process exists under this pane's shell —
					// report it as running, with no state/session-id
					// (process-tree gives no state signal, only presence).
					// Never inferred from a neighboring tagged pane (no
					// positional assumption).
					byPaneID[paneID] = PaneState{PaneID: paneID, State: "running", Tagged: false}
				}
				// No claude process found: no result at all (not a guess) —
				// this pane is simply absent from byPaneID/bySessionID.
			}
		}
		// A ProcessTree failure is tolerated silently here: the fallback is
		// itself a best-effort path for panes that were already going to
		// report "no data" absent it, so failing to run `ps` just means
		// those panes stay absent from the map — not a SourceError.
	}

	s.mu.Lock()
	s.byPaneID = byPaneID
	s.bySessionID = bySessionID
	s.mu.Unlock()

	s.bus.Publish(store.SourceOKEvent{Source: SourceNameTmux})
}

type paneListEntry struct {
	paneID string
	pid    int64
}

// parsePaneList decodes ListPanes' "<pane_id>\t<pane_pid>" lines, tolerantly
// skipping any line that doesn't match (never a fatal parse error — same
// tolerant-decode convention as every other source).
func parsePaneList(raw []byte) []paneListEntry {
	lines := bytes.Split(bytes.TrimSpace(raw), []byte("\n"))
	entries := make([]paneListEntry, 0, len(lines))
	for _, line := range lines {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		parts := strings.SplitN(string(line), "\t", 2)
		if len(parts) != 2 {
			continue
		}
		pid, err := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
		if err != nil {
			pid = 0
		}
		entries = append(entries, paneListEntry{paneID: strings.TrimSpace(parts[0]), pid: pid})
	}
	return entries
}

// findClaudeDescendants parses `ps -axo pid,ppid,comm` output into a
// child-process adjacency map and, for each root PID in roots, walks its
// descendants looking for a process named "claude" (case-insensitive exact
// match on the comm field — deliberately not a substring match, to avoid a
// false positive against an unrelated binary that merely mentions "claude").
// Returns the set of root PIDs for which such a descendant was found.
// design.md's explicit requirement: never assume a positional relationship
// between panes — this only ever walks the process tree rooted at the
// pane's OWN shell PID, never a neighboring pane's.
func findClaudeDescendants(psOutput []byte, roots []int64) map[int64]bool {
	children := make(map[int64][]int64)
	comm := make(map[int64]string)

	lines := bytes.Split(psOutput, []byte("\n"))
	for i, line := range lines {
		if i == 0 {
			continue // header row ("PID PPID COMMAND" or similar)
		}
		fields := strings.Fields(string(line))
		if len(fields) < 3 {
			continue
		}
		pid, err1 := strconv.ParseInt(fields[0], 10, 64)
		ppid, err2 := strconv.ParseInt(fields[1], 10, 64)
		if err1 != nil || err2 != nil {
			continue
		}
		c := strings.Join(fields[2:], " ")
		comm[pid] = c
		children[ppid] = append(children[ppid], pid)
	}

	found := make(map[int64]bool, len(roots))
	for _, root := range roots {
		seen := map[int64]bool{root: true}
		queue := append([]int64(nil), children[root]...)
		for len(queue) > 0 {
			pid := queue[0]
			queue = queue[1:]
			if seen[pid] {
				continue
			}
			seen[pid] = true
			if strings.EqualFold(comm[pid], "claude") {
				found[root] = true
				break
			}
			queue = append(queue, children[pid]...)
		}
	}
	return found
}
