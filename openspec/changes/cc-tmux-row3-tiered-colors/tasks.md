<!-- beads:epic:if-bqw -->
<!-- beads:feature:if-ae2s -->

# Tasks: cc-tmux-row3-tiered-colors

> Literal `## DB/API/E2E Batch` headers per `/feature`'s wave-plan-build contract — no UI batch
> this time (no `tmux.conf.tmpl`/theme-file changes; everything lives in `render.py`/`cli.py`).
> Owner: general-purpose engineer agents (no dedicated api/ui roles for this Python tmux plugin).

## DB Batch

- [x] [1.1] `apps/cc-tmux/src/cc_tmux/render.py`: remove `BEADS_UNARCHIVED_HIGH`, [beads:if-o0yh]
  `BEADS_BLOCKED_HIGH`, `SWAP_PERIOD_SEC`, `_COUNTDOWN_RAMP` (confirm via grep that nothing else
  in the codebase references any of these four before deleting — design.md already confirms zero
  other references, cite that grep result in the commit). Add the six new per-label threshold
  constants per design.md's exact values: `OP_YELLOW_MIN=6`, `OP_PULSE_MIN=11`, `OP_RED_MIN=21`,
  `BD_YELLOW_MIN=11`, `BD_PULSE_MIN=21`, `BD_RED_MIN=41`. [owner:general-purpose] [type:api]

## API Batch

- [x] [2.1] `apps/cc-tmux/src/cc_tmux/render.py`: remove `beads_bar_phase` and [beads:if-27j1]
  `beads_bar_countdown_glyph` entirely. Remove `_threshold_color`, replacing it with
  `_tiered_color(n, yellow_min, pulse_min, red_min, now)` per design.md's exact implementation
  (DIM below yellow_min, YELLOW below pulse_min, pulsing YELLOW<->DIM on
  `int(now / FRAME_PERIOD_SEC) % 2` below red_min — steady YELLOW when `now is None` — RED at/above
  red_min). [owner:general-purpose] [type:api]
- [x] [2.2] `apps/cc-tmux/src/cc_tmux/render.py`: restructure `_pulse_segment` per design.md's [beads:if-m1dz]
  exact new signature — replace the single `high: int` param with `yellow_min: int, pulse_min:
  int, red_min: int, now: Optional[float]`, and color ALL THREE numbers (`n1`/`n2`/`n3`)
  independently via `_tiered_color`, not just `n3`. Update both call sites in
  `render_beads_bar` (the `op`/`bd` segment builds) to pass their own threshold triple (`op` uses
  `OP_YELLOW_MIN`/`OP_PULSE_MIN`/`OP_RED_MIN`; `bd` uses `BD_YELLOW_MIN`/`BD_PULSE_MIN`/
  `BD_RED_MIN`) plus `now`. [owner:general-purpose] [type:api]
- [x] [2.3] `apps/cc-tmux/src/cc_tmux/render.py`: `render_beads_bar` — remove the `next_text` [beads:if-jway]
  parameter and the entire `if now is None: ... else: phase = beads_bar_phase(now); ...` branch.
  The left side is always the `op:`/`bd:` `counts_left` content (or `""` if neither half is
  present) — no phase selection, no countdown glyph. `now: Optional[float] = None` stays as a
  parameter (now feeding `_pulse_segment`'s pulse-tier animation instead of phase selection).
  [owner:general-purpose] [type:api]
- [x] [2.4] `apps/cc-tmux/src/cc_tmux/cli.py`: remove `_parse_roadmap_pulse_next` entirely (grep [beads:if-2ad7]
  first to confirm no other caller — design.md already confirms this, cite the grep result) and
  its call site in `_build_beads_bar` (the `next_text = _parse_roadmap_pulse_next(content)` line
  and the `next_text=next_text` kwarg in the `render.render_beads_bar(...)` call). Keep
  `now=time.time()` in that same call — still required for the pulse-tier animation.
  [owner:general-purpose] [type:api]

## E2E Batch

- [x] [3.1] `apps/cc-tmux/src/cc_tmux/testing.py`: remove [beads:if-g2na]
  `_test_render_beads_bar_phase_and_countdown_glyph` and `_test_render_beads_bar_next_cycle`
  entirely (they test functions/behavior deleted by tasks 2.1/2.3), including their `_TESTS`
  registrations. [owner:general-purpose] [type:api]
- [x] [3.2] `apps/cc-tmux/src/cc_tmux/testing.py`: rewrite `_test_render_beads_bar` and [beads:if-zmx0]
  `_test_render_beads_bar_account_segment`'s expected-string assertions to match the new
  per-number-colored shape (every count now carries its own `#[fg=...]` wrap, including
  DIM-tier counts that previously rendered bare — see design.md's "Existing tests requiring
  rewrite" section for the exact new shape). Do NOT delete these tests — their non-color
  assertions (segment presence/absence, `_BEADS_SEP` separator, staleness age markers, fail-open
  on partial/all-None data) still apply unchanged, only the literal expected strings change.
  [owner:general-purpose] [type:api]
- [x] [3.3] `apps/cc-tmux/src/cc_tmux/testing.py`: add new tests for `_tiered_color` covering [beads:if-gbyn]
  every boundary for both label's thresholds — `op` at 5/6/10/11/20/21, `bd` at 10/11/20/21/40/41
  — asserting the exact tier (DIM/YELLOW/pulse-tick-parity/RED) at each value. Add a dedicated
  pulse-animation test: same n (in the pulse range) at two `now` values one `FRAME_PERIOD_SEC`
  apart resolves to alternating YELLOW/DIM: and `now=None` at a pulse-range n resolves to steady
  YELLOW (never animates without a real `now`). Add a same-raw-number-different-label test (e.g.
  15 in `op:`'s pulse range vs 15 in `bd:`'s YELLOW range) proving thresholds are genuinely
  independent per label, not shared. [owner:general-purpose] [type:api]
- [x] [3.4] Run `python3 -m py_compile` on all three touched files and the full self-test suite [beads:if-xl97]
  (`cd apps/cc-tmux && python3 -c "import sys; sys.path.insert(0,'src'); from cc_tmux.testing
  import run_self_test; sys.exit(run_self_test())"`) — confirm zero failures (pass count may
  decrease slightly since 2 whole tests are removed in 3.1, then increase again from 3.3's new
  tests; report both numbers, don't just check "increased"). Run `./scripts/check.sh` at the
  repo root. Live-verify via `~/.tmux/plugins/cc-tmux/bin/cc-tmux render-all "$WINDOW_ID"` +
  `tmux show-options -g '@cc-row-beads'`, captured at two wall-clock seconds apart, confirming
  `op:`/`bd:` render every capture (never `next:`) and that if any current real count falls in
  either label's pulse range, it visibly alternates color between the two captures.
  [owner:general-purpose] [type:api]
