# ctx-scan Specification

## ADDED Requirements

### Requirement: File-change triggers a new snapshot
`ctx-scan watch` SHALL re-scan a project when a file inside it changes and append a timestamped
snapshot to `~/.ctx-scan/history.jsonl`.

#### Scenario: Editing CLAUDE.md produces a new snapshot

- **GIVEN** `ctx-scan watch` is running
- **WHEN** a project's CLAUDE.md is edited
- **THEN** a new snapshot for that project SHALL appear in `history.jsonl` within 5 seconds

### Requirement: Snapshot history is append-only
`~/.ctx-scan/history.jsonl` SHALL only ever gain new lines; existing snapshot lines SHALL never
be modified or removed by `watch`.

#### Scenario: Prior snapshots are preserved

- **GIVEN** an existing `history.jsonl` with N snapshots
- **WHEN** a new snapshot is appended
- **THEN** the file SHALL contain N+1 snapshots
- **AND** the original N snapshots SHALL be byte-identical to before

### Requirement: Diff reports only band transitions
`ctx-scan diff <a> <b>` SHALL report every rubric row whose band value differs between snapshot
`a` and snapshot `b`, and SHALL report nothing for rows whose band is unchanged even if the
underlying measured value shifted.

#### Scenario: Seeded band transition is reported

- **GIVEN** two snapshots where rubric row A4 was GREEN in `a` and RED in `b`
- **WHEN** `ctx-scan diff a b` runs
- **THEN** the output SHALL contain exactly one entry: `A4: GREEN → RED`

#### Scenario: Same-band value jitter produces no diff output

- **GIVEN** two snapshots where a row's measured value changed slightly but its band stayed
  GREEN in both
- **WHEN** `ctx-scan diff a b` runs
- **THEN** that row SHALL NOT appear in the diff output
