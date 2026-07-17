# Design: cc-tmux-row3-tiered-colors

## Threshold constants (replaces `BEADS_UNARCHIVED_HIGH`/`BEADS_BLOCKED_HIGH`)

```python
# Row-3 per-number tiered coloring (cc-tmux-row3-tiered-colors). Independent
# per-label thresholds -- bd naturally runs ~2x op's volume at every tier
# (confirmed against live roadmap-pulse cache data across several projects).
OP_YELLOW_MIN = 6    # n <= 5 -> default (DIM); 6 <= n <= 10 -> YELLOW
OP_PULSE_MIN = 11    # 11 <= n <= 20 -> pulsing YELLOW <-> DIM
OP_RED_MIN = 21       # n >= 21 -> RED

BD_YELLOW_MIN = 11   # n <= 10 -> default (DIM); 11 <= n <= 20 -> YELLOW
BD_PULSE_MIN = 21    # 21 <= n <= 40 -> pulsing YELLOW <-> DIM
BD_RED_MIN = 41      # n >= 41 -> RED
```

Boundary resolution (see proposal.md's Why): the ask's stated ranges ("11-20 pulsating", "20+
red") overlap at the edge as literally written. Treating "+" as strictly-above-the-previous-tier
is the only non-overlapping reading; `OP_RED_MIN`/`BD_RED_MIN` are therefore one past the
previous tier's stated upper bound, not equal to it.

## Tiered-color function (replaces `_threshold_color`)

```python
def _tiered_color(
    n: int,
    yellow_min: int,
    pulse_min: int,
    red_min: int,
    now: Optional[float],
) -> str:
    """4-tier color for count `n`: DIM (< yellow_min), YELLOW (< pulse_min),
    pulsing YELLOW<->DIM on FRAME_PERIOD_SEC tick parity (< red_min), RED
    (>= red_min). `now is None` (no wall-clock available) renders the pulse
    tier as steady YELLOW -- fail-open, matching this file's existing
    None-handling convention (e.g. idle_usage_meter's `raw_tokens is None`
    case) -- never animates without a real `now`.
    """
    if n < yellow_min:
        return DIM
    if n < pulse_min:
        return YELLOW
    if n < red_min:
        if now is None:
            return YELLOW
        return YELLOW if int(now / FRAME_PERIOD_SEC) % 2 == 0 else DIM
    return RED
```

Reuses `FRAME_PERIOD_SEC` (already defined, used by `animated_icon`) rather than introducing a
new pulse-period constant — this is a UI-refresh-rate pulse, not a countdown-style timer, so it
belongs on the same cadence as the rest of the file's flash/pulse effects, not `SWAP_PERIOD_SEC`
(which is being deleted).

## `_pulse_segment` restructure

Current signature colors only `n3`:
```python
def _pulse_segment(label, n1, suffix1, n2, suffix2, n3, suffix3, age_sec, high) -> str
```

New signature colors all three, taking the label's three thresholds and `now`:
```python
def _pulse_segment(
    label: str,
    n1: int, suffix1: str,
    n2: int, suffix2: str,
    n3: int, suffix3: str,
    age_sec: Optional[float],
    yellow_min: int, pulse_min: int, red_min: int,
    now: Optional[float],
) -> str:
    c1 = _tiered_color(n1, yellow_min, pulse_min, red_min, now)
    c2 = _tiered_color(n2, yellow_min, pulse_min, red_min, now)
    c3 = _tiered_color(n3, yellow_min, pulse_min, red_min, now)
    age_suffix = ""
    if age_sec is not None and age_sec > BEADS_STALE_AFTER_SEC:
        age_suffix = f" ({format_duration(age_sec)})"
    return (
        f"#[fg={DIM}]{label}: "
        f"#[fg={c1}]{n1}#[fg={DIM}]{suffix1} "
        f"#[fg={c2}]{n2}#[fg={DIM}]{suffix2} "
        f"#[fg={c3}]{n3}#[fg={DIM}]{suffix3}{age_suffix}"
    )
```

Each number gets its own `#[fg=...]` open + a `#[fg={DIM}]` close immediately after its suffix
(reverting to DIM for the suffix text and the space before the next number) — same wrap-then-
revert idiom `_pulse_segment` already uses for `n3` today, just applied to all three instead of
one. `op`/`bd` call sites pass their own threshold triples:

```python
_pulse_segment("op", o, "o", ip, "ip", ua, "ua", age, OP_YELLOW_MIN, OP_PULSE_MIN, OP_RED_MIN, now)
_pulse_segment("bd", o, "o", r, "r", b, "b", age, BD_YELLOW_MIN, BD_PULSE_MIN, BD_RED_MIN, now)
```

## `render_beads_bar` simplification

Remove the `next_text` parameter and the `if now is None: ... else: phase = beads_bar_phase(now); ...`
branch entirely. The left side is always `counts_left` (built from the two `_pulse_segment` calls
above, `_BEADS_SEP`-joined) or `""` — no countdown glyph, no phase selection. `now` stays as an
`Optional[float] = None` parameter, threaded into both `_pulse_segment` calls for the pulse tier.

## `cli.py` — `_build_beads_bar`

Remove `_parse_roadmap_pulse_next` (dead after this — no other caller) and the
`next_text = _parse_roadmap_pulse_next(content)` line. Keep `now=time.time()` in the
`render.render_beads_bar(...)` call — it's now purely for the pulse-tier animation, not phase
selection.

## Removed entirely (confirmed zero other references anywhere in the codebase)

- `render.py`: `SWAP_PERIOD_SEC`, `_COUNTDOWN_RAMP`, `beads_bar_phase`, `beads_bar_countdown_glyph`,
  `BEADS_UNARCHIVED_HIGH`, `BEADS_BLOCKED_HIGH`, `_threshold_color`.
- `cli.py`: `_parse_roadmap_pulse_next`.
- `testing.py`: `_test_render_beads_bar_phase_and_countdown_glyph`, `_test_render_beads_bar_next_cycle`.

## Existing tests requiring rewrite (not removal)

`_test_render_beads_bar` and `_test_render_beads_bar_account_segment` assert exact rendered
strings that assume only `n3` carries a `#[fg=...]` wrap (`n1`/`n2` render as bare `12o 1ip` with
no color markup). Every one of these expected strings changes shape once `n1`/`n2` are
independently colored too — even a DIM-tier count (the common case, e.g. `12o`) now renders as
`#[fg={DIM}]12#[fg={DIM}]o` instead of the current bare `12o`. Update every expected string in
these tests to the new per-number-wrapped shape; do not delete the tests, since their non-color
assertions (segment presence/absence, separators, staleness markers, fail-open on partial data)
still apply unchanged.
