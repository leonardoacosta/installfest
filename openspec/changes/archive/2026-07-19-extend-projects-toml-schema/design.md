# Design: `home/projects.toml` Schema Extension

## Problem

cc's `~/dev/cc/scripts/config/projects.json` (37 projects) carries richer per-project metadata
than `home/projects.toml` (93 projects, if-only) does today: dev ports, deploy target shape,
per-service monitor wiring, personas, seed commands, stack, beads/openspec presence, prod URLs.
Leo decided `home/projects.toml` becomes the canonical fleet registry (resolves if-7cce.6); a
companion cc-side proposal (`migrate-projects-json-to-if-toml`, authored in `~/dev/cc`, blocked
on this one) will rewrite cc's 17 consumer scripts to read this file instead. This document
records the schema design and the concrete per-project migration decisions.

## New optional fields

All fields below are OPTIONAL on a `[[projects]]` entry — absent for the ~56 if-only entries
that don't need them, and absent for any of the 37 migrated entries where cc's source data had
no value. None of the three existing consumers (`generate-raycast.sh`, `cmux-workspaces.sh`,
`mux-remote.sh`) iterate over unknown keys — each reads only `code`/`name`/`icon`/`path`/`tiers`/
`category` by literal dict key (verified by reading all three scripts in full) — so adding new
keys is 100% additive and requires zero consumer changes (see § Consumer impact below).

| Field | Type | Notes |
|---|---|---|
| `devPort` | int | Dev server port (T3/Next apps) |
| `port` | int | Generic service port, distinct from `devPort` (e.g. `guardian`'s 3150) — both appear in source data for different projects, never both on one entry |
| `deploy` | inline table | See § Deploy shape |
| `monitors` | nested tables | See § Monitors shape |
| `personas` | array of strings | e.g. `["attendee", "staff"]` |
| `seed_command` | string | e.g. `"pnpm db:reconcile"` |
| `stack` | string | e.g. `"t3-turbo"` |
| `has_beads` | bool | |
| `has_openspec` | bool | |
| `prod_url` | string | |
| `legacy_codes` | array of strings | See § Legacy code aliasing — NOT in the original field list, added to correctly complete the migration; see rationale below |

## Deploy shape: inline table, not a nested `[projects.deploy]` block

`deploy` varies by `type` (vercel/docker/azure/systemd) but stays small (3-5 keys per type), so
it is represented as a single-line **inline table** directly on the project's scalar-field
region:

```toml
[[projects]]
code = "oo"
...
deploy = { type = "vercel", vercelProject = "otaku-odyssey", devBranch = "dev", prodBranch = "main" }
```

Per-type field vocabulary (normalizes cc's own source-data inconsistency — some entries used
`"vercel-org"` kebab-case, others `vercelProject` camelCase, for the same conceptual field; this
migration standardizes on camelCase throughout):

| `type` | Fields |
|---|---|
| `vercel` | `vercelOrg` (optional), `vercelProject`, `devBranch`, `prodBranch` |
| `docker` | `host`, `domain` (optional), `prodBranch` (optional) |
| `azure` | `host`, `org` (optional), `project` (optional) |
| `systemd` | `host` |

**Verified round-trip** (see § Verification): inline tables inside `[[projects]]` array-of-tables
entries parse correctly and independently per entry via `tomllib`.

**Anomaly — `hn`/harness**: cc's source data has `deploy = "homelab"` — a bare string, the only
entry of 37 not shaped as a table (every other `docker`-deployed entry, e.g. `at`/`hl`/`if`, uses
`{ type = "docker", host = "homelab" }`). This is flagged as a pre-existing data-quality defect
in cc's own registry, not silently "fixed" by guessing — see task `[1.1]` (`[user:pre]` DECISION)
for Leo to pick literal-preservation vs. normalization, per the Breaking Changes Policy default
of asking before reshaping data.

## Monitors shape: nested `[projects.monitors.<service>]` tables, not one inline table

`monitors` holds multiple named per-service sub-objects (`vercel`, `posthog`, `better-stack`,
`azure`, `grafana`, `openreplay`), each carrying `enabled` plus several service-specific fields.
An inline table would force this onto one unreadable multi-field single line; standard TOML
nested tables read cleanly instead, one block per service, placed immediately after the owning
`[[projects]]` entry's scalar/inline-table region and before the next `[[projects]]` header:

```toml
[[projects]]
code = "oo"
...

[projects.monitors.vercel]
enabled = true
project = "oo"

[projects.monitors.posthog]
enabled = false
project_id = 220182
superseded_by = "openreplay"

[projects.monitors.grafana]
enabled = true
dashboard_uid = ""
note = "placeholder"
```

**Verified round-trip**: a fixture with two consecutive `[[projects]]` entries, the first
carrying three `[projects.monitors.X]` blocks and the second carrying one, parsed via
`tomllib.load()` with each project's `monitors` dict correctly scoped to its own entry — no
cross-entry leakage (tested with `oo`/`tc`/`hn` fixture, `/tmp/toml_test.toml`, this session).

## Legacy code aliasing — an addition beyond the original field list, and why

Matching cc's 37 codes against `home/projects.toml` by literal code-string equality (as a first
pass) found **7 mismatches** where cc and if-toml use *different* codes for the *same* project
(matched instead by identical `path`), plus **1 real code collision**, plus **1 brand-new
project**:

| cc code | if-toml canonical code | Match basis |
|---|---|---|
| `pc` | `priceless-config` | same `path` (`dev/priceless/priceless-config`) |
| `pa` | `priceless-app` | same `path` |
| `sj` | `seth-jones` | same `path` |
| `at` | `atlas` | same `path` |
| `tm` | `terraform-modules` | same `path` prefix; cc's own path (`dev/priceless/terraform-modules`) is stale vs. if-toml's (`dev/terraform-modules`) — consistent with the known post-reorg path drift in cc's own registry (see cc memory `fleet-repo-paths-org-reorg-2026-07-10.md`). Migration attaches new fields only; `path` is never touched. |
| `gd` | `guardian` | same `path` |
| `hn` | `harness` | same `path` |

**Collision — `tb`**: cc's `tb` code (`~/dev/brown/thebridge`, "The Bridge") is NOT the same
project as if-toml's *existing* `tb` entry (`dev/tb`, an unrelated project already registered
under that code). The correct match for cc's data is if-toml's separate, existing `thebridge`
entry (`dev/brown/thebridge`). Migrating cc's `tb` fields onto if-toml's `tb` entry would silently
corrupt an unrelated project's data — task `[1.5]` migrates onto `thebridge` instead and
explicitly does NOT add `"tb"` to its `legacy_codes` (that would collide with the real,
already-registered `tb` project — an ambiguous alias is worse than no alias). This is called out
as a hard constraint for the companion cc-side proposal: its rewritten scripts must special-case
`tb` -> `thebridge`, not resolve it via `legacy_codes`.

**Brand new — `ws-topo`**: no existing if-toml entry by code or path. Task `[1.6]` creates a new
`[[projects]]` entry.

**Decision**: if-toml's *existing* codes stay canonical (they're already muscle-memory for
raycast/cmux on ~56 if-only entries and the 30 already-matching migrated ones) — renaming them to
match cc's shorter codes was considered and rejected as needless churn to an already-working,
daily-used surface. Instead, an optional `legacy_codes` array field (e.g.
`legacy_codes = ["at"]` on the `atlas` entry) records cc's old code as a resolvable alias, so the
companion cc-side proposal's script rewrite has a mechanical way to translate its 17 scripts'
hardcoded old codes to the correct canonical entry without a manual lookup table living only in
this design doc. This is a necessary addition to correctly complete the requested 37-project
migration (silently dropping the alias data would be a partial/incorrect migration, not a scope
expansion) — flagged explicitly here per the ledger-closure norm of documenting any schema
addition beyond an initial ask.

## Consumer impact — no UI batch needed

All three current consumers were read in full this session:

- `scripts/generate-raycast.sh` — Python block accesses `project["path"]`, `project["code"]`,
  `p["tiers"]`, `p["name"]`, `p["icon"]`, `p["category"]` by literal key. No iteration over
  unknown keys.
- `scripts/cmux-workspaces.sh` — same pattern: `p["category"]`, `p["code"]`, `p["path"]`,
  `p["name"]`.
- `scripts/mux-remote.sh` — delegates to `cmux-workspaces.sh` for its picker data; its own
  inline Python only reads `p["category"]`, `p["code"]`, `p["name"]`.

None of the three will error or change behavior when new optional keys are present on other
entries. No UI Batch is needed for this change.

## Verification performed during spec authoring (informational — not the E2E task evidence)

```
$ python3 -c "import tomllib; d=tomllib.load(open('/tmp/toml_test.toml','rb')); import json; print(json.dumps(d,indent=2))"
```
produced correctly-scoped `deploy` inline tables and `monitors` nested tables per entry
(including the `hn` bare-string `deploy = "homelab"` anomaly parsing cleanly as a plain string
value) — see task `[2.1]` for the real fixture's full-registry equivalent, run against the
actual post-migration `home/projects.toml`.
