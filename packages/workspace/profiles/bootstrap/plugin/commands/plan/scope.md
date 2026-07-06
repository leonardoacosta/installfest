---
model: opus
name: plan:scope
description: "Interrogate requirements, challenge assumptions, and lock project scope."
argument-hint: "<context-path>"
effort: high
allowed-tools: Read, Bash, Write, AskUserQuestion, Agent
---

# Project Scope

Interrogate requirements, challenge assumptions, and produce a locked scope document. Extracts
product clarity through two rounds of probing questions with a research sprint in between.

## Arguments

- `$CONTEXT_PATH` - Path to `context.md` from `/project:discover` (required)

---

## Phase 0: Input Guard

Check that the context file exists:

```bash
if [ ! -f "$CONTEXT_PATH" ]; then
  echo "ERROR: Context file not found at $CONTEXT_PATH"
  echo "Run /project:discover first to gather context."
  exit 1
fi
```

Extract `PLAN_DIR` from the context path:

```bash
PLAN_DIR=$(dirname "$CONTEXT_PATH")
```

---

## Phase 0.5: Scan Open Ideas

Check beads for existing ideas that may inform scope decisions:

```bash
IDEAS=$(bd list -l idea --status=open --json 2>/dev/null)
IDEA_COUNT=$(echo "$IDEAS" | jq 'length' 2>/dev/null || echo "0")
```

If ideas exist, present them as prior art before questioning:

```
Prior Ideas ({count} in backlog):
  - {slug}: {description excerpt} (nv-xxx)
  - {slug}: {description excerpt} (nv-xxx)
  ...

These ideas were captured in previous sessions. Consider them as validated
signals when defining scope boundaries — they represent real needs that
surfaced during development.
```

Ideas matching the current project scope should be called out in Phase 2 questions to validate
whether they belong in scope or remain backlogged.

---

## Phase 1: Load Context

Read `$CONTEXT_PATH` and extract:

- **Context tag** (frontend / system / prd)
- **Codebase structure** findings
- **Related work** findings
- **Mode-specific** findings

These feed into question formulation.

---

## Phase 2: Round 1 — Identity Questions

You are a scrutinizing consultant. **Challenge assumptions, expose hidden scope, and force the user
to articulate what they actually want** — not what existing docs happen to say.

Formulate 5-10 probing questions using `AskUserQuestion` across these categories:

| Category | Example Question |
|----------|-----------------|
| **Audience** | "Who are the first 3 humans who will use this daily? What are their job titles?" |
| **Problem** | "What problem does this solve for THEM specifically? (not abstractly)" |
| **Business model** | "SaaS product? Internal tool? Consulting deliverable? One-off project?" |
| **Scope boundary** | "Your existing docs focus on [X]. Is the product exclusively for [X], or is [X] the first vertical?" |
| **Design language** | "What design aesthetic speaks to this audience? Corporate/minimal/playful/editorial?" |
| **Brand personality** | "If this product were a person, how would it introduce itself?" |
| **Existing solutions** | "What exists today that's closest to what you want? Why isn't it good enough?" |
| **Anti-scope** | "What should this explicitly NOT do? What's a feature request you'd reject?" |
| **Success definition** | "What does 'done' look like for v1? What's the one thing it MUST do?" |
| **Timeline pressure** | "Is there a deadline, a demo date, or a client commitment driving timing?" |
| **Data sensitivity** | "What data is too sensitive for the cloud? What compliance regimes apply?" |
| **Scale expectation** | "How many users/projects in year 1? Year 3? Building for 4 people or 400?" |

**Rules:**

- **Challenge, don't confirm** — Don't ask "Is this correct?" Ask "What's the actual boundary?"
- **Expose hidden scope** — "Your docs focus on X. Is the product limited to X, or is X just the first client?"
- **Force specificity** — "Who exactly uses this? Name the roles."
- **Surface constraints** — "What would make this project fail?"
- Use `AskUserQuestion` with 3-4 questions per call. Run multiple rounds if needed.
- Each question should have concrete answer options PLUS options that challenge inferences.

---

## Phase 3: Research Sprint

After Round 1 answers, launch 3 parallel research agents:

```
Task(subagent_type=Explore, run_in_background=true):
  "Audience & Market Validation"
  Research the target audience described by user. Demographics, pain points,
  willingness to pay, existing behavior patterns. WebSearch for market data.

Task(subagent_type=Explore, run_in_background=true):
  "Competitor Deep-Dive"
  Research competitors and alternatives for {product description}.
  Features, pricing, gaps, strengths worth stealing, weaknesses to exploit.
  Build a feature comparison matrix.

Task(subagent_type=Explore, run_in_background=true):
  "Design Trends for Audience"
  Research what design patterns, aesthetics, and UX conventions work
  for {audience type}. What do successful products in this space look like?
```

---

## Phase 4: Round 2 — Generative Interrogation

**This is constructive + challenging. OFFER ideas from research, don't just question.**

After research agents return, synthesize findings and ask a second round:

- "Competitor X has [feature]. Should we incorporate it or differentiate by NOT having it?"
- "Research shows a gap in [area] — should we pivot scope to capture it?"
- "Audience responds to [design pattern] — does this align with your instinct?"
- "There's an adjacent market [Y] — expand scope or stay focused?"
- "Pricing research suggests [model] — does that match your business intent?"
- "Competitor Z charges [price] — where do we position?"

**Key shift:** Round 1 extracts the user's vision. Round 2 enriches it with market reality and
competitive intelligence, potentially reshaping scope.

---

## Phase 5: Scope Lock

After all questions are answered, produce `$PLAN_DIR/scope-lock.md`:

```markdown
# Scope Lock — {EXPLORATION NAME}

## Vision
{1 sentence — what this is}

## Target Users
{specific roles, not "users"}

## Domain
{explicit boundary — what's in, what's out}

## Differentiator
{why this over competitors}

## Features to Steal
{from competitors, if any}

## v1 Must-Do
{the one thing}

## v1 Won't-Do
{explicit exclusions}

## Business Model
{product/tool/deliverable/consulting}

## Brand Direction
{aesthetic, personality, design language}

## Scale Target
{year 1 numbers}

## Hard Constraints
{compliance, hosting, data sensitivity}

## Timeline
{deadline or "no external pressure"}

## Assumptions Corrected
- {assumption} → {correction based on user answers}
- {assumption} → {correction}
```

---

## Phase 6: Lock

Create the lock marker for downstream commands:

```bash
PLAN_DIR=$(dirname "$CONTEXT_PATH")
mkdir -p "$PLAN_DIR/.locks"
touch "$PLAN_DIR/.locks/scope-lock.md.locked"
```

**Lock convention:**

- After producing an artifact, create `.locks/{filename}.locked` marker
- Commands requiring locked inputs check for the marker
- `.locks/` directory lives inside `docs/plan/{name}/.locks/`

**Gate:** User confirms scope lock before proceeding.

Present the scope lock to user via `AskUserQuestion`:

```
SCOPE LOCK — {EXPLORATION NAME}

[show scope-lock.md content]

Confirm scope lock?
  A) Approve — lock scope, proceed to next step
  B) Revise — tell me what to change
```

If user revises, update scope-lock.md and re-present. Only create the `.locked` marker after
approval.

---

## Output

```
PROJECT:SCOPE — Complete
Scope locked: {PLAN_DIR}/scope-lock.md
Lock marker: {PLAN_DIR}/.locks/scope-lock.md.locked

Next steps:
  A) /plan:user-stories {PLAN_DIR}/scope-lock.md  → Personas, flows, wireframes
  B) /plan:strategy {PLAN_DIR}                     → Competitive, financial, marketing
  C) /feature --context={PLAN_DIR}/context.md          → Skip to spec creation
```
