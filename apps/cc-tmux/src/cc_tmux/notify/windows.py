"""Windows notifier (Req-6): PowerShell toast.

Fail open: no PowerShell -> silent no-op. Terminal focus and frontmost detection
are not implemented (no dependable pane->window map from a tmux session running
under WSL/ConPTY), so focus is a best-effort no-op and frontmost is unknown.
"""

from __future__ import annotations

import shutil
import subprocess
from typing import List, Optional

from .. import log


def _pwsh() -> Optional[str]:
    return shutil.which("pwsh") or shutil.which("powershell")


def _run(cmd: List[str]) -> bool:
    try:
        subprocess.run(cmd, capture_output=True, text=True, timeout=10)
        return True
    except (OSError, subprocess.SubprocessError) as exc:
        log.warn("windows notifier cmd failed: %s", exc)
        return False


def _ps_quote(s: str) -> str:
    """Escape for a PowerShell single-quoted literal (double the quote)."""
    return s.replace("'", "''")


def _toast_script(title: str, message: str) -> str:
    t = _ps_quote(title)
    m = _ps_quote(message)
    # WinRT toast; wrapped so any failure (missing assembly) is swallowed.
    return (
        "try {"
        "[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, "
        "ContentType=WindowsRuntime] > $null;"
        "$t=[Windows.UI.Notifications.ToastNotificationManager]::GetTemplateContent("
        "[Windows.UI.Notifications.ToastTemplateType]::ToastText02);"
        "$n=$t.GetElementsByTagName('text');"
        f"$n.Item(0).AppendChild($t.CreateTextNode('{t}')) > $null;"
        f"$n.Item(1).AppendChild($t.CreateTextNode('{m}')) > $null;"
        "$toast=[Windows.UI.Notifications.ToastNotification]::new($t);"
        "[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier("
        "'cc-tmux').Show($toast);"
        "} catch {}"
    )


def send_notification(title: str, message: str, *, pane_id: str) -> None:
    ps = _pwsh()
    if ps is None:
        return
    _run([ps, "-NoProfile", "-NonInteractive", "-Command", _toast_script(title, message)])


def focus_terminal(*, pane_id: str) -> None:
    # No dependable pane->window map on Windows terminals; best-effort no-op.
    return None


def is_terminal_frontmost() -> Optional[bool]:
    return None
