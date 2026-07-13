---
status: draft
---

# Proposal: cc-tmux-git-status-glyphs

## Why

`cc-tmux-adopt-nx-context-and-git-status` (shipped 2026-07-13, `if-d3i9`) gave row 2's dirty
indicator a `*<modified>+<untracked>` format and its ahead indicator a plain `^N`. Reviewing the
live render (`installfest > main*0+1`), Leo asked for a redesign matching this repo's own shell
prompt conventions: `home/dot_config/starship/starship.toml.tmpl`'s `[git_status]` module uses
starship's **unmodified default glyphs** (confirmed: no `[git_status]` symbol overrides in that
file) — `⇡`/`⇣` for ahead/behind, plus per-category status letters. This proposal ports that same
visual language to cc-tmux's row 2, with count-prefixed letters instead of starship's bare glyphs
(cc-tmux's row is denser and benefits from an explicit count Leo can read at a glance):

- `<N>M` — modified, GREEN
- `<N>U` — untracked, YELLOW
- `<N>D` — deleted, RED
- `<N>R` — renamed, BLUE
- `⇡<N>` — ahead of upstream (replaces the current bare `^N`)
- `⇣<N>` — behind upstream (NEW — no current indicator exists for this at all)

Each of the six is hidden entirely when its count is 0, space-separated, in that fixed order.

**Data source, three real decisions, all resolved via clarifying questions before drafting:**

1. **Deleted/renamed have no nx source today.** nx's `GitStatusObject` (`packages/core/src/types/
   git-status.ts`) carries only `dirty: {modified, untracked}` — no deleted/renamed breakdown
   exists anywhere in nx's payload or `git_events` schema. Computed locally via
   `git status --porcelain=v2 --branch` prefix-code parsing (cheap; cc-tmux already shells out to
   git on every `waiting`/`idle` transition).
2. **Ahead/behind stay local-only, same shape as the just-shipped `ahead`.** nx has no
   ahead/behind field at all. `behind` is new local-only computation, same treatment as `ahead`.
3. **Per-field dual-source, not an all-or-nothing block.** Rather than either (a) reverting the
   just-shipped nx-primary behavior for modified/untracked entirely, or (b) hard-coding "M/U from
   nx, D/R/ahead/behind always local" as a permanent special case, this proposal scaffolds a
   uniform PER-FIELD dual-source contract across all six metrics: for each field, prefer nx's
   value when nx's response actually carries that field, else fall back to the local
   `git status --porcelain=v2 --branch` computation. Today this reduces to exactly the mixed
   behavior above (nx has M/U, doesn't have D/R/ahead/behind) — but it's expressed as ONE
   consistent per-field rule, not a hardcoded split, so when nx's git-observer eventually expands
   (tracked via updating bead `nx-mbnqj`, see below) cc-tmux automatically starts preferring the
   richer nx data with ZERO code changes on this side. Leo explicitly accepted the resulting
   cross-field consistency tradeoff (nx's M/U reflecting its last ≤60s poll while D/R/ahead/behind
   reflect the instant of the last local `waiting`/`idle` transition) in favor of this
   forward-compatible shape.

**Efficiency note, not just a stylistic choice:** nx's own `git-observer.ts` derives its entire
git snapshot from a SINGLE `git status --porcelain=v2 --branch` spawn (confirmed by reading that
file directly) — branch, dirty counts, and (once nx implements it) ahead/behind all come out of
one process invocation, one `--branch` header line plus per-file status codes. cc-tmux's local
fallback currently uses THREE separate git shell-outs for the equivalent data (`_git_dirty`'s
`git status --porcelain` v1, plus a dedicated `_git_ahead`'s `git rev-list --count`, plus this
proposal would otherwise need a fourth for `behind`). This proposal replaces those two existing
functions with ONE new `_git_status(cwd)` that parses `--porcelain=v2 --branch` once, mirroring
nx's own approach — one process spawn instead of what would otherwise become four, and the exact
same underlying git subcommand nx uses (so any future comparison between nx's and cc-tmux's
locally-computed values is apples-to-apples). `_git_branch`/`_git_toplevel_name` (branch/project
resolution) are explicitly UNCHANGED — this consolidation is scoped to the dirty/ahead/behind
metrics only, not a wider rewrite of `set_pane_git_identity`'s existing working paths.

**nx bead update, not a new bead.** Bead `nx-mbnqj` (filed by the previous spec, nx's own
tracker) already asks for an ahead/behind field on `git-observer`'s payload. Rather than filing a
second, overlapping bead for deleted/renamed (same underlying `git_status_orbit` surface, almost
certainly the same nx-side change), this proposal UPDATES `nx-mbnqj`'s description to cover the
full six-field expansion (modified/untracked/deleted/renamed counts + ahead/behind vs upstream) —
one bead, one coherent ask, matching the anticipatory-schema framing above.

## What Changes

- **`apps/cc-tmux/src/cc_tmux/tmux.py`**: replace `_git_dirty` and `_git_ahead` with a single
  `_git_status(cwd) -> Optional[GitStatusCounts]` parsing `git status --porcelain=v2 --branch`
  once — `GitStatusCounts` is a small namedtuple/dataclass with `modified`, `untracked`,
  `deleted`, `renamed`, `ahead`, `behind` (all `int`, defaulting to 0 when the porcelain output
  carries no `# branch.ab` line, i.e. no upstream configured). Returns `None` only when `git` is
  unavailable, `cwd` isn't a repo, or the process fails — NOT when all six counts are legitimately
  0 (a clean, up-to-date tree still returns a valid all-zero `GitStatusCounts`, distinct from "no
  git at all," since callers need to distinguish "definitely clean" from "couldn't check").
  `set_pane_git_identity` calls this once (replacing its two prior calls) and persists the result
  as a single new pane option `@cc-git-status` (JSON-encoded 6-field object), retiring `@cc-dirty`/
  `@cc-ahead` (removed from `_ALL_OPTS`, replaced by `@cc-git-status`). `_git_branch`/
  `_git_toplevel_name` and the `@cc-branch`/`@cc-project` options are UNCHANGED.
- **`apps/cc-tmux/src/cc_tmux/nx_agent.py`**: no code change — `project_git_status`'s return
  shape (nx's raw `git` object) already passes through whatever fields nx sends; new fields
  arriving later need no client-side change.
- **`apps/cc-tmux/src/cc_tmux/cli.py`**: `_resolve_branch_dirty` (or its replacement) becomes a
  per-field resolver: for each of `modified`/`untracked`/`deleted`/`renamed`/`ahead`/`behind`,
  prefer the value from nx's `git` object when that key is present there, else the corresponding
  field from the local `@cc-git-status` pane option (JSON-decoded). `branch` sourcing (nx primary,
  `@cc-branch` fallback) is UNCHANGED.
- **`apps/cc-tmux/src/cc_tmux/usage.py`**: add `GREEN = "#00ac3a"` and `BLUE = "#006efe"` constants
  alongside the existing `DIM`/`CYAN`/`YELLOW`/`RED` (values taken directly from
  `home/dot_config/tmux/vercel-theme.conf`'s own documented palette comment block — same brand
  colors this repo's tmux theme already names, not new invented hex values).
- **`apps/cc-tmux/src/cc_tmux/render.py`**: `render_session_bar`'s `dirty: Optional[Tuple[int,
  int]]` parameter is replaced by a richer counts parameter (all six fields); the dirty-marker
  rendering logic changes from `*<modified>+<untracked>` to space-separated, per-category,
  zero-hidden, color-coded `<N>M <N>U <N>D <N>R` (GREEN/YELLOW/RED/BLUE respectively). `ahead`'s
  render changes from `^N` to `⇡N`; a new `behind` param renders `⇣N` (both hidden at 0), in that
  fixed left-to-right order after the branch name.

## Non-Goals

- No nx-side implementation of the expanded schema — `nx-mbnqj`'s description is updated to
  describe it; actually adding the fields to `git-observer`'s payload is nx-repo work, out of
  scope here (same boundary the prior spec already established for `ahead`).
- No change to `@cc-branch`/`@cc-project` sourcing or `_git_branch`/`_git_toplevel_name` — those
  functions and their fail-open unset-on-empty behavior are untouched.
- No change to the model-letter segment, the account-usage (SES:/5H:/7D:) segment, or any other
  row-2 content — scoped entirely to the git working-tree indicator block.
- No attempt to reconcile a genuine cross-field staleness gap between nx-sourced and
  local-sourced values within the same render (e.g. nx's M/U from a 40-second-old poll rendering
  beside local D/R computed just now) — accepted tradeoff per Why item 3, not a bug this proposal
  fixes.

## Context

- Extends: `apps/cc-tmux/src/cc_tmux/tmux.py` (`_git_dirty`, `_git_ahead` — both retired and
  replaced), `apps/cc-tmux/src/cc_tmux/cli.py` (`_resolve_branch_dirty`,
  `_parse_dirty_counts`), `apps/cc-tmux/src/cc_tmux/render.py` (`render_session_bar`),
  `apps/cc-tmux/src/cc_tmux/usage.py` (new `GREEN`/`BLUE` constants)
- Related: `cc-tmux-adopt-nx-context-and-git-status` (`if-d3i9`, archived 2026-07-13) — the
  capability this proposal directly follows on from; supersedes its dirty/ahead render format
  while leaving its branch dual-source design and nx-agent client untouched.
- Related: nx bead `nx-mbnqj` ("Add ahead/behind-vs-upstream field to git-observer payload") —
  UPDATED by this proposal's execution to also cover deleted/renamed counts, not a new bead.
- Related: `home/dot_config/starship/starship.toml.tmpl` — the glyph/color convention this
  proposal ports (starship's own unmodified `git_status` defaults) and the exact hex source for
  the new GREEN/BLUE constants (`home/dot_config/tmux/vercel-theme.conf`'s documented palette).
- touches: `apps/cc-tmux/src/cc_tmux/tmux.py`, `apps/cc-tmux/src/cc_tmux/cli.py`, `apps/cc-tmux/src/cc_tmux/render.py`, `apps/cc-tmux/src/cc_tmux/usage.py`, `apps/cc-tmux/src/cc_tmux/testing.py`, `openspec/specs/cc-tmux/spec.md`

## Testing

| Seam | Coverage |
| --- | --- |
| `tmux._git_status()` (porcelain-v2 parsing, pure-ish against a real fixture repo) | `cc-tmux self-test` cases: clean tree -> all-zero counts, staged+unstaged modified/deleted/renamed/untracked files -> correct per-category counts, ahead-only upstream -> correct ahead/0-behind, behind-only upstream -> correct behind/0-ahead, no upstream -> 0/0 — task 4.1 |
| `cli`'s per-field dual-source resolver | `cc-tmux self-test` cases: nx response carries only modified/untracked -> those two prefer nx, deleted/renamed/ahead/behind fall back to local; nx unreachable -> all six fall back to local; a future nx response carrying ALL six fields (simulated) -> all six prefer nx — task 4.2 |
| `render.render_session_bar`'s new glyph format | `cc-tmux self-test` cases: each of the six fields at a nonzero count renders its glyph in the correct color and position, a zero count renders nothing for that field, all-zero renders no git-status segment at all — task 4.3 |
| End-to-end row-2 render against a live pane | Live verification: real pane, real dirty/ahead/behind state, confirm the exact glyph string matches a hand-computed expectation — paste observed output — task 4.4 |
