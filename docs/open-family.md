# The `*open` command family

Eight commands, one job each: get something in front of a human eyeball —
in a browser tab, a Mac IDE window, or a phone/desktop notification. All are
chezmoi-deployed (`home/dot_local/bin/symlink_*.tmpl` or `executable_*`) —
the sole source of truth for this family lives in `if`.

## The two families

| Family | Commands | Portal-aware? | Why |
| --- | --- | --- | --- |
| **VIEW** | `ropen`, `sopen`, `gopen`, `mopen`, `iopen` | Yes | Opening something to *look at* — a durable portal copy is fine, often better (survives your laptop closing). |
| **EDIT** | `copen`, `vopen`, `zopen` | No | Opening something to *edit* — you always want the real source file, never a rendered copy. |

### VIEW family

All five share one engine, `scripts/lib/open-core.sh`:

`gopen`/`sopen`/`mopen`/`iopen` are a single script, `scripts/viewopen.sh`,
dispatched by basename behind their symlinks (the same pattern `ideopen.sh`
uses for `vopen`/`zopen`); `ropen` keeps its own script because it also owns
the server lifecycle (`--serve`/`--mount`/`--list`).

| Command | Behavior |
| --- | --- |
| `ropen <target>` | Refresh/open a browser tab (tries Chrome, falls back to Safari) |
| `sopen <target>` | Force Safari specifically |
| `gopen <target>` | Force Google Chrome specifically |
| `mopen <target>` | Post a clickable Nexus desktop notification instead of auto-opening — the Mac counterpart to a phone push. No live-reload watcher (a notification is a one-shot). |
| `iopen <target>` | Post a clickable Nexus APNS push to iPhone — the phone counterpart to `mopen` (same one-shot, click-to-open shape, no live-reload watcher). Uses `nx_ropen` (cc's `nx-send.sh`), the iPhone-push sibling of `nx_mopen`. |

`<target>` is any of:
- An existing file or directory, relative or absolute
- A `home/projects.toml` registry code (e.g. `ropen if` opens this repo's root)

### EDIT family

| Command | Editor | Notes |
| --- | --- | --- |
| `copen` | Cursor | **Canonical, separate implementation** (`home/dot_local/bin/executable_copen`) — registry-project-aware: resolves a *project*, clones it on the Mac if missing, then optionally opens a file within it. See its own header comment for full usage. |
| `vopen` | VS Code | `scripts/ideopen.sh`, dispatched by basename |
| `zopen` | Zed | `scripts/ideopen.sh`, dispatched by basename |

`vopen`/`zopen` are simpler than `copen`: they take a bare file/dir/workspace
path and open it via the editor's Remote-SSH mechanism back to this box —
no project registry, no clone-if-missing. `copen`'s richer behavior wasn't
folded into `ideopen.sh`; the two solve different problems.

## Resolution order (VIEW family only)

```
target (file / dir / registry code)
        │
        ▼
  Atlas docs portal lookup   ── HIT ──▶ open the durable Atlas URL
        │
       MISS or unreachable/timeout/error (fail-open)
        │
        ▼
  ropen's live-mount HTTP server (systemd-managed `ropen.service`)
        │
        ▼
  register mount, spawn live-reload watcher (ropen/sopen/gopen only),
  dispatch to the Mac over ssh + AppleScript (or notify, for mopen)
```

Atlas is a **durability optimization on top of** the live-mount server, not
a replacement for it or a dependency it needs to function. See
`~/dev/personal/atlas/docs/INDEX-CONTRACT.md` (the `at` repo) for the manifest contract this implements
against.

### Fail-open contract (non-negotiable)

`open_core_atlas_lookup` in `scripts/lib/open-core.sh` treats every one of
the following as "not indexed, fall back to the live-mount server" — never
a hard error, never a hang:

- `ATLAS_BASE_URL` unset or empty (today's default — Atlas isn't deployed yet)
- `GET $ATLAS_BASE_URL/index.json` times out (`ATLAS_FETCH_TIMEOUT`, default 2s;
  `--connect-timeout 1`)
- Atlas is unreachable (DNS failure, connection refused, Tailscale down)
- Non-2xx response
- Response body isn't valid JSON, or `version` isn't `1`
- The target's key isn't present in `paths`

### Configuring Atlas

One env var, unset by default:

```bash
export ATLAS_BASE_URL="https://atlas.internal"   # once Atlas is actually deployed
```

Set it in your shell rc (or a machine-local env file) once `hl`'s
Traefik/AdGuard wiring finalizes Atlas's real domain — nothing else needs
to change. The manifest is cached locally for `ATLAS_CACHE_TTL` seconds
(default 300) at `$STATE_DIR/atlas-index.json` (`STATE_DIR` = `/tmp/ropen-<uid>`,
shared with the live-mount server's own state) so a repeated open doesn't
refetch every time.

## The live-mount server

Owned by `systemd --user`:

```bash
systemctl --user start|stop|restart|status ropen
```

- CLI: `scripts/ropen.sh` (`--serve` runs the server in the foreground for
  systemd; the default subcommand resolves a target and dispatches to the
  Mac; `--mount <dir>` pre-warms a mount without dispatching; `--list`
  shows active mounts)
- Server: `scripts/ropen-server.py` — multi-project HTTP server, client-side
  markdown rendering (markdown-it + mermaid via CDN, no Python markdown
  dependency), SSE live-reload, directory listings, and a registered-projects
  index sourced from `home/projects.toml`
- Unit: `home/dot_config/systemd/user/ropen.service`

State lives under `/tmp/ropen-<uid>/` (mounts.json, PID file, lock file,
log, Atlas cache) — ephemeral by design, recreated on server start.

## Env vars (VIEW family)

| Var | Default | Meaning |
| --- | --- | --- |
| `ATLAS_BASE_URL` | unset | Atlas portal base URL; unset = skip the portal check entirely |
| `ATLAS_CACHE_TTL` | `300` | Seconds before re-fetching `index.json` |
| `ATLAS_FETCH_TIMEOUT` | `2` | curl `--max-time` for the manifest fetch |
| `ROPEN_PORT` | `8889` | Live-mount server port |
| `OPEN_MAC_HOST` | `mac` | ssh alias for the Mac (AppleScript dispatch target) |

## History

This family used to be split: `ropen`/`mopen`/`iopen`/`ideopen` lived in `cc`
(`~/.claude/tools/`), while `copen` and a separate, simpler unified opener
(`mac-open.sh` — still `if`'s front door for URLs and OAuth-loopback
callbacks, untouched by this migration) already lived here. Beads `if-34u`
had planned to retire `ropen` in favor of `mac-open.sh`'s simpler model;
that direction reversed because `ropen`'s live-mount/live-reload/tab-refresh
mechanism turned out to still be in active daily use — see the comment
trail on `if-34u` for the full reasoning. `sopen`/`gopen` are new; the rest
were relocated into `if` as the sole owner, with Atlas-awareness added on
top.

**`iopen` note:** missed in the initial migration pass (cc's `tools/ropen/`
was deleted before `iopen` had an `if`-side home), which briefly broke it
on `$PATH` alongside `ropen`/`mopen`/`vopen`/`zopen` when the cc-side
cleanup ran ahead of a `chezmoi apply`. Ported immediately as a same-shape
sibling of `mopen` (swap `nx_mopen` for `nx_ropen`) and the missing
executable bit on the whole batch of migrated scripts was fixed in the
same pass — see git history for both fixes.
