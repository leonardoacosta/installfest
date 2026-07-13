## MODIFIED Requirements

### Requirement: Clicking the row-2 account label opens a read-only accounts popup
The plugin SHALL bind a click on row 2's account-label segment to `cc-tmux accounts-popup`, a
read-only floating pane (positioned immediately above the current status-bar row) listing every
tracked-but-not-currently-active Claude account with its 5-hour/7-day utilization, plus a
distinguished row for the currently active account including its live SES (session
context-window-used %). The popup renders as a static, non-selectable `display-popup` — there is
no fzf or `display-menu` dependency; it is not a picker. This is a read-only view — it MUST NOT
switch or swap the active credential. The popup SHALL NOT accept typed text input, and SHALL
render no visual affordance suggesting it can be typed into.

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

#### Scenario: popup is dismissed without accepting typed input
- Given: the accounts popup is open
- When: the user dismisses it (click on a rendered close target, or any keystroke — whichever
  mechanism the tmux version in use supports)
- Then: the popup closes, and at no point during its open state does it render a blinking cursor
  or other affordance suggesting free-form text can be typed into it
