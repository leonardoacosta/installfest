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
// into any tmux invocation — see tmux.go's Dispatch. The OTHER half of
// design.md's "calling validateDispatchTarget on every resolved pane/item
// ID before any dispatch call" invariant — item.ID itself — is enforced
// here in Dispatch (below), once, before either concrete Dispatcher runs;
// Resolver does not need a second, redundant validation pass on a pane ID
// it never itself extracts, but it IS the right (and only) place to
// validate item.ID, since it is the single object every real dispatch call
// funnels through.
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
//
// validateDispatchTarget(item.ID) runs FIRST, before either Dispatcher is
// ever invoked. This is the choke point design.md § Dispatch-boundary
// validation actually requires: Resolver is the one object every real
// dispatch path funnels through (QueuePane's Start action never holds a
// concrete *TmuxDispatcher/*ClipboardDispatcher directly — cmd/wavetui/
// main.go always wires a *Resolver via SetDispatcher), so validating here
// once covers both the Tmux and the Clipboard branch without duplicating
// the check at each concrete Dispatcher. This was previously a gap:
// idShapeRe/validateDispatchTarget existed and was unit-tested but had no
// real caller, so a non-id-shaped item.ID (e.g. "plans:foo" — the shape
// internal/sources/openspec.go's parseFlatMarkdownDir mints for plans/
// advisor-plans items, colon and all) would have sailed straight into
// TmuxDispatcher/ClipboardDispatcher untouched. Neither concrete Dispatcher
// happens to splice item.ID into a shell argv today (TmuxDispatcher keys off
// item.Session.PaneID/scored candidate IDs, validated separately by
// validateTmuxPaneID; ClipboardDispatcher never reads item at all) — but the
// boundary contract in design.md is unconditional ("Applied to item.ID...
// refusing to cross the dispatch boundary"), not contingent on today's call
// graph never needing it, so it is enforced here regardless.
func (r *Resolver) Dispatch(ctx context.Context, item store.Item, promptText string) error {
	if err := validateDispatchTarget(item.ID); err != nil {
		return err
	}
	err := r.Tmux.Dispatch(ctx, item, promptText)
	if errors.Is(err, ErrNoDispatchTarget) {
		return r.Clipboard.Dispatch(ctx, item, promptText)
	}
	return err
}

// DispatchToPane implements TargetOverrideDispatcher (dispatch.go) — the
// confirm action of QueuePane's inline *AmbiguousTargetError picker
// (if-7mq2.1). It bypasses TmuxDispatcher.resolveTarget's own scoring
// entirely: the caller already knows exactly which pane the operator
// picked from a prior tie's Candidates, so re-running the plain Dispatch
// unchanged would re-score the identical candidate set and reproduce the
// same tie rather than act on the operator's choice.
//
// item.ID is still validated via validateDispatchTarget first — the same
// choke point Dispatch itself runs before either concrete Dispatcher is
// ever invoked — so an explicit-target confirm can't become a second,
// unvalidated path across the dispatch boundary. paneID's own shape is
// validated by TmuxDispatcher.Dispatch itself (via validateTmuxPaneID),
// exactly as it already does for a linked-session PaneID: setting
// targeted.Session.PaneID here makes TmuxDispatcher.resolveTarget take its
// existing "already resolved" branch (tmux.go's point 1) rather than a new
// code path, so no additional validation call is needed here. Clipboard is
// deliberately never consulted — an explicit pane pick is, by definition,
// not the "no tmux target at all" case ErrNoDispatchTarget names, so
// Resolver.Dispatch's own fallback rule does not apply.
func (r *Resolver) DispatchToPane(ctx context.Context, item store.Item, promptText, paneID string) error {
	if err := validateDispatchTarget(item.ID); err != nil {
		return err
	}
	targeted := item
	targeted.Session = &store.SessionLink{PaneID: paneID}
	return r.Tmux.Dispatch(ctx, targeted, promptText)
}
