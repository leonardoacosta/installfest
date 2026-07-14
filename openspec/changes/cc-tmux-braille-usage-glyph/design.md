# Design: cc-tmux-braille-usage-glyph

## Encoding: shared-overlay, not segmented lanes

Braille cells (U+2800 base) are a 2-wide x 4-tall dot grid. Dot bit-positions (little-endian from
`U+2800`): dot1=bit0 (col1/row1), dot2=bit1 (col1/row2), dot3=bit2 (col1/row3), dot7=bit6
(col1/row4), dot4=bit3 (col2/row1), dot5=bit4 (col2/row2), dot6=bit5 (col2/row3), dot8=bit7
(col2/row4).

Two candidate encodings were mocked up live during `/openspec:explore`:

1. **Shared overlay** (chosen): all three metrics share the same N-cell run, each on its own
   dot-row(s), independent proportional left-to-right fill per row (bitwise-OR into the same
   cells). Bit-traced against `SES=30%/5H=88%/7D=35%` -> `⣿⣿⣧⠤⠤⠤⠤⠀` (n=8): row3 (5H) "on"
   through cell 6, extending to 87.5%≈88%; row4 (7D) "on" through cell 2, extending to
   37.5%≈35%; rows1-2 (SES) full through cell 1 plus partial cell 2, extending to ~31%≈30%. Each
   row is a genuinely independent, correctly-encoded proportional bar sharing one visual column.
2. **Segmented lanes** (rejected): each metric gets its own dedicated cells (e.g. 4 cells for
   SES, 2 for 5H, 2 for 7D) instead of sharing. More immediately legible per-metric at a glance,
   but loses the "one compact texture" property Leo's original sketch was going for. Explicitly
   discarded per Leo's direct preference for the shared-overlay reading after reviewing both.

**Per-cell dot order** (column-major within a cell — fill column 1 top-to-bottom, then column 2 —
so a growing fill reads left-to-right the same way the existing shade-bar does):

| Metric | Rows | Bits (per cell, fill order) | Dots/cell |
| --- | --- | --- | --- |
| SES | 1-2 | `[0, 1, 3, 4]` | 4 |
| 5H | 3 | `[2, 5]` | 2 |
| 7D | 4 | `[6, 7]` | 2 |

For an N-cell run: total dot budget is `4N` (SES), `2N` (5H), `2N` (7D). `dots_lit =
round(ratio * total)`, filled sequentially cell-by-cell left to right (cell `i` takes
`min(remaining, dots_per_cell)` before moving to cell `i+1`). This is the exact algorithm
validated in the mockup — reuse it verbatim, do not re-derive.

## Width: n=10 (row 2), n=20 (popup)

Resolution floor at width `n`: SES floor = `100/(4n)`%, 5H/7D floor = `100/(2n)`% (values below
the floor round to zero dots for that metric — a real precision loss, but non-blocking since the
exact percentage stays in the accompanying text; the glyph is supplementary, never the source of
truth).

| n | SES floor | 5H/7D floor | glyph cols | approx total row cols |
| --- | --- | --- | --- | --- |
| 8 | 3.12% | 6.25% | 8 | 27 |
| 10 | 2.50% | 5.00% | 10 | 29 |
| 12 | 2.08% | 4.17% | 12 | 31 |
| 16 | 1.56% | 3.12% | 16 | 35 |
| 20 | 1.25% | 2.50% | 20 | 39 |

Leo reviewed this table live and chose **n=10 for row 2** (footprint-neutral against today's
~30-31 column usage-half) and **n=20 for the accounts popup**, where there's more room and no
tight-row footprint constraint to preserve. Both are locked — do not re-open the width question
without a new explicit ask.

## Non-active popup rows get their own 2-metric encoding, not a blank SES gap

The existing spec (`spec.md:640-649`) already treats the popup's active vs. non-active rows
asymmetrically: only the active account has an SES value at all (SES is session-scoped, not
account-scoped — a non-active credential simply has no session to report on). Two options for
non-active rows:

1. Reuse the 3-metric `render_usage_glyph` with SES always `None` — rows 1-2 permanently blank.
2. A dedicated 2-metric encoding: 5H = rows 1-2 (4 dots/cell), 7D = rows 3-4 (4 dots/cell) —
   doubling each metric's per-cell budget since there's no third metric to share space with.

Chose (2). A permanently-blank SES region (option 1) wastes half the glyph's dot budget for no
benefit — non-active rows never need to reserve that space, so giving 5H/7D the full 4-dot rows
each is strictly better resolution at zero cost. This does mean two distinct encoding functions
(`render_usage_glyph` 3-metric, `render_usage_glyph_2metric` 2-metric) rather than one, but the
underlying per-metric dot-fill helper is shared — only the bit-order tables differ.

## Color: text-only, glyph stays neutral

A single braille character can only carry one tmux `#[fg=...]` color for its whole cell, but the
combined glyph encodes three independently-thresholded metrics per cell. Rather than invent a
"worst-of-three" aggregate color rule (e.g. red if ANY metric is >80%), this proposal keeps color
exclusively on the existing text numbers (`usage.color_for`'s RED/YELLOW/CYAN for 5H/7D text) and
renders the glyph itself in a neutral/unstyled color. This mirrors an existing precedent in the
same codebase: the popup's non-active-row 5H/7D text is already "deliberately uncolored —
uniformly green" (`render.py:653-656`) — i.e. this plugin already accepts that some usage
indicators are intentionally neutral where a combined/aggregate signal would mislead more than it
helps.

**Correction found during UI batch verification (task 3.4, if-4lxh.1)**: `_context_color_pair`'s
6-tier severity ramp was NEVER applied to the SES token-count label before this proposal — it was
applied exclusively to the shade-bar's fill color (`render_context_bar`'s `#[fg={color}]{bar}`).
The label itself was always plain DIM. Retiring the bar without moving that ramp onto the label
(the mistake the first UI batch pass made, following this section's original — inaccurate —
claim that the label "already" carried it) silently drops the entire severity signal, including
the pulsing-red near-context-exhaustion warning. The fix carries the ramp forward onto the label
(`#[fg={color}]{label}:` instead of `#[fg={DIM}]{label}:`, reusing `resolve_context_color`/
`_context_bar_parts` — no new logic), so the signal that used to live on the bar's color now
lives on the label's color instead. 5H/7D's `color_for`-driven text colors are unaffected.

## Staleness: per-metric degrade

SES and 5H/7D come from different sources with different cache lifetimes (nx-agent
`session_context()` vs. nexus-agent `/credentials`, 45s TTL) — a shared glyph can easily have one
metric live and another stale at render time. Chosen: **per-metric degrade** — a stale metric
contributes zero dots to its own row(s) only; other metrics' rows render normally. This matches
today's existing behavior (SES/5H/7D each independently degrade to `--`) and this codebase's
established single-source-of-truth precedent (`_resolve_ses_pct` is deliberately shared between
row 2 and the popup specifically so the two surfaces can't drift onto different SES sources — the
same "don't let independent paths diverge" principle, applied here to how staleness is handled
per metric rather than as an all-or-nothing glyph blackout).

**Accepted tradeoff, stated explicitly**: a stale metric's zero-dot rendering is visually
identical to a genuine 0% reading, within the glyph alone. This is not fixed by this proposal —
the text prefix (which already renders `--` for a stale value) is what disambiguates, exactly as
it does today for the existing SES bar and 5H/7D text.

## Live-data verification gap (informational, not a blocker)

During `/openspec:explore`, `nx_agent.session_context()` — the actual production function SES
sourcing goes through — returned `None` for every currently-tracked pane's session-id on this
machine. 5H/7D pulled correctly from nexus-agent `/credentials` in the same session. This is a
pre-existing, separate data-availability gap (same class as the documented `usagePolledAt`-null
issue), not something this proposal needs to fix — SES rendering in any live-verification task
should use illustrative values if the gap persists at execution time, and note it rather than
block on it.

## Batch mapping (DB / API / UI / E2E doesn't map cleanly onto this domain)

Same convention as the prior `cc-tmux-git-status-glyphs` change — this repo has no traditional
database/API/UI/E2E layering (a dotfiles repo plus a tmux CLI plugin), so the four literal
headers `/feature`'s wave-plan-build gate requires are mapped by domain fit:

| Literal header | What actually lives there |
| --- | --- |
| `## DB Batch` | The shared braille bit-order constants + per-metric dot-fill helper (the "data layer" this feature depends on) |
| `## API Batch` | `render_usage_glyph` / `render_usage_glyph_2metric` (the pure encoding functions) |
| `## UI Batch` | `render_session_bar` / `render_accounts_popup` wiring (the actual user-visible rendering surface) |
| `## E2E Batch` | self-test additions + live verification |
