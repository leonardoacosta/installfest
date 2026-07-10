"""Lightweight logging for cc-tmux.

Fail-open contract (design.md invariant 5): a hook or CLI subcommand must never
crash Claude or a tmux keybinding. Logging goes to stderr only when explicitly
enabled via ``CC_TMUX_DEBUG`` so the normal path is silent, and every log call
is itself guarded so a logging failure can never propagate.

There is intentionally no log FILE: pane options are the only state store
(invariant 1); a log file would be a parallel store to reason about. Debug
output is transient stderr, opt-in per invocation.
"""

from __future__ import annotations

import os
import sys
from typing import Any

_DEBUG_ENV = "CC_TMUX_DEBUG"


def _enabled() -> bool:
    val = os.environ.get(_DEBUG_ENV, "").strip().lower()
    return val not in ("", "0", "false", "no", "off")


def debug(msg: str, *args: Any) -> None:
    """Emit a debug line to stderr when CC_TMUX_DEBUG is truthy. Never raises."""
    if not _enabled():
        return
    try:
        rendered = msg % args if args else msg
        sys.stderr.write(f"[cc-tmux] {rendered}\n")
    except Exception:
        # A logging failure must never break the caller.
        pass


def warn(msg: str, *args: Any) -> None:
    """Emit a warning line to stderr when debug is enabled. Never raises."""
    if not _enabled():
        return
    try:
        rendered = msg % args if args else msg
        sys.stderr.write(f"[cc-tmux] WARN {rendered}\n")
    except Exception:
        pass
