<!-- beads:epic:if-bqw -->
<!-- beads:feature:if-qwut -->

# Tasks: cc-tmux-accounts-popup-click-dismiss

> Literal `## DB/API/UI/E2E Batch` headers per `/feature`'s wave-plan-build contract. Owner:
> general-purpose engineer agents.

## DB Batch

- [ ] [1.1] [P-2] **Spike, must land first.** Confirm on the deployed tmux version (3.6a) [beads:if-50mc]
  whether a mouse click can be bound to a specific on-screen position INSIDE an already-open
  `display-popup`'s own pane (to trigger a close action), as distinct from the already-proven
  status-bar range click that OPENS the popup (the archived proposal's task 1.1). Paste the
  confirmed working mechanism, OR paste the evidence it's not achievable on this tmux version.
  Do not proceed past this task until one of the two is confirmed. [owner:general-purpose] [type:api]
- [ ] [1.2] [P-2] Based on 1.1's outcome: EITHER wire the confirmed in-popup click-to-close [beads:if-zswj]
  binding, OR (fallback) keep `read -n 1 -s` in `apps/cc-tmux/cc-tmux.tmux` but wrap it with
  `tput civis` (before) / `tput cnorm` (after, on both normal exit and interrupt) so no blinking
  cursor renders during the idle wait. [owner:general-purpose] [type:config]

## API Batch

- [ ] [2.1] [P-2] `apps/cc-tmux/src/cc_tmux/render.py`: extend `render_accounts_popup` (or add a [beads:if-cm7p]
  thin wrapper) to render a header/footer line reflecting task 1.1's outcome — either a `[x]`
  marker (real click target case) right-aligned on the first line, or a static
  "press any key to close" hint line (fallback case). Pure function, no new inputs beyond what
  the caller already resolves. [owner:general-purpose] [type:api]
- [ ] [2.2] [P-3] `openspec/specs/cc-tmux/spec.md`: MODIFIED requirement on "Clicking the row-2 [beads:if-n31i]
  account label opens a read-only accounts popup" — correct the stale "fzf-with-display-menu-
  fallback" claim (the shipped mechanism is a static, non-selectable `display-popup -E`, no fzf
  dependency) and add the dismiss-UX scenarios (click-to-close or keypress-to-close per 1.1's
  outcome; no visible cursor artifact). Spec-only edit, no code. [owner:general-purpose] [type:api]

## UI Batch

- [ ] [3.1] [P-2] `apps/cc-tmux/cc-tmux.tmux`: wire the final `MouseDown1Status` invocation per [beads:if-6rsi]
  1.1/1.2's confirmed mechanism, replacing the current
  `display-popup -y S -x M -E "$CMD accounts-popup; read -n 1 -s"` line. [owner:general-purpose] [type:config]

## E2E Batch

- [ ] [4.1] [P-2] Extend `apps/cc-tmux/src/cc_tmux/testing.py`: self-test case for [beads:if-1pcy]
  `render_accounts_popup`'s new header/footer hint line (both the click-target and fallback
  render shapes, whichever shipped). Run `cc-tmux self-test` and paste the passing stdout.
  [owner:general-purpose] [type:testing]
- [ ] [4.2] [P-3] Live verification: open the popup on a real pane, dismiss it via the shipped [beads:if-cu74]
  mechanism (click or keypress), confirm no residual blinking-cursor artifact — paste observed
  output. This supersedes `if-jnke`'s fzf-fallback premise (moot, no fzf dependency in what
  shipped — flag `if-jnke` for Leo to close as superseded once this lands).
  [owner:general-purpose] [type:testing]
