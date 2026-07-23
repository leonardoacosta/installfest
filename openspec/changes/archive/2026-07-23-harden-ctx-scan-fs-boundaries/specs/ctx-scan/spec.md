## ADDED Requirements

### Requirement: Discovery and @import resolution stay within the scan root
The system SHALL confine both the fleet-discovery walk (`discovery.ts`) and `@import` chain
resolution (`imports.ts`) to paths within the scan root: a candidate directory or resolved
import whose realpath is not within the root's realpath SHALL be silently skipped (never
followed further, never added to the discovered-project list or the resolved-import list) rather
than causing the scan to throw. This applies in addition to, not instead of, each file's existing
cycle guard.

#### Scenario: Symlink escape yields zero out-of-root projects
- **GIVEN** a fixture tree under `--root` containing a symlink whose target resolves outside the
  root (e.g. to a sibling directory or the filesystem root)
- **WHEN** `ctx-scan scan --root <fixture>` runs
- **THEN** the discovered-project list SHALL NOT contain the symlink's target or anything beneath
  it
- **AND** the scan SHALL complete without throwing

#### Scenario: Traversal @import resolves to zero out-of-root imports
- **GIVEN** a project's CLAUDE.md containing an `@import` directive that traverses outside the
  project root (e.g. `@../../../etc/hosts`)
- **WHEN** the `@import` chain is resolved for that project
- **THEN** the resolved-import list SHALL NOT contain the out-of-root path
- **AND** a legitimate in-project `@import` (e.g. `@rules/CORE.md`) in the same file SHALL still
  resolve normally
- **AND** resolution SHALL complete without throwing

### Requirement: --probe-hooks clearly warns about its untrusted per-project command source
`ctx-scan`'s `--probe-hooks` flag SHALL carry warning text (in both its CLI option help and its
runtime stderr warning) that explicitly names project-local `.claude/settings.json` as the
source of the shell command it executes, so an operator understands the flag runs untrusted
per-project content across every discovered project, not just ctx-scan's own trusted
configuration.

#### Scenario: Warning text names the untrusted source
- **WHEN** `ctx-scan scan --help` is inspected for the `--probe-hooks` option, and when
  `ctx-scan scan --root <path> --probe-hooks` is run
- **THEN** both the option help text and the runtime warning SHALL state that the probed command
  originates from each discovered project's own `.claude/settings.json`
- **AND** SHALL advise using the flag only on roots containing solely trusted repositories
