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
flashes between two braille glyphs (`◉` colored YELLOW, `◎` default/unstyled — a permission/
question/plan/elicitation pulse, changed from the prior rising/falling shade sequence); `active`
flashes between two braille glyphs (changed from the prior rotating block edge); `idle` renders a
single-cell session-usage meter (cc-tmux-idle-tab-usage-meter): a 17-state ramp — `░` at state 0,
braille fill `⡀⣀⣄⣤⣦⣶⣷` to `⣿` at 50%, braille drain `⢿⠿⠻⠛⠙⠉⠈` toward 93.75%, `▓` at the top
state — indexed by `round(clamp(ratio, 0, 1) * 16)` where `ratio` is the session's absolute
context-token burn over a fixed 1,000,000-token scale. The meter glyph's colour MUST come from
the existing context-severity ramp (`resolve_context_color`) applied to the same raw token
count — including its pulsing tiers — reused verbatim with no meter-specific colour logic. State
0 (nearly fresh) MUST flash by alternating `░` with a same-width blank on the same wall-clock
parity the colour pulse uses; every other meter state renders a data-driven-static glyph that
changes only when the underlying token count changes. When the session's raw token count is
unavailable (`None`), the idle icon MUST fall back to the static glyph `█` with no meter colour
applied — a data gap MUST NOT render as the fresh-session flash. A window with no tracked Claude
pane MUST render no icon at all (not even the idle glyph).

#### Scenario: waiting state pulses between the permission glyphs
- Given: a window's highest-priority tracked state is `waiting`
- When: the live tabs row is captured at two different wall-clock seconds one second apart
- Then: it shows `◉` (colored YELLOW) at one capture and `◎` (default color) at the other,
  alternating by wall-clock tick parity

#### Scenario: active state flashes between two braille glyphs
- Given: a window's highest-priority tracked state is `active`
- When: the live tabs row is captured at two different wall-clock seconds one second apart
- Then: it shows two different frames from the dedicated active-state braille pair for that
  window, alternating by wall-clock tick parity

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

### Requirement: The animated tab icon reflects sub-agent activity
The animated tab icon SHALL render one of four distinct FLASHING glyph pairs when a pane has one
or more sub-agent dispatches tracked as active — foreground, via a matched `PreToolUse`/
`PostToolUse` pair on the `Task` tool, or background, via a time-boxed heuristic since no hook
signals a background dispatch's true completion — instead of its normal `@cc-state`-driven
animation. Each of the four cases (foreground count 1, foreground count 2+, background count 1,
background count 2+) SHALL flash between two braille glyphs dedicated to that case, alternating
by wall-clock tick parity — changed from the PRIOR static single-glyph-per-case rendering
(`◎`/`◉`/`◇`/`◆`, none of which were animated). When no sub-agent activity is tracked for a pane,
the tab icon SHALL render exactly as the existing "Animated tab icon" Requirement already
specifies (unchanged). Foreground activity SHALL take precedence over background activity when
both are nonzero, since foreground tracking is an exact signal and background tracking is a
heuristic — this precedence rule is UNCHANGED by this requirement version.

#### Scenario: no sub-agents tracked renders the existing icon unchanged
- Given: a tracked pane with `@cc-subagent-fg` at 0 and no unexpired `@cc-subagent-bg` entries
- When: the tab icon renders
- Then: it shows the existing `@cc-state`-driven glyph (waiting/idle/active), unaffected by this
  Requirement

#### Scenario: a foreground sub-agent dispatch increments and decrements the count
- Given: a pane whose Claude session dispatches a foreground (blocking) sub-agent
- When: the dispatch's `PreToolUse` (`Task` matcher) fires
- Then: `@cc-subagent-fg` increments; when the matching `PostToolUse` fires (the dispatch
  returned), it decrements back

#### Scenario: a background dispatch ages out of the active count
- Given: a pane's Claude session dispatches a background sub-agent, recorded in
  `@cc-subagent-bg` with a launch timestamp
- When: more than `@cc-subagent-bg-timeout` seconds have elapsed since that launch
- Then: that entry no longer counts toward the tab icon's sub-agent-activity glyph (pruned on
  read, not necessarily deleted immediately)

#### Scenario: foreground activity takes precedence over background
- Given: a pane with both a running foreground sub-agent and an unexpired background entry
- When: the tab icon renders
- Then: it reflects the foreground count's flashing glyph pair, not the background one

#### Scenario: each of the four sub-agent cases flashes its own dedicated glyph pair
- Given: four separate tracked panes, one each in foreground-count-1, foreground-count-2+,
  background-count-1, and background-count-2+ states
- When: each pane's tab icon is captured at two different wall-clock seconds one second apart
- Then: each pane shows two different braille frames from ITS OWN dedicated pair — no two of the
  four cases share a frame pair, preserving the pre-existing visual distinctness between all
  four states
