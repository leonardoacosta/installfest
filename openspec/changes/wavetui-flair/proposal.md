---
order: 0720g
---

# Proposal: wavetui-flair — animation and reward layer for wavetui

## Change ID
`wavetui-flair`

## Summary
Add a `FlairManager` sub-model to `apps/wavetui/` that renders purely-decorative, meaning-gated
animation (row flashes, spring-in toasts, particle bursts, full-screen celebration overlays) as a
reaction to real Store-snapshot diffs — a bead closing, a proposal archiving, a blocker clearing —
never as ambient idle motion. Flair sits entirely behind a config flag and a global calm-mode
toggle, auto-degrades on non-truecolor terminals, and is provably inert when disabled: the app
renders byte-for-byte identical output minus animation frames.

## Context
- depends on: `wavetui-core`
- **Depends on `wavetui-core`** (spec dir `openspec/changes/wavetui-core/`, capability epic
  `if-tkva`, feature bead `if-3g1c`): this proposal reacts to `wavetui-core`'s `Snapshot` values
  (the same immutable value the root model already receives via `Program.Send()`) and implements
  the same `Pane`-adjacent conventions for its overlay compositor. Soft dependency only — this
  proposal is independently authored/reviewable, but `wavetui-core` must land first in any apply
  wave since every flair trigger reads fields (`Item.Blocker`, `Item` presence by ID) that only
  exist once `wavetui-core`'s `Store`/`Snapshot` types exist.
- **This is proposal 6 of 7 in the wavetui dependency spine**: `wavetui-core` ->
  {`wavetui-sessions`, `wavetui-dispatch`, `wavetui-memory-timeline`, `wavetui-flair`} ->
  {`wavetui-decision-lanes`, `wavetui-daemon`}. Resolves to the SAME capability epic (`if-tkva`,
  `[CAPABILITY] wavetui`) — verified at Phase 4 Gate 4.1 below, not re-created. Unlike
  `wavetui-dispatch` and `wavetui-decision-lanes`, this proposal declares only ONE hard dependency
  (`wavetui-core`) — it is enriched by `wavetui-sessions` (session-state-driven presence sprites)
  and `wavetui-dispatch` (wave-progress triggers) as ADDITIVE, non-blocking enhancements, per the
  Scope and design.md § Alternatives sections below.
- **Stack-risk verification (bubbletea v2 / lipgloss v2 stability) — resolved via direct evidence,
  not assumed**: queried the Go module proxy directly (`proxy.golang.org`, the authoritative
  release-tag source, not a changelog or blog post) during this authoring session.
  `github.com/charmbracelet/bubbletea/v2` is at `v2.0.8` and `github.com/charmbracelet/lipgloss/v2`
  is at `v2.0.5` — both past their `v2.0.0` final tag (preceded by `alpha`/`beta`/`rc` tags that
  are no longer the latest), i.e. **stable, not beta**, contradicting the exploration-session
  premise that seeded this proposal. Full version list and exact commands are in `design.md` §
  Verified dependency versions. Given this, no blocking first-task verification gate is needed —
  see design.md § Alternatives for why the recommendation is still a MIXED v1/v2 adoption (v2
  scoped to this proposal's own package only) rather than a wholesale module upgrade.
- **Reuse-not-rebuild (Reader Gate)**: `wavetui-decision-lanes`'s design.md already establishes
  that a blocker clearing is observable purely from `wavetui-core`'s existing `Item.Blocker` field
  transitioning to `nil` on the next `Snapshot` — no new event type, no dependency on
  `wavetui-decision-lanes` shipping. This proposal reuses that same field for its
  blocker-resolved trigger rather than waiting on `wavetui-decision-lanes` or inventing a second
  state machine. Verified by reading `wavetui-decision-lanes/design.md`'s "badge-clear signal"
  section during authoring (see design.md § Alternatives for the citation).
- Capability Preflight (Phase 1): not applicable, matching `wavetui-core`'s precedent — local dev
  tool, no hosting/deploy component. Both greenfield probes returned empty as expected for a
  dotfiles repo; skipped per explicit operator authorization (same call `wavetui-core` made).
- touches: `apps/wavetui/internal/flair/manager.go`, `apps/wavetui/internal/flair/manager_test.go`, `apps/wavetui/internal/flair/effects.go`, `apps/wavetui/internal/flair/effects_test.go`, `apps/wavetui/internal/flair/overlay.go`, `apps/wavetui/internal/flair/overlay_test.go`, `apps/wavetui/internal/flair/reward.go`, `apps/wavetui/internal/flair/reward_test.go`, `apps/wavetui/internal/flair/sprite.go`, `apps/wavetui/internal/flair/sprite_test.go`, `apps/wavetui/internal/ui/queuepane.go`, `apps/wavetui/internal/config/config.go`, `apps/wavetui/cmd/wavetui/main.go`, `apps/wavetui/go.mod`

## Motivation
`wavetui-core` renders a live, correct queue — but a state change (a bead closing, a wave
finishing) currently looks identical to a no-op render tick: same layout, same colors, no signal
that anything meaningful just happened. Operators lose the small dopamine hit of "I closed
something" that makes a long triage session feel like progress rather than a chore. `wavetui-flair`
closes that gap with an animation layer whose entire design discipline is that motion always means
something changed — never ambient, never decorative-for-its-own-sake — so the reward signal stays
meaningful instead of becoming visual noise that gets tuned out.

## Requirements

### Requirement: FlairManager derives animation triggers by diffing consecutive Store snapshots, never by intercepting the event bus
See `specs/wavetui/spec.md`.

### Requirement: The tick loop runs only while an animation is live and idles at zero cost otherwise
See `specs/wavetui/spec.md`.

### Requirement: Disabling flair produces byte-for-byte-identical rendering minus animation frames
See `specs/wavetui/spec.md`.

### Requirement: Flair auto-degrades on non-truecolor terminals and respects a global calm-mode toggle
See `specs/wavetui/spec.md`.

### Requirement: Negative-attention effects are reserved exclusively for genuinely bad events
See `specs/wavetui/spec.md`.

### Requirement: Victory-recap numbers are computed from the same Snapshot data the queue already renders, never a separate accounting path
See `specs/wavetui/spec.md`.

### Requirement: Session-linked presence sprites and wave-progress triggers degrade gracefully when their source data is absent
See `specs/wavetui/spec.md`.

## Scope
- **IN**: `FlairManager` (snapshot-diff trigger derivation, tick-loop lifecycle, config +
  calm-mode gating, truecolor detection/degradation), the core event->effect map (bead closed, item
  appeared, proposal archived, blocker resolved, negative/zombie events), row-scoped highlight
  application to `QueuePane`, a lipgloss v2 Layer/Canvas-based overlay compositor for toast/
  full-screen effects scoped to this proposal's own package, the disabled-equals-identical
  invariant and its test.
- **OUT**: reward mechanics beyond a later, explicitly lower-priority task batch (streak counter,
  combo multiplier, all-clear state, variable-reward celebration variant — real but MVP-optional,
  see tasks.md E2E-adjacent later batch), wave-progress-bar triggers (blocked on `wavetui-dispatch`
  shipping a progress event — not built until then, degrades to absent), session-linked presence
  sprites beyond a conditional, skip-if-absent task (blocked on `wavetui-sessions`' `Item.Session`
  field existing at execution time), zoom/pane-geometry interpolation (blocked on `wavetui-core`
  shipping a scope/zoom model it does not have yet), any change to `wavetui-core`'s `Store`/
  `Snapshot` schema (flair reads existing fields only, never adds or mutates them), dispatch/
  wave-file format (`wavetui-dispatch`), decision-lanes UI (`wavetui-decision-lanes`), daemon/
  background mode (`wavetui-daemon`), memory-timeline pane (`wavetui-memory-timeline`).

## Done Means
- A bead closing on disk produces a visible row-flash animation in the queue pane within one
  render cycle, with no data lag introduced by the animation itself.
- Disabling flair via config produces byte-for-byte-identical (minus animation) rendering compared
  to flair enabled, for the same Store snapshot.
- On a non-truecolor terminal, the app auto-degrades flair rendering rather than rendering broken
  or wrong colors.
- Calm mode replaces animated sprites with static glyphs while preserving the underlying state
  signal (e.g. still shows "blocked on you" as a static icon, just without the animation).

## Testing
| Affected seam | Unit task | E2E task |
|----------------|-----------|----------|
| `internal/flair/manager.go` (snapshot diffing, tick-loop lifecycle, config/calm-mode gating) | `[4.1]` | `[4.6]` |
| `internal/flair/effects.go` (event->effect map, harmonica spring/particle primitives, negative-effect reservation) | `[4.2]` | `[4.6]` |
| `internal/flair/overlay.go` (lipgloss v2 Layer/Canvas compositor, truecolor detection/degradation) | `[4.3]` | `[4.6]` |
| `internal/flair/reward.go` (streak/combo/all-clear/variable-reward — later batch) | `[4.4]` | `[4.6]` |
| `internal/flair/sprite.go` (conditional presence sprite, skip-if-`Item.Session`-absent) | `[4.5]` | `[4.6]` |
| `internal/ui/queuepane.go` highlight application | N/A — no pure-function render logic beyond Go compile | `[4.6]` (pty runtime verification) |

## Impact
| Area | Change |
|------|--------|
| `apps/wavetui/internal/flair/` | New package: `manager.go`, `effects.go`, `overlay.go`, `reward.go`, `sprite.go` plus tests |
| `apps/wavetui/internal/ui/queuepane.go` | Additive: accepts an optional per-item highlight map from `FlairManager`, renders unchanged when the map is empty/nil |
| `apps/wavetui/internal/config/config.go` | Additive: `Flair.Enabled` and `Flair.CalmMode` fields |
| `apps/wavetui/cmd/wavetui/main.go` | Wire `FlairManager` into the root model's `Update`/`View` loop, append-only |
| `apps/wavetui/go.mod` | New requires: `github.com/charmbracelet/lipgloss/v2 v2.0.5`, `github.com/charmbracelet/harmonica v0.2.0`, `github.com/lucasb-eyer/go-colorful v1.4.0` (bubbletea stays v1 — see design.md § Alternatives) |
| `openspec/specs/wavetui/` | `## ADDED Requirements` merged into the existing capability spec (parent now exists from earlier-landed sibling proposals) |
| Existing repo files outside `apps/wavetui/` | None modified |

## Risks
| Risk | Mitigation |
|------|-----------|
| `- touches:` includes `apps/wavetui/internal/ui/queuepane.go`, which `wavetui-core` creates and `wavetui-decision-lanes` also extends (lane badges) | Both other edits are additive (new fields/params, no removal or signature change to existing exported methods). `wave-plan-build`'s conflict matrix will serialize all three specs into different waves; ordering only matters in that `wavetui-core` must land first (hard dependency), `wavetui-decision-lanes` has no ordering requirement relative to this proposal since neither reads the other's addition. |
| Bubbletea v2 / lipgloss v2 beta-risk premise from the originating exploration turned out to be stale (both are stable final releases) | Documented with direct proxy.golang.org evidence in Context and design.md rather than silently building against an unverified premise or over-cautiously blocking on a redundant verification task. |
| `wavetui-sessions`' `Item.Session` field (needed for presence sprites) may not exist yet when this proposal's implementation tasks execute, since the two proposals are siblings with no ordering dependency | `sprite.go`'s task is written as conditional: check for the field's existence at execution time (`go build` against the then-current `wavetui-core` `Item` struct), implement if present, otherwise skip and file a follow-up bead — never a hard `- depends on: wavetui-sessions` line, per the explicit non-blocking instruction for this feature. |
| No Go-aware `/apply` engineer agent exists in the fleet yet | Same `stack: t3` workaround `wavetui-core` used, for the same reason (`commands/apply/references/stacks.md`'s crosswalk has no `go-cli` value yet) — cited here rather than re-derived; tracked in `wavetui-core`'s own Risks table, not duplicated as a new tracked risk. |
| A future contributor could be tempted to add ambient/idle motion (a subtle pulsing border, a breathing cursor) since it "looks nice" | Explicitly prohibited in Requirements below and design.md's invariants — the disabled-equals-identical test (`[4.6]`) only proves animation is state-gated at the render-diff level; the ambient-motion ban itself is a design-review discipline, not something a unit test alone can enforce, so it is called out here for future reviewers. |
