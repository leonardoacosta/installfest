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
import os
import sys
import tempfile
import time
import urllib.request
from typing import Dict, List, Optional, Tuple

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
    """Account label: full email + last char of the org id, e.g. ``leo@x.dev·7``.

    A bare account name (``"Leo"``) isn't indicative enough — the SAME email
    can be authenticated against multiple orgs, so the full email disambiguates
    the account and the org-id suffix disambiguates which org it's currently
    authenticated against. Falls back to ``accountName``/``name`` (no suffix)
    when there's no email to anchor the org suffix to.
    """
    email = credential.get("accountEmail")
    if isinstance(email, str) and email:
        org_uuid = credential.get("orgUuid")
        if isinstance(org_uuid, str) and org_uuid:
            return f"{email}·{org_uuid[-1]}"
        return email
    for key in ("accountName", "name"):
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


# ---------------------------------------------------------------------------
# Cached active-usage (installfest plan 003)
#
# The session-bar row calls this at 1Hz per window (status-interval 1), but the
# /credentials payload is ~4MB and changes on a minutes scale. A short-TTL
# on-disk cache of the EXTRACTED (label, 5h, 7d) triple bounds the fetch to
# once per TTL instead of once per tick. This caches EXTERNAL HTTP data, not
# pane state — tmux pane options remain the only pane-state store (invariant 1;
# precedent: the roadmap-pulse / session-context cache files cli.py reads).
# Fail open everywhere: any cache error falls through to a live fetch; any
# write error is swallowed.
# ---------------------------------------------------------------------------

USAGE_CACHE_TTL_SECS = 45.0


def _cache_path() -> str:
    """Per-user cache file in the system temp dir (uid-suffixed, multi-user safe)."""
    uid = os.getuid() if hasattr(os, "getuid") else 0
    return os.path.join(tempfile.gettempdir(), f"cc-tmux-usage-cache.{uid}.json")


def extract_active(payload: dict) -> Tuple[str, Optional[float], Optional[float]]:
    """``(label, 5H util, 7D util)`` for the active credential, or ``('', None, None)``."""
    if not isinstance(payload, dict):
        return "", None, None
    credentials = payload.get("credentials")
    if not isinstance(credentials, list):
        return "", None, None
    active = next(
        (c for c in credentials if isinstance(c, dict) and c.get("isActive") is True),
        None,
    )
    if active is None:
        return "", None, None
    return (
        _account_label(active),
        _extract_util(active, "usage5hUsed", "usage5hLimit"),
        _extract_util(active, "usage7dUsed", "usage7dLimit"),
    )


# ---------------------------------------------------------------------------
# Credentials dedupe (cc-tmux-account-switcher-popup task 1.2/2.1)
#
# The /credentials payload is known to accumulate historical duplicate rows
# for the same account identity over time (if-lp8v/if-m5q6, nexus-agent-side
# bloat, still open — 2,709 rows observed on one machine). This is a
# self-contained CLIENT-SIDE view-layer stopgap, NOT a substitute for the
# real nexus-agent-side prune those beads track — once that lands, this
# becomes a no-op over an already-clean payload, not dead code to remove.
# ---------------------------------------------------------------------------


def dedupe_credentials(credentials: object) -> List[dict]:
    """Collapse duplicate ``(accountEmail, orgUuid)`` rows, most-recent kept.

    Groups by ``(accountEmail, orgUuid)`` when ``accountEmail`` is present
    (mirrors :func:`_account_label`'s primary identity); when it is absent,
    falls back to the label :func:`_account_label` would itself render (so
    distinct email-less accounts do not collapse into one bucket just because
    they both lack an email).

    Within a group, an ``isActive: True`` row ALWAYS wins over an
    ``isActive: False`` row, regardless of timestamps — confirmed live
    (2026-07-12) that a group can contain rows with genuinely DIFFERENT
    ``fingerprint``/``id`` values (distinct token-swap generations, not just
    repeated polls of one identity) sharing the same ``(accountEmail,
    orgUuid)``, and a stale sibling's ``usagePolledAt`` can be lexically
    LATER than the real active row's (observed: the active row polled at
    ``21:31:17.119Z``, a stale duplicate at ``21:31:17.154Z``, 35ms later) —
    letting recency alone decide would silently drop the one row this
    feature actually needs (the credential the accounts-popup marks SES on).
    Only when both candidates share the same ``isActive`` value does the
    recency tie-break apply: prefers whichever has the more recent
    ``usagePolledAt`` (an ISO-8601 string, lexically comparable) when BOTH
    carry one; otherwise "last one wins" — the payload's own list order is
    presumed oldest-to-newest, matching an accumulating-log shape, and
    ``usagePolledAt`` is frequently ``null`` on this machine (nexus-agent
    has never polled most accounts), so that fallback is the common path.

    Pure function, no HTTP — operates on the ``credentials`` list a caller
    already fetched via :func:`_query`. Non-list input -> ``[]``; non-dict
    entries within the list are skipped (fail-open, mirrors
    :func:`_extract_util`'s tolerance for malformed rows). Return order
    follows each group's first appearance in ``credentials``.
    """
    if not isinstance(credentials, list):
        return []

    order: List[Tuple[str, Optional[str]]] = []
    groups: Dict[Tuple[str, Optional[str]], dict] = {}
    for candidate in credentials:
        if not isinstance(candidate, dict):
            continue
        email = candidate.get("accountEmail")
        org = candidate.get("orgUuid")
        org_key = org if isinstance(org, str) else None
        key = (
            (email, org_key)
            if isinstance(email, str) and email
            else (_account_label(candidate), org_key)
        )

        existing = groups.get(key)
        if existing is None:
            order.append(key)
            groups[key] = candidate
            continue

        existing_active = existing.get("isActive") is True
        candidate_active = candidate.get("isActive") is True
        if candidate_active and not existing_active:
            groups[key] = candidate
            continue
        if existing_active and not candidate_active:
            continue  # never let a stale/inactive duplicate evict the live row

        new_polled = candidate.get("usagePolledAt")
        old_polled = existing.get("usagePolledAt")
        if (
            isinstance(new_polled, str) and new_polled
            and isinstance(old_polled, str) and old_polled
        ):
            if new_polled >= old_polled:
                groups[key] = candidate
        else:
            # No dependable timestamp on one/both sides -> last one wins.
            groups[key] = candidate

    return [groups[key] for key in order]


def _read_usage_cache(path: str, now: float, ttl: float):
    """Cached triple if ``path`` is fresh (|now - mtime| < ttl) and well-formed, else None."""
    try:
        age = now - os.stat(path).st_mtime
        if not (-ttl < age < ttl):
            return None
        with open(path, "r", encoding="utf-8") as f:
            data = json.load(f)
        if not isinstance(data, dict):
            return None
        label = data.get("label")
        if not isinstance(label, str):
            return None
        utils = []
        for key in ("u5", "u7"):
            value = data.get(key)
            if value is None:
                utils.append(None)
            elif isinstance(value, bool) or not isinstance(value, (int, float)):
                return None
            else:
                utils.append(float(value))
        return label, utils[0], utils[1]
    except Exception:  # noqa: BLE001 - fail open: unreadable cache -> live fetch
        return None


def _write_usage_cache(
    path: str, label: str, u5: Optional[float], u7: Optional[float]
) -> None:
    """Atomic (.tmp + os.replace) best-effort cache write; never raises."""
    tmp = f"{path}.tmp.{os.getpid()}"
    try:
        with open(tmp, "w", encoding="utf-8") as f:
            f.write(json.dumps({"label": label, "u5": u5, "u7": u7}))
        os.replace(tmp, path)
    except Exception:  # noqa: BLE001 - fail open: cache write is best-effort
        try:
            os.unlink(tmp)
        except Exception:  # noqa: BLE001
            pass


def active_usage(
    ttl: float = USAGE_CACHE_TTL_SECS,
    cache_path: Optional[str] = None,
    now: Optional[float] = None,
) -> Tuple[str, Optional[float], Optional[float]]:
    """Cached ``(label, 5H, 7D)`` for the active credential.

    Cache hit (fresh + well-formed) -> no HTTP. Miss/stale/corrupt -> live
    ``_query()`` fetch, extract, rewrite cache (INCLUDING the empty result on
    fetch failure — negative caching, so a down nexus-agent is probed once per
    TTL, not per tick). ``cache_path`` / ``now`` are injectable for self-test.
    """
    path = cache_path or _cache_path()
    t = time.time() if now is None else now
    cached = _read_usage_cache(path, t, ttl)
    if cached is not None:
        return cached
    payload = _query()
    result = extract_active(payload) if payload else ("", None, None)
    _write_usage_cache(path, *result)
    return result


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
