---
status: draft
---

# Proposal: cc-tmux-active-pane-resolution

## Why

Live feedback (screenshot review, 2026-07-12 + `/openspec:explore` follow-up) surfaced four
apparent row-2 defects: the model letter (F/O/H/S) renders unreliably, the SES gauge always
shows `--`, the git dirty(`*`)/ahead(`^N`) markers never appear, and — named directly by the
user — row 2 shows "just the first pane's cc session" rather than the one actually being
looked at.

All four trace to ONE root cause, confirmed in code and live on this machine: `_build_session_bar`
(`cli.py`) resolves a window's "representative pane" via `tmux.get_window_top_pane()`, which sorts
tracked panes by `@cc-state` priority (`waiting` > `idle` > `active`) and, on a tie, takes
Python's `min()` over tmux's pane-list iteration order — i.e. literally the first pane by index
when no pane is `waiting`. It never consults tmux's own focused-pane attribute
(`#{pane_active}`), which `tmux.get_window_active_pane()` already exposes but which is currently
used only as a row-3 fallback (`_beads_pane`), never for row 2.

Model letter, SES, and dirty/ahead all come from the SAME file —
`session-context.<pane_id>.json`, written by nexus-statusline keyed on whichever pane's
`$TMUX_PANE` is actually running that Claude process. When row 2 resolves the wrong pane id (any
window with 2+ tracked Claude panes, or a tie with no `waiting` pane), the lookup misses and every
field sourced from it degrades silently (by design — fail-open) to blank/`--`. Verified live:
only ONE `session-context.*.json` file exists on this machine right now, for an unrelated
pane/project — confirming most window lookups miss today, not just in theory.

Starship's own shell-prompt `git_status` module was separately verified (direct `starship
prompt` invocation against a dirty repo) to render correctly — ruling it out as the source of
the missing git markers. The dirty/ahead symptom is specifically about the tmux row, and shares
the exact same cache-miss mechanism as the model-letter and SES symptoms.

Bundled in this same proposal (same two files, same render pass, independent of the fix above):
remove the `◉`/`◌` session-count glyph from row 2's left side. It has no bearing on the bug
above; it is being retired because the user does not want it tracked.

## What Changes

- **`cli.py`**: add a small resolver that prefers the window's tmux-ACTIVE pane when it carries a
  valid `@cc-state` (i.e. it's a tracked Claude pane), falling back to the existing
  priority-based `get_window_top_pane()` only when the active pane is untracked (e.g. a plain
  shell pane focused in a split next to a background Claude pane). `_build_session_bar` (row 2
  only — NOT `_beads_pane`/row 3, which already has its own active-pane fallback for a different
  reason) uses this resolver instead of calling `get_window_top_pane()` directly.
- **`render.py`**: remove `_session_glyph`, its call site in `render_session_bar`, and the
  `session_count` parameter. `render_session_bar`'s left side becomes model letter + project +
  branch (no leading glyph).
- **`cli.py`**: drop the `session_count` computation (`sum(1 for p in tmux.get_hop_panes() ...)`)
  in `_build_session_bar` — it exists solely to feed the removed glyph.
- **`openspec/specs/cc-tmux/spec.md`**: MODIFIED Requirement — "A dedicated tmux status row shows
  session identity and usage" gets a new representative-pane-resolution scenario (prefer focused
  tracked pane over priority pick) and the session-count-glyph scenario is removed from the
  Given/Then examples.

## Non-Goals

- No change to SES's data source or definition. SES stays `context_used_pct` from
  `session-context.<pane>.json` (a local context-window-utilization metric) — it is NOT being
  wired to a direct Anthropic API call, Claude Code CLI stdin, or an "ant cli". That would also
  conflict with the committed Requirement "Usage polling is consolidated to a single Anthropic
  caller" (nexus-agent is the only process allowed to call `/api/oauth/usage`). This proposal
  expects the EXISTING SES definition to start rendering correctly once the pane-resolution bug
  is fixed — no new data source needed.
- No change to `get_window_top_pane()`'s priority semantics themselves (still waiting > idle >
  active for the fallback case) — only when it's consulted changes.
- No change to row 3 (`_beads_pane`/`_build_beads_bar`) — its active-pane fallback already exists
  for an unrelated reason (BEADS-03: row 3 needs only a cwd, not hook liveness) and is untouched.
- No new tmux.py primitive — the fix composes two existing functions
  (`get_window_active_pane` + `get_pane_option`) already used elsewhere in this file; no new
  server round-trip is introduced.
- No nexus-statusline (nx repo) changes — the writer side already keys correctly by pane id; only
  the reader side (which pane id to look up) was wrong.

## Context

- touches: `apps/cc-tmux/src/cc_tmux/cli.py`, `apps/cc-tmux/src/cc_tmux/render.py`,
  `openspec/specs/cc-tmux/spec.md`
- Related: extends the `[CAPABILITY] cc-tmux` epic (`if-bqw`). Follows directly from
  `2026-07-11-cc-tmux-session-usage-bars` (introduced `get_window_top_pane` /
  `render_session_bar`'s session-count glyph) and `2026-07-11-cc-tmux-bar-cleanup` (moved the
  model letter to `session-context.<pane>.json`, the file this bug's lookup misses against). No
  conflict with the in-progress `view-command` spec (unrelated files — terminal file rendering,
  not the status bar).
- Origin: `/openspec:explore` session, 2026-07-12 — findings pre-loaded into this proposal per
  that session's Phase Z scaffold offer; not re-derived here.

## Testing

| Seam | Coverage |
| --- | --- |
| New pane resolver (pure-ish: two existing tmux.py calls composed) | `cc-tmux self-test` case: active pane tracked -> returns it; active pane untracked -> falls back to `get_window_top_pane()`'s pick; no active pane resolvable -> falls back cleanly (fail-open) — task 1.3 |
| `render_session_bar()` without session-count glyph | `cc-tmux self-test` case: left side renders model+project+branch only, no leading glyph token, for 0/1/2+ tracked panes in the project (count no longer affects output) — task 1.4 |
| End-to-end: model letter / SES / dirty / ahead on a real 2-pane window | Live verification: split a window into two Claude panes, focus each in turn, observe `cc-tmux session-bar <window_id>` (or the live rendered row) reflect the FOCUSED pane's project/branch/model/SES/dirty/ahead each time — paste observed output for both focus states — task 2.2 |
| Fallback case: focused pane untracked | Live verification: focus a plain shell pane in a window that also has a background waiting Claude pane; confirm row 2 still surfaces the waiting Claude pane's identity (fallback intact) — task 2.3 |
