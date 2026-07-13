<!-- beads:epic:if-bqw -->
<!-- beads:feature:if-ovnl -->

# Tasks: cc-tmux-git-status-glyphs

> Literal `## DB/API/UI/E2E Batch` headers per `/feature`'s wave-plan-build contract — mapped by
> domain fit (DB = unified local git-status parser + new pane option; API = cli.py per-field
> dual-source resolver; UI = render.py glyph format + new palette constants; E2E = tests + live
> verification). Owner: general-purpose engineer agents (no dedicated api/ui roles for this Python
> tmux plugin).

## DB Batch

- [x] [1.1] `apps/cc-tmux/src/cc_tmux/tmux.py`: replace `_git_dirty` and `_git_ahead` with a [beads:if-230l]
  single `_git_status(cwd: str) -> Optional[GitStatusCounts]` (a `NamedTuple` or small
  `@dataclass` with `modified: int`, `untracked: int`, `deleted: int`, `renamed: int`,
  `ahead: int`, `behind: int`, all defaulting to 0) that runs ONE `git status --porcelain=v2
  --branch` (via `_run_git`) and parses: the `# branch.ab +<ahead> -<behind>` header line (absent
  -> ahead=0, behind=0, no upstream) for ahead/behind; each `1 <XY> ...` ordinary-change line's
  `XY` status-code pair for modified (`M` in either position)/deleted (`D` in either
  position)/untracked is NOT an ordinary-change line (`?` lines are separate, see below);
  each `2 <XY> ...` line (renamed/copied) increments renamed; each `? <path>` line increments
  untracked. Return `None` only on git failure/not-a-repo/git-unavailable — a clean, up-to-date
  repo returns a valid all-zero `GitStatusCounts`, NOT `None` (callers must distinguish "checked,
  clean" from "couldn't check"). [owner:general-purpose] [type:api]
- [x] [1.2] `apps/cc-tmux/src/cc_tmux/tmux.py`: add `OPT_GIT_STATUS = "@cc-git-status"` constant; [beads:if-eitq]
  remove `OPT_DIRTY`/`OPT_AHEAD` from `_ALL_OPTS`, add `OPT_GIT_STATUS`. Extend
  `set_pane_git_identity` to call `_git_status(cwd)` ONCE (replacing its prior separate
  `_git_dirty(cwd)`/`_git_ahead(cwd)` calls) and write `@cc-git-status` as
  `json.dumps({"modified": m, "untracked": u, "deleted": d, "renamed": r, "ahead": a, "behind":
  b})` when the result is non-`None`, else `_unset_opt` (same fail-open unset-on-empty pattern as
  `@cc-branch`). `_git_branch`/`_git_toplevel_name` calls and the `@cc-branch`/`@cc-project`
  writes in this same function are UNCHANGED — do not touch them. Update the module docstring's
  tracked-options table: remove the `@cc-dirty`/`@cc-ahead` rows, add one `@cc-git-status` row.
  [owner:general-purpose] [type:api]
- [x] [1.3] `apps/cc-tmux/src/cc_tmux/usage.py`: add `GREEN = "#00ac3a"` and `BLUE = "#006efe"` [beads:if-irwp]
  module-level constants alongside the existing `DIM`/`CYAN`/`YELLOW`/`RED` (values taken from
  `home/dot_config/tmux/vercel-theme.conf`'s documented palette comment block — cite the exact
  source lines in a comment here, do not invent new hex values).
  [owner:general-purpose] [type:api]

## API Batch

- [x] [2.1] `apps/cc-tmux/src/cc_tmux/cli.py`: replace `_resolve_branch_dirty`/ [beads:if-3vho]
  `_parse_dirty_counts` with a per-field resolver — e.g. `_resolve_git_status(pane: str) ->
  Tuple[str, GitStatusCounts]` (branch, and a `GitStatusCounts`-shaped result assembled
  field-by-field). For EACH of `modified`/`untracked`/`deleted`/`renamed`/`ahead`/`behind`:
  prefer the value from `nx_agent.project_git_status(<registry code>)`'s `git` object when that
  specific key is present (use `.get(key)` presence-checked, not just truthiness — a legitimate
  `0` from nx must still count as "nx has this field"), else fall back to the corresponding field
  of the local `@cc-git-status` pane option (JSON-decoded; malformed/missing -> all-zero
  `GitStatusCounts`, fail open). Branch resolution (nx primary / `@cc-branch` fallback) is
  UNCHANGED — reuse the existing logic, do not rewrite it. Update `_build_session_bar` to call
  this new resolver and pass the six-field result plus `ahead`/`behind` to
  `render.render_session_bar` (task 3.1 updates that callee's signature — pass the new shape
  through even though the callee doesn't support it yet until 3.1 lands, same sequencing pattern
  as the prior spec's task 2.1). [owner:general-purpose] [type:api]
- [x] [2.2] `apps/cc-tmux/src/cc_tmux/cli.py`: update `_build_session_bar`'s docstring to describe [beads:if-1xen]
  the six-field per-field dual-source rule (replacing the now-stale `dirty`-as-2-tuple / bare
  `ahead`-int prose left over from the prior spec). [owner:general-purpose] [type:docs]

## UI Batch

- [x] [3.1] `apps/cc-tmux/src/cc_tmux/render.py`: change `render_session_bar`'s `dirty: [beads:if-2wia]
  Optional[Tuple[int, int]]` + `ahead: int` parameters to accept the full six-field shape (either
  a `GitStatusCounts`-like object/dict or six individual keyword params — implementer's choice,
  document whichever in the docstring). Import `GREEN`, `BLUE` from `.usage` alongside the
  existing `CYAN, DIM, RED, YELLOW`. Render, in fixed order after the branch segment: `<N>M`
  (GREEN) if modified>0, `<N>U` (YELLOW) if untracked>0, `<N>D` (RED) if deleted>0, `<N>R` (BLUE)
  if renamed>0, `⇡<N>` if ahead>0, `⇣<N>` if behind>0 — space-separated, each entirely omitted
  (no glyph, no stray space) when its count is 0. Update the function's docstring accordingly.
  [owner:general-purpose] [type:ui]

## E2E Batch

- [x] [4.1] Extend `apps/cc-tmux/src/cc_tmux/testing.py`: self-test cases for `tmux._git_status()` [beads:if-5pyy]
  against a real fixture git repo (subprocess-created, matching this file's existing fixture-repo
  convention from the prior spec's `tmux.git_dirty_ahead_fixture` case): clean tree -> all-zero
  `GitStatusCounts`; one staged-modified + one unstaged-deleted + one untracked file -> correct
  modified/deleted/untracked counts; a renamed file (via `git mv` or `git add` after a rename) ->
  correct renamed count; a branch ahead of a real local upstream -> correct ahead, 0 behind; a
  branch behind (via a second local clone/remote reset) -> correct behind, 0 ahead; no upstream
  configured -> 0/0. Run `cc-tmux self-test` and paste the passing stdout.
  [owner:general-purpose] [type:testing]
- [x] [4.2] Extend `apps/cc-tmux/src/cc_tmux/testing.py`: self-test cases for the new per-field [beads:if-sk45]
  dual-source resolver — nx response carrying only `modified`/`untracked` keys results in those
  two preferring nx while `deleted`/`renamed`/`ahead`/`behind` fall back to local (with
  deliberately different nx-vs-local values per field so a wrong-source bug is visible, mirroring
  the prior spec's `cli.build_session_bar_dual_source` test's technique); nx unreachable falls all
  six back to local; a SIMULATED nx response carrying all six keys results in all six preferring
  nx (proves the forward-compatible per-field rule with no future code change needed). Run
  `cc-tmux self-test` and paste the passing stdout. [owner:general-purpose] [type:testing]
- [x] [4.3] Extend `apps/cc-tmux/src/cc_tmux/testing.py`: self-test cases for [beads:if-57i6]
  `render_session_bar`'s new glyph format — each of the six fields at a representative nonzero
  count renders its exact glyph string in the correct color and position (e.g. modified=3 ->
  `#[fg=<GREEN>]3M`); a zero count for any field renders nothing for that field (no stray glyph,
  no stray separator); all-six-zero renders no working-tree-indicator segment at all. Run
  `cc-tmux self-test` and paste the passing stdout. [owner:general-purpose] [type:testing]
- [ ] [4.4] [deferred] [P-2] Live verification: with a real tracked pane in this repo, dirty the [beads:if-n9tl]
  working tree in a controlled way (modify a file, delete a file, rename a file, add an untracked
  file) and confirm row 2 renders the exact expected glyph string with correct colors — paste
  observed output. If the modified plugin isn't yet deployed as the live binary at task-run time,
  this may need execution-time deferral (never an authoring-time annotation) with real
  runtime evidence gathered from within the worktree in place of the visual click, mirroring the
  prior spec's task 4.4 handling — but attempt the real verification first; do not assume deferral.
  DEFERRED (2026-07-13, /apply orchestrator): attempted the real verification first, per the task
  text above. `~/.tmux/plugins/cc-tmux` is a real symlink straight to this repo's MAIN checkout
  (`/home/nyaptor/dev/personal/installfest/apps/cc-tmux`) — confirmed via `readlink -f` — so the
  live plugin only reflects `main`, not this session's worktree, until merge-back; no live
  tmux pane is currently tracking project `if` to click on anyway (checked all panes'
  `@cc-project` values). What WAS gathered instead, against REAL running services (not mocked):
  (1) `tmux._git_status('.')` against this worktree's own real dirty state returned a correct,
  non-`None` `GitStatusCounts`; (2) driving the REAL `cli._resolve_git_status` (monkeypatching
  only the pane-option/cwd LOOKUP plumbing, not any git/nx logic) with the cwd forced to the
  registered `if` project and a real `nx_agent.project_git_status('if')` call against the actual
  running nx-agent resolved `modified=1, untracked=1` from nx (nx's real live payload) and
  correctly fell back to local for `deleted`/`renamed` (nx has neither field yet); (3) **discovered
  live** that nx's `/projects/if/status` response NOW carries top-level `ahead`/`behind` keys
  (both `0`) that did not exist when this spec was authored — the per-field resolver correctly
  started preferring them over local's `ahead=9, behind=3` with ZERO code changes, proving the
  forward-compatible design against a REAL, not simulated, nx schema evolution; (4) rendered the
  real resolved `GitStatusCounts(modified=1, untracked=1, deleted=0, renamed=0, ahead=0,
  behind=0)` through the real `render_session_bar` — output: `...main #[fg=#00ac3a]1M
  #[fg=#FAC760]1U#[default]...` (correct GREEN `1M`, YELLOW `1U`, everything else correctly
  omitted). What remains genuinely unverified: the actual on-screen row-2 render after a real
  deploy, and the deleted/renamed glyphs specifically (no real dirty-with-deletes/renames pane
  available this session) — needs Leo to do a real click after this spec deploys.
  [owner:general-purpose] [type:testing]
- [x] [4.5] Update nx bead `nx-mbnqj` (nx's own tracker, NOT `if-*`) — extend its description to [beads:if-e3zf]
  cover the full anticipated schema expansion: `modified`/`untracked`/`deleted`/`renamed` counts
  (currently only `modified`/`untracked` exist) alongside the already-requested `ahead`/`behind`
  vs-upstream counts, all on `GET /projects/:id/status`'s `git` object. Do NOT create a second
  bead — this is one coherent `git_status_orbit` schema ask. Paste the updated bead's content.
  [owner:general-purpose] [type:docs]
  PREMISE CHANGED (2026-07-13): `nx-mbnqj` is already CLOSED — a separate nx-side spec
  (`add-git-ahead-behind-status`) shipped ahead/behind support to `GitStatusObject` today,
  discovered live during task 4.4's verification. The "one coherent ask" no longer holds (half
  done, half open), so "update, don't duplicate" no longer applies as written — filed a fresh,
  narrower bead `nx-c62s2` ("Add deleted/renamed counts to git-observer payload") for the
  remaining half instead, explicitly cross-referencing `nx-mbnqj` as precedent/pattern. Did NOT
  reopen or edit the already-correctly-closed `nx-mbnqj`.
