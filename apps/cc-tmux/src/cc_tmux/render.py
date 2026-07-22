"""Presentation-layer pure functions for cc-tmux views (Req-5, Req-7).

Everything here is a *pure* function of its inputs — no tmux dependency — so the
status-format, duration, and inbox-column rendering are unit-testable without a
live server (design.md invariant 2: views derive, they do not store). The CLI
handlers in :mod:`cc_tmux.cli` read tmux options and hand the values in.

State icons are functional status-bar glyphs (Geometric Shapes block, not
emoji) with sensible defaults, each overridable via ``@cc-icon-<state>``.
"""

from __future__ import annotations

import re
import time
from typing import Callable, Dict, List, Optional, Sequence, Tuple

from . import tmux
from .usage import (
    BLUE,
    BRIGHT_RED,
    CYAN,
    DARK_RED,
    DIM,
    GREEN,
    LIGHT_GREEN,
    ORANGE,
    RED,
    YELLOW,
    color_for,
    pct_for,
)

# Default state glyphs. Geometric Shapes (U+25CF/25CB/25D0), NOT emoji — plain
# monospace-friendly marks that render in any terminal and are overridable.
DEFAULT_ICONS: Dict[str, str] = {
    "waiting": "●",  # ● filled — needs attention
    "idle": "○",     # ○ hollow — done / ready
    "active": "◐",   # ◐ half — working
}

# ---------------------------------------------------------------------------
# Animated window-tab icon (Req: animated tab icon)
#
# The literal window NAME (set via `rename-window`) only changes on discrete
# Claude Code hook events — irregular, sometimes minutes apart, sometimes
# bursty — so it cannot drive a believable animation on its own. Real motion
# needs a wall-clock-driven re-render, which tmux already provides for free
# via `window-status-format`/`window-status-current-format`: those are
# re-evaluated on every status-bar refresh (`status-interval`), independent of
# hook activity. The render-all tabs-row job (status-format[0]) re-renders
# every window's icon each status-interval tick, so :func:`animated_icon`
# picks a frame purely from the caller-supplied wall-clock time — no timer,
# no background process, same "daemon-free" invariant as the rest of this
# plugin (tmux.py's own docstring).
#
# Frame family per state (distinct motion language, not just distinct icons):
#   waiting (needs a decision: permission/question/plan/elicitation) -> a
#     rising/falling shade pulse, reads as "needs attention".
#   active (Claude mid-turn) -> a rotating block edge, reads as "in motion".
#   idle (turn ended, nothing pending) -> a single static glyph, deliberately
#     NOT animated — nothing is happening, so nothing should move.
# ---------------------------------------------------------------------------

IDLE_GLYPH = "█"

# cc-tmux-braille-flash-and-permission-pulse (design.md § Glyph picks): dedicated
# 2-frame flash pairs that replace the former BLOCK_FRAMES (active/thinking) and
# SHADE_FRAMES (waiting) full-cycle animations in `animated_icon` (task 2.1 wired
# the swap; those two constants have since been removed — zero remaining
# references after task 3.1's testing.py rewrite, per the post-wave review gate).
# PERMISSION_PULSE_FRAMES reuses the circle-with-dot glyphs freed by the
# SUBAGENT_FG_1/SUBAGENT_FG_2PLUS rename below — `◉` is colored YELLOW, `◎`
# default/unstyled (task 2.3 wires the color branch in `resolve_tab_glyph`).
#
# cc-tmux-glyph-unification: ACTIVE_FLASH_FRAMES's fixed braille pair is no
# longer used by `animated_icon`'s active branch — active now pulses between
# two adjacent :data:`IDLE_METER_RAMP` glyphs bracketing the session's current
# burn (see `animated_icon` below), the same ramp language the idle meter
# already speaks. Kept defined (not deleted): `testing.py` still asserts the
# old braille-pair behavior — see this task's final report for the citation.
ACTIVE_FLASH_FRAMES: Tuple[str, str] = ("⠋", "⠙")
PERMISSION_PULSE_FRAMES: Tuple[str, str] = ("◉", "◎")

# Seconds per frame. Matches the (default 1s-floor) status-interval driving
# re-renders — a shorter period than the actual refresh cadence would just
# mean some frames are silently skipped, which is harmless.
FRAME_PERIOD_SEC = 1.0

# ---------------------------------------------------------------------------
# Idle-tab usage meter (cc-tmux-idle-tab-usage-meter)
#
# Replaces the static IDLE_GLYPH with a 17-state ramp driven by absolute
# context tokens burned this session. See
# openspec/changes/cc-tmux-idle-tab-usage-meter/design.md § "The 17-state
# ramp" for the full boundary math (round-to-nearest-16th rationale, fill/
# drain glyph shape derivation) — not re-derived here.
# ---------------------------------------------------------------------------

# Absolute-token scale the ramp index is computed against (design.md §
# "The 17-state ramp" § Scale) — deliberately the SAME domain as
# resolve_context_color's absolute-burn colour tiers, not window-relative.
IDLE_METER_SCALE_TOKENS = 1_000_000

IDLE_METER_RAMP: Tuple[str, ...] = (
    "░",  # 0   — 0%      (flash: alternates with U+2800 blank on FRAME_PERIOD_SEC parity)
    "⡀",  # 1   — 6.25%
    "⣀",  # 2   — 12.5%
    "⣄",  # 3   — 18.75%
    "⣤",  # 4   — 25%
    "⣦",  # 5   — 31.25%
    "⣶",  # 6   — 37.5%
    "⣷",  # 7   — 43.75%
    "⣿",  # 8   — 50%
    "⢿",  # 9   — 56.25%
    "⠿",  # 10  — 62.5%
    "⠻",  # 11  — 68.75%
    "⠛",  # 12  — 75%
    "⠙",  # 13  — 81.25%
    "⠉",  # 14  — 87.5%
    "⠈",  # 15  — 93.75%
    "▓",  # 16  — 100%
)


def _idle_meter_index(ratio: float) -> int:
    """Ramp index (0-16) for `ratio` (0..1), clamped then rounded to the
    nearest 16th — see design.md § "The 17-state ramp" § Index function.
    """
    return round(max(0.0, min(1.0, ratio)) * 16)


def idle_usage_meter(raw_tokens: Optional[float], now: float) -> Tuple[str, str]:
    """``(glyph, color)`` for the idle-tab usage meter at wall-clock ``now``.

    ``raw_tokens is None`` (cc-tmux-glyph-unification: unifies the idle
    no-data fallback onto the ramp) renders the ramp's own state-0 glyph
    (:data:`IDLE_METER_RAMP` index 0, ``░``) STATIC in DIM — never flashing,
    and never :data:`IDLE_GLYPH`'s old solid block. This stays visually
    distinct from the genuinely-fresh state-0 case just below (which DOES
    flash ``░`` against a blank cell): a data gap MUST NOT render as the
    fresh-session flash, but it also must not collapse to a second,
    meaning-free idle glyph — both now speak the same ramp language, just one
    moves and one doesn't.

    Otherwise the glyph is :data:`IDLE_METER_RAMP` indexed by
    :func:`_idle_meter_index` against :data:`IDLE_METER_SCALE_TOKENS`; index 0
    additionally flashes between the ramp glyph and a blank braille cell
    (U+2800, same column width) on :data:`FRAME_PERIOD_SEC` parity
    (design.md § "Flash"). Color is always :func:`resolve_context_color`
    reused verbatim for the data-present case — no meter-specific color logic
    (locked decision, design.md § "Color + pulse"); the no-data case uses
    :data:`DIM` directly (there is no raw-token count for
    :func:`resolve_context_color` to grade).
    """
    if raw_tokens is None:
        return IDLE_METER_RAMP[0], DIM
    ratio = raw_tokens / IDLE_METER_SCALE_TOKENS
    idx = _idle_meter_index(ratio)
    if idx == 0:
        glyph = IDLE_METER_RAMP[0] if int(now / FRAME_PERIOD_SEC) % 2 else "⠀"
    else:
        glyph = IDLE_METER_RAMP[idx]
    return glyph, resolve_context_color(raw_tokens, now)


def animated_icon(state: str, now: float, raw_tokens: Optional[float] = None) -> str:
    """The tab-icon glyph for ``state`` at wall-clock ``now``.

    Pure function of its inputs (testable without a live clock or tmux) —
    callers supply the real ``time.time()`` (see :func:`cc_tmux.cli._build_tabs_row`).
    ``waiting`` cycles :data:`PERMISSION_PULSE_FRAMES` by ``now // FRAME_PERIOD_SEC``.
    ``idle`` always returns the same static glyph (:data:`IDLE_GLYPH`) — this
    branch is for a window with NO tracked pane data at all, a different code
    path than :func:`idle_usage_meter`'s own no-data fallback, which speaks
    the ramp instead (cc-tmux-glyph-unification; see that function's
    docstring). Any other state (or an empty string, meaning no tracked pane)
    falls back to :data:`DEFAULT_ICONS`, then to ``""`` — callers should treat
    an empty result as "print nothing".

    ``active`` (cc-tmux-glyph-unification: ramp-adjacent active pulse) pulses
    between two adjacent :data:`IDLE_METER_RAMP` glyphs on the same
    :data:`FRAME_PERIOD_SEC` wall-clock parity every other branch here uses,
    replacing the old fixed :data:`ACTIVE_FLASH_FRAMES` braille pair with
    motion that carries session burn — same visual language the idle meter
    already speaks, plus motion. ``raw_tokens is None`` (caller has no token
    data for this window yet — the ``optional`` default keeps this parameter
    backward compatible for every pre-existing caller) pulses ramp index 1
    against a blank cell (``" "``/``⡀``) uncoloured, not the ramp's own
    index-0 glyph — motion without data still speaks ramp, but the 0-6.25%
    low frame reads as "nothing yet" rather than the shade-block ``░``
    glyph. Otherwise the ramp index ``i`` is :func:`_idle_meter_index` against
    :data:`IDLE_METER_SCALE_TOKENS`, pulsing between ``i`` and
    ``min(i + 1, 16)``; at ``i == 16`` (already at the top of the ramp) both
    frames would degenerate to the same glyph, so that case special-cases to
    15<->16 instead, preserving two-frame contrast. This function only
    returns the glyph — colour (``resolve_context_color(raw_tokens, now)``
    when data is present) is the caller's problem, matching
    :func:`resolve_tab_icon`'s existing plain-``str`` contract rather than
    widening it (see :func:`resolve_tab_glyph`, which composes the pair).
    """
    if state == "waiting":
        return PERMISSION_PULSE_FRAMES[int(now / FRAME_PERIOD_SEC) % 2]
    if state == "active":
        if raw_tokens is None:
            lo, hi = 0, 1
        else:
            i = _idle_meter_index(raw_tokens / IDLE_METER_SCALE_TOKENS)
            lo, hi = (15, 16) if i >= 16 else (i, min(i + 1, 16))
        # The 0-6.25% low frame reads as a blank cell, not the ramp's own
        # index-0 shade-block glyph (░) — every other index still pulses
        # between its own two ramp glyphs.
        low_frame = " " if lo == 0 else IDLE_METER_RAMP[lo]
        return (low_frame, IDLE_METER_RAMP[hi])[int(now / FRAME_PERIOD_SEC) % 2]
    if state == "idle":
        return IDLE_GLYPH
    return DEFAULT_ICONS.get(state, "")


# ---------------------------------------------------------------------------
# Sub-agent tab-icon overlay (cc-tmux-subagent-tab-icon)
#
# cc-tmux-glyph-unification collapses what was a 6-way glyph mapping (Leo,
# 2026-07-12, superseded) down to ONE legible presence signal: any tracked
# sub-agent activity (foreground OR background, any count) flashes a single
# `◇`/`◆` diamond pair. The old distinction — circle vs diamond shape family
# for foreground vs background, hollow=1/filled=2+ within each family — is
# GONE from the rendered glyph; foreground-precedence and background age-out
# PRUNING logic are unchanged upstream (:func:`cc_tmux.cli.prune_background_entries`),
# they just no longer produce a distinguishable glyph. Per-agent detail moves
# to a dedicated status row instead (sibling proposal
# `cc-tmux-row4-session-title`) — the tab overlay's job is now just "some
# sub-agent is running," not "which kind, how many."
# ---------------------------------------------------------------------------

# Single diamond flash pair (cc-tmux-glyph-unification) replacing the four
# fg/bg-count-keyed pairs below. `SUBAGENT_FG1_FLASH_FRAMES` /
# `SUBAGENT_FG2PLUS_FLASH_FRAMES` / `SUBAGENT_BG1_FLASH_FRAMES` /
# `SUBAGENT_BG2PLUS_FLASH_FRAMES` are no longer referenced by
# `resolve_tab_icon` below — kept defined only because `testing.py` still
# asserts the old four-pair behavior directly (see this task's final report
# for the citation); the four now-dead STATIC identity constants they
# replaced (`SUBAGENT_FG_1`/`SUBAGENT_FG_2PLUS`/`SUBAGENT_BG_1`/
# `SUBAGENT_BG_2PLUS`) had zero remaining callers anywhere and are removed.
SUBAGENT_ACTIVITY_FLASH_FRAMES: Tuple[str, str] = ("◇", "◆")

SUBAGENT_FG1_FLASH_FRAMES: Tuple[str, str] = ("⠒", "⠲")
SUBAGENT_FG2PLUS_FLASH_FRAMES: Tuple[str, str] = ("⠶", "⠦")
SUBAGENT_BG1_FLASH_FRAMES: Tuple[str, str] = ("⠂", "⠄")
SUBAGENT_BG2PLUS_FLASH_FRAMES: Tuple[str, str] = ("⠆", "⠇")


def resolve_tab_icon(state: str, now: float, fg_count: int, bg_count: int) -> str:
    """The tab-icon glyph, sub-agent-aware (cc-tmux-subagent-tab-icon overlay).

    Pure function of its inputs — ``bg_count`` MUST already be the caller's
    PRUNED count (:func:`cc_tmux.cli.prune_background_entries`); this function
    has no clock-aging logic of its own, it only branches on counts. ANY
    nonzero ``fg_count`` or ``bg_count`` flashes a single ``◇``/``◆`` diamond
    pair (:data:`SUBAGENT_ACTIVITY_FLASH_FRAMES`) on :data:`FRAME_PERIOD_SEC`
    wall-clock parity (cc-tmux-glyph-unification: foreground vs background
    precedence and count no longer affect the RENDERED glyph — only whether
    the overlay activates at all; per-agent detail moved to a dedicated
    status row, sibling proposal ``cc-tmux-row4-session-title``). Falls
    through to the plain :func:`animated_icon` state-based glyph
    (waiting/active/idle) when neither is active — this is an ADDITIVE overlay
    on top of the existing ``@cc-state`` animation, not a replacement for it
    (proposal.md Non-Goals).
    """
    if fg_count > 0 or bg_count > 0:
        return SUBAGENT_ACTIVITY_FLASH_FRAMES[int(now / FRAME_PERIOD_SEC) % 2]
    return animated_icon(state, now)


def resolve_tab_glyph(
    state: str,
    now: float,
    fg_count: int,
    bg_count: int,
    raw_tokens: Optional[float] = None,
) -> Tuple[str, str]:
    """``(glyph, color)`` tab-icon pair, idle-usage-meter- and
    permission-pulse-aware (cc-tmux-idle-tab-usage-meter overlay +
    cc-tmux-braille-flash-and-permission-pulse).

    Pure additive wrapper — :func:`resolve_tab_icon` itself is untouched and
    remains the glyph-precedence core that this wrapper extends, rendering
    the plain, monochrome glyph unchanged. Precedence is IDENTICAL
    to :func:`resolve_tab_icon`'s documented order: sub-agent overlays beat
    both the meter and the pulse. Three plain, un-overlaid cases swap in a
    coloured pair instead of the bare glyph: ``fg_count == 0 and bg_count ==
    0 and state == "idle"`` uses :func:`idle_usage_meter`'s ramp glyph +
    severity colour, ``fg_count == 0 and bg_count == 0 and state ==
    "waiting"`` uses :data:`PERMISSION_PULSE_FRAMES` coloured YELLOW on
    ``◉`` and unstyled on ``◎`` (design.md § "Coloring the permission
    pulse"), and ``fg_count == 0 and bg_count == 0 and state == "active"``
    (cc-tmux-glyph-unification: ramp-adjacent active pulse) uses
    :func:`animated_icon`'s new ramp-pulse glyph coloured via
    :func:`resolve_context_color` on the same ``raw_tokens`` — reusing that
    helper verbatim, exactly as :func:`idle_usage_meter` does, no new colour
    logic — or left uncoloured when ``raw_tokens is None`` (the no-data pulse
    carries motion but no severity signal). Every other case returns
    ``(resolve_tab_icon(...), "")`` — an empty colour, so callers never wrap
    the existing sub-agent glyphs in a stray ``#[fg=...]``
    (design.md § "API shape: additive wrapper, `resolve_tab_icon` untouched").
    """
    if fg_count == 0 and bg_count == 0 and state == "idle":
        return idle_usage_meter(raw_tokens, now)
    if fg_count == 0 and bg_count == 0 and state == "waiting":
        icon = PERMISSION_PULSE_FRAMES[int(now / FRAME_PERIOD_SEC) % 2]
        color = YELLOW if icon == PERMISSION_PULSE_FRAMES[0] else ""
        return icon, color
    if fg_count == 0 and bg_count == 0 and state == "active":
        icon = animated_icon(state, now, raw_tokens)
        color = resolve_context_color(raw_tokens, now) if raw_tokens is not None else ""
        return icon, color
    return resolve_tab_icon(state, now, fg_count, bg_count), ""


def resolve_icons(get_option: Callable[[str], str]) -> Dict[str, str]:
    """Icon map with per-state ``@cc-icon-<state>`` overrides applied.

    ``get_option`` is injected (``tmux.get_global_option`` in production, a stub
    in tests) so this stays pure and testable.
    """
    icons = dict(DEFAULT_ICONS)
    for state in DEFAULT_ICONS:
        try:
            override = get_option(f"@cc-icon-{state}")
        except Exception:  # noqa: BLE001 - fail open to defaults
            override = ""
        if override:
            icons[state] = override
    return icons


def format_duration(seconds: float) -> str:
    """Compact human duration: ``5s`` / ``3m`` / ``2h`` / ``1d`` (floored)."""
    try:
        s = int(seconds)
    except (TypeError, ValueError):
        return "0s"
    if s < 0:
        s = 0
    if s < 60:
        return f"{s}s"
    m = s // 60
    if m < 60:
        return f"{m}m"
    h = m // 60
    if h < 24:
        return f"{h}h"
    return f"{h // 24}d"


def inbox_rows(
    panes: Sequence[object],
    icons: Dict[str, str],
    now: float,
) -> List[Tuple[str, str]]:
    """Aligned ``(label, pane_id)`` rows for the inbox / picker (Req-5 columns).

    Columns: state icon | ``session:window`` | project | branch | time-in-state
    | wait reason | task. Every column except the trailing task is left-padded to
    a common width so fzf/menu rows line up. Each pane needs ``id``/``session``/
    ``window``/``state``/``timestamp``/``project``/``branch``/``wait_reason``/
    ``task`` attributes (a :class:`~cc_tmux.tmux.PaneInfo`).
    """
    cells: List[List[str]] = []
    for p in panes:
        state = getattr(p, "state", "")
        cells.append(
            [
                icons.get(state, state or "?"),
                f"{getattr(p, 'session', '')}:{getattr(p, 'window', '')}",
                getattr(p, "project", "") or "-",
                getattr(p, "branch", "") or "-",
                format_duration(now - float(getattr(p, "timestamp", 0.0) or 0.0)),
                getattr(p, "wait_reason", "") or "-",
                getattr(p, "task", "") or "-",
                getattr(p, "id", ""),  # machine field, not aligned into the label
            ]
        )

    # Pad columns 0..5 (leave the trailing task at index 6 unpadded).
    pad_cols = 6
    widths = [0] * pad_cols
    for row in cells:
        for i in range(pad_cols):
            widths[i] = max(widths[i], len(row[i]))

    out: List[Tuple[str, str]] = []
    for row in cells:
        padded = [row[i].ljust(widths[i]) for i in range(pad_cols)]
        label = "  ".join(padded + [row[6]]).rstrip()
        out.append((label, row[7]))
    return out


# ---------------------------------------------------------------------------
# Context-window bar (cc-tmux-context-bar, Leo's ask 2026-07-13)
#
# SES colour tiers (label colour driven by raw_tokens; see below). Two
# independent scales, deliberately: the BAR FILL is driven by `used_pct`
# (this session's fraction of its OWN context window — "how close to hitting
# the wall right now"), while the BAR COLOUR is driven by `raw_tokens`
# (absolute context tokens burned this session, regardless of window size —
# "how much has been spent overall"). A 1M-window session at 250k tokens is
# only ~25% full (dim bar) but already ORANGE (absolute-burn colour), and
# that mismatch is intentional — both signals are useful together, not meant
# to agree.
# ---------------------------------------------------------------------------


def _context_color_pair(raw_tokens: Optional[float]) -> Tuple[str, Optional[str]]:
    """``(base_color, pulse_color_or_None)`` for `raw_tokens` context tokens used.

    Six-tier escalation ramp: <=100k -> DIM (safe zone, no signal); >100k
    GREEN; >200k YELLOW; >300k ORANGE; >500k RED (steady); >600k RED pulsing
    against BRIGHT_RED; >750k DARK_RED pulsing against RED — a visually
    darker, more urgent pulse than the 600k tier, not just a repeat of it.
    ``None`` (context data unavailable) -> ``(DIM, None)``, same as the
    safe-zone case — fail-open.

    Returns the pulse PAIR rather than one resolved colour: callers animate
    the pulsing tiers by alternating ``base``/``pulse`` on wall-clock parity
    (see :func:`resolve_context_color`) — ``pulse is None`` means "render
    ``base`` steady, no animation."
    """
    if raw_tokens is None:
        return DIM, None
    if raw_tokens > 750_000:
        return DARK_RED, RED
    if raw_tokens > 600_000:
        return RED, BRIGHT_RED
    if raw_tokens > 500_000:
        return RED, None
    if raw_tokens > 300_000:
        return ORANGE, None
    if raw_tokens > 200_000:
        return YELLOW, None
    if raw_tokens > 100_000:
        return GREEN, None
    return DIM, None


def resolve_context_color(raw_tokens: Optional[float], now: float) -> str:
    """The single colour to render `raw_tokens` at wall-clock `now`.

    Resolves :func:`_context_color_pair`'s pulse tiers to one concrete colour
    for this tick, alternating base/pulse every :data:`FRAME_PERIOD_SEC` —
    same wall-clock-driven, daemon-free cadence :func:`animated_icon` uses
    (and matches this plugin's 1s ``status-interval``, so the pulse actually
    reads as motion rather than a slow, unconvincing flicker).
    """
    base, pulse = _context_color_pair(raw_tokens)
    if pulse is None:
        return base
    return pulse if int(now / FRAME_PERIOD_SEC) % 2 else base


def format_context_tokens(raw_tokens: Optional[float]) -> str:
    """``"252.5k"``-style label for `raw_tokens` context tokens, or ``"--"``."""
    if raw_tokens is None:
        return "--"
    return f"{raw_tokens / 1000:.1f}k"


# ---------------------------------------------------------------------------
# Combined braille usage glyph (cc-tmux-braille-usage-glyph)
#
# Bit-order constants and the shared per-metric dot-fill helper. See
# openspec/changes/cc-tmux-braille-usage-glyph/design.md § Encoding for the
# full bit-math derivation (dot-position table, shared-overlay vs
# segmented-lanes rationale, width choice) — not re-derived here.
# ---------------------------------------------------------------------------

_BRAILLE_BASE = 0x2800
_SES_BITS = (0, 1, 3, 4)  # rows 1-2, 4 dots/cell (3-metric glyph)
_H5_BITS = (2, 5)  # row 3, 2 dots/cell (3-metric glyph)
_D7_BITS = (6, 7)  # row 4, 2 dots/cell (3-metric glyph)
_H5_BITS_WIDE = (0, 1, 3, 4)  # rows 1-2, 4 dots/cell (2-metric popup glyph)
_D7_BITS_WIDE = (2, 5, 6, 7)  # rows 3-4, 4 dots/cell (2-metric popup glyph)


def _apply_metric_dots(
    cells: List[int],
    ratio: Optional[float],
    bits: Tuple[int, ...],
    n: int,
) -> None:
    """OR this metric's proportional dot-fill into `cells` in place.

    `ratio` is 0..1 (matching the existing `_resolve_ses_pct`/`_extract_util`
    convention — NOT 0..100). `ratio is None` is a no-op: per-metric degrade
    per design.md § Staleness — this metric contributes zero dots, other
    metrics' already-OR'd bits in `cells` are unaffected.

    Otherwise: `total = len(bits) * n` is this metric's full dot budget,
    `dots = round(ratio * total)` (clamped to [0, 1] first) dots are filled
    sequentially left to right, each cell taking `min(remaining, len(bits))`
    dots from `bits` in order before moving to the next cell. This is the
    exact algorithm validated live in the `/openspec:explore` mockup — reuse
    verbatim, do not redesign the fill order.
    """
    if ratio is None:
        return
    total = len(bits) * n
    dots = round(max(0.0, min(1.0, ratio)) * total)
    remaining = dots
    for i in range(n):
        if remaining <= 0:
            break
        take = min(remaining, len(bits))
        for bit in bits[:take]:
            cells[i] |= 1 << bit
        remaining -= take


def render_usage_glyph(
    ses_ratio: Optional[float],
    h5_ratio: Optional[float],
    d7_ratio: Optional[float],
    n: int = 10,
) -> str:
    """Combined 3-metric braille usage glyph: SES (rows 1-2), 5H (row 3), 7D
    (row 4), all sharing one ``n``-cell run (design.md § Encoding — shared
    overlay, not segmented lanes).

    Worked example validated live during ``/openspec:explore``:
    ``render_usage_glyph(0.30, 0.88, 0.35, n=8) == "⣿⣿⣧⠤⠤⠤⠤⠀"``
    (``⣿⣿⣧⠤⠤⠤⠤⠀``) — SES=30% fills rows 1-2 to ~31%, 5H=88% fills row 3 to
    87.5%, 7D=35% fills row 4 to 37.5%.

    Per-metric degrade (design.md § Staleness): each ratio is independently
    ``None``-able. A ``None`` ratio contributes zero dots to its own row(s)
    only — the other metrics' rows render normally, unaffected. All three
    ``None`` -> fully blank glyph (every cell ``chr(_BRAILLE_BASE)``, i.e.
    U+2800).
    """
    cells = [0] * n
    _apply_metric_dots(cells, ses_ratio, _SES_BITS, n)
    _apply_metric_dots(cells, h5_ratio, _H5_BITS, n)
    _apply_metric_dots(cells, d7_ratio, _D7_BITS, n)
    return "".join(chr(_BRAILLE_BASE + c) for c in cells)


def render_usage_glyph_2metric(
    h5_ratio: Optional[float],
    d7_ratio: Optional[float],
    n: int = 20,
) -> str:
    """Combined 2-metric braille usage glyph: 5H (rows 1-2), 7D (rows 3-4),
    each getting the full 4-dot-per-cell budget (design.md § Non-active popup
    rows). Used exclusively by :func:`render_accounts_popup`'s non-active
    account rows, which never have an SES value to show (SES is
    session-scoped, not account-scoped).

    Per-metric degrade applies the same as :func:`render_usage_glyph`: a
    ``None`` ratio contributes zero dots to its own row(s) only.
    """
    cells = [0] * n
    _apply_metric_dots(cells, h5_ratio, _H5_BITS_WIDE, n)
    _apply_metric_dots(cells, d7_ratio, _D7_BITS_WIDE, n)
    return "".join(chr(_BRAILLE_BASE + c) for c in cells)


# ---------------------------------------------------------------------------
# Session / beads status rows (row 2 + row 3 — cc-tmux-session-usage-bars,
# corrected post cc-tmux-bar-cleanup)
#
# Both are *pure* composition functions. They emit tmux status-format strings
# using the same ``#[fg=…]``/``#[default]`` escaping convention as the
# retired ``cc_tmux.usage.render_usage`` did, reusing that module's
# ``CYAN``/``DIM`` colour constants. The CLI handler (``cmd_render_all``)
# reads tmux/cache state and hands plain values in — nothing here touches tmux or
# a subprocess.
#
# Claude usage stats (account label, SES/5H/7D gauges) render on row 2's
# right side, alongside the left-side session/model/git identity. Only row 1
# (the window-tabs `status-right`) stays usage-free — that part of
# cc-tmux-bar-cleanup was correct and stays; cleanup's removal of usage from
# row 2 itself was a live-testing regression, reverted here.
# ---------------------------------------------------------------------------

# Branch-name colour (purple), distinct from usage.py's util palette. Model
# letter, project, and gauge labels reuse DIM/CYAN from usage.py.
BRANCH = "#B267E6"

# Row-2 model-letter colour, keyed by the single-letter family tag
# _resolve_model_letter (cli.py) returns: Opus=YELLOW, Sonnet=GREEN,
# Haiku=LIGHT_GREEN, Fable=RED. Any other/empty letter falls back to CYAN
# (fail-open, matching render_session_bar's existing "empty field drops out"
# convention) via .get()'s default rather than a KeyError.
_MODEL_LETTER_COLORS: Dict[str, str] = {
    "O": YELLOW,
    "S": GREEN,
    "H": LIGHT_GREEN,
    "F": RED,
}

# cc-tmux-usage-reset-countdown: row 2's 5H segment only grows a "when can I
# resume" countdown once utilization is at or above this threshold — matches
# nx's own poller hot-interval threshold, and the proposal's row-width budget
# call (below 80% the info is in the accounts popup, not row 2).
_ROW2_COUNTDOWN_THRESHOLD = 0.80

# mechanize-session-boundaries (cc, task 2.4): SES handoff signal fires at
# 70% of cc's CLAUDE_AUTOCOMPACT_PCT_OVERRIDE (default 90, cc `settings.json`)
# so the human has runway before the 90%-of-window autocompact fire — 0.70 *
# 0.90 = 0.63 of the raw context window, expressed here as a `ses_pct` ratio
# (0..1) since that's what compaction itself keys off, not an absolute token
# count. Hardcoded rather than read from cc's env: cc-tmux runs as a tmux
# plugin/hook shim with no guaranteed access to cc's CLAUDE_AUTOCOMPACT_PCT_
# OVERRIDE env var at render time (same constraint that keeps
# _ROW2_COUNTDOWN_THRESHOLD a literal above). If cc's override value ever
# changes from 90, this constant needs a matching manual update.
_SES_HANDOFF_THRESHOLD = 0.63


def _format_row2_countdown(remaining_secs: float) -> str:
    """``47m`` (under 60 min) or ``1h12m`` (at/above 60 min) compact countdown.

    Row-2-specific compact form — distinct from the accounts popup's
    ``HH:mm``/``dd:HH:mm`` (:func:`_format_reset_countdown`), which needs a
    fixed-width popup column rather than an inline status-format segment.
    Rounds to the nearest minute (never ``0m`` for a genuinely future reset)
    since callers already gate ``remaining_secs > 0`` before calling this.
    """
    total_minutes = max(1, round(remaining_secs / 60))
    if total_minutes < 60:
        return f"{total_minutes}m"
    hours, minutes = divmod(total_minutes, 60)
    return f"{hours}h{minutes}m"


def _row2_reset_countdown(
    five_h_pct: Optional[float],
    five_h_reset: Optional[float],
    now: Optional[float],
) -> str:
    """Compact row-2 5H countdown suffix, or ``""`` when it should not render.

    ``""`` (byte-identical to the pre-countdown segment, the proposal's Done
    Means fail-open contract) unless ``five_h_pct`` is at/above
    :data:`_ROW2_COUNTDOWN_THRESHOLD` AND ``five_h_reset`` is a real epoch
    strictly in the future.
    """
    if five_h_pct is None or five_h_pct < _ROW2_COUNTDOWN_THRESHOLD:
        return ""
    if five_h_reset is None:
        return ""
    t = time.time() if now is None else now
    remaining = five_h_reset - t
    if remaining <= 0:
        return ""
    return _format_row2_countdown(remaining)


def render_session_bar(
    model_letter: str,
    project: str,
    branch: str,
    ses_pct: Optional[float],
    five_h_pct: Optional[float],
    seven_d_pct: Optional[float],
    *,
    git_status: Optional["tmux.GitStatusCounts"] = None,
    raw_tokens: Optional[float] = None,
    five_h_reset: Optional[float] = None,
    now: Optional[float] = None,
) -> str:
    """Row-2 status-format string: model/project/git on the left, usage on the right.

    Left side: model letter + project + git branch, followed by up to six
    working-tree indicator segments (cc-tmux-git-status-glyphs task 3.1),
    each entirely omitted — no glyph, no stray separator — when its count is
    0. ``git_status`` is a :class:`cc_tmux.tmux.GitStatusCounts` (or ``None``,
    treated identically to an all-zero instance). In this fixed left-to-right
    order, space-separated: ``<N>M`` (GREEN) if ``modified > 0``, ``<N>U``
    (YELLOW) if ``untracked > 0``, ``<N>D`` (RED) if ``deleted > 0``, ``<N>R``
    (BLUE) if ``renamed > 0``, ``⇡<N>`` if ``ahead > 0``, ``⇣<N>`` if
    ``behind > 0`` — the ahead/behind glyphs are unstyled/DIM, matching the
    branch segment's own styling rather than getting a distinct colour. The
    whole indicator run is dropped (fail-open) when ``branch`` is empty, so a
    marker never appears without the branch it describes — same fail-open
    contract the prior ``dirty``/``ahead`` params had. Right side, in this
    order (design.md § Decision 2): the SES token-count label
    (``#[fg={_context_color_pair(...)[0]}]{label}:`` — text via
    :func:`format_context_tokens`, colour via :func:`_context_color_pair`'s
    6-tier severity ramp (base colour only, no wall-clock pulse),
    cc-tmux-braille-usage-glyph task 3.4 correction),
    then the 5H:/7D: gauges (coloured via color_for, formatted via pct_for),
    then a combined 3-metric braille usage glyph LAST
    (cc-tmux-braille-usage-glyph, replaces the former shade-block fill bar —
    see :func:`render_usage_glyph`, and ``design.md`` § Encoding for the
    full bit-math rationale) — target composition ``85.0K 5H:50% 7D:9%
    [glyph]``. The account-identity segment that previously led this row (and
    its ``#[range=user|accounts]`` click marker) moved off row 2 entirely to
    row 3 (:func:`render_beads_bar`); ``render_session_bar`` no longer takes
    an ``account_label`` parameter. The two sides are joined with a
    #[align=right] directive so tmux fills the gap between them. ``ses_pct``
    / ``five_h_pct`` / ``seven_d_pct`` (each 0..1, or ``None`` when unpolled
    -> that metric's row(s) render blank in the glyph, per-metric degrade;
    design.md § Staleness) feed the glyph directly at ``n=20`` (matching the
    accounts-popup's ``n=20`` "wide" convention,
    cc-tmux-row2-model-color-usage-format), which itself
    renders in a neutral/unstyled colour (design.md § Color — no severity
    ramp on the glyph). ``raw_tokens`` selects both the SES label's text via
    :func:`format_context_tokens` and its colour via
    :func:`_context_color_pair`'s 6-tier severity ramp — base colour only,
    the pulse variant is deliberately ignored, so the label is a STATIC
    per-tier colour that swaps as ``raw_tokens`` crosses thresholds and
    never blinks on the wall clock. five_h_pct
    / seven_d_pct are utilization ratios in 0..1 (or None when unpolled ->
    '--' in DIM for the text; also feed the glyph).

    ``five_h_reset`` (cc-tmux-usage-reset-countdown) is the active
    credential's 5H reset epoch. When ``five_h_pct >= 0.80`` AND
    ``five_h_reset`` is a future epoch, the 5H segment grows a compact DIM
    ``·<countdown>`` suffix (``5H:94%·47m``, or ``5H:94%·1h12m`` at/above 60
    minutes remaining) via :func:`_row2_reset_countdown` — below the
    threshold, or with no/past reset data, the segment renders byte-identical
    to before this parameter existed (fail-open). ``now`` defaults to
    ``time.time()`` when omitted; injectable for self-test determinism.

    ``ses_pct`` (mechanize-session-boundaries task 2.4) ALSO gates a handoff
    signal on the SES label itself: at/above :data:`_SES_HANDOFF_THRESHOLD`
    (0.63 — 70% of cc's ``CLAUDE_AUTOCOMPACT_PCT_OVERRIDE`` default of 90),
    the label grows a RED `` !handoff:/workflow:handoff`` suffix naming the
    escape hatch by name (start a fresh session via ``/workflow:handoff``)
    rather than relying on colour alone. Below threshold, or when ``ses_pct``
    is ``None`` (context data unavailable), the label is byte-identical to
    before this feature (fail-open).

    Pure function of its inputs (no tmux/subprocess). Empty model_letter /
    project / branch fields drop out of the left side (fail-open).
    """
    left_parts: List[str] = []
    if model_letter:
        model_color = _MODEL_LETTER_COLORS.get(model_letter, CYAN)
        left_parts.append(f"#[fg={model_color}]{model_letter}")
    if project:
        left_parts.append(f"#[fg={DIM}]{project}")
    if branch:
        left_parts.append(f"#[fg={DIM}]>")
        seg = f"#[fg={BRANCH}]{branch}"
        gs = git_status or tmux.GitStatusCounts()
        indicators: List[str] = []
        if gs.modified > 0:
            indicators.append(f"#[fg={GREEN}]{gs.modified}M")
        if gs.untracked > 0:
            indicators.append(f"#[fg={YELLOW}]{gs.untracked}U")
        if gs.deleted > 0:
            indicators.append(f"#[fg={RED}]{gs.deleted}D")
        if gs.renamed > 0:
            indicators.append(f"#[fg={BLUE}]{gs.renamed}R")
        if gs.ahead > 0:
            indicators.append(f"#[fg={DIM}]⇡{gs.ahead}")
        if gs.behind > 0:
            indicators.append(f"#[fg={DIM}]⇣{gs.behind}")
        if indicators:
            seg += " " + " ".join(indicators)
        left_parts.append(seg)
    left = " ".join(left_parts) + "#[default]"

    c5, c7 = color_for(five_h_pct), color_for(seven_d_pct)
    p5, p7 = pct_for(five_h_pct), pct_for(seven_d_pct)
    # SES label text unchanged (cc-tmux-context-bar); the shade-block bar is
    # replaced by the neutral combined usage glyph (cc-tmux-braille-usage-
    # glyph — design.md § Color: glyph stays unstyled). The 6-tier severity
    # ramp that used to colour the bar's fill now moves onto the label
    # itself (task 3.4 correction — the label was never actually wired to
    # the ramp before, despite the original design.md text assuming it was).
    ses_label = format_context_tokens(raw_tokens)
    ses_color, _ = _context_color_pair(raw_tokens)
    usage_glyph = render_usage_glyph(ses_pct, five_h_pct, seven_d_pct, n=20)
    # mechanize-session-boundaries task 2.4: past _SES_HANDOFF_THRESHOLD, the
    # SES label grows a DIM-RED "!handoff" suffix naming the escape hatch
    # explicitly (a colour swap alone doesn't tell the reader WHAT to do).
    # Fail-open to "" — byte-identical to the pre-existing render — whenever
    # `ses_pct` is unavailable or below threshold, same convention as the 5H
    # countdown suffix below.
    handoff_hint = ""
    if ses_pct is not None and ses_pct >= _SES_HANDOFF_THRESHOLD:
        handoff_hint = f"#[fg={RED}] !handoff:/workflow:handoff#[default]"
    # Right side: SES label, then 5H, then 7D, then the combined usage glyph
    # LAST (design.md § Decision 2). The account-identity segment + its
    # #[range=user|accounts] click marker moved off this row to row 3
    # (render_beads_bar). The SES label drops its trailing colon
    # (cc-tmux-row2-model-color-usage-format) -- 5H:/7D: keep theirs
    # unchanged -- and a single space now separates the 7D percentage from
    # the usage glyph (previously concatenated with zero space). The 5H
    # segment optionally grows a DIM "·<countdown>" suffix
    # (cc-tmux-usage-reset-countdown) BEFORE its own #[default] -- fail-open
    # to "" leaves the segment byte-identical to before this feature.
    countdown = _row2_reset_countdown(five_h_pct, five_h_reset, now)
    five_h_segment = f"#[fg={DIM}]5H:#[fg={c5}]{p5}"
    if countdown:
        five_h_segment += f"#[fg={DIM}]·{countdown}"
    five_h_segment += "#[default]"
    right = (
        f"#[fg={ses_color}]{ses_label}#[default]{handoff_hint} "
        f"{five_h_segment} "
        f"#[fg={DIM}]7D:#[fg={c7}]{p7}#[default] {usage_glyph}"
    )
    return f"{left}#[align=right]{right}"


# ---------------------------------------------------------------------------
# Accounts popup (cc-tmux-account-switcher-popup)
#
# Renders the multi-line body shown when the row-2 account-label segment is
# clicked (the #[range=user|accounts] marker above). This body reaches a
# REAL terminal — either fzf (``--ansi``, see ``cc-tmux.tmux``'s
# accounts_popup_cmd) or the plain ``display-popup -E "... ; read -n 1 -s"``
# fallback for pre-3.2 tmux/no-fzf — never tmux's own status-format renderer,
# so genuine ANSI SGR escapes (:func:`_green`) are the right mechanism for
# colour here, unlike :func:`render_session_bar`'s ``#[fg=...]`` tokens
# (which WOULD show up as literal garbage in this popup — the two renderers
# target two different consumers, not interchangeable escaping).
# ---------------------------------------------------------------------------

# Truecolor ANSI green matching this module's own tmux-hex GREEN (#00ac3a,
# usage.py) — Leo's ask (2026-07-13): every number/datetime in the popup is
# uniformly this green, NOT severity-escalated the way color_for's RED/
# YELLOW/CYAN colors the tmux status-format segments elsewhere in this file.
_ANSI_GREEN = "\x1b[38;2;0;172;58m"
_ANSI_RESET = "\x1b[0m"

# SGR-only escape matcher (colour codes, always ``\x1b[...m`` — every escape
# this module emits, see ``_green`` above, ends in ``m``).
# Shared with :mod:`cc_tmux.testing` (``_strip_ansi``, test-content assertions)
# and :mod:`cc_tmux.cli` (``cmd_accounts_popup_launch``'s width sizing,
# cc-tmux-status-bar-popup-polish task 3.4 follow-up, 2026-07-14) — both need
# the exact same "how wide does this ANSI-decorated line actually look"
# answer, so this lives once here rather than as two independently-maintained
# regexes drifting apart.
_ANSI_SGR_RE = re.compile(r"\x1b\[[0-9;]*m")


def strip_ansi(text: str) -> str:
    """Remove ANSI SGR (colour) escapes, leaving only what prints visually.

    ``len()`` on a string containing ``\\x1b[38;2;0;172;58m`` counts every byte
    of the escape sequence even though it renders as zero columns — any code
    measuring on-screen width (line length for alignment, popup sizing) MUST
    operate on this stripped form instead of the raw ANSI-decorated string.
    """
    return _ANSI_SGR_RE.sub("", text)


def _green(text: str) -> str:
    """Wrap ``text`` in the popup's ANSI truecolor green + reset."""
    return f"{_ANSI_GREEN}{text}{_ANSI_RESET}"


def _format_reset_countdown(remaining_secs: float, *, with_days: bool) -> str:
    """``HH:mm`` (5H) or ``dd:HH:mm`` (7D) countdown to a reset, or ``"now"``.

    ``remaining_secs`` <= 0 (already reset, or a clock skew edge case) ->
    ``"now"``, matching the convention nexus-statusline's own
    ``formatCountdown`` (``apps/nexus-statusline/src/render.ts``) uses for the
    identical "reset already passed" case.
    """
    if remaining_secs <= 0:
        return "now"
    total = int(remaining_secs)
    if with_days:
        days, rem = divmod(total, 86400)
        hours, rem = divmod(rem, 3600)
        minutes = rem // 60
        return f"{days:02d}:{hours:02d}:{minutes:02d}"
    hours, rem = divmod(total, 3600)
    minutes = rem // 60
    return f"{hours:02d}:{minutes:02d}"


# Width of the 7D line's day slot: a 3-letter weekday abbreviation + one
# separating space (``"Sat "``). The 5H line has no day of its own (its
# reset always lands the same calendar day), but pads with a same-width
# blank so BOTH lines' ``HH:mm a`` clock — and therefore both lines' "a"
# am/pm markers — start in the same column (Leo's 2026-07-13 alignment ask).
_DAY_SLOT_WIDTH = 4


def _format_reset_line(
    window_label: str,
    verb: str,
    reset_epoch: Optional[float],
    now: float,
    *,
    with_day: bool,
) -> Tuple[str, str]:
    """``(plain, colored)`` reset-time line pair, or ``("", "")`` when unresolved.

    ``reset_epoch`` absent (nexus-agent hasn't polled this window yet) ->
    ``("", "")``, so the caller omits the line entirely — fail-open, matching
    :func:`render_accounts_popup`'s own "nothing to show" convention rather
    than rendering a placeholder. Absolute time renders in LOCAL time
    (``time.localtime``, matching this module's ``now: float`` epoch-seconds
    convention elsewhere — :func:`resolve_tab_icon`/:func:`inbox_rows`) since
    that's what a human reading a terminal popup expects.

    ``with_day`` prefixes the short weekday name (``%a`` — ``Sat``/``Mon``/
    ``Thu``; 7D's reset can land on a different calendar day than "today", so
    "which day" matters more than "which date"; 5H always resolves same-day,
    so its slot is left blank — see :data:`_DAY_SLOT_WIDTH`) and switches the
    countdown from ``HH:mm`` to ``dd:HH:mm`` (see
    :func:`_format_reset_countdown`).

    Returns BOTH a plain-text line (for width/alignment math — ANSI escapes
    are zero-width visually but non-zero in ``len()``, so alignment MUST be
    computed off the plain variant) and a colour-decorated one (for actual
    display, :func:`_green` around the "numbers and datetimes" — the clock,
    weekday, and countdown, not the ``"Resets at/on"``/``"in"`` label text).
    """
    if reset_epoch is None:
        return "", ""
    lt = time.localtime(reset_epoch)
    clock = f"{time.strftime('%I:%M', lt)} {time.strftime('%p', lt).lower()}"
    day_slot = f"{time.strftime('%a', lt):<3} " if with_day else " " * _DAY_SLOT_WIDTH
    when = f"{day_slot}{clock}"
    countdown = _format_reset_countdown(reset_epoch - now, with_days=with_day)
    plain = f"{window_label} Resets {verb} {when}  in {countdown}"
    colored = f"{window_label} Resets {verb} {_green(when)}  in {_green(countdown)}"
    return plain, colored


def render_accounts_popup(
    accounts: Sequence[
        Tuple[str, Optional[float], Optional[float], Optional[float], Optional[float], str, str]
    ],
    active_label: str,
    now: Optional[float] = None,
) -> str:
    """Aligned, ANSI-green-accented popup body: one row per deduped account.

    ``accounts`` is every deduped account as an already-extracted
    ``(label, five_h_pct, seven_d_pct, five_h_reset_epoch, seven_d_reset_epoch,
    email, org_id_short)`` 7-tuple — the CLI handler
    (:func:`cc_tmux.cli.cmd_accounts_popup`) builds these via
    :func:`cc_tmux.usage.dedupe_credentials` +
    :func:`cc_tmux.usage._account_label`/:func:`cc_tmux.usage._account_identity`/
    :func:`cc_tmux.usage._extract_util`/:func:`cc_tmux.usage._extract_reset_at`,
    so this function stays pure with no credential-dict shape knowledge.
    ``label`` (email·org-char, see :func:`cc_tmux.usage._account_label`) is
    used ONLY as the internal ``active_label`` matching key now — it is no
    longer printed directly (cc-tmux-context-bar, Leo's 2026-07-13 ask to
    move identity off the summary line, since the SAME email can be
    authenticated against multiple orgs and email-alone is therefore not a
    safe matching key — see :func:`cc_tmux.usage._account_label`'s
    docstring). ``email``/``org_id_short`` print on a dedicated identity row
    instead, one indent level under the summary line, for EVERY account
    (active or not) — matching the existing reset-time lines' "every
    account, not just the starred one" convention.

    EVERY row (active and non-active alike) renders an identical summary
    line: a 2-metric braille usage glyph at ``n=20``
    (:func:`render_usage_glyph_2metric`, design.md § Decision 1) covering
    only 5H/7D, followed by ``5H:xx% 7D:xx%`` text. The popup is
    account-scoped, and SES (context-window-used %) is a property of the
    currently-focused pane's session, not of an account in the abstract, so
    it is not shown here at all — no per-row SES label or 3-metric glyph, no
    ``active_ses_pct``/``active_raw_tokens`` inputs. The row whose ``label``
    equals ``active_label`` (exact match) is distinguished ONLY by a leading
    ``*`` marker (Leo confirmed this is the sole active-account indicator
    wanted); its usage glyph is byte-identical in shape to a non-active
    row's. Every percentage is wrapped in :func:`_green`; the usage glyph is
    the one deliberate exception — it stays neutral/unstyled (design.md §
    Color: a single braille cell can't carry independent per-metric severity
    colours, so colour lives exclusively on the text).

    Below EVERY account's identity row sit up to two indented, aligned,
    green-accented reset-time lines, one per window, each omitted
    independently when that window's reset time hasn't been polled yet (see
    :func:`_format_reset_line`), followed by a full-width ``─`` rule
    separating this account's block from the next:

        * ⣿⣶⠶⠶⠶⠶⠶⠶⠶⠶⠶⠶⠶⠶⠶⠶⠶⠀  5H:36% 7D:71%
             leo@x.dev        37a74420
             5H Resets at      03:45 pm  in 02:14
             7D Resets on  Sat 03:45 pm  in 03:14:22
        ──────────────────────────────────────────

    Percent formatting reuses :func:`pct_for` (``--`` for an absent/unpolled
    value); :func:`color_for` (severity RED/YELLOW/CYAN) is deliberately NOT
    used for 5H/7D here — Leo wants those uniformly green, not escalated by
    utilization (contrast :func:`render_session_bar`'s tmux status-format
    segment, which DOES escalate 5H/7D too). ``now`` is the caller-supplied
    wall-clock epoch (``time.time()`` in production, injectable for
    deterministic tests) — same DI pattern :func:`render_tabs_row` already
    uses for its own ``now`` param, and now also drives the bar's pulse-tier
    animation.

    Pure function of its inputs. Empty ``accounts`` -> ``""`` (fail-open: an
    unreachable nexus-agent, or a payload with zero deduped/labelled
    credentials, renders nothing rather than an empty/garbled popup).
    """
    if not accounts:
        return ""
    t = time.time() if now is None else now

    rows: List[Tuple[str, str, bool, str, str, str, str, str]] = []
    for label, five_h, seven_d, five_h_reset, seven_d_reset, email, org_short in accounts:
        is_active = bool(active_label) and label == active_label
        five_h_str, seven_d_str = pct_for(five_h), pct_for(seven_d)
        # Every row (active or not) renders the same account-scoped 2-metric
        # glyph over 5H/7D (design.md § Decision 1) — SES is session-scoped
        # and no longer shown in this account popup. The active row is
        # distinguished solely by the leading `*` marker below.
        glyph2 = render_usage_glyph_2metric(five_h, seven_d, n=20)
        tail_plain = f"{glyph2} 5H:{five_h_str} 7D:{seven_d_str}"
        tail = f"{glyph2} 5H:{_green(five_h_str)} 7D:{_green(seven_d_str)}"
        identity = f"{email}  {org_short}" if org_short else email
        reset_5h_plain, reset_5h = _format_reset_line("5H", "at", five_h_reset, t, with_day=False)
        reset_7d_plain, reset_7d = _format_reset_line("7D", "on", seven_d_reset, t, with_day=True)
        rows.append((tail_plain, tail, is_active, identity, reset_5h_plain, reset_5h, reset_7d_plain, reset_7d))

    lines: List[str] = []
    for tail_plain, tail, is_active, identity, reset_5h_plain, reset_5h, reset_7d_plain, reset_7d in rows:
        marker = "* " if is_active else "  "
        summary_plain = f"{marker}{tail_plain}"
        lines.append(f"{marker}{tail}")
        block_width = len(summary_plain)
        lines.append(f"   {identity}")
        block_width = max(block_width, len(identity) + 3)
        if reset_5h_plain:
            lines.append(f"   {reset_5h}")
            block_width = max(block_width, len(reset_5h_plain) + 3)
        if reset_7d_plain:
            lines.append(f"   {reset_7d}")
            block_width = max(block_width, len(reset_7d_plain) + 3)
        lines.append("─" * block_width)
    return "\n".join(lines)


# ---------------------------------------------------------------------------
# Window-tabs row (cc-tmux-tabs-and-rename-fix)
#
# The per-window `window-status-format`/`window-status-current-format`
# mechanism never re-evaluates its nested `#()` job on this tmux version (3.6a)
# — confirmed via /openspec:explore runtime evidence — so the animated tab
# icon it was meant to drive never actually moves. This renders the ENTIRE
# tabs row as one string from a single top-level status-format slot instead
# (the same slot class row 2/row 3 already use), which DOES re-evaluate its
# `#()` job on every status-bar refresh. Same daemon-free, status-interval-
# driven cadence as animated_icon/render_session_bar/render_beads_bar — no
# background process, no timer of its own.
# ---------------------------------------------------------------------------


def _detect_portrait(client_width: int, client_height: int) -> bool:
    """Whether the client is in portrait orientation (taller than wide).

    Strictly ``client_height > client_width`` — landscape and the exact
    square case (equal width/height) both return ``False``. Pure function,
    no tmux dependency (cc-tmux-mobile-portrait-tabs task 1.2).
    """
    return client_height > client_width


def _compute_tab_rows(tab_segments: List[str], client_width: int, mobile: bool) -> int:
    """How many physical rows are needed to fit every tab segment without
    horizontal overflow.

    Each segment's effective width is ``len(segment)`` at 1x, or
    ``len(segment) * 3`` when ``mobile`` — matching the padding-based 3x
    mobile sizing decided by tasks.md task 1.1's live-verification outcome
    (OSC 66 did not render at scale in this environment, so padding is the
    active mechanism, not escape-sequence scaling). Sums every segment's
    effective width, divides by ``client_width`` (ceiling division), and
    always returns at least ``1`` — even for an empty ``tab_segments`` list
    or a non-positive ``client_width`` (defensive floor; this function does
    not itself validate caller-supplied dimensions).
    """
    if client_width <= 0:
        return 1
    multiplier = 3 if mobile else 1
    total_width = sum(len(seg) * multiplier for seg in tab_segments)
    if total_width <= 0:
        return 1
    rows = -(-total_width // client_width)  # ceiling division, stdlib-only
    return max(1, rows)


def _osc66_scale(text: str, scale: int = 3) -> str:
    """Wrap ``text`` in a Kitty/Ghostty OSC 66 text-sizing escape sequence.

    Returns ``ESC ]66;s={scale};{text} BEL`` (``ESC`` = ``0x1B``, ``BEL`` =
    ``0x07``), built via ``chr()`` rather than backslash-escape notation to
    sidestep markdown/shell/JSON escaping ambiguity in the source. Dead-but-
    defined per tasks.md task 1.1's live-verification outcome — OSC 66 did
    NOT render at scale on this fleet's actual terminal/tmux combination
    (`tmux display-popup` prototype, 2026-07-16), so PADDING-ONLY is this
    proposal's active mobile-sizing path. This helper is harmless to keep
    defined for a future terminal/tmux version that adds real support, but
    it is not wired into any active render call site by this task.
    """
    return f"{chr(0x1B)}]66;s={scale};{text}{chr(0x07)}"


def _partition_segments(segments: List[str], tab_rows: int) -> List[str]:
    """Split ``segments`` into exactly ``tab_rows`` contiguous, joined row strings.

    Front-loaded balanced partition (the first ``len(segments) % tab_rows``
    rows each get one extra segment) so windows stay in index order across the
    rows — window 0/1 land on physical row 0, later windows flow onto rows
    1..N-1, never scattered round-robin. A window's segment is never split
    mid-way. Always returns a list of length ``tab_rows`` (trailing rows are
    ``""`` when there are fewer segments than rows). Pure, no tmux dependency
    (cc-tmux-mobile-portrait-tabs task 3.1).
    """
    base, extra = divmod(len(segments), tab_rows)
    rows: List[str] = []
    idx = 0
    for r in range(tab_rows):
        take = base + (1 if r < extra else 0)
        rows.append("".join(segments[idx:idx + take]))
        idx += take
    return rows


def render_tabs_row(
    windows: Sequence[object],
    active_window_id: str,
    now: float,
    tab_rows: int = 1,
) -> str:
    """Row-1 status-format string: one ``icon name`` segment per window.

    ``windows`` is any sequence of objects with ``id``/``index``/``name``/
    ``state`` attributes (duck-typed via ``getattr``, matching this module's
    other pane/window-consuming functions — see :func:`inbox_rows`); the
    canonical source is :func:`cc_tmux.tmux.get_window_tabs`. ``state`` is the
    window's highest-priority tracked ``@cc-state``, or ``""`` for a window
    with no tracked Claude pane — that window renders with no icon (matches
    :func:`resolve_tab_icon`'s documented "untracked window -> no icon" contract),
    just its bare ``name``. The window index is never rendered in the visible
    label — only carried in the ``#[range=window|<index>]`` click-routing markup
    (see below). ``now`` is the caller-supplied wall-clock
    time (``time.time()`` in production) handed straight to
    :func:`resolve_tab_icon` (which falls through to :func:`animated_icon` for
    the animation frame when no sub-agent is active — cc-tmux-subagent-tab-icon)
    — same invocation pattern :func:`resolve_tab_icon`'s contract already documents,
    reused here per window rather than re-deriving the state->glyph mapping.
    ``fg``/``bg`` (duck-typed via ``getattr``, defaulting to ``0``/``[]``) are
    the window's sub-agent counts; ``bg`` MUST already be pruned by the caller
    (:func:`cc_tmux.cli._build_tabs_row`) before this is called — same
    contract :func:`resolve_tab_icon` documents. ``raw_tokens`` (duck-typed
    via ``getattr``, defaulting to ``None``) is set by the caller
    (:func:`cc_tmux.cli._build_tabs_row`) only for plain idle windows (no
    sub-agent overlay) and feeds :func:`resolve_tab_glyph` — a plain idle
    window's icon is the idle-usage-meter ramp glyph (cc-tmux-idle-tab-usage-
    meter) rather than the static :data:`IDLE_GLYPH`.

    The active window (``id == active_window_id``) renders bold CYAN; every
    other window renders DIM — the same semantic colour pair
    :func:`render_session_bar` uses for emphasis vs. identity text, reused
    here rather than inventing a third convention. No wrapping bg colour is
    applied (theme ``.conf`` files wrap the whole row, same as
    ``status-format[1]``/``[2]`` — see :func:`render_session_bar`).

    ``model_letter`` (duck-typed via ``getattr``, defaulting to ``""``) is
    set by the caller (:func:`cc_tmux.cli._build_tabs_row`) ONLY for the
    active window — resolving it per-window would mean an nx-agent call per
    tab per render tick. When the active window's ``model_letter == "F"``
    (Fable), its tab text renders bold RED instead of the normal bold CYAN;
    any other letter (or no letter) and inactive windows are unaffected —
    same ``"F": RED`` mapping :data:`_MODEL_LETTER_COLORS` already uses for
    the row-2 model-letter segment, reused here rather than a second RED
    reference.

    :func:`resolve_tab_glyph` returns a ``(glyph, color)`` pair. When
    ``color`` is non-empty (the idle-usage-meter case), ONLY the glyph is
    wrapped in it — ``#[fg={color}]{glyph}#[fg={label_colour}] `` — restoring
    the segment's own active/inactive label colour immediately after, so the
    trailing name text keeps rendering CYAN-bold/DIM exactly as before.
    When ``color`` is empty, the composition is byte-identical to the prior
    plain-icon rendering — no ``#[fg=]`` wrap is added.

    Each segment is wrapped in ``#[range=window|<index>]``/``#[norange]`` —
    the same range markup tmux's native window-status rendering emits, which
    is what makes the default ``MouseDown1Status`` binding (``switch-client
    -t =``) know which window a click landed on. Replacing tmux's native
    per-window rendering with this custom job (see module docstring) means we
    must emit that markup ourselves or clicks land nowhere.

    ``tab_rows`` (cc-tmux-mobile-portrait-tabs task 3.1) is the number of
    PHYSICAL status rows the tabs should span — the value
    :func:`cc_tmux.cli._build_tabs_row` derives from
    :func:`_compute_tab_rows` and publishes as ``@cc-tab-rows``. The default
    ``1`` (landscape) is BYTE-IDENTICAL to the pre-task-3.1 behaviour: all
    segments joined into one string, no newline. When ``tab_rows > 1``
    (portrait), the per-window segments are partitioned across that many rows
    via :func:`_partition_segments` and the rows are joined with a single
    ``\\n`` — a safe delimiter since no segment ever contains a newline
    (segments are ``#[...]``-markup + ``index name`` only). The caller splits
    on ``\\n``: row 0 becomes ``status-format[0]`` (render-all stdout), rows
    1..N-1 are published to ``@cc-tab-row-1``..``@cc-tab-row-{N-1}`` for the
    theme ``status-format[K]`` conditionals (task 3.2). This function performs
    NO tmux side effects itself — publishing the extra rows and growing
    ``status`` are the caller's job (module invariant: this module is a pure
    function of its inputs, tmux I/O lives in :mod:`cc_tmux.cli`).

    Pure function of its inputs (no tmux/subprocess). Empty ``windows`` ->
    ``""`` (nothing to show).
    """
    segments: List[str] = []
    for w in windows:
        state = getattr(w, "state", "") or ""
        fg_count = getattr(w, "fg", 0) or 0
        bg_count = len(getattr(w, "bg", None) or [])
        raw_tokens = getattr(w, "raw_tokens", None)
        index = getattr(w, "index", "")
        name = getattr(w, "name", "")

        model_letter = getattr(w, "model_letter", "") or ""

        is_active = active_window_id and getattr(w, "id", None) == active_window_id
        if is_active and model_letter == "F":
            colour = f"{RED},bold"
        else:
            colour = f"{CYAN},bold" if is_active else DIM

        glyph, meter_color = resolve_tab_glyph(state, now, fg_count, bg_count, raw_tokens)
        if meter_color:
            icon_part = f"#[fg={meter_color}]{glyph}#[fg={colour}] "
        else:
            icon_part = f"{glyph} " if glyph else ""
        label = f"{icon_part}{name}"

        segments.append(
            f"#[fg={colour}]#[range=window|{index}] {label} #[norange]#[default]"
        )
    if tab_rows <= 1:
        return "".join(segments)
    return "\n".join(_partition_segments(segments, tab_rows))


# Row 3 stale threshold: the roadmap-pulse cache is written under a ~5-minute
# SWR contract (rules/TOOLING.md Ambient Surfacing); 15 minutes = three missed
# refresh cycles, at which point the counts get a trailing age marker so stale
# data never masquerades as current (plan 006 / BEADS-01).
BEADS_STALE_AFTER_SEC = 900.0

# Row-3 per-number tiered coloring (cc-tmux-row3-tiered-colors). Independent
# per-label thresholds -- bd naturally runs ~2x op's volume at every tier
# (confirmed against live roadmap-pulse cache data across several projects).
# Boundary resolution (proposal.md § Why): the ask's stated ranges ("11-20
# pulsating", "20+ red") overlap at the edge as literally written; "+" is
# read as strictly-above-the-previous-tier, so *_RED_MIN is one past the
# previous tier's stated upper bound, not equal to it.
OP_YELLOW_MIN = 6    # n <= 5 -> default (DIM); 6 <= n <= 10 -> YELLOW
OP_PULSE_MIN = 11    # 11 <= n <= 20 -> pulsing YELLOW <-> DIM
OP_RED_MIN = 21       # n >= 21 -> RED

BD_YELLOW_MIN = 11   # n <= 10 -> default (DIM); 11 <= n <= 20 -> YELLOW
BD_PULSE_MIN = 21    # 21 <= n <= 40 -> pulsing YELLOW <-> DIM
BD_RED_MIN = 41      # n >= 41 -> RED

_BEADS_SEP = f"#[fg={DIM}] | "


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


def _pulse_segment(
    label: str,
    n1: int,
    suffix1: str,
    n2: int,
    suffix2: str,
    n3: int,
    suffix3: str,
    age_sec: Optional[float],
    yellow_min: int,
    pulse_min: int,
    red_min: int,
    now: Optional[float],
) -> str:
    """Colored ``"label: N1suffix1 N2suffix2 N3suffix3 (age)"`` segment.

    Renders cc's abbreviated roadmap-pulse shape (if-bqw.1) — e.g.
    ``"op: 1o 0ip 0ua"`` or ``"bd: 1o 1r 0b"`` — where each count's suffix is
    glued directly onto the number (no space) and only groups are
    space-separated. Each of the three numbers is colored INDEPENDENTLY via
    :func:`_tiered_color` against the label's own ``yellow_min``/
    ``pulse_min``/``red_min`` triple (cc-tmux-row3-tiered-colors) — every
    count is a health signal now, not just the third. ``age_sec`` beyond
    ``BEADS_STALE_AFTER_SEC`` appends a DIM trailing ``" (<duration>)"``
    marker, independent per segment.
    """
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


def render_beads_bar(
    openspec_open: Optional[int],
    openspec_in_progress: Optional[int],
    openspec_ua: Optional[int],
    beads_open: Optional[int],
    beads_ready: Optional[int],
    beads_blocked: Optional[int],
    openspec_age_sec: Optional[float] = None,
    beads_age_sec: Optional[float] = None,
    account_label: str = "",
    now: Optional[float] = None,
) -> str:
    """Row-3 status-format string from parsed roadmap-pulse counts, or ``''``.

    Renders up to three ``|``-separated segments in cc's abbreviated format
    (if-bqw.1, cc commit b6b9a234 / cc-w83ov.4):
    ``op: {open}o {in_progress}ip {ua}ua ({age})`` and
    ``bd: {open}o {ready}r {blocked}b ({age})``, replacing the prior
    ``openspec: {open} open {unarchived} unarchived`` / ``beads: {ready}
    ready {blocked} blocked`` two-number form. ``ua`` is the closure-debt
    count (done-but-unarchived specs); ``bd:`` carries a third, new number —
    ``open`` — the total standalone beads open/in_progress/blocked, alongside
    the pre-existing ``ready``/``blocked``. When ``account_label`` is
    non-empty, an account-identity segment (``email·orgid8char``, see
    :func:`cc_tmux.usage._account_label`) is appended, wrapped in the
    ``#[range=user|accounts]``/``#[norange]`` click marker relocated here from
    :func:`render_session_bar` (design.md § Decision 3). The
    ``MouseDown1Status`` binding in ``cc-tmux.tmux`` keys off
    ``#{mouse_status_range}`` globally across the whole status line, so moving
    which row emits the marker needs no binding change.

    Unlike the ``op:``/``bd:`` pair (left-flowing, ``_BEADS_SEP``-joined),
    the account segment is pushed to the right edge of the row via a
    ``#[align=right]`` directive — the same mechanism
    :func:`render_session_bar` uses to separate its identity/usage halves
    (Leo's ask: email+org-id reads as a distinct identity strip, not a third
    inline ``|``-joined count segment). It renders right-aligned even when
    ``op:``/``bd:`` are both absent — matching :func:`render_session_bar`'s
    contract of always emitting ``#[align=right]`` before its right-side
    content regardless of whether the left side is empty.

    Each of the three segments is independent and fail-open: a half whose
    triple of counts is not ALL present (``None`` from an absent/malformed
    cache line — see :func:`cc_tmux.cli._parse_roadmap_pulse_counts`) is
    omitted entirely rather than rendered with a placeholder, so a broken
    ``bd:`` line never blanks a valid ``op:`` half and vice versa — unless
    BOTH halves land in the identical state (both fully present-and-zero, or
    both fully absent), in which case the two collapsed-state paragraphs
    below take over instead. The account segment is likewise independent:
    when BOTH ``op:``/``bd:`` triples are absent (no cache) but
    ``account_label`` is non-empty, the row renders the (right-aligned)
    account segment alongside the collapsed ``Not available`` left side, not
    a blank left side. All three omitted (no cache and no account label) ->
    ``Not available`` (DIM) alone — the row's prior "no cache -> empty"
    contract (bare ``""``) was replaced by cc-tmux-row3-empty-states.

    Two collapsed left-side states (cc-tmux-row3-empty-states) replace the
    two-segment composition above when BOTH halves land in the same state.
    When all six count arguments are non-``None`` and all equal ``0``, the
    left side collapses to the single DIM literal ``All caught up`` instead
    of the noisy ``op: 0o 0ip 0ua | bd: 0o 0r 0b``. When all six count
    arguments are ``None`` (both halves fully absent), the left side
    collapses to the DIM literal ``Not available`` instead of an empty
    string. Neither collapse fires when only one half is all-zero/all-absent
    and the other half carries real per-count data: that half's existing
    single-segment rendering (via :func:`_pulse_segment`) is unaffected, and
    the two segments still render side-by-side ``_BEADS_SEP``-joined as
    before.

    Every count in both segments is colored independently via
    :func:`_tiered_color` against its label's own threshold triple
    (cc-tmux-row3-tiered-colors) — DIM/YELLOW/pulsing-YELLOW/RED as the
    number crosses :data:`OP_YELLOW_MIN`/:data:`OP_PULSE_MIN`/
    :data:`OP_RED_MIN` (``op:``) or :data:`BD_YELLOW_MIN`/
    :data:`BD_PULSE_MIN`/:data:`BD_RED_MIN` (``bd:``) — replacing the prior
    scheme where only the third number (``ua``/``blocked``) carried a health
    color and ``open``/``in_progress``/``ready`` stayed permanently DIM.
    ``openspec_age_sec``/``beads_age_sec`` are each independent cache-file
    ages in seconds — both halves read the SAME cache file's single mtime
    today (so callers typically pass the same value for both), but the
    per-segment marker is forward-compatible with a future per-half cache
    split (plan 006 / BEADS-01) with no further render.py change needed.
    Ages beyond ``BEADS_STALE_AFTER_SEC`` append a DIM trailing
    ``" (<duration>)"`` marker via :func:`format_duration`, independently per
    segment. ``now`` (``Optional[float]``, default ``None``) feeds the
    pulse-tier animation in :func:`_tiered_color` for both segments' numbers
    — ``now is None`` renders any number in the pulse tier as steady YELLOW
    rather than animating (fail-open); it no longer selects WHAT content
    renders (that swap-cycle behavior was reversed by this proposal — see
    proposal.md § Why), only how a pulse-tier number is colored at a given
    tick.

    The right-aligned account-identity segment is completely independent of
    the left side's coloring — it renders identically regardless of ``now``.

    Pure function of its inputs (no tmux/subprocess).
    """
    all_counts = (
        openspec_open,
        openspec_in_progress,
        openspec_ua,
        beads_open,
        beads_ready,
        beads_blocked,
    )
    if all(c is not None for c in all_counts) and all(c == 0 for c in all_counts):
        left = f"#[fg={DIM}]All caught up"
    elif all(c is None for c in all_counts):
        left = f"#[fg={DIM}]Not available"
    else:
        left_segments: List[str] = []
        if (
            openspec_open is not None
            and openspec_in_progress is not None
            and openspec_ua is not None
        ):
            left_segments.append(
                _pulse_segment(
                    "op", openspec_open, "o", openspec_in_progress, "ip", openspec_ua, "ua",
                    openspec_age_sec, OP_YELLOW_MIN, OP_PULSE_MIN, OP_RED_MIN, now,
                )
            )
        if beads_open is not None and beads_ready is not None and beads_blocked is not None:
            left_segments.append(
                _pulse_segment(
                    "bd", beads_open, "o", beads_ready, "r", beads_blocked, "b",
                    beads_age_sec, BD_YELLOW_MIN, BD_PULSE_MIN, BD_RED_MIN, now,
                )
            )
        left = _BEADS_SEP.join(left_segments)

    # Account-identity segment: the active account's identity, wrapped in the
    # #[range=user|accounts] click marker relocated from row 2
    # (render_session_bar). Independent of the two count halves above — it
    # renders even when neither openspec nor beads counts are present. Pushed
    # to the right edge via #[align=right] rather than joined inline with
    # _BEADS_SEP (Leo's ask: identity reads as a distinct right-hand strip).
    right = f"#[range=user|accounts]#[fg={DIM}]{account_label}#[norange]" if account_label else ""

    if not left and not right:
        return ""
    if right:
        return f"{left}#[default]#[align=right]{right}#[default]"
    return f"{left}#[default]"


# ---------------------------------------------------------------------------
# Agents row (row 4 -- cc-tmux-row4-session-title)
#
# Pure composition function, same "no I/O, no tmux dependency" contract as
# render_session_bar/render_beads_bar above. Unlike those two rows, this
# function's OUTPUT IS DELIBERATELY UNSTYLED -- no `#[fg=...]` wrap around
# either the title text or the glyph strip. tasks.md task 3.2 (UI batch,
# theme files) describes the row as "title text unstyled, glyphs inherit
# theme accent": the busy/settled distinction is carried entirely by glyph
# SHAPE (`◌`/`○` vs `●`), never colour, so there is no colour decision left
# for THIS function to make -- colour for the row as a whole (DIM-leaning,
# per task 3.2) is applied once, per-theme, around the whole `@cc-row-agents`
# slot, the same `#[bg=...]`-style wrapping the four theme `.conf` files
# already do for rows 2/3 (see render_session_bar's docstring "No wrapping
# bg colour is applied ... theme .conf files wrap the whole row"). This is a
# narrower contract than render_session_bar/render_beads_bar, which DO own
# their per-segment `#[fg=...]` colouring -- row 4 is the first row-composer
# in this module that hands ALL colour to the theme layer.
# ---------------------------------------------------------------------------

# Busy-glyph flash pair (same 2-frame flash idiom as PERMISSION_PULSE_FRAMES/
# SUBAGENT_ACTIVITY_FLASH_FRAMES above): `◌` hollow-dotted <-> `○` hollow,
# alternated on the module's shared FRAME_PERIOD_SEC wall-clock parity while
# a background dispatch is still inside its busy window. `●` (filled,
# static -- reused from DEFAULT_ICONS["waiting"], no new glyph) marks a
# settled entry: past busy_window but not yet aged out by the caller's prune.
AGENTS_BUSY_FLASH_FRAMES: Tuple[str, str] = ("◌", "○")
AGENTS_SETTLED_GLYPH = "●"

# Single space between per-agent glyphs in the strip -- no established
# precedent to match exactly (render_tabs_row's segments already carry their
# own internal spacing so its "".join(...) isn't directly comparable to a
# strip of bare single-character glyphs); a plain space keeps multiple
# glyphs visually distinct without adding markup weight to a row this small.
_AGENTS_GLYPH_SEP = " "


def render_agents_row(
    title: str,
    bg_entries: List[float],
    now: float,
    busy_window: float,
    client_width: Optional[int],
) -> str:
    """Row-4 status-format string: per-agent glyph strip, or the session title.

    ``bg_entries`` is a list of background-dispatch launch epoch timestamps
    -- same shape as a window's ``w.bg`` after
    :func:`cc_tmux.cli.prune_background_entries` -- already pruned/aged-out
    by the caller. This function has no aging logic of its own, mirroring
    :func:`resolve_tab_icon`'s "caller already pruned" contract for its own
    ``bg_count`` parameter.

    Nonempty ``bg_entries`` -> ONE glyph per entry, in launch order (the
    list's own order -- never re-sorted), joined with :data:`_AGENTS_GLYPH_SEP`.
    Per entry: ``now - entry < busy_window`` (still inside its busy window)
    flashes between :data:`AGENTS_BUSY_FLASH_FRAMES`' ``"◌"``/``"○"`` on the
    same wall-clock parity every other flash in this module uses
    (``int(now / FRAME_PERIOD_SEC) % 2``, see :data:`FRAME_PERIOD_SEC`);
    otherwise (past ``busy_window`` but not yet aged out by the caller's
    prune) renders the static :data:`AGENTS_SETTLED_GLYPH` (``"●"``).

    Empty ``bg_entries`` and nonempty ``title`` -> ``title`` truncated to
    ``client_width`` characters (plain ``title[:client_width]`` slice); a
    falsy ``client_width`` (``None``/``0``) leaves ``title`` untruncated --
    fail-open, matching this module's other missing-width-param handling
    (e.g. :func:`_compute_tab_rows`'s ``client_width <= 0`` floor).

    Both empty -> ``""`` (row omitted entirely -- proposal.md's content
    contract; the caller drops the row and the line-count arithmetic when
    this returns empty).

    Pure function of its inputs (no tmux/subprocess).
    """
    if bg_entries:
        glyphs: List[str] = []
        for entry in bg_entries:
            if now - entry < busy_window:
                glyphs.append(AGENTS_BUSY_FLASH_FRAMES[int(now / FRAME_PERIOD_SEC) % 2])
            else:
                glyphs.append(AGENTS_SETTLED_GLYPH)
        return _AGENTS_GLYPH_SEP.join(glyphs)
    if title:
        return title[:client_width] if client_width else title
    return ""
