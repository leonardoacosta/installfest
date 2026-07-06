---
model: opus
name: plan:strategy
description: "Maturity dashboard and streaming DAG orchestrator for the plan:* pipeline."
argument-hint: "<plan-dir> [--all]"
effort: high
allowed-tools: Read, Bash, Write, AskUserQuestion, Agent
---

# Project Strategy — Pipeline Dashboard & Orchestrator

Maturity dashboard showing artifact completion state across the `plan:*` pipeline, with a
streaming DAG orchestrator that can execute the full pipeline end-to-end.

## Arguments

- `$PLAN_DIR` - Path to plan directory (required)
- `--all` - Execute full pipeline automatically (optional)

---

## Phase 0: Parse Args & Initialize State

Extract arguments and set up orchestrator state:

```bash
PLAN_DIR="$1"
ALL_FLAG=false

for arg in "$@"; do
  case "$arg" in
    --all) ALL_FLAG=true ;;
  esac
done

if [ ! -d "$PLAN_DIR" ]; then
  echo "ERROR: Plan directory not found: $PLAN_DIR"
  echo "Run /project:discover first to create the plan directory."
  exit 1
fi

echo "PLAN_DIR: $PLAN_DIR"
echo "ALL_FLAG: $ALL_FLAG"
```

### State Management (--all mode)

Source orchestrator helpers and initialize state for crash-resume support:

```bash
source ~/.claude/skills/orchestrator-patterns/scripts/orchestrator-helpers.sh

STATE_FILE=$(orch_state_path "strategy-$(basename $PLAN_DIR)")
CURRENT_SHA=$(git rev-parse --short HEAD)
STRATEGY_START_MS=$(date +%s%3N)

if $ALL_FLAG; then
  if orch_check_resume "$STATE_FILE"; then
    echo "Resuming from previous run. Skipping completed phases."
  else
    orch_state_init "$STATE_FILE" "$CURRENT_SHA" \
      "foundation" "fan_out" "synthesis" "scrutiny" "finalization"
  fi
fi
```

---

## Phase 0.5: Scan Open Ideas

Check beads for existing ideas to include in the maturity dashboard:

```bash
IDEAS=$(bd list -l idea --status=open --json 2>/dev/null)
IDEA_COUNT=$(echo "$IDEAS" | jq 'length' 2>/dev/null || echo "0")
```

Include in dashboard output:

```
Ideas:         {count} in backlog (bd list -l idea)
```

When `--all` is set and roadmap phase is reached, pass idea list to `/plan:roadmap` so it can
match ideas against PRD domains as pre-validated spec candidates.

---

## Phase 1: Scan Maturity

Read the plan directory and check which artifacts exist and which are locked.

```bash
PLAN_DIR="$1"
echo "=== Artifact Maturity ==="
for artifact in context.md scope-lock.md user-stories.md routes.yaml financial-projections.md prd.md; do
  EXISTS=$([ -f "$PLAN_DIR/$artifact" ] && echo "yes" || echo "no")
  LOCKED=$([ -f "$PLAN_DIR/.locks/$artifact.locked" ] && echo "locked" || echo "unlocked")
  echo "$artifact: exists=$EXISTS locked=$LOCKED"
done
# roadmap: lock file is primary signal; resolve wave-plan via FR25 chain (active.txt → last.txt → legacy)
ROADMAP_WAVE_PLAN=$(~/.claude/scripts/bin/wave-state resolve --allow-completed 2>/dev/null) || ROADMAP_WAVE_PLAN=""
ROADMAP_EXISTS=$([ -f "$PLAN_DIR/roadmap.md" ] || [ -n "$ROADMAP_WAVE_PLAN" ] && echo "yes" || echo "no")
ROADMAP_LOCKED=$([ -f "$PLAN_DIR/.locks/roadmap.md.locked" ] && echo "locked" || ([ -n "$ROADMAP_WAVE_PLAN" ] && echo "locked" || echo "unlocked"))
echo "roadmap.md: exists=$ROADMAP_EXISTS locked=$ROADMAP_LOCKED"
# scrutiny: lock is content-bearing — present is not enough, the verdict must be GO
SCRUTINY_LOCK="$PLAN_DIR/.locks/scrutiny.md.locked"
if [ ! -f "$SCRUTINY_LOCK" ]; then
  SCRUTINY_STATE="empty"
elif grep -qE '^verdict:[[:space:]]*GO\b' "$SCRUTINY_LOCK"; then
  SCRUTINY_STATE="done"          # verdict: GO — roadmap unblocked
else
  SCRUTINY_STATE="blocked-verdict"  # present but BLOCKED/INCONCLUSIVE — roadmap stays gated
fi
echo "scrutiny.md: exists=$([ -f "$PLAN_DIR/scrutiny.md" ] && echo yes || echo no) state=$SCRUTINY_STATE"
# Check brand/ directory
[ -d "$PLAN_DIR/brand" ] && echo "brand/: exists=yes" || echo "brand/: exists=no"
[ -f "$PLAN_DIR/.locks/brand-identity.md.locked" ] && echo "brand/: locked" || echo "brand/: unlocked"
# Check optional infra-plan
[ -f "$PLAN_DIR/infra-plan.md" ] && echo "infra-plan.md: exists=yes (optional)" || echo "infra-plan.md: exists=no (optional)"
```

### Maturity States

| State | Symbol | Meaning |
|-------|--------|---------|
| Complete | `[done]` | Artifact exists AND locked (scrutiny: lock present AND `verdict: GO`) |
| Draft | `[draft]` | Artifact exists but NOT locked |
| Not started | `[empty]` | Artifact does not exist |
| Blocked | `[blocked]` | Dependencies not yet satisfied |
| Verdict not GO | `[blocked-verdict]` | scrutiny.md.locked present but verdict is BLOCKED/INCONCLUSIVE — roadmap stays gated |

---

## Phase 2: Build DAG

Display the dependency graph of `plan:*` commands with maturity annotations.

### Dependency Graph

```
DAG:
  /project:discover ──→ context.md
                  │
  /plan:scope ──→ scope-lock.md
                        │
           ┌────────────┼────────────┐
           ▼            ▼            ▼
  plan:user-stories  plan:financials  plan:design
           │            │            │
           ▼            │            │
     project:routes     │            │
           │            │            │
           └────────────┴────────────┘
                        │
              plan:prd ──→ prd.md
                        │
            plan:scrutiny ──→ scrutiny.md  [GATE: verdict: GO]
                        │
            plan:roadmap ──→ roadmap.md   [wave 0 = scrutiny's spike specs]

  (optional) project:infra ──→ infra-plan.md  [requires scope-lock.md]
  (standalone) project:routes ──→ routes.yaml [existing projects: no deps]
```

### Node Coloring

Color each node based on its output artifact's maturity:

| Symbol | State | Meaning |
|--------|-------|---------|
| `[done]` | Locked | Artifact complete and locked |
| `[draft]` | Exists, unlocked | Work in progress |
| `[empty]` | Not started | Available if deps satisfied |
| `[blocked]` | Deps missing | Cannot run yet |

### Artifact-to-Command Mapping

| Command | Output Artifact | Dependencies | Pipeline Tier |
|---------|----------------|--------------|---------------|
| `/project:discover` | `context.md` | None | Foundation |
| `/plan:scope` | `scope-lock.md` | `context.md` | Foundation |
| `/plan:user-stories` | `user-stories.md` | `scope-lock.md` | Fan-out (parallel) |
| `/plan:financials` | `financial-projections.md` | `scope-lock.md` | Fan-out (parallel) |
| `/plan:design` | `brand/` | `scope-lock.md` | Fan-out (parallel) |
| `/project:routes` | `routes.yaml` | `user-stories.md` (greenfield) / none (existing) | Post-fan-out |
| `/plan:prd` | `prd.md` | user-stories, financials, design, routes | Synthesis |
| `/plan:scrutiny` | `scrutiny.md` (+ `verdict: GO` lock) | `prd.md` | Scrutiny (gate) |
| `/plan:roadmap` | `roadmap.md` | `prd.md` + scrutiny `verdict: GO` | Finalization |
| `/project:infra` | `infra-plan.md` | `scope-lock.md` | Optional |

Print the DAG with maturity state for each node:

```
Pipeline: {plan-name}

  /project:discover ──→ context.md [done]
                  │
  /plan:scope ──→ scope-lock.md [done]
                        │
           ┌────────────┼────────────┐
           ▼            ▼            ▼
     user-stories   financials     design
       [draft]        [empty]      [done]
           │            │            │
           └────────────┴────────────┘
                        │
                   plan:prd
                     [blocked]
                        │
                 plan:roadmap
                     [blocked]
```

---

## Phase 3: Recommend Next

Based on maturity scan, determine which commands are available to run.

### Availability Rules

A command is **available** when all its dependency artifacts are locked:

```
Available (all deps locked):
  → /plan:user-stories — scope-lock.md is locked
  → /plan:financials — scope-lock.md is locked

Blocked (deps not satisfied):
  ✗ /plan:prd — requires user-stories, financials, design all locked
  ✗ /plan:scrutiny — requires prd.md locked
  ✗ /plan:roadmap — requires scrutiny.md.locked with verdict: GO
```

### Recommendation Logic

1. If nothing exists → recommend `/project:discover`
2. If `context.md` exists but no scope lock → recommend `/plan:scope`
3. If scope is locked → recommend available parallel commands (user-stories, design, financials)
4. If all fan-out artifacts locked → recommend `/plan:prd`
5. If PRD locked → recommend `/plan:scrutiny`
6. If scrutiny present but verdict not GO (`[blocked-verdict]`) → recommend re-running `/plan:scrutiny` (or `/plan:scope` if BLOCKED)
7. If scrutiny `verdict: GO` → recommend `/plan:roadmap`
8. If roadmap locked → recommend `/feature` or `/project:present`

Present recommendation:

```
Recommended next:
  1. /plan:financials docs/plan/{name}/scope-lock.md  (available — scope locked)
  2. /plan:user-stories docs/plan/{name}/scope-lock.md (available — scope locked)

Blocked:
  ✗ /plan:prd — waiting on: financials, user-stories
  ✗ /plan:roadmap — waiting on: prd

Optional:
  → /project:infra docs/plan/{name}/scope-lock.md  (available — scope locked, not in default pipeline)
```

---

## Phase 4: --all Mode

If `--all` flag is set, execute the full pipeline automatically using 4-phase orchestration
with crash-resume support.

### Execution Order

#### 1. Foundation (sequential)

Skip if already completed (crash-resume):

```bash
if [[ "$(orch_state_phase_status "$STATE_FILE" "foundation")" != "completed" ]]; then
  # /project:discover (if no context.md locked)
  # /plan:scope (if no scope-lock.md locked)
  orch_state_complete "$STATE_FILE" "foundation" '{"gate":"passed"}'
  orch_tts_notify "Foundation phase complete."
fi
```

- `/project:discover` (if no `context.md`)
- `/plan:scope` (if no `scope-lock.md`)

#### 2. Fan-out (parallel)

Three independent artifacts dispatched as background agents simultaneously. All depend only
on `scope-lock.md`:

```bash
if [[ "$(orch_state_phase_status "$STATE_FILE" "fan_out")" != "completed" ]]; then
  # Track each agent start
  orch_state_start_node "$STATE_FILE" "fan_out" "user-stories"
  orch_state_start_node "$STATE_FILE" "fan_out" "design"
  orch_state_start_node "$STATE_FILE" "fan_out" "financials"

  # Dispatch 3 parallel agents in a single message
fi
```

### Parallel Fan-Out

Spawn all pending artifacts in a single message:

```bash
# All 3 dispatched in one message = true parallel
Task(run_in_background=true): /plan:user-stories "$SCOPE_LOCK_PATH"
Task(run_in_background=true): /plan:design "$SCOPE_LOCK_PATH"
Task(run_in_background=true): /plan:financials "$SCOPE_LOCK_PATH"
```

> **CC v2.1.139 — `claude agents` dashboard.** The `monitor_lock_files` file-watch gate
> in this phase coordinates fan-out completion via filesystem cooperative signals. As a
> complement, `claude agents s:working` shows the same fan-out as live dashboard rows —
> useful for visual progress checks without breaking the file-watch loop. Background
> dispatches via `claude --bg` make the dashboard rows persistent across the
> orchestrator's `Task` spawns.

Each task checks completion status internally and skips if already locked.

After each agent completes, update state:

```bash
orch_state_update_node "$STATE_FILE" "fan_out" "user-stories" "completed" "user-stories.md locked"
orch_state_update_node "$STATE_FILE" "fan_out" "design" "completed" "brand/ locked"
orch_state_update_node "$STATE_FILE" "fan_out" "financials" "completed" "financial-projections.md locked"
```

On agent failure:

```bash
orch_state_update_node "$STATE_FILE" "fan_out" "$AGENT_NAME" "failed" "Agent timed out or errored"
orch_tts_notify "$AGENT_NAME failed in fan-out phase."
```

#### 3. Gate: Verify Fan-out Complete

Wait for all 3 parallel fan-out agents to produce their lock files before proceeding. Because
this is a streaming DAG orchestrator, the gate MUST be event-driven — dispatch a `Monitor` tool
call that sources the helper library and invokes `monitor_lock_files` against
`$PLAN_DIR/.locks/`. The helper already handles a pre-scan for lock files created before the
watcher started and falls back to a 1s `ls` poll loop when `inotifywait` is missing, so no extra
race-handling logic is needed here.

The fan-out artifact agents still call `orch_state_update_node` on their own internal completion
(see the parallel fan-out section above) — the state file writes are unchanged. The Monitor only
observes the filesystem side-effect (lock file creation) that each agent produces as its last
step.

```typescript
Monitor({
  command: `source ~/.claude/scripts/lib/monitor-helpers.sh && monitor_lock_files "$PLAN_DIR/.locks/" "^(user-stories\\.md|brand-identity\\.md|financial-projections\\.md)\\.locked\$"`,
  timeout_ms: 1800000
});
```

The orchestrator waits for three distinct event lines to arrive — one per lock file
(`user-stories.md.locked`, `brand-identity.md.locked`, `financial-projections.md.locked`) — then
terminates the Monitor and continues to the post-condition check below. `timeout_ms: 1800000`
(30 minutes) is a generous bound for the gate given fan-out agents can take 4–8 minutes each.

On Monitor timeout (30 minutes elapsed without all 3 lock files observed): fall back to the
existing report-and-exit path so the user can fix the stuck agent and re-run to resume:

```bash
# Monitor timeout fallback — report missing lock files (ground truth) and bail
echo "Fan-out incomplete. Cannot proceed to PRD."
for lock in user-stories.md.locked brand-identity.md.locked financial-projections.md.locked; do
  if [ -f "$PLAN_DIR/.locks/$lock" ]; then
    echo "  $lock: present"
  else
    echo "  $lock: MISSING"
  fi
done
orch_tts_notify "Fan-out incomplete. PRD blocked."
# Report and exit — user can fix and re-run to resume
exit 1
```

After Monitor exits successfully, verify lock files exist as a defense-in-depth post-condition
check (the Monitor already guarantees this, but a filesystem race between helper emit and
synthesis phase start is cheap to guard against):

```bash
# Verify lock files exist (defense in depth — Monitor already observed them)
for lock in user-stories.md.locked brand-identity.md.locked financial-projections.md.locked; do
  if [ ! -f "$PLAN_DIR/.locks/$lock" ]; then
    echo "ERROR: Lock file missing: $PLAN_DIR/.locks/$lock"
    exit 1
  fi
done

orch_state_complete "$STATE_FILE" "fan_out" '{"gate":"all_3_locked"}'
orch_tts_notify "Fan-out complete. All 3 artifacts locked. Starting PRD."
```

#### 4. Synthesis (sequential)

```bash
if [[ "$(orch_state_phase_status "$STATE_FILE" "synthesis")" != "completed" ]]; then
  # /plan:prd — depends on all fan-out outputs
  Task(subagent_type=general-purpose):
    Run /plan:prd {PLAN_DIR}
    Verify .locks/prd.md.locked created.

  orch_state_complete "$STATE_FILE" "synthesis" '{"gate":"prd_locked"}'
  orch_tts_notify "PRD complete and locked."
fi
```

#### 4b. Scrutiny Gate (sequential, human sign-off)

The scrutiny stage sharpens deliverables, surfaces game-changing features, and flags technical
unknowns (wave-0 spikes). Unlike the other phases, its verdict is a **product-judgment decision**,
so even in `--all` mode it requires human sign-off before writing a `verdict: GO` lock. This is a
legitimate gate, NOT a fabricated pause — the whole point is to catch weak deliverables and missing
game-changing features before specs are generated.

```bash
if [[ "$(orch_state_phase_status "$STATE_FILE" "scrutiny")" != "completed" ]]; then
  # Run /plan:scrutiny — it does the sharpening + GCF + spike-flag passes, then asks the
  # user to sign off on the verdict (GO / revise / BLOCKED) before writing the lock.
  #   Task or inline: /plan:scrutiny {PLAN_DIR}

  # Gate: the verdict lock must exist AND be GO before finalization runs.
  SCRUTINY_LOCK="$PLAN_DIR/.locks/scrutiny.md.locked"
  if [ ! -f "$SCRUTINY_LOCK" ] || ! grep -qE '^verdict:[[:space:]]*GO\b' "$SCRUTINY_LOCK"; then
    echo "Scrutiny not GO — roadmap blocked. Resolve scrutiny and re-run to resume."
    orch_tts_notify "Scrutiny gate not passed. Roadmap blocked."
    exit 1
  fi

  orch_state_complete "$STATE_FILE" "scrutiny" '{"gate":"verdict_go"}'
  orch_tts_notify "Scrutiny passed (verdict GO). Starting roadmap."
fi
```

#### 5. Finalization (sequential)

```bash
if [[ "$(orch_state_phase_status "$STATE_FILE" "finalization")" != "completed" ]]; then
  # /plan:roadmap — depends on prd.md
  Task(subagent_type=general-purpose):
    Run /plan:roadmap {PLAN_DIR}
    Verify roadmap.md created.

  orch_state_complete "$STATE_FILE" "finalization" '{"gate":"roadmap_complete"}'
  orch_tts_notify "Roadmap generated. Pipeline complete."
fi
```

### Progress Tracking

After each phase, re-run maturity scan to verify locks:

```bash
echo "=== Pipeline Progress ==="
TOTAL=8
DONE=0
for artifact in context.md scope-lock.md user-stories.md routes.yaml financial-projections.md prd.md roadmap.md; do
  [ -f "$PLAN_DIR/.locks/$artifact.locked" ] && DONE=$((DONE + 1))
done
[ -f "$PLAN_DIR/.locks/brand-identity.md.locked" ] && DONE=$((DONE + 1))
# scrutiny counts only when the verdict is GO (a present-but-BLOCKED lock is not "done")
grep -qE '^verdict:[[:space:]]*GO\b' "$PLAN_DIR/.locks/scrutiny.md.locked" 2>/dev/null && DONE=$((DONE + 1))
echo "Progress: $DONE/$TOTAL artifacts locked"
```

### Failure Handling

If any parallel command fails:
- Log the failure via `orch_state_update_node`
- Continue other parallel commands
- Report failures in final summary
- Blocked downstream commands (e.g., `/plan:prd`) will not execute
- On re-run, `orch_check_resume` detects existing state and skips completed agents

### Timing Report

At pipeline completion, print a timing breakdown:

```bash
orch_timing_report "$STATE_FILE"

# Clean up state file after successful completion
rm -f "$STATE_FILE"
```

---

## Interactive Mode (without --all)

When `--all` is not set, display the dashboard and let the user choose.

### Dashboard Display

```
╔══════════════════════════════════════════════════╗
║  Project Pipeline: {name}                        ║
╠══════════════════════════════════════════════════╣
║                                                  ║
║  /project:discover ──→ context.md [done]            ║
║                    │                             ║
║  /plan:scope ──→ scope-lock.md [done]         ║
║                        │                         ║
║       ┌────────────┬───┴───┬────────────┐        ║
║       ▼            ▼       ▼            │        ║
║    stories     finance   design         │        ║
║    [draft]     [empty]   [done]         │        ║
║       │            │       │            │        ║
║       └────────────┴───────┘            │        ║
║                    │                    │        ║
║              plan:prd [blocked]      │        ║
║                    │                    │        ║
║            plan:roadmap [blocked]    │        ║
╚══════════════════════════════════════════════════╝

Progress: 3/7 artifacts locked

Available actions:
  1) /plan:user-stories  — finish draft → lock
  2) /plan:financials    — start
  3) /project:infra         — start (optional, not in default pipeline)
  4) --all                  — run remaining pipeline

Blocked:
  ✗ /plan:prd — waiting on: user-stories, financials
  ✗ /plan:roadmap — waiting on: prd
```

Use `AskUserQuestion` to let user pick an action:

```
Which action would you like to take? (Enter number, command name, or 'all')
```

Execute the selected command and return to the dashboard afterward.

---

## Output

### --all Mode Output

```
/plan:strategy --all complete — {name}

Pipeline executed:
  [done] /project:discover → context.md            (foundation)
  [done] /plan:scope → scope-lock.md         (foundation)
  [done] /plan:user-stories → user-stories.md (fan-out, parallel)
  [done] /plan:financials → financial-projections.md (fan-out, parallel)
  [done] /plan:design → brand/               (fan-out, parallel)
  [done] /plan:prd → prd.md                  (synthesis)
  [done] /plan:scrutiny → scrutiny.md        (scrutiny gate, verdict: GO)
  [done] /plan:roadmap → roadmap.md           (finalization)

All 8/8 artifacts locked.

Timing Report
============================================
| Phase                          | Duration        |
|--------------------------------|-----------------|
| foundation                     | 2m 14s          |
| fan_out                        | 4m 31s          |
| synthesis                      | 3m 05s          |
| finalization                   | 1m 22s          |
|--------------------------------|-----------------|
| TOTAL                          | 11m 12s         |

### Node Breakdown (fan_out)
| Node          | Status       | Duration        |
|---------------|--------------|-----------------|
| user-stories  | completed    | 4m 31s          |
| design        | completed    | 3m 12s          |
| financials    | completed    | 2m 48s          |

Output: docs/plan/{name}/
Next: /feature --context=docs/plan/{name}/context.md  → Create feature specs
      /project:present docs/plan/{name}               → Generate presentation
```

### Interactive Mode Output

```
/plan:strategy — {name}

Progress: {N}/7 artifacts locked

Available:
  → /plan:financials docs/plan/{name}/scope-lock.md
  → /plan:user-stories docs/plan/{name}/scope-lock.md

Blocked:
  ✗ /plan:prd — waiting on: financials, user-stories
  ✗ /plan:roadmap — waiting on: prd

Optional:
  → /project:infra docs/plan/{name}/scope-lock.md

Run with --all to execute remaining pipeline automatically.
```
