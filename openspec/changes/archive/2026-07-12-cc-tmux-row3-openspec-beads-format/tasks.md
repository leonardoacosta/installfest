<!-- beads:feature:if-7fsr -->

<!-- beads:epic:if-bqw -->

# Tasks: cc-tmux-row3-openspec-beads-format

> Literal `## DB/API/E2E Batch` headers per `/feature`'s wave-plan-build contract — mapped by
> domain fit (DB = the roadmap-pulse data-producer change in the cc repo; API = the cc-tmux
> reader/render wiring; E2E = tests + live verification). No UI batch — no tmux.conf/theme
> surface changes. Owner: general-purpose engineer agents.

## DB Batch

- [x] [1.1] [P-1] `~/dev/cc/scripts/bin/roadmap-pulse`: add a standalone-beads computation to
  `--line` mode — query `bd ready --json` and `bd blocked --json`, filter out any issue that is a
  transitive descendant (via `parent-child` dep) of an issue whose title starts with `[SPEC]` or
  `[CAPABILITY]`, and emit `standalone_ready`/`standalone_blocked` counts. [owner:general-purpose] [type:api] [beads:if-hgix]
- [x] [1.2] [P-1] `~/dev/cc/scripts/bin/roadmap-pulse`: `--line` mode text output becomes two
  lines — `openspec: {open} open, {unarchived} unarchived` and `beads: {ready} ready, {blocked}
  blocked` — replacing the current single `"N open, M unarchived"` line. Confirm the already-shipped
  commit `88d0558e` radar-token removal is still intact (no regression) as part of this edit.
  [owner:general-purpose] [type:api] [beads:if-svr3]

## API Batch

- [x] [2.1] [P-1] `apps/cc-tmux/src/cc_tmux/cli.py` (`_read_roadmap_pulse` or its caller): strip
  any line starting with `radar:` the same way `next:` lines are already stripped — defensive,
  independent of whether the producer-side fix has landed on a given cache file.
  [owner:general-purpose] [type:api] [beads:if-wp6c]
- [x] [2.2] [P-1] `apps/cc-tmux/src/cc_tmux/cli.py` (`_build_beads_bar`): parse the new two-line
  `openspec: ...` / `beads: ...` cache content into their component counts (open, unarchived,
  ready, blocked) for `render_beads_bar` to consume; missing/malformed beads line degrades to
  omitting that half only (fail-open), matching the row's existing "no cache -> empty" contract.
  [owner:general-purpose] [type:api] [beads:if-w1he]
- [x] [2.3] [P-1] `apps/cc-tmux/src/cc_tmux/render.py` (`render_beads_bar`): reformat to
  `openspec: {open} open {unarchived} unarchived ({age}) | beads: {ready} ready {blocked} blocked
  ({age})`, with each numeric value coloured by semantic threshold (DIM healthy, YELLOW when
  `unarchived > 0` or `standalone_blocked > 0`, RED above a documented high-count threshold — pick
  a concrete threshold value and document it as a named constant, mirroring
  `BEADS_STALE_AFTER_SEC`'s existing documented-constant convention). Independent per-segment
  `(<age>)` marker on each half (both currently read the same file's single mtime — forward
  compatible with a future cache split). [owner:general-purpose] [type:api] [beads:if-1tua]

## E2E Batch

- [x] [3.1] [P-1] Extend `apps/cc-tmux/src/cc_tmux/testing.py`: self-test cases for the `radar:`
  strip, the two-line parse (including malformed/missing-beads-line fail-open), and
  `render_beads_bar`'s new format + threshold coloring (0/nonzero unarchived, 0/nonzero blocked).
  Run `cc-tmux self-test` and paste the passing stdout. [owner:general-purpose] [type:testing] [beads:if-t7yn]
- [x] [3.2] [P-1] Manual test of the `roadmap-pulse` standalone-beads filter: create a throwaway
  bead parented under an existing `[SPEC]`/`[CAPABILITY]` epic and one with no epic parent; run
  `roadmap-pulse --line` and confirm only the unparented bead counts toward
  `standalone_ready`/`standalone_blocked` — paste the observed output, then clean up the
  throwaway beads. [owner:general-purpose] [type:testing] [beads:if-0jyr]
- [x] [3.3] [P-1] Live verification: after both repos deploy, observe row 3 render
  `openspec: N open M unarchived (age) | beads: R ready B blocked (age)` with real numbers and no
  `radar:` text anywhere — paste observed output. [owner:general-purpose] [type:testing] [beads:if-8t3p]
