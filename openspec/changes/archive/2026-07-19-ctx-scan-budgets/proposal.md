---
order: 0718d
---

# Proposal: ctx-scan Shared Rubric Module

## Change ID
`ctx-scan-budgets`

## Summary
Extract `docs/context-budget-rubric.md`'s Table A (rows A1–A14, GREEN/AMBER/RED bands, H/G/R
source tags) and Part 0 band-derivation rules into a single `apps/ctx-scan/src/rubric.ts`
constants module, annotate every scanned `Node` with its violated/passing bands, and ship
`ctx-scan audit --json` emitting the rubric's §E-R1 JSON contract — so `ctx-scan` and any future
`context-budget-audit` (not yet built in `~/dev/cc` — verified absent) share one source of truth
for what GREEN/AMBER/RED means, rather than the scanner defining its own thresholds.

## Context
- Extends: `apps/ctx-scan/src/model.ts`, `apps/ctx-scan/src/assembly.ts` (both from
  `ctx-scan-assembly`)
- Related: `docs/context-budget-rubric.md` (this repo — the actual rubric source, confirmed
  present at `installfest/docs/context-budget-rubric.md`), `telemetry-hook-bytes` (`~/dev/cc`,
  external — its A14-readiness note is what this proposal's audit path ultimately consumes)
- depends on: `ctx-scan-assembly`
- touches: `apps/ctx-scan/src/rubric.ts`, `apps/ctx-scan/src/audit.ts`,
  `apps/ctx-scan/test/fixtures/rubric/**`

## Motivation
`docs/context-budget-rubric.md` already defines a deterministic, sourced (H/G/R-tagged) budget
model — Table A's 14 rows, Part 0's three band-derivation rules, and §E-R1's exact JSON
contract for a data-producer script. That rubric doc's own §E-R1 names
`scripts/bin/context-budget-audit` as the intended producer, but that script does not exist yet
anywhere in `~/dev` (confirmed via a fleet-wide search before drafting this proposal). Rather
than `ctx-scan` inventing a second, parallel set of thresholds (named mistake #7 in the `ctx-scan`
roadmap — "Second rubric"), this proposal makes the rubric doc's own constants and JSON contract
the single implementation, shipped as `ctx-scan audit`, which either satisfies §E-R1 directly or
becomes what a future `context-budget-audit` wraps as a thin consumer.

## Requirements

### Requirement: Rubric constants extracted verbatim from the source document
`apps/ctx-scan/src/rubric.ts` SHALL define Table A rows A1–A14 as a single constants block, each
carrying its GREEN/AMBER/RED thresholds, its H/G/R source tag, and — for every H/G-tagged row —
the source document cited in `docs/context-budget-rubric.md`'s Sources line (code.claude.com
docs, agentskills.io/specification). R-tagged rows carry the anchor rationale from the rubric
doc's Table A "Limit + tag" column instead of an external source.

### Requirement: Band derivation rules are implemented per Part 0
The module SHALL implement all three band-derivation rules from `docs/context-budget-rubric.md`
Part 0: Rule 1 (hard limits: GREEN ≤ 0.8·L, AMBER 0.8·L–L, RED > L), Rule 2 (guidance values:
GREEN ≤ V, AMBER V–2·V, RED > 2·V), and Rule 3 (repo-set values using the documented anchor,
flagged for ratification).

### Requirement: Every Node is annotated with its rubric bands
Every scanned `Node` SHALL gain a `bands` array of `{rule, band, measured, limit}` entries — one
per applicable Table A row — computed by applying the extracted constants and derivation rules
to that `Node`'s `effective_chars`/`raw_chars`/class from `ctx-scan-assembly`'s output.

### Requirement: ctx-scan audit emits the §E-R1 JSON contract
`ctx-scan audit --json` SHALL emit `{"rows":[{"id":"A1","surface":...,"measured":...,
"budget":...,"band":"GREEN"|"AMBER"|"RED","source":"H"|"G"|"R"}],"error":null}` for every Table A
row, exit 0 always (an `error` key on partial failure, never a non-zero exit), and complete in
under 200ms warm with no network access — matching §E-R1's data-producer contract exactly.

### Requirement: One constants module drives both scanner and future ratchet consumer
Changing a single constant in `rubric.ts` SHALL change both `ctx-scan`'s own band-annotated
output and `ctx-scan audit`'s JSON contract output identically — proven by a test that mutates
one threshold and asserts both surfaces reflect the change.

## Scope
- **IN**: the `rubric.ts` constants module (Table A rows, Part 0 derivation rules), `Node` band
  annotation, the `ctx-scan audit --json` subcommand and its §E-R1 contract compliance.
- **OUT**: scaffolding `scripts/bin/context-budget-audit` itself (out of scope — a separate
  initiative per `docs/context-budget-rubric.md` §E-R1, or superseded by this proposal's `audit`
  subcommand); Table B (operational thresholds — timeouts/caps, not context budget; no `ctx-scan`
  consumer needs it); the cc-repo Tier 3 ratchet wiring from §E-R2 (that is cc's own ratchet lane,
  not this repo's concern — this proposal only proves the module is consumable by such a row).

## Testing
| Affected seam | Unit task | E2E task |
|----------------|-----------|----------|
| Rubric constants block | `[4.1]` every constant asserted against its documented source tag | N/A |
| Band derivation rules | `[4.2]` seeded fixtures at exactly the GREEN/AMBER/RED boundary for each rule type | N/A |
| `Node` band annotation | `[4.3]` fixture `Node` set, hand-computed expected bands | N/A |
| `ctx-scan audit --json` contract | `[4.4]` schema assertion against the §E-R1 shape | `[4.5]` reproduces `docs/context-budget-rubric.md` Part 2's cc scorecard exactly when run against `~/dev/cc` |
| Single-source-of-truth guarantee | `[4.6]` mutate one constant, assert both scanner output and audit output change identically | N/A |

## Impact
| Area | Change |
|------|--------|
| `apps/ctx-scan/src/rubric.ts` | New — the shared constants module |
| `apps/ctx-scan/src/audit.ts` | New — `ctx-scan audit` subcommand, §E-R1 contract |
| `apps/ctx-scan/src/model.ts` | `Node.bands` now populated for real (was an empty placeholder since `ctx-scan-core`) |

## Risks
| Risk | Mitigation |
|------|-----------|
| Rubric doc constants drift from this module over time (doc edited, module not) | `[4.5]`'s exact-reproduction test against the live cc repo catches drift the moment either side changes |
| R-tagged (repo-set, unratified) values get silently treated as hard cliffs | `source: "R"` is carried through to every output surface so a consumer can distinguish ratified-hard from anchor-derived-pending |
| `ctx-scan audit` scope creep into re-deriving thresholds instead of reading the doc's values | `[4.1]`'s per-constant source-tag assertion is the guard — a constant with no traceable doc citation fails the test |
