# open-family Specification

## Purpose
The "open family" is the set of tools (`ropen`/`ropen-server.py`, `file-server.py`,
`mac-open.sh`, and their shared client-side helpers in `scripts/lib/open-core.sh`) that serve or
open local files across the 3-machine Tailscale mesh (Mac, homelab, Arch) for remote viewing.
Each tool independently spins up an HTTP file server scoped to some root, and each must bind
only where intended, authenticate requests where the bind is reachable beyond loopback, serve
only vetted directories, and never let an untrusted filename or file body execute as HTML/JS in
the viewer's browser.

## Requirements

### Requirement: ropen-server binds to a scoped interface and gates requests behind a token
`scripts/ropen-server.py` SHALL bind to the interface named by the `ROPEN_BIND` environment
variable, defaulting to `127.0.0.1` when unset, and MUST NOT hardcode `0.0.0.0`. Any request
whose bind is reachable beyond loopback SHALL require a valid shared token, supplied as either a
`?token=` query parameter or an `X-Token` header; a request without a valid token SHALL receive
an HTTP `403` response before any file content or directory listing is returned. The
token-file mechanism SHALL follow the same convention as `scripts/file-server.py`'s existing
`TOKEN_FILE` (0600-permissioned, generated on first run if absent), and MAY share the same token
file — this SHALL be documented in a header comment on both files if shared. The client that
constructs ropen URLs (`scripts/lib/open-core.sh`) SHALL append the token to every constructed
URL so normal usage is unaffected by the gate.

#### Scenario: default bind refuses non-loopback requests
- Given: `ropen-server.py` is started with no `ROPEN_BIND` set
- When: a request arrives at `http://127.0.0.1:<port>/`
- Then: the server responds normally
- And: a request to the same port via the machine's LAN/Tailscale IP is refused (connection
  refused — the socket is not bound there)

#### Scenario: request without a token is rejected
- Given: `ropen-server.py` is running with the token gate active
- When: a request arrives with no `token` query param and no `X-Token` header
- Then: the server responds `403` and does not return file content or a directory listing

#### Scenario: request with a valid token succeeds
- Given: `ropen-server.py` is running with the token gate active and a known token value
- When: a request arrives with `?token=<valid-token>` (or the equivalent `X-Token` header)
- Then: the server responds `200` and serves the requested content as it would have before the
  token gate existed

### Requirement: ropen-server escapes untrusted names in generated HTML
Every filename, path, and mount identifier that `scripts/ropen-server.py` interpolates into
generated HTML (the mount index, the per-directory listing, and the listing title) SHALL be
passed through `html.escape(...)` (or an equivalent HTML-entity escape) before being written
into the response body, matching the existing pattern already used in
`scripts/file-server.py:239`.

#### Scenario: a filename containing markup renders escaped, not executed
- Given: a directory mounted by `ropen-server.py` containing a file literally named
  `<img src=x>.txt`
- When: that directory's listing is fetched
- Then: the response body contains the entity-escaped form (`&lt;img src=x&gt;.txt`) and never
  the raw `<img src=x>.txt` markup

### Requirement: mac-open narrows its served root and validates server reuse before serving
`scripts/mac-open.sh`'s spawned file server SHALL serve only `${MAC_OPEN_ROOT:-$HOME/dev}` by
default — the default root SHALL NOT be `$HOME` — and SHALL refuse (with a clear stderr message)
to serve a target file outside that root unless `MAC_OPEN_ROOT` is explicitly overridden to
include it. Before reusing an already-listening server on the reserved port, the script SHALL
verify liveness via a sentinel file written under the served root at spawn time (rather than a
bare liveness probe against `/`): it SHALL fetch the sentinel path and compare the response body
to the expected root, and MUST NOT reuse a server whose sentinel is missing or mismatched —
in that case it either kills/replaces the foreign process or fails loudly rather than silently
serving through it.

#### Scenario: default root excludes $HOME
- Given: `mac-open.sh` is invoked with no `MAC_OPEN_ROOT` override
- When: the spawned server starts
- Then: `MAC_OPEN_ROOT`'s effective default is `$HOME/dev` (or another non-`$HOME` value), never
  bare `$HOME`

#### Scenario: reuse probe rejects a foreign server
- Given: a server unrelated to `mac-open.sh` is already listening on the reserved port (e.g.
  started manually with `python3 -m http.server <port> --directory /tmp`)
- When: `mac-open.sh` runs its file-serving flow and reaches the reuse check
- Then: the sentinel probe fails (missing or mismatched body) and the script does not silently
  treat the foreign server as its own — it respawns on a free port or fails with a clear error

### Requirement: file-server serves only vetted roots
`scripts/file-server.py`'s `ALLOWED_ROOTS` SHALL NOT include `Path.home() / ".claude"` under any
configuration. `ALLOWED_ROOTS` SHALL NOT include the unscoped `Path("/tmp")`; if a live caller is
found to depend on serving from `/tmp`, the entry SHALL be narrowed to a dedicated subdirectory
(e.g. `Path("/tmp/file-server-public")`) rather than all of `/tmp`.

#### Scenario: Claude credentials directory is never an allowed root
- Given: `scripts/file-server.py`'s `ALLOWED_ROOTS` list as configured after this change
- When: the list is inspected
- Then: it contains no entry resolving to `~/.claude`, and no entry resolving to the unscoped
  `/tmp` root

### Requirement: served HTML and markdown neutralize embedded script execution
`scripts/file-server.py`'s `_serve_html` SHALL serve a full-HTML document as `text/html` as-is
only when the document's path is under the user's `~/dev` tree; a full-HTML document outside
`~/dev` SHALL be served as `text/plain` instead. The markdown-rendering path (in both
`file-server.py` and `ropen-server.py`) SHALL render embedded raw HTML/script tags inert — for
example by escaping `<` characters before parse, configuring the markdown renderer to disable
raw-HTML passthrough (as `ropen-server.py`'s existing `markdown-it({html:false})` already does),
or sanitizing the rendered output before it is inserted into the page.

#### Scenario: a script tag in served markdown does not execute
- Given: a markdown file containing `<script>alert(1)</script>` in its body
- When: the file is served and rendered by either `file-server.py` or `ropen-server.py`
- Then: the browser's rendered page does not execute the script — view-source shows the tag
  escaped or stripped, and no alert fires

#### Scenario: full HTML outside ~/dev is neutralized
- Given: a full HTML document (containing an `<html` opening tag) located outside the user's
  `~/dev` tree
- When: `file-server.py` serves that path
- Then: the response `Content-Type` is `text/plain`, not `text/html`, so the browser does not
  execute any embedded script
