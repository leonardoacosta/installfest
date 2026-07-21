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

// TargetOverrideDispatcher is an OPTIONAL capability a Dispatcher may
// additionally implement: dispatch item to an explicitly chosen tmux pane,
// bypassing Resolver's own scoring entirely. It exists for exactly one
// caller — QueuePane's inline *AmbiguousTargetError picker (if-7mq2.1, see
// tmux.go's AmbiguousTargetError doc comment and design.md § Target
// resolution point 2's "AskUserQuestion-shaped inline pane list" framing):
// once the operator has arrow-keyed to the candidate they actually want,
// re-invoking the plain Dispatcher.Dispatch would re-run the identical
// same-window/same-session/other scoring against the identical candidate
// set and reproduce the exact same tie — there is no way to "just pick
// one" through the narrow Dispatcher interface alone. Resolver implements
// this (see resolver.go); a Dispatcher that doesn't (e.g. a bare test
// fake, or any future Dispatcher that never grows tmux-pane targeting)
// simply cannot resolve a tie through the picker — QueuePane type-asserts
// for this interface and renders a "no override support" failure badge in
// that case rather than silently no-op'ing or panicking.
type TargetOverrideDispatcher interface {
	Dispatcher
	DispatchToPane(ctx context.Context, item store.Item, promptText, paneID string) error
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

// idShapeRe matches an id-shaped string — a bead ID or an openspec change
// slug. See design.md § Dispatch-boundary validation.
//
// RESOLVED (API batch, tasks.md [2.1]-[2.4]): the DB batch flagged that
// this regex, taken verbatim from design.md, does not match a tmux pane ID
// like "%12" ("%" is not in the charclass) even though design.md's prose
// claimed it should apply to both id-spaces. Now that tasks.md [2.4]'s
// Resolver is the first caller to actually validate a real pane ID, the
// question had to be settled: WIDEN this charclass to admit "%", or give
// pane IDs their OWN, separately-scoped pattern.
//
// Decision: separate pattern (see paneIDShapeRe/validateTmuxPaneID below),
// not a widened charclass. Bead IDs and openspec slugs are one id-space
// (this codebase's canonical grammar, matching rules/TOOLING.md's
// BEADS_ID_RE and internal/sources/session_link.go's applyDispatchRe);
// tmux pane IDs are a DIFFERENT, tmux-owned id-space with their own fixed
// grammar ("%" followed by one or more digits, nothing else — never a
// bead ID, never a proposal slug, never anything user/agent-authored).
// Widening idShapeRe to admit "%" would loosen the validator every
// item.ID/bead-ID call site relies on, for a character no bead or proposal
// slug will ever legitimately contain — exactly the kind of blanket
// loosening the Reader Gate's "tighten to the minimum, not the union"
// principle warns against for a boundary validator whose whole job is
// keeping non-id-shaped (attacker-controllable) text out of a shell
// invocation. A second, narrowly-scoped regex that matches tmux's actual
// pane-ID grammar exactly (`^%[0-9]+$`) protects the pane-ID call sites
// (tmux.go's Dispatch/Switch) without touching this one at all.
var idShapeRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// paneIDShapeRe matches a tmux pane ID exactly as tmux itself formats one —
// "%" followed by one or more digits (e.g. "%12"). See idShapeRe's doc
// comment above for why this is a separate pattern rather than a widened
// idShapeRe.
var paneIDShapeRe = regexp.MustCompile(`^%[0-9]+$`)

// validateDispatchTarget refuses to let a non-id-shaped string cross the
// dispatch boundary into a tmux/shell invocation. Applies to bead IDs and
// openspec change slugs (item.ID) — never to a tmux pane ID, which is a
// different id-space validated by validateTmuxPaneID instead. Never
// applied to promptText, which is free-form prose by design and is
// delivered exclusively through the paste-buffer/OSC52 payload, never
// through shell argument interpolation. See design.md § Dispatch-boundary
// validation and idShapeRe's doc comment above.
func validateDispatchTarget(id string) error {
	if !idShapeRe.MatchString(id) {
		return fmt.Errorf("dispatch target %q is not id-shaped, refusing to cross the dispatch boundary", id)
	}
	return nil
}

// validateTmuxPaneID refuses to let a non-pane-ID-shaped string cross the
// dispatch boundary into a tmux invocation. Applied to every pane ID
// TmuxDispatcher resolves (item.Session.PaneID, or a scored candidate's ID
// from `cc-tmux conductor list --json`) immediately before it is used as a
// `-t <target>` argument to tmux/cc-tmux — see tmux.go's Dispatch/Switch.
// See idShapeRe's doc comment above for why this is deliberately a
// separate validator from validateDispatchTarget rather than a widened
// shared regex.
func validateTmuxPaneID(id string) error {
	if !paneIDShapeRe.MatchString(id) {
		return fmt.Errorf("dispatch target %q is not pane-ID-shaped, refusing to cross the dispatch boundary", id)
	}
	return nil
}
