## MODIFIED Requirements

### Requirement: A dedicated tmux status row shows session identity and usage
The plugin SHALL render a dedicated tmux status row (`status-format[1]`) showing, left-justified,
a single-letter model tag (Fable=F, Opus=O, Haiku=H, Sonnet=S), the project code, the git branch,
and (when any of the six working-tree metrics below is nonzero) working-tree indicators.
Right-justified on the same row, the plugin SHALL render Claude usage statistics for the active
nexus-agent credential: an account label, and SES:/5H:/7D: utilization gauges. This row SHALL
remain separate from the window-tabs row.

**Model letter** (unchanged sourcing, disclosed degradation) and **branch** (unchanged dual
source: nx `project_git_status` primary, local `@cc-branch` fallback) are UNCHANGED by this
requirement version — see the prior MODIFIED delta (`cc-tmux-adopt-nx-context-and-git-status`)
for their full contract, still in force.

**Working-tree indicators** (per-field dual source, six metrics): the plugin SHALL render, in
this fixed left-to-right order after the branch name, each of the following ONLY when its count
is nonzero (a zero-count metric renders nothing — no glyph, no leading space beyond the single
separator to the next nonzero metric):

| Metric | Glyph | Color |
| --- | --- | --- |
| Modified | `<N>M` | GREEN |
| Untracked | `<N>U` | YELLOW |
| Deleted | `<N>D` | RED |
| Renamed | `<N>R` | BLUE |
| Ahead of upstream | `⇡<N>` | (unstyled/DIM, matching branch segment styling) |
| Behind upstream | `⇣<N>` | (unstyled/DIM, matching branch segment styling) |

For EACH of the six metrics independently: the plugin SHALL prefer the value from nx-agent's
`GET /projects/:id/status` `git` object (`nx_agent.project_git_status`) when that specific key is
present in nx's response, and SHALL fall back to the corresponding field of the local
`@cc-git-status` pane option (a JSON-encoded object with `modified`/`untracked`/`deleted`/
`renamed`/`ahead`/`behind` int fields, written by `tmux.set_pane_git_identity` via a single
`git status --porcelain=v2 --branch` parse on `waiting`/`idle` transitions) when nx's response is
absent, unreachable, or does not carry that key. As of this requirement version, nx's `git` object
carries only `modified`/`untracked` — `deleted`/`renamed`/`ahead`/`behind` SHALL always fall back
to local until nx's payload is extended (tracked externally; this requirement's per-field
resolution rule requires no future code change when that happens).

#### Scenario: row 2 renders the session identity and usage
- Given: a tracked Claude pane in project `if` on branch `main`, model Fable, and the active
  nexus-agent credential has usage data
- When: the session-bar row renders
- Then: the left side shows `F if > main` (model letter, project, branch) and the right side
  shows the account label plus SES:/5H:/7D: gauges

#### Scenario: modified and untracked prefer nx, deleted/renamed/ahead/behind fall back to local
- Given: a tracked pane in project `if`; `GET /projects/if/status` returns a `git` object with
  `dirty: {modified: 3, untracked: 1}` (no `deleted`/`renamed`/`ahead`/`behind` keys present);
  the local `@cc-git-status` option holds `{modified: 5, untracked: 9, deleted: 2, renamed: 1,
  ahead: 4, behind: 1}`
- When: the session-bar row renders
- Then: the row shows `3M 1U 2D 1R ⇡4 ⇣1` — modified/untracked from nx (3/1, not the local 5/9),
  deleted/renamed/ahead/behind from local (2/1/4/1, nx had no such keys)

#### Scenario: nx unreachable falls all six metrics back to local
- Given: a tracked pane in project `if` with local `@cc-git-status` = `{modified: 1, untracked: 0,
  deleted: 0, renamed: 0, ahead: 0, behind: 0}`, and `GET /projects/if/status` fails (connection
  refused)
- When: the session-bar row renders
- Then: the row shows `1M` (only the nonzero metric renders; all six sourced from local)

#### Scenario: a fully nx-extended response prefers nx for every field
- Given: a tracked pane where `GET /projects/if/status`'s `git` object carries all six keys
  (`modified`, `untracked`, `deleted`, `renamed`, `ahead`, `behind`, hypothetically once nx's
  payload is extended)
- When: the session-bar row renders
- Then: every one of the six metrics is sourced from nx's response, none from the local
  `@cc-git-status` fallback — proving the per-field rule requires no code change to adopt an
  expanded nx payload

#### Scenario: an all-clean, up-to-date tree shows no working-tree indicators
- Given: a tracked pane with a clean working tree, no commits ahead or behind upstream (all six
  metrics resolve to 0 regardless of source)
- When: the session-bar row renders
- Then: no working-tree indicator segment renders at all — just model/project/branch on the left

#### Scenario: registry-code mismatch at nx falls back to local, same as unreachable
- Given: a tracked pane whose registry project code is not present in nx's own project registry
  and `GET /projects/<code>/status` returns 404
- When: the session-bar row renders
- Then: all six working-tree metrics fall back to the local `@cc-git-status` pane option,
  identical to the unreachable-agent case

#### Scenario: model letter degrades to blank once nx stops writing the legacy file
- Given: a tracked pane whose legacy `session-context.<pane>.json` file is absent or older than
  the existing freshness cutoff (nx no longer writes it)
- When: the session-bar row renders
- Then: the row renders project/branch/working-tree-indicators/usage as normal with no model
  letter (fail open, no error) — unchanged from the prior requirement version

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
