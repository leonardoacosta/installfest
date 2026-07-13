<!-- beads:epic:if-bqw -->
<!-- beads:feature:if-d3i9 -->

# Tasks: cc-tmux-adopt-nx-context-and-git-status

> Literal `## DB/API/UI/E2E Batch` headers per `/feature`'s wave-plan-build contract — mapped by
> domain fit (DB = new nx HTTP client + local git helpers + new pane options; API = cli.py
> wiring/composition; UI = render.py display format; E2E = tests + live verification). Owner:
> general-purpose engineer agents (this repo has no dedicated api-engineer/ui-engineer roles for a
> Python tmux plugin).

## DB Batch

- [x] [1.1] `apps/cc-tmux/src/cc_tmux/nx_agent.py` (new module): `session_context(session_id: str, [beads:if-rovv]
  ttl=5.0, cache_path=None, now=None) -> Optional[dict]` querying `GET
  http://localhost:7400/sessions/{id}/context`, and `project_git_status(code: str, ttl=5.0,
  cache_path=None, now=None) -> Optional[dict]` querying `GET
  http://localhost:7400/projects/{code}/status` and returning only the `git` sub-object (or `None`
  if absent/404/unreachable). Both reuse `usage._query()` for the HTTP fetch (1s timeout,
  fail-open `None` on any error — do not duplicate urllib boilerplate) and a NEW small generic
  on-disk cache pair (atomic `.tmp` + `os.replace` write, mtime-based TTL check) mirroring
  `usage._read_usage_cache`/`_write_usage_cache`'s shape but generic over an arbitrary JSON dict
  rather than the label/5H/7D triple those are hardcoded to — `usage.py`'s cache helpers are
  `_`-private and payload-shape-specific; a fresh, small, generic pair here is the right call over
  a cross-cutting refactor of an already-shipped module. `cache_path`/`now` injectable for
  self-test, matching `usage.active_usage`'s existing convention.
  [owner:general-purpose] [type:api]
- [x] [1.2] `apps/cc-tmux/src/cc_tmux/tmux.py`: add `_git_dirty(cwd: str) -> Optional[Tuple[int, [beads:if-vjqc]
  int]]` (parse `git status --porcelain` output into `(modified, untracked)` counts — lines
  starting `??` are untracked, everything else non-blank is modified; `None` on a clean tree or
  git failure) and `_git_ahead(cwd: str) -> int` (parse `git rev-list --count @{upstream}..HEAD`;
  `0` on no upstream, detached HEAD, or git failure — mirrors `_git_branch`'s fail-open shape,
  reuse `_run_git`). [owner:general-purpose] [type:api]
- [ ] [1.3] `apps/cc-tmux/src/cc_tmux/tmux.py`: extend `set_pane_git_identity` to call the new [beads:if-xpsb]
  task-1.2 helpers alongside the existing `_git_branch` call, writing `@cc-dirty` (JSON-encoded
  `[modified, untracked]`, unset when `None`/clean) and `@cc-ahead` (stringified int, unset when
  `0`) pane options. Add `OPT_DIRTY = "@cc-dirty"`, `OPT_AHEAD = "@cc-ahead"`, and
  `OPT_SESSION_ID = "@cc-session-id"` constants; add all three to `_ALL_OPTS` so
  `clear_pane_state` retires them on `SessionEnd` like every other tracked option. Update the
  module docstring's tracked-options table to list the three new options.
  [owner:general-purpose] [type:api]
- [ ] [1.4] `apps/cc-tmux/src/cc_tmux/cli.py`: in `cmd_register`'s existing `SessionStart` branch [beads:if-mro6]
  (the block that already reads `hook_payload.get("session_title")`), also read
  `hook_payload.get("session_id")` and, when present, call `tmux._set_opt(pane,
  tmux.OPT_SESSION_ID, session_id)`. [owner:general-purpose] [type:api]

## API Batch

- [ ] [2.1] `apps/cc-tmux/src/cc_tmux/cli.py`: rewrite `_build_session_bar`'s field resolution. [beads:if-56x4]
  `context_used_pct` -> `nx_agent.session_context(tmux.get_pane_option(pane,
  tmux.OPT_SESSION_ID))`'s `usedPercentage / 100` when present, else `None`. `branch`/`dirty` ->
  `nx_agent.project_git_status(registry.resolve_project_code(pane_cwd))`'s `git` object when
  present, else fall back to `tmux.get_pane_option(pane, tmux.OPT_BRANCH)` /
  `tmux.get_pane_option(pane, tmux.OPT_DIRTY)` (JSON-decoded). `ahead` ->
  `tmux.get_pane_option(pane, tmux.OPT_AHEAD)` always (no nx query). `model_letter` -> UNCHANGED,
  still `_read_session_context`'s read of the legacy per-pane file. Keep `_read_session_context`
  itself in place for the model_letter read (its return shape may shrink to just the letter, or
  stay as-is with the other fields simply unused by the caller — implementer's call, document
  whichever is chosen in the function's own docstring). [owner:general-purpose] [type:api]
- [ ] [2.2] `apps/cc-tmux/src/cc_tmux/cli.py`: update the docstrings on `_build_session_bar`, [beads:if-2soe]
  `_read_session_context`, and `SESSION_CONTEXT_MAX_AGE_SECS` to reflect the new dual-source
  reality — do not leave stale prose claiming the legacy file is the sole source of
  branch/dirty/ahead/context_used_pct. [owner:general-purpose] [type:docs]

## UI Batch

- [ ] [3.1] `apps/cc-tmux/src/cc_tmux/render.py`: change `render_session_bar`'s `dirty` parameter [beads:if-d178]
  from `bool = False` to `dirty: Optional[Tuple[int, int]] = None` (modified, untracked). Render
  `*<modified>+<untracked>` in the existing YELLOW styling when `modified + untracked > 0`, render
  nothing when `None` or `(0, 0)`. `ahead`'s existing `int`/`^N` behavior is unchanged.
  [owner:general-purpose] [type:ui]

## E2E Batch

- [ ] [4.1] Extend `apps/cc-tmux/src/cc_tmux/testing.py`: self-test cases for `nx_agent.py` — cache [beads:if-cy8g]
  hit skips the HTTP call, cache miss/expiry triggers a live fetch and rewrites the cache
  (including negative-caching the empty result on failure, mirroring `usage.active_usage`'s
  documented convention), unreachable/malformed response -> `None`. Separate cases for
  `tmux._git_dirty`/`_git_ahead` against a real fixture git repo (clean tree, dirty tree with
  known modified+untracked counts, no-upstream branch). Run `cc-tmux self-test` and paste the
  passing stdout. [owner:general-purpose] [type:testing]
- [ ] [4.2] Extend `apps/cc-tmux/src/cc_tmux/testing.py`: self-test cases for `cmd_register`'s [beads:if-1l6j]
  session_id capture (SessionStart payload sets `@cc-session-id`; non-SessionStart events leave it
  untouched) and `_build_session_bar`'s dual-source composition (nx reachable -> nx values used
  for branch/dirty/context_used_pct; nx unreachable/404 -> local `@cc-branch`/`@cc-dirty` fallback
  used; `ahead` always local regardless of nx reachability; `model_letter` untouched/unaffected by
  any of the above). Run `cc-tmux self-test` and paste the passing stdout.
  [owner:general-purpose] [type:testing]
- [ ] [4.3] Extend `apps/cc-tmux/src/cc_tmux/testing.py`: self-test case for [beads:if-c029]
  `render_session_bar`'s new dirty-counts format — `(3, 2)` renders `*3+2`, `(0, 0)`/`None`
  renders nothing, `ahead=2` still renders `^2` unchanged. Run `cc-tmux self-test` and paste the
  passing stdout. [owner:general-purpose] [type:testing]
- [ ] [4.4] [P-2] Live verification: with a real tracked pane in this repo (project `if`) and [beads:if-7pdj]
  nx-agent running, confirm row 2 renders SES% from `/sessions/:id/context`, branch/dirty-counts
  from `/projects/if/status`, and `ahead` from local git — paste observed row-2 output. Then stop
  nx-agent (or point at an unreachable port) and confirm row 2 falls back to local
  branch/dirty/blank-SES without erroring — paste that observed output too.
  [owner:general-purpose] [type:testing]
- [ ] [4.5] [P-3] File two nx-side beads in nx's own tracker (not `if-*`): (1) add an [beads:if-2qyc]
  ahead/behind-vs-upstream field to `git-observer`'s payload, referencing this proposal's Non-Goals
  and nx bead `nx-yn6c2`'s sibling scope; (2) add a `model` field to `/sessions/:id/context`,
  referencing this proposal's Why item 3. Paste the created bead IDs.
  [owner:general-purpose] [type:docs]
