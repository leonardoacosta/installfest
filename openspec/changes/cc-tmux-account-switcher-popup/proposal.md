---
status: draft
---

# Proposal: cc-tmux-account-switcher-popup

## Why

User ask (`/openspec:explore`, 2026-07-12): clicking the account-label segment on row 2 should
open a floating pane, positioned just above the current row, listing the OTHER tracked Claude
accounts alongside their usage — so switching context between accounts doesn't require
remembering each one's numbers separately.

Two things need surfacing before this is buildable as described:

1. **Mechanism risk, not assumption.** This exact plugin already shipped one proposal
   (`2026-07-11-cc-tmux-tabs-and-rename-fix`) specifically because a tmux-mechanism assumption
   (`window-status-format`'s embedded `#()` job re-evaluating) turned out wrong on the deployed
   tmux version (3.6a) and had to be redesigned around a confirmed-working alternative. The two
   tmux mechanisms this feature needs — a clickable range bound to a NON-default action (not
   tmux's built-in `switch-client -t =`), and `display-popup` positioned "just above" a specific
   status-bar row — are BOTH unverified on this machine as of this proposal. Task 1.1 is a
   spike to confirm both empirically before the rest of the build commits to a design.
2. **SES is not an account-level metric.** Per the current `cc-tmux` spec, SES = the ACTIVE
   pane's live context-window-used % (see `cc-tmux-active-pane-resolution`'s Non-Goals) — a
   property of a running Claude session, not of a credential/account in the abstract. An
   account you are not currently in has no "SES" to show. The popup will render `5H:`/`7D:`
   (genuinely account-scoped, from nexus-agent's poller) for every OTHER tracked account, and
   reserve the SES field for the row representing the account you're CURRENTLY in (same source
   as row 2's own gauge).

## What Changes

- **`apps/cc-tmux/src/cc_tmux/usage.py`**: add a dedupe helper over the raw `/credentials`
  payload — group by `(accountEmail, orgUuid)`, keep the most-recently-seen entry per group (the
  payload is known to carry 2,709 accumulated historical rows per account, per if-lp8v/if-m5q6,
  still open). This is a self-contained client-side stopgap, NOT a substitute for the real
  nexus-agent-side prune those beads track — once that lands, this dedupe becomes redundant but
  harmless (a no-op over an already-clean payload).
- **`apps/cc-tmux/src/cc_tmux/cli.py` + `parser.py`**: new `cc-tmux accounts-popup` subcommand.
  Reuses the SAME fzf-popup-with-`display-menu`-fallback pattern `cmd_inbox` already established
  (Reader Gate: no new popup mechanism invented) — lists every deduped tracked account except the
  currently active one, each row showing `<label> 5H:xx% 7D:xx%`; the currently-active account
  renders as a distinguished top/bottom row also showing `SES:xx%` (sourced identically to row
  2's own gauge, not re-fetched).
- **`apps/cc-tmux/src/cc_tmux/render.py`**: add a pure `render_accounts_popup(accounts,
  active_label, active_ses_pct)` composition function, mirroring `inbox_rows`'s aligned-columns
  approach.
- **tmux config** (theme `.conf` files / `tmux.conf.tmpl`): give row 2's account-label segment a
  `#[range=user|account]`-style marker (mechanism confirmed by task 1.1) and a `MouseDown1Status`
  binding that runs `cc-tmux accounts-popup` when the click lands in that range, falling through
  to tmux's default window-switch behavior everywhere else on the status line.

## Non-Goals

- No account SWITCHING/swap action — this is a read-only usage popup. (The previously deferred,
  now-superseded `add-tmux-credential-status` task 4.1 was a DIFFERENT feature — an actual
  `prefix+a` swap keybinding blocked on a `POST /credentials/swap` endpoint. That endpoint
  dependency does NOT apply here; this proposal only reads `/credentials`.)
- No fix to nexus-agent's underlying 2,709-row accumulation (if-lp8v/if-m5q6, both already open
  and unaffected by this proposal) — this proposal's dedupe is a client-side view-layer stopgap
  only.
- No SES value for non-active accounts — flagged above as a genuine metric-semantics constraint,
  not an oversight.
- No change to row 2's existing account-label rendering itself (`_account_label` in `usage.py`)
  beyond making it clickable — the label text/format is unchanged.

## Context

- touches: `apps/cc-tmux/src/cc_tmux/usage.py`, `apps/cc-tmux/src/cc_tmux/cli.py`,
  `apps/cc-tmux/src/cc_tmux/parser.py`, `apps/cc-tmux/src/cc_tmux/render.py`,
  `openspec/specs/cc-tmux/spec.md`, `home/dot_config/tmux/tmux.conf.tmpl`,
  `home/dot_config/tmux/vercel-theme.conf`, `home/dot_config/tmux/one-hunter-vercel-theme.conf`,
  `home/dot_config/tmux/tokyo-night-abyss-theme.conf`, `home/dot_config/tmux/nord-theme.conf`
- Related: extends the `[CAPABILITY] cc-tmux` epic (`if-bqw`). Complementary to if-lp8v/if-m5q6
  (nexus-agent credentials payload bloat) — soft dependency, not blocking: this proposal ships a
  self-contained dedupe so it does not have to wait on those. Distinct from the superseded
  `add-tmux-credential-status` task 4.1 (an account-SWAP feature, not this read-only popup) —
  see Non-Goals.
- Origin: `/openspec:explore` session, 2026-07-12. User explicitly accepted scaffolding this
  despite the explore session's own recommendation to sequence it behind the credentials-dedup
  bead — honored via the self-contained client-side dedupe above rather than a hard wait.

## Testing

| Seam | Coverage |
| --- | --- |
| tmux mechanism spike (mouse-range click + popup positioning) | Manual: confirm a custom `#[range=user|...]`-marked segment fires a non-default `MouseDown1Status` action, and `display-popup` can be positioned immediately above a specific status-bar row, on the deployed tmux version — paste both confirmations before task 1.2+ proceed — task 1.1 |
| Credentials dedupe helper (pure) | `cc-tmux self-test` case: a payload with duplicate `(accountEmail, orgUuid)` rows collapses to one entry, most-recent kept — task 2.1 |
| `render_accounts_popup()` (pure) | `cc-tmux self-test` case: active account shows SES+5H+7D, other accounts show 5H+7D only (no SES column) — task 2.4 |
| End-to-end popup | Live verification: click the account label on a real pane, confirm the popup renders above the current row with real other-account data — paste observed output — task 3.2 |
