"""Claude multi-account usage data for the status rows (nx-agent /credentials).

Queries the local ``nexus-agent`` credentials endpoint for the active
account's 5-hour / 7-day utilization, consumed by row 2
(:func:`extract_active` / :func:`active_usage`) and the accounts popup.

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

**Invariant 5 (fail open):** ANY failure — unreachable agent, HTTP error, JSON
parse error, missing fields, timeout — produces empty output and exit 0. It never
raises, so the tmux ``#(…)`` call is silent on failure exactly like the original.

No external dependencies (no ``curl`` / ``jq`` / ``bc`` subprocess): urllib + json.
"""

from __future__ import annotations

import json
import os
import tempfile
import time
import urllib.request
from datetime import datetime, timezone
from typing import Dict, List, Optional, Tuple

# tmux colour codes — identical to the retired tmux-nexus-creds sh script.
DIM = "#454D54"
CYAN = "#5BD1B9"
YELLOW = "#FAC760"
RED = "#E61F44"

# Git status glyph colours — sourced from home/dot_config/tmux/vercel-theme.conf's
# documented Vercel Geist Dark palette block (Blue / Green rows).
GREEN = "#00ac3a"
BLUE = "#006efe"

# Context-window bar colour tiers (cc-tmux-context-bar, Leo's ask 2026-07-13) —
# escalation shades beyond YELLOW/RED for the raw-token-count colour ramp.
# ORANGE sits between YELLOW and RED; BRIGHT_RED/DARK_RED are the two pulse
# partners for the >600k/>750k tiers (see render.resolve_context_color).
ORANGE = "#FF8C00"
BRIGHT_RED = "#FF6B6B"
DARK_RED = "#8B0000"

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


def _extract_reset_at(credential: dict, key: str) -> Optional[float]:
    """Epoch-seconds reset time for ``key`` (e.g. ``usage5hResetAt``), or ``None``.

    nx-agent serialises the reset columns (``usage_5h_reset_at`` /
    ``usage_7d_reset_at``, see ``packages/db/src/schema/credentials.ts`` in the
    nexus repo) as ISO-8601 strings, ``Z``- or offset-suffixed. A missing,
    non-string, or unparseable value -> ``None`` (fail-open, same contract as
    :func:`_extract_util`) — nexus-agent leaves the column null until it has
    actually polled the account, which is the expected "nothing to show" case,
    not a bug here.
    """
    value = credential.get(key)
    if not isinstance(value, str) or not value:
        return None
    try:
        parsed = datetime.fromisoformat(value.replace("Z", "+00:00"))
    except ValueError:
        return None
    if parsed.tzinfo is None:
        parsed = parsed.replace(tzinfo=timezone.utc)
    return parsed.timestamp()


def _account_label(credential: dict) -> str:
    """Account label: full email + first 8 chars of the org id, e.g. ``leo@x.dev·bc7da511``.

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
            return f"{email}·{org_uuid[:8]}"
        return email
    for key in ("accountName", "name"):
        value = credential.get(key)
        if isinstance(value, str) and value:
            return value
    return ""


def _account_identity(credential: dict) -> Tuple[str, str]:
    """``(display_name, org_id_short)`` for the popup's identity sub-row.

    ``display_name`` mirrors :func:`_account_label`'s own email-first,
    ``accountName``/``name``-fallback chain — so the sub-row never shows a
    blank identity when :func:`_account_label` itself wouldn't — but WITHOUT
    baking in the org-uuid-suffix: that stays :func:`_account_label`'s job
    alone, since it is the unique key :func:`render.render_accounts_popup`
    matches ``active_label`` against (the same email can be authenticated
    against multiple orgs, so email-alone is not a safe matching key — see
    :func:`_account_label`'s docstring). ``org_id_short`` is the first 8
    chars of ``orgUuid`` (Leo's 2026-07-13 ask), or ``""`` when absent — the
    caller omits the org segment entirely when this is empty, same
    fail-open convention as the reset-time lines.
    """
    email = credential.get("accountEmail")
    if isinstance(email, str) and email:
        name = email
    else:
        name = ""
        for key in ("accountName", "name"):
            value = credential.get(key)
            if isinstance(value, str) and value:
                name = value
                break
    org_uuid = credential.get("orgUuid")
    org_short = org_uuid[:8] if isinstance(org_uuid, str) and org_uuid else ""
    return name, org_short


# Credentials query
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


def _freshest_active(deduped: List[dict]) -> Optional[dict]:
    """Freshest ``isActive: True`` row in ``deduped``, or ``None`` if there is none.

    :func:`dedupe_credentials` only collapses duplicates WITHIN an
    ``(accountEmail, orgUuid)`` group — it cannot merge across groups, so the
    SAME email authenticated against two different orgs can independently
    survive dedupe as two separate ``isActive: True`` rows (if-lh9u, confirmed
    live 2026-07-14: one org's row stale since a prior day, the other current,
    both ``isActive: True`` post-dedupe). Picking the first such row in list
    order — the pre-fix behaviour — can silently render a stale, no-longer-
    current org's numbers.

    Resolves the cross-group tie with the SAME "data-presence-first, then
    recency, then last-wins" tie-break :func:`dedupe_credentials` already
    applies to its own WITHIN-group ``isActive`` ties (see its ``new_polled``/
    ``old_polled``/``new_has_ts``/``old_has_ts`` block) — this is a second
    application of that pattern across groups, not a new rule.
    """
    best: Optional[dict] = None
    for candidate in deduped:
        if candidate.get("isActive") is not True:
            continue
        if best is None:
            best = candidate
            continue

        new_polled = candidate.get("usagePolledAt")
        old_polled = best.get("usagePolledAt")
        new_has_ts = isinstance(new_polled, str) and bool(new_polled)
        old_has_ts = isinstance(old_polled, str) and bool(old_polled)

        if new_has_ts and old_has_ts:
            if new_polled >= old_polled:
                best = candidate
        elif new_has_ts and not old_has_ts:
            best = candidate
        elif old_has_ts and not new_has_ts:
            pass
        else:
            best = candidate
    return best


def extract_active(payload: dict) -> Tuple[str, Optional[float], Optional[float]]:
    """``(label, 5H util, 7D util)`` for the active credential, or ``('', None, None)``.

    Runs :func:`dedupe_credentials` first (fixed 2026-07-13, row2/popup usage
    mismatch). Before this fix, the raw ``credentials`` list was scanned
    directly for the first ``isActive is True`` row in payload order — but
    nx-agent's ``/credentials`` payload carries duplicate rows per identity
    (if-lp8v/if-m5q6), and the accounts-popup (:func:`cc_tmux.cli.
    cmd_accounts_popup`) already ran every row through
    :func:`dedupe_credentials`'s freshest-wins tie-break before picking the
    active one. The two surfaces could therefore resolve DIFFERENT rows for
    the same identity — row2 (this function, via :func:`active_usage`)
    reading a stale duplicate's 5H/7D while the popup read the freshest one
    — confirmed live: same account, row2 showed 5H:90%/7D:64%, the popup's
    starred row showed 5H:36%/7D:71%. Routing through the same dedupe here
    closes that drift class, mirroring the earlier if-hrbd fix that unified
    the two surfaces' SES resolution onto one function
    (:func:`cc_tmux.cli._resolve_ses_pct`) for the identical reason.

    :func:`dedupe_credentials` can still leave MORE THAN ONE ``isActive: True``
    row when the same identity is split across ``orgUuid``s (if-lh9u) —
    :func:`_freshest_active` resolves that residual tie by ``usagePolledAt``
    recency instead of picking the first list match.
    """
    if not isinstance(payload, dict):
        return "", None, None
    credentials = payload.get("credentials")
    if not isinstance(credentials, list):
        return "", None, None
    deduped = dedupe_credentials(credentials)
    active = _freshest_active(deduped)
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

    Before grouping, drops orphaned junk rows outright: no ``accountEmail``
    AND ``status == "refresh_failed"``. Confirmed live (2026-07-13, GET
    localhost:7400/credentials) that this exact shape — 20 of 107 rows,
    each with a distinct auto-generated ``acct-XXXXXXXX`` name and its own
    unique ``duplicateGroupId`` (nx's per-row fingerprint, NOT a shared
    dedupe key — verified none of the 20 collapse against each other) —
    are dead OAuth attempts that never linked to a real account, not
    duplicates of a live one. The email-less grouping fallback above
    cannot merge these away since each row's own generated name IS its
    fallback key, so without this pre-filter every one of them survives
    as a distinct fake "account" in the popup (if-lp8v/if-m5q6's
    nexus-agent-side bloat leaking straight through the client-side
    stopgap). This is still a view-layer drop, not a fix to nx's own
    accumulation — the real prune belongs server-side per those beads.
    A row WITH an email is never dropped by this check, even if transiently
    ``refresh_failed`` — only genuinely identity-less junk qualifies.

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
    recency tie-break apply, and it is data-presence-first, not
    list-position-first (fixed 2026-07-13): whichever side actually HAS a
    usable ``usagePolledAt`` wins over a side that doesn't, before falling
    back to comparing two real timestamps or, only when NEITHER side has
    one, "last one wins" (payload's own list order presumed oldest-to-newest).
    The prior version treated "either side missing a timestamp" as one
    undifferentiated case and always took the newer list entry regardless of
    which side actually had data — since the real payload interleaves
    genuinely-polled rows (real usage figures) with
    ``status: "refresh_failed"`` junk duplicates (all-null, no timestamp) for
    the SAME ``(accountEmail, orgUuid)`` in no guaranteed order, that bug let
    a later junk row silently erase an earlier row's real 5H/7D data —
    confirmed live (2026-07-13): the accounts popup showed blank usage for
    two real, non-active accounts that DO have real polled data in the raw
    payload, exactly this shape.

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
        if not (isinstance(email, str) and email) and candidate.get("status") == "refresh_failed":
            continue  # orphaned junk row, never linked to a real account — drop, don't group
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
        new_has_ts = isinstance(new_polled, str) and bool(new_polled)
        old_has_ts = isinstance(old_polled, str) and bool(old_polled)

        if new_has_ts and old_has_ts:
            if new_polled >= old_polled:
                groups[key] = candidate
        elif new_has_ts and not old_has_ts:
            # Candidate carries real polled data, existing doesn't -- prefer data.
            groups[key] = candidate
        elif old_has_ts and not new_has_ts:
            # Existing already carries real polled data; candidate is an
            # unpolled/refresh_failed duplicate -- keep it, don't let a later
            # junk row erase real usage data (this was the bug).
            pass
        else:
            # Neither side has a usable timestamp -> no basis to prefer
            # either; last one wins (list order presumed oldest-to-newest).
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


