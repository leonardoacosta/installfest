# cc-tmux — glyph unification deltas

## MODIFIED Requirements

### Requirement: Animated tab icon reflects state via a wall-clock-driven refresh
The tab icon SHALL be rendered from a top-level status-format job (`#(cc-tmux tabs-row)`) that
composes the ENTIRE window-tabs row itself — icon, index, and name per window, with
active-window highlighting — rather than from the tmux-native per-window
`window-status-format`/`window-status-current-format` options. This relocation is required
because `#()` shell jobs nested inside tmux's default per-window `#{T:window-status-format}`
expansion do not execute on this fleet's tmux version, while top-level status-format jobs are
proven to execute. No background process or timer SHALL be introduced by this plugin to achieve
the animation — the row is re-evaluated on tmux's existing `status-interval` cadence.

All tracked states SHALL speak ONE visual language: the 17-state session-usage ramp — `░` at
state 0, braille fill `⡀⣀⣄⣤⣦⣶⣷` to `⣿` at 50%, braille drain `⢿⠿⠻⠛⠙⠉⠈` toward 93.75%, `▓` at
the top state — indexed by `round(clamp(ratio, 0, 1) * 16)` where `ratio` is the session's
absolute context-token burn over a fixed 1,000,000-token scale. The solid block `█` SHALL NOT
render as any tab state (changed from the prior version, where `█` was the idle no-data
fallback).

Per state:
- `waiting` flashes between two braille glyphs (`◉` colored YELLOW, `◎` default/unstyled) —
  UNCHANGED from the prior version.
- `active` SHALL flash between the ramp glyph at the session's current meter state `i` and the
  ramp glyph at state `min(i + 1, 16)`, alternating by wall-clock tick parity (changed from the
  prior fixed two-glyph braille pair); at meter state 16 the pair SHALL be states 15 and 16 so
  two distinct frames always render. When the session's raw token count is unavailable
  (`None`), the active icon SHALL flash between the state-0 and state-1 ramp glyphs (`░` and
  `⡀`) with no meter colour applied.
- `idle` renders a single-cell session-usage meter: the ramp glyph for the session's meter
  state, coloured by the existing context-severity ramp (`resolve_context_color`) applied to
  the same raw token count — including its pulsing tiers — reused verbatim with no
  meter-specific colour logic. State 0 (nearly fresh) MUST flash by alternating `░` with a
  same-width blank on the same wall-clock parity the colour pulse uses; every other meter state
  renders a data-driven-static glyph. When the session's raw token count is unavailable
  (`None`), the idle icon MUST render the state-0 glyph `░` STATICALLY in DIM styling with no
  meter colour and no flash — a data gap MUST NOT render as the fresh-session flash (rule
  preserved from the prior version; only the fallback glyph changed from `█` to DIM `░`).

The active-state ramp lookup SHALL resolve the session's raw token count for active windows
through the same resolution path idle windows already use, relying on the existing short-TTL
nx-agent response cache — no new cache layer and no per-window network amplification beyond
that existing cache's contract. A window with no tracked Claude pane MUST render no icon at all.

#### Scenario: waiting state pulses between the permission glyphs
- Given: a window's highest-priority tracked state is `waiting`
- When: the live tabs row is captured at two different wall-clock seconds one second apart
- Then: it shows `◉` (colored YELLOW) at one capture and `◎` (default color) at the other,
  alternating by wall-clock tick parity

#### Scenario: active state pulses between ramp-adjacent glyphs
- Given: a window's highest-priority tracked state is `active` and its session's raw token
  count resolves to 70,000 (meter state 1 on the 1M scale)
- When: the live tabs row is captured at two different wall-clock seconds one second apart
- Then: it shows the state-1 glyph (`⡀`) at one capture and the state-2 glyph (`⣀`) at the
  other, alternating by wall-clock tick parity

#### Scenario: active state at the top of the ramp still shows two frames
- Given: an active window whose session's raw token count resolves to meter state 16
- When: the tabs row is captured at two wall-clock seconds of opposite parity
- Then: the icon alternates between the state-15 (`⠈`) and state-16 (`▓`) glyphs

#### Scenario: active state with unavailable usage data pulses the ramp base
- Given: an active window whose session raw-token resolution returns `None`
- When: the tabs row is captured at two wall-clock seconds of opposite parity
- Then: the icon alternates between `░` and `⡀` with no meter colour applied — never a fixed
  braille pair and never the solid block `█`

#### Scenario: idle meter reflects session usage on the 1M scale
- Given: a window's highest-priority tracked state is `idle` with no sub-agent overlay, and its
  session's raw token count resolves to 500,000
- When: the tabs row renders
- Then: that window's icon is `⣿` (state 8 of the ramp), coloured by `resolve_context_color`
  for 500,000 tokens, and the index/name text keeps its unchanged active/inactive colouring

#### Scenario: nearly fresh idle session flashes the light shade block
- Given: an idle window whose session's raw token count resolves below ~31,250 tokens (meter
  state 0)
- When: the tabs row is captured at two wall-clock seconds of opposite parity
- Then: the icon alternates between `░` and a same-width blank — the label column does not shift

#### Scenario: unavailable usage data renders the dimmed ramp base, not a solid block
- Given: an idle window whose session raw-token resolution returns `None` (e.g. nx-agent
  unreachable or no session id)
- When: the live tabs row is captured at any two different wall-clock times
- Then: it shows the state-0 glyph `░` in DIM styling both times — static (no flash), no meter
  colour, and the solid block `█` appears nowhere in the row

#### Scenario: untracked window renders no icon
- Given: a window with no tracked Claude pane (a plain shell)
- When: the live tabs row renders
- Then: that window's entry shows no icon prefix

#### Scenario: the icon actually appears in the live render
- Given: the `tabs-row` job is wired into a top-level status-format slot
- When: the live rendered tab row is byte-captured (e.g. via `tmux display-message -F`)
- Then: the icon glyph is present in the captured output — not silently dropped

### Requirement: The animated tab icon reflects sub-agent activity
The animated tab icon SHALL render a single flashing diamond pair — `◇` alternating with `◆` by
wall-clock tick parity — when a pane has one or more sub-agent dispatches tracked as active,
whether foreground (via a matched `PreToolUse`/`PostToolUse` pair on the `Task` tool) or
background (via the time-boxed heuristic), and regardless of count (changed from the PRIOR
four dedicated braille flash pairs keyed by foreground/background and count 1/2+; per-agent
count and idle detail is delegated to the dedicated sub-agent status row introduced by the
`cc-tmux-row4-session-title` proposal). When no sub-agent activity is tracked for a pane, the
tab icon SHALL render exactly as the "Animated tab icon reflects state via a wall-clock-driven
refresh" Requirement specifies (unchanged). Foreground/background tracking mechanics —
increment/decrement on hook fire, background time-boxed age-out pruning on read — are UNCHANGED
by this requirement version; only the rendering collapses to one pair.

#### Scenario: no sub-agents tracked renders the existing icon unchanged
- Given: a tracked pane with `@cc-subagent-fg` at 0 and no unexpired `@cc-subagent-bg` entries
- When: the tab icon renders
- Then: it shows the existing `@cc-state`-driven glyph (waiting/idle/active), unaffected by
  this Requirement

#### Scenario: any sub-agent activity flashes the diamond pair
- Given: four separate tracked panes, one each in foreground-count-1, foreground-count-2+,
  background-count-1, and background-count-2+ states
- When: each pane's tab icon is captured at two different wall-clock seconds one second apart
- Then: every one of the four panes shows `◇` at one capture and `◆` at the other — the same
  single pair for all four cases

#### Scenario: a foreground sub-agent dispatch increments and decrements the count
- Given: a pane whose Claude session dispatches a foreground (blocking) sub-agent
- When: the dispatch's `PreToolUse` (`Task` matcher) fires
- Then: `@cc-subagent-fg` increments; when the matching `PostToolUse` fires (the dispatch
  returned), it decrements back — and the tab icon returns to its state-driven glyph once both
  counts are zero

#### Scenario: a background dispatch ages out of the active count
- Given: a pane's Claude session dispatches a background sub-agent, recorded in
  `@cc-subagent-bg` with a launch timestamp
- When: more than `@cc-subagent-bg-timeout` seconds have elapsed since that launch
- Then: that entry no longer counts toward the diamond overlay (pruned on read, not
  necessarily deleted immediately)
