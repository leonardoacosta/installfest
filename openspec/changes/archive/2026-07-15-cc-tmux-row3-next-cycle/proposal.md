---
status: draft
order: 0715a
---

# Proposal: cc-tmux-row3-next-cycle

## Why

Row 3 (`_build_beads_bar` / `render_beads_bar` — the row Leo's own mental model calls "row 2",
confirmed convention per `archive/2026-07-14-cc-tmux-status-bar-popup-polish`'s exploration
notes) today renders ONLY the `op: ... | bd: ...` counts, always-on, every tick. The project's
"what should I do next" recommendation (`next: ...`) already exists — it's the first line of the
exact same `~/.claude/scripts/state/roadmap-pulse.<code>.line` cache file `_read_roadmap_pulse`
already reads for the `op:`/`bd:` counts — but it's currently invisible on the tmux status bar
entirely (only visible inside the CC pane itself, via `nexus-statusline`'s `getRoadmapPulse()`
reading that same file independently).

Leo's ask (`/openspec:explore`, this session): cycle row 3 between the two, on a wall-clock
timer, with a countdown glyph showing time-to-swap — instead of adding a 4th physical row.

**This directly reverses a scenario-tested exclusion in the current committed spec.** The
shipped requirement "A dedicated tmux status row surfaces open/ready beads and proposals"
(`openspec/specs/cc-tmux/spec.md:613-615,653-658`) explicitly states "Any line in the cache
starting with `next:` or `radar:` SHALL NOT be rendered on this row" and carries a dedicated
scenario, "a stray next: or radar: line never renders", whose entire purpose is guaranteeing
`next:` is filtered OUT. That exclusion was deliberate (`if-bqw.1` / cc commit `b6b9a234`) — this
proposal does not silently relax it; it replaces "never render `next:`" with "render `next:` half
the time, on a visible countdown, never both at once." The `radar:` exclusion is UNCHANGED —
`radar:stale` lines (a producer-side leftover token, per `_read_roadmap_pulse`'s own docstring)
still never render on this row, in either cycle phase.

**No new fetch, no cross-repo call.** Both `cc-tmux`'s row-3 reader and `nexus-statusline`'s
`getRoadmapPulse()` already read the identical on-disk file
(`~/.claude/scripts/state/roadmap-pulse.<code>.line`) independently — confirmed by direct
inspection of both source trees and the live cache file content (`if` project's cache is
literally two lines: `next: [WORKSPACE-CMDCENTER] Wor...` then `bd: 0o 0r 0b`). `cc-tmux` already
has the full file content in hand via `_read_roadmap_pulse` for the counts parse; this proposal
only adds a second pure-function parse of the SAME already-fetched string, never a new read path
or a call into `nexus-statusline`/nx-agent.

**Animation reuses the existing daemon-free cadence, not a new timer.** `render.py`'s
`animated_icon`/`idle_usage_meter` already derive their frame purely from
`int(now / FRAME_PERIOD_SEC)`, re-evaluated on tmux's own `status-interval` re-render tick — the
plugin's established "daemon-free" invariant (`tmux.py`'s own docstring). This proposal adds one
more wall-clock-keyed pure function in that same family; it introduces no background process, no
timer, no new tmux hook.

**Explicitly NOT in scope** (separately root-caused during the same exploration, tracked as
independent bugs, NOT fixed here):
- Row 2's (session bar's) model-family letter renders blank today because the write-side data
  path is dead: `telemetry.sh` forwards `model=${CLAUDE_MODEL:-}` to nx-agent on
  `session_start`/heartbeat, but `CLAUDE_MODEL` is never assigned anywhere — confirmed via a live
  empty `env` read, zero assignment sites across `~/dev/cc` + this repo, and zero occurrences in
  the installed `claude` binary's strings (the binary only emits a `model` object on the
  `StatusLine` hook event, never on `SessionStart`/heartbeat-class events). This is a `nexus` +
  `cc` cross-repo fix, unrelated to row 3's rendering logic.
- `nexus-statusline`'s row-1 model+effort token (inside the CC pane, not the tmux status bar)
  already renders correctly but carries no label prefix, making it easy to overlook next to
  labeled segments like `CTX`/`5H`/`7D`. A `nexus`-repo-only cosmetic fix.

## What Changes

- **`apps/cc-tmux/src/cc_tmux/render.py`**: add `SWAP_PERIOD_SEC = 8.0` and a countdown-glyph
  helper reusing the existing `IDLE_METER_RAMP[8:16]` drain-half glyphs (⣿⢿⠿⠻⠛⠙⠉⠈) — no new
  glyph table, per the reuse-before-reinvention rule. Add `beads_bar_phase(now) -> int` (0 =
  counts, 1 = next) and `beads_bar_countdown_glyph(now) -> str`, both pure functions of `now`.
  Extend `render_beads_bar` to accept an optional `next_text: Optional[str]` and `now: float`
  parameter; when `next_text` is present, alternate the row's LEFT-flowing content between
  today's `op:`/`bd:` segments (phase 0) and the `next_text` line (phase 1), each prefixed with
  the countdown glyph. The right-aligned account-identity segment is UNCHANGED — it renders
  every tick regardless of phase, in both the old and new behavior.
- **`apps/cc-tmux/src/cc_tmux/cli.py`**: add `_parse_roadmap_pulse_next(content) -> Optional[str]`
  — extracts the line starting with `next:` from the SAME `content` string
  `_parse_roadmap_pulse_counts` already parses (no new fetch). `_build_beads_bar` calls it and
  passes the result plus `time.time()` through to `render_beads_bar`.
- **`openspec/specs/cc-tmux/spec.md`**: MODIFIED delta for "A dedicated tmux status row surfaces
  open/ready beads and proposals" — replaces the "next: SHALL NOT render" clause and its scenario
  with the cycling contract (phase selection, countdown glyph, `radar:` still always excluded).

## Non-Goals

- No change to the `op:`/`bd:` count computation, coloring, or staleness-age logic — only WHEN
  that content is visible changes, not what it contains.
- No change to the right-aligned account-identity segment or its click/popup binding.
- No new physical status row — stays at 3 lines total, continuing the
  `2026-07-11-cc-tmux-bar-cleanup` decision to trim back to 3.
- No fix to the row-2 model-letter pipeline (`CLAUDE_MODEL` phantom env var) — tracked separately.
- No fix to `nexus-statusline`'s row-1 model-token label — tracked separately, `nexus` repo.
- No change to `nexus-statusline`'s own `getRoadmapPulse()` or its trailing-line rendering inside
  the CC pane — this proposal only adds a second, independent reader of the same cache file.

## Context

- Related: `openspec/changes/archive/2026-07-14-cc-tmux-idle-tab-usage-meter/` — most recent
  prior change establishing the wall-clock-driven, daemon-free animation pattern
  (`FRAME_PERIOD_SEC`, `int(now / period)` phase selection) this proposal extends to row 3, and
  the DB/API/UI/E2E batch-mapping convention for this Python-plugin, no-traditional-layers repo
  (used as this proposal's `tasks.md` structural template).
- Related: `openspec/changes/archive/2026-07-14-cc-tmux-status-bar-popup-polish/` — established
  that Leo's own "row 2" refers to the beads/openspec row (code's row 3), and moved the account
  identity segment onto that row; this proposal's cycling logic must not disturb that segment.
- Related, separately filed (not fixed here): cc-tmux row-2 model-letter pipeline dead end
  (`CLAUDE_MODEL` env var never assigned) and nexus-statusline row-1 model-token label gap — both
  to be filed as beads issues in their owning repos, out of this proposal's scope.
- touches: `apps/cc-tmux/src/cc_tmux/render.py`, `apps/cc-tmux/src/cc_tmux/cli.py`,
  `apps/cc-tmux/src/cc_tmux/testing.py`, `openspec/specs/cc-tmux/spec.md`

## Testing

| Seam | Coverage |
| --- | --- |
| `beads_bar_phase(now)` / `beads_bar_countdown_glyph(now)` | Unit self-tests in `testing.py` — phase flips at exact `SWAP_PERIOD_SEC` boundaries, glyph indexes sweep the 8-frame drain ramp |
| `_parse_roadmap_pulse_next(content)` | Unit self-tests — extracts `next:` line verbatim, `None` when absent, ignores `radar:`/`bd:`/`op:` lines |
| `render_beads_bar` phase-gated content | Unit self-tests — phase 0 renders unchanged `op:`/`bd:` output byte-identical to today; phase 1 renders `next_text` + countdown glyph, never both; account segment renders in both phases |
| End-to-end live render | Live verification task: re-register plugin bindings, capture the real rendered row-3 status format across a full `SWAP_PERIOD_SEC` cycle (at least one full phase-0-to-phase-1-to-phase-0 swap), paste captured output for both phases |
