---
stack: t3
---

<!-- owner: homelab-specialist -->
<!-- stack: t3 is the documented placeholder for installfest specs (rules/PATTERNS.md) — this
     is a shell/Python repo with no deploy component; precedent: reference_feature_tasks_stack_no_infra_value. -->

# Implementation Tasks

## API Batch

- [ ] [1.1] `scripts/ropen-server.py`: add `ROPEN_BIND` env var (default `127.0.0.1`) and pass it
  to `ThreadingHTTPServer((ROPEN_BIND, port), Handler)` at `:426`, replacing the hardcoded
  `'0.0.0.0'`. Update `home/dot_config/systemd/user/ropen.service` with an `Environment=
  ROPEN_BIND=...` line only if a mesh caller needs non-loopback access (resolve via `tailscale
  ip -4` at launch time, else leave the default). `[type:security]`
  - depends on: none

- [ ] [1.2] `scripts/ropen-server.py`: port `file-server.py`'s token mechanism (`TOKEN_FILE`
  read/generate, `:37-51` shape) — require a valid `?token=` query param or `X-Token` header on
  every `do_GET` request, respond `403` otherwise, before any file content or listing is
  returned. Update `scripts/lib/open-core.sh`'s `OPEN_URL`/`OPEN_URL_PREFIX` construction
  (`:263-264`) to append the token to every constructed URL. `[type:security]`
  - depends on: 1.1

- [ ] [1.3] `scripts/ropen-server.py`: wrap every interpolated name/path in `html.escape(...)` at
  the mount index (`:290-305`), the per-directory listing (`:372`), and the listing title
  (`:374`), matching `file-server.py:239`'s existing pattern. `[type:security]`
  - depends on: none

- [ ] [1.4] `scripts/mac-open.sh`: change the spawned server to root at
  `${MAC_OPEN_ROOT:-$HOME/dev}` (never bare `$HOME`) and refuse — with a clear stderr message —
  to serve a target outside that root unless `MAC_OPEN_ROOT` is explicitly overridden. Skip this
  narrowing (and report the finding instead) only if a caller opening files outside `~/dev` is
  found (`rg mac-open scripts/`, `docs/open-family.md`). `[type:security]`
  - depends on: none

- [ ] [1.5] `scripts/mac-open.sh`: replace the bare `curl .../` liveness probe at `:101` with a
  sentinel-file check — the spawn writes a marker (e.g. `.mac-open-sentinel-$FILE_PORT`
  containing the served root) under the served root, and the reuse check probes
  `http://$ip:$FILE_PORT/.mac-open-sentinel-$FILE_PORT`, comparing the body to the expected
  root; mismatch or `404` means kill/ignore and respawn on a free port (or fail loudly).
  `[type:security]`
  - depends on: 1.4

- [ ] [1.6] `scripts/file-server.py`: remove `Path.home() / ".claude"` and `Path("/tmp")` from
  `ALLOWED_ROOTS` (`:26-30`). If a live caller depending on `/tmp` or `~/.claude` is found (`rg
  "\.claude|/tmp" docs/open-family.md docs/executables.md scripts/view.sh scripts/mac-open.sh`),
  narrow `/tmp` to a dedicated `Path("/tmp/file-server-public")` subdirectory instead of dropping
  it outright; `~/.claude` is dropped unconditionally — STOP and report if something needs it.
  `[type:security]`
  - depends on: none

- [ ] [1.7] `scripts/file-server.py`/`scripts/ropen-server.py`: in `_serve_html` (`:206-216`),
  serve full-HTML documents as `text/plain` unless the path is under `~/dev` (the
  `docs/recon/*.html` use case stays `text/html`). For the markdown wrapper (`:97`, and
  ropen-server's equivalent), ensure raw HTML/script tags render inert — the smallest change
  that makes `<script>alert(1)</script>` in a served `.md` file render inert (pre-escape `<`
  before parse, or configure the renderer to disable raw-HTML passthrough, matching
  ropen-server's existing `markdown-it({html:false})`). `[type:security]`
  - depends on: none

## E2E Batch

- [ ] [2.1] Run the gate: `scripts/check.sh` (exit 0 — shellcheck covers the `.sh`/`.service`
  edits) and `python3 -m py_compile scripts/ropen-server.py scripts/file-server.py` (exit 0,
  python files aren't gated by check.sh). Paste both command outputs.
  - depends on: 1.1, 1.2, 1.3, 1.4, 1.5, 1.6, 1.7

- [ ] [2.2] Manual runtime verification against the Done Means in `proposal.md` — run each
  command and record the output:
  - `rg "0\.0\.0\.0" scripts/ropen-server.py` → no hits.
  - Start `ropen-server.py` with default env; `curl -s -o /dev/null -w '%{http_code}'
    http://127.0.0.1:8889/` without a token → `403`; retry with a valid `?token=` → `200`.
  - Create a fixture file `/tmp/rtest/<img src=x>.txt`, mount `/tmp/rtest` via ropen, fetch the
    listing, `view-source` confirms `&lt;img` (escaped, not raw markup).
  - `rg "html.escape" scripts/ropen-server.py | wc -l` → `>= 3`.
  - Manually start a foreign `python3 -m http.server 8790 --directory /tmp`, then run a
    `mac-open.sh` file flow — confirm it does NOT silently reuse the foreign server (sentinel
    mismatch triggers respawn/failure, not silent reuse).
  - `rg 'MAC_OPEN_ROOT:-' scripts/mac-open.sh` → default is not `$HOME`.
  - `rg '\.claude' scripts/file-server.py` → no hits inside `ALLOWED_ROOTS`.
  - Serve a fixture `.md` file containing `<script>alert(1)</script>` via `file-server.py` and
    via `ropen-server.py` — view-source on both confirms the script renders inert (escaped or
    stripped), no alert fires.
  - depends on: 2.1
