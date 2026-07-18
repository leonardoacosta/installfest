# ctx-scan Specification

## ADDED Requirements

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
