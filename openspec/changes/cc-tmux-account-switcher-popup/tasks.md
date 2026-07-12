<!-- beads:feature:if-4l6r -->

<!-- beads:epic:if-bqw -->

# Tasks: cc-tmux-account-switcher-popup

> Literal `## DB/API/UI/E2E Batch` headers per `/feature`'s wave-plan-build contract — mapped by
> domain fit (DB = the mechanism spike + dedupe helper; API = subcommand/render wiring; UI = tmux
> config/click binding; E2E = tests + live verification). Owner: general-purpose engineer agents.

## DB Batch

- [ ] [1.1] [P-1] **Spike, must land first.** Confirm on the deployed tmux version (3.6a) that
  (a) a custom `#[range=user|<name>]`-marked status-bar segment can be bound to a NON-default
  `MouseDown1Status` action (not tmux's built-in `switch-client -t =`), and (b) `tmux
  display-popup` can be positioned immediately above a specific status-bar row. Paste both
  confirmations (or the working alternative mechanism, if either assumption is wrong — same
  class of correction `cc-tmux-tabs-and-rename-fix` already had to make once on this exact
  plugin). Do not proceed past this task until both are confirmed. [owner:general-purpose] [type:api] [beads:if-96rq]
- [ ] [1.2] [P-1] `apps/cc-tmux/src/cc_tmux/usage.py`: add a dedupe helper over the raw
  `/credentials` payload — group by `(accountEmail, orgUuid)`, keep the most-recently-seen entry
  per group. Pure function, no new HTTP call. [owner:general-purpose] [type:api] [beads:if-y0u4]

## API Batch

- [ ] [2.1] [P-1] `apps/cc-tmux/src/cc_tmux/render.py`: add `render_accounts_popup(accounts,
  active_label, active_ses_pct)` — pure, aligned-columns composition mirroring `inbox_rows`'s
  approach: active account row shows `SES:`/`5H:`/`7D:`, every other account row shows `5H:`/
  `7D:` only. [owner:general-purpose] [type:api] [beads:if-2ih5]
- [ ] [2.2] [P-1] `apps/cc-tmux/src/cc_tmux/cli.py` + `parser.py`: new `cc-tmux accounts-popup`
  subcommand — fetches `/credentials`, applies the task-1.2 dedupe, resolves the active account's
  live SES via the same path row 2 uses (`_read_session_context`/`_active_usage`), calls
  `render_accounts_popup`, and opens it via the same fzf-popup-with-`display-menu`-fallback
  mechanism `cmd_inbox` already uses (reuse, do not reimplement). [owner:general-purpose] [type:api] [beads:if-cvsn]

## UI Batch

- [ ] [3.1] [P-1] Using the mechanism confirmed in task 1.1: mark row 2's account-label segment
  with the appropriate range marker in `render_session_bar`'s output, and add the
  `MouseDown1Status` binding (in `apps/cc-tmux/cc-tmux.tmux` or the relevant theme `.conf`,
  whichever this plugin's existing range-click bindings live in) that runs
  `cc-tmux accounts-popup` when the click lands in that range, falling through to the default
  window-switch behavior everywhere else on the row. [owner:general-purpose] [type:config] [beads:if-3v54]

## E2E Batch

- [ ] [4.1] [P-1] Extend `apps/cc-tmux/src/cc_tmux/testing.py`: self-test cases for the
  credentials-dedupe helper (duplicate `(accountEmail, orgUuid)` rows collapse to one,
  most-recent kept) and `render_accounts_popup` (active row has SES, others don't; unreachable
  nexus-agent -> empty, fail-open). Run `cc-tmux self-test` and paste the passing stdout.
  [owner:general-purpose] [type:testing] [beads:if-zoqu]
- [ ] [4.2] [P-1] Live verification: click the account label on a real pane with 2+ tracked
  accounts, confirm the popup opens positioned above the current row with real other-account
  5H/7D data and the active account's SES — paste observed output. [owner:general-purpose] [type:testing] [beads:if-bnbr]
- [ ] [4.3] [P-2] Live verification: with `fzf` temporarily unavailable (or simulated absent),
  confirm the `display-menu` fallback still lists accounts — paste observed output.
  [owner:general-purpose] [type:testing] [beads:if-jnke]
