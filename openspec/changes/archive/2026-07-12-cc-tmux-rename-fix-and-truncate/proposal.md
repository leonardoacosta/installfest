---
status: draft
---

# Proposal: cc-tmux-rename-fix-and-truncate

## Why

Two related complaints about the opt-in window-rename feature (`@cc-window-rename on`,
`@cc-window-rename-format title` — both confirmed enabled in `home/dot_config/tmux/
tmux.conf.tmpl`): the tab name isn't visibly updating, and the 10-character truncation budget is
too tight for `<project-code>·<session-title>`.

**Root cause of "not replacing tab name" (verified in code, not yet reproduced live — no `$TMUX`
in this investigation environment):** `_maybe_rename_window` (`cli.py`) issues
`tmux._run_tmux(["rename-window", "-t", pane_id, name])` and then unconditionally returns `True`
— the tmux subprocess's exit status is **never checked**. The diagnostic trace log
(`cc-tmux-register-trace.log`, added specifically to debug this class of bug) confirms
`rename_fired: true` on **100% of 2,875 real hook calls** sampled live on this machine — but
"fired" here only means "the code reached the point of issuing the command," not "tmux
confirmed the rename." A transient failure (stale pane id, a race with the window closing, any
non-zero exit from `rename-window`) would look identical in the trace to a genuine success,
which is exactly why the trace hasn't already surfaced this as an obvious failure: it was never
built to distinguish the two. This proposal makes `_maybe_rename_window` check the actual command
result and records it, turning the trace from "attempted" into "attempted, and here's whether it
worked."

**Contributing factor, also verified:** `@cc-title` (the session-title half of the `title`
format) is populated ONLY from the `SessionStart` hook payload's `session_title` field, captured
once per session. A session with no custom title (`/rename` or `-n` never used) leaves `@cc-title`
unset for its entire lifetime, so `_title_window_name` falls back to the project code alone (or
the bare project name) — which can look identical to "the rename isn't doing anything new" even
when the mechanism is technically firing correctly. Not fixed in this proposal (no session-title
data exists to use when the user never set one) — flagged so the live-verification task
distinguishes "genuinely broken" from "title format degraded to code-only because there's no
title to show."

## What Changes

- **`apps/cc-tmux/src/cc_tmux/cli.py`** (`_maybe_rename_window`): capture `rename-window`'s actual
  success/failure (via `tmux._run_tmux`'s existing `None`-on-failure return contract — no new
  primitive needed) and return `False` when it failed, instead of always `True` once a command was
  issued.
- **`apps/cc-tmux/src/cc_tmux/cli.py`** (`_trace_register`): add a `rename_succeeded` field
  (distinct from the existing `rename_attempted`/`rename_fired`) so a future live trace can show
  attempted-but-failed vs succeeded, closing the exact diagnostic gap this bug exposed.
- **`_TAB_NAME_MAX`**: `10` -> `20`. Trivial constant change; `compose_title_name`'s truncation
  logic is otherwise unaffected.
- **`openspec/specs/cc-tmux/spec.md`**: MODIFIED Requirement — "Opt-in window rename supports a
  project-code + session-title format" gets its truncation figure and worked example updated from
  10 to 20 characters, plus a new scenario covering a failed `rename-window` call.

## Non-Goals

- No fix for `@cc-title` frequently being unset — that's a data-availability gap (no title was
  ever set for that session), not a bug in the rename mechanism itself.
- No retry/backoff on a failed rename — a single failed attempt just means the NEXT hook fire
  (there's one on nearly every tool use) tries again; this proposal makes failure *visible*, not
  self-healing beyond what already happens naturally via hook frequency.
- No change to the `state`-format path (non-default; only `title` format is in scope, since
  that's what's actually enabled on this machine).

## Context

- touches: `apps/cc-tmux/src/cc_tmux/cli.py`, `openspec/specs/cc-tmux/spec.md`
- Related: extends the `[CAPABILITY] cc-tmux` epic (`if-bqw`). No file overlap with
  `cc-tmux-active-pane-resolution` or `cc-tmux-row3-openspec-beads-format` (both touch different
  functions in the same `cli.py` — a wave-plan conflict-matrix check should still confirm no
  line-range collision before parallel dispatch, same note as those proposals' own Context
  sections).
- Origin: `/openspec:explore` session, 2026-07-12.

## Testing

| Seam | Coverage |
| --- | --- |
| `_maybe_rename_window`'s success/failure return | `cc-tmux self-test` case: a mocked `_run_tmux` returning `None` (failure) makes `_maybe_rename_window` return `False`; a mocked success return makes it return `True` — task 1.2 |
| `_trace_register`'s new `rename_succeeded` field | `cc-tmux self-test` case: the trace entry's `rename_succeeded` matches the mocked command outcome — task 1.2 |
| `compose_title_name` at the new 20-char budget | `cc-tmux self-test` case: a code+title combination that fits in 11-20 chars (previously truncated, now not) renders in full; anything over 20 still truncates — task 1.3 |
| End-to-end rename, live | Live verification: with the register-trace fix deployed, tail `cc-tmux-register-trace.log` for a real multi-hour session and confirm `rename_succeeded: true` correlates with an actually-changed visible tab name (paste both the trace lines and the observed tab name at the same timestamps) — task 2.1 |
