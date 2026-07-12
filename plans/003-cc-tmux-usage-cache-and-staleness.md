# Plan 003: TTL-cache the 4MB credentials fetch, cut off stale session-context, delete the dead glyph function, and document the glyph

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` if that file exists — unless a reviewer dispatched you
> and told you they maintain the index.
>
> **Drift check (run first)**:
> `git -C /home/nyaptor/dev/personal/installfest diff --stat 60a1441..HEAD -- apps/cc-tmux/src/cc_tmux/usage.py apps/cc-tmux/src/cc_tmux/cli.py apps/cc-tmux/src/cc_tmux/tmux.py apps/cc-tmux/src/cc_tmux/testing.py apps/cc-tmux/skills/cc-status/SKILL.md apps/cc-tmux/README.md`
> If any in-scope file changed since this plan was written, compare the
> "Current state" excerpts against the live code before proceeding; on a
> mismatch, treat it as a STOP condition.

## Status

- **Priority**: P1
- **Effort**: M
- **Risk**: MED (edits the live status-bar render path — the tmux plugin runs
  repo HEAD via symlink, so a broken intermediate edit shows up in the user's
  status bar immediately; every step below keeps the tree importable)
- **Depends on**: none. **File-level conflict warning**: plan
  `plans/005-*` (per-tick spawn consolidation) also touches
  `apps/cc-tmux/src/cc_tmux/cli.py` and `usage.py`. If 005 has already landed
  (see STOP conditions), the cache from Step 2 belongs inside 005's
  consolidated entrypoint instead — the TTL logic is identical, only the call
  site moves. Do not attempt that merge yourself; STOP and report.
- **Category**: perf + bug + tech-debt + docs
- **Planned at**: commit `60a1441`, 2026-07-11

## Why this matters

The tmux status bar's row 2 (`cc-tmux session-bar`, wired into
`status-format[1]` in every theme conf) re-runs every second
(`status-interval 1`, one distinct `#()` job **per window**). On every run it
does a fresh HTTP GET + full JSON parse of nexus-agent's `/credentials`
payload — measured live at **4.03 MB / 2,709 rows / ~44 ms** — to extract one
`isActive` row whose values change on a minutes-to-hours scale. That is
~348 GB/day of loopback traffic and 86,400 requests/day per attached client,
multiplied by window count. Separately: the same row renders a model letter and
SES% from `session-context.<pane>.json` without ever checking the file's `ts`
field, so a stale file (observed live: 2h18m old) renders confidently wrong
data forever. Finally, `tmux.session_count_glyph` is confirmed dead code kept
green by a self-test that wires nothing, and the ◌/◉/◉ N glyph the bar
actually shows is documented nowhere user-facing.

After this plan: the credentials fetch happens at most once per ~45 s
(mtime-TTL on-disk cache, atomic write, fail-open), stale session-context
files render as absent instead of wrong, the dead function and its
self-deceiving test are gone, and the glyph is documented with its known
worktree limitation.

## Current state

All excerpts are fresh reads at commit `60a1441`. Working tree was clean for
all in-scope files at planning time.

### The repo and its constraints (you have zero other context — read this)

- Repo: personal dotfiles, `/home/nyaptor/dev/personal/installfest`
  (chezmoi-managed; project code `if`). Target app: `apps/cc-tmux` — a
  **Python 3.10+ STDLIB-ONLY** tmux + Claude Code plugin. `pyproject.toml`
  says `dependencies = []` ("No runtime dependencies: stdlib-only by design").
  **You may not add any dependency.**
- Quality gates: `apps/cc-tmux/bin/cc-tmux self-test` (pure-function suite,
  non-zero exit on failure; currently prints `cc-tmux self-test: 42/42
  passed`) and `apps/cc-tmux/bin/cc-tmux doctor` (env diagnostics, always
  exit 0). **New pure functions MUST get self-test coverage in
  `src/cc_tmux/testing.py`.**
- Design invariants (from the `tmux.py` module header — they constrain every
  fix):
  1. tmux pane options are the ONLY tracked-**pane**-state store — no new
     state files for pane state. Short-TTL caches for **external HTTP data**
     are acceptable: they cache external data, not pane state (precedent: the
     `roadmap-pulse.<code>.line` and `session-context.<pane>.json` files under
     `~/.claude/scripts/state/` that this same module already reads).
  2. Views derive, never store.
  5. Fail open — every hook/status entrypoint exits 0, never blocks
     tmux/Claude. Any cache error must fall through to a live fetch or an
     empty render, never an exception.
- Dual-install gotcha: the tmux side runs **repo HEAD** via the
  `~/.tmux/plugins/cc-tmux` symlink (your edits are live on the next 1 s
  status tick, even uncommitted). The Claude-hook side runs a SNAPSHOT at
  `~/.claude/plugins/cache/cc-tmux/cc-tmux/0.1.1/`. **This plan touches only
  tmux-side entry points** (`session-bar` render path, self-test, docs) — no
  `hooks.json` or register-path code — so **no plugin version bump / operator
  update gate is needed**. It does mean a syntax error you leave on disk
  breaks the user's live status bar, so run the `py_compile` verification
  after every code step.
- Commit pattern: `type(scope): subject` (recent examples:
  `fix(cc-tmux): restore tab click-to-switch via range=window, fix spacing`).
  Targeted `git add <paths>` only — never `git add .`.

### Relevant files

- `apps/cc-tmux/src/cc_tmux/usage.py` — nexus-agent `/credentials` query +
  segment rendering. `_query()` (lines 182–197) is the uncached fetch.
- `apps/cc-tmux/src/cc_tmux/cli.py` — CLI handlers. `_read_session_context`
  (607–636), `_active_usage` (639–664), `cmd_session_bar` (667–701) are the
  1 Hz render path.
- `apps/cc-tmux/src/cc_tmux/tmux.py` — pane-option state store.
  `session_count_glyph` (336–351) is the dead function to delete.
- `apps/cc-tmux/src/cc_tmux/render.py` — pure render functions.
  `_session_glyph` (209–213) is the LIVE glyph mapping. **Do not modify.**
- `apps/cc-tmux/src/cc_tmux/testing.py` — self-test suite + `_TESTS` registry.
- `apps/cc-tmux/skills/cc-status/SKILL.md` — user-facing skill doc; § States
  table at lines 42–48 lists only waiting/idle/active, no glyph legend.
- `apps/cc-tmux/README.md` — user-facing README; no glyph legend anywhere
  (sections: `## CLI` → `### Diagnostics` → `### Recency & freshness` (ends
  line 45) → `### fzf preview` (line 47) → `## Layout`).

### The 1 Hz cadence is real (why the cache matters)

`home/dot_config/tmux/tmux.conf.tmpl:231`:

```
set -g status-interval 1
```

`home/dot_config/tmux/tokyo-night-abyss-theme.conf:57` (all four theme confs
have the equivalent line):

```
set -g status-format[1] "#[bg=#0D0E15]#(~/.tmux/plugins/cc-tmux/bin/cc-tmux session-bar #{window_id})"
```

Because `#{window_id}` is expanded before the job string is hashed, tmux runs
a DISTINCT `session-bar` job per window per second.

### `usage.py` — the uncached fetch (lines 59–60, 182–197)

```python
CREDENTIALS_URL = "http://localhost:7400/credentials"
TIMEOUT_SECS = 1.0
```

```python
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
```

Current imports in `usage.py` (lines 43–48): `json`, `sys`,
`urllib.request`, `typing.Optional` — no `os`/`time`/`tempfile`/`Tuple` yet.
`_query` has exactly two callers: `usage.build_segment()` (line 200, the
legacy `cc-tmux usage` status-right segment — verified NOT wired into any
tmux conf today) and `cli._active_usage()` (the hot path).

### `cli.py` — `_active_usage` re-derives extraction per tick (lines 639–664)

```python
def _active_usage() -> Tuple[str, Optional[float], Optional[float]]:
    """``(account_label, 5H util, 7D util)`` for the active credential, or ``('', None, None)``.

    Reuses ``usage.py``'s query + active-credential-finding + field-extraction
    logic (same package) rather than re-deriving it. Fail-open on every branch.
    """
    try:
        payload = usage._query()
        if not payload:
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
            usage._account_label(active),
            usage._extract_util(active, "usage5hUsed", "usage5hLimit"),
            usage._extract_util(active, "usage7dUsed", "usage7dLimit"),
        )
    except Exception:
        return "", None, None
```

Called from `cmd_session_bar` at line 693:

```python
    model_letter, ses_pct = _read_session_context(pane)
    account_label, five_h_pct, seven_d_pct = _active_usage()
```

### `cli.py` — `_read_session_context` ignores `ts` (lines 607–636)

```python
def _read_session_context(pane_id: str) -> Tuple[str, Optional[float]]:
    ...
    if not pane_id:
        return "", None
    try:
        data = json.loads((_cc_state_dir() / f"session-context.{pane_id}.json").read_text(encoding="utf-8"))
    except Exception:
        return "", None

    letter = data.get("model")
    if not isinstance(letter, str):
        letter = ""

    pct = data.get("context_used_pct")
    if isinstance(pct, bool) or not isinstance(pct, (int, float)):
        pct = None
    else:
        pct = float(pct) / 100.0

    return letter, pct
```

The writer (nexus-statusline) writes `{"context_used_pct": N, "model": "S",
"ts": <epoch secs>}` per pane. `ts` is never read here. Live evidence at
audit time: `session-context.%3.json` was 2h15m stale and still rendered
`S / SES:15%`. The writer-side GC (prunes >6h, 1-in-100 gate, runs only when
the statusline itself renders) is inert exactly when the writer stalls — the
reader must enforce its own cutoff.

### `tmux.py` — dead `session_count_glyph` (lines 336–351)

```python
def session_count_glyph(project: str) -> str:
    """Session-count glyph for ``project`` — ``◌`` / ``◉`` / ``◉ N`` for 0 / 1 / 2+.
    ...
    """
    count = sum(1 for pane in get_hop_panes() if pane.project == project)
    if count > 1:
        return f"◉ {count}"
    if count == 1:
        return "◉"
    return "◌"
```

Zero production callers (verified by repo-wide grep at 60a1441). The live
path uses `render._session_glyph` (render.py:209–213). `cli.py:688–690` even
tells callers not to use it:

```python
    # Raw session count (an int) — render_session_bar maps it to a glyph itself,
    # so pass the count, not tmux.session_count_glyph()'s already-mapped string.
    session_count = sum(1 for p in tmux.get_hop_panes() if p.project == project) if project else 0
```

Its only other references: `testing.py:559–564` (`_FakeProjectPane`
dataclass, used ONLY by this test), `testing.py:576–596`
(`_test_tmux_session_count_glyph`, monkeypatches `get_hop_panes` and asserts
the dead function), and `testing.py:924` (registry row
`("tmux.session_count_glyph", _test_tmux_session_count_glyph),`).

### `testing.py` — existing session-context test writes an ancient `ts` (lines 861–863)

```python
        fixture = os.path.join(state_dir, "session-context.%9.json")
        with open(fixture, "w") as f:
            f.write('{"context_used_pct": 42, "model": "F", "ts": 123}')
```

**This fixture will FAIL after the staleness cutoff lands** (`ts: 123` is
1970). Step 5 updates it. `testing.py`'s top-level imports (lines 13–19) are
`os`, `shutil`, `tempfile`, `dataclasses.dataclass`, `typing` — **no `time`
yet**; you will add it.

### Docs — glyph documented nowhere

`skills/cc-status/SKILL.md:42–48` § States table rows are only
waiting / idle / active. `README.md` never mentions ◌ or ◉ (checked by grep).

### Settled context (do NOT re-litigate; cite as given)

- Zero-`isActive` → empty segment is documented Invariant-5 fail-open; do
  not add "no active acct" markers.
- Representative-pane (waiting>idle>active) choice for session-bar is by
  design.
- cc-tmux as passive reader of the roadmap-pulse cache is settled-by-spec:
  **no new background process** — the cache added here is written inline by
  the render process itself on TTL expiry, never by a spawned refresher.
- nexus-agent unpolled accounts (`usagePolledAt` null) render `--` by design.
- GLYPH-02 (linked worktrees resolve to a different git-toplevel basename and
  are excluded from the ◉ N count — verified live: a pane in
  `.worktrees/20260711-1353-89b833bc/` gets `@cc-project` =
  `20260711-1353-89b833bc`): **document as a known limitation only.** Do NOT
  change `tmux.py:_git_toplevel_name` (line 674) or the counting semantics —
  that fix needs a separate decision.

## Commands you will need

Run from repo root `/home/nyaptor/dev/personal/installfest`.

| Purpose | Command | Expected on success |
|---------|---------|---------------------|
| Self-test (gate) | `apps/cc-tmux/bin/cc-tmux self-test` | `cc-tmux self-test: N/N passed`, exit 0 (42/42 before this plan; 43/43 after) |
| Doctor (gate) | `apps/cc-tmux/bin/cc-tmux doctor` | PASS/WARN/FAIL rows printed, exit 0 always |
| Syntax check | `python3 -m py_compile apps/cc-tmux/src/cc_tmux/usage.py apps/cc-tmux/src/cc_tmux/cli.py apps/cc-tmux/src/cc_tmux/tmux.py apps/cc-tmux/src/cc_tmux/testing.py` | exit 0, no output |
| Live payload probe (optional context) | `curl -s -o /dev/null -w '%{size_download}\n' http://localhost:7400/credentials` | ~4000000 (bytes); may fail if nexus-agent is down — that is fine, everything here is fail-open |

There is no separate lint/typecheck gate for this app; self-test + doctor +
py_compile are the whole gate set.

## Scope

**In scope** (the only files you should modify):

- `apps/cc-tmux/src/cc_tmux/usage.py`
- `apps/cc-tmux/src/cc_tmux/cli.py`
- `apps/cc-tmux/src/cc_tmux/tmux.py` (deletion only)
- `apps/cc-tmux/src/cc_tmux/testing.py`
- `apps/cc-tmux/skills/cc-status/SKILL.md`
- `apps/cc-tmux/README.md`
- `plans/README.md` (status row only, if the file exists)

**Out of scope** (do NOT touch, even though they look related):

- `apps/cc-tmux/src/cc_tmux/render.py` — `_session_glyph` there is the live
  glyph mapping; it stays.
- `apps/cc-tmux/src/cc_tmux/tmux.py` `_git_toplevel_name` /
  `set_pane_git_identity` — the GLYPH-02 worktree counting gap is
  document-only in this plan (separate decision required to change it).
- `home/dot_config/tmux/*.conf*` — no tmux config changes; the cadence stays
  1 s by design.
- `apps/cc-tmux/hooks/`, `apps/cc-tmux/.claude-plugin/`,
  `apps/cc-tmux/cc-tmux.tmux` — Claude-hook side; touching these triggers the
  plugin version-bump operator gate. Nothing here needs them.
- `usage.build_segment` / `cmd_usage` beyond what Step 2 says — the legacy
  `cc-tmux usage` segment is not wired into any conf today; do not refactor
  it onto the cache (noted as deferred follow-up in Maintenance notes).
- `~/dev/personal/nexus` (nexus-agent / nexus-statusline) — separate repo,
  separate plan (004) and operator gate. The 2,709-row payload bloat is an
  nx-side problem; here it only gets a bead (Step 7).
- Per-tick subprocess/spawn consolidation — owned by plan 005.

## Git workflow

- Work on the current branch (`main` — this repo commits ad-hoc work directly
  to main; recent history: `fix(cc-tmux): restore tab click-to-switch via
  range=window, fix spacing`).
- Single commit at the end, message style `perf(cc-tmux): ...` (Step 8).
  Write the message to a temp file and use `git commit -F <file>` (repo
  convention — avoids a known multi-line-message mangling footgun).
- Targeted `git add <explicit paths>` only. Never `git add .` / `-A`.
- Do NOT push unless the operator instructed it.

## Steps

### Step 1: Baseline

From `/home/nyaptor/dev/personal/installfest`, run the drift check from the
header, then:

**Verify**: `apps/cc-tmux/bin/cc-tmux self-test` → `cc-tmux self-test: 42/42 passed`, exit 0.
If the count is not 42/42, treat as drift → STOP.

### Step 2: Add the TTL cache to `usage.py`

Edit `apps/cc-tmux/src/cc_tmux/usage.py`.

2a. Extend imports (lines 43–48): add `import os`, `import tempfile`,
`import time`, and change `from typing import Optional` to
`from typing import Optional, Tuple`.

2b. Below `_query` (after line 197), add three new module-level pieces —
constants, extraction, cache read/write, and the cached entrypoint. Target
shape (produce exactly this behavior; docstrings may be tightened):

```python
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
```

Notes:
- The cache stores the extracted ~100-byte triple, NOT the 4 MB payload —
  cache hits skip both the HTTP round-trip and the 4 MB JSON parse.
- `|age| < ttl` (rather than `0 <= age < ttl`) tolerates small clock/fs
  mtime skew without refetching every tick.
- Do NOT touch `_query`, `render_usage`, `build_segment`, or `cmd_usage`.

**Verify**: `python3 -m py_compile apps/cc-tmux/src/cc_tmux/usage.py` → exit 0, and
`PYTHONPATH=apps/cc-tmux/src python3 -c "from cc_tmux import usage; print(usage.active_usage(cache_path='/tmp/plan003-probe.json'))"`
→ prints a 3-tuple (real values if nexus-agent is up, `('', None, None)` if
down) and exits 0; `/tmp/plan003-probe.json` now exists. Clean up:
`rm -f /tmp/plan003-probe.json`.

### Step 3: Point `cli._active_usage` at the cache

Edit `apps/cc-tmux/src/cc_tmux/cli.py`: replace the entire body of
`_active_usage` (lines 639–664, excerpted above) with a delegation:

```python
def _active_usage() -> Tuple[str, Optional[float], Optional[float]]:
    """``(account_label, 5H util, 7D util)`` for the active credential, or ``('', None, None)``.

    Delegates to :func:`usage.active_usage` — a short-TTL on-disk cache over
    the ~4MB /credentials fetch, so the 1Hz session-bar tick does not re-fetch
    and re-parse the full payload every second (plan 003). Fail-open.
    """
    try:
        return usage.active_usage()
    except Exception:
        return "", None, None
```

Do not change `cmd_session_bar` itself.

**Verify**: `python3 -m py_compile apps/cc-tmux/src/cc_tmux/cli.py` → exit 0, and
`grep -n "usage._query()" apps/cc-tmux/src/cc_tmux/cli.py` → no matches.

### Step 4: Enforce a `ts` staleness cutoff in `_read_session_context`

Edit `apps/cc-tmux/src/cc_tmux/cli.py`. Immediately above
`def _read_session_context` (line 607) add a module-level constant:

```python
# session-context.<pane>.json freshness cutoff (plan 003): the writer
# (nexus-statusline) refreshes ts on every statusline render, i.e. every turn.
# A file older than this is a dead session or a recycled pane id — render it
# as absent rather than confidently wrong. Writer-side GC (>6h prune) is inert
# exactly when the writer stalls, so the reader enforces its own cutoff.
SESSION_CONTEXT_MAX_AGE_SECS = 900.0
```

Then inside `_read_session_context`, after the `json.loads(...)` try/except
and BEFORE the `letter = data.get("model")` line, insert:

```python
    ts = data.get("ts")
    if isinstance(ts, bool) or not isinstance(ts, (int, float)):
        return "", None
    if time.time() - float(ts) > SESSION_CONTEXT_MAX_AGE_SECS:
        return "", None
```

And harden the letter to a single character — change:

```python
    letter = data.get("model")
    if not isinstance(letter, str):
        letter = ""
```

to:

```python
    letter = data.get("model")
    if not isinstance(letter, str):
        letter = ""
    letter = letter[:1]
```

Behavior decisions (deliberate, do not soften):
- Missing or non-numeric `ts` → treated as stale → `("", None)`. The only
  writer always includes `ts`; a file without it is unverifiable.
- A future `ts` (clock skew) passes the check — fail toward showing data.
- `time` is already imported at the top of `cli.py`.

Update the function's docstring to mention the cutoff. Also fix the now-stale
comment at lines 688–689 (it names the function Step 6 deletes) — replace
both comment lines with:

```python
    # Raw session count (an int) — render_session_bar maps it to a glyph
    # itself (render._session_glyph), so pass the count, not a pre-mapped string.
```

**Verify**: `python3 -m py_compile apps/cc-tmux/src/cc_tmux/cli.py` → exit 0, and
`grep -n "SESSION_CONTEXT_MAX_AGE_SECS\|session_count_glyph" apps/cc-tmux/src/cc_tmux/cli.py`
→ exactly two `SESSION_CONTEXT_MAX_AGE_SECS` hits (definition + use), zero
`session_count_glyph` hits.

NOTE: `apps/cc-tmux/bin/cc-tmux self-test` now FAILS on
`cli.read_session_context` (its fixture has `ts: 123`). That is expected
until Step 5 — do not "fix" it by weakening the cutoff.

### Step 5: Update self-tests — staleness cases + new cache tests

Edit `apps/cc-tmux/src/cc_tmux/testing.py`.

5a. Add `import time` to the stdlib import block (after `import tempfile`,
line 15).

5b. In `_test_cli_read_session_context` (line 850), the fixture write
currently is:

```python
        with open(fixture, "w") as f:
            f.write('{"context_used_pct": 42, "model": "F", "ts": 123}')
```

Replace with a fresh timestamp so existing assertions keep passing:

```python
        with open(fixture, "w") as f:
            f.write(json.dumps({"context_used_pct": 42, "model": "F", "ts": time.time()}))
```

(`json` is not imported in testing.py either — check; if absent, use an
f-string with `time.time()` instead: `f.write(f'{{"context_used_pct": 42, "model": "F", "ts": {time.time()}}}')`.)

Then, after the existing malformed-JSON assertion, append four new cases
(same fixture path, same style as the surrounding `_check` calls):

- stale `ts` (`time.time() - 3600`) → `("", None)`
- missing `ts` (`{"context_used_pct": 42, "model": "F"}`) → `("", None)`
- boolean `ts` (`"ts": true`) → `("", None)`
- fresh `ts` + multi-char model (`"model": "Fable"`) → letter clamps to `"F"`

5c. Add two new test functions in the usage-tests region (near
`_test_usage_fail_open`, before the Session/beads section at line ~556),
modeled structurally on `_test_cli_read_session_context` (tempdir +
try/finally restore — that is this suite's fixture pattern):

```python
def _test_usage_extract_active() -> None:
    _check(usage.extract_active({}) == ("", None, None), "empty payload -> empty triple")
    _check(usage.extract_active({"credentials": "x"}) == ("", None, None), "non-list -> empty")
    _check(
        usage.extract_active({"credentials": [{"isActive": False}]}) == ("", None, None),
        "no active credential -> empty",
    )
    label, u5, u7 = usage.extract_active(
        {"credentials": [
            {"isActive": False, "accountName": "other"},
            {"isActive": True, "accountName": "leo",
             "usage5hUsed": 1.0, "usage5hLimit": 4.0,
             "usage7dUsed": None, "usage7dLimit": None},
        ]}
    )
    _check(label == "leo", f"label from active credential: {label!r}")
    _check(u5 == 0.25, f"5h util extracted: {u5!r}")
    _check(u7 is None, "unpolled 7d -> None")


def _test_usage_active_usage_ttl() -> None:
    # Cache round-trip + TTL + negative caching, with _query monkeypatched to count.
    calls = {"n": 0}
    saved_query = usage._query
    tmpdir = tempfile.mkdtemp(prefix="cc-tmux-usage-cache-test-")
    path = os.path.join(tmpdir, "cache.json")
    payload = {"credentials": [{"isActive": True, "accountName": "leo",
                                "usage5hUsed": 2.0, "usage5hLimit": 4.0,
                                "usage7dUsed": 1.0, "usage7dLimit": 10.0}]}
    try:
        def counting_query(url=usage.CREDENTIALS_URL, timeout=usage.TIMEOUT_SECS):
            calls["n"] += 1
            return payload
        usage._query = counting_query  # type: ignore[assignment]

        first = usage.active_usage(ttl=45.0, cache_path=path)
        _check(first == ("leo", 0.5, 0.1), f"miss fetches + extracts: {first!r}")
        _check(calls["n"] == 1, "first call hit the network once")
        _check(os.path.exists(path), "cache file written")

        second = usage.active_usage(ttl=45.0, cache_path=path)
        _check(second == first, "fresh cache returns same triple")
        _check(calls["n"] == 1, "fresh cache -> NO second fetch")

        os.utime(path, (time.time() - 3600, time.time() - 3600))
        third = usage.active_usage(ttl=45.0, cache_path=path)
        _check(third == first and calls["n"] == 2, "stale mtime -> refetch")

        # Corrupt cache fails open to a fetch.
        with open(path, "w") as f:
            f.write("not json")
        os.utime(path, None)
        fourth = usage.active_usage(ttl=45.0, cache_path=path)
        _check(fourth == first and calls["n"] == 3, "corrupt cache -> refetch")

        # Negative caching: failed fetch writes the empty triple; next call
        # within TTL serves it without re-querying.
        os.unlink(path)
        usage._query = lambda url=None, timeout=None: None  # type: ignore[assignment]
        down = usage.active_usage(ttl=45.0, cache_path=path)
        _check(down == ("", None, None), "fetch failure -> empty triple")
        usage._query = counting_query  # type: ignore[assignment]
        down2 = usage.active_usage(ttl=45.0, cache_path=path)
        _check(down2 == ("", None, None) and calls["n"] == 3,
               "negative cache served without refetch")
    finally:
        usage._query = saved_query  # type: ignore[assignment]
        shutil.rmtree(tmpdir, ignore_errors=True)
```

5d. Register both in `_TESTS` (append after the existing
`("usage.fail_open", ...)` row, line ~922):

```python
    ("usage.extract_active", _test_usage_extract_active),
    ("usage.active_usage_ttl", _test_usage_active_usage_ttl),
```

**Verify**: `apps/cc-tmux/bin/cc-tmux self-test` →
`cc-tmux self-test: 44/44 passed`, exit 0. (42 baseline + 2 new usage rows +
0 net from the session-context edits; the count drops to 43 in Step 6.)

### Step 6: Delete the dead glyph function and its self-test

6a. `apps/cc-tmux/src/cc_tmux/tmux.py`: delete the whole
`session_count_glyph` function (lines 336–351 in the pre-edit file, excerpted
above), including its blank-line separation. Nothing else in tmux.py changes.

6b. `apps/cc-tmux/src/cc_tmux/testing.py`: delete all three of —
- the `_FakeProjectPane` dataclass (pre-edit lines 559–564, including the
  `@dataclass` decorator) — it has no other user;
- `_test_tmux_session_count_glyph` (pre-edit lines 576–596);
- the registry row `("tmux.session_count_glyph", _test_tmux_session_count_glyph),`
  (pre-edit line 924).

Keep `_test_render_session_glyph` and the `("render.session_glyph", ...)`
row — that is the LIVE mapping's test.

**Verify**:
`grep -rn "session_count_glyph\|_FakeProjectPane" apps/cc-tmux/src/` → no
matches, and `apps/cc-tmux/bin/cc-tmux self-test` →
`cc-tmux self-test: 43/43 passed`, exit 0.

### Step 7: Document the glyph (SKILL.md + README) and file the nx-side bead

7a. `apps/cc-tmux/skills/cc-status/SKILL.md`: after the § States table
(pre-edit lines 42–48), add a new subsection:

```markdown
## Status-bar session glyph

Row 2 of the tmux status bar (the cc-tmux session-bar) leads with a
session-count glyph for the active window's project:

| Glyph | Meaning |
| ----- | ------- |
| ◌     | No tracked Claude pane in this project |
| ◉     | Exactly one tracked Claude pane in this project |
| ◉ N   | N tracked Claude panes in this project (2+) |

"This project" = panes whose `@cc-project` (the git-toplevel directory
basename) matches the active window's pane. Known limitation: a pane inside a
linked git worktree (e.g. `.worktrees/<session-id>/`) resolves to the
worktree directory's own basename, so it is NOT counted toward the parent
project's ◉ N.
```

7b. `apps/cc-tmux/README.md`: insert a new subsection between
`### Recency & freshness` (ends pre-edit line 45) and `### fzf preview`
(pre-edit line 47):

```markdown
### Status-bar session glyph

The session-bar row (status row 2) leads with a per-project session-count
glyph: `◌` no tracked Claude pane in the active window's project, `◉` one,
`◉ N` for N (2+). Counting keys on `@cc-project` (git-toplevel basename), so
panes inside linked git worktrees (`.worktrees/<id>/`) resolve to the
worktree's own basename and are not counted toward the parent project —
a known limitation.
```

7c. File the operator note as a bead (best-effort; the payload bloat is
root-caused in the nexus repo, not fixable here):

```bash
bd create "nexus-agent: /credentials returns 2,709 accumulated rows (4.03MB payload) - prune/dedupe junk credentials" -t task -p 2 --json
```

If `bd` is unavailable or errors, skip and mention it in your final report
instead — do not block the plan on it.

**Verify**: `grep -c "◉" apps/cc-tmux/skills/cc-status/SKILL.md apps/cc-tmux/README.md`
→ both counts ≥ 1, and `grep -n "worktree" apps/cc-tmux/skills/cc-status/SKILL.md apps/cc-tmux/README.md`
→ at least one hit in each.

### Step 8: Full gates, live check, commit

8a. Gates:

```bash
apps/cc-tmux/bin/cc-tmux self-test    # -> cc-tmux self-test: 43/43 passed, exit 0
apps/cc-tmux/bin/cc-tmux doctor       # -> checklist rows, exit 0
python3 -m py_compile apps/cc-tmux/src/cc_tmux/usage.py apps/cc-tmux/src/cc_tmux/cli.py apps/cc-tmux/src/cc_tmux/tmux.py apps/cc-tmux/src/cc_tmux/testing.py
```

8b. Live cache proof (runtime evidence; works only if a tmux server with the
plugin is running — if not, note it and rely on the self-test evidence):

```bash
CACHE="$(python3 -c 'import tempfile,os;print(os.path.join(tempfile.gettempdir(),f"cc-tmux-usage-cache.{os.getuid()}.json"))')"
rm -f "$CACHE"
WID="$(tmux display-message -p '#{window_id}' 2>/dev/null)"
apps/cc-tmux/bin/cc-tmux session-bar "$WID" > /dev/null
stat -c %Y "$CACHE"          # mtime after first render
sleep 2
apps/cc-tmux/bin/cc-tmux session-bar "$WID" > /dev/null
stat -c %Y "$CACHE"          # SAME mtime -> second render served from cache, no rewrite/fetch
```

Expected: both `stat` outputs identical, and the cache file is tiny
(`wc -c "$CACHE"` well under 1 KB, vs. the 4 MB payload).

8c. Commit (message via file, targeted adds):

```bash
git status --short   # ONLY in-scope files may appear
```

Write the commit message to `/tmp/commit-msg-plan003.txt`:

```
perf(cc-tmux): TTL-cache 4MB credentials fetch, cut off stale session-context, drop dead glyph fn

- usage.active_usage(): 45s mtime-TTL on-disk cache (atomic .tmp+os.replace,
  negative caching, fail-open) over the ~4MB 1Hz /credentials fetch
- cli._read_session_context: ts older than 900s renders as absent; model
  letter clamped to one char
- delete dead tmux.session_count_glyph + its self-test (live mapping is
  render._session_glyph)
- document the session-count glyph + worktree caveat in cc-status SKILL.md
  and README

Plan: plans/003-cc-tmux-usage-cache-and-staleness.md (at 60a1441)
```

```bash
git add apps/cc-tmux/src/cc_tmux/usage.py apps/cc-tmux/src/cc_tmux/cli.py apps/cc-tmux/src/cc_tmux/tmux.py apps/cc-tmux/src/cc_tmux/testing.py apps/cc-tmux/skills/cc-status/SKILL.md apps/cc-tmux/README.md
git commit -F /tmp/commit-msg-plan003.txt
```

(If the pre-commit hook stages `.beads/` exports alongside, that is expected
repo behavior — do not fight it.) Do NOT push unless the operator instructed it.

**Verify**: `git log --oneline -1` shows the new commit; `git status --short`
shows no unstaged modifications to in-scope files.

## Test plan

- New self-tests (Step 5), all in
  `apps/cc-tmux/src/cc_tmux/testing.py`, registered in `_TESTS`:
  - `usage.extract_active` — empty payload / non-list credentials / no active
    credential / active credential with one polled + one unpolled window.
  - `usage.active_usage_ttl` — miss→fetch+write, fresh hit→no fetch, stale
    mtime→refetch, corrupt file→fail-open refetch, fetch failure→negative
    cache served within TTL.
  - Extended `cli.read_session_context` — fresh-ts fixture keeps passing;
    stale ts / missing ts / boolean ts → `("", None)`; multi-char model
    clamps to one letter.
- Structural pattern to mimic: `_test_cli_read_session_context`
  (testing.py:850) — tempdir + `CLAUDE_CONFIG_DIR`/monkeypatch save-restore
  in `try/finally`, `_check(cond, msg)` assertions.
- Verification: `apps/cc-tmux/bin/cc-tmux self-test` →
  `cc-tmux self-test: 43/43 passed`, exit 0.

## Done criteria

Machine-checkable. ALL must hold (from repo root):

- [ ] `apps/cc-tmux/bin/cc-tmux self-test` prints `cc-tmux self-test: 43/43 passed` and exits 0
- [ ] `apps/cc-tmux/bin/cc-tmux doctor` exits 0
- [ ] `python3 -m py_compile apps/cc-tmux/src/cc_tmux/usage.py apps/cc-tmux/src/cc_tmux/cli.py apps/cc-tmux/src/cc_tmux/tmux.py apps/cc-tmux/src/cc_tmux/testing.py` exits 0
- [ ] `grep -rn "session_count_glyph\|_FakeProjectPane" apps/cc-tmux/src/` → no matches
- [ ] `grep -c "def active_usage" apps/cc-tmux/src/cc_tmux/usage.py` → `1`
- [ ] `grep -c "usage._query()" apps/cc-tmux/src/cc_tmux/cli.py` → `0` (grep exits 1)
- [ ] `grep -c "SESSION_CONTEXT_MAX_AGE_SECS" apps/cc-tmux/src/cc_tmux/cli.py` → `2`
- [ ] `grep -l "◉" apps/cc-tmux/skills/cc-status/SKILL.md apps/cc-tmux/README.md` → lists both files
- [ ] `git status --short` shows no modifications outside the in-scope list
- [ ] `plans/README.md` status row updated (only if that file exists)

## STOP conditions

Stop and report back (do not improvise) if:

- The drift check shows any in-scope file changed since `60a1441`, or the
  baseline self-test is not `42/42 passed`.
- `apps/cc-tmux/src/cc_tmux/cli.py` contains a consolidated per-tick
  entrypoint (a `render-all`-style subcommand emitting multiple status rows
  in one process) — plan 005 landed first; the cache belongs inside its
  consolidated path and this plan's Steps 2–3 must be reconciled with it.
- `grep -rn "session_count_glyph" apps/cc-tmux/ home/` (excluding
  `testing.py` and the cli.py comment quoted above) reveals a caller this
  plan's Current state says does not exist.
- A step's verification fails twice after a reasonable fix attempt —
  especially the self-test counts (42 → 44 → 43); a different count means an
  assumption broke.
- The fix appears to require touching `render.py`, any theme conf, anything
  under `apps/cc-tmux/hooks/` or `.claude-plugin/`, or the nexus repo.
- You are tempted to change `_git_toplevel_name` / the ◉ N counting semantics
  to "fix" the worktree caveat — that is explicitly document-only here.

## Maintenance notes

- **TTL tradeoff**: usage gauges (account label, 5H/7D) can lag reality by up
  to 45 s, and a recovered/switched nexus-agent account shows through the
  negative cache only after TTL expiry. Deliberate — usage moves on a minutes
  scale. Tune `USAGE_CACHE_TTL_SECS` (usage.py) if this ever feels stale;
  keep it well above the 1 s tick.
- **Thundering herd (accepted)**: N windows each run their own `session-bar`
  process; on TTL expiry up to N of them may fetch concurrently in the same
  second. Atomic `os.replace` makes the concurrent writes safe; at ~4 MB ×
  N once per 45 s this is noise vs. the prior 4 MB × N per second.
- **Plan 005 interplay**: if per-tick spawn consolidation lands later, move
  the `usage.active_usage()` call into the consolidated entrypoint — the
  cache helpers in usage.py are call-site agnostic and need no change.
- **Deferred**: `usage.build_segment` (`cc-tmux usage`, the legacy
  status-right segment) still uses the uncached `_query()`. It is wired into
  no tmux conf today; if it is ever re-wired, route it through
  `active_usage()` + a small formatter first.
- **Root cause lives in nexus**: the 4 MB payload is 2,709 accumulated
  credential rows (junk/dupes) in nexus-agent — the Step 7c bead tracks
  pruning it nx-side. This plan's cache is consumer-side mitigation; if nx
  prunes the payload the cache is still correct, just cheaper on misses.
- **GLYPH-02 left open by design**: worktree panes are excluded from ◉ N
  (documented in both docs now). Fixing it means resolving the primary repo
  root (e.g. `git rev-parse --git-common-dir`) in
  `tmux.py:_git_toplevel_name` — a semantics change (it also collapses
  same-basename distinct repos) that needs its own decision + plan.
- **Reviewer focus**: fail-open discipline in the new cache code (no code
  path may raise out of `active_usage`), and that the negative-cache write
  cannot mask a permanently-down agent beyond one TTL.
