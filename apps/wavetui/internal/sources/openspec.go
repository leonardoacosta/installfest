// See doc.go for the package-level contract. This file implements
// OpenSpecSource — see openspec/changes/wavetui-core/tasks.md [2.2] and
// design.md § Architecture.
//
// design.md's architecture diagram lists "openspec show / tasks.md parse"
// as this source's re-query action, and the top-level invariant says
// sources re-query "the CLI" after debounce rather than parsing a changed
// file directly. Unlike BeadsSource, there is no stable CLI surface to
// shell out to here for structured data (the `openspec` binary itself may
// not even be on PATH — irrelevant to what's needed: proposal.md/tasks.md
// are plain text this repo's own conventions define, not bd's opaque
// SQLite schema). "tasks.md parse" in the diagram IS the sanctioned
// re-query action for this source. The invariant this file still honors is
// the *never infer from which file changed* half: any relevant fs event —
// whether it names proposal.md, tasks.md, or a whole new proposal
// directory — triggers the SAME full re-parse of every current proposal,
// never a targeted "only re-read the one file that changed" shortcut.
package sources

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/leonardoacosta/installfest/apps/wavetui/internal/blocker"
	"github.com/leonardoacosta/installfest/apps/wavetui/internal/bus"
	"github.com/leonardoacosta/installfest/apps/wavetui/internal/config"
	"github.com/leonardoacosta/installfest/apps/wavetui/internal/store"
)

// SourceNameOpenSpec identifies this source in store.SourceError.Source and
// store.SourceOKEvent.Source.
const SourceNameOpenSpec = "openspec"

const (
	defaultOpenSpecDebounce = 400 * time.Millisecond
	defaultOpenSpecPoll     = 15 * time.Second
)

// openspecParser is the re-query boundary OpenSpecSource depends on, so
// tests can inject a stub instead of touching a real filesystem tree.
// fsOpenspecParser (below) is the only implementation that ever touches
// disk.
type openspecParser interface {
	Parse(ctx context.Context, root string, cfg config.Config) ([]store.Item, error)
}

type fsOpenspecParser struct{}

func (fsOpenspecParser) Parse(_ context.Context, root string, cfg config.Config) ([]store.Item, error) {
	items, err := parseProposals(filepath.Join(root, "openspec", "changes"))
	if err != nil {
		return nil, err
	}
	if cfg.ShowPlans {
		items = append(items, parseFlatMarkdownDir(filepath.Join(root, "plans"), "plan")...)
	}
	if cfg.ShowAdvisorPlans {
		items = append(items, parseFlatMarkdownDir(filepath.Join(root, "advisor-plans"), "advisor-plan")...)
	}
	return items, nil
}

var (
	titleRe    = regexp.MustCompile(`(?m)^#\s+Proposal:\s*(.+)$`)
	checkboxRe = regexp.MustCompile(`(?m)^\s*-\s*\[([ xX])\]`)
)

// parseProposals walks changesDir one level deep (non-recursive — mirrors
// the watch strategy) and parses each proposal subdirectory. archive/ and
// dotfiles are skipped: archived proposals are no longer active work.
func parseProposals(changesDir string) ([]store.Item, error) {
	entries, err := os.ReadDir(changesDir)
	if err != nil {
		return nil, err
	}

	items := make([]store.Item, 0, len(entries))
	for _, e := range entries {
		if !isProposalDir(e) {
			continue
		}
		items = append(items, parseOneProposal(changesDir, e.Name()))
	}
	return items, nil
}

func isProposalDir(e os.DirEntry) bool {
	return e.IsDir() && e.Name() != "archive" && !strings.HasPrefix(e.Name(), ".")
}

// parseOneProposal never fails: a missing or unreadable proposal.md/
// tasks.md just leaves the corresponding fields at their tolerant default
// (slug-derived title, nil TaskProgress, no Blocker) rather than dropping
// the item from the queue.
func parseOneProposal(changesDir, slug string) store.Item {
	dir := filepath.Join(changesDir, slug)
	item := store.Item{
		ID:    slug,
		Kind:  store.KindProposal,
		Title: slug,
	}

	proposalPath := filepath.Join(dir, "proposal.md")
	if b, err := os.ReadFile(proposalPath); err == nil {
		content := string(b)
		if m := titleRe.FindStringSubmatch(content); m != nil {
			item.Title = strings.TrimSpace(m[1])
		}
		if note, ok := parseProposalBlocker(content); ok {
			item.Blocker = note
		}
	}

	if info, err := os.Stat(proposalPath); err == nil {
		item.CreatedAt = info.ModTime()
	}

	tasksPath := filepath.Join(dir, "tasks.md")
	if b, err := os.ReadFile(tasksPath); err == nil {
		matches := checkboxRe.FindAllStringSubmatch(string(b), -1)
		if total := len(matches); total > 0 {
			done := 0
			for _, m := range matches {
				if strings.EqualFold(m[1], "x") {
					done++
				}
			}
			item.TaskProgress = &store.TaskProgress{Done: done, Total: total}
		}
	}

	return item
}

// parseProposalBlocker looks for a "blocked: ..." line, restricted to the
// "## Context" section per design.md's location decision — scoping to that
// section (rather than the whole file) avoids a false match against
// unrelated prose elsewhere in a long proposal.
func parseProposalBlocker(proposalMD string) (*store.BlockerNote, bool) {
	section := extractSection(proposalMD, "Context")
	for _, line := range strings.Split(section, "\n") {
		if n, ok := blocker.Parse(line); ok {
			return &store.BlockerNote{Type: n.Type, Reason: n.Reason, Ref: n.Ref}, true
		}
	}
	return nil, false
}

// extractSection returns the body text of the "## <heading>" section
// (case-insensitive), i.e. everything between that heading line and the
// next "## " heading or end of file. Returns "" if the heading is absent.
func extractSection(markdown, heading string) string {
	var buf []string
	inSection := false
	for _, line := range strings.Split(markdown, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") {
			if inSection {
				break
			}
			if strings.EqualFold(strings.TrimSpace(trimmed[3:]), heading) {
				inSection = true
			}
			continue
		}
		if inSection {
			buf = append(buf, line)
		}
	}
	return strings.Join(buf, "\n")
}

// parseFlatMarkdownDir handles the plans/ and advisor-plans/ [1.4]
// config-gated directories: flat one-off markdown docs, not structured
// proposal directories, so there is no tasks.md/checkbox convention to
// parse for them — each *.md file becomes one Item titled by its filename.
// A missing/unreadable directory contributes zero items (tolerated, not an
// error) — that path is only ever reached because the caller already
// gated it behind the corresponding config flag.
func parseFlatMarkdownDir(dir, idPrefix string) []store.Item {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	items := make([]store.Item, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".md")
		item := store.Item{
			ID:    idPrefix + ":" + name,
			Kind:  store.KindProposal,
			Title: name,
		}
		if info, err := e.Info(); err == nil {
			item.CreatedAt = info.ModTime()
		}
		items = append(items, item)
	}
	return items
}

// OpenSpecSource watches openspec/changes/ (and, behind config flags,
// plans/ and advisor-plans/) and republishes the current proposal set
// after each debounced batch of changes.
type OpenSpecSource struct {
	root   string
	bus    *bus.Bus
	cfg    config.Config
	parser openspecParser

	debounce time.Duration
	poll     time.Duration

	// last mirrors BeadsSource.last — this source's own view of what it
	// last successfully published, for diff-based ItemRemoveEvents and for
	// re-publishing with Stale=true on failure. Mutated only from
	// requeryOnce (never called concurrently with itself).
	last map[string]store.Item

	// watchedDirs tracks which openspec/changes/<slug>/ subdirectories
	// currently have an fsnotify watch, so a Remove/Rename can be
	// unregistered and a new directory (dir-create re-arm) can be added
	// without re-walking the whole tree.
	watchedDirs map[string]bool

	// afterQuery is a test-only hook, see BeadsSource.afterQuery.
	afterQuery func()
}

// NewOpenSpecSource constructs an OpenSpecSource rooted at root that
// publishes onto b, honoring cfg's plans/advisor-plans visibility flags.
func NewOpenSpecSource(root string, b *bus.Bus, cfg config.Config) *OpenSpecSource {
	return &OpenSpecSource{
		root:        root,
		bus:         b,
		cfg:         cfg,
		parser:      fsOpenspecParser{},
		debounce:    defaultOpenSpecDebounce,
		poll:        defaultOpenSpecPoll,
		last:        make(map[string]store.Item),
		watchedDirs: make(map[string]bool),
	}
}

func (s *OpenSpecSource) openspecDir() string { return filepath.Join(s.root, "openspec") }
func (s *OpenSpecSource) changesDir() string  { return filepath.Join(s.openspecDir(), "changes") }

// Run watches openspec/changes/ and re-parses it on change, until ctx is
// cancelled. Every goroutine Run starts is derived from ctx (task 2.4's
// audit requirement).
func (s *OpenSpecSource) Run(ctx context.Context) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("openspec: new watcher: %w", err)
	}
	defer watcher.Close()

	// Always watch root, mirroring BeadsSource: this is the fallback that
	// notices `openspec/` itself being created on a repo that has not
	// adopted OpenSpec at all yet.
	if err := watcher.Add(s.root); err != nil {
		return fmt.Errorf("openspec: watch %s: %w", s.root, err)
	}

	loop := newRequeryLoop()
	go loop.Run(ctx, s.requery)

	debounceIn := make(chan struct{}, 1)
	go debounce(ctx, debounceIn, s.debounce, loop.Trigger)
	signal := func() {
		select {
		case debounceIn <- struct{}{}:
		default:
		}
	}

	openspecDir := s.openspecDir()
	changesDir := s.changesDir()

	openspecWatched := false
	changesAvailable := false

	if dirExists(openspecDir) {
		if err := watcher.Add(openspecDir); err == nil {
			openspecWatched = true
		}
	}
	if dirExists(changesDir) {
		if err := s.armChanges(watcher, changesDir); err == nil {
			changesAvailable = true
			loop.Trigger() // initial query
		}
	}
	if !changesAvailable {
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
			s.handleEvent(watcher, openspecDir, changesDir, ev, &openspecWatched, &changesAvailable, loop, signal)
		case _, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			// Non-fatal; the poll fallback below keeps data eventually
			// fresh even if the watch degrades.
		case <-poll.C:
			loop.Trigger()
		}
	}
}

// armChanges watches changesDir itself (to catch new proposal directories
// being created — "dir-create re-arm") plus every existing proposal
// subdirectory (so writes to their proposal.md/tasks.md are seen; fsnotify
// is not recursive, so each subdirectory needs its own watch).
func (s *OpenSpecSource) armChanges(w *fsnotify.Watcher, changesDir string) error {
	if err := w.Add(changesDir); err != nil {
		return err
	}
	entries, err := os.ReadDir(changesDir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !isProposalDir(e) {
			continue
		}
		sub := filepath.Join(changesDir, e.Name())
		if err := w.Add(sub); err == nil {
			s.watchedDirs[sub] = true
		}
	}
	return nil
}

func (s *OpenSpecSource) handleEvent(
	w *fsnotify.Watcher,
	openspecDir, changesDir string,
	ev fsnotify.Event,
	openspecWatched, changesAvailable *bool,
	loop *requeryLoop,
	signal func(),
) {
	switch {
	case ev.Name == openspecDir:
		switch {
		case ev.Op.Has(fsnotify.Create):
			if !*openspecWatched {
				if err := w.Add(openspecDir); err == nil {
					*openspecWatched = true
				}
			}
		case ev.Op.Has(fsnotify.Remove), ev.Op.Has(fsnotify.Rename):
			if *openspecWatched {
				*openspecWatched = false
				if *changesAvailable {
					*changesAvailable = false
					s.watchedDirs = make(map[string]bool)
					s.publishUnavailable()
				}
			}
		}

	case ev.Name == changesDir:
		switch {
		case ev.Op.Has(fsnotify.Create):
			if !*changesAvailable {
				if err := s.armChanges(w, changesDir); err == nil {
					*changesAvailable = true
					s.publishAvailable()
					loop.Trigger()
				}
			}
		case ev.Op.Has(fsnotify.Remove), ev.Op.Has(fsnotify.Rename):
			if *changesAvailable {
				*changesAvailable = false
				s.watchedDirs = make(map[string]bool)
				s.publishUnavailable()
			}
		}

	case *changesAvailable && filepath.Dir(ev.Name) == changesDir:
		// A proposal directory appeared, was removed, or was renamed
		// directly under changes/. Any of these warrants a re-parse; a
		// newly-created directory additionally needs its own watch added
		// (dir-create re-arm), since fsnotify does not watch recursively.
		base := filepath.Base(ev.Name)
		if base != "archive" && !strings.HasPrefix(base, ".") {
			if ev.Op.Has(fsnotify.Create) {
				if info, err := os.Stat(ev.Name); err == nil && info.IsDir() {
					if err := w.Add(ev.Name); err == nil {
						s.watchedDirs[ev.Name] = true
					}
				}
			}
			if ev.Op.Has(fsnotify.Remove) || ev.Op.Has(fsnotify.Rename) {
				delete(s.watchedDirs, ev.Name)
			}
			signal()
		}

	case *changesAvailable && s.watchedDirs[filepath.Dir(ev.Name)]:
		// A write inside an already-watched proposal directory
		// (proposal.md/tasks.md content change — the common case).
		if ev.Op.Has(fsnotify.Rename) || ev.Op.Has(fsnotify.Remove) {
			// Editors/tools frequently save via tmp+rename, which can
			// orphan an inode-based watch on some platforms. Re-add the
			// watch by path defensively: a no-op if it's still valid, a
			// recovery if the platform dropped it.
			_ = w.Add(filepath.Dir(ev.Name))
		}
		signal()
	}
}

// requery re-parses the full current proposal set and publishes the diff.
// Unlike BeadsSource, OpenSpecSource has no retry-backoff requirement in
// tasks.md [2.2] — the 15s poll fallback and the next real fs event are
// its retry mechanism.
func (s *OpenSpecSource) requery(ctx context.Context) {
	if s.afterQuery != nil {
		defer s.afterQuery()
	}

	items, err := s.parser.Parse(ctx, s.root, s.cfg)
	if err != nil {
		s.markStale(err)
		return
	}

	current := make(map[string]store.Item, len(items))
	for _, item := range items {
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
	s.bus.Publish(store.SourceOKEvent{Source: SourceNameOpenSpec})
}

func (s *OpenSpecSource) markStale(err error) {
	s.bus.Publish(store.SourceErrorEvent{Error: store.SourceError{
		Source:    SourceNameOpenSpec,
		Message:   err.Error(),
		Timestamp: time.Now(),
	}})
	for _, item := range s.last {
		item.Stale = true
		s.bus.Publish(store.ItemUpsertEvent{Item: item})
	}
}

func (s *OpenSpecSource) publishUnavailable() {
	s.bus.Publish(store.SourceErrorEvent{Error: store.SourceError{
		Source:    SourceNameOpenSpec,
		Message:   "unavailable: openspec/changes/ not found",
		Timestamp: time.Now(),
	}})
}

func (s *OpenSpecSource) publishAvailable() {
	s.bus.Publish(store.SourceOKEvent{Source: SourceNameOpenSpec})
}
