---
status: draft
order: 0715a
---

# Proposal: cc-tmux-row3-pour-transition

## Why

`cc-tmux-row3-next-cycle` (merged, archived at
`openspec/changes/archive/2026-07-15-cc-tmux-row3-next-cycle/`) shipped row 3 cycling between the
`op:`/`bd:` counts and the `next:` line on an 8-second wall-clock timer (`SWAP_PERIOD_SEC`), with a
countdown glyph prefix showing time-to-swap. The swap itself is instant — the moment the phase
flips, the whole line changes in one render tick with no transition.

Leo's ask (`/openspec:explore`, this session): give the swap itself a "characters pouring up into
place" animation instead of an instant cut. Explored via two live tmux-popup demos this session:

1. A `terminaltexteffects` (TTE) library demo (`tte pour --pour-direction up`) — established the
   target visual language: whichever line arrives, its characters rise into place, and BOTH lines
   (`op:`/`bd:` and `next:`) get the identical treatment when they arrive — approved.
2. A follow-up demo made explicit that any transition mechanism belongs on the row's EXISTING
   automatic 8s ticker, in place — not a separate manually-triggered popup, and not a new runtime
   dependency (see Non-Goals).

This proposal is the second MODIFIED delta to the same requirement `cc-tmux-row3-next-cycle`
introduced ("A dedicated tmux status row surfaces open/ready beads and proposals") — it does not
touch the phase-selection logic (`beads_bar_phase`) or the countdown glyph, only the moment right
after a swap, where the newly-active line's characters now sweep left-to-right through a short
block-height ramp (`▁` → `▄` → `▇` → the real character) instead of appearing instantly. Characters
further right in the line settle slightly later than characters on the left — a left-to-right wave
that reads as the line "pouring up" into place, bounded to a fixed short duration regardless of
line length (never longer than ~4 seconds, well inside the 8s phase, so it never overlaps the next
swap).

## What Changes

- **`apps/cc-tmux/src/cc_tmux/render.py`**: add `POUR_FRAMES: Tuple[str, ...] = ("▁", "▄", "▇")` —
  three stdlib Unicode block-height glyphs (low, mid, high) — and `POUR_STAGGER_TICKS = 2` (the
  extra delay, in ticks, applied to the last character relative to the first; earlier characters
  interpolate linearly between them). Add `pour_transition_text(text: str, tick_in_phase: int) ->
  str`: a pure function returning `text` with each character replaced by the appropriate
  `POUR_FRAMES` glyph (or the real character once that position has "settled"), per
  `design.md` § Algorithm. Extend `render_beads_bar` to compute `tick_in_phase =
  int((now % SWAP_PERIOD_SEC) / FRAME_PERIOD_SEC)` (reusing the existing `FRAME_PERIOD_SEC`
  constant, same wall-clock-tick idiom `animated_icon` already uses) and run `phase_content`
  through `pour_transition_text` before it's assembled into the row — applied identically whether
  `phase_content` is the `op:`/`bd:` counts or the `next:` line, matching the approved demo's "same
  motion regardless of which line" principle. `now is None` (legacy/default) is UNCHANGED — the
  transition only ever engages when `now` is provided, exactly like the existing phase-cycling
  logic it extends.
- **`apps/cc-tmux/src/cc_tmux/testing.py`**: self-tests for `pour_transition_text` (frame
  progression, left-to-right stagger, settles to real text within the fixed worst-case tick
  budget, empty-string edge case) and an extended `render_beads_bar` self-test confirming the
  transition fires identically for both phases and never extends past its fixed duration.
- **`openspec/specs/cc-tmux/spec.md`**: MODIFIED delta on the same requirement
  (`cc-tmux-row3-next-cycle`'s delta) — adds the pour-transition behavior as an additional clause
  + scenarios; the phase-selection, countdown-glyph, `radar:`-exclusion, and account-segment
  behavior are UNCHANGED and restated verbatim.

## Non-Goals

- No new runtime dependency. `terminaltexteffects` was used only for this session's throwaway
  demo venv (never committed). `apps/cc-tmux/pyproject.toml` states `dependencies = []` /
  "No runtime dependencies: stdlib-only by design" — this proposal keeps that invariant intact by
  hand-rolling the effect as a small pure function, extending the same pattern `animated_icon` /
  `idle_usage_meter` / `beads_bar_countdown_glyph` already establish in `render.py`.
- No new popup, no new keybinding, no new mouse-range, no new tmux command. The transition lives
  entirely inside the row's existing automatic cycle — nothing about how/when it fires changes,
  only what the swap itself looks like.
- No change to `beads_bar_phase`, `SWAP_PERIOD_SEC`, `beads_bar_countdown_glyph`, the `op:`/`bd:`
  count computation, the `next:` extraction, the `radar:` exclusion, or the account-identity
  segment — all unchanged, restated verbatim in the spec delta.
- No change to `nexus-statusline`'s trailing-line rendering inside the CC pane — unaffected,
  separate repo, separate rendering path.

## Context

- Related: `openspec/changes/archive/2026-07-15-cc-tmux-row3-next-cycle/` — the requirement this
  proposal modifies a second time; introduced `SWAP_PERIOD_SEC`, `beads_bar_phase`,
  `beads_bar_countdown_glyph`, and the phase-cycling contract this proposal extends without
  altering.
- Related: `openspec/changes/archive/2026-07-14-cc-tmux-idle-tab-usage-meter/` — established the
  wall-clock-driven, daemon-free, stdlib-only animation pattern (`FRAME_PERIOD_SEC`,
  `int(now / period)` phase selection) this proposal's `tick_in_phase` computation reuses.
- Design reference (not committed, scratch-only, this session): a `terminaltexteffects` demo venv
  and two tmux-popup demo scripts established the approved visual language (characters rise
  left-to-right into place, identical treatment for both lines) that `design.md`'s hand-rolled
  algorithm reproduces without the library.
- touches: `apps/cc-tmux/src/cc_tmux/render.py`, `apps/cc-tmux/src/cc_tmux/testing.py`,
  `openspec/specs/cc-tmux/spec.md`

## Testing

| Seam | Coverage |
| --- | --- |
| `pour_transition_text(text, tick_in_phase)` | Unit self-tests — frame-by-frame progression at each tick, left-to-right stagger ordering, settles to the exact real text within the fixed worst-case tick budget regardless of text length, empty-string input |
| `render_beads_bar` transition wiring | Unit self-tests — transition applies identically to phase-0 (counts) and phase-1 (next) content, `now=None` stays byte-identical to pre-transition behavior, transition never extends into the following phase |
| End-to-end live render | Live verification task: capture the real rendered row-3 status format at consecutive ticks immediately after a real swap boundary, confirm the visible left-to-right glyph progression, confirm it settles to the exact real text well before the next swap |
