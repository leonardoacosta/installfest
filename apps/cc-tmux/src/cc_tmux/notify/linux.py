"""Linux notifier (Req-6): notify-send + best-effort wmctrl/xdotool focus.

Fail open everywhere: no ``notify-send`` -> silent no-op. Terminal focus is
best-effort (no reliable tmux-pane -> X/Wayland-window mapping exists), so it
raises the terminal by class/name when a tool is present and otherwise does
nothing. Frontmost detection is left unknown (``None``) so notifications are
never wrongly suppressed.
"""

from __future__ import annotations

import os
import shutil
import subprocess
from typing import List, Optional

from .. import log


def _run(cmd: List[str]) -> bool:
    try:
        subprocess.run(cmd, capture_output=True, text=True, timeout=5)
        return True
    except (OSError, subprocess.SubprocessError) as exc:
        log.warn("linux notifier cmd failed: %s", exc)
        return False


def send_notification(title: str, message: str, *, pane_id: str) -> None:
    if shutil.which("notify-send") is None:
        return
    _run(["notify-send", "--app-name=cc-tmux", title, message])


def focus_terminal(*, pane_id: str) -> None:
    # No dependable pane->window map; raise the terminal by name, best-effort.
    term = os.environ.get("TERM_PROGRAM", "").strip() or os.environ.get(
        "TERM", ""
    ).strip()
    if shutil.which("wmctrl") is not None:
        # -a matches window titles/classes case-insensitively; harmless if no match.
        _run(["wmctrl", "-a", term or "term"])
        return
    if shutil.which("xdotool") is not None:
        _run(
            [
                "xdotool",
                "search",
                "--limit",
                "1",
                "--class",
                term or "term",
                "windowactivate",
            ]
        )


def is_terminal_frontmost() -> Optional[bool]:
    # Reliable cross-compositor detection is not available; report unknown so the
    # dispatcher does not suppress.
    return None
