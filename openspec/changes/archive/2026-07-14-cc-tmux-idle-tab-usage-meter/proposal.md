---
status: draft
---

# Proposal: cc-tmux-idle-tab-usage-meter

## Why

An idle ("ready") window tab today renders the static glyph `█` (`render.py:71` `IDLE_GLYPH`,
returned by `animated_icon`/`resolve_tab_icon` and composed into row 1 by `render_tabs_row`).
That glyph says only "this session is done and waiting for you" — it carries zero information
about HOW MUCH session budget that pane has already burned, which is exactly the signal Leo
wants when choosing which idle tab to pick up next (a fresh 40K session and a 900K
near-exhaustion session currently look identical on the tab row).

Leo's sketch (2026-07-14 `/openspec:explore`, this session): replace the idle `█` with a
**17-state single-cell usage meter** — flash `░` when nearly fresh, fill braille dots bottom-up
`⡀⣀⣄⣤⣦⣶⣷` to `⣿` at 50%, then drain them bottom-up `⢿⠿⠻⠛⠙⠉⠈` toward 94%, and `▓` above —
with the fill keyed on absolute session tokens against a 1M scale and the color following the
existing context-severity ramp. Design decisions locked during exploration (AskUserQuestion,
recorded):

1. **Color/pulse reuses `_context_color_pair` + `resolve_context_color` verbatim** (Leo chose
   reuse-verbatim over a bespoke 75%-only pulse): DIM<=100K, GREEN>100K, YELLOW>200K,
   ORANGE>300K, RED>500K, RED-pulse>600K, DARK_RED-pulse>750K. One color source of truth —
   the same "don't let two paths diverge" principle `_resolve_ses_pct` sharing established.
2. **`None` data falls back to today's static `█`**, never the flashing `░` — a data gap must
   not masquerade as a fresh session. This matters because `nx_agent.session_context()` is
   KNOWN to return `None` for every tracked session on this machine today (pre-existing gap,
   documented in `archive/2026-07-14-cc-tmux-braille-usage-glyph/proposal.md` and the open
   `cc-tmux-status-bar-popup-polish` proposal; filed separately, NOT in scope here). The meter
   ships render-ready and lights up when the data gap is fixed.
3. **Fill scale = raw session tokens / 1M** (absolute burn, matching the color ramp's domain),
   per Leo's explicit "all the way to 1M". A 200K-window session hits its context wall at only
   ~19% fill — the row-2 token-count text remains the wall-proximity signal; this meter reads
   spend, not wall distance.

This directly contradicts the shipped spec's "idle renders a single static glyph, never
animated" clause (`openspec/specs/cc-tmux/spec.md` Requirement: Animated tab icon reflects
state via a wall-clock-driven refresh, incl. scenario "idle state never animates") — resolved
via a MODIFIED delta in this change: idle stays data-driven-static in the mid range, and
animates ONLY at the extremes (flash when nearly fresh, color-pulse per the existing ramp
tiers). The daemon-free invariant is untouched — all animation remains wall-clock parity on
tmux's existing `status-interval` re-render, exactly like `resolve_context_color` today.

## What Changes

- **`apps/cc-tmux/src/cc_tmux/render.py`**: add the 17-glyph ramp table + index function
  (`round(clamp(ratio) * 16)`), and `idle_usage_meter(raw_tokens, now) -> (glyph, color)` —
  `None` -> `(IDLE_GLYPH, "")` (unstyled fallback, today's exact rendering); index 0 alternates
  `░` with a same-width blank on `FRAME_PERIOD_SEC` parity (the flash); color =
  `resolve_context_color(raw_tokens, now)` reused verbatim (pulse tiers included). Add
  `resolve_tab_glyph(state, now, fg_count, bg_count, raw_tokens) -> (glyph, color)` wrapping
  `resolve_tab_icon`: routes ONLY the plain-idle case (no sub-agent overlay) to the meter;
  every other state/overlay returns `(existing glyph, "")` unchanged.
- **`apps/cc-tmux/src/cc_tmux/render.py`**: `render_tabs_row` consumes a per-window duck-typed
  `raw_tokens` attribute (default `None`) and, when the meter carries a color, wraps just the
  icon as `#[fg={meter_color}]{glyph}#[fg={label_colour}]` inside the segment so the
  index/name keep their existing CYAN-bold/DIM colors.
- **`apps/cc-tmux/src/cc_tmux/cli.py`**: `_build_tabs_row` resolves `raw_tokens` for idle,
  non-sub-agent windows only (representative pane via `_resolve_session_pane`, tokens via the
  existing `_resolve_ses_tokens`) — bounded by `nx_agent`'s existing disk+negative cache, no
  new HTTP cadence. Legacy `cmd_window_icon` stays byte-identical (documented-dead path on
  this fleet's tmux).
- **`openspec/specs/cc-tmux/spec.md`**: MODIFIED delta on "Animated tab icon reflects state
  via a wall-clock-driven refresh" — replaces the idle-never-animates clause/scenario with the
  meter contract (fill states, flash, ramp-driven color/pulse, `None` fallback).

## Non-Goals

- No fix for the `session_context()` -> `None` production gap (separate, already filed) — this
  change is render-layer only and degrades to today's exact idle rendering until data flows.
- No change to `waiting`/`active` animations, the sub-agent overlay glyphs or their precedence
  (`resolve_tab_icon` order), or rows 2/3.
- No new color logic — `_context_color_pair`/`resolve_context_color` reused as-is; Leo
  explicitly declined a bespoke 187.5K-green/750K-only-pulse variant.
- No change to `cmd_window_icon` (legacy per-window path, never re-evaluates on tmux 3.6a).
- No configurability (`@cc-*` option) for the meter scale or ramp — 1M and the 17-glyph table
  are fixed until asked otherwise.

## Context

- Extends: `render.py` tab-icon stack (`animated_icon`/`resolve_tab_icon`/`render_tabs_row`)
  and reuses `_context_color_pair`/`resolve_context_color` (color+pulse) plus cli.py's
  `_resolve_session_pane`/`_resolve_ses_tokens` (data path) — reuse search done in the explore
  session; nothing new below the ramp table itself.
- Related: `openspec/changes/archive/2026-07-14-cc-tmux-braille-usage-glyph/` — braille
  precedent + self-test harness pattern + the color-single-source doctrine this follows;
  `openspec/changes/archive/2026-07-13-cc-tmux-git-status-glyphs/` — tasks.md structural
  template. Open `cc-tmux-status-bar-popup-polish` overlaps on FILES only (different
  functions) — wave-level serialization via touches, no logical dependency.
- touches: `apps/cc-tmux/src/cc_tmux/render.py`, `apps/cc-tmux/src/cc_tmux/cli.py`, `apps/cc-tmux/src/cc_tmux/testing.py`, `openspec/specs/cc-tmux/spec.md`

## Testing

| Seam | Coverage |
| --- | --- |
| Ramp table + index function (pure: ratio -> glyph) | `cc-tmux self-test`: boundary sweep — 0.0 flash-frame pair, nominal 16ths hit Leo's exact glyphs (0.0625 `⡀`, 0.5 `⣿`, 0.75 `⠛`, 0.9375 `⠈`), >=0.969 `▓`, clamp above 1.0 — task 4.1 |
| `idle_usage_meter` (fallback, flash, color reuse) | `cc-tmux self-test`: `None` -> `(IDLE_GLYPH, "")` exactly; index-0 alternates glyph across two wall-clock parities; color equals `resolve_context_color` verbatim for a tier-per-tier sweep incl. a pulsing tier at both parities — task 4.1 |
| `resolve_tab_glyph` precedence | `cc-tmux self-test`: waiting/active/sub-agent-overlay outputs byte-identical to `resolve_tab_icon` with empty color; only plain-idle routes to the meter — task 4.2 |
| `render_tabs_row` wiring | `cc-tmux self-test`: idle window with `raw_tokens` renders meter glyph + `#[fg=` color wrap then restores label colour; window without `raw_tokens` renders today's exact segment; active-window CYAN-bold highlighting unchanged — task 4.2 |
| `_build_tabs_row` data plumbing | `cc-tmux self-test` or direct-call check: idle window gets `raw_tokens` populated via the `_resolve_ses_tokens` path, non-idle windows skip resolution — task 4.2 |
| End-to-end live render | Live verification on the real status bar (plugin re-registered via `tmux run-shell`), real data first, illustrative raw_tokens via the nx-agent cache file if the known `None` gap persists — paste captured row output — task 4.3 |
