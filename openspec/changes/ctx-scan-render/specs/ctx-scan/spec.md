# ctx-scan Specification

## ADDED Requirements

### Requirement: Render produces a self-contained static HTML file
`ctx-scan render` SHALL produce a single HTML file with all data, CSS, and JS inlined, requiring
no network access to view.

#### Scenario: Airplane test

- **GIVEN** a rendered HTML file and network access disabled
- **WHEN** the file is opened in a browser
- **THEN** the page SHALL render correctly with no failed network requests

#### Scenario: No external asset references

- **WHEN** the rendered HTML is inspected
- **THEN** it SHALL contain zero external `<script src=`, `<link href=`, `fetch(`, or
  `XMLHttpRequest` references

### Requirement: Four-level drill-down navigation
The renderer SHALL support drilling from a fleet leaderboard (level 0) into a project stacked
bar (level 1), into a class proportional bar (level 2), into a document detail view (level 3).

#### Scenario: Full drill-down path

- **GIVEN** a rendered fleet view
- **WHEN** a user clicks a project, then a class segment, then a document
- **THEN** each click SHALL navigate to the corresponding next level
- **AND** the document detail view SHALL show its violation header, tier, origin, and
  raw-vs-effective sizes

### Requirement: Band-colored violations are visible without drilling in
Every RED-banded row present in the underlying scan data SHALL be visibly distinguishable as RED
at the level where it first appears, without requiring the user to open the document detail.

#### Scenario: Known cc REDs render RED

- **GIVEN** a render of the `~/dev/cc` scan
- **WHEN** the project-level view is displayed
- **THEN** the A1 (listing total), A7/A8 (always-loaded chain), and A4 (oversized bodies)
  violations SHALL be visibly RED

### Requirement: Trim plan proposes without editing
The trim-plan panel SHALL compute a greedy remediation ordering ranked by tokens recovered, with
a running total, and SHALL NOT write to or modify any source file.

#### Scenario: Trim plan reaches GREEN

- **GIVEN** a project with a known token overage on rubric row A1
- **WHEN** the trim plan is computed
- **THEN** its running total of proposed trims SHALL be at least the A1 overage amount
- **AND** no file on disk SHALL be modified as a result of viewing the trim plan
