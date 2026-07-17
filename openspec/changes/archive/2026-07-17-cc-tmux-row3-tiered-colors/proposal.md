---
order: 0716e
---

# Proposal: cc-tmux-row3-tiered-colors

## Why

Leo's ask (`/explore`, this session): row 3's `op:`/`bd:` counts should be permanently visible —
remove the `next:` swap-cycle entirely — and every count number (not just today's single
"closure-debt" number) should be colored against a 4-tier scheme: default, yellow, a pulsating
yellow<->default tier, and red.

**This is a second reversal of the same requirement in two days.** Row 3
(`render_beads_bar`/`_build_beads_bar`) originally rendered `op:`/`bd:` counts permanently, with
an explicit exclusion: "a `next:` line SHALL NOT be rendered on this row" (`if-bqw.1`, cc commit
`b6b9a234`). `cc-tmux-row3-next-cycle` (archived 2026-07-15, one day before this proposal)
deliberately reversed that exclusion, cycling the row between counts and `next:` on an 8-second
wall-clock timer with a countdown glyph. This proposal reverses it back — `op:`/`bd:` render
every tick again, `next:` never renders on this row. Named explicitly so this isn't silently lost
context; the swap feature was a considered, working addition, not a mistake being corrected.

**Removal is self-contained — confirmed via grep, no other consumers exist anywhere in the
codebase**: `SWAP_PERIOD_SEC`, `_COUNTDOWN_RAMP`, `beads_bar_phase`, `beads_bar_countdown_glyph`
(all `render.py`-local), and `cli.py`'s `_parse_roadmap_pulse_next` + its call site in
`_build_beads_bar`. `nexus-statusline`'s own `getRoadmapPulse()` reads the identical
`~/.claude/scripts/state/roadmap-pulse.<code>.line` cache file independently, inside the CC pane
— this removal has zero effect on that surface.

**Live-data threshold check** (real cache files, not hypothetical): `if` project
`op: 3o 1ip 0ua` / `bd: 2o 1r 1b` (all default); `cc` project `bd: 15o 13r 0b` (yellow tier);
`tc` project `bd: 10o 9r 0b` (right at the default/yellow boundary). `bd:`'s thresholds running
~2x `op:`'s at every tier matches the real volume gap between proposal-level and task-level
counts — the ask is calibrated against actual fleet data, not arbitrary round numbers.

**Boundary resolution (stated explicitly, not silently assumed):** the ask's stated ranges
overlap at their edges as written (`op` "11-20 pulsating" then "20+ red" both include 20; `bd`
"20-40 pulsating" then "40+ red" both include 40). The only self-consistent, non-overlapping
reading — given the explicit "11-20"/"20-40" upper bounds stated immediately before each "+" tier
— is that "20+"/"40+" means strictly above the previous tier's upper bound: `op` red starts at
21, `bd` red starts at 41. This is called out here so it's easy to correct if the intent was
different (e.g. inclusive-red-at-20).

**No new fetch, timer, or process.** `render_beads_bar`'s `now: Optional[float]` parameter stays
in the signature — its role changes from "which content phase" to "pulse-tier tick parity for
per-number coloring," reusing the exact `(base_color, pulse_color)`-pair-alternated-on-wall-clock-
parity mechanism `_context_color_pair`/`resolve_context_color` already establish elsewhere in this
file. This is the first use of that mechanism for a warning<->normal pulse (every existing pulse
pair today is alarm<->more-alarm); the mechanism itself is not new.

## What Changes

- **`apps/cc-tmux/src/cc_tmux/render.py`**:
  - Remove `SWAP_PERIOD_SEC`, `_COUNTDOWN_RAMP`, `beads_bar_phase`, `beads_bar_countdown_glyph`.
  - Remove `BEADS_UNARCHIVED_HIGH`/`BEADS_BLOCKED_HIGH` (the current shared-value=5, n3-only,
    2-tier scheme) and `_threshold_color`. Replace with six new per-label threshold constants
    (`OP_YELLOW_MIN=6`, `OP_PULSE_MIN=11`, `OP_RED_MIN=21`, `BD_YELLOW_MIN=11`, `BD_PULSE_MIN=21`,
    `BD_RED_MIN=41`) and a new tiered-color function that additionally accepts `now: Optional[
    float]` to resolve the pulsating tier's current frame (steady YELLOW when `now` is `None` —
    fail-open, matching this file's existing None-handling convention).
  - `_pulse_segment`: restructure so all three numbers (`n1`/`n2`/`n3`) are each independently
    colored via the new tiered function against their label's threshold set, instead of only
    `n3` being colored while `n1`/`n2` stay hardcoded DIM.
  - `render_beads_bar`: remove the `next_text` parameter and the phase-branch entirely — the left
    side always renders the `op:`/`bd:` counts (or `""` if neither half is present), no swap, no
    countdown glyph. `now` stays as a parameter, now feeding the pulse-tier animation for both
    labels' numbers.
- **`apps/cc-tmux/src/cc_tmux/cli.py`**: remove `_parse_roadmap_pulse_next` entirely and its call
  site in `_build_beads_bar` (keep `now=time.time()` passed through — still needed for the pulse
  animation).
- **`apps/cc-tmux/src/cc_tmux/testing.py`**: remove `_test_render_beads_bar_phase_and_countdown_glyph`
  and `_test_render_beads_bar_next_cycle` (test deleted functions/behavior); rewrite every
  existing `_test_render_beads_bar*` assertion whose expected string assumed only `n3` is colored
  (every count in every segment now carries its own `#[fg=...]` wrap); add new boundary tests for
  both label's 4 tiers and the pulse animation.
- **`openspec/specs/cc-tmux/spec.md`**: MODIFIED delta for "A dedicated tmux status row surfaces
  open/ready beads and proposals" — removes the phase-cycle contract (restoring permanent
  `op:`/`bd:` visibility, `next:` never rendering), and replaces the 2-tier n3-only color
  contract with the new 4-tier all-numbers-independently-colored contract.

## Non-Goals

- No change to the right-aligned account-identity segment — unaffected by either the removal or
  the recoloring, renders exactly as it does today in every case.
- No change to `nexus-statusline`'s own (independent, in-pane) `next:` display.
- No new data source, fetch, timer, or background process — reuses the existing cache-read and
  the existing wall-clock-pulse mechanism verbatim.
- Does not touch row 1 (tabs) or row 2 (session bar) — this proposal is scoped entirely to row 3.

## Context
- touches: `apps/cc-tmux/src/cc_tmux/render.py`, `apps/cc-tmux/src/cc_tmux/cli.py`, `apps/cc-tmux/src/cc_tmux/testing.py`
- Note: four stale, untracked `openspec/changes/cc-tmux-*` directories currently sit on disk in
  this repo (superseded scaffolds from an earlier `/apply:all` run this session, already fully
  archived in git history — `openspec-status`/`wave-plan-build` still see them since they were
  never deleted per an explicit user decision this session). They nominally share `render.py`/
  `cli.py`/`testing.py` with this proposal's touch list, but they are dead leftovers, not real
  in-flight work — no `- depends on:` is declared against them.

## Testing

- `apps/cc-tmux/src/cc_tmux/testing.py`: unit tests for the new tiered-color function at every
  boundary for both `op` (5/6/10/11/20/21) and `bd` (10/11/20/21/40/41) thresholds; pulse-tier
  animation (tick-parity alternation when `now` is provided, steady YELLOW when `now` is `None`);
  `_pulse_segment`'s per-number independent coloring; `render_beads_bar`'s removal of the
  `next_text`/phase behavior (no `next:` text ever appears, `now` alone no longer changes WHAT
  renders, only how numbers in the pulse tier are colored at that tick).
- Live-verify: `~/.tmux/plugins/cc-tmux/bin/cc-tmux render-all "$WINDOW_ID"` +
  `tmux show-options -g '@cc-row-beads'`, captured at two wall-clock seconds apart, confirming
  `op:`/`bd:` render every tick (never a `next:` line) and a number in the pulse tier visibly
  alternates color between captures.
