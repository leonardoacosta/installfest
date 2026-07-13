## MODIFIED Requirements

### Requirement: A minimal per-render session-context field bridges the one field only nexus-statusline can see
`cc-tmux` SHALL query nx-agent's session-context HTTP endpoint (`GET
http://localhost:7400/sessions/:id/context`) to obtain the current session's context-window
used-percentage, keyed by a `session_id` captured from the Claude Code hook payload on
`SessionStart` and stored as a new `@cc-session-id` pane option (same capture block as the
existing `session_title` read in `cmd_register`). This replaces the retired per-pane
`session-context.<pane_id>.json` file `nexus-statusline` used to write — that file is no longer
written by nx as of the 2026-07-13 nx API migration. The query SHALL be cached on disk with a
short TTL (~5s) so the 1Hz session-bar render tick does not re-fetch on every tick; a cache miss
or expiry SHALL fall through to a live fetch with a 1s timeout, fail-open to absent on any error
(unreachable agent, non-2xx, malformed body, missing/stale entry).

This requirement is scoped to exactly this one field (`context_used_pct`). The model family
letter is NOT covered — see the sibling "A dedicated tmux status row shows session identity and
usage" requirement for its (unchanged, now-degrading) sourcing.

#### Scenario: session_id captured on SessionStart
- Given: a `SessionStart` hook payload carrying `session_id: "abc123"`
- When: `cc-tmux register --state idle` runs (the SessionStart hook entrypoint)
- Then: the pane's `@cc-session-id` option is set to `abc123`

#### Scenario: context percentage sourced from the nx endpoint
- Given: a tracked pane with `@cc-session-id` set, and nx-agent's `/sessions/abc123/context`
  returns `{"usedPercentage": 42, ...}`
- When: the session-bar row renders for that pane's window
- Then: the SES gauge reflects 42%, sourced from the endpoint response (not any on-disk file)

#### Scenario: unreachable nx-agent degrades to absent, not an error
- Given: a tracked pane with `@cc-session-id` set, and nx-agent is not running (connection
  refused)
- When: the session-bar row renders for that pane's window
- Then: the SES gauge renders `--` (fail open), and no exception propagates

#### Scenario: no session_id captured yet
- Given: a tracked pane whose `@cc-session-id` option has never been set (e.g. a pane registered
  before this feature shipped, or a hook fired before the first `SessionStart`)
- When: the session-bar row renders for that pane's window
- Then: the SES gauge renders `--` (fail open), identical to the unreachable-agent case

### Requirement: A dedicated tmux status row shows session identity and usage
The plugin SHALL render a dedicated tmux status row (`status-format[1]`) showing, left-justified,
a single-letter model tag (Fable=F, Opus=O, Haiku=H, Sonnet=S), the project code, the git branch,
and (when dirty or ahead) working-tree indicators. Right-justified on the same row, the plugin
SHALL render Claude usage statistics for the active nexus-agent credential: an account label, and
SES:/5H:/7D: utilization gauges. This row SHALL remain separate from the window-tabs row.

**Model letter** (unchanged sourcing, disclosed degradation): the model letter SHALL continue to
be sourced from the legacy per-pane `session-context.<pane>.json` cache exactly as before this
proposal — no new mechanism is introduced for it (see this capability's proposal `## Why` item 3
for the rationale: no source, local or nx, exists for this field today). Once nx stops writing
that legacy file, the model letter SHALL degrade to absent via the existing freshness-cutoff
fail-open path — this is expected, disclosed behavior, not a bug this requirement is claiming to
fix.

**Branch and dirty** (dual source): the plugin SHALL prefer nx's git-status data
(`nx_agent.project_git_status`, keyed by the pane's resolved registry project code) for `branch`
and `dirty` when nx-agent returns a `git` object for that project. When nx-agent is unreachable,
returns a 404 for the project code, or has not yet observed that project's git state, the plugin
SHALL fall back to the pane's local `@cc-branch`/`@cc-dirty` options (resolved via
`tmux.set_pane_git_identity` on `waiting`/`idle` transitions). `dirty` SHALL render as
`*<modified>+<untracked>` when the total of modified + untracked is nonzero (sourced from nx's
`{modified, untracked}` counts when nx is the source, or the local git-status-porcelain
equivalent when the fallback is the source), and SHALL render nothing when the total is zero.

**Ahead** (local-only, no nx source exists): `ahead` SHALL be sourced exclusively from the pane's
local `@cc-ahead` option (a `git rev-list --count @{upstream}..HEAD`-equivalent resolved on the
same `waiting`/`idle` cadence as branch/dirty) — nx's git-status payload carries no
ahead/behind-vs-upstream field as of this proposal, so there is no nx value to prefer. `ahead`
SHALL render as `^N` when N > 0, and nothing otherwise, unchanged from prior behavior.

The window's representative pane resolution (tmux-active pane first, priority-pick fallback) is
unchanged by this proposal.

#### Scenario: row 2 renders the session identity and usage
- Given: a tracked Claude pane in project `if` on branch `main`, model Fable, and the active
  nexus-agent credential has usage data
- When: the session-bar row renders
- Then: the left side shows `F if > main` (model letter, project, branch) and the right side
  shows the account label plus SES:/5H:/7D: gauges

#### Scenario: branch and dirty prefer nx's git-status data
- Given: a tracked pane in project `if`, and `GET /projects/if/status` returns a `git` object
  with `branch: "feature-x"`, `dirty: {modified: 3, untracked: 2}`
- When: the session-bar row renders
- Then: the branch shown is `feature-x` and the dirty indicator renders `*3+2`, sourced from nx
  (not the local `@cc-branch`/`@cc-dirty` pane options)

#### Scenario: branch and dirty fall back to local git when nx is unreachable
- Given: a tracked pane in project `if` with local `@cc-branch` = `main` and `@cc-dirty` =
  `[1, 0]`, and nx-agent's `/projects/if/status` request fails (connection refused)
- When: the session-bar row renders
- Then: the branch shown is `main` and the dirty indicator renders `*1+0`, sourced from the local
  pane options (fail-open fallback, not a blank row)

#### Scenario: unknown project code at nx falls back to local git
- Given: a tracked pane whose registry project code is not present in nx's own project registry
  (registry drift between `home/projects.toml` and nx's `~/.claude/scripts/config/projects.json`)
  and `GET /projects/<code>/status` returns 404
- When: the session-bar row renders
- Then: the branch/dirty indicators fall back to the local `@cc-branch`/`@cc-dirty` pane options,
  identical to the unreachable-agent case — a registry mismatch never produces a blank branch when
  a local value is available

#### Scenario: ahead is always local, never queried from nx
- Given: a tracked pane 2 commits ahead of its upstream, with `@cc-ahead` = `2`
- When: the session-bar row renders
- Then: the ahead indicator renders `^2`, sourced from the local pane option regardless of
  whether nx-agent is reachable

#### Scenario: model letter degrades to blank once nx stops writing the legacy file
- Given: a tracked pane whose legacy `session-context.<pane>.json` file is absent or older than
  the existing freshness cutoff (nx no longer writes it)
- When: the session-bar row renders
- Then: the row renders project/branch/dirty/ahead/usage as normal with no model letter (fail
  open, no error) — this is expected behavior, not a regression this proposal introduces

#### Scenario: the active pane is used, not the priority-first pane
- Given: a window with two tracked Claude panes, pane A (`idle`, lower pane index) and pane B
  (`idle`, higher pane index, currently focused)
- When: the session-bar row renders
- Then: the left/right side reflects pane B's project/branch/model/usage, not pane A's

#### Scenario: an untracked focused pane falls back to the priority pick
- Given: a window with a focused plain-shell pane (no `@cc-state`) and a background tracked
  Claude pane in `waiting`
- When: the session-bar row renders
- Then: the row reflects the `waiting` Claude pane (fallback to the existing priority-based
  pick), not an empty row

#### Scenario: unpolled usage windows render as '--'
- Given: an active nexus-agent credential that has not yet been polled for 5-hour/7-day usage
- When: the session-bar row renders
- Then: the SES:/5H:/7D: gauges render `--` in a dimmed colour rather than a stale/wrong percent

#### Scenario: untracked window shows nothing on this row
- Given: a tmux window with no tracked Claude pane
- When: the session-bar row renders for that window
- Then: the row is empty (no session identity, no usage) for that window
