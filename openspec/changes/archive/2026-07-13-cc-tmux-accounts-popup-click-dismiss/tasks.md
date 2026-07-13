<!-- beads:epic:if-bqw -->
<!-- beads:feature:if-qwut -->

# Tasks: cc-tmux-accounts-popup-click-dismiss

> Literal `## DB/API/UI/E2E Batch` headers per `/feature`'s wave-plan-build contract. Owner:
> general-purpose engineer agents.

## DB Batch

- [x] [1.1] [P-2] **Spike, must land first.** Confirm on the deployed tmux version (3.6a) [beads:if-50mc]
  whether a mouse click can be bound to a specific on-screen position INSIDE an already-open
  `display-popup`'s own pane (to trigger a close action), as distinct from the already-proven
  status-bar range click that OPENS the popup (the archived proposal's task 1.1). Paste the
  confirmed working mechanism, OR paste the evidence it's not achievable on this tmux version.
  Do not proceed past this task until one of the two is confirmed. [owner:general-purpose] [type:api]
  CONFIRMED (2026-07-13): `man tmux` — `display-popup` has NO native mouse-click dismissal;
  only Escape/C-c or `-k` (any key). No popup-internal click-target primitive exists. Real
  clickability comes from routing through fzf 0.71.0 (already installed, already used by
  `cmd_inbox`/`cmd_picker_data`) instead: `fzf --bind 'click-header:abort'` is a genuine,
  verified click-to-close bind (confirmed valid syntax by contrast with an invalid action name,
  which fzf rejects with a distinct `unknown action:` parse error — the accepted form instead
  proceeds to `inappropriate ioctl for device`, i.e. past arg-parsing, failing only on the
  headless tty). `--no-input` hides/disables the query box entirely (real "cannot be typed
  into", stronger than the original fallback idea of just hiding the cursor). Reuses the
  existing `supports_popup` fzf-gate, same pattern as the picker/inbox popups — no new mechanism
  invented.
- [x] [1.2] [P-2] Based on 1.1's outcome: EITHER wire the confirmed in-popup click-to-close [beads:if-zswj]
  binding, OR (fallback) keep `read -n 1 -s` in `apps/cc-tmux/cc-tmux.tmux` but wrap it with
  `tput civis` (before) / `tput cnorm` (after, on both normal exit and interrupt) so no blinking
  cursor renders during the idle wait. [owner:general-purpose] [type:config]
  DONE via the fzf mechanism (not the tput fallback — 1.1 confirmed a real click target exists).
  `apps/cc-tmux/cc-tmux.tmux`'s `MouseDown1Status` "accounts" branch now gates on
  `supports_popup` (same helper the picker/inbox bindings already use): fzf path pipes
  `accounts-popup` through `fzf --no-input --header='[x] ...' --bind 'click-header:abort'
  --bind 'q:abort' --bind 'enter:ignore' --bind 'left-click:ignore'`; no-fzf/pre-3.2 path keeps
  the original `read -n 1 -s` static popup.

## API Batch

- [x] [2.1] [P-2] `apps/cc-tmux/src/cc_tmux/render.py`: extend `render_accounts_popup` (or add a [beads:if-cm7p]
  thin wrapper) to render a header/footer line reflecting task 1.1's outcome — either a `[x]`
  marker (real click target case) right-aligned on the first line, or a static
  "press any key to close" hint line (fallback case). Pure function, no new inputs beyond what
  the caller already resolves. [owner:general-purpose] [type:api]
  NO CODE CHANGE NEEDED (reuse over reinvention, not a skip): fzf's own `--header` flag supplies
  the `[x]` hint text directly in the tmux-side invocation string — `render_accounts_popup`'s
  existing plain-line output already works unmodified as fzf's input list (each line just
  becomes a selectable, but inert, row). Adding a Python-side header/footer render would have
  duplicated what a single CLI flag already does.
- [x] [2.2] [P-3] `openspec/specs/cc-tmux/spec.md`: MODIFIED requirement on "Clicking the row-2 [beads:if-n31i]
  account label opens a read-only accounts popup" — correct the stale "fzf-with-display-menu-
  fallback" claim (the shipped mechanism is a static, non-selectable `display-popup -E`, no fzf
  dependency) and add the dismiss-UX scenarios (click-to-close or keypress-to-close per 1.1's
  outcome; no visible cursor artifact). Spec-only edit, no code. [owner:general-purpose] [type:api]
  DONE — requirement text updated to describe the real shipped mechanism (fzf `--no-input` +
  `click-header:abort` when available, static any-keystroke fallback otherwise); the ironic
  outcome is the fzf claim the archived spec made is now actually true, just for a different
  reason (click-to-close, not click-to-select-and-switch).

## UI Batch

- [x] [3.1] [P-2] `apps/cc-tmux/cc-tmux.tmux`: wire the final `MouseDown1Status` invocation per [beads:if-6rsi]
  1.1/1.2's confirmed mechanism, replacing the current
  `display-popup -y S -x M -E "$CMD accounts-popup; read -n 1 -s"` line. [owner:general-purpose] [type:config]
  DONE — same edit as 1.2 (both landed together since they're the same binding line).

## E2E Batch

- [x] [4.1] [P-2] Extend `apps/cc-tmux/src/cc_tmux/testing.py`: self-test case for [beads:if-1pcy]
  `render_accounts_popup`'s new header/footer hint line (both the click-target and fallback
  render shapes, whichever shipped). Run `cc-tmux self-test` and paste the passing stdout.
  [owner:general-purpose] [type:testing]
  Adjusted to match what actually shipped (no render.py change, per 2.1) — added
  `_test_accounts_popup_click_dismiss_wiring` instead: a static content-grep of
  `cc-tmux.tmux` asserting the fzf branch, `click-header:abort`, `--no-input`, and the
  `read -n 1 -s` fallback are all present. `cc-tmux self-test` output:
  `cc-tmux self-test: 74/74 passed`
- [ ] [4.2] [deferred] [P-3] Live verification: open the popup on a real pane, click the `[x]` [beads:if-cu74]
  header (and separately press `q`), confirm the popup closes and no query/cursor artifact
  appears — paste observed output. This supersedes `if-jnke`'s fzf-fallback premise (moot in a
  different way than expected: fzf IS now genuinely in the mechanism, but as a click-to-close
  UX, not a fallback picker — flag `if-jnke` for Leo to close as superseded once this lands).
  DEFERRED (2026-07-13, /apply orchestrator): a physical mouse click cannot be performed
  headlessly (same category as the original popup proposal's task 4.2). What WAS verified this
  run: (1) `man tmux` confirms no native popup click mechanism exists; (2) fzf 0.71.0 accepts
  `click-header:abort`/`--no-input`/`left-click:ignore`/`enter:ignore` as valid bind
  syntax (contrasted against a deliberately-invalid action name's distinct parse error);
  (3) `bash -n` confirms the modified `cc-tmux.tmux` has valid shell syntax; (4) the live
  deployed conf (`~/.tmux/plugins/cc-tmux/cc-tmux.tmux`) is a symlink to this repo's
  `apps/cc-tmux/cc-tmux.tmux`, so a `tmux source-file`/plugin reload after merge-back picks up
  this change with no separate deploy step. What remains genuinely unverified: the actual
  on-screen click-and-see experience. Needs a real click from Leo after merge-back + tmux
  reload. Filed as a P4 backlog task per Phase 4 Step 1 (deferred-task handling), not left as a
  silent checkbox.
