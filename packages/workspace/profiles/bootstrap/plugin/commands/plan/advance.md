---
model: opus
name: plan:advance
description: >
  Validate current plan completion, archive it, and bootstrap the next planning phase. Use when
  transitioning from one project phase to the next (e.g., MVP → post-MVP). Validates exit criteria,
  carries forward deferred work, and scaffolds the new plan directory.
argument-hint: "<current-plan-dir> <next-plan-name>"
allowed-tools: Read, Bash, Write, Edit, AskUserQuestion
---

# Plan Advance — Phase Transition

Validate that the current plan is complete, archive it, and bootstrap the next phase with
carry-forward context from deferred work, open specs, and lessons learned.

## Arguments

- `$CURRENT_PLAN` - Path to current plan directory (required)
- `$NEXT_NAME` - Name for the next plan phase (required, kebab-case)
- `--force` - Skip validation gates and advance anyway
- `--dry-run` - Show what would happen without executing

---

## Phase 0: Pre-flight — Clean Working Tree

Before any validation, ensure the working tree is clean and everything is committed:

```bash
# Check for uncommitted changes
DIRTY=$(git status --porcelain | grep -v '^\?\?' | wc -l)
UNTRACKED=$(git status --porcelain | grep '^\?\?' | wc -l)

if [ "$DIRTY" -gt 0 ]; then
  echo "ERROR: $DIRTY uncommitted changes. Commit or stash before advancing."
  git status --short | head -20
  exit 1
fi

if [ "$UNTRACKED" -gt 0 ]; then
  echo "WARNING: $UNTRACKED untracked files. Review before advancing:"
  git ls-files --others --exclude-standard | head -10
  # Ask: stage these or proceed without them?
fi

# Verify pushed to remote
LOCAL=$(git rev-parse HEAD)
REMOTE=$(git rev-parse @{u} 2>/dev/null || echo "no-remote")
if [ "$LOCAL" != "$REMOTE" ]; then
  echo "WARNING: Local is ahead of remote. Pushing..."
  git push
fi
```

**Gate 0:** Working tree clean, all changes committed and pushed.

---

## Phase 1: Validate Current Plan

### Step 1.1: Check Artifact Locks

Verify all required planning artifacts are locked:

```bash
PLAN_DIR="$CURRENT_PLAN"
FAILED=0

for artifact in context.md scope-lock.md prd.md roadmap.md; do
  if [ ! -f "$PLAN_DIR/.locks/$artifact.locked" ]; then
    echo "UNLOCKED: $artifact"
    FAILED=$((FAILED + 1))
  fi
done

# Scrutiny gate: if the plan ran scrutiny, its verdict must be GO. A present-but-non-GO lock
# (BLOCKED/INCONCLUSIVE) means the plan was advanced past an unresolved scrutiny — block it.
SCRUTINY_LOCK="$PLAN_DIR/.locks/scrutiny.md.locked"
if [ -f "$SCRUTINY_LOCK" ] && ! grep -qE '^verdict:[[:space:]]*GO\b' "$SCRUTINY_LOCK"; then
  echo "SCRUTINY NOT GO: $(grep -E '^verdict:' "$SCRUTINY_LOCK" || echo 'verdict: (none)')"
  FAILED=$((FAILED + 1))
fi

if [ "$FAILED" -gt 0 ] && [ "$FORCE" != true ]; then
  echo "ERROR: $FAILED gate(s) unsatisfied. Run /plan:strategy to complete pipeline (incl. /plan:scrutiny)."
  exit 1
fi
```

### Step 1.2: Check Spec Completion

Scan `openspec/changes/` for specs that belong to this plan phase:

```bash
OPEN_SPECS=0
ARCHIVED_SPECS=0
DEFERRED_TASKS=0

for dir in openspec/changes/*/; do
  [ "$(basename "$dir")" = "archive" ] && continue
  [ ! -f "$dir/tasks.md" ] && continue
  open=$(grep -cP '^\- \[ \]' "$dir/tasks.md" 2>/dev/null || echo "0")
  done=$(grep -cP '^\- \[x\]' "$dir/tasks.md" 2>/dev/null || echo "0")
  deferred=$(grep -cP '^\- \[ \].*\[deferred\]' "$dir/tasks.md" 2>/dev/null || echo "0")
  user=$(grep -cP '^\- \[ \].*\[user\]' "$dir/tasks.md" 2>/dev/null || echo "0")

  non_deferred_open=$((open - deferred - user))

  if [ "$non_deferred_open" -gt 0 ]; then
    echo "OPEN: $(basename "$dir") ($non_deferred_open actionable tasks remaining)"
    OPEN_SPECS=$((OPEN_SPECS + 1))
  fi
  DEFERRED_TASKS=$((DEFERRED_TASKS + deferred + user))
done

ARCHIVED_SPECS=$(ls openspec/changes/archive/ 2>/dev/null | wc -l)
```

### Step 1.3: Present Validation Report

```
Plan Validation: {plan-name}
═══════════════════════════════

Artifacts:     {locked}/{total} locked
Specs:         {archived} archived, {open} with open tasks
Deferred:      {deferred} tasks carrying forward
Build status:  {cargo build / pnpm build result}
Test status:   {cargo test / pnpm test result}

{PASS or FAIL with details}
```

**Gate:** All artifacts locked, no non-deferred open tasks (or --force flag).

If open specs have non-deferred tasks, present options:

```
⚠️ {N} specs have open tasks. Options:
  A) Archive as-is — carry deferred tasks to next phase
  B) Apply remaining — execute open specs before advancing
  C) Force — advance anyway (open tasks become deferred in next phase)
```

### Step 1.4: Treadmill Guard (advisor-plans/027 — advisory, never blocking)

Leo may deliberately re-scope a phase — this step warns, it never blocks `--force` or the normal
advance. It answers "how much of what the roadmap promised actually shipped?"

**Scoping caveat:** no marker links `$PLAN_DIR` to the specific `wave-plan.json` run-id(s) it
generated, so this scans every `docs/apply/*/wave-plan.json` on disk (fleet/all-time, not
phase-precise). Precise phase-scoping would need a new linkage mechanism — out of scope for this
step; treat the number as directional, not exact.

```bash
ALL_PLANNED=$(jq -r '.spec_names[]' docs/apply/*/wave-plan.json 2>/dev/null | sort -u)
# grep -c prints "0" on no-match AND exits 1 -- use || true so we keep grep's "0" without
# appending a second one (same footgun documented in scripts/bin/openspec-status).
TOTAL_PLANNED=$(echo "$ALL_PLANNED" | grep -c . || true)
TOTAL_PLANNED="${TOTAL_PLANNED:-0}"

if [ "$TOTAL_PLANNED" -gt 0 ]; then
  SHIPPED=0
  UNSHIPPED_LIST=""
  while IFS= read -r spec; do
    [ -z "$spec" ] && continue
    # find, not `ls path/*glob` -- an unmatched glob throws under zsh's default nomatch
    # behavior even with output redirected (the error fires at shell-level glob
    # expansion, before the command's own stderr redirection ever applies).
    if [ -d "openspec/changes/$spec" ] || \
       find openspec/changes/archive -maxdepth 1 -iname "*$spec" 2>/dev/null | grep -q .; then
      SHIPPED=$((SHIPPED + 1))
    else
      UNSHIPPED_LIST="$UNSHIPPED_LIST $spec"
    fi
  done <<< "$ALL_PLANNED"

  RATE=$((SHIPPED * 100 / TOTAL_PLANNED))
  if [ "$RATE" -lt 50 ]; then
    echo "⚠️  Treadmill guard: only ${RATE}% ($SHIPPED/$TOTAL_PLANNED) of roadmap-planned specs"
    echo "   ever landed a spec dir (open or archived) — fleet/all-time scan, not phase-precise."
    echo "   Never-created specs:$UNSHIPPED_LIST"
  fi
fi
```

---

## Phase 1.5: Reconcile Unplanned Specs

Specs are often added mid-phase that weren't in the original roadmap. Before archiving, reconcile
the archive against the plan's roadmap to capture everything that was actually delivered.

### Step 1.5.1: Scan for Unplanned Specs

Compare archived specs against the plan's roadmap:

```bash
PLAN_DIR="$CURRENT_PLAN"
ROADMAP="$PLAN_DIR/roadmap.md"

# Extract spec names from roadmap (lines matching `add-*` or `fix-*` or similar kebab-case IDs)
PLANNED_SPECS=$(grep -oP '`[a-z][-a-z0-9]+`' "$ROADMAP" 2>/dev/null | tr -d '`' | sort -u)

# Get all archived specs (strip date prefix: 2026-03-23-add-foo → add-foo)
ARCHIVED_SPECS=$(ls openspec/changes/archive/ 2>/dev/null | sed 's/^[0-9]\{4\}-[0-9]\{2\}-[0-9]\{2\}-//' | sort -u)

# Find specs archived but NOT in the roadmap
UNPLANNED=$(comm -23 <(echo "$ARCHIVED_SPECS") <(echo "$PLANNED_SPECS"))
UNPLANNED_COUNT=$(echo "$UNPLANNED" | grep -c '[a-z]' || echo "0")
```

### Step 1.5.2: Present Reconciliation Report

If unplanned specs are found:

```
📋 Spec Reconciliation
═══════════════════════
Roadmap specs:    {N} planned
Archived specs:   {M} total
Unplanned:        {K} specs added mid-phase

Unplanned additions:
  + add-service-diagnostics      (Checkable trait, nv check CLI)
  + add-neon-management-tools    (Neon API projects/branches/compute)
  + add-teams-graph-tools        (MS Graph Teams integration)
  + fix-persistent-subprocess    (CC 2.1.81 stream-json fix)
  + rewrite-mobile-formatters    (Telegram mobile-friendly output)
  + fix-tool-result-strip        (leaked tool artifact cleanup)
  ...

Options:
  A) Append all to roadmap — update roadmap.md with "Unplanned Additions" section
  B) Select — choose which to include
  C) Skip — archive without reconciling (unplanned specs are still in the archive)
```

### Step 1.5.3: Append to Roadmap

If user chooses A or B, append an "Unplanned Additions" section to `roadmap.md`:

```bash
if [ "$UNPLANNED_COUNT" -gt 0 ]; then
  # Default to Option A (Step 1.5.2): append all unplanned specs. For Option B,
  # replace SELECTED_SPECS with the user's chosen subset.
  SELECTED_SPECS="$UNPLANNED"
  PLANNED_COUNT=$(echo "$PLANNED_SPECS" | grep -c '[a-z]')
  cat >> "$ROADMAP" << EOF

## Unplanned Additions

Specs added mid-phase that were not in the original roadmap:

$(for spec in $SELECTED_SPECS; do
  # Extract summary from archived proposal
  PROPOSAL="openspec/changes/archive/*-${spec}/proposal.md"
  SUMMARY=$(grep -A1 '## Summary' $PROPOSAL 2>/dev/null | tail -1)
  echo "- \`$spec\` — $SUMMARY"
done)

Total: ${#SELECTED_SPECS[@]} unplanned specs delivered alongside ${PLANNED_COUNT} planned.
EOF

  git add "$ROADMAP"
  echo "Updated roadmap.md with ${#SELECTED_SPECS[@]} unplanned additions."
fi
```

### Step 1.5.4: Update Wave Plan (if exists)

If a wave plan exists (in-flight or completed), reference it in the completion summary. Resolve via the FR25 chain — `--allow-completed` lets us also read forensic-resume markers:

```bash
WAVE_PLAN=$(~/.claude/scripts/bin/wave-state resolve --allow-completed 2>/dev/null) || WAVE_PLAN=""
if [ -n "$WAVE_PLAN" ] && [ -f "$WAVE_PLAN" ]; then
  WAVE_COUNT=$(jq '.waves | length' "$WAVE_PLAN" 2>/dev/null || echo "0")
  WAVE_SPECS=$(jq '[.waves[].specs[].name] | length' "$WAVE_PLAN" 2>/dev/null || echo "0")
  WAVE_STATUS=$(jq -r '.status' "$WAVE_PLAN" 2>/dev/null || echo "unknown")
  echo "Wave plan: $WAVE_COUNT waves, $WAVE_SPECS specs ($WAVE_STATUS)"
fi
```

**Gate 1.5:** Unplanned specs reconciled (appended, selected, or skipped).

---

## Phase 2: Collect Carry-Forward Context

Gather everything the next phase needs to know:

### Step 2.0: Open Ideas Inventory

Scan beads for ideas that should carry forward to the next phase:

```bash
IDEAS=$(bd list -l idea --status=open --json 2>/dev/null)
IDEA_COUNT=$(echo "$IDEAS" | jq 'length' 2>/dev/null || echo "0")
```

Include in the carry-forward context.md for the next phase:

```markdown
## Carry-Forward: Open Ideas ({count})

| Slug | ID | Description |
|------|-----|-------------|
| {slug} | {id} | {description excerpt} |
...
```

These ideas represent validated needs from previous sessions. The next phase's `/plan:scope`
and `/plan:roadmap` will read them as prior art and spec candidates.

### Step 2.1: Deferred Task Inventory

Extract all `[deferred]` and `[user]` tasks from open specs:

```markdown
## Carry-Forward: Deferred Tasks

### From jira-integration

- Retry wrapper with exponential backoff
- Callback handlers (edit, cancel, expiry)
- Integration tests

### From telegram-channel

- Integration test + manual e2e

### From nexus-integration

- Wire error callbacks
```

### Step 2.2: Lessons Learned

Read archived specs for patterns that worked:

- Which agent types were effective?
- What gates caught real issues?
- What was over-engineered? What was under-engineered?
- What deferred tasks should have been done in MVP?

### Step 2.3: Runtime Observations

If the project has a running service, capture:

- Health endpoint status
- Recent error patterns from logs
- Memory/performance observations
- User feedback from bootstrap/conversations

---

## Phase 3: Archive Current Plan

### Step 3.1: Create Archive Entry

```bash
ARCHIVE_DIR="docs/plan/archive"
TIMESTAMP=$(date +%Y-%m-%d)
ARCHIVE_NAME="${TIMESTAMP}-$(basename "$CURRENT_PLAN")"

mkdir -p "$ARCHIVE_DIR"
cp -r "$CURRENT_PLAN" "$ARCHIVE_DIR/$ARCHIVE_NAME"
```

### Step 3.2: Delete Original Plan Directory

After successful copy to archive, remove the original to avoid stale duplicates:

```bash
rm -rf "$CURRENT_PLAN"
echo "Deleted original plan directory: $CURRENT_PLAN"
```

The archive in `docs/plan/archive/` is now the single source of truth for this phase.

### Step 3.3: Write Completion Summary

Write `$ARCHIVE_DIR/$ARCHIVE_NAME/COMPLETION.md`:

```markdown
# Plan Completion: {name}

## Phase: {MVP / Post-MVP / v2 / etc.}

## Completed: {date}

## Duration: {start → end}

## Delivered (Planned)

- {list of completed roadmap specs}

## Delivered (Unplanned)

- {specs added mid-phase, from reconciliation step}

## Deferred

- {carried forward to next phase}

## Metrics

- LOC: {total}
- Tests: {count}
- Specs: {archived}/{total}

## Lessons

- {what worked}
- {what didn't}
```

---

## Phase 4: Bootstrap Next Phase

### Step 3.3: Commit, Tag, and Push

Stage all archive artifacts, commit with a conventional message, tag the commit, and push:

```bash
PLAN_NAME=$(basename "$CURRENT_PLAN")
TAG_NAME="${PLAN_NAME}-complete"

# Stage archive + deletion of original
git add docs/plan/archive/ openspec/
git rm -r --cached "$CURRENT_PLAN" 2>/dev/null || true
git add -A docs/plan/
git commit -m "chore(plan): archive ${PLAN_NAME} — phase complete"

# Tag the commit
git tag -a "$TAG_NAME" -m "Plan phase complete: ${PLAN_NAME}

Artifacts: all locked
Specs: $(ls docs/plan/archive/${ARCHIVE_NAME}/ | wc -l) archived
Deferred: ${DEFERRED_TASKS} tasks carried forward
Completion: docs/plan/archive/${ARCHIVE_NAME}/COMPLETION.md"

# Push commit + tag
BRANCH=$(git rev-parse --abbrev-ref HEAD)
git push origin "$BRANCH" --tags
```

**Tag format:** `{plan-name}-complete` (e.g., `master-agent-harness-complete`)

The tag provides a permanent reference point: `git show master-agent-harness-complete` shows exactly
what was delivered, deferred, and learned at the end of that phase.

---

### Step 4.1: Scaffold New Plan Directory

```bash
NEXT_DIR="docs/plan/$NEXT_NAME"
mkdir -p "$NEXT_DIR/.locks"
```

### Step 4.2: Write Context

Create `$NEXT_DIR/context.md` with:

- Previous phase summary (from COMPLETION.md)
- Carry-forward deferred tasks
- Current codebase state (LOC, tests, architecture)
- Runtime observations
- Open questions from previous phase

### Step 4.3: Present Next Steps

```
Plan Advanced: {current} → {next}

Archive:    docs/plan/archive/{date}-{current-name}/
New plan:   docs/plan/{next-name}/
Context:    docs/plan/{next-name}/context.md

Carry-forward:
  {N} deferred tasks from {M} specs
  {N} open specs (not yet applied)

Next steps:
  A) /plan:strategy docs/plan/{next-name}  → Full pipeline
  B) /plan:scope docs/plan/{next-name}/context.md  → Lock next phase scope
```

---

## Output

```
plan:advance complete

Previous: docs/plan/{current}/ → archived to docs/plan/archive/{date}-{current}/ (original deleted)
Next:     docs/plan/{next}/ (context.md written)

Validation: PASS ({artifacts} locked, {specs} archived)
Carry-forward: {N} deferred tasks, {M} open specs
Completion: docs/plan/archive/{date}-{current}/COMPLETION.md

Next steps:
  A) /plan:strategy docs/plan/{next-name}
  B) /plan:scope docs/plan/{next-name}/context.md
```
