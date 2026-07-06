---
model: opus
name: plan:prd
description: Accumulate locked artifacts into comprehensive PRD with ambiguity audit.
argument-hint: "<plan-dir-path>"
effort: high
allowed-tools: Read, Bash, Write, AskUserQuestion
---

# PRD — Accumulate Artifacts into Product Requirements Document

Scan locked artifacts in the plan directory, accumulate them into a comprehensive PRD with
cross-referenced sections, run an ambiguity audit, and score clarity.

## Arguments

- `$PLAN_DIR` - Path to plan directory (required, e.g. `docs/plan/{name}`)

---

## Phase 0: Inventory

Scan `$PLAN_DIR/.locks/` for locked artifacts. List what is available.

```bash
LOCKS_DIR="$PLAN_DIR/.locks"

if [ ! -d "$LOCKS_DIR" ]; then
  echo "ERROR: No .locks/ directory found in $PLAN_DIR"
  echo "No artifacts have been locked yet. Run /plan:scope first."
  exit 1
fi

echo "Locked artifacts:"
ls "$LOCKS_DIR"
```

### Artifact Catalog

| Lock File | Source Artifact | Status |
|-----------|----------------|--------|
| `scope-lock.md.locked` | `$PLAN_DIR/scope-lock.md` | Required |
| `user-stories.md.locked` | `$PLAN_DIR/user-stories.md` + `wireframes/` | Optional |
| `routes.yaml.locked` | `$PLAN_DIR/routes.yaml` | Optional |
| `financial-projections.md.locked` | `$PLAN_DIR/financial-projections.md` | Optional |
| `brand-identity.md.locked` | `$PLAN_DIR/brand/` (incl. `DESIGN.md` — design source of truth) | Optional |
| `infra-plan.md.locked` | `$PLAN_DIR/infra-plan.md` | Optional |

**Gate 0:** At least 1 locked artifact found.

---

## Phase 1: Sparse Guard

If fewer than 2 artifacts are locked, warn the user before proceeding.

```
PRD will be sparse with only {N} artifact(s). Continue?

Locked:
  - scope-lock.md

Missing:
  - user-stories.md (run /plan:user-stories)
  - financial-projections.md (run /plan:financials)
  - brand/ (run /plan:design)

Options:
  A) Continue — generate PRD from available artifacts
  B) Abort — run missing commands first
```

Use `AskUserQuestion` to confirm. If user selects B, exit with the list of commands to run.

**Gate 1:** User confirms continuation (or >= 2 artifacts locked).

---

## Phase 2: Accumulate

Read each locked artifact and map to PRD sections.

### Artifact-to-Section Mapping

| Artifact | PRD Sections |
|----------|-------------|
| `scope-lock.md` | Vision, Target Users, Problem Statement, Success Metrics, Anti-Scope |
| `user-stories.md` | Functional Requirements, User Flows, Acceptance Criteria |
| `routes.yaml` | Page Inventory, Route Architecture, E2E Coverage Gaps, Action Map |
| `financial-projections.md` | Business Case, Revenue Model, Pricing, Unit Economics |
| `brand/` (`DESIGN.md` is the source of truth) | Design Language, UI Specifications, Color System, Typography |
| `infra-plan.md` | Technical Architecture, Deployment, Infrastructure Requirements |

### PRD Structure

```markdown
# Product Requirements Document — {Product Name}

## 1. Vision & Problem Statement
{From scope-lock.md: Vision, Domain, Differentiator}

## 2. Target Users
{From scope-lock.md: Target Users, expanded with user-stories.md personas}

## 3. Success Metrics
{From scope-lock.md: Scale Target, v1 Must-Do}

## 4. Functional Requirements
{From user-stories.md: user flows mapped to requirements}

### 4.1 User Flows
{Mermaid diagrams from user-stories.md}

### 4.2 Acceptance Criteria
{Derived from user flows — testable criteria per flow}

### 4.3 Page Inventory
{From wireframes — page list with purpose and key elements}

## 5. Business Case
{From financial-projections.md: revenue model, projections summary}

### 5.1 Revenue Model
{Revenue streams, pricing mechanism}

### 5.2 Pricing
{Tier structure, competitive positioning}

### 5.3 Unit Economics
{CAC, LTV, margins, payback period}

## 6. Design Language
{From brand/: palette, typography, component patterns}

> **Source of truth:** `DESIGN.md` (in `brand/`, copied to project root by `/project:init`) holds
> the exact, lint-validated token values. This section summarizes; `DESIGN.md` is authoritative for
> any frontend implementation.

### 6.1 Color System
{Primary, secondary, accent, semantic colors}

### 6.2 Typography
{Font families, size scale}

### 6.3 UI Specifications
{Component patterns, interaction states}

## 7. Technical Architecture
{From infra-plan.md: stack, deployment, data model}

## 8. Scope & Constraints

### 8.1 In Scope
{From scope-lock.md: v1 Must-Do, Domain}

### 8.2 Out of Scope
{From scope-lock.md: v1 Won't-Do, Anti-Scope}

### 8.3 Hard Constraints
{From scope-lock.md: compliance, hosting, data sensitivity}

## 9. Timeline
{From scope-lock.md: Timeline, milestone mapping from financials}
```

For each section, note the source artifact. Sections with no source artifact are marked
`[NOT AVAILABLE — run /plan:{command}]`.

---

## Phase 3: Coherence Edit

Cross-reference all sections for internal consistency:

| Check | What to Verify |
|-------|----------------|
| **User ↔ Requirements** | Every persona has at least one user flow; every flow maps to a persona |
| **Requirements ↔ Scope** | No requirement exceeds scope-lock boundary; all v1 Must-Do items have flows |
| **Pricing ↔ Users** | Pricing tiers align with user segments (not pricing enterprise for consumers) |
| **Design ↔ Users** | Design language matches audience expectations (not playful for enterprise) |
| **Architecture ↔ Scale** | Technical choices support the scale target from scope lock |
| **Timeline ↔ Financials** | Break-even timeline is realistic given the implementation timeline |

Fill gaps where one artifact implies information that another section needs. Ensure no section
contradicts another.

---

## Phase 4: Ambiguity Audit

Scan the complete PRD for clarity issues.

### Scan Targets

| Pattern | Category | Severity |
|---------|----------|----------|
| "should", "might", "could", "possibly" | Weasel words | Medium |
| "generally", "typically", "usually" | Hedging | Medium |
| "some", "many", "few", "several", "various" | Vague quantities | High |
| Undefined references (terms used without prior definition) | Undefined terms | High |
| Missing error handling or failure modes | Missing edge cases | High |
| "TBD", "TODO", "to be determined" | Unresolved decisions | Critical |
| "etc.", "and so on", "and more" | Open-ended lists | Medium |
| Passive voice hiding the actor | Unclear responsibility | Low |

### Scoring

Score the PRD 1–10 on clarity:

| Score | Meaning | Action |
|-------|---------|--------|
| 9–10 | Crystal clear — ready for implementation | Proceed |
| 7–8 | Minor ambiguities — document them, proceed | Proceed with audit notes |
| 5–6 | Significant gaps — iterate on worst offenders | Re-edit, re-score |
| 1–4 | Major issues — missing artifacts or contradictions | Abort, run missing commands |

**If score < 8:** Iterate on the worst offenders. Fix weasel words with concrete values. Replace
vague quantities with specific numbers. Define undefined terms. Resolve TBDs. Re-score after each
pass. Maximum 3 iterations.

### Audit Report

For each finding:

```markdown
| # | Location | Pattern | Original | Suggested Fix | Severity |
|---|----------|---------|----------|---------------|----------|
| 1 | §4.1 User Flow 3 | Weasel word | "should display results" | "displays results within 2s" | Medium |
| 2 | §5.2 Pricing | Vague quantity | "some users" | "users on the Pro tier" | High |
```

---

## Phase 5: Write Output

### Artifacts

1. **PRD:**
   Write `$PLAN_DIR/prd.md` — complete product requirements document

2. **Ambiguity audit:**
   Write `$PLAN_DIR/ambiguity-audit.md` — findings table + clarity score

3. **Lock marker:**
   Create `$PLAN_DIR/.locks/prd.md.locked`

**Gate 5:** All files written. Lock marker created. Clarity score >= 7.

---

## Output

```
plan:prd complete

Artifacts accumulated: {N} of 5
  {list of locked artifacts used}

PRD Sections: {N} filled, {M} marked [NOT AVAILABLE]
Clarity Score: {score}/10
Ambiguity Findings: {N} ({critical}, {high}, {medium}, {low})

Artifacts:
  {plan-dir}/prd.md
  {plan-dir}/ambiguity-audit.md
  .locks/prd.md.locked

Next:
  /plan:scrutiny {plan-dir}   Sharpen deliverables + surface game-changing features (gates roadmap)
  Review prd.md for final sign-off
```

> The PRD's ambiguity audit catches vague **wording**. The next stage, `/plan:scrutiny`,
> interrogates the **substance** — sharpens deliverables, surfaces game-changing features, and
> flags technical unknowns that need wave-0 spikes — and emits the `verdict: GO` lock that
> `/plan:roadmap` requires. Critical/High ambiguity-audit findings feed scrutiny's
> technical-unknown flagging.
