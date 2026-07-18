---
stack: t3
---
<!-- beads:epic:if-wfel -->
<!-- beads:feature:if-52m5 -->

# Implementation Tasks

## DB Batch

- [ ] [1.1] Define the render-time view model in `src/render/view-model.ts`: derives the [beads:if-taql]
  4-level drill-down structure (fleet → project → class → document) plus toggle state from the
  `Fleet` JSON document produced by `ctx-scan scan`, without mutating the source data.

## API Batch

- [ ] [2.1] Implement `src/render/level0-fleet.ts`: leaderboard bar generation, global-baseline [beads:if-l8o0]
  vs project-delta sub-stack split.
  - depends on: 1.1
- [ ] [2.2] Implement `src/render/level1-project.ts`: stacked bar per class, the four toggles [beads:if-qoaa]
  (post-compaction, include-T2, predicted-drops, calibrated-constant marking).
  - depends on: 1.1
- [ ] [2.3] Implement `src/render/level2-class.ts`: proportional per-document bar, band-colored [beads:if-qw32]
  borders sourced from `ctx-scan-budgets`' `Node.bands`.
  - depends on: 1.1
- [ ] [2.4] Implement `src/render/level3-document.ts`: rendered content, violation header text [beads:if-fwhb]
  assembly (e.g. `"A2 listing entry 1,610/1,536 [H]"`), tier/origin/truncation/raw-vs-effective
  display.
  - depends on: 1.1
- [ ] [2.5] Implement `src/render/trim-plan.ts`: greedy remediation plan over RED/AMBER rows, [beads:if-x8yr]
  ranked by tokens-recovered-per-change, with a running total; read-only — no file-write code
  path.
  - depends on: 1.1

## UI Batch

- [ ] [3.1] Implement `src/render.ts` and the `ctx-scan render [--project <name>|--fleet] [beads:if-9h3k]
  [-o <path>]` command: assemble all levels + trim panel into one self-contained HTML file
  (inline CSS/JS, inline JSON data, no CDN dependency), write to `-o` or a default path.
  - depends on: 2.1, 2.2, 2.3, 2.4, 2.5

## E2E Batch

- [ ] [4.1] Grep the rendered HTML output for any external `<script src=` / `<link href=` / [beads:if-elkk]
  `fetch(`/`XMLHttpRequest` reference; assert zero matches. Open the file with network access
  disabled and assert it still renders (the airplane test).
  - depends on: 3.1
- [ ] [4.2] Fixture data exercising all 4 levels; assert each level's DOM structure matches the [beads:if-j112]
  expected shape and that clicking through levels 0→1→2→3 works.
  - depends on: 3.1
- [ ] [4.3] Fixture with known RED/AMBER/GREEN nodes; assert each renders with the correct band [beads:if-kj1x]
  color.
  - depends on: 2.3
- [ ] [4.4] Fixture RED set with a known overage; assert the trim plan's running total reaches [beads:if-mpmv]
  or exceeds the overage.
  - depends on: 2.5
- [ ] [4.5] Render `ctx-scan render --project cc -o /tmp/ctx-scan-cc.html` (or fleet equivalent) [beads:if-l21g]
  for the real `~/dev/cc` scan; open it, click through all 4 drill levels, and confirm the three
  known REDs (A1, A7/A8, A4×6 per `docs/context-budget-rubric.md` Part 2) render visibly RED and
  the trim panel's plan sums to at least the A1 overage.
  - depends on: 3.1
- [ ] [4.6] Minimal empty-findings fixture (a tiny project with zero rubric violations); assert [beads:if-yv0e]
  the renderer produces valid output with no error, at every level.
  - depends on: 3.1
- [ ] [4.7] `tsc --noEmit` and `bun test` both green. [beads:if-lefh]
  - depends on: 3.1
