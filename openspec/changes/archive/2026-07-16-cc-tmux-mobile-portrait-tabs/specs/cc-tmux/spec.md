## MODIFIED Requirements

### Requirement: The tmux status bar is three lines
`home/dot_config/tmux/tmux.conf.tmpl` SHALL set a BASE `status 3` (landscape/desktop default,
UNCHANGED from the prior requirement version), and each shipped theme (`vercel-theme.conf`,
`one-hunter-vercel-theme.conf`, `tokyo-night-abyss-theme.conf`, `nord-theme.conf`) SHALL define
its session/usage and beads/proposals rows via a COMPUTED index (a `@cc-tab-rows` global tmux
option the render job maintains) rather than the literal `status-format[1]`/`[2]` indices, so
that on a portrait/mobile client where the tabs row wraps across N > 1 physical lines, the
session-bar and beads-bar rows land at `status-format[N]`/`[N+1]` instead of colliding with
wrapped tab content. On a landscape/desktop client (the common case), `@cc-tab-rows` resolves to
`1` and this is BYTE-IDENTICAL to the prior fixed-index behavior — no visible change. A theme
MUST NOT enable the three-plus-line bar without defining both extra rows relative to whatever
`@cc-tab-rows` resolves to.

#### Scenario: landscape client is byte-identical to the prior fixed-index behavior
- Given: a client where `client_width >= client_height` (landscape/desktop)
- When: the status bar renders
- Then: `@cc-tab-rows` resolves to `1`, `status` is `3`, and `status-format[1]`/`[2]` hold the
  session-bar/beads-bar content exactly as the prior requirement version specified

#### Scenario: portrait client with wrapped tabs shifts the extra rows
- Given: a client where `client_height > client_width` (portrait/mobile) and enough windows that
  tabs wrap across 2 physical rows at the mobile tab size
- When: the status bar renders
- Then: `@cc-tab-rows` resolves to `2`, `status` is set to `4`, and the session-bar/beads-bar
  content renders at `status-format[2]`/`[3]` — no row collision, no dropped content

#### Scenario: all four themes define both extra rows relative to the computed index
- Given: `@cc-tab-rows` has resolved to any value N >= 1
- When: any of the four shipped themes is active
- Then: both extra rows render at `status-format[N]`/`[N+1]` — none falls back to tmux's default
  pane-list row, regardless of N

## ADDED Requirements

### Requirement: The window-tabs row adapts to a portrait/mobile client
The plugin SHALL detect portrait orientation from `client_width`/`client_height` (passed as job
arguments to the render-all command, the same pattern `#{window_id}` already uses) and, when
`client_height > client_width`, render the tabs row at an enlarged size instead of the default
landscape sizing. Enlargement SHALL be attempted via Kitty's OSC 66 text-sizing escape sequence
(`s=3`, literal 3x character scale) wrapping each tab's index/icon/name segment, contingent on a
documented fallback: if live verification (see the Testing section of this capability's owning
proposal) finds OSC 66 renders incorrectly or corrupts the surrounding status bar in this
fleet's actual terminal (Ghostty), the plugin SHALL instead render tabs at increased horizontal
padding/spacing only, with no escape-sequence-based scaling. When the combined width of all
enlarged tab segments exceeds `client_width`, the plugin SHALL wrap tabs across multiple
physical status-format rows (see "The tmux status bar is three lines" MODIFIED delta for how the
extra rows shift to accommodate this) rather than truncating or overflowing off the right edge.
On a landscape client, this requirement has NO effect — tabs render exactly as the pre-existing
`render_tabs_row` behavior, single row, default sizing.

#### Scenario: landscape client renders tabs unchanged
- Given: a client where `client_width >= client_height`
- When: the tabs row renders
- Then: every tab renders at default (1x) sizing, in a single physical row, byte-identical to
  the pre-existing behavior

#### Scenario: portrait client enlarges tabs via OSC 66
- Given: a client where `client_height > client_width`, and OSC 66 has been confirmed to render
  correctly in this fleet's terminal (live-verified, not assumed)
- When: the tabs row renders
- Then: each tab's index/icon/name segment is wrapped in an OSC 66 `s=3` escape sequence,
  rendering at approximately 3x the default character size

#### Scenario: portrait client falls back to padding-only sizing if OSC 66 is unreliable
- Given: a client where `client_height > client_width`, and OSC 66 has been found (via live
  verification) to render incorrectly or corrupt the status bar in this fleet's terminal
- When: the tabs row renders
- Then: tabs render at increased horizontal padding/spacing only — no OSC 66 escape sequence is
  emitted, and the surrounding status bar renders correctly

#### Scenario: many tabs on a narrow portrait client wrap to a second row
- Given: a portrait client whose enlarged tab segments' combined width exceeds `client_width`
  when rendered in a single row
- When: the tabs row renders
- Then: tabs wrap across as many physical status-format rows as needed to fit without horizontal
  overflow, and the session-bar/beads-bar rows shift down to accommodate (per the MODIFIED
  "three lines" delta)

#### Scenario: a portrait client with few tabs still fits in one row
- Given: a portrait client whose enlarged tab segments' combined width fits within
  `client_width` in a single row
- When: the tabs row renders
- Then: `@cc-tab-rows` resolves to `1` and no wrapping occurs, even though sizing is enlarged
