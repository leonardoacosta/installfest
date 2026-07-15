# Design: cc-tmux-row3-pour-transition

## Why hand-rolled, not `terminaltexteffects`

`apps/cc-tmux/pyproject.toml` states `dependencies = []` with the comment "No runtime
dependencies: stdlib-only by design (Python 3.10+ on every target machine)". `terminaltexteffects`
(TTE) — the library used for this session's throwaway demo — is MIT-licensed, zero-transitive-deps,
and actively maintained, but adopting it would still be the first exception to that stated
invariant for the whole plugin. `render.py` already has three precedents for exactly this kind of
small, pure, stdlib-only animation function (`animated_icon`, `idle_usage_meter`,
`beads_bar_countdown_glyph`) — this extends that pattern rather than diverging from it. Leo chose
hand-rolled over the dependency explicitly (`/openspec:explore` AskUserQuestion, this session).

## Algorithm

```python
POUR_FRAMES: Tuple[str, ...] = ("▁", "▄", "▇")  # low, mid, high — stdlib Unicode block elements
POUR_STAGGER_TICKS = 2  # extra delay (in ticks) the LAST character carries vs. the FIRST

def pour_transition_text(text: str, tick_in_phase: int) -> str:
    """`text` with each character replaced by its POUR_FRAMES glyph, or the
    real character once that position has "settled" — a left-to-right wave,
    first character settling soonest, last character settling
    POUR_STAGGER_TICKS ticks later. Bounded duration regardless of len(text):
    settles fully within len(POUR_FRAMES) - 1 + POUR_STAGGER_TICKS + 1 ticks
    (5, with the frames/stagger above) of `tick_in_phase` reaching that value.
    Empty text -> empty text (no-op).
    """
    if not text:
        return text
    n = len(text)
    out = []
    for i, ch in enumerate(text):
        local_progress = i / max(1, n - 1)  # 0.0 (first char) .. 1.0 (last char)
        stagger = round(local_progress * POUR_STAGGER_TICKS)
        char_tick = tick_in_phase - stagger
        if char_tick < 0:
            out.append(POUR_FRAMES[0])
        elif char_tick < len(POUR_FRAMES):
            out.append(POUR_FRAMES[char_tick])
        else:
            out.append(ch)
    return "".join(out)
```

`tick_in_phase` is computed at the call site as `int((now % SWAP_PERIOD_SEC) / FRAME_PERIOD_SEC)`
— the same wall-clock-tick idiom `animated_icon` already uses (`int(now / FRAME_PERIOD_SEC) %
len(frames)`), just measuring elapsed ticks *since the most recent phase boundary* instead of a
repeating cycle. `now % SWAP_PERIOD_SEC` is already computed for `beads_bar_countdown_glyph` —
this is the second consumer of that same value, not a new derivation.

## Worked example (`text` = 44 chars, `"next: [WORKSPACE-CMDCENTER] Ship the thing"`)

| `tick_in_phase` | first char (i=0, stagger=0) | middle char (i≈21, stagger=1) | last char (i=43, stagger=2) |
|---|---|---|---|
| 0 | `▁` | `▁` | `▁` |
| 1 | `▄` | `▁` | `▁` |
| 2 | `▇` | `▄` | `▁` |
| 3 | real char | `▇` | `▄` |
| 4 | real char | real char | `▇` |
| 5 | real char | real char | real char |

The wave visibly sweeps left to right over 5 ticks (~5 seconds at `FRAME_PERIOD_SEC=1.0`) — fixed,
independent of line length, and well inside half of `SWAP_PERIOD_SEC` (8s), so it always fully
settles before the next swap and never straddles two swaps.

## Wiring into `render_beads_bar`

At the point `render_beads_bar` currently selects `phase_content` (either the `op:`/`bd:` counts
or the `next:` line, per the existing `cc-tmux-row3-next-cycle` logic), when `now is not None`:

```python
tick_in_phase = int((now % SWAP_PERIOD_SEC) / FRAME_PERIOD_SEC)
phase_content = pour_transition_text(phase_content, tick_in_phase)
```

This runs identically regardless of which phase `phase_content` came from — the SAME transition
applies whether the newly-visible line is the counts or the `next:` line, matching the approved
demo's "same motion regardless of which line" principle. `now is None` (legacy/default) never
calls `pour_transition_text` at all — byte-identical to today's pre-transition output, same
protection the existing `now is None` contract already provides.

The countdown glyph prefix (`beads_bar_countdown_glyph`) is UNCHANGED — it continues to render
every tick as today; this proposal only changes what `phase_content` itself looks like during the
first few ticks of a new phase.

## Batch mapping (no traditional DB/API/UI/E2E layers — same convention as prior cc-tmux changes)

- **DB** = `POUR_FRAMES` + `POUR_STAGGER_TICKS` constants.
- **API** = `pour_transition_text` (pure function) + `render_beads_bar` wiring (both touch
  `render.py`, kept as one batch since the wiring is a two-line addition immediately consuming the
  function it's paired with — splitting them into separate API/UI batches would be artificial for
  a change this small).
- **E2E** = `testing.py` self-tests + live verification.

No dedicated UI batch — there is no `cli.py` change this time (unlike `cc-tmux-row3-next-cycle`,
which needed to wire a new caller-side data source; this proposal only changes `render.py`'s
internal rendering, which `_build_beads_bar` already calls with `now` set).
