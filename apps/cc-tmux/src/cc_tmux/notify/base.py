"""Strategy protocol shared by every per-OS notifier (Req-6).

A platform module (:mod:`.macos`, :mod:`.linux`, :mod:`.windows`) provides three
module-level functions matching :class:`Notifier`. Every one MUST fail open — a
missing OS tool is a silent no-op, never an exception — so a Claude hook can
never be blocked by the notification path (design.md invariant 5).
"""

from __future__ import annotations

from typing import Optional, Protocol


class Notifier(Protocol):
    """The per-OS surface the dispatcher in :mod:`cc_tmux.notify` calls."""

    def send_notification(self, title: str, message: str, *, pane_id: str) -> None:
        """Raise an OS notification. No-op when the platform tool is absent."""
        ...

    def focus_terminal(self, *, pane_id: str) -> None:
        """Bring the terminal (and, on macOS, its tab) to the foreground."""
        ...

    def is_terminal_frontmost(self) -> Optional[bool]:
        """Whether the terminal is already frontmost.

        ``True``/``False`` when determinable; ``None`` when unknown. The
        dispatcher treats ``None`` as "do not suppress" (better to over-notify
        than silently drop a transition).
        """
        ...
