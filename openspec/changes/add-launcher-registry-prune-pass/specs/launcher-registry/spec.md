# launcher-registry Specification

## ADDED Requirements

### Requirement: Generators prune orphaned per-code output
Any script that generates per-project-code launcher artifacts from `home/projects.toml` SHALL
delete artifacts whose code is no longer a registry key, on every normal (non-dry-run)
invocation, so the registry is the single source of truth for both presence and absence.

#### Scenario: registry removal triggers cleanup
- **WHEN** a `[projects.xx]` entry that previously existed is removed from `projects.toml` and
  the generator runs again
- **THEN** every output file the generator previously wrote for `xx` SHALL be deleted

#### Scenario: dry-run previews without mutating
- **WHEN** the generator runs with `--dry-run` after a registry removal
- **THEN** it SHALL list the files it would delete without deleting them
