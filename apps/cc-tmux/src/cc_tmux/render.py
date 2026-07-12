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
from typing import Callable, Dict, List, Optional, Sequence, Tuple

from .usage import CYAN, DIM, YELLOW, color_for, pct_for

# Default state glyphs. Geometric Shapes (U+25CF/25CB/25D0), NOT emoji — plain
# monospace-friendly marks that render in any terminal and are overridable.
DEFAULT_ICONS: Dict[str, str] = {
    "waiting": "●",  # ● filled — needs attention
    "idle": "○",     # ○ hollow — done / ready
    "active": "◐",   # ◐ half — working
}

# Default @cc-status-format: "icon count" per state, highest attention first.
DEFAULT_STATUS_FORMAT = "{waiting:icon} {idle:icon} {active:icon}"

_TOKEN_RE = re.compile(r"\{(\w+):icon\}")

# ---------------------------------------------------------------------------
# Animated window-tab icon (Req: animated tab icon)
#
# The literal window NAME (set via `rename-window`) only changes on discrete
# Claude Code hook events — irregular, sometimes minutes apart, sometimes
# bursty — so it cannot drive a believable animation on its own. Real motion
# needs a wall-clock-driven re-render, which tmux already provides for free
# via `window-status-format`/`window-status-current-format`: those are
# re-evaluated on every status-bar refresh (`status-interval`), independent of
# hook activity. `cli.cmd_window_icon` is invoked FROM that format string
# (`#(cc-tmux window-icon #{window_id})`), so :func:`animated_icon` picks a
# frame purely from the caller-supplied wall-clock time — no timer, no
# background process, same "daemon-free" invariant as the rest of this
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

# Seconds per frame. Matches the (default 1s-floor) status-interval driving
# re-renders — a shorter period than the actual refresh cadence would just
# mean some frames are silently skipped, which is harmless.
FRAME_PERIOD_SEC = 1.0


def animated_icon(state: str, now: float) -> str:
    """The tab-icon glyph for ``state`` at wall-clock ``now``.

    Pure function of its inputs (testable without a live clock or tmux) —
    :func:`cc_tmux.cli.cmd_window_icon` supplies the real ``time.time()``.
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


def render_status(fmt: str, counts: Dict[str, int], icons: Dict[str, str]) -> str:
    """Render ``@cc-status-format`` — ``{state:icon}`` -> "icon count" when > 0.

    A state with a zero count renders empty (the token drops out); leftover
    whitespace is collapsed so ``"● 2  ◐ 1"`` never has ragged gaps.
    """
    def _repl(match: "re.Match[str]") -> str:
        state = match.group(1)
        count = counts.get(state, 0)
        if count <= 0:
            return ""
        return f"{icons.get(state, state)} {count}"

    out = _TOKEN_RE.sub(_repl, fmt or "")
    return re.sub(r"\s+", " ", out).strip()


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
# Session / beads status rows (row 2 + row 3 — cc-tmux-session-usage-bars,
# corrected post cc-tmux-bar-cleanup)
#
# Both are *pure* composition functions. They emit tmux status-format strings
# using the same ``#[fg=…]``/``#[default]`` escaping convention as
# :func:`cc_tmux.usage.render_usage`, reusing that module's ``CYAN``/``DIM``
# colour constants. The CLI handlers (``cmd_session_bar``/``cmd_beads_bar``)
# read tmux/cache state and hand plain values in — nothing here touches tmux or
# a subprocess.
#
# Claude usage stats (account label, SES/5H/7D gauges) render on row 2's
# right side, alongside the left-side session/model/git identity. Only row 1
# (the window-tabs `status-right`) stays usage-free — that part of
# cc-tmux-bar-cleanup was correct and stays; cleanup's removal of usage from
# row 2 itself was a live-testing regression, reverted here.
# ---------------------------------------------------------------------------

# Branch-name colour (purple), distinct from usage.py's util palette. Session
# glyph, model letter, project, and gauge labels reuse DIM/CYAN from usage.py.
BRANCH = "#B267E6"

# Session-count indicator: hollow when no tracked pane, filled at one, filled +
# count at two or more (design.md Testing: 0/1/2+ -> ``◌``/``◉``/``◉ N``).
SESSION_GLYPH_FILLED = "◉"
SESSION_GLYPH_HOLLOW = "◌"


def _session_glyph(session_count: int) -> str:
    """Session-count glyph: ``◌`` (0), ``◉`` (1), ``◉ N`` (2+)."""
    if session_count <= 1:
        return SESSION_GLYPH_HOLLOW if session_count <= 0 else SESSION_GLYPH_FILLED
    return f"{SESSION_GLYPH_FILLED} {session_count}"


def render_session_bar(
    session_count: int,
    model_letter: str,
    project: str,
    branch: str,
    account_label: str,
    ses_pct: Optional[float],
    five_h_pct: Optional[float],
    seven_d_pct: Optional[float],
    *,
    dirty: bool = False,
    ahead: int = 0,
) -> str:
    """Row-2 status-format string: session/model/git on the left, usage on the right.

    Left side: session-count glyph + model letter (sourced from
    session-context.<pane>.json, tracks mid-session /model switches) + project +
    git branch. When ``branch`` is non-empty, a YELLOW ``*`` marks a dirty
    worktree and a YELLOW ``^N`` marks N commits ahead of upstream — both
    dropped (fail-open) when no branch renders, so a marker never appears
    without the branch it describes. Right side: account label + SES:/5H:/7D:
    gauges, each coloured via color_for and formatted via pct_for. The two
    sides are joined with a #[align=right] directive so tmux fills the gap
    between them. ses_pct / five_h_pct / seven_d_pct are utilization ratios in
    0..1 (or None when unpolled -> '--' in DIM).

    Pure function of its inputs (no tmux/subprocess). Empty model_letter /
    project / branch fields drop out of the left side (fail-open).
    """
    left_parts = [f"#[fg={DIM}]{_session_glyph(session_count)}"]
    if model_letter:
        left_parts.append(f"#[fg={CYAN}]{model_letter}")
    if project:
        left_parts.append(f"#[fg={DIM}]{project}")
    if branch:
        left_parts.append(f"#[fg={DIM}]>")
        seg = f"#[fg={BRANCH}]{branch}"
        if dirty:
            seg += f"#[fg={YELLOW}]*"
        if ahead > 0:
            seg += f"#[fg={YELLOW}]^{ahead}"
        left_parts.append(seg)
    left = " ".join(left_parts) + "#[default]"

    cs, c5, c7 = color_for(ses_pct), color_for(five_h_pct), color_for(seven_d_pct)
    ps, p5, p7 = pct_for(ses_pct), pct_for(five_h_pct), pct_for(seven_d_pct)
    right = (
        f"#[fg={DIM}]{account_label} "
        f"#[fg={DIM}]SES:#[fg={cs}]{ps}#[default] "
        f"#[fg={DIM}]5H:#[fg={c5}]{p5}#[default] "
        f"#[fg={DIM}]7D:#[fg={c7}]{p7}#[default]"
    )
    return f"{left}#[align=right]{right}"


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
    """Row-1 status-format string: one ``index:icon name`` segment per window.

    ``windows`` is any sequence of objects with ``id``/``index``/``name``/
    ``state`` attributes (duck-typed via ``getattr``, matching this module's
    other pane/window-consuming functions — see :func:`inbox_rows`); the
    canonical source is :func:`cc_tmux.tmux.get_window_tabs`. ``state`` is the
    window's highest-priority tracked ``@cc-state``, or ``""`` for a window
    with no tracked Claude pane — that window renders with no icon (matches
    :func:`cmd_window_icon`'s existing "untracked window -> no icon" contract),
    just its bare ``index:name``. ``now`` is the caller-supplied wall-clock
    time (``time.time()`` in production) handed straight to
    :func:`animated_icon` for the animation frame — same invocation pattern
    :func:`cc_tmux.cli.cmd_window_icon` already uses, reused here per window
    rather than re-deriving the state->glyph mapping.

    The active window (``id == active_window_id``) renders bold CYAN; every
    other window renders DIM — the same semantic colour pair
    :func:`render_session_bar` uses for emphasis vs. identity text, reused
    here rather than inventing a third convention. No wrapping bg colour is
    applied (theme ``.conf`` files wrap the whole row, same as
    ``status-format[1]``/``[2]`` — see :func:`render_session_bar`).

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
        icon = animated_icon(state, now)
        icon_part = f"{icon} " if icon else ""
        index = getattr(w, "index", "")
        name = getattr(w, "name", "")
        label = f"{index} {icon_part}{name}"

        is_active = active_window_id and getattr(w, "id", None) == active_window_id
        colour = f"{CYAN},bold" if is_active else DIM
        segments.append(
            f"#[fg={colour}]#[range=window|{index}] {label} #[norange]#[default]"
        )
    return "".join(segments)


_BEADS_SEP = f"#[fg={DIM}] | "


def render_beads_bar(pulse_line: str) -> str:
    """Row-3 status-format string from roadmap-pulse cache content, or ``''``.

    ``pulse_line`` is the raw ``roadmap-pulse.<code>.line`` content, which may
    carry a ``"next: /apply cc-tmux-scout-adop… 0o 2u"`` line alongside a
    plain ``"12 open / 24 waiting"`` counts line. Any ``next:`` line is
    dropped entirely — row 3 shows only the counts line(s), never the
    ``next:`` content, regardless of ordering in the source file. Remaining
    non-``next:`` lines are joined onto one full-width row with a DIM ``|``
    separator (relevant only if there were ever more than one — in practice
    this is a single counts line). Single-line content renders exactly as
    before (no separator artifact). Returns the empty string for
    falsy/blank-only ``pulse_line``, or when the only content was a ``next:``
    line with no counts to show, so row 3 shows nothing when there's nothing
    pending.

    Pure function of its input (no tmux/subprocess).
    """
    if not pulse_line:
        return ""
    lines = [ln for ln in pulse_line.splitlines() if ln.strip()]
    if not lines:
        return ""

    other_lines = [ln for ln in lines if not ln.startswith("next:")]
    if not other_lines:
        return ""

    segments = [f"#[fg={DIM}]{ln}" for ln in other_lines]
    return _BEADS_SEP.join(segments) + "#[default]"
