---
model: opus
name: plan:scrutiny
description: >
  Pre-build gate between /plan:prd and /plan:roadmap. Reads the locked PRD, sharpens vague
  deliverables into concrete testable outcomes, runs a divergent game-changing-feature pass, and
  flags technical unknowns that need wave-0 spikes — then emits a verdict lock that gates roadmap
  generation. Catches the big ideas and weak deliverables in planning, not mid-development.
argument-hint: "<plan-dir-path>"
allowed-tools: Bash, Read, Write, Edit, Glob, Grep, AskUserQuestion, Agent
effort: high
---

# Scrutiny — Sharpen Deliverables + Surface Game-Changing Features

A deliberate scrutiny stage that runs AFTER `/plan:prd` and BEFORE `/plan:roadmap`. It does three
things and unifies them into one go/no-go gate:

1. **Sharpen** — turn vague/weak deliverables into concrete, valuable, testable outcomes.
2. **Elevate** — a divergent "what would make this 10x better?" pass that surfaces game-changing
   features the plan is missing, with include/defer/reject decisions.
3. **De-risk** — flag technical unknowns you'd build *wrongly* on if unresolved; each becomes a
   wave-0 spike spec the roadmap must schedule first.

The output is a sharpened deliverable set + a game-changing-feature decision log + a technical
unknowns list, gated by a content-bearing **verdict lock** (`verdict: GO`). Roadmap, strategy, and
advance all refuse to proceed unless the verdict is `GO`.

> Why a distinct stage and not part of `/plan:prd`: the PRD's ambiguity audit catches vague
> **wording** (convergent). Scrutiny interrogates the **substance** — is each deliverable worth
> building, what is the plan *missing*, and what must be de-risked first (divergent). Sharpening
> must happen BEFORE `/plan:roadmap` turns deliverables into specs, or you rework specs.

## Arguments

- `$PLAN_DIR` - Path to plan directory (required, e.g. `docs/plan/{name}`). A path to `prd.md` is
  also accepted; the plan dir is its parent.

---

## Phase 0: Dependency Guard

Scrutiny requires a locked PRD.

```bash
source ~/.claude/scripts/lib/project-guards.sh 2>/dev/null || true
PLAN_DIR="${1%/prd.md}"   # accept either the dir or the prd.md path
[ -d "$PLAN_DIR" ] || { echo "ERROR: Plan directory not found: $PLAN_DIR"; exit 1; }

if [ ! -f "$PLAN_DIR/.locks/prd.md.locked" ]; then
  echo "ERROR: $PLAN_DIR/.locks/prd.md.locked not found. Run /plan:prd first."
  exit 1
fi
```

**Gate 0:** `.locks/prd.md.locked` exists.

---

## Phase 1: Load

Read the locked inputs in full:

- `$PLAN_DIR/prd.md` — the deliverable set + phase structure (primary input).
- `$PLAN_DIR/ambiguity-audit.md` — the wording-level findings (seeds technical-unknown flagging).
- `$PLAN_DIR/scope-lock.md` — scope boundary + anti-scope (a GCF must not violate these).
- `$PLAN_DIR/user-stories.md` — personas + journeys (a GCF should serve a real persona).

Extract:

- The **deliverable list** (functional requirements / v1 Must-Do items).
- Any audit findings marked Critical/High (candidate technical unknowns).
- The locked scope boundary + anti-scope (the rails for the GCF pass).

---

## Phase 2: Sharpen Deliverables (convergent)

For each deliverable, test it on four axes:

| Axis | Question | Fail -> action |
|------|----------|----------------|
| **Concrete** | Is the outcome testable, not "improve X"? | Rewrite to a measurable outcome |
| **Valuable** | Is the user/business value clear and worth the build? | Strengthen or **cut** |
| **Bounded** | Is there a clear done condition? | Add the done condition |
| **Owned** | Is the actor/surface explicit? | Name the actor/surface |

Produce a sharpening table:

```markdown
| # | Deliverable (original) | Issue | Sharpened restatement | Verdict |
|---|------------------------|-------|-----------------------|---------|
| 1 | "improve checkout"     | not concrete/bounded | "checkout completes in <=2 taps; failure shows a ret[...]" | sharpened |
| 2 | "nice-to-have export"  | low value vs effort  | -- | cut |
```

Present the cut/strengthen calls (only the genuinely contested ones) to the user via
`AskUserQuestion`. Do NOT relitigate already-sharp deliverables.

---

## Phase 3: Game-Changing Features (divergent)

An adversarial "10x" pass. Given the locked scope, generate candidate features that would
**materially change the product's value** and that the plan currently misses. Use the
game-changing-feature lens (see `commands/audit/rules/journeys-rubric.md` "Game Changing
Features"). Each candidate MUST stay inside the locked scope boundary (or be explicitly proposed as
a scope amendment, which routes back to `/plan:scope`).

For each candidate:

```markdown
| GCF | Value hypothesis | Effort (rough) | Fits scope? | Risk | Decision |
|-----|------------------|----------------|-------------|------|----------|
| ... | ...              | S/M/L          | yes/amend   | ...  | include-now / defer-vN / reject |
```

Present candidates via `AskUserQuestion` with `include-now / defer / reject` per candidate.
Record every decision (with rationale) in the GCF decision log. Included GCFs become new
deliverables and flow into the roadmap; deferred ones are captured for a future phase (and MAY be
filed as `bd` ideas).

> Restraint: surface 2-5 real candidates, not a brainstorm dump. A game-changing feature is
> high-leverage and plausible within scope — not every idea qualifies. If none clear the bar, say
> so; "no GCF found, plan stands" is a valid outcome.

---

## Phase 4: Technical Unknowns (unified spike flagging)

From the PRD ambiguity audit (Critical/High) plus the sharpened deliverables and included GCFs,
identify **technical unknowns you would build wrongly on if unresolved** — architectural,
isolation, feasibility, or integration questions. Classify each:

| Unknown | Why it blocks | Class |
|---------|---------------|-------|
| "is series-scoping universal?" | wrong answer => cross-tenant leak across 600+ sites | spike-now |
| "which date lib for X" | local, reversible | resolve-in-spec |

- **spike-now** unknowns become **wave-0 spike specs** in the roadmap (gate all later waves).
- **resolve-in-spec** unknowns are noted but do not block.

Each spike-now unknown gets a proposed spec slug (e.g. `spike-<topic>-audit`) and the runtime
evidence it must produce per CORE's Verification Iron Law. Present + confirm via `AskUserQuestion`.

---

## Phase 5: Verdict

Compute the unified verdict:

| Verdict | Condition | Effect |
|---------|-----------|--------|
| **GO** | Deliverables sharpened AND GCFs decided AND every spike-now unknown is enumerated with a wave-0 spec slug | Roadmap may proceed |
| **BLOCKED** | A deliverable/unknown is so fundamental the plan cannot proceed even with a spike (scope is wrong) | Kick back to `/plan:scope` or `/plan:prd` |
| **INCONCLUSIVE** | Decisions still open (user deferred) | Not ready; re-run scrutiny |

Present the verdict for sign-off via `AskUserQuestion` (Approve GO / Revise / mark BLOCKED).
Only write a `GO` lock after explicit approval.

---

## Phase 6: Write Output

1. **Scrutiny artifact:** Write `$PLAN_DIR/scrutiny.md` — the sharpening table, the GCF decision
   log, the technical-unknowns (wave-0 spike list), and the verdict rationale.

2. **Verdict lock (content-bearing — NOT an empty touch):** Write
   `$PLAN_DIR/.locks/scrutiny.md.locked`:

   ```
   verdict: GO            # GO | BLOCKED | INCONCLUSIVE
   deliverables_sharpened: true
   gcf_reviewed: true
   gcf_included: <slug-a, slug-b>      # or none
   spike_specs: <spike-foo-audit, spike-bar-audit>   # wave-0 specs roadmap must emit; or none
   date: <ISO date>
   evidence: docs/plan/<name>/scrutiny.md
   notes: <one-line summary>
   ```

   The gate downstream is: file present AND `grep -qE '^verdict:[[:space:]]*GO\b'`.

**Gate 6:** `scrutiny.md` written, `.locks/scrutiny.md.locked` written with a verdict line. Only
`verdict: GO` unblocks `/plan:roadmap`.

> BLOCKED/INCONCLUSIVE: still write the lock (with that verdict) so the state is recorded and the
> dashboard reflects it — but downstream commands will refuse to proceed. This is the point: a
> `touch`'d empty lock proves nothing; a verdict with an `evidence:` pointer forces scrutiny to
> actually conclude before the pipeline spends hundreds of hours building.

---

## Output

```
plan:scrutiny complete — {name}

Verdict: GO
Deliverables: {N} sharpened, {M} cut
Game-changing features: {I} included, {D} deferred, {R} rejected
Technical unknowns: {S} spike-now -> wave-0 specs [{slugs}], {V} resolve-in-spec

Artifacts:
  {plan-dir}/scrutiny.md
  .locks/scrutiny.md.locked  (verdict: GO)

Next:
  /plan:roadmap {plan-dir}   — generate specs from the sharpened set; wave-0 = the spike specs
```
