"""Claude multi-account usage status segment (Req-8, task 1.9).

Clean-room stdlib reimplementation of the retired ``tmux-nexus-creds`` sh script.
It queries the local ``nexus-agent`` credentials endpoint and renders the active
account's 5-hour / 7-day utilization as a tmux ``status-right`` segment, byte-for-
byte identical to the sh original so the status bar looks unchanged.

Behavior parity with the sh script (the Config batch asserts a live diff):

* Query ``http://localhost:7402/credentials`` with a 1s timeout.
* Read ``active_account``; if absent/empty -> output nothing.
* Find the matching entry in ``accounts[]``; if none -> output nothing.
* Per account read ``five_hour.utilization`` / ``seven_day.utilization``.
* Colour: absent -> DIM, ``> 0.80`` -> RED, ``>= 0.50`` -> YELLOW, else CYAN.
* Percent: absent -> ``--``, else ``round(util*100)`` + ``%``.
* Emit ``#[fg=…]<acct> #[fg=…]5H:#[fg=…]<pct>#[default] #[fg=…]7D:…#[default]``
  with NO trailing newline (the sh used ``printf`` without ``\n``).

**Invariant 5 (fail open):** ANY failure — unreachable agent, HTTP error, JSON
parse error, missing fields, timeout — produces empty output and exit 0. It never
raises, so the tmux ``#(…)`` call is silent on failure exactly like the original.

No external dependencies (no ``curl`` / ``jq`` / ``bc`` subprocess): urllib + json.
"""

from __future__ import annotations

import json
import sys
import urllib.request
from typing import Optional

# tmux colour codes — identical to the retired tmux-nexus-creds sh script.
DIM = "#454D54"
CYAN = "#5BD1B9"
YELLOW = "#FAC760"
RED = "#E61F44"

# nexus-agent credentials endpoint + query timeout (seconds).
CREDENTIALS_URL = "http://localhost:7402/credentials"
TIMEOUT_SECS = 1.0


# ---------------------------------------------------------------------------
# Pure presentation helpers (unit-tested by cc-tmux self-test)
# ---------------------------------------------------------------------------

def color_for(util: Optional[float]) -> str:
    """Colour for a utilization value, mirroring the sh ``color_for``.

    ``None`` (absent field, the jq ``// empty`` case) -> DIM; ``> 0.80`` -> RED;
    ``>= 0.50`` -> YELLOW; otherwise CYAN. A present ``0.0`` is CYAN, not DIM
    (only a genuinely absent field dims).
    """
    if util is None:
        return DIM
    if util > 0.80:
        return RED
    if util >= 0.50:
        return YELLOW
    return CYAN


def pct_for(util: Optional[float]) -> str:
    """Percent label for a utilization value, mirroring the sh ``pct_for``.

    ``None`` -> ``--``; otherwise ``util * 100`` rounded to a whole percent (the
    sh used ``printf '%.0f%%'``; Python's ``.0f`` shares its round-half-to-even
    rule, so realistic 2-decimal inputs match byte-for-byte).
    """
    if util is None:
        return "--"
    return f"{util * 100:.0f}%"


def _extract_util(account: dict, window_key: str) -> Optional[float]:
    """Read ``<window_key>.utilization`` as a float, or ``None`` when absent.

    Mirrors jq ``.<window>.utilization // empty``: a missing window, a missing
    ``utilization``, an explicit ``null``, or a ``false`` all map to ``None``
    (the "empty" case). A numeric ``0`` maps to ``0.0`` (present, not empty).
    """
    window = account.get(window_key)
    if not isinstance(window, dict):
        return None
    value = window.get("utilization")
    if value is None or isinstance(value, bool):
        return None
    try:
        return float(value)
    except (TypeError, ValueError):
        return None


def render_usage(payload: dict) -> str:
    """Render the tmux segment from a parsed credentials payload, or ``''``.

    Returns the empty string in every case where the sh script would ``exit 0``
    with no output: no active account, active account not found in ``accounts``,
    or a malformed payload shape.
    """
    active = payload.get("active_account")
    if not isinstance(active, str) or not active:
        return ""

    accounts = payload.get("accounts")
    if not isinstance(accounts, list):
        return ""

    account = None
    for candidate in accounts:
        if isinstance(candidate, dict) and candidate.get("name") == active:
            account = candidate
            break
    if account is None:
        return ""

    util_5h = _extract_util(account, "five_hour")
    util_7d = _extract_util(account, "seven_day")

    c5 = color_for(util_5h)
    c7 = color_for(util_7d)
    p5 = pct_for(util_5h)
    p7 = pct_for(util_7d)

    # Exact byte layout of the sh printf (no trailing newline).
    return (
        f"#[fg={DIM}]{active} "
        f"#[fg={DIM}]5H:#[fg={c5}]{p5}#[default] "
        f"#[fg={DIM}]7D:#[fg={c7}]{p7}#[default]"
    )


# ---------------------------------------------------------------------------
# Query + CLI handler
# ---------------------------------------------------------------------------

def _query(url: str = CREDENTIALS_URL, timeout: float = TIMEOUT_SECS) -> Optional[dict]:
    """Fetch + parse the credentials JSON, or ``None`` on any failure.

    Equivalent to ``curl -sf --max-time 1``: a non-2xx response (urllib raises
    ``HTTPError``) or any network/parse error yields ``None`` (fail open).
    """
    try:
        with urllib.request.urlopen(url, timeout=timeout) as resp:  # noqa: S310 - localhost only
            status = getattr(resp, "status", 200)
            if status is not None and status >= 400:
                return None
            raw = resp.read()
        parsed = json.loads(raw)
    except Exception:  # noqa: BLE001 - fail open on any error, like `curl -sf || exit 0`
        return None
    return parsed if isinstance(parsed, dict) else None


def build_segment() -> str:
    """Query nexus-agent and render the segment, or ``''`` on any failure."""
    payload = _query()
    if payload is None:
        return ""
    try:
        return render_usage(payload)
    except Exception:  # noqa: BLE001 - never let a shape surprise raise
        return ""


def cmd_usage(args) -> int:
    """Emit the Claude usage status segment (Req-8). Fail open: silent + exit 0.

    Writes with no trailing newline to byte-match the retired ``tmux-nexus-creds``
    ``printf`` output.
    """
    segment = build_segment()
    if segment:
        sys.stdout.write(segment)
    return 0
