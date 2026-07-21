// Command wavetui is the entrypoint for the wavetui terminal dashboard.
//
// This is a scaffold stub for the wavetui-core DB batch — it wires nothing
// yet. The bubbletea Program, root model, and source dispatch land in later
// batches/waves (see openspec/changes/wavetui-core/tasks.md § API/UI Batch
// and the design.md architecture diagram).
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
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
	<-ctx.Done()
	return nil
}
