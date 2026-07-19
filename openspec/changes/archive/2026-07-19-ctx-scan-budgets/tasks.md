---
stack: t3
---
<!-- beads:epic:if-wfel -->
<!-- beads:feature:if-xvzw -->

# Implementation Tasks

## DB Batch

- [x] [1.1] Write `src/rubric.ts`: a single constants block for Table A rows A1–A14 from [beads:if-khk9]
  `docs/context-budget-rubric.md`, each entry carrying `{id, surface, greenMax, amberMax, limit,
  source: "H"|"G"|"R", sourceCitation}` sourced verbatim from the doc's Table A columns
  (Measurement / GREEN / AMBER / RED / Limit+tag columns).
- [x] [1.2] Extend `src/model.ts`'s `Node.bands` type (currently an empty placeholder from [beads:if-njnb]
  `ctx-scan-core`) to the real shape: `{rule: string, band: "GREEN"|"AMBER"|"RED", measured:
  number, limit: number}[]`.

## API Batch

- [x] [2.1] Implement Part 0's three band-derivation rules as pure functions in `src/rubric.ts`: [beads:if-e64w]
  Rule 1 (hard limits, GREEN ≤ 0.8·L / AMBER 0.8·L–L / RED > L), Rule 2 (guidance values, GREEN ≤
  V / AMBER V–2·V / RED > 2·V), Rule 3 (repo-set anchor values, same shape as Rule 1 but tagged
  `source: "R"`).
  - depends on: 1.1
- [x] [2.2] Implement `Node` band annotation: for every scanned `Node`, apply the applicable [beads:if-9b5o]
  Table A row(s) (matched by `cls`/measurement type) via `[2.1]`'s derivation functions, and
  populate `Node.bands`.
  - depends on: 2.1, 1.2
- [x] [2.3] Implement `src/audit.ts` and the `ctx-scan audit --json` subcommand: emit [beads:if-gqvm]
  `{"rows":[{"id":"A1","surface":...,"measured":...,"budget":...,"band":"GREEN"|"AMBER"|"RED",
  "source":"H"|"G"|"R"}],"error":null}` for every Table A row against the current scan, exit 0
  always (partial failures surface as an `error` key, never a non-zero exit), warm runtime under
  200ms, no network access.
  - depends on: 2.2

## UI Batch

- [x] [3.1] Wire `ctx-scan audit [--json]` as a top-level commander.js command alongside `scan` [beads:if-fq0g]
  and `calibrate`.
  - depends on: 2.3

## E2E Batch

- [x] [4.1] Assert every constant in `src/rubric.ts` carries a traceable source citation matching [beads:if-hc1m]
  `docs/context-budget-rubric.md`'s stated sources (code.claude.com docs pages, or the specific
  anchor rationale for R-tagged rows); a constant with no citation fails the test.
  - depends on: 1.1
- [x] [4.2] Seeded boundary fixtures at exactly the GREEN/AMBER and AMBER/RED transition points [beads:if-ug8h]
  for a Rule-1 row, a Rule-2 row, and a Rule-3 row; assert each lands in the correct band.
  - depends on: 2.1
- [x] [4.3] Fixture `Node` set with hand-computed expected bands across multiple classes; assert [beads:if-byk3]
  `[2.2]`'s annotation matches exactly.
  - depends on: 2.2
- [x] [4.4] Schema assertion that `ctx-scan audit --json`'s output matches the §E-R1 contract [beads:if-y2zm]
  shape exactly, including the `error: null` success case and an `error`-populated partial
  failure case.
  - depends on: 2.3
- [x] [4.5] Run `ctx-scan audit --json` against `~/dev/cc` and assert it reproduces [beads:if-ujtc]
  `docs/context-budget-rubric.md` Part 2's scorecard exactly — same bands for A1 (RED,
  ~5.8× budget), A3 (RED, 1 over), A4 (RED, 6 over), A7 (RED, ~1.6×), A9 (GREEN), A13 (GREEN),
  and the AMBER rows (A5, A11, A12).
  - depends on: 3.1
- [x] [4.6] Mutate one constant in `src/rubric.ts` (e.g. A2's 1,536-char limit) and assert both [beads:if-150o]
  `ctx-scan scan`'s band-annotated `Node` output and `ctx-scan audit --json`'s row output reflect
  the change identically — proves the single-source-of-truth guarantee.
  - depends on: 2.2, 2.3
- [x] [4.7] `tsc --noEmit` and `bun test` both green. [beads:if-hojq]
  - depends on: 3.1
