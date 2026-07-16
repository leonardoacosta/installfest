# Design: cc-tmux-braille-flash-and-permission-pulse

## Glyph picks

Five independent 2-frame braille flash pairs, chosen for visual distinctness from each other and
from every other glyph already in use in this file (circle `○`/`●`/`◎`/`◉`-freed, diamond
`◇`/`◆`, square `□`/`■`, and the idle-meter's braille fill ramp `⡀⣀⣄⣤⣦⣶⣷⣿`/`⢿⠿⠻⠛⠙⠉⠈`). These are
starting picks, not load-bearing — swap freely at implementation time if a live capture doesn't
read as intended; the CONTRACT that matters is "two distinct braille glyphs, flashed on tick
parity," not these exact code points.

| Constant | Frames | Rationale |
| --- | --- | --- |
| `ACTIVE_FLASH_FRAMES` | `("⠋", "⠙")` | The `cli-spinners` "dots" braille pair — widely recognized as a generic "working" indicator, fits `active`/thinking. |
| `SUBAGENT_FG1_FLASH_FRAMES` | `("⠒", "⠲")` | Distinct low-dot pattern, echoes a single small mark (mirrors the "1" tier). |
| `SUBAGENT_FG2PLUS_FLASH_FRAMES` | `("⠶", "⠦")` | Denser pattern than the FG-1 pair (mirrors "2+" being visually "more"). |
| `SUBAGENT_BG1_FLASH_FRAMES` | `("⠂", "⠄")` | Sparse single-dot pair, distinct from FG-1's `⠒`/`⠲` despite the shared "tier 1" concept — background stays visually lighter/heuristic-feeling than foreground's exact signal. |
| `SUBAGENT_BG2PLUS_FLASH_FRAMES` | `("⠆", "⠇")` | Denser than BG-1, mirroring FG's 1-vs-2+ density relationship. |
| `PERMISSION_PULSE_FRAMES` | `("◉", "◎")` | Explicit in the ask — NOT braille, reuses the freed circle-with-dot glyphs. `◉` colored YELLOW, `◎` default/unstyled. |

## Wiring

- `animated_icon(state, now)`: the `active` branch returns
  `ACTIVE_FLASH_FRAMES[int(now / FRAME_PERIOD_SEC) % 2]` instead of indexing `BLOCK_FRAMES` by
  `% len(BLOCK_FRAMES)` — same tick source, just a 2-frame modulo instead of 4-frame. The
  `waiting` branch returns `PERMISSION_PULSE_FRAMES[int(now / FRAME_PERIOD_SEC) % 2]` instead of
  indexing `SHADE_FRAMES` — same tick source, glyph only (no color at this layer — see below).
- `resolve_tab_icon(state, now, fg_count, bg_count)`: each of the four `return SUBAGENT_*`
  branches becomes `return SUBAGENT_*_FLASH_FRAMES[int(now / FRAME_PERIOD_SEC) % 2]` — same
  precedence order (fg_count >= 2 -> fg_count == 1 -> bg_count >= 2 -> bg_count == 1 -> fall
  through to `animated_icon`), unchanged.
- **Coloring the permission pulse — corrected during this proposal's own authoring (initial
  draft wrongly assumed an existing color-wrap; verified against the real code instead):**
  `animated_icon`/`resolve_tab_icon` both return a BARE glyph string today, no color at all —
  `render_tabs_row` (render.py:888) gets its per-glyph color exclusively through
  `resolve_tab_glyph` (render.py:241), which returns a `(glyph, color)` pair and currently emits
  a real color ONLY for the idle-usage-meter case (`return idle_usage_meter(raw_tokens, now)`);
  every other case returns `(resolve_tab_icon(...), "")` — empty color, no wrap. `resolve_tab_glyph`
  needs a new branch: when `state == "waiting"` AND the current frame is `PERMISSION_PULSE_FRAMES[0]`
  (`◉`), return `(icon, YELLOW)`; when the frame is `PERMISSION_PULSE_FRAMES[1]` (`◎`), return
  `(icon, "")` (no wrap — reads as default/unstyled). `render_tabs_row`'s existing
  `if meter_color: icon_part = f"#[fg={meter_color}]{glyph}#[fg={colour}] "` wrapping logic
  (render.py:951-954) needs NO change — it already handles "wrap the glyph in this color, then
  restore the label's own color" generically; it just needs `resolve_tab_glyph` to hand it a
  real color string for this new case instead of always `""`.
