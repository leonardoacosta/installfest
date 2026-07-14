# cc-tmux (delta)

## MODIFIED Requirements

### Requirement: Animated tab icon reflects state via a wall-clock-driven refresh
The tab icon SHALL be rendered from a top-level status-format job (`#(cc-tmux tabs-row)`) that
composes the ENTIRE window-tabs row itself — icon, index, and name per window, with
active-window highlighting — rather than from the tmux-native per-window
`window-status-format`/`window-status-current-format` options. This relocation is required
because `#()` shell jobs nested inside tmux's default per-window `#{T:window-status-format}`
expansion do not execute on this fleet's tmux version (confirmed: a literal job embedded in
`window-status-format` and read back via `#{T:...}` never runs, across repeated timed retries),
while top-level status-format jobs are proven to execute (row 2 and row 3 already render
correctly via exactly this mechanism). No background process or timer SHALL be introduced by
this plugin to achieve the animation — the row is re-evaluated on tmux's existing
`status-interval` cadence, identical to how row 2/row 3 already refresh, just via a job placed
where jobs actually run. Each tracked state SHALL use a distinct visual language: `waiting`
cycles a rising/falling shade pulse (`░▒▓█▓▒░`); `active` cycles a rotating block edge
(`▁▏▔▕`); `idle` renders a single-cell session-usage meter (cc-tmux-idle-tab-usage-meter): a
17-state ramp — `░` at state 0, braille fill `⡀⣀⣄⣤⣦⣶⣷` to `⣿` at 50%, braille drain
`⢿⠿⠻⠛⠙⠉⠈` toward 93.75%, `▓` at the top state — indexed by `round(clamp(ratio, 0, 1) * 16)`
where `ratio` is the session's absolute context-token burn over a fixed 1,000,000-token scale.
The meter glyph's colour MUST come from the existing context-severity ramp
(`resolve_context_color`) applied to the same raw token count — including its pulsing tiers —
reused verbatim with no meter-specific colour logic. State 0 (nearly fresh) MUST flash by
alternating `░` with a same-width blank on the same wall-clock parity the colour pulse uses;
every other meter state renders a data-driven-static glyph that changes only when the
underlying token count changes. When the session's raw token count is unavailable (`None`),
the idle icon MUST fall back to the static glyph `█` with no meter colour applied — a data gap
MUST NOT render as the fresh-session flash. A window with no tracked Claude pane MUST render
no icon at all (not even the idle glyph).

#### Scenario: waiting state pulses through the shade sequence
- Given: a window's highest-priority tracked state is `waiting`
- When: the live tabs row is captured at two different wall-clock seconds one second apart
- Then: it shows two different frames from `░▒▓█▓▒░` for that window, advancing by one position

#### Scenario: active state rotates through the block sequence
- Given: a window's highest-priority tracked state is `active`
- When: the live tabs row is captured at two different wall-clock seconds one second apart
- Then: it shows two different frames from `▁▏▔▕` for that window, advancing by one position

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

#### Scenario: high-burn idle meter pulses via the reused colour ramp
- Given: an idle window whose session's raw token count resolves above 750,000 tokens
- When: the tabs row is captured at two wall-clock seconds of opposite parity
- Then: the meter glyph's colour alternates between the ramp's DARK_RED and RED pulse pair,
  exactly as `resolve_context_color` dictates, with no meter-specific colour rule

#### Scenario: unavailable usage data falls back to the static idle glyph
- Given: an idle window whose session raw-token resolution returns `None` (e.g. nx-agent
  unreachable or no session id)
- When: the live tabs row is captured at any two different wall-clock times
- Then: it shows the static glyph `█` both times, with no meter colour wrap and no flash

#### Scenario: untracked window renders no icon
- Given: a window with no tracked Claude pane (a plain shell)
- When: the live tabs row renders
- Then: that window's entry shows no icon prefix

#### Scenario: the icon actually appears in the live render
- Given: the `tabs-row` job is wired into a top-level status-format slot
- When: the live rendered tab row is byte-captured (e.g. via `tmux display-message -F`)
- Then: the icon glyph is present in the captured output — not silently dropped the way the
  prior per-window `window-status-format` mechanism dropped it
