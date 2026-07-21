// Command wavetui is the entrypoint for the wavetui terminal dashboard.
//
// This batch (API) wires the bus, Store, and both sources end-to-end with
// graceful shutdown on SIGINT/SIGTERM (tasks.md [2.4]) — the bubbletea
// Program and root model land in the UI batch (tasks.md [3.1]-[3.4]), so
// `run` currently just keeps both sources alive until ctx is cancelled.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/leonardoacosta/installfest/apps/wavetui/internal/bus"
	"github.com/leonardoacosta/installfest/apps/wavetui/internal/config"
	"github.com/leonardoacosta/installfest/apps/wavetui/internal/sources"
	"github.com/leonardoacosta/installfest/apps/wavetui/internal/store"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "wavetui:", err)
		os.Exit(1)
	}
}

// run is the real entrypoint body, separated from main so it can be tested
// and so main stays a thin os.Exit wrapper.
func run(ctx context.Context) error {
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
	// The Store is the single writer; this is its only subscriber. The UI
	// batch adds a second subscriber (or reads st.Snapshot() from the
	// bubbletea goroutine) — see design.md § Architecture.
	b.Subscribe(ctx, func(ev bus.Event) { st.Apply(ev) })

	beadsSrc := sources.NewBeadsSource(cwd, b)
	openspecSrc := sources.NewOpenSpecSource(cwd, b, cfg)

	// Both sources run on their own ctx-derived goroutine; ctx cancellation
	// (SIGINT/SIGTERM, via signal.NotifyContext above) is what stops them —
	// see task 2.4.
	errCh := make(chan error, 2)
	go func() { errCh <- beadsSrc.Run(ctx) }()
	go func() { errCh <- openspecSrc.Run(ctx) }()

	<-ctx.Done()

	var firstErr error
	for range 2 {
		if err := <-errCh; err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
