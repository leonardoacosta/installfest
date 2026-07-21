// See dispatch.go for the package-level Dispatcher contract. This file
// implements Resolver — see openspec/changes/wavetui-dispatch/tasks.md
// [2.4] and design.md § Target resolution.
package dispatch

import (
	"context"
	"errors"

	"github.com/leonardoacosta/installfest/apps/wavetui/internal/store"
)

// Resolver is the single entry point a later batch's QueuePane Start
// action calls (tasks.md [3.1]: "dispatches the highlighted item via
// Resolver+Dispatcher in one action") — see design.md § Target resolution.
//
// Resolver itself picks WHICH Dispatcher handles a given item; picking
// WHICH tmux pane is TmuxDispatcher's own concern (it resolves its target
// internally from item — see tmux.go's resolveTarget — since Dispatcher's
// signature is deliberately narrow and carries no separate target
// parameter). Every pane ID TmuxDispatcher resolves is validated via
// validateTmuxPaneID immediately inside its own Dispatch, before crossing
// into any tmux invocation — see tmux.go's Dispatch — which is where
// design.md's "calling validateDispatchTarget on every resolved pane/item
// ID before any dispatch call" invariant is actually enforced; Resolver
// does not need a second, redundant validation pass on a pane ID it never
// itself extracts.
//
// Tmux always runs first. Resolver intervenes only when Tmux reports
// ErrNoDispatchTarget (design.md § Target resolution point 3: zero
// candidates, or no $TMUX session at all), falling back to Clipboard. Any
// other error from Tmux — an *AmbiguousTargetError tie, an
// ErrPaneInCopyMode/ErrSessionStreaming refusal, or a genuine tmux command
// failure once a target was actually found — is returned to the caller
// unchanged. Clipboard fallback is deliberately narrow to the "no target
// at all" case per design.md; it is never a catch-all for every tmux
// failure (a refused or ambiguous dispatch must surface as such, not
// silently degrade to a clipboard copy the operator did not ask for).
type Resolver struct {
	// Tmux and Clipboard are typed as the Dispatcher interface (not the
	// concrete *TmuxDispatcher/*ClipboardDispatcher) so tests can inject
	// fakes without touching a real tmux server or terminal — see
	// resolver_test.go (tasks.md [4.1]/[4.2] E2E batch).
	Tmux      Dispatcher
	Clipboard Dispatcher
}

// NewResolver constructs a Resolver from a concrete TmuxDispatcher and
// ClipboardDispatcher — the production wiring a later batch's
// cmd/wavetui/main.go performs (tasks.md [3.4]).
func NewResolver(tmux *TmuxDispatcher, clipboard *ClipboardDispatcher) *Resolver {
	return &Resolver{Tmux: tmux, Clipboard: clipboard}
}

// Dispatch implements Dispatcher, so Resolver itself is the single object
// a later batch's QueuePane needs to hold. See the type doc comment for the
// exact fallback contract.
func (r *Resolver) Dispatch(ctx context.Context, item store.Item, promptText string) error {
	err := r.Tmux.Dispatch(ctx, item, promptText)
	if errors.Is(err, ErrNoDispatchTarget) {
		return r.Clipboard.Dispatch(ctx, item, promptText)
	}
	return err
}
