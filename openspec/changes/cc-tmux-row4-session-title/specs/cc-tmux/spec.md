# cc-tmux — row-4 session-title/agents deltas

## ADDED Requirements

### Requirement: A dedicated status row shows the session title or per-agent detail
The plugin SHALL publish a fourth status-row payload (`@cc-row-agents` global option, produced
by the same `render-all` job that publishes the session and beads rows) for the focused
window's representative tracked pane. Content contract, in precedence order:

1. When the pane has one or more tracked sub-agent dispatches (foreground count nonzero or
   unexpired background entries), the row SHALL render a glyph strip with one glyph per
   tracked background dispatch in launch order: `◌` alternating with `○` by wall-clock tick
   parity while the dispatch is inside its busy window (not-idle), and a static `●` once the
   dispatch is older than the busy window but not yet aged out (settled/idle). The busy window
   SHALL be a tunable pane/global option (`@cc-subagent-bg-busy-window`) defaulting below the
   existing `@cc-subagent-bg-timeout` age-out. Because no hook signals a background dispatch's
   true completion on this CC fleet, busy-vs-idle is an elapsed-time heuristic — documented as
   such, mirroring the existing background age-out rule's posture.
2. Otherwise, when the pane carries a Claude session title (`@cc-title`, captured from the
   SessionStart `session_title` payload), the row SHALL render that title (truncated to the
   client width).
3. Otherwise the published payload SHALL be empty and the row SHALL NOT occupy a status line
   (see the MODIFIED three-lines requirement below).

The row is display-only: it SHALL NOT introduce any new hook, daemon, or network fetch — all
inputs are existing pane options read during the same `render-all` tick.

#### Scenario: foreground-only titled session shows the title
- Given: the focused window's tracked pane has `@cc-title` set, `@cc-subagent-fg` at 0, and no
  unexpired `@cc-subagent-bg` entries
- When: the status bar renders on a landscape client
- Then: the fourth row renders the session title text

#### Scenario: a busy background agent pulses the dotted circle
- Given: the pane has one `@cc-subagent-bg` entry younger than `@cc-subagent-bg-busy-window`
- When: the row is captured at two wall-clock seconds of opposite parity
- Then: the row shows `◌` at one capture and `○` at the other, in place of the title

#### Scenario: a settled background agent renders the filled circle
- Given: the pane has one `@cc-subagent-bg` entry older than the busy window but younger than
  `@cc-subagent-bg-timeout`
- When: the row renders at any two different wall-clock times
- Then: the row shows a static `●` both times

#### Scenario: multiple background agents render one glyph each
- Given: the pane has two unexpired `@cc-subagent-bg` entries, one inside and one past the
  busy window
- When: the row renders
- Then: the strip shows two glyphs in launch order — a pulsing `◌`/`○` and a static `●`

#### Scenario: no title and no agents publishes an empty payload
- Given: a focused window whose tracked pane has no `@cc-title` and no sub-agent activity
- When: `render-all` publishes the row options
- Then: `@cc-row-agents` is empty and the status bar renders without the fourth row

## MODIFIED Requirements

### Requirement: The tmux status bar is three lines
`home/dot_config/tmux/tmux.conf.tmpl` SHALL set a BASE `status 3` (landscape/desktop default),
and each shipped theme (`vercel-theme.conf`, `one-hunter-vercel-theme.conf`,
`tokyo-night-abyss-theme.conf`, `nord-theme.conf`) SHALL define its session/usage,
beads/proposals, AND agents/title rows via COMPUTED indices (the `@cc-tab-rows` global tmux
option the render job maintains) rather than literal `status-format[N]` indices: session at
`[N]`, beads at `[N+1]`, and — when the `@cc-row-agents` payload is nonempty — agents/title at
`[N+2]` (changed from the prior version, which defined only the session and beads rows). The
render job SHALL set the total line count to `tab_rows + 2`, plus one when the agents-row
payload is nonempty, capped at tmux's hard five-line ceiling; at the cap the agents row is the
one omitted (lowest priority — a portrait client whose tabs wrap to three rows keeps session
and beads rows and drops agents). On a landscape/desktop client with no agents-row content,
this is BYTE-IDENTICAL to the prior fixed behavior (`status 3`, session/beads at `[1]`/`[2]`).
A theme MUST NOT enable the multi-line bar without defining all extra rows relative to
whatever `@cc-tab-rows` resolves to.

#### Scenario: landscape client without agents content is byte-identical to the prior behavior
- Given: a landscape client whose focused window publishes an empty agents-row payload
- When: the status bar renders
- Then: `@cc-tab-rows` resolves to `1`, `status` is `3`, and `status-format[1]`/`[2]` hold the
  session-bar/beads-bar content exactly as the prior requirement version specified

#### Scenario: landscape client with agents content gains a fourth line
- Given: a landscape client whose focused window publishes a nonempty agents-row payload
- When: the status bar renders
- Then: `status` is `4` and the agents/title row renders at `status-format[3]`, below the
  beads row

#### Scenario: portrait client with wrapped tabs shifts the extra rows
- Given: a portrait client where tabs wrap across 2 physical rows and the agents payload is
  nonempty
- When: the status bar renders
- Then: `@cc-tab-rows` resolves to `2`, `status` is `5`, and session/beads/agents render at
  `status-format[2]`/`[3]`/`[4]` — no row collision, no dropped content

#### Scenario: the five-line ceiling drops the agents row first
- Given: a portrait client where tabs wrap across 3 physical rows and the agents payload is
  nonempty
- When: the status bar renders
- Then: `status` is `5`, session/beads render at `status-format[3]`/`[4]`, and the agents row
  is omitted — session and beads rows are never the ones sacrificed

#### Scenario: all four themes define the extra rows relative to the computed index
- Given: `@cc-tab-rows` has resolved to any value N >= 1
- When: any of the four shipped themes is active
- Then: every enabled extra row renders at its computed index (`[N]`, `[N+1]`, and `[N+2]`
  when present) — none falls back to tmux's default pane-list row, regardless of N
