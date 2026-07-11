# cc-tmux Specification Delta

## ADDED Requirements

### Requirement: Usage polling is consolidated to a single Anthropic caller
`nexus-agent`'s credential-usage-poller SHALL be the only process that calls Anthropic's
`/api/oauth/usage` endpoint. It SHALL write the active credential's polled 5-hour/7-day usage to a
shared, on-disk cache file on every successful poll tick. `nexus-statusline` SHALL read that cache
file instead of independently calling Anthropic; its own direct Anthropic usage-polling code path
SHALL be removed.

#### Scenario: statusline reads the shared cache, not Anthropic
- Given: nexus-agent's poller has successfully written the shared usage-cache file
- When: nexus-statusline resolves usage data for a render
- Then: it reads the cache file and does not issue an HTTP request to Anthropic

#### Scenario: stale or missing cache degrades to no gauges
- Given: the shared usage-cache file is missing or unreadable
- When: nexus-statusline resolves usage data for a render
- Then: the usage gauges are omitted from that render (same degradation as an Anthropic call
  failing today) — no exception, no stale-forever data

### Requirement: A minimal per-render session-context field bridges the one field only nexus-statusline can see
`nexus-statusline` SHALL write the current session's context-window used-percentage to a per-pane
cache file on every render, gated on the `TMUX_PANE` environment variable being set. This is
scoped to exactly this one field — no other per-render field (cost, lines, clock, model, output
style, speed, worktree, spec) SHALL be written by this mechanism.

#### Scenario: context percentage reaches cc-tmux
- Given: nexus-statusline has written a per-pane session-context cache file for the active pane
- When: cc-tmux's session-bar command reads that file for the pane's window
- Then: the session ("SES") usage percentage renders in the tmux status bar

#### Scenario: no TMUX_PANE means no write
- Given: a Claude Code process running outside tmux (no `TMUX_PANE` set)
- When: nexus-statusline renders
- Then: no session-context cache file is written for that process

### Requirement: A dedicated tmux status row shows session identity and usage
The plugin SHALL render a dedicated tmux status row (`status-format[1]`) showing, left-justified,
a session-count indicator, a single-letter model tag (Fable=F, Opus=O, Haiku=H, Sonnet=S), the
project code, and the git branch; and, right-justified, the account label with session (context
%), 5-hour, and 7-day usage percentages. This row SHALL be separate from the window-tabs row —
not folded into the existing single-line status-right segment.

#### Scenario: row 2 renders the left-side session identity
- Given: a tracked Claude pane in project `if` on branch `main`, model Sonnet, and it is the only
  tracked session for that project
- When: the session-bar row renders
- Then: the left side shows `◉ S if>main` (session-count glyph, model letter, project, branch)

#### Scenario: row 2 renders the right-side usage
- Given: the active account's session/5-hour/7-day usage percentages are available
- When: the session-bar row renders
- Then: the right side shows the account label followed by `SES:`, `5H:`, and `7D:` percentages,
  each colored per the existing `color_for` thresholds

#### Scenario: untracked window shows nothing on this row
- Given: a tmux window with no tracked Claude pane
- When: the session-bar row renders for that window
- Then: the row is empty (no session identity, no usage) for that window

### Requirement: A dedicated tmux status row surfaces open/ready beads and proposals
The plugin SHALL render a second dedicated tmux status row (`status-format[2]`) showing the
current project's cached roadmap-pulse line (top actionable item plus compact open/updated
counts), read directly from `~/.claude/scripts/state/roadmap-pulse.<code>.line`. No new data
production mechanism SHALL be introduced for this row — it reads the cache nexus-statusline's own
`getRoadmapPulse()` already maintains.

#### Scenario: row 3 renders the project's roadmap-pulse line
- Given: a cached roadmap-pulse line exists for the current project's code
- When: the beads-bar row renders
- Then: it shows that cached content verbatim (e.g. `next: <item> <open-count> <updated-count>`)

#### Scenario: no cache yet renders nothing
- Given: no roadmap-pulse cache file exists yet for the current project
- When: the beads-bar row renders
- Then: the row is empty — no error, no placeholder text

### Requirement: The tmux status bar is three lines
`home/dot_config/tmux/tmux.conf.tmpl` SHALL set `status 3`, and each shipped theme
(`vercel-theme.conf`, `one-hunter-vercel-theme.conf`, `tokyo-night-abyss-theme.conf`,
`nord-theme.conf`) SHALL define both `status-format[1]` and `status-format[2]`. A theme MUST NOT
enable the three-line bar without defining both extra rows.

#### Scenario: all four themes define both extra rows
- Given: `status 3` is enabled in `tmux.conf.tmpl`
- When: any of the four shipped themes is active
- Then: both `status-format[1]` and `status-format[2]` render the session/usage and
  beads/proposals rows respectively — none falls back to tmux's default pane-list row
