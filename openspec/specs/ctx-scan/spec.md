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

