// Package dispatch implements wavetui's Dispatcher boundary — the
// mechanism that delivers a rendered prompt to a target destination (a
// tmux pane, the system clipboard, and later a headless subprocess). See
// openspec/changes/wavetui-dispatch/design.md § Dispatcher interface.
//
// This file scaffolds the interface, the sentinel refusal errors, and the
// dispatch-boundary id validator per tasks.md [1.2]. TmuxDispatcher and
// ClipboardDispatcher (the concrete implementations) land in the API
// batch — see design.md's architecture diagram.
package dispatch

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/leonardoacosta/installfest/apps/wavetui/internal/store"
)

// Dispatcher delivers promptText for item to whatever destination a
// concrete implementation resolves to (a tmux pane, the clipboard, or —
// out of scope here, wavetui-daemon's concern — a headless subprocess).
// One method, deliberately narrow: nothing in this signature assumes a
// tmux pane exists, which is what lets a future HeadlessDispatcher
// implement it without a breaking change. See design.md § Dispatcher
// interface.
type Dispatcher interface {
	Dispatch(ctx context.Context, item store.Item, promptText string) error
}

// ErrPaneInCopyMode is returned when a TmuxDispatcher's target pane is in
// copy-mode ("#{pane_in_mode}" == "1") — a paste into that pane would be
// silently eaten, so the dispatch is refused rather than attempted. See
// design.md § TmuxDispatcher primitive choice.
var ErrPaneInCopyMode = errors.New("dispatch target pane is in copy-mode, refusing to paste")

// ErrSessionStreaming is returned when the item's linked session (see
// store.SessionLink) is actively mid-turn — dispatching into a busy
// session risks interleaving a new prompt with in-flight output. See
// design.md § Mid-turn safety: the caller (QueuePane) renders this as
// "queued — session busy" with no automatic retry.
var ErrSessionStreaming = errors.New("dispatch target session is actively streaming, refusing to dispatch")

// idShapeRe matches an id-shaped string — a bead ID, an openspec change
// slug, or a tmux pane ID (e.g. "%12"). See design.md § Dispatch-boundary
// validation.
//
// Deliberately does not allow "%" itself in the charclass even though
// tmux pane IDs are conventionally "%<digits>" — a caller passing a raw
// pane ID through this validator will fail it today. This mirrors
// design.md's own regex verbatim (`^[A-Za-z0-9_-]+$`); tightening it to
// admit pane-ID shapes is a decision for whichever API-batch task first
// calls validateDispatchTarget on a real pane ID, not something to
// silently pre-empt here.
var idShapeRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// validateDispatchTarget refuses to let a non-id-shaped string cross the
// dispatch boundary into a tmux/shell invocation. Applied to item.ID and
// item.Session.PaneID immediately before either is used to build a
// tmux/shell command — never to promptText, which is free-form prose by
// design and is delivered exclusively through the paste-buffer/OSC52
// payload, never through shell argument interpolation. See design.md §
// Dispatch-boundary validation.
func validateDispatchTarget(id string) error {
	if !idShapeRe.MatchString(id) {
		return fmt.Errorf("dispatch target %q is not id-shaped, refusing to cross the dispatch boundary", id)
	}
	return nil
}
