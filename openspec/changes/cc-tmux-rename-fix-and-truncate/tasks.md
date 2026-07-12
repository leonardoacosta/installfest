<!-- beads:feature:if-dse2 -->

<!-- beads:epic:if-bqw -->

# Tasks: cc-tmux-rename-fix-and-truncate

> Literal `## API/E2E Batch` headers per `/feature`'s wave-plan-build contract — mapped by domain
> fit (this repo has no DB/UI layering for a pure-Python fix with no new config surface). Owner:
> general-purpose engineer agents.

## API Batch

- [x] [1.1] [P-1] `apps/cc-tmux/src/cc_tmux/cli.py`: bump `_TAB_NAME_MAX` from `10` to `20`.
  [owner:general-purpose] [type:api] [beads:if-93wo]
- [x] [1.2] [P-1] `apps/cc-tmux/src/cc_tmux/cli.py` (`_maybe_rename_window`): capture the result
  of the `tmux._run_tmux(["rename-window", ...])` call (its existing `None`-on-failure contract)
  and return `False` when it failed, instead of unconditionally returning `True` after issuing the
  command. [owner:general-purpose] [type:api] [beads:if-px60]
- [x] [1.3] [P-1] `apps/cc-tmux/src/cc_tmux/cli.py` (`_trace_register`): add a `rename_succeeded`
  field to the trace entry, sourced from task 1.2's return value, distinct from the existing
  `rename_attempted`/`rename_fired` fields. [owner:general-purpose] [type:api] [beads:if-47ck]
- [x] [1.4] [P-2] `openspec/specs/cc-tmux/spec.md`: apply this proposal's MODIFIED Requirement
  delta (20-char truncation + worked example + failed-rename scenario) — archive-time work, noted
  here so `wave-plan-build` accounts for it. [owner:general-purpose] [type:docs] [beads:if-nc4l]

## E2E Batch

- [x] [2.1] [P-1] Extend `apps/cc-tmux/src/cc_tmux/testing.py`: self-test cases for
  `_maybe_rename_window`'s success/failure return (mocked `_run_tmux` success vs `None`),
  `_trace_register`'s new field, and `compose_title_name` at the 20-char budget (an 11-20 char
  combination now renders in full; anything over 20 still truncates). Run `cc-tmux self-test` and
  paste the passing stdout. [owner:general-purpose] [type:testing] [beads:if-n2yf]
- [x] [2.2] [P-1] Live verification: after deploying, tail `cc-tmux-register-trace.log` during a
  real multi-hour session and confirm `rename_succeeded: true` entries correlate with an actually
  visible tab-name change — paste trace lines alongside the observed tab name at matching
  timestamps. If any `rename_succeeded: false` entries appear, paste those too (that's the
  diagnostic this proposal exists to surface). [owner:general-purpose] [type:testing] [beads:if-4ve9]
