## ADDED Requirements

### Requirement: view-model subcommand emits the band-annotated view model as JSON
The CLI SHALL provide a `view-model` command accepting `--root <path>` and optional
`--json <path>` that assembles the fleet for the given root, annotates rubric bands
(`annotateFleetBands`), derives the display-ready view model (`buildViewModel`), and emits it as
JSON wrapped in an envelope carrying a `schemaVersion` field, to stdout or the given file. The
command SHALL NOT execute hooks, probe telemetry, or perform any network I/O — it is pure
filesystem reads, same as a default `scan`. The emitted payload SHALL carry computed band
values on class and document entries (never the raw-`scan` empty-bands shape).

#### Scenario: view-model output is band-annotated and schema-versioned
- Given: a fixture project tree with at least one document exceeding a Table A cap
- When: `ctx-scan view-model --root <fixture>` runs
- Then: the JSON output carries `schemaVersion: 1`, and the offending entry carries a non-GREEN
  band — bands are present without a separate `audit` invocation

#### Scenario: emitted shape is locked by a fixture test
- Given: the view-model fixture snapshot test
- When: the emitted envelope's shape changes without a `schemaVersion` bump
- Then: the test fails, per the existing schema-versioning requirement
