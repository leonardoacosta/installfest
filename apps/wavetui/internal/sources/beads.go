// See doc.go for the package-level contract. This file implements
// BeadsSource — see openspec/changes/wavetui-core/tasks.md [2.1] and
// design.md § Architecture / § Alternatives (why it shells bd instead of
// reading .beads/*.db directly).
package sources

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/leonardoacosta/installfest/apps/wavetui/internal/blocker"
	"github.com/leonardoacosta/installfest/apps/wavetui/internal/bus"
	"github.com/leonardoacosta/installfest/apps/wavetui/internal/store"
)

// SourceNameBeads identifies this source in store.SourceError.Source and
// store.SourceOKEvent.Source.
const SourceNameBeads = "beads"

const (
	// defaultBeadsDebounce is the trailing-edge debounce window: 300-500ms
	// per design.md/tasks.md [2.1].
	defaultBeadsDebounce = 400 * time.Millisecond
	// defaultBeadsPoll is the periodic re-query fallback, independent of
	// fsnotify — belt-and-suspenders for a watch that silently stops
	// delivering (e.g. an inotify instance limit hit elsewhere on the
	// machine).
	defaultBeadsPoll = 15 * time.Second
)

// beadsCLI is the shell-out boundary BeadsSource depends on, so tests can
// inject a stub instead of actually invoking bd. execBeadsCLI (below) is
// the only thing that ever touches os/exec.
type beadsCLI interface {
	List(ctx context.Context) ([]byte, error)
	Ready(ctx context.Context) ([]byte, error)
}

// execBeadsCLI shells out to the real bd binary.
//
// --limit 0 is mandatory on both calls: `bd list` defaults to --limit 50
// and `bd ready` defaults to --limit 10 (confirmed against a live bd
// install). Either default silently undercounts on a repo with more
// open/ready issues than the cap, which would misreport a truly-ready item
// as blocked purely because it fell off the tail of a capped page — not a
// hypothetical, this is a documented footgun in this repo's own operating
// notes (`bd ready` default limit 10).
type execBeadsCLI struct{}

func (execBeadsCLI) List(ctx context.Context) ([]byte, error) {
	return runJSON(ctx, "bd", "list", "--json", "--limit", "0")
}

func (execBeadsCLI) Ready(ctx context.Context) ([]byte, error) {
	return runJSON(ctx, "bd", "ready", "--json", "--limit", "0")
}

func runJSON(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// beadRecord is the subset of `bd list`/`bd ready` JSON fields wavetui
// understands. This is the tolerant-decode contract in practice:
// encoding/json.Unmarshal only ever populates fields this struct declares
// (any other field bd emits is silently ignored — "unknown fields ignored"),
// and any field declared here that a given bd version happens to omit from
// its output simply decodes to that field's zero value ("missing optional
// fields -> zero value") — neither case needs special-case code.
type beadRecord struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
	Notes  string `json:"notes"`
	// Description is bd's own `description` field, threaded verbatim into
	// Item.Description — see wavetui-item-description's spec.md "a bead's
	// description is threaded through from bd's own output" scenario.
	Description string `json:"description"`
	// CreatedAt is RFC3339 text on the wire; parsed tolerantly in toItem
	// (an unparsable or absent value just leaves Item.CreatedAt zero).
	CreatedAt string `json:"created_at"`
}

// BeadsSource watches .beads/ and republishes bd's own view of the world
// (bd list + bd ready) after each debounced batch of changes. It never
// parses .beads/*.db itself — see design.md § Alternatives/Related Work:
// bd's on-disk schema is not a stable contract across bd releases, while
// `bd list`/`bd ready --json` are the documented stable interface. Watching
// the WAL/db files is purely a debounce trigger, never a data source.
type BeadsSource struct {
	root string // project root; .beads/ is expected directly under it
	bus  *bus.Bus
	cli  beadsCLI

	debounce time.Duration
	poll     time.Duration

	// last is this source's own view of what it last successfully
	// published, keyed by item ID. Sources never read Store state (they
	// only ever publish to the bus — see the "sources never touch Store
	// internals" constraint), so this local cache is what lets a failed
	// requery re-publish the SAME items with Stale=true, and what lets a
	// successful requery diff away items bd no longer returns via
	// ItemRemoveEvent. Mutated only from requeryOnce, which the
	// requeryLoop guarantees is never called concurrently with itself.
	last map[string]store.Item

	// failCount tracks consecutive requeryOnce failures, for backoff.
	// Reset to 0 on any success. Mutated only from the Run goroutine's
	// requeryLoop callback, same single-writer guarantee as last.
	failCount int

	// afterQuery, when set, is invoked once after every requeryOnce
	// attempt (success or failure) — a test-only hook for the
	// debounce-coalescing assertion ("exactly one re-query"). Production
	// callers never set it.
	afterQuery func()
}

// NewBeadsSource constructs a BeadsSource rooted at root (a project
// root — typically the cwd wavetui was launched from) that publishes onto
// b.
func NewBeadsSource(root string, b *bus.Bus) *BeadsSource {
	return &BeadsSource{
		root:     root,
		bus:      b,
		cli:      execBeadsCLI{},
		debounce: defaultBeadsDebounce,
		poll:     defaultBeadsPoll,
		last:     make(map[string]store.Item),
	}
}

func (s *BeadsSource) beadsDir() string { return filepath.Join(s.root, ".beads") }

// isBeadsDataFile reports whether name (a full path) is one of the files
// design.md names: .beads/*.db, *.db-wal, *.db-shm.
func isBeadsDataFile(name string) bool {
	base := filepath.Base(name)
	return strings.HasSuffix(base, ".db") ||
		strings.HasSuffix(base, ".db-wal") ||
		strings.HasSuffix(base, ".db-shm")
}

// Run watches .beads/ and re-queries bd on change, until ctx is cancelled.
// Every goroutine Run starts is derived from ctx (task 2.4's audit
// requirement) and exits when ctx is done; Run itself returns once ctx is
// done (or on a fatal setup error, e.g. the OS refusing to hand out a
// watcher).
func (s *BeadsSource) Run(ctx context.Context) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("beads: new watcher: %w", err)
	}
	defer watcher.Close()

	// Always watch root: this is how we notice `.beads/` being created
	// later if it's missing now (task 2.3), and how we re-arm if `.beads/`
	// itself is ever removed+recreated (the tmp+rename/rmdir+mkdir class
	// of "orphaned watch" edge case, applied at the directory level).
	if err := watcher.Add(s.root); err != nil {
		return fmt.Errorf("beads: watch %s: %w", s.root, err)
	}

	loop := newRequeryLoop()
	go loop.Run(ctx, func(c context.Context) {
		if s.requeryOnce(c) {
			s.failCount = 0
			return
		}
		s.failCount++
		delay := backoffDelay(s.failCount, s.poll)
		go func() {
			select {
			case <-c.Done():
			case <-time.After(delay):
				loop.Trigger()
			}
		}()
	})

	debounceIn := make(chan struct{}, 1)
	go debounce(ctx, debounceIn, s.debounce, loop.Trigger)
	signal := func() {
		select {
		case debounceIn <- struct{}{}:
		default:
		}
	}

	beadsDir := s.beadsDir()
	available := dirExists(beadsDir)
	if available {
		if err := watcher.Add(beadsDir); err != nil {
			return fmt.Errorf("beads: watch %s: %w", beadsDir, err)
		}
		loop.Trigger() // initial query
	} else {
		s.publishUnavailable()
	}

	poll := time.NewTicker(s.poll)
	defer poll.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			s.handleEvent(watcher, beadsDir, ev, &available, loop, signal)
		case _, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			// fsnotify internal error (e.g. a watch queue overflow); not
			// fatal to wavetui — the poll fallback keeps data eventually
			// fresh regardless of a dropped/degraded watch.
		case <-poll.C:
			loop.Trigger()
		}
	}
}

// handleEvent classifies one fsnotify.Event and either flips the
// available/unavailable transition (task 2.3) or signals the debounce
// timer (the common "something in .beads/ changed" path).
func (s *BeadsSource) handleEvent(w *fsnotify.Watcher, beadsDir string, ev fsnotify.Event, available *bool, loop *requeryLoop, signal func()) {
	switch {
	case ev.Name == beadsDir:
		switch {
		case ev.Op.Has(fsnotify.Create):
			if !*available {
				if err := w.Add(beadsDir); err == nil {
					*available = true
					s.publishAvailable()
					loop.Trigger()
				}
			}
		case ev.Op.Has(fsnotify.Remove), ev.Op.Has(fsnotify.Rename):
			if *available {
				*available = false
				s.publishUnavailable()
			}
		}
	case *available && filepath.Dir(ev.Name) == beadsDir && isBeadsDataFile(ev.Name):
		signal()
	}
}

// requeryOnce runs `bd list`/`bd ready`, decodes them tolerantly, and
// publishes the result. It returns true on success. On any failure (a
// non-zero exit from either command, or malformed JSON from either), it
// leaves the Store's last-good items in place — republished with
// Stale=true — and publishes a SourceErrorEvent instead of touching the
// item set. Never called concurrently with itself (guaranteed by
// requeryLoop), so mutating s.last/s.failCount here needs no lock.
func (s *BeadsSource) requeryOnce(ctx context.Context) bool {
	if s.afterQuery != nil {
		defer s.afterQuery()
	}

	listRaw, err := s.cli.List(ctx)
	if err != nil {
		s.markStale(fmt.Errorf("bd list: %w", err))
		return false
	}
	readyRaw, err := s.cli.Ready(ctx)
	if err != nil {
		s.markStale(fmt.Errorf("bd ready: %w", err))
		return false
	}

	var listRecs []beadRecord
	if err := json.Unmarshal(listRaw, &listRecs); err != nil {
		s.markStale(fmt.Errorf("bd list: malformed json: %w", err))
		return false
	}
	var readyRecs []beadRecord
	if err := json.Unmarshal(readyRaw, &readyRecs); err != nil {
		s.markStale(fmt.Errorf("bd ready: malformed json: %w", err))
		return false
	}

	ready := make(map[string]bool, len(readyRecs))
	for _, r := range readyRecs {
		ready[r.ID] = true
	}

	current := make(map[string]store.Item, len(listRecs))
	for _, rec := range listRecs {
		item := toItem(rec, ready)
		current[item.ID] = item
	}

	for id := range s.last {
		if _, ok := current[id]; !ok {
			s.bus.Publish(store.ItemRemoveEvent{ID: id})
		}
	}
	for _, item := range current {
		s.bus.Publish(store.ItemUpsertEvent{Item: item})
	}
	s.last = current
	s.bus.Publish(store.SourceOKEvent{Source: SourceNameBeads})
	return true
}

// toItem converts one decoded bd record into a store.Item.
//
// Blocker precedence: an explicit "blocked: ..." line in the bead's own
// notes (design.md's chosen location for this source) always wins. Absent
// that, an open item bd's own `ready` set omits is missing something (an
// unresolved dependency or gate) even though it carries no note explaining
// why — a synthetic generic blocker keeps that visible rather than the
// item silently looking clean. Restricted to status=="open": an
// in-progress item is also absent from `bd ready` (it's already being
// worked, not "ready to start") and must not be mislabeled blocked.
func toItem(rec beadRecord, ready map[string]bool) store.Item {
	item := store.Item{
		ID:          rec.ID,
		Kind:        store.KindBead,
		Title:       rec.Title,
		Description: rec.Description,
	}

	if t, err := time.Parse(time.RFC3339, rec.CreatedAt); err == nil {
		item.CreatedAt = t
	}
	// Tolerant: an unparsable or absent CreatedAt just leaves the zero
	// time.Time — no error, no dropped item.

	if note, ok := parseBlockerFromNotes(rec.Notes); ok {
		item.Blocker = note
	} else if rec.Status == "open" && !ready[rec.ID] {
		item.Blocker = &store.BlockerNote{
			Type:   "dependency",
			Reason: "not in bd ready — check open dependencies/gates",
		}
	}

	return item
}

func parseBlockerFromNotes(notes string) (*store.BlockerNote, bool) {
	for _, line := range strings.Split(notes, "\n") {
		if n, ok := blocker.Parse(line); ok {
			return &store.BlockerNote{Type: n.Type, Reason: n.Reason, Ref: n.Ref}, true
		}
	}
	return nil, false
}

func (s *BeadsSource) markStale(err error) {
	s.bus.Publish(store.SourceErrorEvent{Error: store.SourceError{
		Source:    SourceNameBeads,
		Message:   err.Error(),
		Timestamp: time.Now(),
	}})
	for _, item := range s.last {
		item.Stale = true
		s.bus.Publish(store.ItemUpsertEvent{Item: item})
	}
}

func (s *BeadsSource) publishUnavailable() {
	s.bus.Publish(store.SourceErrorEvent{Error: store.SourceError{
		Source:    SourceNameBeads,
		Message:   "unavailable: .beads/ not found",
		Timestamp: time.Now(),
	}})
}

func (s *BeadsSource) publishAvailable() {
	s.bus.Publish(store.SourceOKEvent{Source: SourceNameBeads})
}
