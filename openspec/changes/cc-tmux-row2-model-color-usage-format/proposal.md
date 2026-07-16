---
order: 0716b
---

# Proposal: cc-tmux-row2-model-color-usage-format

## Why

Row 2 (`render_session_bar`, `render.py:538-642`) already renders almost exactly the layout Leo
asked for in `/openspec:explore` this session — model letter, project, `>branch`, git-status
indicators on the left; SES token count, `5H:`/`7D:` percentages, and a combined braille usage
glyph on the right. Four formatting/coloring deltas remain, all confirmed directly against the
current code (no ambiguity needed clarifying with Leo):

1. **Model letter color**: today every model renders the same static CYAN
   (`render.py:596-597`, `f"#[fg={CYAN}]{model_letter}"`). Leo wants it colored by model:
   Opus=yellow, Sonnet=green, Haiku=light green, Fable=red.
2. **Drop the SES label's colon**: the right side's format string
   (`render.py:637-640`) is `f"#[fg={ses_color}]{ses_label}:#[default] " f"#[fg={DIM}]5H:..." f"#[fg={DIM}]7D:..."`
   — the SES label alone carries a colon (`{ses_label}:`, e.g. `"252.5k:"`). Leo's note
   `(remove ":")` sits specifically next to the session-usage segment, not `5H`/`7D` — those
   keep their colons unchanged.
3. **Add a space before the usage glyph**: the current format string ends
   `...{p7}#[default]{usage_glyph}` — the 7D percentage and the braille glyph are directly
   concatenated with zero space between them today. Leo's `(Add space)` note sits right before
   the glyph segment.
4. **Double the glyph's width**: `render_usage_glyph` already takes an `n` (cell count)
   parameter; row 2 calls it with `n=10` (`render.py:632`). The accounts-popup's 2-metric glyph
   already uses `n=20` (`render.py:847`, `cc-tmux-braille-usage-glyph`'s established "wide"
   convention) — this proposal reuses that exact precedent for row 2's 3-metric glyph rather
   than inventing a new width or algorithm change.

## What Changes

- **`apps/cc-tmux/src/cc_tmux/usage.py`**: add a `LIGHT_GREEN` color constant (a lighter shade
  than the existing `GREEN = "#00ac3a"`) alongside the existing `DIM`/`CYAN`/`YELLOW`/`RED`/
  `GREEN`/`BLUE` constants.
- **`apps/cc-tmux/src/cc_tmux/render.py`**:
  - `render_session_bar`: replace the static `CYAN` model-letter color with a lookup keyed by
    model name/letter (Opus->YELLOW, Sonnet->GREEN, Haiku->LIGHT_GREEN, Fable->RED), falling
    back to the current CYAN for an unrecognized/empty model (fail-open, matching this
    function's existing "empty field drops out" convention).
  - Drop the trailing `:` from the SES label segment only (`5H:`/`7D:` unchanged).
  - Insert a single space between the 7D percentage segment and `{usage_glyph}`.
  - Change the `render_usage_glyph(ses_pct, five_h_pct, seven_d_pct, n=10)` call to `n=20`.

## Non-Goals

- No change to `render_usage_glyph`'s encoding algorithm, bit-order constants, or per-metric
  degrade behavior — only the `n` argument at this one call site changes; the function itself
  is unmodified and already supports arbitrary `n`.
- No change to the accounts-popup's own glyph rendering (`render_usage_glyph_2metric`, `n=20`) —
  already wide, untouched by this proposal.
- No change to the model-LETTER resolution itself (`_resolve_model_letter`, `cli.py:1069`,
  fixed this session under if-bqw.2) — only the COLOR applied to whatever letter it returns.
- No change to `5H:`/`7D:` colon presence, the git-status indicator colors/thresholds, or the
  left-side project/branch rendering — all unchanged, restated verbatim in the spec delta.

## Context
- touches: `apps/cc-tmux/src/cc_tmux/render.py`, `apps/cc-tmux/src/cc_tmux/usage.py`

## Testing

- `apps/cc-tmux/src/cc_tmux/testing.py`: extend `render_session_bar`'s self-tests to assert
  per-model color codes (Opus/Sonnet/Haiku/Fable each produce the expected `#[fg=...]` prefix,
  an unrecognized model falls back to CYAN), the SES label renders without a trailing colon
  while `5H:`/`7D:` retain theirs, a single space precedes the usage glyph, and
  `render_usage_glyph` is invoked with `n=20` (assert on the glyph's character length).
