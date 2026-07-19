---
order: 0719a
---

# Proposal: Extend `home/projects.toml` Schema + Migrate cc Registry Data

## Change ID
`extend-projects-toml-schema`

## Summary
Extend `home/projects.toml`'s per-project schema with optional richer metadata (dev port, deploy
target, monitor wiring, personas, seed command, stack, beads/openspec presence, prod URL, legacy
code aliases) and migrate cc's 37-project `~/dev/cc/scripts/config/projects.json` registry's
data into the matching (or newly-created) `home/projects.toml` entries, so this repo's registry
becomes the canonical fleet source of truth cc's own consumer scripts can be rewritten against.

## Context
- Extends: `home/projects.toml` (776 lines, 93 projects today)
- Related: `openspec/specs/launcher-registry/spec.md` (parent capability — governs
  `projects.toml`-consuming generators; this proposal adds new ADDED requirements to it rather
  than creating a separate capability), the companion cc-side proposal
  `migrate-projects-json-to-if-toml` (authored in `~/dev/cc`, out of scope here, BLOCKED on this
  proposal landing first — it rewrites cc's 17 consumer scripts to read this file)
- touches: `home/projects.toml`

## Motivation
Leo decided (resolves if-7cce.6) that `home/projects.toml` becomes the canonical fleet project
registry, replacing cc's own `scripts/config/projects.json`. cc's registry carries per-project
metadata (`devPort`, `deploy`, `monitors`, `personas`, `seed_command`, `stack`, `has_beads`,
`has_openspec`, `prod_url`) that `home/projects.toml` has no schema slot for today. Without this
schema extension + data migration, the companion cc-side proposal that rewrites cc's 17 consumer
scripts has nothing to read — this proposal is the hard blocker it depends on.

## Requirements

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

## Scope
- **IN**: schema design for the 11 new optional fields (10 requested + `legacy_codes`); migrating
  all 37 cc-registry projects' new-field data onto the correct existing or newly-created
  `home/projects.toml` entry; documenting the schema in the file's own header comment; full-file
  and consumer-output verification.
- **OUT**: rewriting cc's own 17 consumer scripts to read this file (the separate, blocked
  companion proposal `migrate-projects-json-to-if-toml` in `~/dev/cc`); changing any of the ~56
  if-only entries beyond the optional-field additions this schema now permits; renaming any
  existing if-toml project code to match cc's shorter code; deleting cc's `projects.json` (the
  companion proposal's concern, not this one's).

## Done Means
- `home/projects.toml` documents and accepts all 11 new optional fields on any `[[projects]]`
  entry without breaking parsing of entries that don't set them.
- All 37 cc-registry projects' new-field data lives in the correct `home/projects.toml` entry
  (matched by project identity, not literal code), with zero duplicate entries and zero data
  written onto the wrong (`tb`-collision) entry.
- `ws-topo` exists as a new, correctly-populated `[[projects]]` entry.
- The three existing consumer scripts run unmodified and produce unchanged output for every
  if-only entry.

## Testing
| Affected seam | Unit task | E2E task |
|---|---|---|
| Schema parse (new fields, inline `deploy`, nested `monitors`, bare-string anomaly) | N/A — TOML config, no unit harness in this repo | `[2.1]` |
| 37-project data migration correctness (code-mismatch + collision + new-entry handling) | N/A | `[2.1]` |
| Consumer script compatibility | N/A | `[2.2]`, `[2.3]` |

## Impact
| Area | Change |
|---|---|
| `home/projects.toml` | Schema-extended header comment; 11 new optional fields; new-field data added to ~30 directly-matched + 7 identity-matched existing entries; 1 new entry (`ws-topo`) |
| `scripts/generate-raycast.sh`, `scripts/cmux-workspaces.sh`, `scripts/mux-remote.sh` | None — verified to already tolerate unknown keys |

## Risks
| Risk | Mitigation |
|---|---|
| Migrating onto the wrong entry corrupts an unrelated project's data (the `tb`/`thebridge` collision case) | Match by `path` identity, not code string; `design.md` documents the collision explicitly and the task that handles it names both entries by full path |
| Silently renaming if-toml's existing codes to match cc's shorter codes breaks daily raycast/cmux muscle memory | Keep if-toml's existing codes canonical; use `legacy_codes` for old-code resolution instead of renaming |
| `hn`'s bare-string `deploy` anomaly gets silently "fixed" without confirming Leo's intent | `[user:pre]` DECISION task before any data migration touches `hn` |
