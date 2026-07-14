---
status: draft
---

# Proposal: cc-tmux-braille-usage-glyph

## Why

Today SES, 5H, and 7D are three visually separate elements on row 2 (`render.py:332-345`
`render_context_bar` + `render.py:394-493` `render_session_bar`): SES is a 10-segment shade-block
bar (`▓▓▓░░░░░░░`) plus a token-count label, while 5H and 7D are plain colored percentage text
with **no bar at all** (`render.py:489-491`). Full usage-half footprint is ~30-31 columns
(`252.5k:▓▓▓░░░░░░░ 5H:36% 7D:71%`). The accounts popup (`render.py:595-706`) repeats the same
shade-bar-for-SES-only, text-for-5H/7D split for the active account's row.

Leo proposed replacing this with a single combined Unicode Braille sparkline: each braille cell
is a 2-wide x 4-tall dot grid, and one glyph run can encode all three metrics simultaneously —
top two dot-rows = SES, third row = 5H, fourth (bottom) row = 7D — each row an independent
proportional left-to-right fill sharing the same cell run, exactly like the current shade-bar's
segmented-fill logic but generalized to 3 rows instead of 1.

**Validated via live mockup during `/openspec:explore`, not speculative:**

1. **Encoding correctness**, confirmed by manual bit-trace against a rendered example
   (SES=30%/5H=88%/7D=35% -> `⣿⣿⣧⠤⠤⠤⠤⠀` at n=8): each dot-row's fill extent independently
   reproduces its own input percentage (row3/5H extended to 87.5%≈88%, row4/7D to 37.5%≈35%,
   rows1-2/SES to ~31%≈30%). A rejected alternative (segmented lanes — each metric gets dedicated
   cells instead of sharing) was prototyped and discarded; Leo confirmed the shared-overlay
   reading is what he wants.
2. **Live-data test**: real 5H/7D pulled from nexus-agent `/credentials` (e.g. 5H=90/7D=64,
   5H=15/7D=79 across active accounts) rendered correctly through the algorithm. SES could not be
   pulled live — confirmed by calling the actual `nx_agent.session_context()` function directly,
   which returned `None` for every currently-tracked pane's session-id on this machine (a separate,
   pre-existing data-availability gap, not something this proposal fixes).
3. **Precision-floor caveat** (non-blocking — exact percentages stay in the accompanying text,
   the glyph is supplementary): at width n, SES gets a `4n`-dot budget and 5H/7D each get a
   `2n`-dot budget, so values below `100/(4n)`% (SES) or `100/(2n)`% (5H/7D) round to a fully
   blank dot-row. Leo reviewed a footprint-vs-floor table across n=8/10/12/16/20 and explicitly
   chose **n=10** for row 2 (footprint-neutral vs today's ~30-31 cols; floor ~2.5%/SES, ~5%/5H+7D)
   and **n=20** for the accounts popup (more screen real estate available there; floor ~1.25%/SES,
   ~2.5%/5H+7D for the active row).

**Popup asymmetry, carried over from the existing spec's own precedent** (`spec.md:640-649`):
the active account's popup row already shows SES+5H+7D while non-active rows show only 5H+7D (no
SES field — SES is inherently session-scoped, not account-scoped, so a non-active credential has
no SES value to show at all). This proposal keeps that asymmetry but gives non-active rows their
own 2-metric encoding (5H = rows 1-2, 7D = rows 3-4, i.e. each metric gets a full 4-dot-per-cell
budget instead of splitting a shared 2-dot row) rather than leaving the SES dot-rows permanently
blank in a 3-metric glyph — this doubles non-active rows' effective resolution since they never
carry a third metric to share space with.

**Color**: the glyph itself renders in a neutral/unstyled color; the existing color-coded text
numbers (`usage.color_for`'s RED/YELLOW/CYAN thresholds for 5H/7D, `_context_color_pair`'s 6-tier
ramp for the SES token count) remain the sole color signal, unchanged. A combined 3-metric glyph
cannot carry 3 independent per-metric colors on one tmux-styled character run, so rather than
inventing a "worst-of-three" aggregate color rule, this proposal keeps color exclusively on the
text (mirroring the existing precedent that the popup's non-active 5H/7D text is "deliberately
uncolored — uniformly green", i.e. this codebase already treats some usage indicators as
intentionally neutral where a combined/aggregate signal would be misleading).

**Staleness**: SES/5H/7D can each go stale/unpolled independently (different data sources, different
cache TTLs — nx-agent session-context vs nexus-agent `/credentials`, 45s TTL). This proposal
adopts **per-metric degrade**: a stale metric contributes zero dots to its own row(s) only, rows
with live data still render normally — matching today's existing per-element independence (each
of SES/5H/7D already degrades to `--` independently) and this codebase's established
single-source-of-truth precedent (`_resolve_ses_pct` is shared by row 2 and the popup specifically
so the two surfaces cannot drift onto two different SES sources — same "don't let two paths
diverge" principle applied here to staleness handling). Accepted tradeoff, stated explicitly: a
stale metric's zero dots are visually indistinguishable from a genuine 0% reading in the glyph
alone — the text prefix (which already renders `--` for a stale metric) is what disambiguates,
same as today.

**Spec/code drift found during exploration, folded into this change**: `spec.md:435`'s
Requirement text still literally says "SES:" even though shipped code (`cc-tmux-context-bar`)
already replaced that with the token-count bar — this proposal's spec delta corrects that text
alongside the new combined-glyph requirement, since it's already being touched.

## What Changes

- **`apps/cc-tmux/src/cc_tmux/render.py`**: add a shared braille-encoding helper (bit-order
  constants for the 3-metric rows1-2/row3/row4 split and the 2-metric rows1-2/rows3-4 split, plus
  a per-metric dot-fill function honoring `None` -> zero-dots-for-that-metric-only). Add
  `render_usage_glyph(ses_ratio, h5_ratio, d7_ratio, n=10)` (3-metric, replaces
  `render_context_bar`'s call site in `render_session_bar`) and
  `render_usage_glyph_2metric(h5_ratio, d7_ratio, n=20)` (2-metric, for the popup's non-active
  rows). The popup's active-row rendering reuses `render_usage_glyph` at `n=20`. All three ratio
  inputs are 0..1 floats (matching the existing `_resolve_ses_pct`/`_extract_util` convention),
  `None` meaning "stale/unpolled for this metric only."
- **`apps/cc-tmux/src/cc_tmux/render.py`**: `render_session_bar` and `render_accounts_popup` call
  the new glyph function(s) instead of `render_context_bar` + separate 5H/7D text-only rendering.
  The exact `5H:xx% 7D:xx%` (and SES token-count label) text stays as today, unchanged — the glyph
  is appended alongside it, not a replacement for the numeric read-out.
- **`openspec/specs/cc-tmux/spec.md`**: MODIFIED delta for "A dedicated tmux status row shows
  session identity and usage" (documents the new combined glyph, corrects the stale "SES:" text)
  and "Clicking the row-2 account label opens a read-only accounts popup" (documents the
  active-row 3-metric glyph and non-active-row 2-metric glyph).

## Non-Goals

- No change to the color-threshold logic itself (`usage.color_for`'s 50%/80% buckets,
  `_context_color_pair`'s 6-tier ramp) — those stay exactly as-is, still driving the text colors.
- No attempt to fix the pre-existing SES-live-data gap on this machine (`nx_agent.session_context()`
  returning `None` for every currently-tracked session) — separate, tangential issue.
- No change to row 3 (beads/openspec bar) or any other status-bar row.
- No nx-agent/nexus-agent server-side changes — purely a cc-tmux render-layer change consuming
  existing data sources as-is.
- No attempt to make a stale metric's zero-dot rendering visually distinct from a genuine 0%
  reading within the glyph itself — accepted tradeoff, the text prefix already disambiguates.

## Context

- Extends: `apps/cc-tmux/src/cc_tmux/render.py` (`render_context_bar` retired in favor of
  `render_usage_glyph`; `render_session_bar`, `render_accounts_popup` updated to call the new
  glyph functions)
- Related: `openspec/changes/archive/2026-07-11-cc-tmux-session-usage-bars/` — established this
  exact row's precedent of resolving layout via live mockup iteration (flipped 4 times) rather
  than blind spec-writing; this proposal follows that same process (mockup-validated during
  `/openspec:explore` before drafting).
- Related: `openspec/changes/archive/2026-07-13-cc-tmux-git-status-glyphs/` — most recent prior
  change to this exact row (git working-tree indicators), used here as the tasks.md/proposal.md
  structural template (DB/API/UI/E2E batch mapping for this Python-plugin, no-traditional-layers
  repo).
- touches: `apps/cc-tmux/src/cc_tmux/render.py`, `apps/cc-tmux/src/cc_tmux/testing.py`,
  `openspec/specs/cc-tmux/spec.md`

## Testing

| Seam | Coverage |
| --- | --- |
| Braille dot-fill helper (pure function: ratio -> dot bits) | `cc-tmux self-test` cases: each metric at a representative ratio produces the exact expected dot-bit set per cell; `None` produces zero dots for that metric only, other metrics' dots unaffected; 0.0 and 1.0 edges render fully-empty/fully-full — task 4.1 |
| `render_usage_glyph` (3-metric, n=10) | `cc-tmux self-test` cases: reproduces the validated mockup example (SES=30%/5H=88%/7D=35% -> the traced glyph) at n=10; all-stale -> fully blank glyph; mixed live/stale -> only stale metric's row(s) blank — task 4.2 |
| `render_usage_glyph_2metric` (2-metric, n=20) | `cc-tmux self-test` case: 5H/7D at representative ratios produce the expected rows1-2/rows3-4 split, each getting the full 4-dot-per-cell budget — task 4.3 |
| `render_session_bar` / `render_accounts_popup` wiring | `cc-tmux self-test` cases: row 2 renders the glyph alongside unchanged text; popup's active row uses the 3-metric glyph at n=20, non-active rows use the 2-metric glyph at n=20 — task 4.4 |
| End-to-end live render | Live verification: real pane, real 5H/7D from nexus-agent (SES illustrative if the live-data gap persists), confirm row 2 and popup render the exact expected glyph — paste observed output — task 4.5 |
