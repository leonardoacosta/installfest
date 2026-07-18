# ctx-scan Specification

## ADDED Requirements

### Requirement: References shelf lists on-demand surfaces grouped by owner
The system SHALL list every `references/` file, un-imported rules file, and memory topic file
for the focused project, grouped by the skill/command/agent that owns it.

#### Scenario: Shelf groups by owner

- **GIVEN** two skills each with their own `references/` directory
- **WHEN** the shelf view is generated
- **THEN** each skill's reference files SHALL appear under that skill's own group

### Requirement: Orphan detection
Each shelf entry SHALL be annotated with its reaching citation when an owning document links to
it via markdown, or `orphan` when no such link exists.

#### Scenario: Cited reference reports its citation

- **GIVEN** a `references/foo.md` linked from `SKILL.md` line 42
- **WHEN** the shelf view is generated
- **THEN** the entry for `foo.md` SHALL report "routed from SKILL.md line 42"

#### Scenario: Uncited reference reports orphan

- **GIVEN** a `references/bar.md` with no link from any owning document
- **WHEN** the shelf view is generated
- **THEN** the entry for `bar.md` SHALL report `orphan`

### Requirement: Shelf reuses budget-band data without re-derivation
Each shelf entry's ToC-presence (A5) and nesting-depth (A6) bands SHALL be sourced from
`ctx-scan-budgets`' existing `Node.bands` computation.

#### Scenario: Shelf band matches audit band

- **GIVEN** a reference file with a known A5 band from `ctx-scan audit --json`
- **WHEN** the shelf view is generated for the same file
- **THEN** the shelf entry's A5 band SHALL match the audit output exactly

### Requirement: Skill-scoped shelf view
The system SHALL support scoping the shelf to a single named skill's own references.

#### Scenario: Skill scoping excludes other skills

- **GIVEN** a fleet with multiple skills each carrying references
- **WHEN** the shelf is scoped to one skill
- **THEN** only that skill's reference entries SHALL appear
