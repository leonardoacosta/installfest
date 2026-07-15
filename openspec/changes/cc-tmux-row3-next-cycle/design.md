# Design: cc-tmux-row3-next-cycle

## Phase selection: reuse the existing wall-clock cadence, no new timer

`render.py` already derives every animated glyph purely from a caller-supplied `now` (real
`time.time()` at the call site), re-evaluated on tmux's own `status-interval` re-render tick —
`animated_icon`'s `int(now / FRAME_PERIOD_SEC) % len(frames)` is the established idiom. This
proposal adds one more constant and one more phase function in the same family:

```python
SWAP_PERIOD_SEC = 8.0  # seconds each phase (counts vs. next) stays visible

def beads_bar_phase(now: float) -> int:
    """0 = op:/bd: counts, 1 = next-action line. Pure function of now."""
    return int(now / SWAP_PERIOD_SEC) % 2
```

8 seconds is long enough to read either state at a glance, short enough that the swap is noticed
within a normal render loop. Tunable — not a locked decision, unlike the color/flash rules the
idle-tab-usage-meter design locked. No daemon, no background process, no new tmux hook: the row
re-renders (and therefore re-evaluates phase) on the same 1s-floor `status-interval` tick every
other row already relies on.

## Countdown glyph: reuse `IDLE_METER_RAMP`'s drain half, not a new table

Reader-gate check: does a glyph ramp expressing "time draining until an event" already exist in
this file? Yes — `IDLE_METER_RAMP[8:16]` (`⣿ ⢿ ⠿ ⠻ ⠛ ⠙ ⠉ ⠈`) is exactly a fill-to-empty braille
drain sequence, already established for the idle-tab usage meter. Reusing it here (rather than
inventing a second glyph family) keeps the plugin's visual vocabulary consistent — a user who has
learned "this braille drain means something is running out" from the tab row sees the same
metaphor on the beads row.

```python
_COUNTDOWN_RAMP: Tuple[str, ...] = IDLE_METER_RAMP[8:16]  # 8 frames, full -> empty

def beads_bar_countdown_glyph(now: float) -> str:
    """8-frame drain glyph showing progress through the current SWAP_PERIOD_SEC phase."""
    elapsed_in_phase = now % SWAP_PERIOD_SEC
    idx = min(7, int(elapsed_in_phase / SWAP_PERIOD_SEC * 8))
    return _COUNTDOWN_RAMP[idx]
```

Rendered DIM (matching row 3's other informational, non-health-signal segments — the `open`/
`in_progress`/`ready` counts are already DIM by the same convention), prefixed to whichever
content is currently showing, in BOTH phases — so the countdown is visible continuously, not just
during the counts phase.

## `next:` extraction: a second pure parse of content `_read_roadmap_pulse` already fetched

`_read_roadmap_pulse` already returns the full (radar:-stripped) cache-file `content` string to
`_build_beads_bar`, which today only feeds it to `_parse_roadmap_pulse_counts`. No new fetch, no
new cache, no call into `nexus-statusline` or nx-agent — `_build_beads_bar` gains a second,
independent parse of the same string:

```python
def _parse_roadmap_pulse_next(content: str) -> Optional[str]:
    """The `next: ...` line from roadmap-pulse content, or None if absent.

    Content arrives pre-truncated by the producer (~/dev/cc roadmap-pulse --line mode already
    truncates long text with '...' before writing the cache file) — no truncation logic needed
    here.
    """
    for line in content.splitlines():
        if line.startswith("next:"):
            return line
    return None
```

Verified live cache-file shape (`~/.claude/scripts/state/roadmap-pulse.if.line`, this session):

```
next: [WORKSPACE-CMDCENTER] Wor...
bd: 0o 0r 0b
```

`radar:` lines are ALREADY stripped upstream by `_read_roadmap_pulse` before `content` reaches
either parser — no change needed to preserve the spec's existing `radar:`-exclusion guarantee.

## `render_beads_bar`: phase-gated left side, right side untouched

Current signature (unchanged params retained):

```python
def render_beads_bar(
    openspec_open, openspec_in_progress, openspec_ua,
    beads_open, beads_ready, beads_blocked,
    openspec_age_sec=None, beads_age_sec=None,
    account_label="",
    next_text: Optional[str] = None,   # NEW
    now: Optional[float] = None,       # NEW — None = today's exact byte-identical behavior
) -> str:
```

`now is None` (the default) renders EXACTLY today's output — no phase logic engages, no countdown
glyph appears, callers that don't pass `now`/`next_text` (if any exist, e.g. tests exercising the
old contract) see zero behavior change. When `now` is provided:

- `beads_bar_phase(now) == 0` (or `next_text is None`, i.e. no next-line available this tick):
  render today's `op:`/`bd:` segments exactly as now, prefixed with `beads_bar_countdown_glyph(now)`.
- `beads_bar_phase(now) == 1` and `next_text` is present: render `next_text` alone (no `op:`/`bd:`
  segments) as the LEFT-flowing content, prefixed with the same countdown glyph.
- The right-aligned account-identity segment renders in BOTH phases, unchanged — it is
  independent of the left-side cycle, exactly as it's independent of the `op:`/`bd:` presence
  today (see the existing "no roadmap-pulse cache, but an active account resolves" scenario).
- No `op:`/`bd:` AND no `next_text` AND no account label -> `""`, matching today's "nothing
  available" contract.

## `_build_beads_bar`: one extra pure-function call, no new I/O

```python
content, age_sec = _read_roadmap_pulse(pane)   # unchanged — already fetches the file
(...) = _parse_roadmap_pulse_counts(content)    # unchanged
next_text = _parse_roadmap_pulse_next(content)  # NEW — same already-fetched content
account_label, _, _ = _active_usage()           # unchanged
return render.render_beads_bar(
    ..., account_label=account_label,
    next_text=next_text, now=time.time(),       # NEW
)
```

## Spec delta: MODIFIED, not ADDED

This changes an EXISTING requirement's behavior ("A dedicated tmux status row surfaces open/ready
beads and proposals") — same row, same function, same capability — so the delta is `## MODIFIED
Requirements`, replacing the "next: SHALL NOT render" clause + its dedicated scenario with the
cycling contract, and adding new scenarios for phase 0/phase 1/no-next-available. The `radar:`
exclusion scenario is UNCHANGED (radar: still never renders, in either phase) and is kept
verbatim in the modified requirement body.

## Batch mapping (no traditional DB/API/UI/E2E layers — same convention as prior cc-tmux changes)

- **DB** = the new constant + glyph-ramp slice + phase/countdown pure functions (data/constants).
- **API** = `_parse_roadmap_pulse_next` + `render_beads_bar`'s extended signature (pure functions).
- **UI** = `_build_beads_bar` wiring (plumbing the new parse + `now` through to the renderer).
- **E2E** = `testing.py` self-tests + live tmux verification.
