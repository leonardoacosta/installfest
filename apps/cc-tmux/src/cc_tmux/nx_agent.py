"""HTTP client for nx-agent's session-context + project-git-status endpoints.

`docs/explainers/nx-session-context-api-migration.md` (2026-07-13) documents that
nx retired the pane-keyed ``session-context.<pane_id>.json`` file cc-tmux used to
read for row 2's ``context_used_pct`` / ``branch`` / ``dirty`` fields, and shipped
two HTTP surfaces on nx-agent (port 7400 — the SAME host :mod:`usage` already
queries for ``/credentials``) in its place:

* ``GET /sessions/:id/context`` — session-id-keyed, in-memory, ~600s TTL. Carries
  ``usedPercentage`` / ``contextWindowSize`` / ``updatedAt`` / ``sessionId``
  (nx's ``SessionContextResponse`` type). No model tag.
* ``GET /projects/:id/status`` — project-code-keyed, backed by a 60s-polling
  git-observer. Its optional ``git`` sub-object carries ``branch`` / ``headSha`` /
  ``detached`` / ``dirty: {modified, untracked}`` / ``observedAt``.

This module is that client. It exposes :func:`session_context` and
:func:`project_git_status`, each of which:

* Reuses :func:`usage._query` for the actual fetch rather than duplicating urllib
  boilerplate — that helper already does 1s-timeout + status-code + JSON-parse +
  fail-open-``None`` exactly right, and is the single place the localhost-only
  ``# noqa: S310`` lives.
* Is backed by a short-TTL on-disk cache (:func:`_read_cache` / :func:`_write_cache`)
  so a render-tick-frequency caller (the row-2 status bar runs at ~1Hz) probes the
  network at most once per TTL, not once per tick. The cache is NEGATIVE — an
  unreachable-agent / 404 / malformed result is cached as JSON ``null`` for the TTL
  too, so a down nx-agent is probed once per TTL rather than every tick (mirrors
  :func:`usage.active_usage`'s documented "including the empty result on fetch
  failure" behavior).

Why a fresh cache pair here instead of reusing :mod:`usage`'s: ``usage``'s
``_read_usage_cache`` / ``_write_usage_cache`` are ``_``-private and hardcoded to a
``(label, u5, u7)`` triple shape — they cannot store an arbitrary JSON dict. This
module's pair is generic over any JSON-serializable dict (or ``None``), which is
what the two endpoint payloads need, so a small local pair is the right call over a
cross-cutting refactor of an already-shipped module.

**Fail open everywhere (invariant 5):** every function returns ``None`` on any
failure and NEVER raises. Any exception anywhere in the read / fetch / write path
results in ``None`` (and, where relevant, that ``None`` is cached as the negative
result). ``cache_path`` / ``now`` are injectable optional params (matching
:func:`usage.active_usage`'s convention) so a self-test can drive caching
deterministically without touching the real filesystem clock.

No external dependencies — stdlib only (json + os + re + tempfile + time), same as
:mod:`usage`.
"""

from __future__ import annotations

import json
import os
import re
import tempfile
import time
from typing import Optional

from . import usage

# nx-agent base + per-endpoint URL templates. Port 7400 is the REAL nx-agent
# listening port (see :mod:`usage` — ``/credentials`` is served from the same host).
_BASE_URL = "http://localhost:7400"

# Default cache TTL (seconds). Short enough that a render-tick caller sees fresh-ish
# data, long enough that a 1Hz status bar does not hit the network every tick.
CACHE_TTL_SECS = 5.0

# Chars allowed verbatim in a cache filename's key segment; anything else is
# replaced with ``_`` so a weird session_id / project code cannot collide with
# another key or escape the temp dir via path separators.
_KEY_SANITIZE_RE = re.compile(r"[^A-Za-z0-9_-]")


def _sanitize_key(key: str) -> str:
    """Replace any char outside ``[A-Za-z0-9_-]`` with ``_`` for safe filenames."""
    return _KEY_SANITIZE_RE.sub("_", key)


def _cache_path(kind: str, key: str) -> str:
    """Per-kind, per-key, uid-suffixed cache file in the system temp dir.

    e.g. ``cc-tmux-nx-context-cache.1000.abc123.json``. The uid suffix keeps it
    multi-user safe (mirrors :func:`usage._cache_path`); the sanitized key keeps
    distinct sessions/projects in distinct files without collision or escape.
    """
    uid = os.getuid() if hasattr(os, "getuid") else 0
    return os.path.join(
        tempfile.gettempdir(),
        f"cc-tmux-nx-{kind}-cache.{uid}.{_sanitize_key(key)}.json",
    )


def _read_cache(path: str, now: float, ttl: float) -> Optional[dict]:
    """Cached dict if ``path`` is fresh (|now - mtime| < ttl) and a well-formed dict.

    Returns ``None`` for a stale, missing, unreadable, or non-dict-JSON cache. Note
    a negatively-cached entry (JSON ``null``) parses to ``None`` and so also returns
    ``None`` here — the caller cannot distinguish "no fresh cache" from "cached
    negative result", which is intentional: both mean "do not use a cached dict".
    The freshness check on a negative entry is what still suppresses the fetch — see
    :func:`_fetch_cached`. Fail open: any error -> ``None`` (falls through to fetch).
    """
    try:
        age = now - os.stat(path).st_mtime
        if not (-ttl < age < ttl):
            return None
        with open(path, "r", encoding="utf-8") as f:
            data = json.load(f)
        return data if isinstance(data, dict) else None
    except Exception:  # noqa: BLE001 - fail open: unreadable/stale cache -> live fetch
        return None


def _write_cache(path: str, value: Optional[dict]) -> None:
    """Atomic (.tmp + os.replace) best-effort cache write; never raises.

    ``value=None`` writes a JSON ``null`` — this is the NEGATIVE cache: a failed
    fetch is cached for the TTL so a down nx-agent is probed once per TTL, not once
    per render tick (mirrors :func:`usage._write_usage_cache` caching the empty
    triple on failure). Any write error is swallowed after cleaning up the temp file.
    """
    tmp = f"{path}.tmp.{os.getpid()}"
    try:
        with open(tmp, "w", encoding="utf-8") as f:
            f.write(json.dumps(value))
        os.replace(tmp, path)
    except Exception:  # noqa: BLE001 - fail open: cache write is best-effort
        try:
            os.unlink(tmp)
        except Exception:  # noqa: BLE001
            pass


def _is_cache_fresh(path: str, now: float, ttl: float) -> bool:
    """True if ``path`` exists and its mtime is within ``ttl`` of ``now``.

    Distinguishes a fresh NEGATIVE cache entry (JSON ``null`` — :func:`_read_cache`
    returns ``None`` for it, same as a miss) from a genuine miss, so a cached
    failure still suppresses the fetch for the rest of the TTL.
    """
    try:
        age = now - os.stat(path).st_mtime
        return -ttl < age < ttl
    except Exception:  # noqa: BLE001 - no file / unreadable -> treat as not fresh
        return False


def _fetch_cached(
    kind: str,
    key: str,
    url: str,
    ttl: float,
    cache_path: Optional[str],
    now: Optional[float],
) -> Optional[dict]:
    """Cached-fetch core shared by both public functions.

    Cache hit (fresh dict) -> return it, no HTTP. Fresh negative entry -> return
    ``None``, no HTTP. Otherwise live :func:`usage._query`, write the result
    (including ``None`` on failure — negative caching), return it. Fail open: any
    unexpected error -> ``None``.
    """
    try:
        path = cache_path or _cache_path(kind, key)
        t = time.time() if now is None else now
        cached = _read_cache(path, t, ttl)
        if cached is not None:
            return cached
        if _is_cache_fresh(path, t, ttl):
            # Fresh negative entry (JSON null): honor it, skip the fetch.
            return None
        result = usage._query(url, timeout=usage.TIMEOUT_SECS)
        _write_cache(path, result)
        return result
    except Exception:  # noqa: BLE001 - fail open on any unexpected error
        return None


def session_context(
    session_id: str,
    ttl: float = CACHE_TTL_SECS,
    cache_path: Optional[str] = None,
    now: Optional[float] = None,
) -> Optional[dict]:
    """Cached ``GET /sessions/{session_id}/context``, or ``None`` on any failure.

    Returns the parsed JSON dict (``usedPercentage`` / ``contextWindowSize`` /
    ``updatedAt`` / ``sessionId`` per nx's ``SessionContextResponse``) on success.
    Returns ``None`` — and negatively caches it for ``ttl`` — when: ``session_id``
    is empty, nx-agent is unreachable, the response is non-2xx (including a 404 when
    nx has no fresh entry for that session), or the body is malformed. Backed by a
    short-TTL on-disk cache so a render-tick caller probes the network at most once
    per ``ttl``. ``cache_path`` / ``now`` are injectable for self-test.
    """
    if not session_id:
        return None
    url = f"{_BASE_URL}/sessions/{session_id}/context"
    return _fetch_cached("context", session_id, url, ttl, cache_path, now)


def project_git_status(
    code: str,
    ttl: float = CACHE_TTL_SECS,
    cache_path: Optional[str] = None,
    now: Optional[float] = None,
) -> Optional[dict]:
    """Cached ``GET /projects/{code}/status``, returning only its ``git`` sub-object.

    Returns the ``git`` dict (``{"branch": str|None, "headSha": str, "detached":
    bool, "dirty": {"modified": int, "untracked": int}, "observedAt": str}``) on
    success. Returns ``None`` — and negatively caches it for ``ttl`` — when: ``code``
    is empty, nx has not observed that project yet (response has no ``git`` field),
    the project code is unknown to nx (404), nx-agent is unreachable, or the body is
    malformed.

    NOTE the cache stores the FULL ``/status`` response dict (what
    :func:`usage._query` returns), and the ``git`` extraction happens on the
    already-cached dict on every call — so a cache hit still yields the ``git``
    sub-object without a fetch, and a response present-but-without-``git`` is
    correctly cached (as the full dict) and consistently yields ``None`` here.
    ``cache_path`` / ``now`` are injectable for self-test.
    """
    if not code:
        return None
    url = f"{_BASE_URL}/projects/{code}/status"
    payload = _fetch_cached("status", code, url, ttl, cache_path, now)
    if not isinstance(payload, dict):
        return None
    git = payload.get("git")
    return git if isinstance(git, dict) else None
