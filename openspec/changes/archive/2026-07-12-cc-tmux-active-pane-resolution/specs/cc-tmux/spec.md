# cc-tmux Specification Delta

## MODIFIED Requirements

### Requirement: A dedicated tmux status row shows session identity and usage
The plugin SHALL render a dedicated tmux status row (`status-format[1]`) showing, left-justified,
a single-letter model tag (Fable=F, Opus=O, Haiku=H, Sonnet=S), the project code, and the git
branch. The model letter SHALL be sourced from the per-pane `session-context.<pane>.json` cache
written by nexus-statusline — not from the SessionStart hook payload, whose `model` field is
unreliable and which never re-fires on a mid-session `/model` switch. Right-justified on the same
row, the plugin SHALL render Claude usage statistics for the active nexus-agent credential: an
account label, and SES:/5H:/7D: utilization gauges (session-context %, 5-hour, and 7-day), each
coloured by utilization threshold. This row SHALL remain separate from the window-tabs row, whose
own `status-right` stays usage-free.

The window's representative pane — the pane whose project/branch/model/usage this row renders —
SHALL be the window's tmux-ACTIVE (focused) pane when that pane carries a valid `@cc-state`
(i.e. it is itself a tracked Claude pane). Only when the active pane is untracked (e.g. a plain
shell pane focused in a split alongside a background Claude pane) SHALL the plugin fall back to
the existing priority-based pick (highest-priority `@cc-state` among the window's tracked panes,
ties broken by pane order).

#### Scenario: row 2 renders the session identity and usage
- Given: a tracked Claude pane in project `if` on branch `main`, model Fable, and the active
  nexus-agent credential has usage data
- When: the session-bar row renders
- Then: the left side shows `F if > main` (model letter, project, branch) and the right side
  shows the account label plus SES:/5H:/7D: gauges

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

#### Scenario: model letter tracks a mid-session model switch
- Given: a tracked pane whose `session-context.<pane>.json` model letter changes from `F` to `O`
  after a `/model` switch
- When: the session-bar row next renders
- Then: the model letter shown is `O` (no SessionStart event required)

#### Scenario: missing session-context cache drops the letter only
- Given: a tracked pane with no `session-context.<pane>.json` file
- When: the session-bar row renders
- Then: the row renders project and branch with no model letter (fail-open, no error)

#### Scenario: unpolled usage windows render as '--'
- Given: an active nexus-agent credential that has not yet been polled for 5-hour/7-day usage
- When: the session-bar row renders
- Then: the SES:/5H:/7D: gauges render `--` in a dimmed colour rather than a stale/wrong percent

#### Scenario: untracked window shows nothing on this row
- Given: a tmux window with no tracked Claude pane
- When: the session-bar row renders for that window
- Then: the row is empty (no session identity, no usage) for that window
