<!-- beads:epic:if-bqw -->
<!-- beads:feature:if-mphu -->

# Tasks: cc-tmux-row3-pour-transition

> Literal `## DB/API/E2E Batch` headers per `/feature`'s wave-plan-build contract — no UI batch
> this time (unlike `cc-tmux-row3-next-cycle`, there is no `cli.py` caller-side change; this
> proposal only changes `render.py`'s internal rendering, called with `now` already set). Owner:
> general-purpose engineer agents (no dedicated api/ui roles for this Python tmux plugin). Full
> design rationale (algorithm, worked example, wiring point) in `design.md` — do not re-derive
> here.

## DB Batch

- [x] [1.1] `apps/cc-tmux/src/cc_tmux/render.py`: add module-level `POUR_FRAMES: Tuple[str, ...] = [beads:if-c9q7]
  ("▁", "▄", "▇")` (three stdlib Unicode block-height glyphs: low, mid, high) and
  `POUR_STAGGER_TICKS = 2` (extra delay, in ticks, the LAST character carries vs. the FIRST;
  earlier characters interpolate linearly — see design.md § Algorithm). Cite design.md rather
  than re-deriving the rationale inline. [owner:general-purpose] [type:ui]

## API Batch

- [x] [2.1] `apps/cc-tmux/src/cc_tmux/render.py`: add `pour_transition_text(text: str, [beads:if-s6eg]
  tick_in_phase: int) -> str` exactly per design.md § Algorithm — for each character at index
  `i` of `text` (length `n`), compute `local_progress = i / max(1, n - 1)`, `stagger =
  round(local_progress * POUR_STAGGER_TICKS)`, `char_tick = tick_in_phase - stagger`; if
  `char_tick < 0` emit `POUR_FRAMES[0]`; if `0 <= char_tick < len(POUR_FRAMES)` emit
  `POUR_FRAMES[char_tick]`; else emit the real character. Empty `text` returns `text` unchanged
  (no-op, no IndexError from the `max(1, n-1)` guard). Pure function, no tmux/subprocess, no
  randomness — deterministic given `(text, tick_in_phase)`. [owner:general-purpose] [type:api]
- [x] [2.2] `apps/cc-tmux/src/cc_tmux/render.py`: in `render_beads_bar`, at the point [beads:if-9azk]
  `phase_content` is selected (the existing `cc-tmux-row3-next-cycle` logic — either the
  `op:`/`bd:` counts string or the `next:` line, per the current phase), when `now is not None`
  compute `tick_in_phase = int((now % SWAP_PERIOD_SEC) / FRAME_PERIOD_SEC)` and reassign
  `phase_content = pour_transition_text(phase_content, tick_in_phase)` BEFORE it is assembled
  into the row's left side (i.e., before the countdown-glyph prefix is prepended — the glyph
  prefix itself is untouched by this change). Apply this identically regardless of which phase
  `phase_content` came from — do not special-case phase 0 vs phase 1. When `now is None`, this
  code path MUST NOT execute at all — `render_beads_bar`'s existing byte-identical-legacy
  contract for `now is None` stays intact. [owner:general-purpose] [type:api]

## E2E Batch

- [x] [3.1] Extend `apps/cc-tmux/src/cc_tmux/testing.py`: self-test cases for [beads:if-w6ye]
  `pour_transition_text`: at `tick_in_phase=0` every character (first, middle, last) renders as
  `POUR_FRAMES[0]`; at `tick_in_phase=1` the first character has advanced to `POUR_FRAMES[1]`
  while the last character is still at `POUR_FRAMES[0]` (confirms the left-to-right stagger
  ordering — first settles soonest); by `tick_in_phase=5` (with the shipped constants) every
  character has settled to its real value for a representative ~44-char string (reproduce the
  design.md worked-example table's exact tick-by-tick values for first/middle/last characters);
  empty-string input returns `""` at any `tick_in_phase` with no exception; a very short string
  (1-2 chars) never raises (guards the `max(1, n-1)` division). [owner:general-purpose]
  [type:testing]
- [x] [3.2] Extend `apps/cc-tmux/src/cc_tmux/testing.py`: self-test cases for the [beads:if-0azy]
  `render_beads_bar` wiring: at a `now` landing exactly on a phase-0 swap boundary
  (`tick_in_phase=0`), the rendered `op:`/`bd:` content shows the transition glyphs, not the real
  counts text, with the countdown-glyph prefix unaffected; at a `now` landing exactly on a
  phase-1 swap boundary, the `next:` line shows the IDENTICAL transition progression (same
  `POUR_FRAMES`/`POUR_STAGGER_TICKS` behavior, proving the "same motion regardless of phase"
  requirement); at a `now` several ticks past either boundary, the content has fully settled to
  the real text (matches the pre-transition reference output for that phase); `now=None` (or
  omitted) is BYTE-IDENTICAL to `render_beads_bar`'s existing pre-transition self-tests — run the
  full self-test suite and confirm every pre-existing assertion for `now=None` still passes
  unmodified. [owner:general-purpose] [type:testing]
- [x] [3.3] Run `./apps/cc-tmux/bin/cc-tmux self-test` from the repo root and paste the full [beads:if-3tgz]
  passing output (baseline before this spec was 101/101 — confirm the new count and zero
  failures). [owner:general-purpose] [type:testing]
- [x] [3.4] Live verification: re-register the plugin bindings/format in the running server via [beads:if-82fa]
  `tmux run-shell ~/.tmux/plugins/cc-tmux/cc-tmux.tmux` if needed (or use the lower-risk direct
  `bin/cc-tmux render-all <window-id>` path against a real tracked window, per this session's own
  established safe pattern for cc-tmux live verification — read-only render calls are safe, never
  send keys into or mutate an existing window/pane). Capture the REAL rendered row-3 output at
  several consecutive ticks starting at (or as close as achievable to) a real swap boundary,
  showing the visible left-to-right glyph progression settling to real text within a few ticks,
  well before the following 8s swap. Paste the captured sequence. [owner:general-purpose]
  [type:testing]
