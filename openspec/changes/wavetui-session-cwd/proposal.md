---
order: 0722a
---

# Proposal: wavetui-session-cwd — surface session CWD in SessionsPane, clarify its scope

## Change ID
`wavetui-session-cwd`

## Summary
Add the linked session's own transcript `cwd` field to `store.SessionLink`, render it in
`SessionsPane`'s per-row output, and change the pane's static "Sessions" header to state its
scope explicitly. Closes an observability gap found live during a `/explore` session: `cwd` is
already the fallback session-linkage match key (`SessionLinker.matchCWDTimestamp`), but it is
never rendered anywhere, so an operator has no way to visually verify why a session did or did
not link to a claimed item.

## Context
- depends on: `wavetui-sessions`
- **Depends on `wavetui-sessions`** (spec dir `openspec/changes/archive/2026-07-21-wavetui-sessions/`,
  capability epic `if-tkva`, feature bead `if-yufp`): this proposal is a small additive
  modification of `store.SessionLink`, `internal/sources/transcript.go`'s
  `toStoreSessionLink`, and `internal/ui/sessionspane.go`'s `renderRow` — all three shipped by
  `wavetui-sessions` (archived 2026-07-21). Hard dependency: the fields/functions this proposal
  extends do not exist without it.
- **Found live, not speculative**: this proposal originates from a `/explore` session
  (2026-07-22) investigating why `apps/wavetui`'s "Sessions" panel showed "No linked Claude Code
  sessions." while two live installfest Claude Code sessions were confirmed running (`ps`+
  `/proc/*/cwd`+`tmux list-panes` cross-reference: PIDs 849073 and 2231180, both
  `cwd=/home/nyaptor/dev/personal/installfest`). Root cause: the panel is correctly scoped to
  the *currently selected item* (`if-7cce`, capability epic `[CAPABILITY] unsorted` — epics are
  never claimed/dispatched directly per `rules/BEADS.md`), so an empty panel there was expected
  behavior, not a bug. Investigating further surfaced the real, actionable gap this proposal
  fixes: `cwd` is already the algorithm's fallback match key
  (`internal/sources/session_link.go:246`, `matchCWDTimestamp`) but is dropped before reaching
  `store.SessionLink` — `internal/sources/transcript.go`'s `toStoreSessionLink` (line ~799) never
  copies `agg.cwd` into the returned `*store.SessionLink`, even though `agg.cwd` is already
  available there (used two calls earlier in `resolveLink`).
- **Reuse-not-rebuild (Reader Gate)**: `agg.cwd` (the `sessionAgg` struct's own field, populated
  from each transcript line's `cwd`) already exists and is already read by `resolveLink`
  (`transcript.go:766`) to build the `SessionTranscript` passed into `SessionLinker.Link`. This
  proposal reads that same already-populated field a second time in `toStoreSessionLink` — no new
  parsing, no new transcript field, no new source.
- Capability Preflight (Phase 1): not applicable — local dev tool, no hosting/deploy component,
  same precedent `wavetui-core`/`wavetui-sessions` both cite. The generic preflight's
  `VERCEL_TOKEN: missing` result is a known mismatch for this non-T3, no-deploy Go repo (see
  `rules/PATTERNS.md`'s documented `stack: t3`-as-placeholder precedent for installfest specs) —
  skipped per that same standing precedent rather than re-litigated with a fresh
  `AskUserQuestion`.
- touches: `apps/wavetui/internal/store/store.go`, `apps/wavetui/internal/sources/transcript.go`,
  `apps/wavetui/internal/sources/transcript_test.go`, `apps/wavetui/internal/ui/sessionspane.go`

## Motivation
`cwd` is already the exact signal an operator needs to debug "why didn't my session link to this
item" — it's the fallback match key the linking algorithm itself uses — but it never reaches the
screen. An operator staring at an unexpectedly-empty (or unexpectedly-linked) `SessionsPane` row
today has no way to see what cwd the live session actually reported, or compare it against the
claimed item's repo path, without reading Go source. Separately, the pane's bare "Sessions"
header reads as "every live Claude Code session" when it actually means "sessions linked to the
currently selected item" — exactly the ambiguity that produced a real operator's "we have two
live sessions, why does this say none?" confusion during the `/explore` session that found this
gap.

## Requirements

### Requirement: A claimed item is linked to its session via an /apply reference or cwd+timestamp proximity
See `specs/wavetui/spec.md`.

### Requirement: SessionsPane renders the pane map, context gauges, and zombie badges as a focus-ring pane
See `specs/wavetui/spec.md`.

## Scope
- **IN**: additive `CWD string` field on `store.SessionLink`; populating it in
  `transcript.go`'s `toStoreSessionLink` from the already-available `agg.cwd`; rendering it in
  `SessionsPane.renderRow`; changing the pane's header text to state its per-selected-item scope.
- **OUT**: a new unscoped "all live Claude Code sessions in this repo" diagnostic view
  (deliberately not adopted — the `/explore` session's own trade-off analysis found the
  CWD-rendering fix alone already gives the needed certainty without a new pane/view); any change
  to the linkage algorithm itself (`SessionLinker.matchCWDTimestamp` is correct and unmodified —
  this proposal is rendering-only); `internal/sources/tmux.go` (unrelated to this gap).

## Done Means
- Operator can see, in `SessionsPane`, the `cwd` a linked session's own transcript reported,
  next to its existing pane/context%/zombie fields.
- Operator reading the "Sessions" pane header can tell at a glance that it lists sessions linked
  to the currently selected item, not every live Claude Code session in the repo.

## Testing
| Affected seam | Unit task | E2E task |
|----------------|-----------|----------|
| `internal/store/store.go` additive `CWD` field | `[4.1]` | `[4.3]` |
| `internal/sources/transcript.go` (`toStoreSessionLink` threads `agg.cwd` through) | `[4.2]` | `[4.3]` |
| `internal/ui/sessionspane.go` (`renderRow` cwd line, header scope text) | N/A — no pure-function render logic beyond Go compile | `[4.3]` (pty runtime verification) |

## Impact
| Area | Change |
|------|--------|
| `apps/wavetui/internal/store/store.go` | Additive `SessionLink.CWD string` field only — no existing field renamed, removed, or re-typed |
| `apps/wavetui/internal/sources/transcript.go` | One-line addition to `toStoreSessionLink`: copy `agg.cwd` into the returned struct |
| `apps/wavetui/internal/ui/sessionspane.go` | `renderRow` gains a cwd segment; `View`'s static header string changes |
| `openspec/specs/wavetui/spec.md` | Two existing Requirements get `## MODIFIED Requirements` deltas (full text pasted, new scenarios added) |
| Existing repo files outside the four `- touches:` paths | None modified |

## Risks
| Risk | Mitigation |
|------|-----------|
| A long cwd path could overflow `SessionsPane`'s fixed-width row layout (`defaultSessionsWidth = 96`) | Render the path's basename or a truncated form consistent with `_cwd_basename`-style truncation already used elsewhere in this codebase family (cc-tmux's own `_cwd_basename`); exact truncation behavior is a UI-batch implementation detail, not a design change |
| `store.SessionLink`'s doc comment says "do not add fields here without a corresponding design.md update, same convention as Item's own doc comment" | This proposal's own `design.md` (if authored) or this proposal.md's Context/Impact sections serve as that update — same precedent `wavetui-sessions`' own API-batch addendum set when it added `Errors`/`ExecutorLaneFlag` without a full design.md rewrite |
| None of the four touched files are touched by any other in-flight proposal | Confirmed via `openspec-status --json` (zero non-archived proposals) at Phase 2.3 of this feature's own authoring — no wave-conflict risk |
