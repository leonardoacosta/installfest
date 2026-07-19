# ctx-scan Specification

## Purpose
TBD - created by archiving change ctx-scan-core. Update Purpose after archive.
## Requirements
### Requirement: Commander.js CLI skeleton
The system SHALL provide a `ctx-scan` binary built on commander.js exposing a `scan` subcommand
that accepts `--root <path>` (default `~/dev`) and `--json <path>` (default: stdout).

#### Scenario: Default invocation scans ~/dev

- **WHEN** `ctx-scan scan` is run with no flags
- **THEN** the scan SHALL walk `~/dev` as the root
- **AND** the resulting JSON document SHALL be written to stdout

#### Scenario: Explicit root and output path

- **WHEN** `ctx-scan scan --root /tmp/fixture --json /tmp/out.json` is run
- **THEN** the scan SHALL walk `/tmp/fixture` as the root
- **AND** the resulting JSON document SHALL be written to `/tmp/out.json`

### Requirement: Fleet discovery excludes vendored, archived, and worktree paths
The system SHALL discover project roots as any directory containing `CLAUDE.md`, `.claude/`, or
`.mcp.json`, deduped to the outermost containing git root, while excluding `node_modules`,
`.git`, `archive*`, `*-archive`, `archived`, `plugins/cache`, `plugins/marketplaces`,
`.worktrees`, `dist`, and `build` at any depth.

#### Scenario: Vendored and archived directories produce zero phantom projects

- **GIVEN** a fixture tree containing a project root, a `plugins/marketplaces/some-plugin/`
  subtree with its own `CLAUDE.md`, an `archive/old-project/` subtree with its own `.claude/`,
  and a `.worktrees/session-1/` subtree with its own `.mcp.json`
- **WHEN** `ctx-scan scan --root <fixture>` runs
- **THEN** the discovered project list SHALL contain exactly the one real project root
- **AND** SHALL NOT contain the plugin, archive, or worktree subtrees

#### Scenario: Symlink cycle does not hang or double-count

- **GIVEN** a fixture tree with a symlink that creates a directory cycle
- **WHEN** `ctx-scan scan --root <fixture>` runs
- **THEN** the scan SHALL terminate
- **AND** SHALL NOT report the cyclic path as a duplicate project

### Requirement: Settings precedence is resolved and attributed per key
The system SHALL resolve every effective setting across the managed, CLI, project-local,
project-shared, and user layers in that precedence order, recording which layer's value won for
each key.

#### Scenario: Project-local override wins over project-shared

- **GIVEN** the same key set in both `.claude/settings.local.json` and `.claude/settings.json`
  with different values
- **WHEN** the resolver runs
- **THEN** the reported winning value SHALL be the `.claude/settings.local.json` value
- **AND** the reported winning layer SHALL be `.claude/settings.local.json`

#### Scenario: Malformed settings file does not abort the scan

- **GIVEN** a `.claude/settings.json` containing invalid JSON
- **WHEN** the resolver runs
- **THEN** the resolver SHALL report a per-file parse error for that path
- **AND** SHALL continue resolving the remaining layers rather than throwing

### Requirement: Global layer is identified and scanned exactly once
`~/.claude` (resolved via `realpath`, following any symlink to its real target) SHALL be scanned
exactly once as the `global` origin and SHALL NOT appear in the discovered-project list.

#### Scenario: Symlinked global config is not double-counted

- **GIVEN** `~/.claude` is a symlink to `~/dev/cc`
- **WHEN** `ctx-scan scan` runs over `~/dev`
- **THEN** the fleet output SHALL contain exactly one `global` entry
- **AND** `~/dev/cc` SHALL NOT also appear as a separate discovered project

### Requirement: Data model is schema-versioned
The system SHALL emit a JSON document with a `schemaVersion` integer field, where every `Node`
carries `path`, `cls` (one of the 13 documented classes), `tier`, `raw_chars`,
`effective_chars`, `est_tokens`, `origin` (`global` or `project`), `truncations`, and `bands`.

#### Scenario: Snapshot fixture locks the schema shape

- **WHEN** `ctx-scan scan` runs against a fixed fixture tree
- **THEN** the output JSON SHALL match the committed schema snapshot exactly
- **AND** any structural change to the `Node` shape without a `schemaVersion` bump SHALL fail
  the snapshot test

### Requirement: CLAUDE.md @import chain is resolved up to 4 hops
The system SHALL resolve `@import` directives in CLAUDE.md up to 4 hops deep, and SHALL NOT
resolve an `@`-prefixed token appearing inside a fenced code block.

#### Scenario: Import chain resolves to the correct depth

- **GIVEN** a fixture CLAUDE.md with a 3-hop `@import` chain
- **WHEN** the assembly engine runs
- **THEN** all 3 hops SHALL be resolved into the `claude-md-chain` class

#### Scenario: Code-fenced @-token is not treated as an import

- **GIVEN** a CLAUDE.md containing `` `@import example.md` `` inside a fenced code block used as
  documentation
- **WHEN** the assembly engine runs
- **THEN** that token SHALL NOT be resolved as a real import

### Requirement: Listing entries are truncated to platform-accurate size
The system SHALL cap each skill/command/agent listing entry at 1,536 characters and apply the
listing's overall budget-fraction cap with least-invoked-first drop ordering when invocation
frequency data is available, marking entries `order: unknown` otherwise.

#### Scenario: Oversized description is truncated to the effective cap

- **GIVEN** a skill frontmatter `description` of 2,000 characters
- **WHEN** the assembly engine runs
- **THEN** the `Node`'s `effective_chars` SHALL be capped at 1,536
- **AND** `raw_chars` SHALL retain the full 2,000

#### Scenario: Drop prediction without invocation data is marked unknown

- **GIVEN** no invocation-frequency telemetry is reachable
- **WHEN** the listing budget-fraction cap forces a drop decision
- **THEN** the affected entries SHALL be marked `order: unknown` rather than a guessed rank

### Requirement: MEMORY.md and MCP descriptions are capped with raw retained
The system SHALL cap MEMORY.md at 200 lines / 25KB and each MCP description at 2KB, retaining the
uncapped `raw` value on the `Node` alongside the capped `effective` value.

#### Scenario: Oversized MEMORY.md is capped

- **GIVEN** a 400-line MEMORY.md fixture
- **WHEN** the assembly engine runs
- **THEN** `effective_chars` SHALL reflect only the first 200 lines / 25KB, whichever binds first
- **AND** `raw_chars` SHALL reflect the full file

### Requirement: Hook injection size comes from telemetry or explicit probe, never source reading
The system SHALL source `hooks-injected` sizes from `hook_output_metrics` telemetry when
reachable, from `--probe-hooks` execution when telemetry is unavailable, and SHALL render
`unknown` — never zero — when neither source is available.

#### Scenario: Telemetry-backed hook size

- **GIVEN** `hook_output_metrics` data is reachable for a given hook
- **WHEN** the assembly engine runs
- **THEN** that hook's `Node` SHALL report the telemetry-measured `stdout_bytes`

#### Scenario: No telemetry, no probe run

- **GIVEN** telemetry is unreachable and `--probe-hooks` was not passed
- **WHEN** the assembly engine runs
- **THEN** that hook's `Node` SHALL report `unknown`
- **AND** SHALL NOT report a size of 0

### Requirement: Telemetry calibration degrades gracefully when unreachable
`ctx-scan calibrate --from-telemetry` SHALL resolve its endpoint via the documented order, verify
required event-attribute schema before trusting any query, and exit 0 with an `unavailable`
status and reason whenever resolution or schema verification fails.

#### Scenario: Containers reachable, schema verified

- **GIVEN** Loki is reachable at the resolved endpoint with the expected event schema
- **WHEN** `ctx-scan calibrate --from-telemetry --json` runs
- **THEN** the output SHALL include endpoint provenance, verified schema summary, and a fitted
  chars→tokens ratio
- **AND** the process SHALL exit 0

#### Scenario: Containers unreachable

- **GIVEN** no telemetry endpoint resolves
- **WHEN** `ctx-scan calibrate --from-telemetry --json` runs
- **THEN** the output SHALL report `unavailable` with the specific failure reason
- **AND** the process SHALL exit 0

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

