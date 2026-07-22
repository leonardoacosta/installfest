---
order: 0722h
---

# Proposal: harden-mesh-file-servers — scope binds, add auth, and neutralize XSS across the three mesh file servers

## Change ID
`harden-mesh-file-servers`

## Why
Three sibling tools serve files across the 3-machine Tailscale mesh, each implementing the same
capability with a different security posture:

| Tool | Auth | Bind | Root served |
|------|------|------|-------------|
| `scripts/file-server.py` | static token | `0.0.0.0` | `~/dev`, `~/.claude`, `/tmp` |
| `scripts/ropen-server.py` | **none** | **`0.0.0.0`** | every registered project + active mounts |
| `scripts/mac-open.sh` (spawned server) | **none** | tailscale IP | **`$HOME`** for 1 hour |

The weakest two (`ropen-server.py`, `mac-open.sh`) expose broad, unauthenticated read access to
whatever is on the tailnet/LAN, and `file-server.py`'s allowed roots include `~/.claude`
(Claude credentials) and all of `/tmp` (any local process's scratch files). The realistic threat
is a compromised or merely untrusted device on the tailnet/LAN — not a remote internet attacker,
since none of these binds are internet-reachable — plus, for `/tmp`, any local process on the
same machine. Directory listings and served HTML/markdown also render untrusted filenames and
content without escaping, which is a stored-XSS vector the moment a filename or file body
contains a `<script>` tag.

## What Changes
1. **`ropen-server.py` bind scoping** — new `ROPEN_BIND` env var (default `127.0.0.1`) replaces
   the hardcoded `0.0.0.0` bind; mesh access opts in explicitly via the launcher.
2. **`ropen-server.py` token gate** — port `file-server.py`'s existing token-file mechanism;
   every request must carry a valid `?token=`/`X-Token` or gets `403`; the URL-constructing
   client (`scripts/lib/open-core.sh`) appends the token automatically.
3. **`ropen-server.py` escaped listings** — every interpolated name/path in the mount index and
   directory listing is wrapped in `html.escape(...)`, matching `file-server.py`'s existing
   pattern.
4. **`mac-open.sh` narrowed serve root** — the spawned server roots at `${MAC_OPEN_ROOT:-$HOME/dev}`
   (never bare `$HOME`) unless a caller is found needing a path outside `~/dev`.
5. **`mac-open.sh` reuse-probe fix** — the bare liveness `curl` is replaced with a sentinel-file
   check so a foreign server already listening on the reserved port is never silently reused.
6. **`file-server.py` dropped roots** — `~/.claude` and `/tmp` are removed from `ALLOWED_ROOTS`
   unless a live caller is found needing them (in which case `/tmp` is narrowed to a dedicated
   subdirectory, never all of `/tmp`).
7. **`file-server.py`/`ropen-server.py` HTML/markdown XSS neutralization** — full-HTML documents
   outside `~/dev` serve as `text/plain`; the markdown wrapper is configured so a `<script>` tag
   in served markdown renders inert (both already use client-side `markdown-it({html:false})` or
   an equivalent escape — this closes the one remaining raw-HTML-passthrough gap).

None of the three servers are unified into one — that consolidation is a separate direction
decision, not part of this change.

## Context
- touches: `scripts/ropen-server.py`, `scripts/file-server.py`, `scripts/mac-open.sh`,
  `scripts/lib/open-core.sh` (client-side token append at the `OPEN_URL`/`OPEN_URL_PREFIX`
  construction site, `:263-264`), `home/dot_config/systemd/user/ropen.service` (launcher —
  passes `ROPEN_BIND` if the mesh needs it).
- **Source**: `plans/002-mesh-file-server-hardening.md` (advisor plan, `/improve` audit
  2026-07-22, written against commit `d441448`), findings #2 (ropen-server unauthenticated on
  `0.0.0.0`, HIGH), #3 (mac-open serves `$HOME`, HIGH), #4 (file-server roots + XSS, MED-HIGH),
  plus the mac-open reuse-probe correctness note. Plan 2 of 8 in `plans/README.md`'s audit —
  independent of every other plan in that set (no `depends on:`).
- **Verified against current HEAD**: line references in the source plan (`ropen-server.py:426`
  bind, `:372`/`:374` listing interpolation; `file-server.py:24-30` `ALLOWED_ROOTS`,
  `:206-216` `_serve_html`; `mac-open.sh:50` `FILE_ROOT`, `:101-106` reuse probe) were spot-checked
  against the current tree during spec authoring and match — no drift since the plan's stamped
  commit.
- **Launcher located**: `home/dot_config/systemd/user/ropen.service` (`ExecStart=%h/.local/bin/ropen
  --serve`) → `scripts/ropen.sh:101` (`exec python3 scripts/ropen-server.py "$ROPEN_PORT"
  "$MOUNTS_JSON"`) → `ThreadingHTTPServer(('0.0.0.0', port), Handler)` in `ropen-server.py:426`.
- **Client-side URL construction located**: `scripts/lib/open-core.sh:263-264`
  (`OPEN_URL`/`OPEN_URL_PREFIX`, built from `open_core_resolve_ts_ip`) — this is where the token
  query param gets appended.
- **file-server HTML-as-is legitimate use confirmed**: `docs/recon/*.html` exist under `~/dev` as
  full HTML documents intended to be served and rendered as-is (recon reports) — the `~/dev`
  carve-out in step 7 preserves this use case.
- **No live `.claude`/`/tmp`/outside-`~/dev` caller found**: `rg '\.claude|/tmp'` across
  `docs/open-family.md`, `docs/executables.md`, `scripts/view.sh`, `scripts/mac-open.sh` turned up
  only unrelated `cmux` socket paths (`/tmp/cmux.sock`, `/tmp/cmux-remote.sock`) and `ropen`'s own
  `/tmp/ropen-<uid>` state dir (not `file-server.py`'s `ALLOWED_ROOTS`) — dropping `~/.claude` and
  `/tmp` from `file-server.py`, and narrowing `mac-open.sh`'s default root to `~/dev`, has no
  known live consumer to break. If a task's own grep during implementation finds a real one, that
  task's escape hatch applies (see `plans/002-mesh-file-server-hardening.md` § Escape hatches) —
  STOP that task, report the exact caller, continue with the rest.

## Testing
| Seam | Coverage |
| --- | --- |
| `ropen-server.py` bind default + `ROPEN_BIND` override | `[2.2]` `curl 127.0.0.1:8889/` succeeds, `curl <lan-ip>:8889/` refused |
| `ropen-server.py` token gate | `[2.2]` request without token → `403`; with valid token → `200` |
| `ropen-server.py` escaped listings | `[2.2]` fixture file `<img src=x>.txt` renders `&lt;img` in view-source |
| `mac-open.sh` narrowed root + reuse probe | `[2.2]` `rg 'MAC_OPEN_ROOT:-'` shows a non-`$HOME` default; manual foreign-server-on-port flow does not silently reuse it |
| `file-server.py` dropped roots | `[2.2]` `rg '\.claude'` in `ALLOWED_ROOTS` → no hits |
| `file-server.py`/`ropen-server.py` XSS neutralization | `[2.2]` fixture `.md` containing `<script>alert(1)</script>` renders inert (view-source shows escaped/sanitized) |
| Both `.py` files still parse | `[2.1]` `python3 -m py_compile` exit 0; `scripts/check.sh` exit 0 (shellcheck covers the `.sh`/`.service` edits) |

These scripts have no existing test harness and adding one is out of scope (per the source plan)
— the Done Means below are the tests; each command + its output is recorded in the `[2.2]` task.

## Done Means
- `rg "0\.0\.0\.0" scripts/ropen-server.py` shows no hits — the bind is env-defaulted
  (`ROPEN_BIND`, default `127.0.0.1`) only.
- A request to `ropen-server.py` without a valid token returns `403`; with a valid token,
  `200`.
- `rg "html.escape" scripts/ropen-server.py | wc -l` is `>= 3` (listing, mount index, listing
  title paths all escaped).
- `mac-open.sh` serves only the target file's directory or an explicit `MAC_OPEN_ROOT`
  override — never the full `$HOME` — and its reuse probe rejects a foreign server already
  bound to the reserved port.
- `rg '\.claude' scripts/file-server.py` shows no hits inside `ALLOWED_ROOTS`; if `/tmp` is
  still needed for a discovered live caller, it is narrowed to a dedicated subdirectory, never
  all of `/tmp`.
- A `<script>` tag in a served markdown or non-`~/dev` HTML file renders inert (view-source
  shows escaped/plain content, not an executing script).
- `python3 -m py_compile` on both `.py` files and `scripts/check.sh` both exit `0`.
