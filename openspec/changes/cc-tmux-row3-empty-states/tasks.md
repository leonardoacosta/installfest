---
stack: cc-meta
---
<!-- beads:epic:if-bqw -->
<!-- beads:feature:if-rq7c -->

# Tasks: cc-tmux-row3-empty-states

> Literal `## DB/API/UI/E2E Batch` headers per `/feature`'s wave-plan-build contract. Owner:
> general-purpose engineer agents. Verification is `python -c` function-level stdout + live
> `cc-tmux render-all` capture against a crafted roadmap-pulse cache file — no pytest harness
> exists in this plugin (see `testing.py`'s own `_check`-based convention). No chezmoi apply /
> plugin reload needed — `bin/cc-tmux` points `PYTHONPATH` directly at `src/`, no install step,
> no long-lived process caching the module.

## DB Batch

- [x] [1.1] `apps/cc-tmux/src/cc_tmux/render.py` (`render_beads_bar`): after building [beads:if-hldl]
  `left_segments` from the existing per-half `if ... is not None` checks, add the two collapsed
  branches — (a) when all six count args are non-`None` and all `== 0`, set `left = "All caught
  up"` (`#[fg={DIM}]All caught up`) instead of joining `left_segments`; (b) when all six count
  args are `None` (today's `left_segments == []` case), set `left = "Not available"`
  (`#[fg={DIM}]Not available`) instead of `""`. Update the function's docstring to describe both
  new branches (mirroring the existing "fail-open"/"independent segment" prose style). Preserve
  the existing partial-half behavior (one half present-and-zero or present-non-zero, other half
  `None`) unchanged — neither collapsed branch fires unless BOTH halves match the same state.
- [x] [1.2] Function-level verification (paste stdout): `python -c` cases — all six `0` -> `All [beads:if-lijl]
  caught up`, no numbers, no separator; all six `None` -> `Not available`; one half `0/0/0` +
  other half real non-zero (e.g. `3o 2r 1b`) -> both segments render verbatim, no collapse; one
  half `None`, other half real non-zero -> unchanged single-segment rendering (regression check
  against the pre-existing "partial half omitted" contract); `account_label` set + all six
  `None` -> `Not available` on the left AND the account segment still renders on the right.

## API Batch

(empty — no new tmux options, no `cli.py`/`tmux.py` wiring; `_build_beads_bar` already passes
every count through to `render_beads_bar` unconditionally, so the new branches need no new
plumbing upstream)

## UI Batch

(empty — no new status row, no theme-file changes; both collapsed strings render as plain DIM
text inside the same row segment the four theme `.conf` files already wrap, so no per-theme
styling decision is needed)

## E2E Batch

- [ ] [4.1] Live acceptance (paste captures): craft a temporary roadmap-pulse `.line` file with [beads:if-k7oo]
  `op: 0o 0ip 0ua` / `bd: 0o 0r 0b`, run `cc-tmux render-all <window_id> <w> <h>` against a pane
  resolving to that project, and paste the rendered row-3 output showing `All caught up`. Repeat
  with an empty/missing `.line` file (or one with no matching `op:`/`bd:` lines at all) and paste
  the output showing `Not available`. Repeat once more with an active nexus-agent credential
  present alongside the missing-cache case, confirming both `Not available` (left) and the
  account identity segment (right) render together.
- [ ] [4.2] Update `apps/cc-tmux/src/cc_tmux/testing.py`: change the existing assertion at (or [beads:if-f6ez]
  near) line 2101 from `render.render_beads_bar(None, None, None, None, None, None) == ""` to
  assert `== "Not available"` styling, and add the three new cases from task 1.2 as `_check`
  assertions in the same section. Paste the full `testing.py` run's pass/fail summary output.
