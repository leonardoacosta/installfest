---
status: draft
---

# Proposal: cc-tmux-accounts-popup-click-dismiss

## Why

User ask (`/openspec:explore`, 2026-07-13, screenshot of the live accounts popup): the popup
should have a clickable `[x]` in the top-right to dismiss, and should not visually read as
something you can type into.

**Current state** (`apps/cc-tmux/cc-tmux.tmux:120-121`):
```
tmux bind-key -T root MouseDown1Status if-shell -F '#{==:#{mouse_status_range},accounts}' \
  "display-popup -y S -x M -E \"$CMD accounts-popup; read -n 1 -s\""
```
Dismissal is any single keystroke (`read -n 1 -s`). There is no rendered `[x]`, no click target,
and the idle `read` leaves a blinking cursor block that cosmetically reads as an open text-input
field — even though it already only ever consumes exactly one keystroke, never a typed string.

**Mechanism risk, not assumption** — same class this exact plugin already had to correct once
(`cc-tmux-tabs-and-rename-fix`, a `window-status-format` embedded-job assumption that turned out
wrong on the deployed tmux 3.6a) and once more in this popup's own prior proposal (task 1.1,
which proved a status-bar range click can trigger a non-default `MouseDown1Status` action to
**open** the popup). Neither of those confirmed that a mouse click can be bound to a specific
on-screen position **inside an already-open `display-popup`'s own pane** to trigger a close
action — `display-popup` panes have their own addressing/mouse semantics, unverified on this
machine. Task 1.1 here is a spike to confirm this empirically before the rest of the design
commits. If unprovable, the documented fallback is: keep keypress-to-close, fix the cursor
cosmetics, and render a static (non-clickable) `[x]` as a visual hint only — not a silent
scope-shrink, an explicit degradation path.

**Spec drift found while researching this** (contradiction guard, `/openspec:explore`): the
parent capability spec's existing requirement ("Clicking the row-2 account label opens a
read-only accounts popup", `openspec/specs/cc-tmux/spec.md`) states the popup "SHALL use the
same fzf-with-`display-menu`-fallback mechanism `cc-tmux inbox` already uses." That is not what
shipped — the actual implementation is a static, non-selectable `display-popup -E` with no
fzf/display-menu dependency at all (confirmed in the archived spec's own task 4.3 deferred note).
This proposal corrects that stale requirement text as part of the same MODIFIED delta, since
it's the exact requirement block being touched anyway.

## What Changes

- **`apps/cc-tmux/cc-tmux.tmux`**: revise the `MouseDown1Status` popup invocation per the task
  1.1 spike's confirmed mechanism — either a real in-popup click-to-close binding, or (fallback)
  keep `read -n 1 -s` but wrap it with `tput civis`/`tput cnorm` so the popup doesn't render a
  misleading blinking cursor.
- **`apps/cc-tmux/src/cc_tmux/render.py`**: `render_accounts_popup` gains a header/footer line
  rendering either a real `[x]` (if the spike confirms a click target) or a static
  "press any key to close" hint (fallback) — pure function change, no new data dependencies.
- **`openspec/specs/cc-tmux/spec.md`**: MODIFIED requirement — correct the stale
  fzf/display-menu claim to match the shipped static-popup mechanism, and add the dismiss-UX
  scenarios below.

## Non-Goals

- No account-switching/swap action. Confirmed via live `curl -X POST localhost:7400/credentials/
  swap` this session: **404, endpoint does not exist.** This was already a Non-Goal in the
  archived popup proposal and stays one here — clicking an account row does nothing.
- No fix to nexus-agent's own credential accumulation (`if-lp8v`/`if-m5q6`, both open, unaffected
  by this proposal).
- No change to `cmd_accounts_popup`'s SES-sourcing gap (`if-hrbd`, separate, already tracked).
- Does not touch the credentials dedupe logic — the orphaned-junk-row symptom visible in the
  originating screenshot was already fixed ad-hoc this session
  (`apps/cc-tmux/src/cc_tmux/usage.py`, commit `e6f89f6`, prior to this proposal existing).

## Context

- touches: `apps/cc-tmux/cc-tmux.tmux`, `apps/cc-tmux/src/cc_tmux/render.py`,
  `apps/cc-tmux/src/cc_tmux/cli.py`, `apps/cc-tmux/src/cc_tmux/testing.py`,
  `openspec/specs/cc-tmux/spec.md`
- **Shared-touches note (not a hard conflict):** the in-progress
  `cc-tmux-adopt-nx-context-and-git-status` spec (`if-d3i9`, 0/12 tasks done) also touches
  `cli.py`/`render.py`/`testing.py`/`openspec/specs/cc-tmux/spec.md` — different functions
  (session-context/nx migration vs. this popup's dismiss UX), no logical collision, but
  `wave-plan-build`'s conflict matrix should serialize the two into different waves given the
  file overlap.
- Related: extends the `[CAPABILITY] cc-tmux` epic (`if-bqw`). Builds on the archived
  `cc-tmux-account-switcher-popup` proposal and its still-open live-verify tasks (`if-bnbr`,
  `if-jnke`) — this proposal's own task 4 supersedes `if-jnke`'s fzf-fallback premise (see that
  task's own deferred note: likely moot since no fzf dependency exists in what shipped).
- Origin: `/openspec:explore` session, 2026-07-13. User accepted scaffolding this as the
  fan-out's #2 item; item #1 (junk-credential dedupe) shipped ad-hoc in the same session
  (commit `e6f89f6`); item #4 (click-to-swap) was parked as blocked on a missing nx endpoint.

## Testing

| Seam | Coverage |
| --- | --- |
| In-popup click-target mechanism spike | Manual: confirm (or rule out) a mouse binding on a specific position inside an open `display-popup` pane, on the deployed tmux version — paste the confirmation or the ruled-out result before task 1.2+ proceed — task 1.1 |
| `render_accounts_popup` dismiss-hint line (pure) | `cc-tmux self-test` case: header/footer renders the confirmed mechanism's hint (either `[x]` marker text or the static keypress hint) — task 2.1 |
| Cursor suppression (if click-target unprovable) | Manual: `tput civis`/`cnorm` wraps the `read`, confirm no visible cursor block during the popup's idle wait — task 1.2 (fallback branch) |
| End-to-end dismiss | Live verification: open the popup on a real pane, dismiss it via the confirmed mechanism (click or keypress), confirm no residual cursor artifact — task 4.1 |
