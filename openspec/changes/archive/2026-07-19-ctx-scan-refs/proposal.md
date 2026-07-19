---
order: 0718f
---

# Proposal: ctx-scan References Explorer

## Change ID
`ctx-scan-refs`

## Summary
Add a T3-tier (on-demand, never-in-the-primary-bar) "references shelf" view to `ctx-scan`:
every `references/` file, un-imported rules file, and memory topic file, grouped by owning
skill/command/agent, annotated with reachability (routed-from-line-N vs orphan), size, ToC
presence (rubric A5), and reference-nesting depth (rubric A6).

## Context
- Extends: consumes `ctx-scan-assembly`'s discovery output and `ctx-scan-budgets`' A5/A6 band
  data; no changes to either module's source.
- depends on: `ctx-scan-render`
- touches: `apps/ctx-scan/src/refs.ts`, `apps/ctx-scan/test/fixtures/refs/**`

## Motivation
T3 (on-demand) surfaces are deliberately excluded from `ctx-scan`'s primary token bar (per C1 —
they're free until read), but "free until read" is not the same as "irrelevant" — an orphaned
reference file nobody's SKILL.md links to is dead weight regardless of its always-loaded cost,
and `docs/context-budget-rubric.md`'s A5 (ToC presence) and A6 (nesting depth) rows already
measure exactly this shelf. This proposal is where that existing rubric data becomes browsable
instead of just a count in the audit JSON.

## Requirements

### Requirement: Per-project references shelf
For the focused project, the system SHALL list every `references/` file, every rules file not
reached via `@import`, and every memory topic file, grouped by the skill/command/agent that owns
it.

### Requirement: Reachability annotation
Each listed file SHALL be annotated either with its reaching citation (e.g. "routed from
SKILL.md line 42") or as `orphan` when no owning document references it.

### Requirement: Size, ToC, and nesting annotation
Each listed file SHALL carry its size, ToC-presence status (rubric A5 band), and
reference-nesting-depth status (rubric A6 band), sourced from `ctx-scan-budgets`' existing band
computation — not re-derived.

### Requirement: Per-skill focused view
Focusing on a single skill SHALL scope the shelf view to only that skill's references, answering
"what could this skill pull in, and what of it is currently unreachable."

### Requirement: Every listed reference opens in the detail view
Every entry in the shelf SHALL be click-openable into `ctx-scan-render`'s level-3 document
detail view.

## Scope
- **IN**: the references shelf (project-scoped and skill-scoped), reachability/orphan detection,
  ToC/nesting annotation display, detail-view linkage.
- **OUT**: watch mode / drift tracking (`ctx-scan-watch`); any change to how `references/` files
  are scored (that's `ctx-scan-budgets`' A5/A6 already-shipped logic, reused here read-only).

## Testing
| Affected seam | Unit task | E2E task |
|----------------|-----------|----------|
| Reachability detection | `[4.1]` fixture with a linked and an orphaned reference file | N/A |
| ToC/nesting annotation reuse | `[4.2]` assert shelf entries match `ctx-scan-budgets`' A5/A6 bands exactly, no re-derivation | N/A |
| Per-skill scoping | `[4.3]` multi-skill fixture, assert scoped view excludes other skills' refs | N/A |
| cc-audit real-data pass | N/A | `[4.4]` orphan files (if any) flagged; the 79 known no-ToC files appear AMBER; every entry click-opens |

## Impact
| Area | Change |
|------|--------|
| `apps/ctx-scan/src/refs.ts` | New — reachability graph + shelf assembly |
| `apps/ctx-scan/src/render/level3-document.ts` | Reused unchanged as the detail-view target for shelf entries (from `ctx-scan-render`) |

## Risks
| Risk | Mitigation |
|------|-----------|
| Reachability detection produces false-orphan positives on a reference cited only via prose (not a markdown link) | `[4.1]`'s fixture includes both a markdown-link citation and a prose-only citation to establish the detection boundary explicitly, rather than silently guessing |
| Re-deriving A5/A6 instead of reusing `ctx-scan-budgets`' computation | `[4.2]` is an explicit equality assertion against the shared module's output, not a parallel implementation |
