# ctx-scan Specification

## ADDED Requirements

### Requirement: Rubric constants are extracted verbatim with source tags
The system SHALL define Table A rows A1–A14 from `docs/context-budget-rubric.md` as a single
constants block, each carrying its GREEN/AMBER/RED thresholds and its H/G/R source tag.

#### Scenario: Every constant carries a traceable source

- **WHEN** `rubric.ts`'s constants block is inspected
- **THEN** every row SHALL carry a `source` of `"H"`, `"G"`, or `"R"`
- **AND** every `"H"`/`"G"`-tagged row SHALL carry a citation traceable to the rubric doc's
  Sources line
- **AND** every `"R"`-tagged row SHALL carry the anchor rationale from the rubric doc's Table A

### Requirement: Band derivation follows the rubric's three rules
The system SHALL classify a measurement into GREEN/AMBER/RED using Rule 1 for hard limits
(GREEN ≤ 0.8·L, AMBER 0.8·L–L, RED > L), Rule 2 for guidance values (GREEN ≤ V, AMBER V–2·V, RED
> 2·V), and Rule 3 for repo-set anchor values (same shape as Rule 1, tagged `R`).

#### Scenario: Hard-limit row at the AMBER boundary

- **GIVEN** a Rule-1 row with limit L
- **WHEN** a measurement equals exactly 0.8·L
- **THEN** the band SHALL be GREEN

#### Scenario: Hard-limit row just past the limit

- **GIVEN** a Rule-1 row with limit L
- **WHEN** a measurement equals L + 1
- **THEN** the band SHALL be RED

### Requirement: Scanned nodes are annotated with rubric bands
Every scanned `Node` SHALL carry a `bands` array populated by applying the applicable Table A
row(s) to that node's measured values.

#### Scenario: Oversized skill body node is annotated RED

- **GIVEN** a `Node` for a SKILL.md body of 600 lines (over the A4 500-line RED threshold)
- **WHEN** band annotation runs
- **THEN** the node's `bands` array SHALL contain an entry with `rule: "A4"`, `band: "RED"`

### Requirement: ctx-scan audit emits the rubric's data-producer contract
`ctx-scan audit --json` SHALL emit one row per Table A rubric item in the shape
`{"id","surface","measured","budget","band","source"}`, wrapped in `{"rows":[...],"error":null}`,
exit 0 in all cases, completing in under 200ms warm with no network access.

#### Scenario: Successful audit run

- **WHEN** `ctx-scan audit --json` runs against a fully-scanned project
- **THEN** the output SHALL contain one row per Table A rubric item
- **AND** `error` SHALL be `null`
- **AND** the process SHALL exit 0

#### Scenario: Partial failure still exits 0

- **GIVEN** one rubric row's underlying measurement cannot be computed
- **WHEN** `ctx-scan audit --json` runs
- **THEN** the output SHALL populate the `error` key describing the failure
- **AND** the process SHALL still exit 0

### Requirement: Rubric doc reproduction against the live cc repo
Running `ctx-scan audit --json` against `~/dev/cc` SHALL reproduce
`docs/context-budget-rubric.md` Part 2's recorded scorecard bands exactly for every row present
in that scorecard.

#### Scenario: cc repo scorecard reproduction

- **WHEN** `ctx-scan audit --json` runs against `~/dev/cc`
- **THEN** row A1 SHALL report `RED`
- **AND** row A7 SHALL report `RED`
- **AND** row A9 SHALL report `GREEN`
