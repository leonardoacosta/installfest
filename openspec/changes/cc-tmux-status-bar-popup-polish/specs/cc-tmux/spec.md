## MODIFIED Requirements

### Requirement: A dedicated tmux status row shows session identity and usage
The plugin SHALL render a dedicated tmux status row (`status-format[1]`) showing, left-justified,
a single-letter model tag (Fable=F, Opus=O, Haiku=H, Sonnet=S), the project code, the git branch,
and (when any of the six working-tree metrics below is nonzero) working-tree indicators.
Right-justified on the same row, the plugin SHALL render Claude usage statistics for the active
nexus-agent credential: a token-count label for SES (e.g. `252.5k:`, unchanged from the prior
`cc-tmux-context-bar` format) plus exact `5H:xx%`/`7D:xx%` text, followed LAST by a combined
Unicode Braille usage glyph (10 cells wide) encoding all three values in one glyph run — top two
dot-rows = SES, third dot-row = 5H, fourth (bottom) dot-row = 7D, each row an independent
proportional left-to-right fill. The glyph renders in a neutral/unstyled color; the exact text
values remain the sole color-coded signal (unchanged `usage.color_for`/`_context_color_pair`
thresholds). The active account's identity (email + org id) is NOT rendered on this row — see
the beads/proposals row requirement below, which now carries it. This row SHALL remain separate
from the window-tabs row.

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

**Combined usage glyph** (`render_usage_glyph`, 10 braille cells): for a metric with ratio `r`
(0..1) and a bit-order table of `k` bits per cell (SES: 4 bits/cell, rows 1-2; 5H: 2 bits/cell,
row 3; 7D: 2 bits/cell, row 4), the total dot budget is `k * 10` and `dots_lit =
round(r * budget)`, filled sequentially cell-by-cell left to right — the same segmented-fill
principle as the prior token-count bar, generalized to 3 independently-filling rows sharing one
10-cell run. A metric whose data is unavailable (see the unpolled scenario below) contributes
ZERO dots to its own row(s) only — other metrics' rows are unaffected (per-metric degrade, not an
all-or-nothing glyph blackout).

#### Scenario: row 2 renders the session identity and usage, no account identity
- Given: a tracked Claude pane in project `if` on branch `main`, model Fable, and the active
  nexus-agent credential has usage data
- When: the session-bar row renders
- Then: the left side shows `F if > main` (model letter, project, branch) and the right side
  shows `252.5k: 5H:xx% 7D:xx%` text (SES's token-count label plus 5H/7D percentages) followed
  LAST by the combined 10-cell braille glyph with each row's fill proportional to that metric's
  value — no account label or identity text appears anywhere on this row

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

#### Scenario: unpolled usage windows render as '--' and blank that metric's glyph row(s) only
- Given: an active nexus-agent credential that has not yet been polled for 5-hour/7-day usage,
  while SES has live data
- When: the session-bar row renders
- Then: the `5H:`/`7D:` text renders `--` in a dimmed colour rather than a stale/wrong percent,
  the combined glyph's row 3 (5H) and row 4 (7D) render zero dots, and the glyph's rows 1-2 (SES)
  still render SES's live fill unaffected

#### Scenario: untracked window shows nothing on this row
- Given: a tmux window with no tracked Claude pane
- When: the session-bar row renders for that window
- Then: the row is empty (no session identity, no usage) for that window

### Requirement: A dedicated tmux status row surfaces open/ready beads and proposals plus the active account identity
The plugin SHALL render a second dedicated tmux status row (`status-format[2]`) showing the
current project's cached roadmap-pulse counts, read directly from
`~/.claude/scripts/state/roadmap-pulse.<code>.line`, PLUS a third independent segment carrying
the active nexus-agent credential's identity (email + 8-character org id, e.g.
`leo@priceless.dev·bc7da511` — the same format used by the accounts popup's identity rows). The
openspec/beads portion SHALL render in the form `openspec: {open} open {unarchived} unarchived
({age}) | beads: {ready} ready {blocked} blocked ({age})`, where the beads half counts only
"standalone" beads — issues that are NOT a transitive descendant, via a `parent-child`
dependency, of any issue whose title starts with `[SPEC]` or `[CAPABILITY]` — so the two halves
are additive rather than double-counting OpenSpec-tracked work. Any line in the cache starting
with `next:` or `radar:` SHALL NOT be rendered on this row — only the openspec/beads counts
render, regardless of what else the cache file contains (defense against a stale or rolled-back
cache carrying either token). Each half's numeric values SHALL be coloured by semantic threshold
(DIM for a healthy zero/low count, YELLOW when `unarchived > 0` or `standalone_blocked > 0`, RED
above a documented high-count threshold). No new data production mechanism SHALL be introduced
for the openspec/beads portion — it reads the cache nexus-statusline's own `getRoadmapPulse()`
already maintains, extended upstream to carry the beads fields.

**Account identity segment**: the plugin SHALL append the active credential's identity as a
third segment, independent of the openspec/beads pair — present whenever an active nexus-agent
credential resolves, regardless of whether the openspec/beads cache exists at all. The segment
SHALL be clickable, bound to `cc-tmux accounts-popup` (the same read-only popup behavior
previously bound to row 2 — see the popup requirement below), via the same
`#[range=user|accounts]` mouse-range marker mechanism.

#### Scenario: row 3 renders both halves with independent staleness ages, plus the account identity
- Given: a cached roadmap-pulse file whose counts are `2 open, 1 unarchived` (openspec) and `3
  ready, 0 blocked` (standalone beads), and an active nexus-agent credential
  `leo@priceless.dev` / org `bc7da511-...`
- When: the beads-bar row renders
- Then: it shows `openspec: 2 open 1 unarchived (<age>) | beads: 3 ready 0 blocked (<age>) |
  leo@priceless.dev·bc7da511` with `1 unarchived` coloured YELLOW, the rest DIM/CYAN, and the
  account segment clickable via the same mouse-range marker row 2 used to carry

#### Scenario: no roadmap-pulse cache, but an active account resolves
- Given: no roadmap-pulse cache file exists yet for the current project, and an active
  nexus-agent credential resolves
- When: the beads-bar row renders
- Then: the row shows ONLY the account identity segment (`leo@priceless.dev·bc7da511`) — not an
  empty row, since the account segment is independent of the openspec/beads cache

#### Scenario: openspec/beads cache present, no active account resolves
- Given: a cached roadmap-pulse file with real counts, and nexus-agent is unreachable (no active
  credential resolves)
- When: the beads-bar row renders
- Then: the row shows only the openspec/beads segments, unchanged from the prior requirement
  version — no empty account segment, no error

#### Scenario: a stray next: or radar: line never renders
- Given: a cached roadmap-pulse file containing a `next: …` line, a `radar:stale` line (stale
  pre-fix content), and a counts line
- When: the beads-bar row renders
- Then: only the openspec/beads counts (and, if applicable, the account segment) render —
  neither the `next:` nor the `radar:` line appears anywhere on the row

#### Scenario: standalone beads exclude OpenSpec-tracked work
- Given: 5 open beads total, 3 of which are tasks under a `[SPEC] some-proposal` feature (itself
  under a `[CAPABILITY]` epic), and 2 of which have no epic ancestor at all
- When: the standalone-beads count is computed
- Then: only the 2 unparented beads count toward `beads: {ready}/{blocked}` — the 3
  OpenSpec-tracked tasks do not, since they're already represented by the openspec half

#### Scenario: counts-only cache renders as-is
- Given: a cached roadmap-pulse file containing only the openspec/beads counts (no `next:` or
  `radar:` line), and no active account resolves
- When: the beads-bar row renders
- Then: it shows both openspec/beads halves, unchanged

#### Scenario: nothing available renders nothing
- Given: no roadmap-pulse cache file exists yet for the current project, and no active
  nexus-agent credential resolves
- When: the beads-bar row renders
- Then: the row is empty — no error, no placeholder text (unchanged from the prior requirement
  version's "no cache yet" contract, now also gated on the account segment's own absence)

### Requirement: Clicking the account-identity segment opens a read-only accounts popup
The plugin SHALL bind a click on the account-identity segment (row 3, per the beads/proposals
row requirement above) to `cc-tmux accounts-popup`, a read-only floating pane (positioned
immediately above the current status-bar row) listing every tracked Claude account's 5-hour/7-day
utilization as text plus a combined 2-metric braille glyph (20 cells wide: rows 1-2 = 5H, rows
3-4 = 7D, each metric using the full 4-dot-per-cell budget) — uniformly for every account,
active or not. The popup SHALL NOT render any session-context-window (SES) data anywhere: SES is
a property of the currently-focused pane's session, not of an account, and does not belong in an
account-usage view. The active account is distinguished ONLY by a leading `*` marker — no
separate glyph shape, token-count label, or other session-scoped field marks it. When fzf and
tmux >= 3.2 are available (the same `supports_popup` gate `cc-tmux inbox`/`picker-data` already
use), the popup pipes through fzf with `--no-input` (query box hidden/disabled — genuinely
cannot be typed into, not merely dismissed on the first keystroke) and a `[x]`-labeled header
bound via `--bind 'click-header:abort'` (a real clickable close target — tmux's own
`display-popup` has no native mouse-click dismissal). Row clicks and Enter are inert
(`--bind 'left-click:ignore'`/`'enter:ignore'`) — this is a read-only view, it MUST NOT switch or
swap the active credential. Without fzf/tmux 3.2+, the popup falls back to a static
`display-popup` dismissed by any keystroke. The fzf-backed popup SHALL render its full account
list within the popup pane's actual available height — it MUST NOT truncate the list to less
than the pane's true height when the account count would otherwise fit.

#### Scenario: popup lists every tracked account uniformly, no SES anywhere
- Given: 3 tracked nexus-agent credentials, one active, and the click lands on the row-3 account
  identity segment
- When: the accounts popup opens
- Then: all 3 accounts (active and non-active alike) each show `<label> 5H:xx% 7D:xx%` (no SES
  field anywhere) plus a 20-cell 2-metric braille glyph (rows 1-2 = 5H, rows 3-4 = 7D); the
  active account's row is distinguished only by its leading `*` marker

#### Scenario: duplicate and orphaned credential rows collapse or drop before display
- Given: nexus-agent's `/credentials` payload contains multiple historical rows for the same
  `(accountEmail, orgUuid)` pair (per if-lp8v/if-m5q6), and/or orphaned rows with no
  `accountEmail` and `status: refresh_failed`
- When: the accounts popup resolves its account list
- Then: exactly one row appears per distinct `(accountEmail, orgUuid)` pair using its
  most-recently-seen usage data, and orphaned no-email/`refresh_failed` rows are dropped
  entirely rather than rendered as fake accounts

#### Scenario: popup positions above the current row
- Given: the accounts popup opens
- When: it renders
- Then: it appears as a floating pane positioned immediately above the current status-bar row,
  not overlapping it

#### Scenario: the popup fills its actual available height, no truncation
- Given: the accounts popup opens with 3 deduped accounts (5 rendered lines each: summary,
  identity, two reset lines, separator)
- When: the popup renders
- Then: every account's full block is visible with no row cut off, using the popup pane's actual
  available height rather than a fixed fraction that leaves real content off-screen

#### Scenario: unreachable nexus-agent shows nothing
- Given: nexus-agent is unreachable
- When: the account identity segment is clicked
- Then: the popup shows no accounts (fail-open, no error) — same degradation convention as every
  other nexus-agent-dependent segment in this plugin

#### Scenario: popup is dismissed via a real click target when fzf is available
- Given: fzf and tmux >= 3.2 are available, and the accounts popup is open
- When: the user clicks the `[x]` header or presses `q`
- Then: the popup closes (`click-header:abort` / `q:abort`), and at no point does the popup
  accept typed query input (`--no-input`) or act on a row click/Enter

#### Scenario: popup falls back to any-keystroke dismiss without fzf
- Given: fzf is unavailable or tmux is below 3.2
- When: the accounts popup opens
- Then: it renders as a static `display-popup`, dismissed by any single keystroke (no click
  target in this fallback)
