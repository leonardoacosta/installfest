# Design: cc-tmux-idle-tab-usage-meter

## The 17-state ramp (Leo's sketch, normalized to 16ths)

Leo's stated boundaries (6/12.5/18/25/32/37.5/44/50/56/62.5/68/75/82/87.5/94) are the 6.25%
multiples rounded for prose — the meter is a **single braille/shade cell advancing one state per
1/16th of the scale**. Fill phase adds dots bottom-up to full at 50%; drain phase removes dots
bottom-up toward the top-right at ~94%; the extremes use shade blocks. Canonical table
(index -> glyph, nominal value = index * 6.25%):

```python
IDLE_METER_RAMP = (
    "░",  # 0   — 0%      (flash: alternates with U+2800 blank on FRAME_PERIOD_SEC parity)
    "⡀",  # 1   — 6.25%
    "⣀",  # 2   — 12.5%
    "⣄",  # 3   — 18.75%
    "⣤",  # 4   — 25%
    "⣦",  # 5   — 31.25%
    "⣶",  # 6   — 37.5%
    "⣷",  # 7   — 43.75%
    "⣿",  # 8   — 50%
    "⢿",  # 9   — 56.25%
    "⠿",  # 10  — 62.5%
    "⠻",  # 11  — 68.75%
    "⠛",  # 12  — 75%
    "⠙",  # 13  — 81.25%
    "⠉",  # 14  — 87.5%
    "⠈",  # 15  — 93.75%
    "▓",  # 16  — 100%
)
```

**Index function**: `index = round(max(0.0, min(1.0, ratio)) * 16)` — round-to-nearest of
16ths, so each glyph covers +/-3.125% around its nominal value (e.g. `⣿` covers
46.9%–53.1%, `▓` kicks in at >=96.9%). Round-to-nearest (not floor) is chosen because Leo's
table labels glyphs by nominal value, and it makes 0.5 land exactly on `⣿` — the mnemonic
anchor of the whole ramp. The fill-then-drain shape is unambiguous in practice: bottom-heavy
dots = below 50%, top-heavy = above, and the ramp color (GREEN vs ORANGE/RED bands) makes the
two phases impossible to confuse at a glance.

**Scale**: `ratio = raw_tokens / 1_000_000` (module constant `IDLE_METER_SCALE_TOKENS`).
Absolute burn, deliberately the SAME domain as the color ramp — unlike row 2's two-scale
split (fill=window%, color=absolute), a one-cell meter gets one scale, and Leo specified 1M.
Consequence stated in proposal Why #3: this reads spend, not wall-proximity.

## Color + pulse: `resolve_context_color` verbatim (locked)

Leo explicitly chose (AskUserQuestion, explore session) reusing `_context_color_pair` /
`resolve_context_color` as-is over a bespoke ramp. So the meter's color tiers are: DIM<=100K,
GREEN>100K, YELLOW>200K, ORANGE>300K, RED>500K, RED<->BRIGHT_RED pulse>600K, DARK_RED<->RED
pulse>750K. His sketch's "green below 187.5K" maps to the existing DIM/GREEN band (boundary
200K); his ">75% pulse" is the existing 750K tier — the 600K pulse tier comes along with the
reuse, accepted. Do NOT re-open this; a second color scale is the drift class
`_resolve_ses_pct` sharing exists to prevent.

## Flash (<~3.1%, index 0): glyph alternation, not color

"Flash the light shade block": index 0 alternates `░` with `"⠀"` (U+2800 blank braille — SAME
column width, so the tab label never shifts) on `int(now / FRAME_PERIOD_SEC) % 2` — the exact
parity idiom `resolve_context_color` already uses for pulse. Color at index 0 is whatever the
ramp says (DIM at those token counts). A fresh idle session therefore blinks softly: "this tab
is cheap to pick up."

## `None` fallback: static `█`, never `░`

`idle_usage_meter(None, now) == (IDLE_GLYPH, "")` — byte-identical to today's idle rendering,
no color wrap emitted. Rationale: `nx_agent.session_context()` currently returns `None` for
every tracked session on this machine (known, separately-filed gap), and a flashing `░` for
missing data would falsely advertise every stalled-data tab as a fresh session. Empty color
string means `render_tabs_row` emits NO `#[fg=...]` wrap for the icon — the segment renders
exactly as today.

## API shape: additive wrapper, `resolve_tab_icon` untouched

`resolve_tab_icon` keeps its exact signature/behavior (legacy `cmd_window_icon` still calls it,
and that path is monochrome + documented-dead on tmux 3.6a — it keeps rendering `█` for idle
via the `None` default, no edit needed). New pure function:

```python
def resolve_tab_glyph(state, now, fg_count, bg_count, raw_tokens=None) -> Tuple[str, str]:
    # sub-agent overlay and waiting/active: (resolve_tab_icon(...), "") — unchanged
    # plain idle (fg==0 and bg==0 and state=="idle"): idle_usage_meter(raw_tokens, now)
```

Precedence is IDENTICAL to `resolve_tab_icon`'s documented order — sub-agent overlays beat the
meter (a session running sub-agents isn't "ready" anyway).

`render_tabs_row` reads `raw_tokens = getattr(w, "raw_tokens", None)` (duck-typed, same
convention as `fg`/`bg`) and composes the icon part as:

```
#[fg={meter_color}]{glyph}#[fg={label_colour}]   # only when meter_color != ""
```

inside the existing `#[fg={label_colour}]#[range=window|{index}] ... ` segment, so index/name
keep CYAN-bold (active) / DIM (inactive) exactly as today. Styled `#[]` runs in this row are
proven — the segment wrapper + `#[range=window|...]` markup already ship.

## Data plumbing + cost (`_build_tabs_row`)

For each window with `state == "idle"` and no sub-agent overlay (`fg == 0` and pruned
`bg` empty): resolve the representative pane via `_resolve_session_pane(w.id)` and set
`w.raw_tokens = _resolve_ses_tokens(pane)`. All other windows skip resolution entirely
(their glyphs never consume tokens). Cost bounds:

- HTTP: `_resolve_ses_tokens` -> `_resolve_context_window` -> `nx_agent.session_context`,
  which is disk-cached AND negative-cached per session-id (`nx_agent._fetch_cached`) — at most
  one probe per session per TTL regardless of tick rate or window count.
- Per-tick subprocess: `_resolve_session_pane` adds tmux calls per IDLE window per tick. Row 2
  already pays this once per tick; a session-wide tabs row multiplies it by idle-window count.
  Acceptable at this fleet's window counts (<10); if it ever measures slow, the fix is caching
  pane resolution on the window object — out of scope now, noted for the record.

## Spec delta: MODIFIED, not ADDED

Parent requirement "Animated tab icon reflects state via a wall-clock-driven refresh" exists in
`openspec/specs/cc-tmux/spec.md` (archive-safe MODIFIED). The delta rewrites the idle clause:
idle renders the usage meter — data-driven-static in the mid range, flash at index 0,
color-pulse per the reused ramp tiers, static `█` on `None` — and replaces the "idle state
never animates" scenario with meter scenarios. `waiting`/`active` clauses, the untracked-window
clause, and the sub-agent requirement are untouched.

## Batch mapping (no traditional DB/API/UI/E2E layers — same convention as prior cc-tmux changes)

| Literal header | What actually lives there |
| --- | --- |
| `## DB Batch` | Ramp table + index function + scale constant (the data layer) |
| `## API Batch` | `idle_usage_meter` + `resolve_tab_glyph` (pure functions) |
| `## UI Batch` | `render_tabs_row` styling + `_build_tabs_row` plumbing (visible surface) |
| `## E2E Batch` | self-tests + live verification |
