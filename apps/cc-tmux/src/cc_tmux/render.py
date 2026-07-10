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
from typing import Callable, Dict, List, Sequence, Tuple

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
