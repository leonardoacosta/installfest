# Plan 001: Add token auth to file-server.py so the tailnet cannot read ~/dev and ~/.claude

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `docs/plans/README.md`.
>
> **Drift check (run first)**: `git diff --stat 2068bad..HEAD -- scripts/file-server.py home/dot_config/systemd/user/file-server.service home/dot_zsh/rc/linux.zsh`
> If any in-scope file changed since this plan was written, compare the
> "Current state" excerpts against the live code before proceeding; on a
> mismatch, treat it as a STOP condition.

## Status

- **Priority**: P1
- **Effort**: S
- **Risk**: LOW
- **Depends on**: none
- **Category**: security
- **Planned at**: commit `2068bad`, 2026-07-02

## Why this matters

`scripts/file-server.py` runs as a systemd user service on the homelab, bound to
`0.0.0.0:8787` with **no authentication**, serving every file under `~/dev`,
`~/.claude`, and `/tmp`. Any host that can reach TCP 8787 — the whole Tailscale
tailnet including the employer-managed Windows CloudPC, plus the LAN — can read
every repo's gitignored `.env` (real secrets live at `~/dev/if/.env` by design),
every other project's env files, and Claude session state under `~/.claude`.
Path traversal is already correctly neutralized (`resolve()` + `is_relative_to`);
the exposure is purely no-auth + wide-open bind. A shared token closes it while
keeping the one legitimate remote consumer (`flink`, which builds clickable URLs
opened in the Mac browser) working.

## Current state

- `scripts/file-server.py` — stdlib HTTP file server, ~242 lines. Key excerpts:

```python
# scripts/file-server.py:20-27
PORT = int(os.environ.get("FILE_SERVER_PORT", 8787))
BIND = os.environ.get("FILE_SERVER_BIND", "0.0.0.0")

ALLOWED_ROOTS = [
    Path.home() / "dev",
    Path.home() / ".claude",
    Path("/tmp"),
]
```

```python
# scripts/file-server.py:119-133 (approx; do_GET)
class FileHandler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        raw_path = unquote(self.path.lstrip("/"))
        if not raw_path:
            self.send_error(400, "No file path specified")
            return

        path = Path("/" + raw_path)

        if not is_allowed(path):
            self.send_error(403, f"Not in allowed directories: {path}")
            return
```

There is no `Authorization`, token, or cookie handling anywhere in the file
(verified by grep). Note `do_GET` currently feeds `self.path` — including any
query string — into the filesystem path; the token step below must strip the
query string before path resolution.

- `home/dot_config/systemd/user/file-server.service` — the unit:

```ini
# home/dot_config/systemd/user/file-server.service:8,12
ExecStart=/usr/bin/python3 %h/dev/if/scripts/file-server.py --port 8787 --bind 0.0.0.0
Environment=FILE_SERVER_BIND=0.0.0.0
```

- `home/dot_zsh/rc/linux.zsh:87-99` — `flink`, the only in-repo consumer that
  crosses machines. It prints an OSC 8 hyperlink to
  `http://<tailscale-ip>:8787<abs_path>`, which Leo clicks from the Mac:

```zsh
# home/dot_zsh/rc/linux.zsh:93-98
  host=$(tailscale ip -4 2>/dev/null \
    || ip -4 addr show tailscale0 2>/dev/null | grep -oP 'inet \K[\d.]+' \
    || echo "localhost")
  local port="${FILE_SERVER_PORT:-8787}"
  local url="http://${host}:${port}${abs_path}"
```

- Repo conventions: shell follows the existing style in `linux.zsh` (plain zsh
  functions, local vars, `${VAR:-default}` fallbacks). Python is stdlib-only —
  do NOT add dependencies. This is a chezmoi repo: files under `home/` deploy
  via `chezmoi apply`; `dot_config/...` deploys to `~/.config/...`.

## Commands you will need

| Purpose | Command | Expected on success |
|---------|---------|---------------------|
| Python syntax | `python3 -m py_compile scripts/file-server.py` | exit 0 |
| Zsh syntax | `zsh -n home/dot_zsh/rc/linux.zsh` | exit 0 |
| Run server locally | `FILE_SERVER_PORT=8799 python3 scripts/file-server.py` | serves on 8799 |
| Smoke (authorized) | `curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:8799/tmp/probe.txt?t=$(cat ~/.local/state/file-server.token)"` | `200` |
| Smoke (unauthorized) | `curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:8799/tmp/probe.txt` | `403` |

## Scope

**In scope** (the only files you should modify):
- `scripts/file-server.py`
- `home/dot_config/systemd/user/file-server.service`
- `home/dot_zsh/rc/linux.zsh` (the `flink` function only)

**Out of scope** (do NOT touch, even though they look related):
- `scripts/mac-open.sh` — has its own inline `python3 -m http.server` on a
  different port with a TTL; consolidating the two servers is a separate
  decision, not this plan.
- `ALLOWED_ROOTS` narrowing — keep the three roots as-is; token auth is the fix.
- Tailscale ACLs / `tailscale serve` — out of band.
- `home/run_once_install-packages.sh.tmpl` (enables the service) — unchanged.

## Git workflow

- Work on the current branch (repo convention: direct commits to `main`).
- Commit style: conventional commits, e.g. `fix(file-server): require shared token for all requests`.
- Do NOT push unless the operator instructed it.

## Steps

### Step 1: Token load + generation in file-server.py

After the `ALLOWED_ROOTS` block, add token handling:

- Token file path: `Path.home() / ".local/state/file-server.token"`,
  overridable via `FILE_SERVER_TOKEN_FILE` env var.
- On startup: if the file exists, read/strip the token; if not, generate one
  (`secrets.token_hex(16)`), write it with `0600` permissions (create parent
  dir with `mkdir(parents=True, exist_ok=True)`), and log one line to stderr
  saying a token was generated (do NOT log the token value).

**Verify**: `python3 -m py_compile scripts/file-server.py` → exit 0

### Step 2: Enforce the token in do_GET (query param or cookie)

At the top of `do_GET`, before any path handling:

1. Split query string off `self.path` (`urllib.parse.urlsplit`). Use only the
   path component for file resolution from here on (this also fixes the
   pre-existing quirk where a query string would corrupt the filesystem path).
2. Accept the request if EITHER the `t` query parameter equals the token, OR
   a cookie `fs_token=<token>` matches (parse the `Cookie` header with
   `http.cookies.SimpleCookie`).
3. If the query param matched, include
   `Set-Cookie: fs_token=<token>; Path=/; HttpOnly` on the response — this
   makes relative links in directory listings and rendered markdown work
   without threading `?t=` through every href.
4. Otherwise `self.send_error(403, "Missing or invalid token")` and return.

Use `hmac.compare_digest` for the comparison. Directory-listing and markdown
rendering code below this point needs no changes because of the cookie.

**Verify** (in one shell):
```
echo probe > /tmp/probe.txt
FILE_SERVER_PORT=8799 python3 scripts/file-server.py &
sleep 1
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:8799/tmp/probe.txt          # expect 403
TOK=$(cat ~/.local/state/file-server.token)
curl -s -o /dev/null -w '%{http_code}\n' "http://127.0.0.1:8799/tmp/probe.txt?t=$TOK" # expect 200
curl -s -c /tmp/fs.cookies "http://127.0.0.1:8799/tmp/probe.txt?t=$TOK" >/dev/null
curl -s -b /tmp/fs.cookies -o /dev/null -w '%{http_code}\n' http://127.0.0.1:8799/tmp/probe.txt  # expect 200 (cookie)
kill %1
```

### Step 3: Teach flink to append the token

In `home/dot_zsh/rc/linux.zsh`, inside `flink`, after computing `$url`: if
`~/.local/state/file-server.token` exists, append `?t=$(<~/.local/state/file-server.token)`.
Keep the no-token fallback (print the bare URL) so `flink` never breaks when the
server has not started yet.

**Verify**: `zsh -n home/dot_zsh/rc/linux.zsh` → exit 0; then
`zsh -ic 'flink /tmp/probe.txt'` → printed URL contains `?t=`.

### Step 4: Deploy and live-verify the unit

The unit file needs no functional change, but confirm end-to-end:

```
chezmoi apply
systemctl --user restart file-server.service
systemctl --user is-active file-server.service     # expect: active
TOK=$(cat ~/.local/state/file-server.token)
curl -s -o /dev/null -w '%{http_code}\n' "http://$(tailscale ip -4):8787/tmp/probe.txt"        # expect 403
curl -s -o /dev/null -w '%{http_code}\n' "http://$(tailscale ip -4):8787/tmp/probe.txt?t=$TOK" # expect 200
```

## Test plan

No test framework exists in this repo. The runnable checks above ARE the test
plan; additionally leave one self-check behind per repo convention: none needed
beyond the curl matrix (server is exercised end-to-end). Record the four curl
results (403/200/200-cookie/403-live) in the commit message body.

## Done criteria

- [ ] `python3 -m py_compile scripts/file-server.py` exits 0
- [ ] `zsh -n home/dot_zsh/rc/linux.zsh` exits 0
- [ ] Tokenless request over the tailscale IP returns `403`
- [ ] Tokened request returns `200`; cookie-only follow-up returns `200`
- [ ] `grep -c 'compare_digest' scripts/file-server.py` >= 1
- [ ] No files outside the in-scope list modified (`git status`)
- [ ] `docs/plans/README.md` status row updated

## STOP conditions

Stop and report back (do not improvise) if:

- The excerpts above do not match the live code (drift).
- `flink` turns out to have consumers that cannot carry a query param
  (search: `grep -rn 'flink' home/ scripts/ packages/` — if any caller parses
  the URL, report before changing the format).
- Another consumer of port 8787 exists beyond `flink`
  (`grep -rn '8787' home/ scripts/ packages/ platform/` shows a new hit).
- The systemd unit fails to start after the change.

## Maintenance notes

- If `mac-open.sh`'s inline `http.server` is ever merged into this server, the
  token scheme must come along.
- Reviewer should scrutinize: the query-string strip in Step 2 (it changes path
  parsing for ALL requests), and that the token never appears in logs.
- Deferred: narrowing `ALLOWED_ROOTS` (drop `~/.claude`) and binding to the
  tailscale interface instead of 0.0.0.0 — both are further hardening, skipped
  to keep this change minimal and unbreaking.
