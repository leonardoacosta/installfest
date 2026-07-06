---
model: opus
name: plan:design
description: Generate brand identity, design tokens, and visual system through guided interrogation.
argument-hint: "<scope-lock-path>"
effort: high
allowed-tools: Read, Bash, Write, AskUserQuestion, Skill, Agent
---

# Design — Brand Identity, Tokens & Visual System

Generate a complete brand identity through design-focused interrogation, produce design tokens as
CSS custom properties, and render an HTML brand board with palette, typography, and component
samples.

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

- **Brand Direction** — aesthetic, personality, design language
- **Target Users** — specific roles (informs color psychology, density preferences)
- **Differentiator** — what makes this product unique (informs visual identity)
- **Domain** — product category (informs design conventions)
- **Business Model** — enterprise vs consumer (informs formality level)

Store these as working context for Phases 2–4.

---

## Phase 2: Design Interrogation

Use the `frontend-design` skill for design-focused interrogation. Ask targeted questions that drive
concrete visual decisions.

### Question Categories

| Category | Example Questions |
|----------|-------------------|
| **Color Psychology** | "Your target users are {roles}. Do you want to project trust (blues), energy (oranges/reds), sophistication (dark neutrals), or something else?" |
| **Typography** | "Serif (editorial, premium), sans-serif (modern, clean), or monospace (technical, developer)? Mixed heading/body?" |
| **Layout Density** | "Dense/data-heavy (dashboards, tables) or spacious/editorial (marketing, content)?" |
| **Animation/Motion** | "Minimal (instant transitions), subtle (200ms eases), or expressive (spring physics, page transitions)?" |
| **Dark Mode Strategy** | "Dark-first, light-first with dark toggle, system-preference auto, or light-only?" |
| **Visual References** | "Name 2–3 products whose look-and-feel you admire. What specifically do you like about each?" |
| **Personality** | "If this product had a voice, is it formal/authoritative, friendly/casual, playful/bold, or minimal/quiet?" |

Use `AskUserQuestion` with 3–4 questions per round. Run 1–2 rounds until design direction is clear.

**Gate 2:** User has answered enough questions to derive a coherent visual direction.

---

## Phase 2.5: Design Intelligence Lookup

Before generating brand identity, query `ui-ux-pro-max` for industry-specific recommendations:

```
Skill({ skill: "ui-ux-pro-max" })
```

Using the product type and industry from Phase 2 answers, extract:

- **Recommended UI style** (from 67 styles: glassmorphism, brutalism, soft UI, etc.)
- **Color palette** (industry-specific, WCAG-compliant)
- **Typography pairing** (with Google Fonts URL and Tailwind config)
- **Landing page pattern** (section order, CTA placement)
- **Anti-patterns** (what to avoid for this industry)

Feed these recommendations into Phase 3 as **constraints, not mandates** — `frontend-design` in
Phase 3.5 may override style choices for aesthetic reasons, but should justify deviations from
industry data.

**Gate 2.5:** Design intelligence lookup returned actionable recommendations for style, colors,
and typography. If the skill returns no results, proceed without constraints.

---

## Phase 3: Brand Identity + CHECKPOINT 1

### 3.1 Brand Research

Launch a background research agent before generating:

```
Task(subagent_type=Explore, run_in_background=true):
  Research visual benchmarks for the brand context extracted from scope lock:
  - What colors dominate in this market category?
  - What typography conventions exist (editorial vs. utility vs. playful)?
  - What brands succeeded/failed with this audience visually?
  - Find 3-5 reference brands that share the target user profile.

  Return findings as markdown. DO NOT write files.
```

### 3.2 Generate Brand Identity

Invoke the `frontend-design` skill for naming, voice, and visual direction:

```
Skill({ skill: "frontend-design" })
```

Using the skill output + scope lock brand direction + research findings, generate:

**Identity Document:** `$OUTPUT_DIR/brand-identity.md`

```markdown
# Brand Identity: {Product Name}

## Name & Tagline
- Product name (if not specified in scope lock)
- Tagline: [5-8 word promise statement]
- Elevator pitch: [one sentence for a business card]

## Voice & Tone
- Personality: [3-5 adjectives with brief explanation]
- Tone register: [formal/casual/technical/editorial/playful]
- Writing style: [active/passive, sentence length, vocabulary level]
- Anti-patterns: [what this brand would NEVER sound like]

## Color Palette
| Role | Hex | Usage |
|------|-----|-------|
| Primary | #XXXXXX | CTAs, hero elements, brand marks |
| Secondary | #XXXXXX | Supporting accents, highlights |
| Neutral | #XXXXXX | Text, borders, backgrounds |
| Surface | #XXXXXX | Card backgrounds, panels |
| Error | #XXXXXX | Validation, alerts |
| Success | #XXXXXX | Confirmations, positive states |

## Typography System
- Display font: [Name] — [where used, why chosen]
- Body font: [Name] — [where used, why chosen]
- Mono font: [Name] — [where used, why chosen]
- Scale: [H1 size / H2 / H3 / body / small]

## Design Principles
1. [Principle 1] — [brief description]
2. [Principle 2] — [brief description]
3. [Principle 3] — [brief description]
```

### 3.3 Color Palette

Generate a complete palette with semantic roles:

| Token | Role | Example |
|-------|------|---------|
| `--color-primary` | Brand identity, CTAs, key actions | `#2563EB` |
| `--color-primary-hover` | Interactive state | Darken 10% |
| `--color-secondary` | Supporting elements, secondary actions | Complementary hue |
| `--color-accent` | Highlights, badges, notifications | Contrasting pop |
| `--color-neutral-50` through `--color-neutral-900` | Backgrounds, borders, text | Gray scale |
| `--color-success` | Positive feedback | Green variant |
| `--color-warning` | Caution states | Amber variant |
| `--color-error` | Error states, destructive actions | Red variant |
| `--color-info` | Informational | Blue variant |

All colors must pass WCAG AA contrast ratio (4.5:1) against their intended background.

### 3.4 Typography Scale

| Token | Role | Example |
|-------|------|---------|
| `--font-heading` | Headings (h1-h6) | `'Inter', sans-serif` |
| `--font-body` | Body text, labels | `'Inter', sans-serif` |
| `--font-code` | Code, technical content | `'JetBrains Mono', monospace` |
| `--font-size-xs` through `--font-size-4xl` | Size scale | Modular scale (1.25 ratio) |
| `--line-height-tight` / `--line-height-normal` / `--line-height-relaxed` | Vertical rhythm | 1.25 / 1.5 / 1.75 |

### 3.5 HTML Brand Board

A single-page HTML file showcasing the complete visual system:

- Color swatches with hex values, contrast ratios, and copy-to-clipboard
- Typography samples at each scale step
- Voice examples: 3 sample micro-copy strings in brand voice
- Personality panel: adjectives with visual indicators
- Logo placeholder zone: text-based logomark using chosen typography
- Dark mode toggle (if applicable)
- Spacing/sizing reference

Apply `frontend-design` skill standards for the HTML:

```
Skill({ skill: "frontend-design" })
```

Iterate 2-3x until the board is coherent and polished.

### 3.6 Checkpoint 1: Identity Review

Present to user:

```
======================================================
BRAND: Identity Complete
======================================================

Files:
  $OUTPUT_DIR/brand-identity.md    (voice, typography, color rationale)
  $OUTPUT_DIR/brand-board.html     (open in browser to review)
  $OUTPUT_DIR/palette.svg          (exportable palette)

Summary:
  Colors: {primary} / {secondary} / {neutral}
  Display font: {font name}
  Voice: {3 adjectives}
  Tagline: "{tagline}"

Checkpoint 1: Identity Review
  Approve  -> continues to Brand Visuals (logo, icon style, components)
  Reject   -> tell me what to change (colors? typography? voice?)
  Partial  -> approve some, reject others
```

Wait for user verdict. If rejected or partial, iterate with specific feedback before continuing.

**Gate 3:** Identity approved by user

---

## Phase 4: Design Tokens — Canonical DESIGN.md + Derived Outputs

The **canonical, authored** design artifact is a `DESIGN.md` file in the
[`google-labs-code/design.md`](https://github.com/google-labs-code/design.md) format (YAML token
frontmatter + spec-ordered prose). `tokens.css`, the Tailwind theme, and `tokens.json` are
**derived** from it — generated, never hand-edited. This eliminates two-source drift and gives the
plan a lintable, agent-portable design contract that the CLAUDE.md template, `frontend-design`
skill, and UI agents all consume as the source of truth.

### 4.1 Author `DESIGN.md` (canonical)

Write `docs/plan/{name}/brand/DESIGN.md` from the Phase 2 interrogation answers and approved
Phase 3 identity. Map the brand-identity values into the token schema:

```md
---
version: alpha
name: {Product Name}
description: {one-line aesthetic direction}
colors:
  primary: "#2563EB"
  on-primary: "#FFFFFF"
  secondary: "#7C3AED"
  neutral: "#FAFAFA"
  surface: "#FFFFFF"
  on-surface: "#171717"
  error: "#DC2626"
  success: "#16A34A"
typography:
  headline-lg:
    fontFamily: {Display Font}
    fontSize: 36px
    fontWeight: 700
    lineHeight: 1.1
    letterSpacing: -0.02em
  body-md:
    fontFamily: {Body Font}
    fontSize: 16px
    fontWeight: 400
    lineHeight: 1.6
  label-sm:
    fontFamily: {Body Font}
    fontSize: 12px
    fontWeight: 500
    lineHeight: 1
rounded:
  sm: 4px
  md: 8px
  lg: 12px
  full: 9999px
spacing:
  xs: 4px
  sm: 8px
  md: 16px
  lg: 32px
  xl: 64px
components:
  button-primary:
    backgroundColor: "{colors.primary}"
    textColor: "{colors.on-primary}"
    typography: "{typography.label-sm}"
    rounded: "{rounded.md}"
    padding: "{spacing.md}"
---

## Overview
{Brand personality, target audience, emotional intent — from Phase 2/3}

## Colors
{Semantic role of each palette — primary drives CTAs, neutral the page ground, etc.}

## Typography
{Usage guidance per level — display vs body vs label}

## Layout
{Grid model + spacing strategy}

## Shapes
{Corner-radius philosophy}

## Components
{Per-component style notes for button/input/card variants}

## Do's and Don'ts
- Do use the primary color for the single most important action per screen
- Don't mix rounded and sharp corners in the same view
- Do maintain WCAG AA contrast (4.5:1 normal text)
```

Sections present MUST follow spec order: Overview, Colors, Typography, Layout, Elevation & Depth,
Shapes, Components, Do's and Don'ts (any may be omitted).

### 4.2 Lint gate (graceful-degrade)

Validate the authored DESIGN.md before locking. Surface any `severity: error` findings to the
user; degrade silently when the CLI is unavailable (offline machines):

```bash
npx --yes @google/design.md lint docs/plan/{name}/brand/DESIGN.md 2>/dev/null || \
  echo "design.md CLI unavailable — skipping lint gate (non-blocking)"
```

The linter checks broken token references, WCAG contrast on component color pairs, orphaned
tokens, and section order — a free validation pass over the hand-derived WCAG checks below.

### 4.3 Derive `tokens.css` and Tailwind theme (generated)

Generate the consumable token files **from** DESIGN.md. Published `@google/design.md` v0.1.1
(verified 2026-05-24) emits Tailwind v3 `theme.extend` JSON and W3C DTCG:

```bash
# Tailwind v3 theme.extend JSON (published)
npx --yes @google/design.md export --format tailwind docs/plan/{name}/brand/DESIGN.md > docs/plan/{name}/brand/tailwind.theme.json
# W3C Design Tokens (published)
npx --yes @google/design.md export --format dtcg docs/plan/{name}/brand/DESIGN.md > docs/plan/{name}/brand/tokens.json
```

For the **Tailwind v4 `@theme`** target (the T3 fleet), emit the CSS block directly from the same
token values you just authored — the published CLI does not yet ship `css-tailwind` (it exists in
upstream `main`; adopt `export --format css-tailwind` once published). The derived `tokens.css`
mirrors the DESIGN.md tokens:

```css
:root {
  /* Colors — Primary */
  --color-primary: #2563EB;
  --color-primary-hover: #1D4ED8;
  --color-primary-light: #DBEAFE;

  /* Colors — Neutral */
  --color-neutral-50: #FAFAFA;
  --color-neutral-100: #F5F5F5;
  /* ... full scale ... */
  --color-neutral-900: #171717;

  /* Colors — Semantic */
  --color-success: #16A34A;
  --color-warning: #D97706;
  --color-error: #DC2626;
  --color-info: #2563EB;

  /* Typography */
  --font-heading: 'Inter', system-ui, sans-serif;
  --font-body: 'Inter', system-ui, sans-serif;
  --font-code: 'JetBrains Mono', ui-monospace, monospace;

  /* Font sizes */
  --font-size-xs: 0.75rem;
  --font-size-sm: 0.875rem;
  --font-size-base: 1rem;
  --font-size-lg: 1.125rem;
  --font-size-xl: 1.25rem;
  --font-size-2xl: 1.5rem;
  --font-size-3xl: 1.875rem;
  --font-size-4xl: 2.25rem;

  /* Spacing */
  --space-1: 0.25rem;
  --space-2: 0.5rem;
  --space-3: 0.75rem;
  --space-4: 1rem;
  --space-6: 1.5rem;
  --space-8: 2rem;
  --space-12: 3rem;
  --space-16: 4rem;

  /* Border radius */
  --radius-sm: 0.25rem;
  --radius-md: 0.375rem;
  --radius-lg: 0.5rem;
  --radius-xl: 0.75rem;
  --radius-full: 9999px;

  /* Shadows */
  --shadow-sm: 0 1px 2px rgba(0, 0, 0, 0.05);
  --shadow-md: 0 4px 6px rgba(0, 0, 0, 0.1);
  --shadow-lg: 0 10px 15px rgba(0, 0, 0, 0.1);

  /* Transitions */
  --transition-fast: 150ms ease;
  --transition-base: 200ms ease;
  --transition-slow: 300ms ease;
}

/* Dark mode (if applicable) */
[data-theme="dark"] {
  --color-neutral-50: #171717;
  --color-neutral-900: #FAFAFA;
  /* ... inverted scale ... */
}
```

Actual values must reflect the interrogation answers from Phase 2, not these defaults — and MUST
match the canonical `DESIGN.md` token values exactly (this file is a derived mirror, not a second
source of truth). Mark it generated; never hand-edit `tokens.css` independently of `DESIGN.md`.

---

## Phase 5: Brand Visuals + CHECKPOINT 2

### 5.1 Generate Logo Concepts

Invoke the design system skill for component and visual conventions:

```
Skill({ skill: "design-system-starter" })
Skill({ skill: "frontend-design" })
```

Using approved identity tokens + design system conventions, generate:

**Logo Concepts:** `$OUTPUT_DIR/logo-concepts.html`

Self-contained HTML showing 3 logo direction options:
- Option A: Wordmark -- refined typography treatment
- Option B: Icon + wordmark -- geometric symbol + type
- Option C: Lettermark -- monogram or abbreviation treatment

Each option rendered at 4 sizes (32px, 64px, 128px, 256px) on both light and dark backgrounds.
SVG-based, scales cleanly. Iterate 2-3x on each option.

### 5.2 Icon Style Guide

**Icon Style Guide:** `$OUTPUT_DIR/icon-style.md`

```markdown
# Icon Style Guide

## Style
- Type: [line / filled / duotone / mixed]
- Corner radius: [sharp / slightly rounded / circular]
- Stroke weight: [1px / 1.5px / 2px]
- Size grid: [16px / 20px / 24px / 32px]

## Personality Fit
[Why this icon style matches brand personality]

## Reference Icons
[5-6 icons demonstrating the style as inline SVG or data URIs]
```

### 5.3 Component Samples

**Component Samples:** `$OUTPUT_DIR/components.html`

Self-contained HTML showing 8-10 key UI components in brand style:

- **Buttons** -- primary, secondary, ghost, destructive (all states: default, hover, active, disabled)
- **Text input** with label + validation states
- **Card** component (with image placeholder, title, body, action)
- **Badge / tag / pill** variants
- **Navigation item** (active + inactive)
- **Alert / notification** component
- **Select, checkbox, radio** (all states: default, focus, error, disabled)
- **Typography** -- heading hierarchy (h1-h4), body text, small text, code

Use approved tokens.css. Apply both light mode and dark mode variants. Iterate 2-3x for polish.

### 5.4 Checkpoint 2: Visuals Review

Present to user:

```
======================================================
BRAND: Visuals Complete
======================================================

Files:
  $OUTPUT_DIR/logo-concepts.html   (3 logo direction options)
  $OUTPUT_DIR/icon-style.md        (icon style specifications)
  $OUTPUT_DIR/components.html      (8-10 UI component samples)

Checkpoint 2: Visuals Review
  Approve  -> brand package complete, unlocks dependents
  Reject   -> tell me what to change
  Partial  -> approve some options, reject others
```

Wait for user verdict. If rejected or partial, iterate with specific feedback.

**Gate 5:** Visuals approved by user

---

## Phase 6: Write Output

### Artifacts

1. **Design system (CANONICAL):**
   Write `docs/plan/{name}/brand/DESIGN.md` -- the normative design source of truth (YAML tokens +
   prose). All other token files derive from this. Lint-validated in Phase 4.2.

2. **Brand identity:**
   Write `docs/plan/{name}/brand/brand-identity.md` -- voice, tone, typography, color rationale

3. **Brand board:**
   Write `docs/plan/{name}/brand/brand-board.html` -- full visual system showcase

4. **Design tokens (DERIVED from DESIGN.md):**
   Write `docs/plan/{name}/brand/tokens.css` (CSS custom properties),
   `tailwind.theme.json` (Tailwind v3), and `tokens.json` (DTCG) -- all generated, never
   hand-edited

5. **Palette visualization:**
   Write `docs/plan/{name}/brand/palette.svg` -- color swatches as SVG

6. **Logo concepts:**
   Write `docs/plan/{name}/brand/logo-concepts.html` -- 3 logo direction options

7. **Icon style guide:**
   Write `docs/plan/{name}/brand/icon-style.md` -- icon style specifications

8. **Component samples:**
   Write `docs/plan/{name}/brand/components.html` -- 8-10 UI component samples

9. **Lock marker:**
   Create `$PLAN_DIR/.locks/brand-identity.md.locked`

**Gate 6:** All files written. Lock marker created.

---

## Skills Referenced

| Skill | When Used |
|-------|-----------|
| `frontend-design` | Design interrogation (Phase 2); voice, tone, positioning framework (Phase 3.2); brand board HTML, component samples (Phase 3.5, 5.1, 5.3); also consumes the emitted `DESIGN.md` as the design source of truth |
| `ui-ux-pro-max` | Industry-specific style, color, typography, layout data (Phase 2.5) |
| `design-system-starter` | Token structure, component patterns (Phase 5.1) |

The emitted `DESIGN.md` is the contract that downstream consumers honor: `/plan:prd` references it,
`/project:init` copies it to the project root, and the CLAUDE.md template + `frontend-design` skill
+ `ui-engineer`/`ux-specialist` agents read it before any frontend work.

---

## Output

```
plan:design complete

Identity:
  Colors: {primary} / {secondary} / {neutral}
  Display: {heading font} / Body: {body font}
  Voice: {adjective 1}, {adjective 2}, {adjective 3}
  Tagline: "{tagline}"

Visuals:
  Logo: {chosen direction}
  Icons: {style description}
  Components: {count} samples

Dark Mode: {yes/no/system}

Artifacts:
  docs/plan/{name}/brand/DESIGN.md          (CANONICAL — design source of truth, lint-validated)
  docs/plan/{name}/brand/brand-identity.md
  docs/plan/{name}/brand/brand-board.html
  docs/plan/{name}/brand/tokens.css         (derived from DESIGN.md)
  docs/plan/{name}/brand/tailwind.theme.json (derived)
  docs/plan/{name}/brand/tokens.json        (derived, DTCG)
  docs/plan/{name}/brand/palette.svg
  docs/plan/{name}/brand/logo-concepts.html
  docs/plan/{name}/brand/icon-style.md
  docs/plan/{name}/brand/components.html
  .locks/brand-identity.md.locked

Unlocks:
  Marketing Copy (needs brand identity + user stories)
  Layout/UX Prototype (needs brand visuals + user stories)
  Presentation Builder (adds brand slides 6-7, 13-14)
  PRD sections: Design Language, Brand Guidelines, UI Specifications

Next:
  /plan:user-stories {scope-lock-path}  User personas + wireframes
  /plan:financials {scope-lock-path}    Financial projections
  /plan:prd {plan-dir-path}             Accumulate into PRD
```
