"""Claude multi-account usage status segment (Req-8, task 1.9).

Queries the local ``nexus-agent`` credentials endpoint and renders the active
account's 5-hour / 7-day utilization as a tmux ``status-right`` segment.

Originally a clean-room reimplementation of the retired ``tmux-nexus-creds`` sh
script targeting an OLDER nexus-agent response shape (top-level ``active_account``
string + ``accounts[]`` with nested ``five_hour.utilization`` / ``seven_day.
utilization`` floats, served on port 7402). Re-verified live 2026-07-11 against
the actual running nexus-agent and found BOTH the port and the schema had moved
on without this file — the real service listens on **7400**, and the payload
shape is now:

    {"credentials": [{"isActive": bool, "accountName": str, "accountEmail": str,
                       "usage5hUsed": float|None, "usage5hLimit": float|None,
                       "usage7dUsed": float|None, "usage7dLimit": float|None,
                       ...}, ...],
     "activeFingerprint": str}

(no ``active_account``/``accounts``/nested ``five_hour`` objects anywhere).
Fixed to match the current shape:

* Query ``http://localhost:7400/credentials`` with a 1s timeout.
* Find the credential with ``isActive is True``; if none -> output nothing.
* Label from ``accountName`` (falls back to ``accountEmail``, then ``name``).
* Utilization = ``usage5hUsed / usage5hLimit`` (and the 7d equivalent) when both
  are present and the limit is nonzero, else absent — nexus-agent only starts
  populating these once it has actually polled the account (``usagePolledAt``
  non-null); an unpolled account is expected to render ``--`` for both windows,
  not a bug in this file.
* Colour: absent -> DIM, ``> 0.80`` -> RED, ``>= 0.50`` -> YELLOW, else CYAN.
* Percent: absent -> ``--``, else ``round(util*100)`` + ``%``.
* Emit ``#[fg=…]<acct> #[fg=…]5H:#[fg=…]<pct>#[default] #[fg=…]7D:…#[default]``
  with NO trailing newline (visual format preserved from the original sh script).

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

# nexus-agent credentials endpoint + query timeout (seconds). Port 7400 is the
# REAL listening port (confirmed live via `ss -tlnp`) — 7402 (this file's prior
# value) has never been correct against the current nexus-agent.
CREDENTIALS_URL = "http://localhost:7400/credentials"
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


def _extract_util(credential: dict, used_key: str, limit_key: str) -> Optional[float]:
    """``<used_key> / <limit_key>`` as a 0..1 float, or ``None`` when unusable.

    Mirrors the old jq ``// empty`` semantics on the new flat field pair: a
    missing/``null``/non-numeric ``used`` or ``limit``, or a ``limit <= 0``, all
    map to ``None`` (the "not polled yet / nothing to show" case — nexus-agent
    leaves both null until it has actually polled that account).
    """
    used = credential.get(used_key)
    limit = credential.get(limit_key)
    if isinstance(used, bool) or isinstance(limit, bool):
        return None
    try:
        used_f = float(used)
        limit_f = float(limit)
    except (TypeError, ValueError):
        return None
    if limit_f <= 0:
        return None
    return used_f / limit_f


def _account_label(credential: dict) -> str:
    """Human-readable account label, preferring name over email over the raw id."""
    for key in ("accountName", "accountEmail", "name"):
        value = credential.get(key)
        if isinstance(value, str) and value:
            return value
    return ""


def render_usage(payload: dict) -> str:
    """Render the tmux segment from a parsed credentials payload, or ``''``.

    Returns the empty string whenever there's nothing sensible to show: no
    ``credentials`` list, no credential with ``isActive is True``, or a
    malformed payload shape.
    """
    credentials = payload.get("credentials")
    if not isinstance(credentials, list):
        return ""

    active = None
    for candidate in credentials:
        if isinstance(candidate, dict) and candidate.get("isActive") is True:
            active = candidate
            break
    if active is None:
        return ""

    label = _account_label(active)
    if not label:
        return ""

    util_5h = _extract_util(active, "usage5hUsed", "usage5hLimit")
    util_7d = _extract_util(active, "usage7dUsed", "usage7dLimit")

    c5 = color_for(util_5h)
    c7 = color_for(util_7d)
    p5 = pct_for(util_5h)
    p7 = pct_for(util_7d)

    # Exact byte layout of the original sh printf (no trailing newline).
    return (
        f"#[fg={DIM}]{label} "
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
