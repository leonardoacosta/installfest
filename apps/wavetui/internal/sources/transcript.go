// See doc.go for the package-level contract. This file implements
// TranscriptSource — see openspec/changes/wavetui-sessions/tasks.md
// [2.1]-[2.4] and design.md § Verified transcript fields / § Zombie
// detection / § Rate-limit backpressure.
//
// TranscriptSource tails every `*.jsonl` file under
// `~/.claude/projects/<flattened-root>/` (one file per Claude Code session,
// named `<sessionId>.jsonl` — confirmed against this environment's own real
// transcript directory during authoring), maintaining a per-file byte
// offset and a partial-line remainder buffer so only newly-appended,
// complete lines are decoded on each pass. Every decode is tolerant: an
// unknown `type` value is a no-op (design.md's real-dump finding — ten
// distinct `type` values were observed in a single real session, several
// carrying no `usage`/`cwd` at all), and a malformed JSON line is skipped,
// never fatal — a bad line degrades that one session's derived state, never
// the whole source or the app (spec.md's "malformed line" Requirement).
package sources

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/leonardoacosta/installfest/apps/wavetui/internal/bus"
	"github.com/leonardoacosta/installfest/apps/wavetui/internal/store"
)

// SourceNameTranscript identifies this source in store.SourceError.Source
// and store.SourceOKEvent.Source.
const SourceNameTranscript = "transcript"

const (
	// defaultTranscriptDebounce mirrors BeadsSource/OpenSpecSource's
	// trailing-edge debounce window.
	defaultTranscriptDebounce = 400 * time.Millisecond
	// defaultTranscriptPoll is the periodic full-rescan fallback,
	// independent of fsnotify — belt-and-suspenders for a dropped watch,
	// same rationale as every other source in this package.
	defaultTranscriptPoll = 5 * time.Second
	// defaultZombieCheckEvery re-evaluates zombie status on a timer, not
	// just on new transcript bytes — a session can go zombie purely by time
	// passing with zero new activity, which no fsnotify event would ever
	// trigger.
	defaultZombieCheckEvery = 15 * time.Second

	// DefaultZombieInactivity is design.md's "N minutes (config, default
	// 15)" zombie-detection threshold.
	DefaultZombieInactivity = 15 * time.Minute

	// defaultModelContextWindow is an approximate context-window size (in
	// tokens) used for every model — design.md explicitly calls this an
	// "approximate model-window size" estimate, not a per-model exact
	// figure; a future revision could key this by the session's observed
	// model name instead of one constant.
	defaultModelContextWindow int64 = 200_000

	// ContextGaugeThresholdPct is the handoff-prompt badge threshold —
	// exported so SessionsPane (tasks.md [3.1], a later batch) renders the
	// badge off the same constant this source uses, rather than
	// re-declaring the 70% magic number independently.
	ContextGaugeThresholdPct = 70.0
)

// IsHandoffThreshold reports whether pct (a SessionLink.ContextPct value)
// has crossed the handoff-prompt badge threshold. store.SessionLink has no
// dedicated boolean field for this (design.md's Store shape only carries
// the raw ContextPct) — the badge is a pure render-time computation off
// that percent, and this helper is the single source of truth for it so a
// future caller (SessionsPane) never re-derives the 70% comparison itself.
func IsHandoffThreshold(pct float64) bool {
	return pct >= ContextGaugeThresholdPct
}

// --- Error classification (design.md's API-batch addendum) ---------------

// Real error strings confirmed by dumping this environment's own live
// transcripts during authoring (never assumed):
//   - read-first violation:   "<tool_use_error>File has not been read yet.
//     Read it first before writing to it.</tool_use_error>"
//   - edit string-not-found:  "<tool_use_error>String to replace not found
//     in file.\nString: ...</tool_use_error>"
//   - gate.sh BLOCKED output: "PreToolUse:<Tool> hook error:
//     [<hook-path>]: BLOCKED: <reason>"
var (
	reReadFirstViolation = regexp.MustCompile(`(?i)has not been read yet`)
	reEditStringNotFound = regexp.MustCompile(`(?i)string to replace not found`)
	reGateBlocked        = regexp.MustCompile(`hook error: \[[^\]]*\]:\s*BLOCKED:`)
)

const (
	errorClassReadFirst    = "read_first_violation"
	errorClassEditNotFound = "edit_string_not_found"
	errorClassGateBlocked  = "gate_blocked"
	errorClassUnclassified = "unclassified"
)

// classifyToolError maps a tool_result error's rendered text to one of the
// recognized classes, per design.md/spec.md's "Error feed" Requirement.
// Never returns an empty string — an unrecognized shape still gets a class
// ("unclassified"), matching spec.md: "still recorded in the error feed
// under a generic/unclassified class rather than being discarded."
func classifyToolError(text string) string {
	switch {
	case reReadFirstViolation.MatchString(text):
		return errorClassReadFirst
	case reEditStringNotFound.MatchString(text):
		return errorClassEditNotFound
	case reGateBlocked.MatchString(text):
		return errorClassGateBlocked
	default:
		return errorClassUnclassified
	}
}

// --- Rate-limit indicator (design.md's honesty caveat: heuristic, not a
// verified live-positive field match) --------------------------------------

var rateLimitKeywords = []string{"rate limit", "rate_limit_error", "429", "overloaded"}

func looksRateLimited(text string) bool {
	lower := strings.ToLower(text)
	for _, kw := range rateLimitKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

// --- Tolerant transcript-line decoding ------------------------------------

// rawTranscriptLine is the tolerant-decode contract in practice: only the
// fields this source understands are declared, so any other field a future
// Claude Code release adds is silently ignored, and any field a given
// release omits just decodes to its zero value — see design.md § Verified
// transcript fields for the real field names this was checked against.
type rawTranscriptLine struct {
	Type              string      `json:"type"`
	SessionID         string      `json:"sessionId"`
	CWD               string      `json:"cwd"`
	Timestamp         string      `json:"timestamp"`
	IsSidechain       bool        `json:"isSidechain"`
	ParentUUID        string      `json:"parentUuid"`
	IsApiErrorMessage bool        `json:"isApiErrorMessage"`
	Message           *rawMessage `json:"message"`
}

type rawMessage struct {
	Role    string          `json:"role"`
	Model   string          `json:"model"`
	Content json.RawMessage `json:"content"`
	Usage   *rawUsage       `json:"usage"`
}

type rawUsage struct {
	InputTokens          int64 `json:"input_tokens"`
	CacheReadInputTokens int64 `json:"cache_read_input_tokens"`
	OutputTokens         int64 `json:"output_tokens"`
}

// rawContentBlock is one entry of a message's `content` array — used for
// both a user message's text/tool_result blocks and (defensively) an
// assistant message's blocks. Content, when Type=="tool_result", is itself
// either a plain string or a nested block array — extractText handles both.
type rawContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ToolUseID string          `json:"tool_use_id"`
	IsError   bool            `json:"is_error"`
	Content   json.RawMessage `json:"content"`
}

// decodeContentBlocks decodes a message.content value that may be a plain
// string (one earlier-era shape observed in real transcripts) or an array
// of typed blocks (the common shape). Returns nil for anything else
// (absent, malformed) — tolerant, never an error.
func decodeContentBlocks(raw json.RawMessage) []rawContentBlock {
	if len(raw) == 0 {
		return nil
	}
	var blocks []rawContentBlock
	if err := json.Unmarshal(raw, &blocks); err == nil {
		return blocks
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil && s != "" {
		return []rawContentBlock{{Type: "text", Text: s}}
	}
	return nil
}

// extractText renders a content value (string or block array) down to
// plain text, joining any text-bearing blocks. Used for tool_result.Content,
// which nests the same string-or-array shape.
func extractText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	blocks := decodeContentBlocks(raw)
	parts := make([]string, 0, len(blocks))
	for _, b := range blocks {
		if b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// --- Per-session aggregate state ------------------------------------------

type sessionAgg struct {
	sessionID    string
	cwd          string
	earliestTS   time.Time
	lastActivity time.Time
	isSidechain  bool
	parentUUID   string
	userMessages []string

	contextTokens    int64 // cumulative input_tokens + cache_read_input_tokens
	lastModel        string
	tokensByModel    map[string]int64
	executorLaneFlag bool

	errors []store.ErrorEntry

	linkedItemID string
	linked       bool

	zombie      bool
	zombieSince time.Time
}

func newSessionAgg(id string) *sessionAgg {
	return &sessionAgg{sessionID: id, tokensByModel: make(map[string]int64)}
}

func (a *sessionAgg) contextPct(window int64) float64 {
	if window <= 0 {
		return 0
	}
	pct := float64(a.contextTokens) / float64(window) * 100
	if pct > 100 {
		pct = 100
	}
	return pct
}

// --- Per-file tail state ---------------------------------------------------

type transcriptFile struct {
	path      string
	sessionID string // derived from the filename (<sessionId>.jsonl)
	offset    int64
	remainder []byte
}

// --- transcriptCLI: the shellout boundary for claimed-item metadata -------

// transcriptCLI is the shell-out boundary for the cwd+timestamp fallback's
// candidate list — see design.md § Session linkage algorithm point 2. This
// is TranscriptSource's OWN independent `bd` shellout (mirroring
// BeadsSource's own injectable beadsCLI) — sources never call each other's
// Go types directly, but two independent sources each shelling the same
// stable `bd` CLI surface is not "touching each other."
type transcriptCLI interface {
	ClaimedItems(ctx context.Context) ([]byte, error)
}

type execTranscriptCLI struct{}

// ClaimedItems lists every currently in_progress (claimed) bead — the
// candidate pool for the cwd+timestamp fallback. --limit 0 for the same
// reason BeadsSource's execBeadsCLI uses it: a capped default page would
// silently drop a real candidate.
func (execTranscriptCLI) ClaimedItems(ctx context.Context) ([]byte, error) {
	return runJSON(ctx, "bd", "list", "--json", "--limit", "0", "--status", "in_progress")
}

type claimedRecord struct {
	ID        string `json:"id"`
	StartedAt string `json:"started_at"`
}

// --- TranscriptSource -------------------------------------------------------

// TranscriptSource tails Claude Code transcript JSONL files and derives
// per-session state (link, context gauge, zombie badge, error feed, token
// meter, rate-limit signal) — see design.md § Architecture.
type TranscriptSource struct {
	root string
	bus  *bus.Bus
	cli  transcriptCLI

	// projectsDir is ~/.claude/projects by default; overridable (test-only)
	// so tests never touch a real home directory.
	projectsDir string

	poll             time.Duration
	debounce         time.Duration
	zombieCheckEvery time.Duration
	zombieAfter      time.Duration
	modelWindow      int64

	// panes is optional (nil-safe) — see PaneStateSource's doc comment for
	// why this is an interface, not a *TmuxSource field.
	panes PaneStateSource

	linker *SessionLinker

	files    map[string]*transcriptFile
	sessions map[string]*sessionAgg

	afterQuery func() // test-only hook, called after every tail/scan cycle
}

// NewTranscriptSource constructs a TranscriptSource rooted at root (the
// project root wavetui was launched from — the same root BeadsSource/
// OpenSpecSource use) that publishes onto b.
func NewTranscriptSource(root string, b *bus.Bus) *TranscriptSource {
	home, _ := os.UserHomeDir() // "" on failure is tolerated: watch simply
	// never finds a projects dir to arm, same as any other "unavailable"
	// source path.
	return &TranscriptSource{
		root:             root,
		bus:              b,
		cli:              execTranscriptCLI{},
		projectsDir:      filepath.Join(home, ".claude", "projects"),
		poll:             defaultTranscriptPoll,
		debounce:         defaultTranscriptDebounce,
		zombieCheckEvery: defaultZombieCheckEvery,
		zombieAfter:      DefaultZombieInactivity,
		modelWindow:      defaultModelContextWindow,
		linker:           NewSessionLinker(0),
		files:            make(map[string]*transcriptFile),
		sessions:         make(map[string]*sessionAgg),
	}
}

// SetPaneStateSource wires TranscriptSource's zombie-detection cross-check
// to a pane-state provider (TmuxSource in production). Called from
// cmd/wavetui/main.go (tasks.md [3.3]) after both sources are constructed —
// never required: a nil/never-called PaneStateSource means every zombie
// check fails open per design.md.
func (s *TranscriptSource) SetPaneStateSource(p PaneStateSource) {
	s.panes = p
}

// flattenProjectDir mirrors Claude Code's own directory-flattening scheme
// (confirmed against this environment's real ~/.claude/projects/ layout
// during authoring): every path separator becomes a literal hyphen. This is
// lossy — a project path containing a real hyphen is indistinguishable from
// a flattened slash, design.md's own cited reason cwd (recorded verbatim
// inside each transcript line) must always be trusted over any path
// reconstructed from this flattened name.
func flattenProjectDir(root string) string {
	return strings.ReplaceAll(root, string(filepath.Separator), "-")
}

func (s *TranscriptSource) transcriptDir() string {
	return filepath.Join(s.projectsDir, flattenProjectDir(s.root))
}

// Run watches the transcript directory and tails every *.jsonl file within
// it until ctx is cancelled. Every goroutine Run starts is derived from ctx.
func (s *TranscriptSource) Run(ctx context.Context) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("transcript: new watcher: %w", err)
	}
	defer watcher.Close()

	if err := watcher.Add(s.projectsDir); err != nil {
		// The whole ~/.claude/projects tree not existing (or not yet
		// created) is tolerated exactly like BeadsSource's missing
		// .beads/ case: publish unavailable and keep polling for it to
		// appear, never a fatal Run error over a missing directory that
		// may simply not exist yet on a machine with no prior Claude Code
		// sessions.
		s.publishUnavailable()
	}

	loop := newRequeryLoop()
	go loop.Run(ctx, s.tailAll)

	debounceIn := make(chan struct{}, 1)
	go debounce(ctx, debounceIn, s.debounce, loop.Trigger)
	signal := func() {
		select {
		case debounceIn <- struct{}{}:
		default:
		}
	}

	dir := s.transcriptDir()
	watched := false
	if dirExists(dir) {
		if err := watcher.Add(dir); err == nil {
			watched = true
			loop.Trigger() // initial tail of any pre-existing files
		}
	}

	poll := time.NewTicker(s.poll)
	defer poll.Stop()
	zombieTick := time.NewTicker(s.zombieCheckEvery)
	defer zombieTick.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			s.handleEvent(watcher, dir, ev, &watched, loop, signal)
		case _, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			// Non-fatal; the poll fallback keeps data eventually fresh.
		case <-poll.C:
			loop.Trigger()
		case <-zombieTick.C:
			s.reevaluateZombies()
		}
	}
}

func (s *TranscriptSource) handleEvent(
	w *fsnotify.Watcher,
	dir string,
	ev fsnotify.Event,
	watched *bool,
	loop *requeryLoop,
	signal func(),
) {
	switch {
	case ev.Name == s.projectsDir:
		if ev.Op.Has(fsnotify.Create) && !*watched {
			if dirExists(dir) {
				if err := w.Add(dir); err == nil {
					*watched = true
					s.publishAvailable()
					loop.Trigger()
				}
			}
		}
	case ev.Name == dir:
		switch {
		case ev.Op.Has(fsnotify.Create):
			if !*watched {
				if err := w.Add(dir); err == nil {
					*watched = true
					s.publishAvailable()
					loop.Trigger()
				}
			}
		case ev.Op.Has(fsnotify.Remove), ev.Op.Has(fsnotify.Rename):
			if *watched {
				*watched = false
				s.publishUnavailable()
			}
		}
	case *watched && filepath.Dir(ev.Name) == dir && strings.HasSuffix(ev.Name, ".jsonl"):
		signal()
	}
}

// tailAll lists every *.jsonl file in the transcript directory and tails
// any new bytes for each — a full directory rescan on any relevant event,
// same "never infer from which file changed" convention OpenSpecSource
// documents for its own requery.
func (s *TranscriptSource) tailAll(ctx context.Context) {
	if s.afterQuery != nil {
		defer s.afterQuery()
	}

	dir := s.transcriptDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		// Directory missing/unreadable: tolerated, matches every other
		// source's "not available yet" path — no SourceError here since
		// Run's own watch-arming already handles the availability signal.
		return
	}

	claims := s.fetchClaims(ctx)

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		f, ok := s.files[path]
		if !ok {
			f = &transcriptFile{path: path, sessionID: strings.TrimSuffix(e.Name(), ".jsonl")}
			s.files[path] = f
		}
		s.tailOne(f, claims)
	}
}

// fetchClaims retrieves the current claimed-item candidate pool for the
// cwd+timestamp fallback. A CLI failure degrades to an empty candidate list
// (fallback linkage just doesn't resolve this cycle) rather than blocking
// the whole tail pass — tolerant, matches the source's own convention.
func (s *TranscriptSource) fetchClaims(ctx context.Context) []ClaimedItem {
	raw, err := s.cli.ClaimedItems(ctx)
	if err != nil {
		return nil
	}
	var recs []claimedRecord
	if err := json.Unmarshal(raw, &recs); err != nil {
		return nil
	}
	claims := make([]ClaimedItem, 0, len(recs))
	for _, r := range recs {
		claimedAt, err := time.Parse(time.RFC3339, r.StartedAt)
		if err != nil {
			continue // no usable claim timestamp -> not a fallback candidate
		}
		// Every candidate's RepoPath is this wavetui instance's own root:
		// wavetui is a single-project tool (BeadsSource/OpenSpecSource are
		// both rooted at the same one root), so every bead this instance's
		// `bd` sees belongs to that one project by construction — there is
		// no per-bead repo-path field in bd's own data model to read
		// instead.
		claims = append(claims, ClaimedItem{ItemID: r.ID, RepoPath: s.root, ClaimedAt: claimedAt})
	}
	return claims
}

// tailOne reads any new bytes appended to f since its stored offset,
// buffering a trailing partial line and resetting to offset 0 on
// truncation/replacement (spec.md's "TranscriptSource tails..." Requirement
// scenarios, verbatim).
func (s *TranscriptSource) tailOne(f *transcriptFile, claims []ClaimedItem) {
	info, err := os.Stat(f.path)
	if err != nil {
		return // file disappeared mid-scan; tolerated, next pass reconciles
	}
	size := info.Size()

	if size < f.offset {
		// Truncated or replaced: reset and re-read from the start.
		f.offset = 0
		f.remainder = nil
	}
	if size == f.offset {
		return // nothing new
	}

	fh, err := os.Open(f.path)
	if err != nil {
		return
	}
	defer fh.Close()
	if _, err := fh.Seek(f.offset, io.SeekStart); err != nil {
		return
	}
	buf, err := io.ReadAll(fh)
	if err != nil {
		return
	}
	// f.offset advances by exactly len(buf): those are the only disk bytes
	// this read consumed (from f.offset to the file's size at read time).
	// It must NOT advance only by however much turned out to be complete
	// lines — the file position for the NEXT read has to start after buf,
	// never re-reading bytes already pulled into memory here. Any
	// unterminated tail of `data` lives on ONLY in f.remainder from this
	// point forward (in memory, not re-read from disk).
	newOffset := f.offset + int64(len(buf))

	data := append(f.remainder, buf...)
	lines := bytes.Split(data, []byte("\n"))
	complete := lines[:len(lines)-1]
	f.remainder = append([]byte(nil), lines[len(lines)-1]...)

	for _, line := range complete {
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 {
			continue
		}
		s.processLine(f.sessionID, trimmed, claims)
	}
	f.offset = newOffset
}

// processLine tolerantly decodes one complete transcript line and folds it
// into that line's session aggregate. Any decode failure is a silent skip
// (spec.md: "malformed JSON on one line degrades only that session's state
// ... processing continues to subsequent well-formed lines").
func (s *TranscriptSource) processLine(fileSessionID string, line []byte, claims []ClaimedItem) {
	var raw rawTranscriptLine
	if err := json.Unmarshal(line, &raw); err != nil {
		return
	}

	sid := raw.SessionID
	if sid == "" {
		sid = fileSessionID
	}
	agg, ok := s.sessions[sid]
	if !ok {
		agg = newSessionAgg(sid)
		s.sessions[sid] = agg
	}

	ts, tsErr := time.Parse(time.RFC3339, raw.Timestamp)
	if tsErr == nil {
		agg.lastActivity = ts
	} else {
		agg.lastActivity = time.Now()
	}

	switch raw.Type {
	case "user":
		s.handleUserLine(agg, raw, ts)
	case "assistant":
		s.handleAssistantLine(agg, raw)
	default:
		// Every other observed type (system, attachment, last-prompt,
		// custom-title, agent-name, mode, permission-mode,
		// file-history-snapshot, file-history-delta) and any future type
		// carries no usage/cwd/tool_result data this source derives state
		// from — tolerant no-op, per design.md's real-dump finding.
	}

	s.resolveLink(agg, claims)
	s.publishSession(agg)
}

func (s *TranscriptSource) handleUserLine(agg *sessionAgg, raw rawTranscriptLine, ts time.Time) {
	if raw.CWD != "" && agg.cwd == "" {
		agg.cwd = raw.CWD
	}
	if !ts.IsZero() && (agg.earliestTS.IsZero() || ts.Before(agg.earliestTS)) {
		agg.earliestTS = ts
	}
	if raw.IsSidechain {
		agg.isSidechain = true
		if raw.ParentUUID != "" {
			agg.parentUUID = raw.ParentUUID
		}
	}

	if raw.Message == nil {
		return
	}
	for _, b := range decodeContentBlocks(raw.Message.Content) {
		switch b.Type {
		case "text":
			if b.Text != "" {
				agg.userMessages = append(agg.userMessages, b.Text)
			}
		case "tool_result":
			s.handleToolResult(agg, b)
		}
	}
}

func (s *TranscriptSource) handleToolResult(agg *sessionAgg, b rawContentBlock) {
	if !b.IsError {
		return
	}
	text := extractText(b.Content)
	class := classifyToolError(text)
	agg.errors = append(agg.errors, store.ErrorEntry{
		Timestamp: agg.lastActivity,
		Class:     class,
		Message:   text,
	})

	if looksRateLimited(text) {
		s.emitRateLimitSignal(text)
	}
}

func (s *TranscriptSource) handleAssistantLine(agg *sessionAgg, raw rawTranscriptLine) {
	if raw.Message == nil {
		return
	}
	if raw.Message.Model != "" {
		agg.lastModel = raw.Message.Model
	}
	if u := raw.Message.Usage; u != nil {
		agg.contextTokens += u.InputTokens + u.CacheReadInputTokens
		if agg.tokensByModel == nil {
			agg.tokensByModel = make(map[string]int64)
		}
		agg.tokensByModel[raw.Message.Model] += u.OutputTokens
	}

	// design.md's addendum: a Task-dispatched subagent (isSidechain) is the
	// best available proxy for "executor lane" — this fleet's own
	// convention reserves opus for the orchestrating top-level session.
	if agg.isSidechain && strings.Contains(strings.ToLower(raw.Message.Model), "opus") {
		agg.executorLaneFlag = true
	}

	// design.md's honesty caveat: isApiErrorMessage is a real, confirmed
	// field, but no live positive example was available during authoring —
	// treated as a signal only in combination with a keyword match on the
	// rendered text of this line's own content, never on the boolean alone.
	if raw.IsApiErrorMessage {
		text := extractText(raw.Message.Content)
		if text == "" || looksRateLimited(text) {
			s.emitRateLimitSignal(text)
		}
	}
}

// resolveLink attempts to link agg to a claimed item via SessionLinker, and
// publishes a store.SessionLinkEvent the moment a link is (re)established.
// Idempotent: re-running Link on an already-linked session is cheap and
// simply reconfirms the same result (SessionLinker itself caches resolved
// sessions for sidechain inheritance).
func (s *TranscriptSource) resolveLink(agg *sessionAgg, claims []ClaimedItem) {
	sess := SessionTranscript{
		SessionID:         agg.sessionID,
		CWD:               agg.cwd,
		EarliestTimestamp: agg.earliestTS,
		IsSidechain:       agg.isSidechain,
		ParentUUID:        agg.parentUUID,
		UserMessages:      agg.userMessages,
	}
	itemID, ok := s.linker.Link(sess, claims)
	if !ok {
		return
	}
	if agg.linked && agg.linkedItemID == itemID {
		return // no change; avoid republishing an unchanged link every line
	}
	agg.linked = true
	agg.linkedItemID = itemID
}

// publishSession derives this cycle's SessionLink view and republishes it
// via store.SessionLinkEvent, and re-evaluates the zombie badge for this one
// session immediately (in addition to the periodic sweep in
// reevaluateZombies, so a session that just went quiet doesn't wait for the
// next tick to reflect its own last-known state).
func (s *TranscriptSource) publishSession(agg *sessionAgg) {
	if !agg.linked {
		return // nothing to attribute this session's state to yet
	}
	s.updateZombie(agg)
	s.bus.Publish(store.SessionLinkEvent{
		ItemID:  agg.linkedItemID,
		Session: s.toStoreSessionLink(agg),
	})
}

func (s *TranscriptSource) toStoreSessionLink(agg *sessionAgg) *store.SessionLink {
	paneID, _, _ := s.paneState(agg.sessionID)
	tokensByModel := make(map[string]int64, len(agg.tokensByModel))
	for k, v := range agg.tokensByModel {
		tokensByModel[k] = v
	}
	errs := append([]store.ErrorEntry(nil), agg.errors...)

	return &store.SessionLink{
		SessionID:        agg.sessionID,
		PaneID:           paneID,
		ContextPct:       agg.contextPct(s.modelWindow),
		LastActivity:     agg.lastActivity,
		Zombie:           agg.zombie,
		ZombieSince:      agg.zombieSince,
		ErrorCount:       len(agg.errors),
		TokensByModel:    tokensByModel,
		Errors:           errs,
		ExecutorLaneFlag: agg.executorLaneFlag,
	}
}

// paneState is a nil-safe wrapper around s.panes.StateForSession — see
// PaneStateSource's doc comment for the fail-open contract.
func (s *TranscriptSource) paneState(sessionID string) (paneID, state string, ok bool) {
	if s.panes == nil {
		return "", "", false
	}
	return s.panes.StateForSession(sessionID)
}

// updateZombie applies design.md § Zombie detection's two-signal rule: a
// session is zombie-badged only when BOTH the transcript has been quiet for
// >= zombieAfter AND (when tmux has data for this session's pane) that
// pane's state is not "active". Fail-open when tmux has no data.
func (s *TranscriptSource) updateZombie(agg *sessionAgg) {
	inactiveFor := time.Since(agg.lastActivity)
	quiet := agg.lastActivity.IsZero() || inactiveFor >= s.zombieAfter

	if !quiet {
		s.clearZombie(agg)
		return
	}

	_, state, ok := s.paneState(agg.sessionID)
	if ok && state == "active" {
		// tmux signal overrides transcript-only inactivity (spec.md: "the
		// tmux signal overrides transcript-only inactivity").
		s.clearZombie(agg)
		return
	}

	if !agg.zombie {
		agg.zombie = true
		agg.zombieSince = time.Now()
	}
}

func (s *TranscriptSource) clearZombie(agg *sessionAgg) {
	agg.zombie = false
	agg.zombieSince = time.Time{}
}

// reevaluateZombies re-checks every currently-tracked, linked session on a
// timer — a session can cross the inactivity threshold purely from time
// passing, with no new transcript bytes to trigger processLine.
func (s *TranscriptSource) reevaluateZombies() {
	for _, agg := range s.sessions {
		if !agg.linked {
			continue
		}
		wasZombie := agg.zombie
		s.updateZombie(agg)
		if agg.zombie != wasZombie {
			s.bus.Publish(store.SessionLinkEvent{
				ItemID:  agg.linkedItemID,
				Session: s.toStoreSessionLink(agg),
			})
		}
	}
}

// emitRateLimitSignal publishes a store.RateLimitSignalEvent — EMISSION
// ONLY, per design.md § Rate-limit backpressure: no consuming queue or
// scheduling logic exists anywhere in this proposal.
func (s *TranscriptSource) emitRateLimitSignal(message string) {
	if message == "" {
		message = "rate-limit indicator observed in transcript stream"
	}
	s.bus.Publish(store.RateLimitSignalEvent{Signal: store.RateLimitSignal{
		Detected: time.Now(),
		Message:  message,
	}})
}

func (s *TranscriptSource) publishUnavailable() {
	s.bus.Publish(store.SourceErrorEvent{Error: store.SourceError{
		Source:    SourceNameTranscript,
		Message:   "unavailable: transcript directory not found",
		Timestamp: time.Now(),
	}})
}

func (s *TranscriptSource) publishAvailable() {
	s.bus.Publish(store.SourceOKEvent{Source: SourceNameTranscript})
}

// ReleaseClaim releases a zombie-badged item's bd claim — the ONE-KEY,
// NEVER-AUTOMATIC operator action spec.md's Zombie-detection Requirement
// requires. This is a plain exported function callers invoke on explicit
// user input (e.g. a keypress handler in SessionsPane, tasks.md [3.1]) —
// nothing in this file calls it. `bd release` does not exist as a bd
// subcommand (confirmed via `bd --help`/`bd release --help` during
// authoring: "unknown command \"release\" for \"bd\""); the real inverse of
// `bd update --claim` (which "sets assignee to you, status to in_progress")
// is `bd update <id> --status open --assignee ""` — verified against a live
// bd install's --help output for `bd update`.
func ReleaseClaim(ctx context.Context, itemID string) error {
	_, err := runJSON(ctx, "bd", "update", itemID, "--status", "open", "--assignee", "")
	return err
}
