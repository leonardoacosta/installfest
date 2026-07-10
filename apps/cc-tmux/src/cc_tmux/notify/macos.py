"""macOS notifier (Req-6): AppleScript notification + optional terminal-notifier.

Fail open everywhere: no ``osascript`` -> silent no-op; ``terminal-notifier`` is
used for click-to-focus only when present, otherwise a plain AppleScript
``display notification`` is emitted.
"""

from __future__ import annotations

import os
import shutil
import subprocess
from typing import List, Optional

from .. import log

# TERM_PROGRAM value -> AppleScript application name.
_TERM_APP = {
    "iTerm.app": "iTerm2",
    "iTerm2": "iTerm2",
    "Apple_Terminal": "Terminal",
    "ghostty": "Ghostty",
    "Ghostty": "Ghostty",
    "WezTerm": "WezTerm",
    "Alacritty": "Alacritty",
    "kitty": "kitty",
}

# AppleScript application name -> bundle id (for terminal-notifier -activate).
_BUNDLE_ID = {
    "iTerm2": "com.googlecode.iterm2",
    "Terminal": "com.apple.Terminal",
    "Ghostty": "com.mitchellh.ghostty",
    "WezTerm": "com.github.wez.wezterm",
    "Alacritty": "io.alacritty",
    "kitty": "net.kovidgoyal.kitty",
}


def _run(cmd: List[str]) -> bool:
    try:
        subprocess.run(cmd, capture_output=True, text=True, timeout=5)
        return True
    except (OSError, subprocess.SubprocessError) as exc:
        log.warn("macos notifier cmd failed: %s", exc)
        return False


def _osascript(script: str) -> Optional[str]:
    if shutil.which("osascript") is None:
        return None
    try:
        proc = subprocess.run(
            ["osascript", "-e", script], capture_output=True, text=True, timeout=5
        )
    except (OSError, subprocess.SubprocessError):
        return None
    if proc.returncode != 0:
        return None
    return proc.stdout.strip()


def _terminal_app() -> Optional[str]:
    tp = os.environ.get("TERM_PROGRAM", "").strip()
    if not tp:
        return None
    return _TERM_APP.get(tp, tp)


def _quote(s: str) -> str:
    """Escape a string for embedding inside an AppleScript double-quoted literal."""
    return s.replace("\\", "\\\\").replace('"', '\\"')


def send_notification(title: str, message: str, *, pane_id: str) -> None:
    app = _terminal_app()
    bundle = _BUNDLE_ID.get(app or "", "")
    if shutil.which("terminal-notifier") is not None:
        cmd = ["terminal-notifier", "-title", title, "-message", message]
        if bundle:
            cmd += ["-activate", bundle]
        if _run(cmd):
            return
    # Fallback: plain AppleScript notification (no click-to-focus).
    _osascript(
        f'display notification "{_quote(message)}" with title "{_quote(title)}"'
    )


def focus_terminal(*, pane_id: str) -> None:
    app = _terminal_app()
    if not app:
        return
    _osascript(f'tell application "{_quote(app)}" to activate')


def is_terminal_frontmost() -> Optional[bool]:
    front = _osascript(
        "tell application \"System Events\" to get name of first application process "
        "whose frontmost is true"
    )
    if front is None:
        return None  # cannot determine -> do not suppress
    app = _terminal_app()
    if not app:
        return None
    front_l = front.lower()
    app_l = app.lower()
    return app_l in front_l or front_l in app_l
