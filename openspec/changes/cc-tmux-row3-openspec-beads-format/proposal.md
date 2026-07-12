---
status: draft
---

# Proposal: cc-tmux-row3-openspec-beads-format

## Why

Row 3 currently shows raw `roadmap-pulse.<code>.line` cache content verbatim (minus any `next:`
line). Two problems, confirmed live via `/openspec:explore`:

1. **Stale "radar:" text.** The visible `radar:stale (1d)` on this machine is NOT current
   behavior — `~/dev/cc` commit `88d0558e` ("fix(roadmap-pulse): full-word --line counts, drop
   radar:stale token") already removed the `radar:` line from `--line` mode output. The cache
   files on this machine (`roadmap-pulse.if.line`, `roadmap-pulse.tc.line`) are simply 1-2 days
   stale, written before that fix. cc-tmux's row-3 reader has no defense against a stray
   `radar:`-prefixed line reappearing in a future stale/rolled-back cache — it only strips
   `next:` lines today.
2. **No beads signal at all.** `roadmap-pulse --line` has never emitted a beads count — it's
   purely OpenSpec-derived (`N open, M unarchived`). The user wants a beads-open signal
   alongside it, refined during requirements clarification to: **ready-count and blocked-count,
   separately, and only for beads NOT already tracked under an OpenSpec proposal** (so this
   number is additive to the openspec count, not a redundant re-count of the same
   feature/task beads the openspec segment already represents).

## What Changes

- **`~/dev/cc/scripts/bin/roadmap-pulse`** (companion cross-repo change — same "cross-repo
  scoping decision" precedent already used by `2026-07-11-cc-tmux-session-usage-bars`, which
  touched `~/dev/personal/nexus` files directly from a cc-tmux proposal): `--line` mode gains a
  standalone-beads computation. "Standalone" = a bead that is NOT a transitive descendant (via
  `parent-child` dep) of any issue whose title starts with `[SPEC]` or `[CAPABILITY]` — i.e. not
  part of the 3-level OpenSpec epic->feature->task hierarchy (`rules/BEADS.md` § Hierarchy).
  Computed via `bd ready --json` + `bd blocked --json`, filtered by walking each result's
  ancestor chain. Output shape (JSON, consumed by both nexus-statusline and cc-tmux):
  `{"standalone_ready": N, "standalone_blocked": M}` alongside the existing openspec fields.
  `--line` mode's text output becomes two lines, matching the existing "each gets its own line"
  convention rather than squeezing everything onto one:
  ```
  openspec: {open} open {unarchived} unarchived
  beads: {ready} ready {blocked} blocked
  ```
  (the `radar:` line is already gone per commit `88d0558e` — nothing further needed there beyond
  confirming it stays gone).
- **`apps/cc-tmux/src/cc_tmux/render.py`** (`render_beads_bar`): reformat to the target template
  `openspec: {open} open {unarchived} unarchived ({age}) | beads: {ready} ready {blocked}
  blocked ({age})`, with **independent per-segment staleness ages** (each half of the joined
  line gets its own trailing `(<duration>)` marker off the cache file's single mtime — both
  halves share the same file today, so both ages will read identically until/unless the two
  counts ever move to separate cache files; the per-segment marker is forward-compatible with
  that split without a render.py change). Numbers are colored by semantic threshold, matching
  every other threshold-colored value in this codebase (`usage.py`'s `color_for`, `render.py`'s
  dirty/ahead `YELLOW` markers): `unarchived > 0` (closure debt) -> YELLOW; `standalone_blocked >
  0` -> YELLOW; either count large (>= a documented threshold) -> RED; otherwise DIM/CYAN for a
  healthy zero/low count.
- **`apps/cc-tmux/src/cc_tmux/cli.py`** (`_read_roadmap_pulse` / `_build_beads_bar`): defensively
  strip any line starting with `radar:` the same way `next:` lines are already stripped — belt
  and suspenders against a rolled-back/older cache, independent of whether the producer-side fix
  has landed. Parse the new `beads` line alongside the existing `openspec` counts line.

## Non-Goals

- No new tmux status row, no new cache file, no new polling/daemon — this reads and extends the
  SAME `roadmap-pulse.<code>.line` cache row 3 already reads (matches the existing Requirement's
  "No new data production mechanism SHALL be introduced for this row").
- No change to how `next:`-line selection/precedence works elsewhere in roadmap-pulse (`--json`/
  `--digest` modes, the `NEXT_F` precedence chain) — only `--line` mode's beads/openspec counts
  output changes.
- The standalone-beads filter does not attempt to handle beads reparented mid-flight (e.g. from
  `unsorted-epic` to a real capability) any differently than `beads:spec-sync`'s existing
  reparenting flow already does — this proposal reads the CURRENT dependency graph at render
  time, it doesn't need its own reparenting logic.

## Context

- touches: `apps/cc-tmux/src/cc_tmux/cli.py`, `apps/cc-tmux/src/cc_tmux/render.py`,
  `openspec/specs/cc-tmux/spec.md`, and (different repo, absolute path, same cross-repo scoping
  precedent as `2026-07-11-cc-tmux-session-usage-bars`) `~/dev/cc/scripts/bin/roadmap-pulse`
- Related: extends the `[CAPABILITY] cc-tmux` epic (`if-bqw`). Depends on the already-shipped
  `~/dev/cc` commit `88d0558e` (radar-token removal) as prior art, not a blocking dependency of
  this proposal's own tasks. No file overlap with `cc-tmux-active-pane-resolution` (that proposal
  touches `cli.py`'s `_build_session_bar`/row 2; this one touches `cli.py`'s `_build_beads_bar`/
  row 3) — both may land in the same wave without conflict, though both editing `cli.py` means a
  wave-plan conflict-matrix check should still confirm no line-range collision before parallel
  dispatch.
- Origin: `/openspec:explore` session, 2026-07-12, refined via this `/feature` invocation's
  Phase 2 clarifying questions (beads scope = ready+blocked, standalone-only; color semantics =
  threshold-based).

## Testing

| Seam | Coverage |
| --- | --- |
| `roadmap-pulse` standalone-beads filter (cc repo) | Manual/self-contained script test: a bead parented under a `[SPEC]` epic is excluded; a bead with no epic parent (or parented under `[CAPABILITY]` directly, no intervening `[SPEC]`) is included — task 1.2 |
| `_read_roadmap_pulse` radar-line strip (cc-tmux) | `cc-tmux self-test` case: a cache file containing a `radar:` line has it stripped, matching the existing `next:`-strip test shape — task 2.2 |
| `render_beads_bar()` new format + coloring (cc-tmux) | `cc-tmux self-test` cases: zero/nonzero unarchived and blocked counts each render the correct DIM/YELLOW/RED threshold color; independent per-segment age markers render correctly — task 2.3 |
| End-to-end row 3 | Live verification: after both repos deploy, observe row 3 render `openspec: N open M unarchived (age) \| beads: R ready B blocked (age)` with real numbers, no `radar:` text anywhere — paste observed output — task 3.1 |
