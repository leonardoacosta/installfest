---
name: plan:README
description: "Plan Commands"
disable-model-invocation: true
---

# Plan Commands

A strict sequential pipeline for taking a new product or initiative from raw idea to an executable spec roadmap. Each command locks an artifact that gates the next stage. `strategy` orchestrates the full pipeline as a dashboard or automated DAG.

## Commands

| Command | Description |
|---------|-------------|
| `/plan:scope` | Interrogate requirements via 2-round questioning + research sprint, then lock `scope-lock.md` |
| `/plan:user-stories` | Generate personas, mermaid sequence flows, and navigable wireframe prototype |
| `/plan:design` | Design interrogation → brand identity, CSS tokens, HTML brand board, logo concepts, component samples |
| `/plan:financials` | Revenue model, unit economics, 3-scenario 3-year projections, competitive pricing matrix |
| `/plan:prd` | Accumulate all locked artifacts into a PRD with cross-reference coherence check and ambiguity audit |
| `/plan:scrutiny` | Sharpen deliverables + surface game-changing features + flag technical unknowns (wave-0 spikes); emits a `verdict: GO` lock that gates `/plan:roadmap` |
| `/plan:roadmap` | Parse PRD domain model, generate specs via parallel `/feature --quick` agents, output `docs/apply/<run-id>/wave-plan.json` (run-scoped, FR22) |
| `/plan:strategy` | Maturity dashboard + DAG orchestrator — interactive picker or `--all` automated pipeline |

## Lifecycle

```
/project:discover
        │
        ▼
/plan:scope ──→ scope-lock.md [locked]
        │
   ┌────┴──────────────┐
   ▼         ▼          ▼
user-stories  financials  design
   │         │          │
   └────┬────┴──────────┘
        │
   project:routes (greenfield)
        │
        ▼
/plan:prd ──→ prd.md [locked]
        │
        ▼
/plan:scrutiny ──→ scrutiny.md  [GATE: verdict: GO]
        │   sharpen deliverables · surface game-changing features · flag wave-0 spikes
        ▼
/plan:roadmap ──→ wave-plan.json   (wave 0 = scrutiny's spike specs)
        │
        ▼
/apply:all (execute waves) or /apply <spec>
```

`/plan:strategy` wraps this entire flow: run interactively for a dashboard, or pass `--all` to execute every remaining step automatically with crash-resume support.

## Entry Points

- New product or major initiative with unknown/unclear requirements
- After `/project:discover` produces `context.md`
- Any point mid-pipeline when scope is already locked (jump directly to a fan-out command)
- **`/plan:scope` alone, for a mid-size undertaking** (advisor-plans/027): the full 7-stage
  pipeline is right-sized for a new product or major initiative, not every scope decision. Run
  `/plan:scope` by itself to lock a `scope-lock.md` — the "what we're building and what we
  explicitly excluded" contract — without running user-stories/financials/design/prd/scrutiny/
  roadmap. The structure is good; treating it as all-or-nothing is what buries it (51% of
  prd/roadmap/scope-lock artifacts are never read after creation per the workflow-harmony audit).
  A standalone `scope-lock.md` is still consulted by `/openspec:explore`'s Phase 0 and
  `/feature`'s ambiguity check even without the rest of the pipeline ever running.

## Exit Points

- `/plan:roadmap` produces `docs/apply/<run-id>/wave-plan.json` → `/apply:all` resolves it via the FR25 chain
- `/plan:prd` produces `prd.md` → `/feature` to create individual specs
- `/project:present` assembles locked artifacts into a stakeholder slideshow
