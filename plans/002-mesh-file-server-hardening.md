# Plan 002 — Harden the mesh file-serving subsystem

**Written against commit:** `d441448` — if any excerpt no longer matches, STOP and report drift.
**Findings:** #2 (ropen-server unauthenticated on 0.0.0.0, HIGH), #3 (mac-open serves $HOME, HIGH), #4 (file-server roots + XSS, MED-HIGH), plus the mac-open reuse-probe correctness note.
**Priority:** 2 of 8. Independent of plan 001 (shell layer is already gated by shellcheck), but land 001 first anyway.

## Why this matters

Three sibling tools serve files across the 3-machine Tailscale mesh. They implement the
same capability with three different security postures, and the weakest two expose broad
read access:

| Tool | Auth | Bind | Root served |
|------|------|------|-------------|
| `scripts/file-server.py` | static token | 0.0.0.0 | `~/dev`, `~/.claude`, `/tmp` |
| `scripts/ropen-server.py` | **none** | **0.0.0.0** | every registered project + active mounts |
| `scripts/mac-open.sh` (spawned server) | **none** | tailscale IP | **`$HOME`** for 1 hour |

The realistic threat is a compromised/untrusted device on the tailnet or LAN, or any
local process (for `/tmp`). `~/.claude` holds Claude credentials; `$HOME` holds `~/.ssh`.

## Current state (verified excerpts)

`scripts/ropen-server.py:426` — no auth check exists anywhere in `Handler.do_GET`:

```python
if __name__ == '__main__':
    print(f'ropen-server listening on :{port} (mounts={mounts_path})', file=sys.stderr, flush=True)
    ThreadingHTTPServer(('0.0.0.0', port), Handler).serve_forever()
```

`scripts/mac-open.sh:50` and `:101-106`:

```bash
FILE_ROOT="${MAC_OPEN_ROOT:-$HOME}"
...
  if curl -s -o /dev/null --max-time 1 "http://${ip}:${FILE_PORT}/" 2>/dev/null; then
    return 0   # already serving — reuse
  fi
  setsid bash -c "timeout '$FILE_TTL' python3 -m http.server '$FILE_PORT' --bind '$ip' --directory '$FILE_ROOT'" \
    >/dev/null 2>&1 < /dev/null &
```

`scripts/file-server.py:24-30`:

```python
BIND = os.environ.get("FILE_SERVER_BIND", "0.0.0.0")

ALLOWED_ROOTS = [
    Path.home() / "dev",
    Path.home() / ".claude",
    Path("/tmp"),
]
```

`scripts/file-server.py:206-216` — `.html`/full-HTML served as-is, and the markdown
wrapper at `:97` does `innerHTML = marked.parse(...)` with no sanitizer.

## Conventions to match

- `file-server.py`'s token mechanism (`TOKEN_FILE` around `:37-51`) is the repo's existing
  auth pattern — reuse its shape in ropen-server rather than inventing a new scheme.
- `ropen-server.py`'s markdown path already uses `markdown-it({html:false})` (`:166`) —
  that is the sanitization exemplar for file-server's markdown wrapper.
- Path-traversal guard in `ropen-server.py:344-348` is sound — do not touch it.
- Shell edits must pass `shellcheck --severity=error` (check.sh gates it).

## Steps

1. **ropen-server: bind scoping.** Add `ROPEN_BIND` env (default `127.0.0.1`) replacing the
   hardcoded `'0.0.0.0'` at `:426`. Grep the repo for how ropen-server is reached
   (`rg -l "8889|ropen-server" scripts/ home/ docs/`) — the callers over the mesh must set
   `ROPEN_BIND` to the homelab's Tailscale IP (resolve at launch: `tailscale ip -4` if
   available, else keep explicit env). Update the launcher (LaunchAgent/systemd unit or
   script that starts it — find it via the grep) to pass the Tailscale bind.
   Verify: start locally with defaults, `curl http://127.0.0.1:8889/` works,
   `curl http://<lan-ip>:8889/` refused.

2. **ropen-server: token gate.** Port file-server.py's token check: read a token file
   (same location convention as file-server's `TOKEN_FILE`; a shared token file is
   acceptable — note it in the header comment), require it as a `?token=` query param or
   `X-Token` header on every request, return 403 otherwise. Update the client side
   (whatever constructs ropen URLs — find with `rg "8889" scripts/ home/`) to append it.
   Verify: request without token → 403; with token → 200.

3. **ropen-server: escape directory listings.** At `:372` (and the mount index at
   `:290-305`, listing title `:374`), wrap every interpolated name/path in `html.escape(...)`.
   file-server.py:239 is the exemplar. Verify: create `/tmp/rtest/<img src=x>.txt`, serve
   `/tmp/rtest` via a mount, fetch the listing, confirm the name renders escaped (view
   source: `&lt;img`).

4. **mac-open: narrow the served root.** In `scripts/mac-open.sh`, change the spawned
   server to serve the target file's parent directory instead of `$FILE_ROOT=$HOME`:
   compute `serve_dir=$(dirname "$abs_path")` and pass `--directory "$serve_dir"`, with the
   URL path becoming just the basename. Keep `MAC_OPEN_ROOT` as an override. If multiple
   files are opened in one TTL window from different dirs, spawning per-dir servers on
   distinct ports is acceptable-but-clunky — simpler: keep ONE server but root it at
   `${MAC_OPEN_ROOT:-$HOME/dev}` (never `$HOME`) and refuse (with a clear stderr message)
   to serve files outside that root. Choose the second option unless you find a caller
   that opens files outside `~/dev` (check `docs/open-family.md` and `rg mac-open scripts/`);
   if you find one, STOP and report which paths are needed.

5. **mac-open: fix the reuse probe.** Replace the bare `curl .../` liveness check at `:101`
   with a sentinel check: have the spawn write a marker file under the served root
   (e.g. `.mac-open-sentinel-$FILE_PORT` containing the root path) and probe
   `http://$ip:$FILE_PORT/.mac-open-sentinel-$FILE_PORT`, comparing the body to the
   expected root. Mismatch or 404 → kill/ignore and respawn on a free port (or fail loudly).
   Verify: start `python3 -m http.server 8790 --directory /tmp` manually, run a mac-open
   file flow, confirm it does NOT silently reuse the foreign server.

6. **file-server: drop dangerous roots.** Remove `Path.home()/".claude"` and `Path("/tmp")`
   from `ALLOWED_ROOTS` (`:26-30`). Check for legitimate users first:
   `rg "\.claude|/tmp" docs/open-family.md docs/executables.md scripts/view.sh scripts/mac-open.sh`
   — if a real flow serves from `/tmp` (e.g. rendered previews), replace with a dedicated
   subdir `Path("/tmp/file-server-public")` instead of all of `/tmp`. `~/.claude` goes
   unconditionally; if something needed it, STOP and report.

7. **file-server: neutralize HTML execution.** In `_serve_html` (`:206-216`): serve
   full-HTML documents as `text/plain` unless the file is under `~/dev` (source-tree HTML
   like docs/recon reports is the legitimate use — check `docs/recon/*.html` exists as the
   use case). For the markdown wrapper at `:97`: configure marked to escape raw HTML
   (`marked.use({renderer}) `/ `marked.parse(x, {  })` — with marked v14 the simplest safe
   change is pre-escaping `<` in the source before parse, or vendoring DOMPurify into the
   wrapper page). Pick the smallest change that makes `<script>alert(1)</script>` in a
   served `.md` render inert. Verify with exactly that fixture file.

8. **Run the gate:** `scripts/check.sh` → exit 0 (shellcheck section covers the .sh edits;
   python files aren't gated — run `python3 -m py_compile scripts/ropen-server.py scripts/file-server.py` yourself).

## Boundaries

- **In scope:** `scripts/ropen-server.py`, `scripts/file-server.py`, `scripts/mac-open.sh`, whatever launcher/unit starts ropen-server, the URL-constructing caller for the token param.
- **Out of scope:** `scripts/view.sh` (clean — uses `printf '%q'` correctly), `scripts/lib/open-core.sh` (plan 003 owns it), `cmux-bridge.py`, tailscale config, `ufw` rules, any `home/` template not directly launching these servers.
- Do not unify the three servers into one — that's a direction decision, not this plan.

## Done criteria (machine-checkable)

- `rg "0\.0\.0\.0" scripts/ropen-server.py` → no hits (env-defaulted bind only).
- `curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:8889/some-known-path` without token → `403`.
- `rg "html.escape" scripts/ropen-server.py | wc -l` → ≥ 3 (listing, index, title paths).
- `rg '\.claude' scripts/file-server.py` → no hits in `ALLOWED_ROOTS`.
- Fixture `.md` containing `<script>` served by file-server renders without executing (manual browser check, view-source shows escaped/sanitized).
- `mac-open.sh`: `rg 'MAC_OPEN_ROOT:-' scripts/mac-open.sh` shows a default that is NOT `$HOME`.
- `python3 -m py_compile` on both .py files → exit 0; `scripts/check.sh` → exit 0.

## Test plan

These scripts have no test harness (and adding one is out of scope). The done criteria
above are the tests; record each command + output in your report. The two servers should
be exercised end-to-end once over localhost.

## Maintenance note

The three-servers-three-postures situation will recur if a fourth serving path is added —
the header comment of each file should state its auth model in one line after this change.
Direction item A (shared session-state reader) and any future "open family" consolidation
should fold auth into one place; watch for that in review.

## Escape hatches

- Any grep in steps 1/4/6 revealing a live workflow that depends on the current permissive behavior (writes from `$HOME` outside `~/dev`, `/tmp` serving, `.claude` fetch): STOP that step, report the exact caller, continue with the other steps.
- If the ropen-server launcher can't be located (it may be started manually or from another repo): implement the server-side changes, default bind `127.0.0.1`, and report that the launcher needs a `ROPEN_BIND` decision from the maintainer.
