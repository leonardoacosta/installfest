---
model: opus
name: plan:roadmap
description: Generate phased spec pipeline from PRD/architecture
allowed-tools: Bash, Read, Write, Edit, Glob, Grep, AskUserQuestion, Agent
effort: high
---

# Workflow Roadmap — PRD-to-Specs Pipeline

## When to Use This Command

| Scenario                             | Command                  | Why                          |
| ------------------------------------ | ------------------------ | ---------------------------- |
| Single feature, unclear requirements | `/feature`               | Full discovery + refinement  |
| Single feature, clear requirements   | `/feature --quick`       | Skip discovery               |
| **Multiple specs from PRD**          | **`/plan:roadmap`**   | **Wave-based spec pipeline** |
| Incremental artifact creation        | `/feature --incremental` | Step-by-step with review     |

Stack-agnostic generalization of the `/phase-waves` pattern; the file-level conflict logic it grounded
now lives in `wave-conflict-check` (called below). Reads PRD output from `/project:discover`, parses the
domain model, creates phased specs via parallel `/feature --quick` agents, builds a file-level
conflict map, and outputs `docs/apply/<run-id>/wave-plan.json` (run-id allocated via `wave-state next-run-id`).

## Arguments

```
/plan:roadmap [PLAN_DIR] [--phase N] [--dry-run] [--progressive]
```

| Flag            | Description                                                                  |
| --------------- | ---------------------------------------------------------------------------- |
| `PLAN_DIR`      | Plan directory holding the locks + artifacts (default `docs/plan`)           |
| (none)          | Full pipeline — all phases, generate all specs                               |
| `--phase N`     | Generate specs for a single phase only (1–4)                                 |
| `--dry-run`     | Show the plan without creating any specs                                     |
| `--progressive` | Present one wave at a time; lock each wave as conflict-free specs accumulate |

## Phase 0: Dependency Guard

```bash
# PLAN_DIR is the first positional; defaults to docs/plan so bare `/plan:roadmap` still works.
PLAN_DIR="${1:-docs/plan}"

source ~/.claude/scripts/lib/project-guards.sh
require_lock "$PLAN_DIR/.locks/prd.md.locked" "Run /plan:prd first to create PRD."

# Scrutiny gate — the verdict lock must be present AND positive (verdict: GO).
SCRUTINY_LOCK="$PLAN_DIR/.locks/scrutiny.md.locked"
if [ ! -f "$SCRUTINY_LOCK" ]; then
  echo "ERROR: $SCRUTINY_LOCK not found. Run /plan:scrutiny before generating the roadmap."
  exit 1
fi
if ! grep -qE '^verdict:[[:space:]]*GO\b' "$SCRUTINY_LOCK"; then
  FOUND=$(grep -E '^verdict:' "$SCRUTINY_LOCK" || echo "verdict: (none)")
  echo "ERROR: scrutiny verdict is not GO ($FOUND). Resolve scrutiny before roadmap."
  echo "  BLOCKED   -> re-run /plan:scope or /plan:prd (scope/deliverables are wrong)."
  echo "  INCONCLUSIVE -> re-run /plan:scrutiny and close the open decisions."
  exit 1
fi
```

**Gate 0:** `.locks/prd.md.locked` exists AND `.locks/scrutiny.md.locked` exists with
`verdict: GO`. Roadmap requires a locked PRD that has cleared scrutiny.

## Step 1: Locate PRD Output

Search for source material in priority order:

1. `docs/plan/` — output from `/project:discover` (preferred)
2. `openspec/project.md` — domain model (if `/project:discover` was not run)
3. `docs/system-architecture.md` — architecture document
4. Any `.md` file in `docs/` containing headings like "Phase", "Entity", "Domain"

```bash
if [ -d "$PLAN_DIR" ]; then
  SOURCE="$PLAN_DIR/"
  echo "SOURCE: $PLAN_DIR/ (explore output)"
elif [ -f openspec/project.md ]; then
  SOURCE="openspec/project.md"
  echo "SOURCE: openspec/project.md"
elif [ -f docs/system-architecture.md ]; then
  SOURCE="docs/system-architecture.md"
  echo "SOURCE: docs/system-architecture.md"
else
  SOURCE=""
  echo "SOURCE: not found"
fi
```

If no source material is found, use `AskUserQuestion`:

```
No PRD or architecture document found. Where is the project's domain model or PRD?

Options:
  A) I'll run /project:discover first to generate docs/plan/
  B) Point me to a specific file: [path]
  C) I'll describe the domain verbally — extract from my description
```

## Step 1.5: Scan Open Ideas

Scan beads for ideas that may already describe features the roadmap should include:

```bash
IDEAS=$(bd list -l idea --status=open --json 2>/dev/null)
IDEA_COUNT=$(echo "$IDEAS" | jq 'length' 2>/dev/null || echo "0")
```

If ideas exist, match them against the PRD domains. For each idea whose description overlaps
with a PRD domain or feature area, flag it as a **pre-validated spec candidate**:

```
Idea Candidates ({count} matching PRD domains):
  - {slug} (nv-xxx) → matches domain "{domain}" — promote to spec? [Y/n]
  - {slug} (nv-xxx) → matches domain "{domain}" — promote to spec? [Y/n]
  ...

Non-matching ideas ({count}):
  - {slug} (nv-xxx) — remains in backlog
```

**Rule:** When an idea matches a PRD domain, use it as the basis for the spec instead of
inventing a new one. The idea's description contains context, feasibility notes, and file
paths from when it was captured. Reference the beads ID in the generated proposal.

## Step 1.6: Read Scrutiny Outputs

The scrutiny stage (gated in Phase 0) sharpened the deliverables, decided which game-changing
features are in scope, and enumerated the technical unknowns that must be de-risked first. Read
`$PLAN_DIR/scrutiny.md` and `$PLAN_DIR/.locks/scrutiny.md.locked` and apply them:

- **Sharpened deliverables** — generate specs from the SHARPENED set, not the raw PRD deliverables.
- **Included game-changing features** — each `gcf_included` slug becomes a spec (or folds into one).
- **Wave-0 spike specs** — every slug in the lock's `spike_specs:` line MUST be emitted as a
  **wave-0 spike spec** that gates all later waves. A spike spec's job is to resolve its technical
  unknown and produce runtime evidence (per CORE's Verification Iron Law) before dependent waves
  build on the answer.

```bash
SPIKE_SPECS=$(grep -E '^spike_specs:' "$PLAN_DIR/.locks/scrutiny.md.locked" | sed 's/^spike_specs:[[:space:]]*//')
GCF_INCLUDED=$(grep -E '^gcf_included:' "$PLAN_DIR/.locks/scrutiny.md.locked" | sed 's/^gcf_included:[[:space:]]*//')
echo "Wave-0 spike specs: ${SPIKE_SPECS:-none}"
echo "Included GCFs:      ${GCF_INCLUDED:-none}"
```

If `spike_specs:` is `none`, there is no wave 0 and Stage-1 foundation specs start at wave 1.

## Step 2: Detect Stack

```bash
if [ -f "go.mod" ]; then STACK="go"
elif ls *.sln 2>/dev/null || ls *.csproj 2>/dev/null; then STACK="dotnet"
elif [ -f "turbo.json" ]; then STACK="t3"
elif ls *.tf 2>/dev/null; then STACK="terraform"
else STACK="bash"
fi
echo "Stack: $STACK"
```

Stack determines how file-level conflict analysis works in Step 3.

## Step 2.5: Run Discovery (shared context)

Gather codebase context once. Inject into every `/feature` agent prompt.

```bash
PROJECT_NAME=$(basename "$PWD")
GIT_SHA=$(git rev-parse --short HEAD 2>/dev/null || echo "no-git")

# Stack-aware discovery
case "$STACK" in
  t3)
    SCHEMA_FILES=$(ls packages/db/src/schema/*.ts 2>/dev/null || echo "none")
    ROUTER_FILES=$(ls packages/api/src/routers/*.ts 2>/dev/null || echo "none")
    APP_ROUTES=$(find apps -name "page.tsx" 2>/dev/null | head -20 || echo "none")
    DISCOVERY="### T3 Schemas\n$SCHEMA_FILES\n\n### T3 Routers\n$ROUTER_FILES\n\n### App Routes\n$APP_ROUTES"
    ;;
  go)
    GO_TYPES=$(grep -r "type .* struct" --include="*.go" . 2>/dev/null | head -30 || echo "none")
    GO_PKGS=$(find . -type d -not -path "./.git/*" -not -path "./vendor/*" 2>/dev/null | head -30 || echo "none")
    DISCOVERY="### Go Types\n$GO_TYPES\n\n### Go Packages\n$GO_PKGS"
    ;;
  dotnet)
    PROJECTS=$(ls **/*.csproj 2>/dev/null || echo "none")
    CONTROLLERS=$(find . -name "*Controller.cs" 2>/dev/null | head -20 || echo "none")
    DISCOVERY="### .NET Projects\n$PROJECTS\n\n### Controllers\n$CONTROLLERS"
    ;;
  *)
    DISCOVERY="### Scripts\n$(ls scripts/ 2>/dev/null || echo 'none')"
    ;;
esac

# Common: beads and archive
BD_RESULTS=$(bd ready --json 2>/dev/null | head -30 || echo "no beads data")
ARCHIVE_LIST=$(ls openspec/changes/archive/ 2>/dev/null | head -20 || echo "no archives")
```

## Step 3: Parse Domain Model

Read the source material and extract:

- **Entity list** (User, Event, Ticket, Campaign, etc.)
- **Entity relationships** (has-many, belongs-to, many-to-many)
- **Feature areas / bounded contexts** (Auth, Catalog, Orders, etc.)
- **Implementation phases** (if explicitly defined)

### Phase Inference (if not defined in source)

If no explicit phases are found, infer from dependency topology:

| Phase | Label         | Contents                                        |
| ----- | ------------- | ----------------------------------------------- |
| 1     | Foundation    | Core schemas, base models, type definitions     |
| 2     | Core Features | CRUD operations, primary business logic         |
| 3     | Integration   | Cross-entity features, aggregations, workflows  |
| 4     | Polish        | UX improvements, performance, edge cases, admin |

### Stack-Aware Scoping

For each feature spec, determine which files it will touch:

**T3:**

- Schema files: `packages/db/src/schema/{entity}.ts`
- Router files: `packages/api/src/routers/{entity}.ts`
- UI routes: `apps/{app}/app/(routes)/{feature}/page.tsx`

**Go:**

- Package directories: `internal/{domain}/`, `pkg/{domain}/`
- Type files: `pkg/types/{domain}.go`

**.NET:**

- Project files: `src/{Service}/{Service}.csproj`
- Controller files: `src/{Service}/Controllers/{Entity}Controller.cs`

**bash/terraform:**

- Script files: `scripts/{feature}.sh`
- Module files: `modules/{feature}/main.tf`

## Step 4: Conflict Analysis

Use `wave-conflict-check` — the shared conflict analysis script also used by `apply:all` Step 6. It
applies two signals: explicit `depends_on` lines in `proposal.md`, then file-level overlap from
`tasks.md`.

After all specs exist in `openspec/changes/`, assign wave numbers:

### Conflict Detection

```bash
# Check for file-level conflicts between specs in the same wave
~/.claude/scripts/bin/wave-conflict-check \
  --state-file "$WAVE_PLAN_PATH" \
  --next-wave "$WAVE_NUM" \
  $SPEC_NAMES
```

If the script is not available, fall back to manual conflict detection:

- List all files modified by each spec (from tasks.md impact tables)
- Flag any file appearing in 2+ specs within the same wave
- Move conflicting specs to separate waves

**Conflict resolution rules** (enforced by the script):

| Conflict type                         | Signal   | Resolution                                     |
| ------------------------------------- | -------- | ---------------------------------------------- |
| Explicit `depends on:` in proposal.md | Signal 1 | Place after the dependency's wave              |
| Same file path in tasks.md            | Signal 2 | Place after the conflicting spec's wave        |
| Same package directory only           | —        | Not a conflict — two specs may share a package |

> Implementation: `~/.claude/scripts/bin/wave-conflict-check --help`

## Step 5: Generate Specs (parallel)

For each feature in each phase, if the spec does not already exist in `openspec/changes/`:

```bash
# Check for existing spec
if [ -d "openspec/changes/$SPEC_NAME" ]; then
  echo "SKIP: $SPEC_NAME (already exists)"
else
  echo "CREATE: $SPEC_NAME"
fi
```

Spawn one Agent per spec using `run_in_background: true` in a single message (all non-conflicting
specs in the same phase can be created in parallel):

```
Agent(
  subagent_type="general-purpose",
  run_in_background=true,
  prompt="""
Run /feature --quick for spec '[SPEC_NAME]' in project at [CWD].

## Roadmap Context

Phase: [N] — [Phase Label]
Spec: [SPEC_NAME]
Stack: [STACK]
Feature area: [FEATURE_AREA]
Entities involved: [ENTITY_LIST]
Files this spec will touch:
  [FILE_LIST from scoping above]

Depends on: [SPEC_DEPS or 'none']
Depended on by: [DEPENDENT_SPECS or 'none']

## Discovery Context (pre-computed)

[DISCOVERY block from Step 2.5]

Use --quick flag. Complete the full spec including beads sync.
Answer any clarifying questions using the context provided above.

## Authored order hint (advisor-plans/029, proposal-queue-linearity)

[IF this is NOT the first spec in this phase:] Set the proposal.md frontmatter
`after: [PRECEDING_SPEC_IN_PHASE] — <one-line reason from this phase's ordering rationale>`
citing the immediately-preceding spec in this phase's generation order. This is a batch —
`openspec-status --queue` needs the rationale or the batch decays into an unordered pile the
same way past roadmap/waves output has. [ELSE, first spec in phase:] Omit `after:` — nothing
precedes it in this batch.
"""
)
```

### Progressive mode (`--progressive`)

As each Agent completes, read its `tasks.md` and update the conflict map incrementally. When a set
of conflict-free specs has accumulated, lock them as a wave and write a partial `wave-plan.json`
immediately rather than waiting for all specs to finish.

### Sequential phase handling

For phases with explicit sequential dependencies (e.g., tree Phase 1: config → git → tui):

- Still create all specs in parallel (spec creation is independent of execution order)
- Enforce the dependency chain in the wave assignments — the dependency map overrides the
  file-conflict analysis

## Step 6: Build wave-plan.json

After all specs are created, read each spec's `tasks.md` to extract the actual file paths it
modifies. Refine the conflict map with the real paths.

Assign specs to waves:

- **Wave 0 is reserved for scrutiny's spike specs** (from Step 1.6). Every other wave depends on
  wave 0 — `/apply:all` runs waves in order, so no Stage-1+ spec executes until the spikes resolve
  their unknowns. A spike that concludes BLOCKED (negative runtime evidence) halts the wave plan
  and routes back to `/plan:scrutiny` / `/plan:scope`. If there are no spike specs, omit wave 0.
- Specs in wave N must have zero conflicts with each other
- Dependency-constrained specs go in separate waves in order
- Within a wave, all specs run in parallel via `/apply:all`

Allocate a fresh run-id and write the run-scoped plan path (FR22 — `docs/apply/<run-id>/wave-plan.json`):

```bash
RUN_ID=$(~/.claude/scripts/bin/wave-state next-run-id)
WAVE_PLAN_PATH="docs/apply/$RUN_ID/wave-plan.json"
```

Write to `$WAVE_PLAN_PATH`:

```json
{
  "generated_at": "ISO_TIMESTAMP",
  "generated_by": "plan:roadmap",
  "project": "PROJECT_NAME",
  "stack": "STACK",
  "git_sha": "SHORT_SHA",
  "spec_names": ["spec-a", "spec-b", "spec-c"],
  "specs": [
    {
      "name": "spec-a",
      "wave": 1,
      "priority": "P2",
      "phase": 1,
      "feature_area": "auth",
      "files": ["packages/db/src/schema/users.ts", "packages/api/src/routers/auth.ts"],
      "conflicts_with": [],
      "depends_on": [],
      "estimated_loc": 200
    }
  ],
  "conflict_map": {
    "packages/db/src/schema/users.ts": ["spec-a", "spec-c"]
  },
  "dependency_map": {
    "spec-b": ["spec-a"],
    "spec-c": ["spec-b"]
  },
  "waves": {
    "1": ["spec-a", "spec-d"],
    "2": ["spec-b", "spec-e"],
    "3": ["spec-c"]
  },
  "decisions": []
}
```

## Step 7: User Approval

Present the wave plan via `AskUserQuestion` before creating any specs (or after `--dry-run`
analysis):

```
Roadmap: [PROJECT_NAME] ([STACK])
Source: [SOURCE_FILE]

Phase 1 — Foundation (Waves 1-2):
  Wave 1: add-user-schema, add-event-schema  [parallel]
  Wave 2: add-relations, add-indexes  [parallel, after Wave 1]

Phase 2 — Core Features (Waves 3-4):
  Wave 3: add-user-crud, add-event-crud  [parallel]
  Wave 4: add-ticket-flow  [after event-crud]

Phase 3 — Integration (Wave 5):
  Wave 5: add-event-ticketing, add-reporting  [parallel]

Total: [N] specs across [K] waves, [P] phases

Options:
  A) Approve all — generate all specs and wave-plan.json
  B) Approve Phase 1 only — generate Phase 1 specs now, defer later phases
  C) Edit plan — describe changes and regenerate
  D) Dry run — show file conflict details without creating specs
  E) Abort
```

If the user selects C (Edit plan), accept their modifications and re-run Steps 3–6 with the adjusted
plan.

### Recording Decisions

Any user choices during approval — wave reordering, spec removals, split requests, guardrail
overrides, execution strategy changes — MUST be appended to the `decisions` array in
`wave-plan.json` before writing:

```json
{
  "date": "2026-03-24",
  "decision": "Short description of what was decided",
  "rationale": "Why — the user's reasoning or the guardrail that triggered it",
  "source": "user",
  "affected_waves": [4],
  "affected_specs": ["spec-name"]
}
```

| Field | Required | Values |
| ----- | -------- | ------ |
| `date` | yes | ISO date (YYYY-MM-DD) |
| `decision` | yes | What was decided (imperative, 1 line) |
| `rationale` | yes | Why — user quote, guardrail rule, or system detection |
| `source` | yes | `user` (explicit choice), `guardrail` (auto-detected), `system` (computed) |
| `affected_waves` | no | Wave numbers affected |
| `affected_specs` | no | Spec names affected |

Examples of decisions worth recording:
- "Skip single-spec waves 6-11, use /apply instead"
- "Split wave 4 into 4a (8 specs) and 4b (6 specs)"
- "Prioritize wave 5 (bug fixes) before wave 1 (spec debt)"
- "Drop spec X from plan — no longer needed"
- "Override 14-spec guardrail for wave 4 — tasks are trivial (1 each)"

## Step 8: Output Summary

```
/plan:roadmap complete — [PROJECT_NAME]

Stack: [STACK]
Source: [SOURCE_FILE]

Phases: [N]
Specs:  [M] total across [K] waves
Conflicts resolved: [C] file-level conflicts across [F] files

docs/apply/$RUN_ID/wave-plan.json written

Specs created:
  Phase 1: spec-a, spec-b, spec-c
  Phase 2: spec-d, spec-e
  Phase 3: spec-f

Skipped (already existed):
  = spec-x (openspec/changes/spec-x/ exists)

Next: /apply:all to execute all specs in wave order
      /apply [spec-name] to execute a single spec
```

## Step 8.5: Lock Completion

```bash
mkdir -p "$PLAN_DIR/.locks"
echo "locked: $(date -Iseconds)" > "$PLAN_DIR/.locks/roadmap.md.locked"
```

Completion marker for downstream commands (strategy maturity scan).

## Step 8.6: Sync Generated Specs to Beads (mandatory)

Mirrors `commands/feature.md` § Phase 4 spec-sync chaining — every spec `/plan:roadmap` creates
must map into beads, closing the head-leak where a repo with no `.beads/` at fan-out time produces
specs invisible to `bd ready` and the priority model (`openspec-funnel-health`).

```bash
if [ ! -d .beads ]; then
  echo "No .beads/ found — initializing before spec-sync."
  bd init
fi
```

For each spec created in Step 5 (skip specs logged `SKIP: ... (already exists)`):

Execute: `scripts/bin/spec-sync sync <spec-name> --append --json`

`--append` is idempotent — it only syncs tasks lacking a `[beads:xxx]` reference, so re-running
against a spec `/feature --quick` already synced individually (the common case when `.beads/`
already existed) is a safe no-op. This step exists for the case where `.beads/` was absent at
fan-out time — the per-singleton sync inside `/feature` Phase 4 had nothing to attach to; this
catch-all runs it again now that `.beads/` exists.

## Dry Run Mode (`--dry-run`)

Show the full plan without creating any specs or writing `wave-plan.json`:

```
/plan:roadmap --dry-run — [PROJECT_NAME]

Would create [M] specs in [N] waves:

Wave 1 (parallel):
  - add-user-schema  [packages/db/src/schema/users.ts]
  - add-event-schema  [packages/db/src/schema/events.ts]

Wave 2 (after Wave 1):
  - add-relations  [packages/db/src/schema/users.ts, packages/db/src/schema/events.ts]
    CONFLICT with: add-user-schema, add-event-schema → must follow Wave 1

File conflict map:
  packages/db/src/schema/users.ts → [add-user-schema, add-relations]
  packages/db/src/schema/events.ts → [add-event-schema, add-relations]

No files written. Run without --dry-run to generate specs.
```

## Notes

- Spec creation via parallel Agent spawns is eventually consistent — wait for all agents before
  writing the final `wave-plan.json`
- If a spec agent fails, log the failure but continue with remaining specs; the wave-plan will note
  which specs could not be created
- The wave-plan.json format is a superset of both OO's and tree's formats — existing wave-plan.json
  files in those projects remain valid
- `/apply:all` reads `docs/apply/<run-id>/wave-plan.json` via the FR25 resolution chain; no manual intervention needed after this
  command completes
