# Context-Budget Rubric — Deterministic Bands + Grace Thresholds

**Repo:** `~/dev/cc-audit` · **Date:** 2026-07-18 · Companion to `skills-external-standards-scrutiny.md`.
**Purpose:** one deterministic rubric covering every Claude Code budget/threshold that affects this repo — skills, commands, CLAUDE.md/rules, memory, agents, MCP, hooks, compaction — with GREEN/AMBER/RED bands so drift is detected mechanically, before the platform's silent-overflow cliffs.
**Sources:** code.claude.com docs (skills.md, memory.md, settings.md, env-vars.md, hooks.md, mcp.md, sub-agents.md, tools-reference.md, context-window.md, costs.md — all fetched 2026-07-18) + agentskills.io/specification. Every number is tagged: **[H]** hard documented limit, **[G]** documented guidance (qualitative or recommended), **[R]** repo-set policy (no external number exists — flagged for ratification).

---

## Part 0 — Band derivation rules (so thresholds are derivable, not arbitrary)

- **Rule 1 (hard limits):** for any **[H]** limit L with silent or lossy overflow: GREEN ≤ 0.8·L · AMBER 0.8·L–L (the grace zone — early warning before the cliff) · RED > L.
- **Rule 2 (guidance numbers):** for any **[G]** guidance value V with no enforced cliff: GREEN ≤ V · AMBER V–2·V · RED > 2·V.
- **Rule 3 (repo-set):** where no external number exists but the surface has unbounded always-loaded cost, the rubric sets **[R]** values using the nearest analogous **[H]**/**[G]** anchor, stated per row. These require operator ratification and live in one place (the lint script header) so changing policy is a one-line edit.
- **Token estimate convention:** tokens ≈ chars / 4. All char measurements are exact; token figures are estimates and marked ~.

---

## Part 1 — The rubric

### Table A — Context budgets (always-paid or trigger-paid)

| # | Surface | Measurement (deterministic) | GREEN | AMBER | RED | Limit + tag | Overflow behavior at RED |
|---|---|---|---|---|---|---|---|
| A1 | **Skill+command listing total** (skills AND commands share one listing since the merge) | Σ over `skills/*/SKILL.md` + `commands/**/*.md` (excl. references/) of frontmatter `len(name)+len(description)+len(when_to_use)` chars | ≤ 6,400 | 6,400–8,000 | > 8,000 | 8,000 chars ≈ 1% of 200K ctx, `skillListingBudgetFraction` default 0.01 **[H]** | Least-invoked descriptions **silently dropped** (debug-log only) — skills stop ambient-triggering |
| A2 | **Per-skill listing entry** | `len(description)+len(when_to_use)` per skill/command | ≤ 1,229 | 1,230–1,536 | > 1,536 | 1,536 chars, `skillListingMaxDescChars` **[H]** | Silent truncation mid-description |
| A3 | **Per-skill `description` alone** | `len(description)` | ≤ 819 | 820–1,024 | > 1,024 | 1,024 chars, Agent Skills spec MUST **[H]** | Spec violation; portability break |
| A4 | **SKILL.md body** | total lines of SKILL.md | ≤ 400 | 401–500 | > 500 | 500 lines **[G→H-treated]** (spec SHOULD + <5,000-token guidance; treated as hard because of A10) | Recurring per-turn cost; post-compaction truncation risk |
| A5 | **Reference file ToC** | lines per `references/*.md`; ToC = link-list or "Contents" heading in first 40 lines | ≤ 100, or any length WITH ToC | 101–300 without ToC | > 300 without ToC | 100 (best-practices) / 300 (skill-creator) **[G]** — bands bridge the two sources | Partial reads (`head -100`) miss content |
| A6 | **Reference nesting depth** | links from `references/*.md` to further reference files | 0 | — | ≥ 1 | one-level-deep, spec SHOULD **[H]-treated** | Claude partial-reads chains; content unreachable |
| A7 | **Always-loaded CLAUDE.md chain** (root CLAUDE.md + every `@import` + project overlay) | Σ bytes/4 of root + imports (≤4 hops) + `~/dev/cc/.claude/CLAUDE.md` | ≤ 5,000 tok | 5,001–10,000 tok | > 10,000 tok | **[R]** — anchored to memory.md's per-file guidance (A8) × a 3-file chain; docs: "Longer files consume more context and reduce adherence" | Paid every turn AND reloaded after every compaction; crowds out conversation |
| A8 | **Per-file CLAUDE.md / rules import** | lines per file in the chain | ≤ 200 | 201–400 | > 400 | 200 lines target, memory.md **[G]** (Rule 2) | Adherence degradation (documented qualitatively) |
| A9 | **MEMORY.md auto-load** | lines AND bytes of each `projects/*/memory/MEMORY.md` | ≤ 160 lines and ≤ 20KB | 161–200 lines or 20–25KB | > 200 lines or > 25KB | 200 lines / 25KB, whichever first, memory.md **[H]** | Content past limit **silently not loaded**; only topic-file pointers survive |
| A10 | **Compaction carry-forward safety** | derived: A4 compliance | body ≤ 400 lines | 401–500 | > 500 | first 5,000 tokens per invoked skill, 25,000 combined **[H]** | A skill body ≤ 500 lines (~≤5k tok) survives carry-forward whole; larger bodies lose their tail **silently** after compaction — this is why A4 is treated as hard |
| A11 | **Agent `description`** (subagent roster, loaded into main context) | `len(description)` per `agents/**/*.md` | ≤ 500 | 501–1,024 | > 1,024 | **[R]** — no documented cap exists (verified); anchor borrowed from A3 | Unbounded roster cost; delegation-decision text bloats every session |
| A12 | **Agent roster total** | Σ agent description chars | ≤ 8,000 | 8,001–12,000 | > 12,000 | **[R]** — parity anchor with A1 (the roster is the "listing" of agents) | Same shape as A1, no platform enforcement at all |
| A13 | **MCP tool description / server instructions** | chars per tool description and per server `instructions` | ≤ 1,600 | 1,601–2,048 | > 2,048 | 2KB each, mcp.md **[H]** | **Silent truncation**; "put critical details near the start" |
| A14 | **Hook stdout / additionalContext / systemMessage** | measured output chars per hook (from telemetry or test fire) | ≤ 8,000 | 8,001–10,000 | > 10,000 | 10,000 chars, hooks.md **[H]** | Excess diverted to file + preview (not silent, but context payload lost from the turn) |

### Table B — Operational thresholds (timeouts and caps; repo overrides noted)

| # | Surface | Default | Repo override (settings.json) | Rubric check |
|---|---|---|---|---|
| B1 | Bash timeout | 120s default / 600s max | **300s / 1800s** (deliberate) | Overrides documented in settings with a comment; any change is a settings diff, not ambient |
| B2 | Auto-compact trigger | model boundary | **`CLAUDE_AUTOCOMPACT_PCT_OVERRIDE=90`** (can only lower — compliant use) | Present and ≤ 100 |
| B3 | UserPromptSubmit hooks | 30s cap; on timeout the hook's context is **silently discarded** | 20 events / 34 matcher groups wired | Every UserPromptSubmit hook has measured warm runtime ≤ 3s (10% of cap) — evidence via `cc-runtime-evidence` probe, re-verified when the hook changes |
| B4 | Stop-hook block cap | 8 consecutive | default | No hook design may rely on > 8 consecutive blocks |
| B5 | MCP output | warn 10K tok, cap 25K tok (`MAX_MCP_OUTPUT_TOKENS`) | default | Data-producer scripts stay < 10K tokens output (aligns with existing <200ms/—json contract) |
| B6 | Subagents per session | 200 (`CLAUDE_CODE_MAX_SUBAGENTS_PER_SESSION`) | default | `/apply:all` worst case (8 specs × 4 phases × retries + reviews + TDD trios) ≈ well under; note only |
| B7 | Bash output | 30K chars, file-diverted | default | Gate scripts pipe through `gate-output-summarizer` (already convention) |
| B8 | Cron/scheduled hook fields | 1,000 chars each | n/a | Check at authoring |

---

## Part 2 — Current scorecard (measured 2026-07-18)

| Rubric row | Measured | Band | Notes |
|---|---|---|---|
| A1 listing total | **46,200 chars (~11,550 tok): skills 31,922 + commands 14,278** | **RED — 5.8× budget** | Worse than the skills-only audit found: the command merge means all 50 command descriptions share the budget. `~/dev/cc/commands/apply.md` alone spends 1,048 chars |
| A2 per-entry | 0 over 1,536 | GREEN | — |
| A3 description | 1 over (t3-code-patterns, 1,190) | **RED (1)** | Point fix queued (E3) |
| A4 bodies | 6 over 500 (max 2,527) | **RED (6)** | 1 deletes with orphan purge; 5 need references/ splits |
| A5 ToCs | 79 files 101+ lines w/o ToC | **AMBER (systemic)** | Scripted one-pass fix (E4) |
| A6 nesting | 0 | GREEN | — |
| A7 always-loaded chain | **~16,126 tok** (CLAUDE.md ~4,800 + CORE ~4,150 + BEADS ~6,976 + overlay ~199) | **RED — 1.6× the [R] ceiling, 8% of a 200K window every turn** | Largest single recurring spend in the repo; also fully reloaded after every compaction |
| A8 per-file | root CLAUDE.md ≈ 460+ lines; CORE ≈ 400+; BEADS ≈ 690+ | **RED (BEADS), AMBER/RED (root, CORE)** | All exceed the 200-line target; BEADS.md beyond 2× |
| A9 MEMORY.md | 1 file, 7 lines / 815B | GREEN | Ample headroom |
| A11 agent descriptions | 1 over 1,024? — max is plan.md at 977 | AMBER (plan.md, ux-journey-auditor 589…) | 4 agents in grace zone |
| A12 roster total | 9,569 chars | AMBER | Within grace; watch on agent additions |
| A13 MCP | mcp.json small | GREEN | — |
| B2 | override present (90) | GREEN | Already best-practice |
| B3 | unmeasured | **UNKNOWN** | Needs one-time runtime evidence pass over the 34 matcher groups |

**Reading:** three structural REDs — the shared listing (A1), the always-loaded chain (A7/A8), and the body/description point violations (A3/A4). A1 and A7 are the two compounding ones: together they consume ~28K tokens (~14% of a 200K window) before the first user message, and both fail silently or degrade adherence rather than erroring.

---

## Part 3 — Enforcement

**E-R1 — `scripts/bin/context-budget-audit` (single data producer).**
Implements every Table-A row: emits `{"rows":[{"id":"A1","surface":...,"measured":...,"budget":...,"band":"GREEN|AMBER|RED","source":"H|G|R"}],"error":null}`, exit 0 always, < 200ms warm, no network. All thresholds live in a single constants block at the top of the script with the H/G/R tag and source URL per constant — ratifying or changing a repo-set value is a one-line diff. Subsumes the skills-only checks from `skill-lint` (E1 of the scrutiny doc) or is the same script — implementer's choice, one script preferred.
*Accept:* run today reproduces Part-2's scorecard exactly; every constant carries its tag + source comment.

**E-R2 — Ratchet wiring (two rows, asymmetric severity).**
Tier-3 `POLICY_CHECKS`: (1) **RED-block row** — any RED on an **[H]**-tagged rubric line fails the tier (these are documented cliffs); (2) **AMBER-monotonic row** — total AMBER count must be non-increasing week-over-week (grace zones may not accumulate). **[R]**-tagged REDs warn-only until ratified (codify-before-enforce), then move to row 1.
*Accept:* both rows in TOOLING.md's ratchet table with `# requires-settings:` headers as applicable; a seeded regression (one AMBER added) trips row 2 in test.

**E-R3 — Remediation order for the current REDs.**
1. **A1 (listing, 5.8×):** orphan deletions (~2.3K) + explicit-only description trims + `when_to_use` splits + command-description diet — `~/dev/cc/commands/apply.md`'s 1,048-char description is keyword-stuffed for routing that `argument-hint` and the body already do. Target ≤ 8,000 chars without raising the fraction; raise `skillListingBudgetFraction` explicitly only if the trimmed floor still exceeds budget — a visible spend replacing silent drops.
2. **A7/A8 (always-loaded ~16K tok):** apply CLAUDE.md's own § CLAUDE.md-split rule to itself — reference tables out of the chain into on-demand skills/rules files. BEADS.md (~7K tok) is the biggest line: its workflow detail belongs behind `bd prime` (which already exists as the on-demand loader) with a ≤200-line always-loaded core. Target: chain ≤ 10K first pass, ≤ 5K second pass.
3. **A3/A4 point fixes + A5 ToC pass:** as already ordered in the scrutiny doc (E3/E4).
4. **B3 evidence pass:** one `cc-runtime-evidence` sweep over UserPromptSubmit hooks; record warm runtimes in the hook headers.

**E-R4 — Probes for the silent cliffs (belt-and-suspenders).**
The platform offers two manual probes: `/context` (post-budget Skills row — shows what actually survived the listing budget) and `/doctor` (flags oversized CLAUDE.md, estimates skill cost, v2.1.206+). Add a session-primer note (or monthly scheduled check) that runs both and compares `/context`'s surviving-skill set against the full roster — the only way to *observe* which skills the budget dropped, since the warning goes to debug log only.
*Accept:* documented in TOOLING.md next to the ratchet rows; first run's dropped-skill list recorded as the baseline.

---

## Appendix — numbers the docs do NOT publish (do not invent)
Read-tool whole-file token default (`CLAUDE_CODE_FILE_READ_MAX_OUTPUT_TOKENS` exists, default unstated) · WebFetch truncation char count ("a fixed character limit") · subagent description cap (none — hence A11/A12 are [R]) · statusline timeout/output cap · output-styles size limits · a universal auto-compact percentage (model-dependent boundary). If any of these gain documented numbers, promote the corresponding [R] rows to [H] and re-derive bands by Rule 1.
