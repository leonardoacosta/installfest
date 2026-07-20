---
order: 0719a
---

# Proposal: Render a 5H reset countdown on cc-tmux row 2 when near the session limit

## Change ID
`cc-tmux-usage-reset-countdown`

## Summary
During a session-limit cooldown ("Session limit reached"), row 2's `5H:94%` answers the wrong
question — the operator wants "when can I resume", and that answer (`usage5hResetAt`) is already
in the nexus-agent `/credentials` payload and already extracted by `usage._extract_reset_at`
(the accounts popup renders it). This proposal surfaces it on row 2 as a compact countdown
appended to the 5H segment (e.g. `5H:94%·47m`), rendered ONLY when the 5-hour utilization is at
or above 80% (the same threshold nx's poller uses for its hot interval) — below that the row is
byte-identical to today.

Observed 2026-07-19: statusline stuck at `5H:94%` during cooldown while CC itself displayed
"Retrying in 11m (9:50pm)" — the reset time (02:50:00Z) was sitting unrendered in the payload
the row was already fetching.

## Context
- Extends: `apps/cc-tmux/src/cc_tmux/usage.py` (`active_usage` cache triple -> quad),
  `render.py` row-2 usage tail (line ~710), `testing.py` self-test
- Related: nx proposal `poll-cooldown-credentials` (companion data-side fix in `~/dev/nx` —
  keeps the 5H value itself fresh during cooldown; cross-repo, not expressible via this repo's
  `- depends on:`). Without it the countdown still works — `usage5hResetAt` is stable once
  polled — so neither proposal blocks the other.
- depends on: (none — `add-cmux-sidebar-widgets` touches cmux sidebar files, not cc-tmux row
  rendering)
- touches: `apps/cc-tmux/src/cc_tmux/usage.py`, `apps/cc-tmux/src/cc_tmux/render.py`, `apps/cc-tmux/src/cc_tmux/testing.py`

> **Two parser-visible contracts.** `/triage` reads `- depends on:`; `wave-plan-build` reads
> `- touches:`.

## Non-Goals
- Changing poll/refresh cadence (nx-side; see `poll-cooldown-credentials`).
- A 7D countdown (7-day resets are days away — a minutes-granularity segment is noise there).
- Rendering the countdown below 80% utilization (row-width budget; the info is in the popup).

## Done Means
- When the active credential's 5H utilization is >= 80% and a future `usage5hResetAt` is
  present, row 2 renders the 5H segment as `5H:<pct>·<countdown>` (e.g. `5H:94%·47m`, hours
  form `1h12m` above 60 minutes), countdown in DIM.
- Below 80%, or with no/past reset timestamp, row 2 renders exactly as before (fail-open).
- The countdown stays fresh across the 45s usage-cache TTL (reset epoch is cached and the
  remaining time is computed at render, so it ticks down between fetches).

## Testing
- `cc-tmux` self-test (`testing.py`) cases for: countdown formatting (minutes / h+m / past ->
  absent), threshold gating (79% no countdown, 80% countdown), cache round-trip of the reset
  epoch, and fail-open on absent field (tasks 1.1).
- Live render check: `tmux display-message -p` of the rendered row (or direct
  `cc-tmux status-row2` invocation) with the real nexus-agent payload (tasks 1.2).
