<!-- beads:epic:if-bqw -->
<!-- beads:feature:if-ef4m -->

# Tasks: cc-tmux-braille-flash-and-permission-pulse

> Literal `## DB/API/E2E Batch` headers per `/feature`'s wave-plan-build contract — no UI batch
> this time (no `cli.py` call-site change; all changes are internal to `render.py`). Owner:
> general-purpose engineer agents (no dedicated api/ui roles for this Python tmux plugin). Full
> glyph picks + exact wiring in `design.md` — do not re-derive here.

## DB Batch

- [x] [1.1] `apps/cc-tmux/src/cc_tmux/render.py`: rename `SUBAGENT_FG_1` (`"◎"` -> `"□"`) and [beads:if-hxms]
  `SUBAGENT_FG_2PLUS` (`"◉"` -> `"■"`) — these become the STATIC identity comment/reference
  only (the actual rendered values become the flash pairs in task 2.x below; keep these renamed
  constants as documentation of "what concept this represents" if useful, or remove them
  entirely if task 2.1 replaces their only use — engineer's call, cite design.md). Add the five
  new braille frame-pair constants per design.md's table: `ACTIVE_FLASH_FRAMES`,
  `SUBAGENT_FG1_FLASH_FRAMES`, `SUBAGENT_FG2PLUS_FLASH_FRAMES`, `SUBAGENT_BG1_FLASH_FRAMES`,
  `SUBAGENT_BG2PLUS_FLASH_FRAMES`, `PERMISSION_PULSE_FRAMES`. [owner:general-purpose] [type:api]

## API Batch

- [ ] [2.1] `apps/cc-tmux/src/cc_tmux/render.py`: `animated_icon` — `active` branch indexes [beads:if-iyir]
  `ACTIVE_FLASH_FRAMES` by `int(now / FRAME_PERIOD_SEC) % 2` instead of `BLOCK_FRAMES`;
  `waiting` branch indexes `PERMISSION_PULSE_FRAMES` the same way instead of `SHADE_FRAMES`.
  Remove `BLOCK_FRAMES`/`SHADE_FRAMES` if nothing else references them after this change (check
  first — cite the grep result in the commit). [owner:general-purpose] [type:api]
- [ ] [2.2] `apps/cc-tmux/src/cc_tmux/render.py`: `resolve_tab_icon` — each of the four [beads:if-1vyo]
  `return SUBAGENT_*` branches becomes `return SUBAGENT_*_FLASH_FRAMES[int(now /
  FRAME_PERIOD_SEC) % 2]` per design.md's mapping (FG>=2 -> FG2PLUS pair, FG==1 -> FG1 pair,
  BG>=2 -> BG2PLUS pair, BG==1 -> BG1 pair). Precedence order and thresholds UNCHANGED — only
  the returned value per branch changes from a static glyph to a flashing pair index.
  [owner:general-purpose] [type:api]
- [ ] [2.3] `apps/cc-tmux/src/cc_tmux/render.py`: `resolve_tab_glyph` — add a new branch: when [beads:if-bb17]
  `state == "waiting"`, compute the current permission-pulse frame and its color (`YELLOW` for
  `PERMISSION_PULSE_FRAMES[0]` i.e. `◉`, `""` for `PERMISSION_PULSE_FRAMES[1]` i.e. `◎`), and
  return `(icon, color)` instead of falling through to the generic `(resolve_tab_icon(...), "")`
  case. All other states/precedence in this function are UNCHANGED (idle-usage-meter branch
  untouched, non-waiting/non-idle-meter cases still return empty color).
  [owner:general-purpose] [type:api]

## E2E Batch

- [ ] [3.1] Extend `apps/cc-tmux/src/cc_tmux/testing.py`: self-tests for `animated_icon` [beads:if-ewn3]
  (`active` cycles `ACTIVE_FLASH_FRAMES` by tick parity, `waiting` cycles
  `PERMISSION_PULSE_FRAMES` by tick parity — both replacing their prior 4-frame/7-frame cycles),
  for `resolve_tab_icon` (all four sub-agent branches flash their own dedicated pair by tick
  parity; precedence/thresholds unchanged; no two of the four pairs share a frame), and for
  `resolve_tab_glyph` (the new `waiting` branch returns `(◉, YELLOW)` and `(◎, "")` on alternating
  ticks; the idle-usage-meter and other existing branches are unaffected).
  [owner:general-purpose] [type:api]
- [ ] [3.2] Run `python3 -m py_compile` and the full self-test suite (`cd apps/cc-tmux && [beads:if-xe10]
  python3 -c "import sys; sys.path.insert(0,'src'); from cc_tmux.testing import
  run_self_test; sys.exit(run_self_test())"`) — confirm pass count increases, zero failures.
  Run `./scripts/check.sh` at the repo root. Live-verify via
  `~/.tmux/plugins/cc-tmux/bin/cc-tmux render-all "$WINDOW_ID"` (or the tabs-row equivalent)
  captured at two wall-clock seconds apart for a pane in each of `active`/`waiting`/each
  sub-agent state, confirming the documented flash behavior and the permission pulse's YELLOW
  coloring on `◉`. [owner:general-purpose] [type:api]
