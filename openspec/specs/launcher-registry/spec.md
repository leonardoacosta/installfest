# launcher-registry Specification

## Purpose
TBD - created by archiving change add-launcher-registry-prune-pass. Update Purpose after archive.
## Requirements
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

### Requirement: projects.toml schema supports optional richer project metadata
`home/projects.toml`'s `[[projects]]` schema SHALL support these additional OPTIONAL fields,
absent when a project doesn't need them: `devPort` (int), `port` (int), `deploy` (inline table),
`monitors` (nested tables), `personas` (array of string), `seed_command` (string), `stack`
(string), `has_beads` (bool), `has_openspec` (bool), `prod_url` (string), `legacy_codes` (array
of string). An entry with none of these fields set MUST remain valid and parse identically to
today's schema.

#### Scenario: entry without new fields still parses
- **WHEN** an if-only `[[projects]]` entry carries none of the new fields
- **THEN** `tomllib.load()` SHALL parse it exactly as it does today, with no new keys present in
  its dict

#### Scenario: entry with all new fields set parses and round-trips
- **WHEN** a `[[projects]]` entry sets every new field (`devPort`, `deploy`, `monitors`,
  `personas`, `seed_command`, `stack`, `has_beads`, `has_openspec`, `prod_url`, `legacy_codes`)
- **THEN** `tomllib.load()` SHALL parse all values with correct types (int/table/array/string/bool)
  scoped to that entry only

### Requirement: `deploy` is represented as a type-keyed TOML inline table
The `deploy` field SHALL be a single inline table `{ type = "vercel"|"docker"|"azure"|"systemd",
...type-specific fields }` attached directly on the owning `[[projects]]` entry, per the field
vocabulary in `design.md` § Deploy shape. A bare-string `deploy` value (the pre-existing `hn`
anomaly in cc's source data) MUST also parse without a schema violation, since TOML permits any
value type on a key.

#### Scenario: vercel deploy entry parses with all sub-fields
- **WHEN** a `[[projects]]` entry sets `deploy = { type = "vercel", vercelProject = "x",
  devBranch = "dev", prodBranch = "main" }`
- **THEN** `tomllib.load()` SHALL return `deploy` as a dict with all four keys correctly typed

#### Scenario: bare-string deploy value does not break parsing
- **WHEN** a `[[projects]]` entry sets `deploy = "homelab"` (a bare string, not a table)
- **THEN** `tomllib.load()` SHALL parse it as a plain string value with no error

### Requirement: `monitors` is represented as nested per-service TOML tables
The `monitors` field SHALL be represented via one `[projects.monitors.<service>]` nested table
block per named monitor (`vercel`, `posthog`, `better-stack`, `azure`, `grafana`, `openreplay`),
placed after the owning `[[projects]]` entry's scalar/inline-table region and before the next
`[[projects]]` header, each block carrying at minimum an `enabled` boolean.

#### Scenario: multi-monitor project scopes correctly
- **WHEN** a `[[projects]]` entry is followed by three `[projects.monitors.X]` blocks before the
  next `[[projects]]` header
- **THEN** `tomllib.load()` SHALL attach all three to that entry's `monitors` dict and NOT to any
  other entry's `monitors`

### Requirement: legacy code aliasing for migrated cc-registry codes
The schema SHALL support an optional `legacy_codes` array field on a `[[projects]]` entry, listing
prior short codes (e.g. from cc's own registry) that referred to the same project under a
different code than if-toml's canonical one, so a downstream consumer resolving an old code can
find the canonical entry without renaming if-toml's already-established code.

#### Scenario: a migrated cc code resolves via legacy_codes
- **WHEN** cc's registry used code `at` for the project if-toml already registers as `atlas`
- **THEN** the `atlas` entry SHALL carry `legacy_codes = ["at"]`, and no new entry or rename SHALL
  be created under the code `at`

#### Scenario: a genuine code collision is never aliased
- **WHEN** cc's code (`tb`, "The Bridge") collides with an if-toml code (`tb`) already assigned to
  an unrelated existing project
- **THEN** cc's data SHALL be migrated onto if-toml's separate, correctly-matched `thebridge`
  entry, and `legacy_codes` on that entry SHALL NOT include `"tb"` (ambiguous alias against an
  already-registered, unrelated project)

### Requirement: existing consumers remain unaffected by new optional fields
`scripts/generate-raycast.sh`, `scripts/cmux-workspaces.sh`, and `scripts/mux-remote.sh` SHALL
continue to run and produce identical output for entries that set none of the new fields, without
modification, since all three read only known literal keys (`code`/`name`/`icon`/`path`/`tiers`/
`category`) and never iterate over unknown keys.

#### Scenario: unmodified consumers tolerate the extended schema
- **WHEN** `generate-raycast.sh --dry-run` and `cmux-workspaces.sh`'s loader run against the
  post-migration `home/projects.toml`
- **THEN** both SHALL complete without error, and their output for any if-only entry (one with no
  new fields set) SHALL be byte-identical to their pre-migration output

### Requirement: cc's 37-project registry is migrated into the extended schema
Each of cc's 37 `scripts/config/projects.json` entries' new-field data (where present) SHALL be
attached to the matching `home/projects.toml` entry — matched by project identity (`path`), not
literal code-string equality — preserving if-toml's existing code as canonical, creating exactly
one new entry (`ws-topo`) for the one project with no existing match, and never producing a
duplicate entry for a project that already exists under a different code.

#### Scenario: full registry still parses after migration
- **WHEN** all 37 projects' data has been migrated into `home/projects.toml`
- **THEN** `python3 -c "import tomllib; tomllib.load(open('home/projects.toml','rb'))"` SHALL
  succeed with no exception, and the resulting `projects` array SHALL contain exactly 94 entries
  (93 existing + 1 new `ws-topo`)

