# cc-tmux Specification Delta

## MODIFIED Requirements

### Requirement: A dedicated tmux status row shows session identity and usage
The plugin SHALL render a dedicated tmux status row (`status-format[1]`) showing, left-justified,
a session-count indicator, a single-letter model tag (Fable=F, Opus=O, Haiku=H, Sonnet=S), the
project code, and the git branch. The model letter SHALL be sourced from the per-pane
`session-context.<pane>.json` cache written by nexus-statusline — not from the SessionStart hook
payload, whose `model` field is unreliable and which never re-fires on a mid-session `/model`
switch. The row SHALL NOT render Claude usage statistics (account label, session-context %,
5-hour, or 7-day gauges) — those live exclusively in the in-pane Claude statusline. This row
SHALL remain separate from the window-tabs row.

#### Scenario: row 2 renders the session identity
- Given: a tracked Claude pane in project `if` on branch `main`, model Fable, and it is the only
  tracked session for that project
- When: the session-bar row renders
- Then: the left side shows `◉ F if > main` (session-count glyph, model letter, project, branch)
  and nothing renders on the right side

#### Scenario: model letter tracks a mid-session model switch
- Given: a tracked pane whose `session-context.<pane>.json` model letter changes from `F` to `O`
  after a `/model` switch
- When: the session-bar row next renders
- Then: the model letter shown is `O` (no SessionStart event required)

#### Scenario: missing session-context cache drops the letter only
- Given: a tracked pane with no `session-context.<pane>.json` file
- When: the session-bar row renders
- Then: the row renders glyph, project, and branch with no model letter (fail-open, no error)

#### Scenario: untracked window shows nothing on this row
- Given: a tmux window with no tracked Claude pane
- When: the session-bar row renders for that window
- Then: the row is empty (no session identity, no usage) for that window

### Requirement: A dedicated tmux status row surfaces open/ready beads and proposals
The plugin SHALL render a second dedicated tmux status row (`status-format[2]`) showing the
current project's cached roadmap-pulse content, read directly from
`~/.claude/scripts/state/roadmap-pulse.<code>.line`. When the cache contains multiple lines
(`next: …` plus a counts line), the plugin SHALL join them onto one row with a visible separator
— no cached line is silently dropped. No new data production mechanism SHALL be introduced for
this row — it reads the cache nexus-statusline's own `getRoadmapPulse()` already maintains.

#### Scenario: row 3 joins both cached pulse lines
- Given: a cached roadmap-pulse file containing `next: /apply foo…` and `0 open, 2 unarchived`
  on separate lines
- When: the beads-bar row renders
- Then: it shows both parts on one row (e.g. `next: /apply foo… | 0 open, 2 unarchived`)

#### Scenario: single-line cache renders as-is
- Given: a cached roadmap-pulse file containing only a counts line
- When: the beads-bar row renders
- Then: it shows that line alone with no separator artifact

#### Scenario: no cache yet renders nothing
- Given: no roadmap-pulse cache file exists yet for the current project
- When: the beads-bar row renders
- Then: the row is empty — no error, no placeholder text

### Requirement: A minimal per-render session-context field bridges the one field only nexus-statusline can see
`nexus-statusline` SHALL write the current session's context-window used-percentage AND the model
family letter (the same letter `modelEffortToken` computes for its own row) to a per-pane cache
file on every render, gated on the `TMUX_PANE` environment variable being set. This is scoped to
exactly these two fields — no other per-render field (cost, lines, clock, output style, speed,
worktree, spec) SHALL be written by this mechanism.

#### Scenario: model letter reaches cc-tmux
- Given: nexus-statusline has written a per-pane session-context cache file for the active pane
  with `model` set to `F`
- When: cc-tmux renders the session-bar row for that pane's window
- Then: the model letter `F` renders in the row's left side

#### Scenario: context percentage remains in the cache file
- Given: nexus-statusline renders with a known context-window used-percentage
- When: the per-pane session-context cache file is written
- Then: it carries both `context_used_pct` and the model letter in the same JSON object

### Requirement: Multi-account Claude usage segment replaces tmux-nexus-creds
`cc-tmux usage` SHALL render the multi-account Claude usage segment (account + 5H/7D utilization
with color thresholds) by querying nexus-agent at `http://localhost:7400/credentials`. The
subcommand SHALL remain invocable on demand but SHALL NOT be wired into any tmux status bar —
Claude usage statistics render exclusively in the in-pane Claude statusline. The standalone
`tmux-nexus-creds` script remains removed.

#### Scenario: usage segment renders from nexus-agent on demand
- Given: nexus-agent is serving credentials at `http://localhost:7400/credentials`
- When: `cc-tmux usage` is invoked manually
- Then: it shows `<account> 5H:xx% 7D:xx%` with cyan/amber/red thresholds

#### Scenario: usage segment is silent on failure
- Given: nexus-agent is unreachable
- When: the usage segment renders
- Then: it outputs nothing (no error output)

#### Scenario: no theme wires the segment into a status bar
- Given: this change is applied
- When: the theme confs under `home/dot_config/tmux/` are inspected
- Then: no `status-right` (or any status-format line) invokes `cc-tmux usage`
