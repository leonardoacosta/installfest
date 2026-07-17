---
order: 0717a
---

# Change: cc-tmux-glyph-unification

## Why

The window-tab glyph language has drifted into three inconsistent visual systems (screenshot
evidence `docs/screenshots/img-20260717-082038.png`, 2026-07-17):

1. **Two idle representations.** An idle tab renders EITHER the 17-glyph session-usage ramp
   (`render.py` `IDLE_METER_RAMP`, :98-116) when token data resolves, OR the static solid block
   `█` (`IDLE_GLYPH`, :65; also the `raw_tokens is None` fallback in `idle_usage_meter`, :142)
   when it does not. Two idle states read as two different meanings when they are one state with
   a data gap — and during any nx-agent data outage (as happened 2026-07-16 when the CC
   statusline producer was removed, cc `2a6eda0c`) EVERY idle tab collapses to the
   meaning-free solid block.
2. **Active flash is disconnected from the ramp.** `ACTIVE_FLASH_FRAMES` (`⠋`/`⠙`, :75) is a
   fixed braille pair carrying zero session information, while the idle state already encodes
   context burn. An active session at 7% burn should pulse between the two ramp glyphs
   bracketing its position (the 6.25% and 12.5% steps) — same visual language, plus motion.
3. **Sub-agent overlay is four unreadable braille pairs.** `resolve_tab_icon` (:211-232) flashes
   one of four dedicated braille pairs (`⠶⠦`/`⠒⠲`/`⠆⠇`/`⠂⠄`) by fg/bg count. The four-way
   distinction is illegible at a glance; per-agent detail is moving to a dedicated status row
   (separate proposal, `cc-tmux-row4-session-title`), so the tab overlay simplifies to a single
   legible presence signal: the `◇` <-> `◆` diamond swap.

This proposal intentionally REVISES two committed requirements ("Animated tab icon reflects
state via a wall-clock-driven refresh", "The animated tab icon reflects sub-agent activity") —
a deliberate reversal of the solid-block fallback and four-pair overlay decisions, not an
oversight.

## What Changes

- **One idle language, ramp only.** Delete the solid-block idle rendering. Idle with token data:
  ramp glyph exactly as today. Idle with `raw_tokens is None`: static DIM `░` (ramp state 0's
  glyph, dimmed, NO flash) — the flash stays reserved for a genuinely-fresh session, preserving
  the committed "a data gap MUST NOT render as the fresh-session flash" rule while keeping every
  idle tab inside the ramp's visual language.
- **Ramp-adjacent active pulse.** Active tabs pulse between ramp index `i` and `min(i+1, 16)`
  for the session's current burn (at index 16, pulse 15 <-> 16 to keep two-frame contrast),
  wall-clock parity as today. Active with no token data pulses `░` <-> `⡀` (ramp indices 0/1)
  — motion without data still speaks ramp. Requires resolving `raw_tokens` for ACTIVE windows
  too (today idle-only, `cli.py:1778-1783`); nx-agent's on-disk cache (~5s TTL) keeps
  per-window resolution cheap. Colour: `resolve_context_color` on the same raw count,
  unchanged; no-data active pulse renders uncoloured.
- **Diamond sub-agent overlay.** Replace all four braille flash pairs with one `◇` <-> `◆`
  wall-clock swap whenever ANY sub-agent activity is tracked (fg or bg, any count). Foreground
  precedence logic and bg age-out pruning are unchanged — only the rendering collapses.
- **Waiting state untouched** (`◉`/`◎` yellow pulse).

## Impact

- Affected specs: `cc-tmux` (2 MODIFIED requirements)
- Affected code: `apps/cc-tmux/src/cc_tmux/render.py` (glyph constants, `idle_usage_meter`,
  `animated_icon`, `resolve_tab_icon`, `resolve_tab_glyph`), `apps/cc-tmux/src/cc_tmux/cli.py`
  (`_build_tabs_row` raw-tokens resolution scope)
- No hooks.json, tmux.conf.tmpl, or theme-file changes; no new options, no new processes.

## Context

- touches: `apps/cc-tmux/src/cc_tmux/render.py`, `apps/cc-tmux/src/cc_tmux/cli.py`
- Data dependency (cross-repo, NOT an in-repo spec dep): session token data currently fails
  open to `None` fleet-wide because the CC statusline producer was removed (cc `2a6eda0c`,
  2026-07-16). Re-supply is nx-agent work — `nx-qayeb.1` (serve per-session context fields in
  `GET /statusline?sessionId=`) plus the existing consumer-switch task `nx-x2ixd.1`. This
  proposal ships correct behaviour for BOTH data-present and data-absent branches, so it does
  not block on nx; the ramp simply stays in its no-data forms until nx lands.
- Sibling proposal (authored this session, ordered after): `cc-tmux-row4-session-title` —
  absorbs the per-agent detail the four-pair overlay used to carry.

## Testing

- Unit-level (no pytest harness exists in this plugin — direct `python -c` invocations of the
  pure `render.py` functions, pasted stdout as evidence): `idle_usage_meter` (data -> ramp
  glyph; `None` -> static DIM `░`, never `█`), active pulse pair selection (7% -> ramp indices
  1<->2, i.e. the 6.25%/12.5% glyphs; state 16 -> 15<->16; `None` -> `░`<->`⡀`), and
  `resolve_tab_icon` sub-agent branch (any fg/bg count -> `◇`/`◆` by parity; zero activity ->
  state-driven glyph unchanged). Grep-negative: no remaining reference to the four braille
  pair constants or a solid-block idle fallback in tab rendering.
- E2E (live tmux, no daemon): `cc-tmux render-all <window_id>` captured at two wall-clock
  parities against a live active session — assert two distinct adjacent ramp frames; against a
  window with a dispatched foreground sub-agent — assert `◇`/`◆` alternation. N/A for browser
  tooling — this is a terminal status bar.
