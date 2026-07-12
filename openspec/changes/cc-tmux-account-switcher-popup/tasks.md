<!-- beads:feature:if-4l6r -->

<!-- beads:epic:if-bqw -->

# Tasks: cc-tmux-account-switcher-popup

> Literal `## DB/API/UI/E2E Batch` headers per `/feature`'s wave-plan-build contract — mapped by
> domain fit (DB = the mechanism spike + dedupe helper; API = subcommand/render wiring; UI = tmux
> config/click binding; E2E = tests + live verification). Owner: general-purpose engineer agents.

## DB Batch

- [x] [1.1] [P-1] **Spike, must land first.** Confirm on the deployed tmux version (3.6a) that
  (a) a custom `#[range=user|<name>]`-marked status-bar segment can be bound to a NON-default
  `MouseDown1Status` action (not tmux's built-in `switch-client -t =`), and (b) `tmux
  display-popup` can be positioned immediately above a specific status-bar row. Paste both
  confirmations (or the working alternative mechanism, if either assumption is wrong — same
  class of correction `cc-tmux-tabs-and-rename-fix` already had to make once on this exact
  plugin). Do not proceed past this task until both are confirmed. [owner:general-purpose] [type:api] [beads:if-96rq]
- [x] [1.2] [P-1] `apps/cc-tmux/src/cc_tmux/usage.py`: add a dedupe helper over the raw
  `/credentials` payload — group by `(accountEmail, orgUuid)`, keep the most-recently-seen entry
  per group. Pure function, no new HTTP call. [owner:general-purpose] [type:api] [beads:if-y0u4]

## API Batch

- [x] [2.1] [P-1] `apps/cc-tmux/src/cc_tmux/render.py`: add `render_accounts_popup(accounts,
  active_label, active_ses_pct)` — pure, aligned-columns composition mirroring `inbox_rows`'s
  approach: active account row shows `SES:`/`5H:`/`7D:`, every other account row shows `5H:`/
  `7D:` only. [owner:general-purpose] [type:api] [beads:if-2ih5]
- [x] [2.2] [P-1] `apps/cc-tmux/src/cc_tmux/cli.py` + `parser.py`: new `cc-tmux accounts-popup`
  subcommand — fetches `/credentials`, applies the task-1.2 dedupe, resolves the active account's
  live SES via the same path row 2 uses (`_read_session_context`/`_active_usage`), calls
  `render_accounts_popup`, and opens it via the same fzf-popup-with-`display-menu`-fallback
  mechanism `cmd_inbox` already uses (reuse, do not reimplement). [owner:general-purpose] [type:api] [beads:if-cvsn]

## UI Batch

- [x] [3.1] [P-1] Using the mechanism confirmed in task 1.1: mark row 2's account-label segment
  with the appropriate range marker in `render_session_bar`'s output, and add the
  `MouseDown1Status` binding (in `apps/cc-tmux/cc-tmux.tmux` or the relevant theme `.conf`,
  whichever this plugin's existing range-click bindings live in) that runs
  `cc-tmux accounts-popup` when the click lands in that range, falling through to the default
  window-switch behavior everywhere else on the row. [owner:general-purpose] [type:config] [beads:if-3v54]

## E2E Batch

- [x] [4.1] [P-1] Extend `apps/cc-tmux/src/cc_tmux/testing.py`: self-test cases for the
  credentials-dedupe helper (duplicate `(accountEmail, orgUuid)` rows collapse to one,
  most-recent kept) and `render_accounts_popup` (active row has SES, others don't; unreachable
  nexus-agent -> empty, fail-open). Run `cc-tmux self-test` and paste the passing stdout.
  [owner:general-purpose] [type:testing] [beads:if-zoqu]
- [ ] [4.2] [deferred] [P-1] Live verification: click the account label on a real pane with 2+
  tracked accounts, confirm the popup opens positioned above the current row with real
  other-account 5H/7D data and the active account's SES — paste observed output.
  [owner:general-purpose] [type:testing] [beads:if-bnbr]
  DEFERRED (2026-07-12, /apply orchestrator): a physical mouse click cannot be performed headlessly.
  What WAS verified live this run: (1) the `MouseDown1Status` binding installs correctly in the
  `-T root` key table — caught and fixed a real bug where the engineer's `bind-key` call omitted
  `-T root` and silently landed in the `prefix` table instead, where a mouse click would never have
  reached it (confirmed via `tmux list-keys -T root` / `-T prefix` before and after the fix);
  (2) `cc-tmux accounts-popup` produces correct real content against the live nexus-agent (23
  deduped accounts, active account correctly marked with SES); (3) `-y S -x M` is the tmux(1)
  -documented syntax for above-status-line positioning. What remains genuinely unverified: the
  actual on-screen click-and-see experience. Needs a real click from Leo after deploy.
- [ ] [4.3] [deferred] [P-2] Live verification: with `fzf` temporarily unavailable (or simulated
  absent), confirm the `display-menu` fallback still lists accounts — paste observed output.
  [owner:general-purpose] [type:testing] [beads:if-jnke]
  DEFERRED (2026-07-12, /apply orchestrator): likely MOOT, not just untested — the shipped design
  (task 2.2's resolution, documented in that task's commit) is a static, non-selectable
  `display-popup -E "$CMD accounts-popup"` with NO fzf/display-menu dependency at all (the
  proposal's Non-Goals rule out any per-account switch action, so there's nothing to select from
  an fzf list). This task's premise — an fzf-vs-display-menu fallback — may not apply to what
  actually shipped. Flagging for Leo to confirm/close rather than silently dropping it.
