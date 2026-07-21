// Command wavetui is the entrypoint for the wavetui terminal dashboard.
//
// This batch (UI) wires the bus, Store, config, both sources, and the
// bubbletea root model end-to-end (tasks.md [3.4]): every Store mutation
// (delivered via the bus subscriber below) triggers a fresh
// Program.Send(ui.SnapshotMsg{...}) so the running tea.Program always
// reflects current Store state, and ctx cancellation (SIGINT/SIGTERM, or the
// TUI's own 'q'/ctrl+c quit) stops both sources and the Program together.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	tea "charm.land/bubbletea/v2"

	"github.com/leonardoacosta/installfest/apps/wavetui/internal/bus"
	"github.com/leonardoacosta/installfest/apps/wavetui/internal/config"
	"github.com/leonardoacosta/installfest/apps/wavetui/internal/flair"
	"github.com/leonardoacosta/installfest/apps/wavetui/internal/sources"
	"github.com/leonardoacosta/installfest/apps/wavetui/internal/store"
	"github.com/leonardoacosta/installfest/apps/wavetui/internal/timeline"
	"github.com/leonardoacosta/installfest/apps/wavetui/internal/ui"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := run(ctx, cancel); err != nil {
		fmt.Fprintln(os.Stderr, "wavetui:", err)
		os.Exit(1)
	}
}

// isExpectedShutdown reports whether err is one of tea's own "the program
// stopped because someone asked it to" sentinels (an interrupt signal, or
// the external ctx being cancelled) rather than a genuine runtime failure —
// a normal ctrl+c/SIGTERM quit must exit 0, not print a "wavetui: ..." error
// line and os.Exit(1).
func isExpectedShutdown(err error) bool {
	return err == nil ||
		errors.Is(err, tea.ErrInterrupted) ||
		errors.Is(err, tea.ErrProgramKilled) ||
		errors.Is(err, context.Canceled)
}

// run is the real entrypoint body, separated from main so it can be tested
// and so main stays a thin os.Exit wrapper. cancel is signal.NotifyContext's
// stop function: calling it both cancels ctx (stopping the sources below)
// and unregisters the signal handler — it is called once run's own work is
// done, whether that happened because ctx was cancelled externally (a real
// SIGINT/SIGTERM) or because the TUI itself quit (task 3.4's "q"/ctrl+c
// keybinding in internal/ui/root.go), so either quit path stops everything.
func run(ctx context.Context, cancel context.CancelFunc) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("wavetui: getwd: %w", err)
	}

	cfg, err := config.Load(cwd)
	if err != nil {
		return fmt.Errorf("wavetui: load config: %w", err)
	}

	b := bus.New()
	st := store.New()

	queue := ui.NewQueuePane()
	detail := ui.NewDetailPane()
	root := ui.NewRoot(queue, detail)

	// wavetui-memory-timeline (UI batch task [3.4]): MemoryTimelinePane is
	// appended to Root's own pane slice (AppendPane, append-only — queue and
	// detail are untouched) and wired to the three read-only timeline
	// sources via EnableMemoryTimeline. These sources are queried on-demand
	// per design.md § On-demand querying, not Snapshot-resident like
	// beadsSrc/openspecSrc below — they are never Run() on a goroutine and
	// never publish onto the bus; EnableMemoryTimeline drives them directly
	// from Root's own selection-change dispatch (task 3.2).
	memoryTimeline := ui.NewMemoryTimelinePane()
	root.EnableMemoryTimeline(
		ctx,
		timeline.NewBeadsHistorySource(cwd),
		timeline.NewOpenSpecArchiveSource(cwd),
		timeline.NewMemoryHistorySource(cwd),
		memoryTimeline,
	)

	// wavetui-flair (task [3.2]): FlairManager and ToastOverlay are wired in
	// via the additive decorator model in flair_wiring.go — root itself
	// never gains a flair dependency. cfg.Flair defaults to
	// {Enabled:false}, the literal disabled-equals-identical path (see
	// config.FlairConfig's doc comment), so an operator who never opts in
	// gets byte-identical behavior to before this task.
	flairMgr := flair.NewFlairManager(cfg.Flair)
	toastOverlay := flair.NewToastOverlay(os.Stderr, os.Environ())
	model := newRootWithFlair(root, flairMgr, toastOverlay)

	program := tea.NewProgram(model, tea.WithContext(ctx))

	// The Store is the single writer, and this is its only subscriber. Every
	// event that mutates Store state also pushes a fresh Snapshot to the
	// running Program — this is the only place a Snapshot is ever sent; the
	// root model's Update (see internal/ui/root.go) never watches or polls
	// anything on its own, per design.md § Architecture.
	b.Subscribe(ctx, func(ev bus.Event) {
		st.Apply(ev)
		program.Send(ui.SnapshotMsg{Snapshot: st.Snapshot()})
	})

	beadsSrc := sources.NewBeadsSource(cwd, b)
	openspecSrc := sources.NewOpenSpecSource(cwd, b, cfg)

	// Both sources run on their own ctx-derived goroutine; ctx cancellation
	// is what stops them — see task 2.4.
	errCh := make(chan error, 2)
	go func() { errCh <- beadsSrc.Run(ctx) }()
	go func() { errCh <- openspecSrc.Run(ctx) }()

	_, runErr := program.Run()

	// The Program has exited — either the user quit it directly, or ctx was
	// already cancelled by a real signal. Either way, cancel now so the
	// sources' own ctx-derived goroutines unwind too (task 2.4's contract),
	// then wait for them to actually finish before returning.
	cancel()

	var firstErr error
	for range 2 {
		if err := <-errCh; err != nil && !isExpectedShutdown(err) && firstErr == nil {
			firstErr = err
		}
	}
	if runErr != nil && !isExpectedShutdown(runErr) && firstErr == nil {
		firstErr = fmt.Errorf("wavetui: program: %w", runErr)
	}
	return firstErr
}
