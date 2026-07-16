<!-- beads:epic:if-bqw -->
<!-- beads:feature:if-jynx -->

# Tasks: cc-tmux-row2-model-color-usage-format

> Literal `## DB/API/E2E Batch` headers per `/feature`'s wave-plan-build contract — no UI batch
> this time (no `cli.py` call-site change; this proposal only changes `render.py`'s internal
> rendering + one new color constant in `usage.py`). Owner: general-purpose engineer agents (no
> dedicated api/ui roles for this Python tmux plugin).

## DB Batch

- [x] [1.1] `apps/cc-tmux/src/cc_tmux/usage.py`: add `LIGHT_GREEN` color constant alongside the [beads:if-eit2]
  existing `DIM`/`CYAN`/`YELLOW`/`RED`/`GREEN`/`BLUE` constants (line ~41-49) — a lighter shade
  than the existing `GREEN = "#00ac3a"`, distinct enough from both `GREEN` and `CYAN` to read as
  its own color at a glance in a tmux status bar. [owner:general-purpose] [type:api]

## API Batch

- [x] [2.1] `apps/cc-tmux/src/cc_tmux/render.py`: `render_session_bar` (line ~596-597) — replace [beads:if-wx74]
  the static `f"#[fg={CYAN}]{model_letter}"` with a color lookup keyed by model letter/name:
  Opus->`YELLOW`, Sonnet->`GREEN`, Haiku->`LIGHT_GREEN`, Fable->`RED`, falling back to `CYAN` for
  any other/empty value. Import `LIGHT_GREEN` from `usage.py` alongside the other color
  constants already imported at the top of the file. [owner:general-purpose] [type:api]
- [x] [2.2] `apps/cc-tmux/src/cc_tmux/render.py`: in the right-side format string (line [beads:if-dpmo]
  ~637-640), drop the trailing `:` from the SES label segment only
  (`f"#[fg={ses_color}]{ses_label}:#[default] "` -> no colon after `{ses_label}`) — `5H:`/`7D:`
  keep their colons unchanged. Insert exactly one space between the 7D percentage segment and
  `{usage_glyph}` (currently concatenated with zero space). Change the
  `render_usage_glyph(ses_pct, five_h_pct, seven_d_pct, n=10)` call (line ~632) to `n=20`,
  matching the accounts-popup's already-established `n=20` "wide" convention
  (`render_usage_glyph_2metric`, line ~847) — no algorithm change, just the cell-count argument.
  [owner:general-purpose] [type:api]

## E2E Batch

- [x] [3.1] Extend `apps/cc-tmux/src/cc_tmux/testing.py`'s `render_session_bar` self-tests: [beads:if-ekqa]
  assert each of Opus/Sonnet/Haiku/Fable produces its documented `#[fg=...]` color prefix on the
  model letter, and an unrecognized/empty model value falls back to `CYAN`. Assert the SES label
  segment has no trailing colon while `5H:`/`7D:` retain theirs. Assert exactly one space
  precedes `{usage_glyph}`. Assert the glyph itself is 20 characters long (was 10) by checking
  `len()` on the glyph segment, or by asserting `render_usage_glyph` is called with `n=20` if the
  test can intercept the call. [owner:general-purpose] [type:api]
- [x] [3.2] Run `python3 -m py_compile` on both touched files and the full self-test suite [beads:if-ct26]
  (`cd apps/cc-tmux && python3 -c "import sys; sys.path.insert(0,'src'); from cc_tmux.testing
  import run_self_test; sys.exit(run_self_test())"`) — confirm the total pass count increases
  and zero failures. Run `./scripts/check.sh` at the repo root and confirm it passes. Live-verify
  via `~/.tmux/plugins/cc-tmux/bin/cc-tmux render-all "$WINDOW_ID"` +
  `tmux show-options -g '@cc-row-session'` (or equivalent) that a real render shows the new
  colon-free SES label, the space before the glyph, and a visibly wider glyph run.
  [owner:general-purpose] [type:api]
