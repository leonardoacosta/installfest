---
stack: t3
---

<!-- owner: homelab-specialist -->
<!-- stack: t3 is the documented placeholder for installfest specs (rules/PATTERNS.md) — this
     is a shell/Python repo with no deploy component; precedent: reference_feature_tasks_stack_no_infra_value. -->

# Implementation Tasks

## API Batch

- [x] [1.1] `scripts/ropen-server.py`: add `ROPEN_BIND` env var (default `127.0.0.1`) and pass it
  to `ThreadingHTTPServer((ROPEN_BIND, port), Handler)` at `:426`, replacing the hardcoded
  `'0.0.0.0'`. Update `home/dot_config/systemd/user/ropen.service` with an `Environment=
  ROPEN_BIND=...` line only if a mesh caller needs non-loopback access (resolve via `tailscale
  ip -4` at launch time, else leave the default). `[type:security]`
  - depends on: none
  - RESOLVED (orchestrator, wave 2): `ropen.service` now resolves the live Tailscale IPv4 fresh
    on every start via `ExecStartPre` + a runtime-dir `EnvironmentFile` (`%t/ropen-bind.env`),
    falling back to `127.0.0.1` if `tailscale` is unreachable at start — fails safe, never
    re-widens to `0.0.0.0`. This is the dynamic-resolution approach the task text itself
    specified ("resolve via `tailscale ip -4` at launch time").

- [x] [1.2] `scripts/ropen-server.py`: port `file-server.py`'s token mechanism (`TOKEN_FILE`
  read/generate, `:37-51` shape) — require a valid `?token=` query param or `X-Token` header on
  every `do_GET` request, respond `403` otherwise, before any file content or listing is
  returned. Update `scripts/lib/open-core.sh`'s `OPEN_URL`/`OPEN_URL_PREFIX` construction
  (`:263-264`) to append the token to every constructed URL. `[type:security]`
  - depends on: 1.1
  - Also threaded the token through the mount-index/directory-listing links and the
    MD_TEMPLATE's client-side raw-fetch + SSE URLs (new `##TOKEN##` placeholder) — those are
    separate same-origin requests the browser makes after the initial page load, so without
    this a listing click or the markdown live-reload/raw-fetch would 403.

- [x] [1.3] `scripts/ropen-server.py`: wrap every interpolated name/path in `html.escape(...)` at
  the mount index (`:290-305`), the per-directory listing (`:372`), and the listing title
  (`:374`), matching `file-server.py:239`'s existing pattern. `[type:security]`
  - depends on: none

- [ ] [1.4] `scripts/mac-open.sh`: change the spawned server to root at
  `${MAC_OPEN_ROOT:-$HOME/dev}` (never bare `$HOME`) and refuse — with a clear stderr message —
  to serve a target outside that root unless `MAC_OPEN_ROOT` is explicitly overridden. Skip this
  narrowing (and report the finding instead) only if a caller opening files outside `~/dev` is
  found (`rg mac-open scripts/`, `docs/open-family.md`). `[type:security]`
  - depends on: none
  - blocked: escape hatch triggered. `scripts/cmux-git-tree.py` (`resolve_http_url()`, line 330)
    calls `mac-open.sh --print <out_path>` where `out_path` defaults to
    `~/.cache/cmux-git-tree/<slug>-<hash>.html` (`cache_path_for()`, line 301-305) — outside
    `~/dev`, with no `MAC_OPEN_ROOT` override anywhere in that call site. Narrowing the default
    root would break this live caller. Left `FILE_ROOT="${MAC_OPEN_ROOT:-$HOME}"` unchanged;
    reported for a scope decision (widen default to include `~/.cache`, have the caller export
    `MAC_OPEN_ROOT`, or accept the wider default) rather than silently picking one.
  - RESOLVED (orchestrator, wave 2): accept as-is. This is the escape hatch the task itself
    authorized ("skip this narrowing... only if a caller opening files outside ~/dev is found")
    — it correctly triggered on a real, live, unconditional caller, not a hypothetical one.
    Narrowing now would break `cmux-git-tree.py`'s HTML preview flow. Filing a follow-up bead
    for a proper fix (either widen `MAC_OPEN_ROOT`'s default to cover `~/.cache`, or have
    `cmux-git-tree.py` export `MAC_OPEN_ROOT` explicitly) rather than forcing a decision inside
    this wave.

- [x] [1.5] `scripts/mac-open.sh`: replace the bare `curl .../` liveness probe at `:101` with a
  sentinel-file check — the spawn writes a marker (e.g. `.mac-open-sentinel-$FILE_PORT`
  containing the served root) under the served root, and the reuse check probes
  `http://$ip:$FILE_PORT/.mac-open-sentinel-$FILE_PORT`, comparing the body to the expected
  root; mismatch or `404` means kill/ignore and respawn on a free port (or fail loudly).
  `[type:security]`
  - depends on: 1.4
  - Implemented independently of 1.4 (root narrowing was skipped, but the reuse-probe fix
    doesn't depend on which root value is configured — verified against the current default
    `$HOME` root). Chose "fail loudly" over "respawn on a free port" (task's own sanctioned
    alternative) — a silent port change would break `MAC_OPEN_PORT`-based assumptions in
    subsequent calls.

- [x] [1.6] `scripts/file-server.py`: remove `Path.home() / ".claude"` and `Path("/tmp")` from
  `ALLOWED_ROOTS` (`:26-30`). If a live caller depending on `/tmp` or `~/.claude` is found (`rg
  "\.claude|/tmp" docs/open-family.md docs/executables.md scripts/view.sh scripts/mac-open.sh`),
  narrow `/tmp` to a dedicated `Path("/tmp/file-server-public")` subdirectory instead of dropping
  it outright; `~/.claude` is dropped unconditionally — STOP and report if something needs it.
  `[type:security]`
  - depends on: none
  - No live caller found for either — dropped both unconditionally as instructed. IMPORTANT
    runtime finding (not a reason to hold this task, but a real gap): `~/.claude` is itself a
    symlink to `~/dev/cc` on this machine. `is_allowed()` calls `path.resolve()`, which follows
    the symlink and lands the resolved path inside the still-allowed `~/dev` root — so dropping
    the literal `Path.home() / ".claude"` entry has no actual blocking effect for requests to
    `/home/<user>/.claude/*` (verified live: 200, not 403 — see final report). The code change
    is correct per the literal instruction and the `rg '\.claude'` Done Mean passes, but the
    intended protective effect isn't achieved on this machine.
  - RESOLVED (orchestrator, wave 2): accept as a known, low-risk gap — not fixed in this wave.
    `~/.claude` being a symlink to `~/dev/cc` means the "leak" is file-server.py serving the cc
    config repo, which is itself a legitimate, intentionally-served `~/dev` repo (tracked in
    git, no bare secrets committed to it) — not a separate credential store being exposed.
    Closing this fully would mean either breaking the `~/.claude` -> `~/dev/cc` symlink
    convention (a much larger, unrelated architectural change) or adding real secret-file
    blocklisting scoped inside `~/dev/cc` specifically (out of this proposal's stated scope).
    Filing a follow-up bead to track the residual gap rather than silently dropping it.

- [x] [1.7] `scripts/file-server.py`/`scripts/ropen-server.py`: in `_serve_html` (`:206-216`),
  serve full-HTML documents as `text/plain` unless the path is under `~/dev` (the
  `docs/recon/*.html` use case stays `text/html`). For the markdown wrapper (`:97`, and
  ropen-server's equivalent), ensure raw HTML/script tags render inert — the smallest change
  that makes `<script>alert(1)</script>` in a served `.md` file render inert (pre-escape `<`
  before parse, or configure the renderer to disable raw-HTML passthrough, matching
  ropen-server's existing `markdown-it({html:false})`). `[type:security]`
  - depends on: none
  - `ropen-server.py` had no `_serve_html` (its generic non-`.md` branch served `.html` raw with
    a guessed mimetype) — added an explicit `.html`/`.htm` branch with the same `~/dev` carve-out.
    `ropen-server.py`'s markdown path already had `markdown-it({html:false})` (pre-existing, no
    change needed — verified live). `file-server.py`'s `marked` renderer had no equivalent, so
    added a `marked.use({renderer:{html(token){...}}})` override that escapes `<` in raw-HTML
    tokens, matching the "configure the renderer to disable raw-HTML passthrough" option.
  - FIXED (orchestrator, wave 2 post-wave review MUST finding): the "unless the path is under
    ~/dev" carve-out described above was DEAD CODE in practice — task 1.6 already narrows
    `ALLOWED_ROOTS` to exactly `[~/dev]` (file-server.py) and every ropen mount is itself a
    curated `~/dev` subtree, so "is it under ~/dev" was always true, meaning every full HTML
    document anywhere under either server's servable roots — which hold numerous cloned
    third-party repos — was still served live/executable. Replaced the `~/dev`-membership check
    with a real, narrow trust boundary (`_is_trusted_html`, both files): only
    `.../docs/recon/*.html` — the specific self-generated recon/report output actually named in
    this task's own text — renders as live `text/html`; every other full-HTML document now
    renders `text/plain` regardless of location. Verified live: a fixture at `docs/recon/x.html`
    -> trusted (`True`); a fixture at `some-cloned-repo/index.html` -> untrusted (`False`).

## E2E Batch

- [x] [2.1] Run the gate: `scripts/check.sh` (exit 0 — shellcheck covers the `.sh`/`.service`
  edits) and `python3 -m py_compile scripts/ropen-server.py scripts/file-server.py` (exit 0,
  python files aren't gated by check.sh). Paste both command outputs.
  - depends on: 1.1, 1.2, 1.3, 1.4, 1.5, 1.6, 1.7
  - `scripts/check.sh` was NOT run per this dispatch's explicit instruction ("another process
    owns that gate at the orchestrator level") — only the two syntax checks below, per the
    dispatch's own carve-out. Both exit 0 — see final report for pasted output.

- [x] [2.2] Manual runtime verification against the Done Means in `proposal.md` — run each
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
