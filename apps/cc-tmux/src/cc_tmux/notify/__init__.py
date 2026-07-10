"""OS-notification + terminal-focus dispatcher (Req-6).

The single entrypoint is :func:`react`, called by ``cmd_register`` right after
``set_pane_state``. It honors the three governing invariants:

* **Invariant 3 (real transition):** ``react`` fires notify/focus ONLY when the
  caller reports a real ``@cc-state`` change. A re-asserted state never re-yanks
  focus.
* **Smart-suppress:** when the terminal is already frontmost, both notify and
  focus are skipped (the user is already looking at it).
* **Per-pane cooldown + fingerprint:** the OS-notification path dedups repeated
  notifications for the same ``(state, wait_reason)`` fingerprint within
  ``@cc-notify-cooldown`` seconds. The cooldown stamp lives in pane options
  (dies with the pane — no parallel store, invariant 1).

Platform selection is by :data:`sys.platform`; an unknown platform is a no-op.
Every path fails open (invariant 5).
"""

from __future__ import annotations

import sys
import time
from typing import Optional

from .. import log, tmux

# Global option lists (comma-separated states, default empty = feature off).
_NOTIFY_OPT = "@cc-notify"
_FOCUS_OPT = "@cc-focus-app"
_COOLDOWN_OPT = "@cc-notify-cooldown"
_DEFAULT_COOLDOWN = 30.0

# Per-pane cooldown breadcrumbs (kept out of tmux._ALL_OPTS on purpose — they are
# an ephemeral dedup stamp, not tracked state a view derives from; they die with
# the pane exactly like every other pane option).
_LAST_AT_OPT = "@cc-notify-last-at"
_LAST_FP_OPT = "@cc-notify-last-fp"


def _platform_module():
    """The notifier module for this OS, or ``None`` when unsupported."""
    plat = sys.platform
    try:
        if plat == "darwin":
            from . import macos

            return macos
        if plat.startswith("linux"):
            from . import linux

            return linux
        if plat.startswith("win") or plat == "cygwin":
            from . import windows

            return windows
    except Exception as exc:  # noqa: BLE001 - fail open on import failure
        log.warn("notify platform import failed: %s", exc)
    return None


def _state_list(option: str) -> set:
    """Parse a comma-separated ``@cc-*`` state list into a set (fail open -> {})."""
    raw = tmux.get_global_option(option)
    return {s.strip() for s in raw.split(",") if s.strip()}


def react(pane_id: str, state: str, changed: bool) -> None:
    """Fire notify/focus for a transition. No-op unless ``changed`` is True."""
    if not changed:
        return  # invariant 3: only real transitions react
    try:
        if state in _state_list(_NOTIFY_OPT):
            notify(pane_id, state)
        if state in _state_list(_FOCUS_OPT):
            focus_app(pane_id)
    except Exception as exc:  # noqa: BLE001 - fail-open boundary
        log.warn("notify.react failed: %s", exc)


def notify(pane_id: str, state: str) -> None:
    """Raise an OS notification for a pane, with suppression + cooldown."""
    mod = _platform_module()
    if mod is None:
        return
    try:
        if mod.is_terminal_frontmost() is True:
            return  # smart-suppress: user is already looking at the terminal
        fingerprint = _fingerprint(pane_id, state)
        if _in_cooldown(pane_id, fingerprint):
            return
        title, message = _compose(pane_id, state)
        mod.send_notification(title, message, pane_id=pane_id)
        _record(pane_id, fingerprint)
    except Exception as exc:  # noqa: BLE001 - fail open, never block Claude
        log.warn("notify failed: %s", exc)


def focus_app(pane_id: str) -> None:
    """Bring the terminal to the foreground, unless it is already frontmost."""
    mod = _platform_module()
    if mod is None:
        return
    try:
        if mod.is_terminal_frontmost() is True:
            return  # smart-suppress
        mod.focus_terminal(pane_id=pane_id)
    except Exception as exc:  # noqa: BLE001 - fail open
        log.warn("focus_app failed: %s", exc)


# ---------------------------------------------------------------------------
# cooldown / fingerprint helpers (pane options via the tmux choke point)
# ---------------------------------------------------------------------------

def _set_pane_opt(pane_id: str, option: str, value: str) -> None:
    tmux._run_tmux(["set-option", "-p", "-t", pane_id, option, value])


def _fingerprint(pane_id: str, state: str) -> str:
    reason = tmux.get_pane_option(pane_id, tmux.OPT_WAIT_REASON)
    return f"{state}:{reason}"


def _cooldown_secs() -> float:
    raw = tmux.get_global_option(_COOLDOWN_OPT)
    try:
        return float(raw) if raw else _DEFAULT_COOLDOWN
    except ValueError:
        return _DEFAULT_COOLDOWN


def _in_cooldown(pane_id: str, fingerprint: str) -> bool:
    """True when the same fingerprint fired within the cooldown window."""
    if tmux.get_pane_option(pane_id, _LAST_FP_OPT) != fingerprint:
        return False
    raw = tmux.get_pane_option(pane_id, _LAST_AT_OPT)
    try:
        last = float(raw) if raw else 0.0
    except ValueError:
        return False
    return (time.time() - last) < _cooldown_secs()


def _record(pane_id: str, fingerprint: str) -> None:
    _set_pane_opt(pane_id, _LAST_FP_OPT, fingerprint)
    _set_pane_opt(pane_id, _LAST_AT_OPT, str(time.time()))


def _compose(pane_id: str, state: str) -> "tuple[str, str]":
    """Build ``(title, message)`` from the pane's tracked identity."""
    project = tmux.get_pane_option(pane_id, tmux.OPT_PROJECT)
    task = tmux.get_pane_option(pane_id, tmux.OPT_TASK)
    reason = tmux.get_pane_option(pane_id, tmux.OPT_WAIT_REASON)
    title = f"Claude: {state}" + (f" ({reason})" if reason else "")
    parts = [x for x in (project, task) if x]
    message = "  —  ".join(parts) if parts else "Claude Code session"
    return title, message
