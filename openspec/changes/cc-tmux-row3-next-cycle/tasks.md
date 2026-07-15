<!-- beads:epic:if-bqw -->
<!-- beads:feature:if-14vj -->

# Tasks: cc-tmux-row3-next-cycle

> Literal `## DB/API/UI/E2E Batch` headers per `/feature`'s wave-plan-build contract — mapped by
> domain fit (DB = new constant + glyph-ramp slice + phase/countdown pure functions; API =
> `_parse_roadmap_pulse_next` + `render_beads_bar`'s extended signature; UI = `_build_beads_bar`
> plumbing; E2E = self-tests + live verification). Owner: general-purpose engineer agents (no
> dedicated api/ui roles for this Python tmux plugin). Full design rationale (phase math,
> countdown-ramp reuse, next-line parse, byte-identical `now=None` fallback) in `design.md` — do
> not re-derive here.

## DB Batch

- [x] [1.1] `apps/cc-tmux/src/cc_tmux/render.py`: add module-level `SWAP_PERIOD_SEC = 8.0` beside [beads:if-7ulw]
  the existing `FRAME_PERIOD_SEC`, and `_COUNTDOWN_RAMP: Tuple[str, ...] = IDLE_METER_RAMP[8:16]`
  (the existing drain-half glyphs `⣿ ⢿ ⠿ ⠻ ⠛ ⠙ ⠉ ⠈` — no new glyph table, see design.md § Countdown
  glyph). Cite design.md rather than re-deriving the ramp slice inline. [owner:general-purpose]
  [type:ui]

## API Batch

- [x] [2.1] `apps/cc-tmux/src/cc_tmux/render.py`: add `beads_bar_phase(now: float) -> int` [beads:if-lf3a]
  returning `int(now / SWAP_PERIOD_SEC) % 2` (0 = counts, 1 = next), and
  `beads_bar_countdown_glyph(now: float) -> str` returning `_COUNTDOWN_RAMP[idx]` where
  `idx = min(7, int((now % SWAP_PERIOD_SEC) / SWAP_PERIOD_SEC * 8))` — both pure functions of
  `now`, no tmux/subprocess. [owner:general-purpose] [type:ui]
- [x] [2.2] `apps/cc-tmux/src/cc_tmux/cli.py`: add `_parse_roadmap_pulse_next(content: str) -> [beads:if-oc8u]
  Optional[str]` — returns the first line in `content` starting with `"next:"`, or `None` if no
  such line exists. Operates on the SAME `content` string `_parse_roadmap_pulse_counts` already
  receives from `_read_roadmap_pulse` — no new fetch, no new cache file. `radar:` lines are
  already stripped upstream by `_read_roadmap_pulse` before `content` reaches this function; do
  not re-implement that stripping here. [owner:general-purpose] [type:api]
- [x] [2.3] `apps/cc-tmux/src/cc_tmux/render.py`: extend `render_beads_bar`'s signature with two [beads:if-82ih]
  new optional trailing parameters, `next_text: Optional[str] = None` and `now: Optional[float] =
  None`. `now is None` (the default) MUST render byte-identical to today's exact output — no
  phase logic engages, no countdown glyph appears (protects existing callers/tests that don't
  pass the new params). When `now` is provided: `beads_bar_phase(now) == 0`, or `next_text is
  None`, renders today's `op:`/`bd:` segments prefixed with `beads_bar_countdown_glyph(now)`;
  `beads_bar_phase(now) == 1` with `next_text` present renders `next_text` ALONE (no `op:`/`bd:`)
  as the left-flowing content, also prefixed with the countdown glyph. The right-aligned
  account-identity segment is UNCHANGED — renders in both phases, independent of the cycle, exact
  same `#[align=right]`/`#[range=user|accounts]` composition as today. [owner:general-purpose]
  [type:api]

## UI Batch

- [ ] [3.1] `apps/cc-tmux/src/cc_tmux/cli.py`: in `_build_beads_bar`, after computing `content, [beads:if-djsl]
  age_sec = _read_roadmap_pulse(pane)` and the existing counts parse, call `next_text =
  _parse_roadmap_pulse_next(content)` and pass `next_text=next_text, now=time.time()` through to
  `render.render_beads_bar(...)` alongside the existing arguments. Update the function's
  docstring to describe the new phase-cycling behavior (see the docstring template in
  `render_beads_bar`'s own updated docstring for the phrasing convention). [owner:general-purpose]
  [type:ui]

## E2E Batch

- [ ] [4.1] Extend `apps/cc-tmux/src/cc_tmux/testing.py`: self-test cases for `beads_bar_phase` + [beads:if-a58k]
  `beads_bar_countdown_glyph`: phase flips exactly at `SWAP_PERIOD_SEC` boundaries (e.g. `now=7.9`
  -> phase 0, `now=8.0` -> phase 1, `now=15.9` -> phase 1, `now=16.0` -> phase 0); countdown glyph
  sweeps all 8 `_COUNTDOWN_RAMP` frames across one full `SWAP_PERIOD_SEC` window and wraps
  correctly at the phase boundary (glyph resets to frame 0 at the start of each new phase, not
  just each new `SWAP_PERIOD_SEC` multiple). [owner:general-purpose] [type:testing]
 
- [ ] [4.2] Extend `apps/cc-tmux/src/cc_tmux/testing.py`: self-test cases for [beads:if-4hm4]
  `_parse_roadmap_pulse_next`: extracts a `next:` line verbatim from multi-line content
  regardless of line position; returns `None` when no `next:` line is present; ignores `bd:`/
  `op:`/`radar:` lines (does not mistake them for `next:`). [owner:general-purpose] [type:testing]
 
- [ ] [4.3] Extend `apps/cc-tmux/src/cc_tmux/testing.py`: self-test cases for `render_beads_bar`'s [beads:if-uh8x]
  extended contract: `now=None` (or omitted) renders byte-identical to a captured pre-change
  reference string for the same counts/account inputs; `now` at phase 0 renders counts + countdown
  glyph; `now` at phase 1 with `next_text` set renders ONLY `next_text` + countdown glyph (no
  `op:`/`bd:` anywhere in the output); `now` at phase 1 with `next_text=None` falls back to
  phase-0 content; account segment renders identically across both phases and both `now`/`None`
  modes; "nothing available" (`no counts`, `next_text=None`, `account_label=""`) still returns
  `""` in every phase. Run `cc-tmux self-test` and paste the passing stdout showing zero failures
  overall (update any pre-existing beads-bar assertions that legitimately changed shape rather
  than deleting or skipping them). [owner:general-purpose] [type:testing]
- [ ] [4.4] Live verification: re-register the plugin bindings/format in the running server via [beads:if-18rd]
  `tmux run-shell ~/.tmux/plugins/cc-tmux/cc-tmux.tmux`, then capture the REAL rendered row-3
  status format across a full cycle — at least one phase-0 capture and one phase-1 capture,
  ideally straddling an actual swap (e.g. capture, wait past `SWAP_PERIOD_SEC`, capture again) —
  via `tmux capture-pane`/status-format interrogation, NOT `display-message -F` alone (which never
  runs `#()` job commands, per this session's own ground-truthing of that gotcha). Confirm the
  countdown glyph visibly changes between captures within a phase, and that the left-side content
  actually swaps from `op:`/`bd:` to `next:` and back across the boundary. Paste both captured
  outputs. [owner:general-purpose] [type:testing]
