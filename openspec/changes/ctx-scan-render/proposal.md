---
order: 0718e
---

# Proposal: ctx-scan Drill-Down Visual

## Change ID
`ctx-scan-render`

## Summary
`ctx-scan render` emits a fully self-contained static HTML file rendering the fleet as a
4-level drill-down: fleet leaderboard → per-project stacked bar → per-class proportional bar →
per-document detail, all band-colored from `ctx-scan-budgets`' rubric annotations, plus a
trim-plan panel proposing a greedy remediation order for any RED rows.

## Context
- Extends: consumes `ctx-scan-assembly`'s populated `Node` data and `ctx-scan-budgets`' band
  annotations; no source changes to either.
- depends on: `ctx-scan-budgets`
- touches: `apps/ctx-scan/src/render.ts`, `apps/ctx-scan/src/render/**` (HTML/CSS/JS template
  assembly), `apps/ctx-scan/test/fixtures/render/**`

## Motivation
Everything through `ctx-scan-budgets` produces correct JSON, but a JSON document is not how a
human decides what to trim first. The roadmap's named mistake #9 ("served-app drift") is the
reason this ships as one static HTML file rather than a Next/Express dev server — the visual
must be viewable by opening a file, matching this repo's own no-server convention for
one-off tooling output (`apps/daily-brief`'s TUI is interactive but local-process; nothing in
this repo runs a background web server for a CLI tool's output). Mistake #12 ("scope creep into
remediation") is why the trim-plan panel only *proposes* — it never edits a file; any real fix
still ships through `/feature` like everything else in this repo.

## Requirements

### Requirement: Self-contained static HTML output
`ctx-scan render [--project <name>|--fleet] [-o <path>]` SHALL produce a single HTML file with
all data and JS/CSS inlined, requiring no network access and no build step to view — opening the
file directly in a browser SHALL render correctly (the "airplane test").

### Requirement: Level 0 — fleet leaderboard
The `--fleet` view SHALL render one leaderboard bar per project, showing always-loaded (T1)
tokens with the global-baseline sub-stack rendered visually distinct from the project-specific
delta.

### Requirement: Level 1 — project stacked bar with toggles
The `--project <name>` view SHALL render a stacked bar with one color-coded segment per class
(the 13-class taxonomy from `ctx-scan-core`), plus three toggles: post-compaction view (only
chain-reloaded T1 surfaces plus skills under the carry-forward model), include-T2 (trigger-paid
surfaces, rendered hatched to distinguish from always-paid), and predicted-drops (dimmed segments
for listing entries `ctx-scan-assembly` predicted would be dropped). Constants (system prompt,
system tools) SHALL be visually marked as calibrated rather than measured.

### Requirement: Level 2 — class proportional bar
Clicking a class segment SHALL drill into a proportional bar of that class's individual
documents, each bordered in its `ctx-scan-budgets` band color (GREEN/AMBER/RED).

### Requirement: Level 3 — document detail
Clicking a document SHALL show its rendered content, a violation header listing every band it
breaks (e.g. "A2 listing entry 1,610/1,536 [H]; A4 body 599/500"), its tier, origin
(global/project), any truncation applied, and both raw and effective sizes.

### Requirement: Trim-plan panel
Given the set of RED/AMBER rows for the focused project, the system SHALL render a greedy
remediation plan ranked by tokens-recovered-per-change, with a running total that reaches GREEN,
without editing any source file — the panel only proposes.

## Scope
- **IN**: the 4-level drill-down HTML renderer, all four toggles, the trim-plan panel, the
  airplane-test self-containment guarantee.
- **OUT**: the references explorer (`ctx-scan-refs`), watch mode / drift sparklines
  (`ctx-scan-watch`), any code that applies a trim suggestion automatically.

## Testing
| Affected seam | Unit task | E2E task |
|----------------|-----------|----------|
| Self-containment | `[4.1]` grep the output HTML for any non-`data:`/inline external reference | `[4.1]` open the file with network disabled, assert it renders |
| 4-level drill-down | `[4.2]` fixture data, assert each level's DOM structure | `[4.5]` open the cc-audit render, click through all 4 levels |
| Band coloring | `[4.3]` fixture with known RED/AMBER/GREEN nodes | `[4.5]` assert the three known cc REDs (A1, A7/A8, A4×6) render visibly RED |
| Trim-plan arithmetic | `[4.4]` fixture RED set, assert running total ≥ overage | `[4.5]` cc-audit trim panel sums to ≥ the A1 overage |
| Empty-fixture render | `[4.6]` minimal fixture with zero findings | N/A |

## Impact
| Area | Change |
|------|--------|
| `apps/ctx-scan/src/render.ts` | New — HTML assembly entrypoint |
| `apps/ctx-scan/src/render/` | New — level-0..3 templates, toggle logic, trim-plan generator |

## Risks
| Risk | Mitigation |
|------|-----------|
| A CDN dependency creeps in for charting/interactivity | `[4.1]`'s grep-for-external-refs test blocks any non-inlined asset |
| Trim-plan panel silently becomes a "fix it for me" button | Explicit non-goal in Scope OUT; no file-write code path exists in `render.ts` by construction |
| Large fleets produce an unusably large single HTML file | Flagged as a known tradeoff, not solved defensively this proposal — `~/dev` fleet size at authoring time (a handful of projects) does not require pagination; revisit if fleet size grows materially |
