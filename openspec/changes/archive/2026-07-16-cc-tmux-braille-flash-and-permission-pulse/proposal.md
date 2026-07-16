---
order: 0716c
---

# Proposal: cc-tmux-braille-flash-and-permission-pulse

## Why

Leo's ask (`/openspec:explore`, this session): replace two of cc-tmux's tab-icon animations.
(1) the "8th blocks" animations for `active`/thinking (`BLOCK_FRAMES`) and for foreground/
background sub-agent activity (the `SUBAGENT_FG_*`/`SUBAGENT_BG_*` overlay, `render.py:211-238`)
should flash between adjacent braille glyphs instead of the current block-edge/shape glyphs.
(2) the pulsing `SHADE_FRAMES` (`░▒▓█▓▒░`) used for `waiting` (permission/question/plan/
elicitation, set via `hooks/hooks.json` `reason=question|plan|permission|elicitation`) should
become a pulse between `◉` (yellow) and `◎` (default color).

**Glyph-collision finding (resolved during this proposal's discovery, not guessed):** `render.py`
already uses `◎`/`◉` for the FOREGROUND sub-agent-count overlay (`SUBAGENT_FG_1`/
`SUBAGENT_FG_2PLUS` — 1 vs 2+ active foreground sub-agents). Reusing the same two glyphs for the
permission/question pulse would collide with that existing, unrelated meaning. Leo's resolution:
rename the EXISTING foreground sub-agent overlay to a different glyph pair, freeing `◎`/`◉`
exclusively for the new permission/question pulse.

**Braille source (resolved during discovery):** the flash is a brand-new, dedicated 2-frame
braille pair per affected state — NOT drawn from any existing braille ramp elsewhere in the
plugin (e.g. row 2/3's countdown glyph), avoiding coupling to an unrelated feature.

**Distinctness decision (this proposal's own design call, not asked — matches established
precedent):** `cc-tmux-subagent-tab-icon`'s own design principle (`render.py:195-208`) is that
foreground vs background sub-agent activity, AND 1-vs-2+ counts within each, get DISTINCT
visual treatment specifically so they remain distinguishable at a glance. Clarified during this
proposal's discovery: today's four sub-agent glyphs (`SUBAGENT_FG_1`/`FG_2PLUS`/`BG_1`/`BG_2PLUS`)
are actually STATIC — no animation exists for them at all yet, unlike `active`/`waiting`. Leo
confirmed (discovery Q2) the ask extends to animating these too, not just `active`. This proposal
preserves all four existing distinctions by giving EACH of the four its own 2-frame braille flash
pair — same TECHNIQUE (flash between two adjacent frames) applied five times total (`active` +
FG-1 + FG-2+ + BG-1 + BG-2+), never collapsing two previously-distinguishable states into one
indistinguishable flash.

## What Changes

- **`apps/cc-tmux/src/cc_tmux/render.py`**:
  - Rename `SUBAGENT_FG_1` (`"◎"` -> `"□"`) and `SUBAGENT_FG_2PLUS` (`"◉"` -> `"■"`) — a new
    hollow/filled SQUARE identity for foreground sub-agents, distinct from the existing circle
    (`○`/`●`, `DEFAULT_ICONS` waiting/idle) and diamond (`◇`/`◆`, background sub-agents) families,
    and from the now-freed `◎`/`◉` below. This is the STATIC identity each animated pair below is
    built from (e.g. the FG-1 flash pair is two braille glyphs evoking a square/hollow motif,
    not literally `□` itself, since `□`/`■` are not braille — see design.md for the exact glyph
    picks per pair, TBD at implementation time within the "flash between two adjacent braille
    frames" contract).
  - Add five new 2-tuples of braille glyphs (new constants, not derived from any existing ramp):
    `ACTIVE_FLASH_FRAMES` (replaces `BLOCK_FRAMES` for the `active`/thinking state),
    `SUBAGENT_FG1_FLASH_FRAMES`, `SUBAGENT_FG2PLUS_FLASH_FRAMES`, `SUBAGENT_BG1_FLASH_FRAMES`,
    `SUBAGENT_BG2PLUS_FLASH_FRAMES` (replace the four static sub-agent glyphs). `animated_icon`'s
    `active` branch and all four branches of `resolve_tab_icon`'s sub-agent overlay flash
    between index 0 and 1 of their respective tuple on the same wall-clock-tick cadence
    `BLOCK_FRAMES`/`SHADE_FRAMES` already use (`FRAME_PERIOD_SEC`-driven, no new timer).
    Foreground-vs-background and 1-vs-2+ precedence/threshold logic in `resolve_tab_icon` is
    UNCHANGED — only what each branch returns (a flashing pair instead of a static glyph)
    changes.
  - Add `PERMISSION_PULSE_FRAMES = ("◉", "◎")`, replacing `SHADE_FRAMES` for the `waiting` state
    (permission/question/plan/elicitation) in `animated_icon`. Coloring `◉` YELLOW requires a new
    branch in `resolve_tab_glyph` (render.py:241) — today it returns an empty color for every
    non-idle-meter case; see design.md for the exact wiring (this was corrected from an initial
    wrong assumption that a color-wrap already existed for `waiting`).
  - `IDLE_GLYPH` and the idle-usage-meter overlay are UNCHANGED — this proposal touches
    `active`, `waiting`, and all four sub-agent-overlay branches, nothing else.

## Non-Goals

- No change to the sub-agent overlay's PRECEDENCE rule (foreground still always wins over
  background when both are nonzero) or its count thresholds (1 vs 2+) — only the glyphs
  themselves change identity/mechanism.
- No change to `IDLE_GLYPH`, the idle-usage-meter ramp, or any row 2/3 rendering — this is
  scoped entirely to the tab-icon animation functions.
- No change to which hook `reason` values map to the `waiting` state, or to the
  `hooks/hooks.json` wiring itself.

## Context
- touches: `apps/cc-tmux/src/cc_tmux/render.py`
- Note: `cc-tmux-row2-model-color-usage-format` (in-flight, same session) also touches
  `render.py`, but a different function (`render_session_bar`, unrelated to this proposal's
  `animated_icon`/`resolve_tab_icon` region) — a file-level wave conflict, not a logical
  dependency; `wave-plan-build`'s conflict matrix serializes them into separate waves.

## Testing

- `apps/cc-tmux/src/cc_tmux/testing.py`: extend self-tests for `animated_icon` (asserts the
  `active` state now cycles `ACTIVE_FLASH_FRAMES` by tick parity, not `BLOCK_FRAMES`), for
  `resolve_tab_icon` (asserts all four sub-agent branches — FG-1, FG-2+, BG-1, BG-2+ — now
  flash their respective new frame pair by tick parity instead of returning a static glyph, and
  that precedence/threshold logic is unchanged), and a new test asserting the `waiting` state
  cycles `PERMISSION_PULSE_FRAMES` (`◉`/`◎`) with the documented yellow/default coloring, not
  `SHADE_FRAMES`.
