## MODIFIED Requirements

### Requirement: Clicking the row-2 account label opens a read-only accounts popup
The plugin SHALL bind a click on row 2's account-label segment to `cc-tmux accounts-popup`, a
read-only floating pane (positioned immediately above the current status-bar row) listing every
tracked-but-not-currently-active Claude account with its 5-hour/7-day utilization, plus a
distinguished row for the currently active account including its live SES (session
context-window-used %). When fzf and tmux >= 3.2 are available (the same `supports_popup` gate
`cc-tmux inbox`/`picker-data` already use), the popup pipes through fzf with `--no-input`
(query box hidden/disabled — genuinely cannot be typed into, not merely dismissed on the first
keystroke) and a `[x]`-labeled header bound via `--bind 'click-header:abort'` (a real clickable
close target — tmux's own `display-popup` has no native mouse-click dismissal). Row clicks and
Enter are inert (`--bind 'left-click:ignore'`/`'enter:ignore'`) — this is a read-only view, it
MUST NOT switch or swap the active credential. Without fzf/tmux 3.2+, the popup falls back to a
static `display-popup` dismissed by any keystroke.

#### Scenario: popup lists other tracked accounts with 5H/7D only
- Given: 3 tracked nexus-agent credentials, one active, and the click lands on row 2's account
  label
- When: the accounts popup opens
- Then: the 2 non-active accounts each show `<label> 5H:xx% 7D:xx%` (no SES field)

#### Scenario: the active account's row includes SES
- Given: the accounts popup is open
- When: the active account's row renders
- Then: it shows `SES:xx% 5H:xx% 7D:xx%`, with SES sourced identically to row 2's own gauge

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

#### Scenario: unreachable nexus-agent shows nothing
- Given: nexus-agent is unreachable
- When: the account label is clicked
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
