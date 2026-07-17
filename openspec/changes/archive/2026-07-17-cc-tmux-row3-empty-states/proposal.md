---
order: 0717b
---

# Proposal: cc-tmux row-3 (op:/bd:) collapsed empty states

## Change ID
`cc-tmux-row3-empty-states`

## Summary
`render_beads_bar`'s left side (the `op:`/`bd:` roadmap-pulse counts on cc-tmux's third status
row, `status-format[2]`) currently renders one of two unhelpful things when both openspec and
beads counts land in the same state: a noisy `op: 0o 0ip 0ua | bd: 0o 0r 0b` when everything is
genuinely caught up, or a bare blank string when the roadmap-pulse cache hasn't resolved at all.
This proposal adds two collapsed left-side messages — `All caught up` and `Not available` —
covering those two states respectively.

## Context
- Extends: `apps/cc-tmux/src/cc_tmux/render.py` (`render_beads_bar`)
- Related: `openspec/changes/archive/2026-07-12-cc-tmux-row3-openspec-beads-format`,
  `openspec/changes/archive/2026-07-17-cc-tmux-row3-tiered-colors`
- touches: `apps/cc-tmux/src/cc_tmux/render.py`, `apps/cc-tmux/src/cc_tmux/testing.py`

## Motivation
Leo asked (via `/explore`) for row 2/3's `op:`/`bd:` segment to show something meaningful when
both halves are "empty" instead of the current noisy-zero or blank-string behavior. Two distinct
states already exist in the parsed data (`_parse_roadmap_pulse_counts` in `cli.py` hands
`render_beads_bar` either a fully-present triple — including real zeros — or a fully-`None`
triple per half, never a partial mix), so this is a pure `render.py` display change: no new data
production, no new fetch, no new cache format.

Confirmed via `/explore` + `AskUserQuestion` (2026-07-17): two-case mapping, not a single
collapsed string —
1. Both halves present, all six counts `0` → `All caught up`.
2. Both halves fully absent (`None`) → `Not available`, independent of whether the right-side
   account-identity segment renders.

**Direct spec conflict, explicitly overridden by Leo's confirmed choice above**: the current
`cc-tmux` capability spec's Requirement "A dedicated tmux status row surfaces open/ready beads
and proposals" § Scenario "nothing available renders nothing" states the row must show *"no
error, no placeholder text"* when both halves are absent. This proposal's `Not available` string
is exactly the placeholder text that scenario prohibits — the MODIFIED delta in
`specs/cc-tmux/spec.md` rewrites that scenario (and the adjacent "no roadmap-pulse cache, but an
active account resolves" scenario, whose Then-clause also changes) to the new contract.

**Known adjacent drift, explicitly out of scope**: `spec.md`'s existing Requirement text still
describes a "Phase 0 (counts) / Phase 1 (next)" wall-clock cycling contract, but the current
`render_beads_bar`/`_build_beads_bar` code has no phase-cycling logic at all — `cli.py`'s own
comment says this swap-cycle behavior "was reversed by cc-tmux-row3-tiered-colors," and that
archived spec apparently never updated this Requirement's prose to match. This is a pre-existing
spec-vs-code drift this proposal does not fix (the MODIFIED delta below preserves the
phase-cycling prose verbatim per the "paste the full existing requirement" convention, since
untangling it is unrelated to the empty-state ask) — tracked separately as `if-bqw.8` instead
of expanding this change's scope.

## Requirements

### Requirement: All-zero op/bd counts collapse to a single "All caught up" message
When `render_beads_bar`'s six count arguments (`openspec_open`, `openspec_in_progress`,
`openspec_ua`, `beads_open`, `beads_ready`, `beads_blocked`) are ALL non-`None` AND ALL equal to
`0`, the function SHALL render the left side as the literal string `All caught up` (wrapped in
the same DIM styling used for healthy-tier counts elsewhere in this function), replacing the
two-segment `op: 0o 0ip 0ua | bd: 0o 0r 0b` output. This does not apply when only one half is
all-zero and the other half has real non-zero counts — that half's existing single-segment
rendering is unaffected.

### Requirement: Fully-absent op/bd counts collapse to a "Not available" message
When all six count arguments are `None` (both halves fully absent — no cache, or fully
unparseable), `render_beads_bar` SHALL render the left side as the literal string
`Not available` (DIM) instead of an empty string. This applies independently of
`account_label`/the right-side account-identity segment — `Not available` renders on the left
whether or not an active nexus-agent credential resolves on the right, replacing the prior
"row is entirely empty only when both counts AND account are absent" contract.

## Scope
- **IN**: the two collapsed-state strings in `render_beads_bar`, updated pytest-equivalent
  assertions in `testing.py` (including the existing `all None -> ''` assertion at
  `testing.py:2101`, which must change to assert `Not available`), and the corresponding
  `specs/cc-tmux/spec.md` MODIFIED delta.
- **OUT**: the roadmap-pulse data-fetch path (covered by the separate, already in-flight
  `cc-tmux-nx-agent-roadmap-pulse` proposal — no dependency, no shared touched files); fixing the
  pre-existing Phase-0/Phase-1 spec-vs-code prose drift described above; any change to the
  right-side account-identity segment's own rendering or click behavior; any new glyph/icon for
  either collapsed state (plain DIM text, consistent with the account segment's existing
  DIM-text styling — no new symbol invented).

## Testing
| Affected seam | Unit task | E2E task |
|----------------|-----------|----------|
| `render_beads_bar` all-zero collapse | `[2.1]` | N/A — pure status-bar string composition, no user-facing browser flow |
| `render_beads_bar` all-None collapse | `[2.2]` | N/A — pure status-bar string composition, no user-facing browser flow |
| `render_beads_bar` partial-half non-collapse (existing behavior unchanged) | `[2.3]` | N/A |

## Impact
| Area | Change |
|------|--------|
| `apps/cc-tmux/src/cc_tmux/render.py` | `render_beads_bar` gains two early-branch string substitutions for the left side |
| `apps/cc-tmux/src/cc_tmux/testing.py` | One existing assertion changes (`all None -> ''` becomes `all None -> 'Not available'`), three new assertions added |
| `openspec/specs/cc-tmux/spec.md` | Requirement "A dedicated tmux status row surfaces open/ready beads and proposals" gains a "Collapsed states" paragraph and 3 new/rewritten scenarios |

## Risks
| Risk | Mitigation |
|------|-----------|
| `Not available` string could be mistaken by a future reader for a genuine error state rather than "no cache yet" | DIM styling (not YELLOW/RED) keeps it visually consistent with the "healthy/no-signal" tier already used elsewhere on this row |
| A future roadmap-pulse producer change that legitimately emits partial per-half data (breaking the "always fully present or fully None per half" invariant this proposal relies on) would need re-verification | `_parse_roadmap_pulse_counts`'s docstring already documents this as a deliberate fail-open contract; no change proposed to it here, but flagged for whoever touches that parser next |
