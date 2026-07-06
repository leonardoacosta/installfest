---
model: opus
name: plan:financials
description: Generate revenue model, unit economics, and financial projections.
argument-hint: "<scope-lock-path>"
effort: high
allowed-tools: Read, Bash, Write
---

# Financials — Revenue Model, Unit Economics & Projections

Derive revenue streams from the scope lock business model, calculate unit economics, produce 3-year
projections across conservative/moderate/aggressive scenarios, and position pricing against
competitors.

## Arguments

- `$SCOPE_LOCK_PATH` - Path to scope-lock.md (required)

---

## Phase 0: Guard

Check that scope has been locked before proceeding.

```bash
source ~/.claude/scripts/lib/scope-guard.sh
require_locked_scope "$SCOPE_LOCK_PATH"
PLAN_DIR=$(dirname "$SCOPE_LOCK_PATH")
```

**Gate 0:** `.locks/scope-lock.md.locked` exists in plan directory. Error if missing:
"Run /plan:scope first to lock scope."

---

## Phase 1: Load Scope

Read `$SCOPE_LOCK_PATH` and extract:

- **Business Model** — product / tool / deliverable / consulting / marketplace
- **Scale Target** — year 1 user/revenue numbers
- **Hard Constraints** — compliance, hosting, data sensitivity
- **Timeline** — deadline or "no external pressure"
- **Features to Steal** — competitive features with pricing implications
- **Differentiator** — positioning advantage

Store these as working context for Phases 2–5.

---

## Phase 2: Revenue Model

Determine revenue streams from the business model extracted in Phase 1.

### Revenue Stream Identification

| Business Model | Typical Revenue Streams |
|----------------|------------------------|
| SaaS | Subscription tiers (free/pro/enterprise), usage-based add-ons, API access |
| Marketplace | Transaction fees, listing fees, premium placement, subscriptions |
| Consulting | Retainer, project-based, hourly, success fees |
| Product (one-time) | License, support contracts, premium features |
| Hybrid | Primary + secondary streams from above |

### For Each Stream

Define:

- **Pricing mechanism** — flat rate, per-seat, per-unit, tiered, usage-based
- **Price points** — specific dollar amounts with justification
- **Conversion assumptions** — free-to-paid rate, upgrade rate, churn rate
- **Revenue timing** — monthly recurring, annual, one-time, milestone-based

---

## Phase 3: Unit Economics

Calculate key metrics using realistic assumptions based on the scope lock Scale Target.

| Metric | Formula | Notes |
|--------|---------|-------|
| **CAC** (Customer Acquisition Cost) | Marketing spend / New customers | Segment by channel if multiple |
| **LTV** (Lifetime Value) | ARPU * Gross Margin * (1 / Churn Rate) | Use monthly or annual ARPU |
| **LTV:CAC Ratio** | LTV / CAC | Target: > 3:1 |
| **Gross Margin** | (Revenue - COGS) / Revenue | Include hosting, support, payment processing |
| **Payback Period** | CAC / (ARPU * Gross Margin) | Months to recover acquisition cost |
| **MRR per Customer** | Total MRR / Active Customers | By tier if tiered pricing |
| **Net Revenue Retention** | (Start MRR + Expansion - Contraction - Churn) / Start MRR | Target: > 100% |

### Assumption Documentation

Every number must have a stated assumption. No magic numbers.

```markdown
| Assumption | Value | Source |
|------------|-------|--------|
| Monthly churn rate | 5% | Industry average for {category} |
| Free-to-paid conversion | 3% | Conservative for new product |
| Average deal size | $X/mo | Based on competitor pricing analysis |
```

---

## Phase 4: Projections

Produce 3-year projections across 3 scenarios.

### Scenario Definitions

| Scenario | Growth Rate | Churn | Conversion | Assumption |
|----------|-------------|-------|------------|------------|
| **Conservative** | Slow organic | Higher churn | Lower conversion | Minimal marketing, word-of-mouth only |
| **Moderate** | Steady growth | Industry average | Average conversion | Moderate marketing spend |
| **Aggressive** | Rapid scaling | Lower churn | Higher conversion | Significant marketing + product-led growth |

### Granularity

- **Year 1:** Monthly granularity (12 data points)
- **Years 2–3:** Quarterly granularity (8 data points)

### Projection Table (per scenario)

| Period | Users | Paying | MRR | ARR | CAC | LTV | Burn Rate | Runway |
|--------|-------|--------|-----|-----|-----|-----|-----------|--------|
| M1 | ... | ... | ... | ... | ... | ... | ... | ... |
| ... | | | | | | | | |
| Y2-Q1 | ... | ... | ... | ... | ... | ... | ... | ... |

### Key Milestones

For each scenario, identify when:

- Break-even is reached (MRR > costs)
- 100 / 1,000 / 10,000 paying customers
- LTV:CAC ratio exceeds 3:1
- Net revenue retention exceeds 100%

---

## Phase 5: Competitive Pricing

Analyze competitor pricing from scope lock (competitors, features to steal).

### Competitor Pricing Matrix

| Competitor | Free Tier | Pro Tier | Enterprise | Key Differentiator |
|------------|-----------|----------|------------|-------------------|
| Competitor A | ... | ... | ... | ... |
| Competitor B | ... | ... | ... | ... |
| **Our Product** | ... | ... | ... | ... |

### Positioning Strategy

Determine pricing position:

| Strategy | When to Use |
|----------|-------------|
| **Undercut** | Entering crowded market, feature parity, competing on price |
| **Match** | Similar offering, competing on UX/brand/support |
| **Premium** | Unique capability, niche audience, enterprise focus |
| **Freemium** | PLG motion, network effects, high volume |
| **Value-based** | Clear ROI story, measurable customer impact |

Justify the chosen strategy against the scope lock Differentiator and Scale Target.

---

## Phase 6: Write Output

### Artifacts

1. **Financial projections document:**
   Write `docs/plan/{name}/financial-projections.md` containing:
   - Revenue model (from Phase 2)
   - Unit economics with assumption table (from Phase 3)
   - 3-scenario projection tables (from Phase 4)
   - Key milestones per scenario (from Phase 4)
   - Competitive pricing matrix and positioning (from Phase 5)

2. **Lock marker:**
   Create `$PLAN_DIR/.locks/financial-projections.md.locked`

**Gate 6:** All files written. Lock marker created.

---

## Output

```
plan:financials complete

Revenue Streams: {N} ({stream names})
Unit Economics: CAC ${X}, LTV ${Y}, LTV:CAC {Z}:1
Break-even: {scenario} — {month/quarter}

Artifacts:
  docs/plan/{name}/financial-projections.md
  .locks/financial-projections.md.locked

Next:
  /plan:user-stories {scope-lock-path}  User personas + wireframes
  /plan:design {scope-lock-path}        Brand identity + design tokens
  /plan:prd {plan-dir-path}             Accumulate into PRD
```
