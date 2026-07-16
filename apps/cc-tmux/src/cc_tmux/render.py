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

SHADE_FRAMES: Tuple[str, ...] = ("░", "▒", "▓", "█", "▓", "▒", "░")
BLOCK_FRAMES: Tuple[str, ...] = ("▁", "▏", "▔", "▕")
IDLE_GLYPH = "█"

# cc-tmux-braille-flash-and-permission-pulse (design.md § Glyph picks): dedicated
# 2-frame flash pairs that replace BLOCK_FRAMES (active/thinking) and SHADE_FRAMES
# (waiting) in `animated_icon` (task 2.1 wires the actual swap; this task only adds
# the constants). PERMISSION_PULSE_FRAMES reuses the circle-with-dot glyphs freed by
# the SUBAGENT_FG_1/SUBAGENT_FG_2PLUS rename below — `◉` is colored YELLOW, `◎`
# default/unstyled (task 2.3 wires the color branch in `resolve_tab_glyph`).
ACTIVE_FLASH_FRAMES: Tuple[str, str] = ("⠋", "⠙")
PERMISSION_PULSE_FRAMES: Tuple[str, str] = ("◉", "◎")

# Seconds per frame. Matches the (default 1s-floor) status-interval driving
# re-renders — a shorter period than the actual refresh cadence would just
# mean some frames are silently skipped, which is harmless.
FRAME_PERIOD_SEC = 1.0

# Row-3 next-cycle (cc-tmux-row3-next-cycle) — see
# openspec/changes/cc-tmux-row3-next-cycle/design.md for the full rationale
# (swap cadence choice). Not re-derived here. (The companion _COUNTDOWN_RAMP
# constant lives below, after IDLE_METER_RAMP, since it slices that tuple.)
SWAP_PERIOD_SEC = 8.0


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


# Row-3 next-cycle (cc-tmux-row3-next-cycle) — countdown-to-swap glyph ramp,
# reusing IDLE_METER_RAMP's drain half rather than a new glyph table. See
# openspec/changes/cc-tmux-row3-next-cycle/design.md § "Countdown glyph" for
# the rationale. Not re-derived here.
_COUNTDOWN_RAMP: Tuple[str, ...] = IDLE_METER_RAMP[8:16]


def beads_bar_phase(now: float) -> int:
    """Row-3 next-cycle phase at wall-clock ``now``: ``0`` = ``op:``/``bd:``
    counts, ``1`` = the ``next:`` action line. Pure function of ``now`` — same
    ``int(now / period) % ...`` wall-clock-framing idiom :func:`animated_icon`
    already establishes, applied to :data:`SWAP_PERIOD_SEC` instead of
    :data:`FRAME_PERIOD_SEC` (design.md § "Phase selection").
    """
    return int(now / SWAP_PERIOD_SEC) % 2


def beads_bar_countdown_glyph(now: float) -> str:
    """8-frame drain glyph for how far ``now`` has progressed through the
    current :data:`SWAP_PERIOD_SEC` phase (full at the phase's start, empty at
    its end). Pure function of ``now``, indexing :data:`_COUNTDOWN_RAMP`
    (design.md § "Countdown glyph").
    """
    idx = min(7, int((now % SWAP_PERIOD_SEC) / SWAP_PERIOD_SEC * 8))
    return _COUNTDOWN_RAMP[idx]


def _idle_meter_index(ratio: float) -> int:
    """Ramp index (0-16) for `ratio` (0..1), clamped then rounded to the
    nearest 16th — see design.md § "The 17-state ramp" § Index function.
    """
    return round(max(0.0, min(1.0, ratio)) * 16)


def idle_usage_meter(raw_tokens: Optional[float], now: float) -> Tuple[str, str]:
    """``(glyph, color)`` for the idle-tab usage meter at wall-clock ``now``.

    ``raw_tokens is None`` renders byte-identical to today's static idle glyph
    — :data:`IDLE_GLYPH` with an empty color string (no ``#[fg=...]`` wrap) —
    never the flash glyph, since missing data is not the same as a fresh
    session (design.md § "`None` fallback: static `█`, never `░`").

    Otherwise the glyph is :data:`IDLE_METER_RAMP` indexed by
    :func:`_idle_meter_index` against :data:`IDLE_METER_SCALE_TOKENS`; index 0
    additionally flashes between the ramp glyph and a blank braille cell
    (U+2800, same column width) on :data:`FRAME_PERIOD_SEC` parity
    (design.md § "Flash"). Color is always :func:`resolve_context_color`
    reused verbatim — no meter-specific color logic (locked decision,
    design.md § "Color + pulse").
    """
    if raw_tokens is None:
        return IDLE_GLYPH, ""
    ratio = raw_tokens / IDLE_METER_SCALE_TOKENS
    idx = _idle_meter_index(ratio)
    if idx == 0:
        glyph = IDLE_METER_RAMP[0] if int(now / FRAME_PERIOD_SEC) % 2 else "⠀"
    else:
        glyph = IDLE_METER_RAMP[idx]
    return glyph, resolve_context_color(raw_tokens, now)


def animated_icon(state: str, now: float) -> str:
    """The tab-icon glyph for ``state`` at wall-clock ``now``.

    Pure function of its inputs (testable without a live clock or tmux) —
    callers supply the real ``time.time()`` (see :func:`cc_tmux.cli._build_tabs_row`).
    ``waiting``/``active`` cycle their frame tuple by ``now // FRAME_PERIOD_SEC``;
    ``idle`` always returns the same static glyph. Any other state (or an
    empty string, meaning no tracked pane) falls back to :data:`DEFAULT_ICONS`,
    then to ``""`` — callers should treat an empty result as "print nothing".
    """
    if state == "waiting":
        return SHADE_FRAMES[int(now / FRAME_PERIOD_SEC) % len(SHADE_FRAMES)]
    if state == "active":
        return BLOCK_FRAMES[int(now / FRAME_PERIOD_SEC) % len(BLOCK_FRAMES)]
    if state == "idle":
        return IDLE_GLYPH
    return DEFAULT_ICONS.get(state, "")


# ---------------------------------------------------------------------------
# Sub-agent tab-icon overlay (cc-tmux-subagent-tab-icon)
#
# Resolved 6-way glyph mapping (Leo, 2026-07-12, tasks.md task 1.1): foreground
# (exact, hook-verified via the Task tool's own PreToolUse/PostToolUse pair)
# and background (heuristic, timeout-aged) sub-agent activity get DISTINCT
# shape families — circle for foreground, diamond for background — so the two
# are visually distinguishable at a glance rather than colliding on the same
# two marks. Within each family, hollow=1 / filled=2+ mirrors the "hollow=one,
# filled=multiple" language DEFAULT_ICONS already uses elsewhere in this
# module. Foreground always takes precedence over background when both are
# nonzero (foreground is the exact signal; background is only a heuristic
# fallback) — see :func:`resolve_tab_icon`.
# ---------------------------------------------------------------------------

#
# cc-tmux-braille-flash-and-permission-pulse (design.md, task 1.1): SUBAGENT_FG_1/
# SUBAGENT_FG_2PLUS renamed "◎"->"□" / "◉"->"■" (hollow/filled square, distinct
# from the circle and diamond families) to free "◎"/"◉" for PERMISSION_PULSE_FRAMES
# above — a collision the original subagent-tab-icon proposal didn't anticipate.
# These two are now the STATIC identity reference only; resolve_tab_icon below
# still returns them directly until task 2.2 swaps each branch to index its
# SUBAGENT_*_FLASH_FRAMES pair instead (kept, not removed, precisely because
# resolve_tab_icon's four branches are out of this task's scope and still need a
# valid name to return in the interim).

SUBAGENT_FG_1 = "□"       # 1 foreground sub-agent running (static identity only)
SUBAGENT_FG_2PLUS = "■"   # 2+ foreground sub-agents running (static identity only)
SUBAGENT_BG_1 = "◇"       # 0 foreground, 1 unexpired background sub-agent
SUBAGENT_BG_2PLUS = "◆"   # 0 foreground, 2+ unexpired background sub-agents

# Dedicated 2-frame braille flash pairs (design.md § Glyph picks) that task 2.2
# wires into resolve_tab_icon's four branches, replacing the static glyphs above.
SUBAGENT_FG1_FLASH_FRAMES: Tuple[str, str] = ("⠒", "⠲")
SUBAGENT_FG2PLUS_FLASH_FRAMES: Tuple[str, str] = ("⠶", "⠦")
SUBAGENT_BG1_FLASH_FRAMES: Tuple[str, str] = ("⠂", "⠄")
SUBAGENT_BG2PLUS_FLASH_FRAMES: Tuple[str, str] = ("⠆", "⠇")


def resolve_tab_icon(state: str, now: float, fg_count: int, bg_count: int) -> str:
    """The tab-icon glyph, sub-agent-aware (cc-tmux-subagent-tab-icon overlay).

    Pure function of its inputs — ``bg_count`` MUST already be the caller's
    PRUNED count (:func:`cc_tmux.cli.prune_background_entries`); this function
    has no clock-aging logic of its own, it only branches on counts. Foreground
    takes precedence over background whenever ``fg_count`` is nonzero (it is
    the exact signal; background is only a time-boxed heuristic). Falls
    through to the plain :func:`animated_icon` state-based glyph
    (waiting/active/idle) when neither is active — this is an ADDITIVE overlay
    on top of the existing ``@cc-state`` animation, not a replacement for it
    (proposal.md Non-Goals).
    """
    if fg_count >= 2:
        return SUBAGENT_FG_2PLUS
    if fg_count == 1:
        return SUBAGENT_FG_1
    if bg_count >= 2:
        return SUBAGENT_BG_2PLUS
    if bg_count == 1:
        return SUBAGENT_BG_1
    return animated_icon(state, now)


def resolve_tab_glyph(
    state: str,
    now: float,
    fg_count: int,
    bg_count: int,
    raw_tokens: Optional[float] = None,
) -> Tuple[str, str]:
    """``(glyph, color)`` tab-icon pair, idle-usage-meter-aware
    (cc-tmux-idle-tab-usage-meter overlay).

    Pure additive wrapper — :func:`resolve_tab_icon` itself is untouched and
    remains the glyph-precedence core that this wrapper extends, rendering
    the plain, monochrome glyph unchanged. Precedence is IDENTICAL
    to :func:`resolve_tab_icon`'s documented order: sub-agent overlays and the
    waiting/active animations beat the meter — only the plain, un-overlaid idle
    case (``fg_count == 0 and bg_count == 0 and state == "idle"``) swaps in
    :func:`idle_usage_meter`'s ramp glyph + severity colour. Every other case
    returns ``(resolve_tab_icon(...), "")`` — an empty colour, so callers never
    wrap the existing waiting/active/sub-agent glyphs in a stray ``#[fg=...]``
    (design.md § "API shape: additive wrapper, `resolve_tab_icon` untouched").
    """
    if fg_count == 0 and bg_count == 0 and state == "idle":
        return idle_usage_meter(raw_tokens, now)
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
    design.md § Staleness) feed the glyph directly at ``n=10``, which itself
    renders in a neutral/unstyled colour (design.md § Color — no severity
    ramp on the glyph). ``raw_tokens`` selects both the SES label's text via
    :func:`format_context_tokens` and its colour via
    :func:`_context_color_pair`'s 6-tier severity ramp — base colour only,
    the pulse variant is deliberately ignored, so the label is a STATIC
    per-tier colour that swaps as ``raw_tokens`` crosses thresholds and
    never blinks on the wall clock. five_h_pct
    / seven_d_pct are utilization ratios in 0..1 (or None when unpolled ->
    '--' in DIM for the text; also feed the glyph).

    Pure function of its inputs (no tmux/subprocess). Empty model_letter /
    project / branch fields drop out of the left side (fail-open).
    """
    left_parts: List[str] = []
    if model_letter:
        left_parts.append(f"#[fg={CYAN}]{model_letter}")
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
    usage_glyph = render_usage_glyph(ses_pct, five_h_pct, seven_d_pct, n=10)
    # Right side: SES label, then 5H, then 7D, then the combined usage glyph
    # LAST (design.md § Decision 2). The account-identity segment + its
    # #[range=user|accounts] click marker moved off this row to row 3
    # (render_beads_bar).
    right = (
        f"#[fg={ses_color}]{ses_label}:#[default] "
        f"#[fg={DIM}]5H:#[fg={c5}]{p5}#[default] "
        f"#[fg={DIM}]7D:#[fg={c7}]{p7}#[default]{usage_glyph}"
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


def render_tabs_row(windows: Sequence[object], active_window_id: str, now: float) -> str:
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

        is_active = active_window_id and getattr(w, "id", None) == active_window_id
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
    return "".join(segments)


# Row 3 stale threshold: the roadmap-pulse cache is written under a ~5-minute
# SWR contract (rules/TOOLING.md Ambient Surfacing); 15 minutes = three missed
# refresh cycles, at which point the counts get a trailing age marker so stale
# data never masquerades as current (plan 006 / BEADS-01).
BEADS_STALE_AFTER_SEC = 900.0

# Row 3 "high count" thresholds for the unarchived-proposal / blocked-bead
# halves (cc-tmux-row3-openspec-beads-format task 2.3): 5 is roughly a day's
# worth of shipped-but-unarchived specs, or blocked beads piling up, before
# it stops being "a couple things to clean up next session" and becomes a
# RED alarm. Any count > 0 is already YELLOW; these constants only gate the
# YELLOW -> RED escalation, mirroring BEADS_STALE_AFTER_SEC's
# documented-constant convention above.
BEADS_UNARCHIVED_HIGH = 5
BEADS_BLOCKED_HIGH = 5

_BEADS_SEP = f"#[fg={DIM}] | "


def _threshold_color(n: int, high: int) -> str:
    """DIM at ``0`` (healthy), YELLOW above ``0``, RED at/above ``high``."""
    if n <= 0:
        return DIM
    if n >= high:
        return RED
    return YELLOW


def _pulse_segment(
    label: str,
    n1: int,
    suffix1: str,
    n2: int,
    suffix2: str,
    n3: int,
    suffix3: str,
    age_sec: Optional[float],
    high: int,
) -> str:
    """Colored ``"label: N1suffix1 N2suffix2 N3suffix3 (age)"`` segment.

    Renders cc's abbreviated roadmap-pulse shape (if-bqw.1) — e.g.
    ``"op: 1o 0ip 0ua"`` or ``"bd: 1o 1r 0b"`` — where each count's suffix is
    glued directly onto the number (no space) and only groups are
    space-separated. ``n1``/``n2`` are purely informational and stay DIM.
    ``n3`` (closure-debt ``ua`` / ``blocked``) is the health signal, colored
    via :func:`_threshold_color`; its suffix reverts to DIM immediately
    after. ``age_sec`` beyond ``BEADS_STALE_AFTER_SEC`` appends a DIM
    trailing ``" (<duration>)"`` marker, independent per segment.
    """
    n3_color = _threshold_color(n3, high)
    age_suffix = ""
    if age_sec is not None and age_sec > BEADS_STALE_AFTER_SEC:
        age_suffix = f" ({format_duration(age_sec)})"
    return (
        f"#[fg={DIM}]{label}: {n1}{suffix1} {n2}{suffix2} "
        f"#[fg={n3_color}]{n3}#[fg={DIM}]{suffix3}{age_suffix}"
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
    next_text: Optional[str] = None,
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
    ``bd:`` line never blanks a valid ``op:`` half and vice versa. The
    account segment is likewise independent: when BOTH ``op:``/``bd:``
    triples are absent (no cache) but ``account_label`` is non-empty, the row
    renders ONLY the (right-aligned) account segment, not ``""``. All three
    omitted (no cache and no account label) -> ``""``, matching the row's
    original "no cache -> empty" contract.

    ``ua``/``blocked`` are colored by semantic threshold
    (:func:`_threshold_color`; DIM healthy, YELLOW above 0, RED at/above
    :data:`BEADS_UNARCHIVED_HIGH`/:data:`BEADS_BLOCKED_HIGH`); ``open``/
    ``in_progress``/``ready`` stay DIM (informational, not a health signal).
    ``openspec_age_sec``/``beads_age_sec`` are each independent cache-file
    ages in seconds — both halves read the SAME cache file's single mtime
    today (so callers typically pass the same value for both), but the
    per-segment marker is forward-compatible with a future per-half cache
    split (plan 006 / BEADS-01) with no further render.py change needed.
    Ages beyond ``BEADS_STALE_AFTER_SEC`` append a DIM trailing
    ``" (<duration>)"`` marker via :func:`format_duration`, independently per
    segment.

    **Row3-next-cycle** (``next_text``/``now``, cc-tmux-row3-next-cycle):
    ``now is None`` (the default) renders this row BYTE-IDENTICAL to the
    behavior documented above — no phase logic engages, no countdown glyph
    ever appears, and ``next_text`` is ignored entirely. This protects every
    existing caller/test that predates this feature and does not pass the two
    new params.

    When ``now`` IS provided, the left-flowing ``op:``/``bd:`` content above
    alternates on a wall-clock timer with ``next_text`` (the project's
    "what to do next" recommendation — see
    :func:`cc_tmux.cli._parse_roadmap_pulse_next`), gated by
    :func:`beads_bar_phase`:

    * Phase 0 (or ``next_text is None``, i.e. no next-line available this
      tick): the left side renders the ``op:``/``bd:`` segments exactly as
      documented above, prefixed with :func:`beads_bar_countdown_glyph`
      (DIM, showing time remaining in the current phase).
    * Phase 1 AND ``next_text`` is present: the left side renders
      ``next_text`` ALONE — no ``op:``/``bd:`` segments at all — also
      prefixed with the countdown glyph. The two are mutually exclusive;
      this row never shows both at once.
    * The countdown glyph is added ONLY when there is left-side content to
      prefix — a tick with no counts and no ``next_text`` renders no glyph
      either, preserving the "nothing available -> no left side" contract
      below.

    The right-aligned account-identity segment is completely independent of
    this cycle — it renders in every phase, with or without ``now``, exactly
    as documented above; the cycle only ever touches the left side.

    Pure function of its inputs (no tmux/subprocess).
    """
    left_segments: List[str] = []
    if (
        openspec_open is not None
        and openspec_in_progress is not None
        and openspec_ua is not None
    ):
        left_segments.append(
            _pulse_segment(
                "op", openspec_open, "o", openspec_in_progress, "ip", openspec_ua, "ua",
                openspec_age_sec, BEADS_UNARCHIVED_HIGH,
            )
        )
    if beads_open is not None and beads_ready is not None and beads_blocked is not None:
        left_segments.append(
            _pulse_segment(
                "bd", beads_open, "o", beads_ready, "r", beads_blocked, "b",
                beads_age_sec, BEADS_BLOCKED_HIGH,
            )
        )
    counts_left = _BEADS_SEP.join(left_segments)

    if now is None:
        # Byte-identical to pre-row3-next-cycle behavior — no phase logic,
        # no countdown glyph, next_text ignored.
        left = counts_left
    else:
        phase = beads_bar_phase(now)
        if phase == 1 and next_text is not None:
            phase_content = next_text
        else:
            phase_content = counts_left
        if phase_content:
            glyph = beads_bar_countdown_glyph(now)
            left = f"#[fg={DIM}]{glyph}#[default] {phase_content}"
        else:
            left = ""

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
