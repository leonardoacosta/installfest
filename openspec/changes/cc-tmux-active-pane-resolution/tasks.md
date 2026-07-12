<!-- beads:feature:if-1x7q -->

<!-- beads:epic:if-bqw -->

# Tasks: cc-tmux-active-pane-resolution

> Literal `## API/E2E Batch` headers per `/feature`'s wave-plan-build contract — mapped by domain
> fit, not a literal database/API/UI/E2E split (this repo has no such layering; precedent:
> `2026-07-11-cc-tmux-session-usage-bars` tasks.md). No DB or UI batch needed — this change is a
> pure Python behavior fix with no new tmux.conf/theme surface. Owner: general-purpose engineer
> agents.

## API Batch

- [x] [1.1] [P-1] `apps/cc-tmux/src/cc_tmux/cli.py`: add `_resolve_session_pane(window)` — returns
  `tmux.get_window_active_pane(window)` when that pane's `tmux.get_pane_option(pane,
  tmux.OPT_STATE)` is in `VALID_STATES` (already imported from `.priority`), else falls back to
  `tmux.get_window_top_pane(window)`. No new `tmux.py` primitive — composes two existing calls.
  [owner:general-purpose] [type:api] [beads:if-vtat]
- [x] [1.2] [P-1] `apps/cc-tmux/src/cc_tmux/cli.py`: swap `_build_session_bar`'s
  `pane = tmux.get_window_top_pane(window)` call for `pane = _resolve_session_pane(window)`
  (only when `pane is None` — `cmd_render_all` still passes an already-resolved `pane` through
  unchanged, so update its call site too: it currently calls `tmux.get_window_top_pane(window)`
  directly to share one resolved pane across row builders — route that through
  `_resolve_session_pane` as well so both entry points agree). Do NOT touch `_beads_pane`/row 3 —
  its active-pane fallback already exists for an unrelated reason (BEADS-03) and stays as-is.
  [owner:general-purpose] [type:api] [beads:if-f5ta]
- [x] [1.3] [P-1] `apps/cc-tmux/src/cc_tmux/cli.py`: remove the `session_count` computation
  (`sum(1 for p in tmux.get_hop_panes() if p.project == project) if project else 0`) from
  `_build_session_bar` and its corresponding parameter in the call to
  `render.render_session_bar(...)`. [owner:general-purpose] [type:api] [beads:if-acvy]
- [x] [1.4] [P-1] `apps/cc-tmux/src/cc_tmux/render.py`: remove `_session_glyph`,
  `SESSION_GLYPH_FILLED`, `SESSION_GLYPH_HOLLOW`, and the `session_count` parameter +
  first-list-item append in `render_session_bar`. Left side becomes
  `[model_letter?, project?, branch?]` composed exactly as today minus the glyph entry.
  [owner:general-purpose] [type:api] [beads:if-puh9]
- [x] [1.5] [P-2] `openspec/specs/cc-tmux/spec.md`: apply this proposal's MODIFIED Requirement
  delta (representative-pane resolution + drop the session-count-glyph scenario) — this is
  archive-time work, not a separate code task; note it here so `wave-plan-build` accounts for it
  in the batch. [owner:general-purpose] [type:docs] [beads:if-vuwv]

## E2E Batch

- [x] [2.1] [P-1] Extend `apps/cc-tmux/src/cc_tmux/testing.py`: self-test cases for
  `_resolve_session_pane` (active+tracked -> returns active; active+untracked -> falls back to
  `get_window_top_pane`'s pick; no resolvable active pane -> fail-open fallback) and for
  `render_session_bar` with no `session_count` argument (left side has no leading glyph token
  regardless of how many tracked panes share the project). Run `cc-tmux self-test` and paste the
  passing stdout. [owner:general-purpose] [type:testing] [beads:if-6lkm]
- [x] [2.2] [P-1] Live verification: split a tmux window into two Claude panes (e.g. two different
  projects or two different `/model` selections), focus each pane in turn, and observe
  `cc-tmux session-bar <window_id>` (or the live rendered row) reflect the FOCUSED pane's
  project/branch/model/SES/dirty/ahead each time it changes — paste observed output for both
  focus states. [owner:general-purpose] [type:testing] [beads:if-kpme]
- [x] [2.3] [P-1] Live verification: focus a plain-shell pane in a window that also has a
  background Claude pane in `waiting`; confirm row 2 still surfaces the waiting pane's identity
  (fallback intact, not a blank row) — paste observed output. [owner:general-purpose] [type:testing] [beads:if-52jh]
- [x] [2.4] [P-2] Live verification: confirm the `◉`/`◌` glyph no longer appears anywhere on row 2
  after `chezmoi apply` + tmux reload — paste the observed row. [owner:general-purpose] [type:testing] [beads:if-200n]
