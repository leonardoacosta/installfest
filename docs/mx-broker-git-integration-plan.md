# Plan: Consume homelab `mx-broker` tokens on the Mac (retire the ADO PAT)

**Status:** Implemented. `scripts/git-credential-mxbroker.sh` is written and wired as the
`credential.https://dev.azure.com.helper` via `home/run_onchange_after_configure-git-azure.sh.tmpl`.
**Repo:** `~/dev/if` (chezmoi dotfiles; source under `home/`, see `.chezmoiroot`).
**Author context:** Written 2026-06-14 during a Mac restore. All facts below were verified live against the running homelab broker.

---

## 1. Goal

Let the Mac authenticate `git` to **Azure DevOps** (`dev.azure.com/brownandbrowninc`, the B&B repos `fb ws sc se ba bo`) using **auto-refreshed tokens from homelab's `mx-broker`**, instead of a manually-minted PAT stored in the macOS keychain.

**In scope:** ADO git credentials (and, as a bonus, a reusable path to the Graph token). **Out of scope:** replacing `az login` for general `az` CLI / RM work — the broker does not provide an az MSAL session.

## 2. Background (what exists today)

- **homelab** runs `mx-broker.service` (systemd --user): a Go daemon (`~/dev/mx/cmd/mx-broker`, binary `~/go/bin/mx-broker`). It owns rotating refresh tokens, proactively mints fresh **Graph** and **ADO** access tokens, and serves them over a **unix socket**.
  - Socket (on homelab, user `nyaptor`): `/home/nyaptor/.mx/broker/broker.sock` (parent dir `0700`, **no TCP listener** by design).
  - Endpoints: `GET /token?resource=<r>&identity=<i>` and `GET /health`.
  - **ADO line:** `resource=ado&identity=o365`. Verified live: `{"access_token":"<JWT ~2434 chars>","expires_on":<unix-seconds>}`, `/health` shows `ado/o365 SERVING` (ttl ~15 min, auto-renewed).
  - **Graph line:** `resource=graph&identity=o365` (ttl ~1h).
  - The ADO token is minted via "az grant mode" — the broker shells out to homelab's own `az --as-o365 account get-access-token --resource <ADO GUID>`. **So the ADO line depends on homelab's `az --as-o365` staying logged in.** (Currently logged in as `leonardo.acosta@bridgespecialty.com`.)
  - **The broker egresses through cloudpc's SOCKS5 proxy** (`127.0.0.1:1080` on homelab) for Conditional Access. homelab is NOT itself a CAE-trusted egress.
- **Mac** already has (set up this session):
  - SSH mesh working: `ssh homelab` (key `~/.ssh/id_ed25519`, host alias → `homelab.tail296462.ts.net`, user `nyaptor`).
  - `cloudpc-tunnel` LaunchAgent: `ssh -D 1080 -N cloudpc` → local SOCKS5 on `127.0.0.1:1080` (CAE egress). Model the new tunnel on this.
  - git config (machine-local `~/.gitconfig`, **not** chezmoi-managed):
    - `http.https://dev.azure.com/.proxy = socks5h://127.0.0.1:1080` ← **KEEP** (routes the git data-plane through cloudpc for CAE).
    - `credential.helper = osxkeychain` ← keep as the **fallback** helper (holds the PAT, if one is ever entered).
- **Reference clients to copy protocol from (on homelab):**
  - Server handler: `~/dev/mx/internal/broker/server.go` → `handleToken` (query params `resource`,`identity`; 200 `{access_token,expires_on}`, 503 when not serving).
  - Existing cross-machine socket tunnel precedent: `mx-imessage-tunnel.service` (homelab→Mac).

## 3. Design

Two new components in `~/dev/if`, plus a git-config change:

1. **Broker-socket tunnel (LaunchAgent)** — forwards homelab's broker socket to the Mac at the *same path* (`~/.mx/broker/broker.sock`) over the SSH mesh, KeepAlive. Mirrors `cloudpc-tunnel`.
2. **git credential helper** (`scripts/git-credential-mxbroker.sh`) — on `get` for `dev.azure.com`, curls the forwarded socket for the ADO token and returns it as the git password. On any failure (socket absent, 503, parse error) it prints nothing and exits 0, so git falls through to the next helper (`osxkeychain` PAT) — **graceful degradation**.
3. **git config** — register the broker helper *before* `osxkeychain` for `dev.azure.com`; keep the existing SOCKS proxy. Since `~/.gitconfig` isn't chezmoi-managed, set this idempotently via a `run_onchange` script (see Task 4) so it survives restores.

```
git push/clone dev.azure.com
   │  http.proxy = socks5h://127.0.0.1:1080  (cloudpc tunnel → CAE)   ← traffic
   │  credential.helper:
   │    1) git-credential-mxbroker  ──curl──▶ ~/.mx/broker/broker.sock ──ssh -L──▶ homelab mx-broker ──▶ fresh ADO token
   │    2) osxkeychain (PAT)  ◀── fallback if broker unreachable
```

Token (audience = ADO) is returned as the **git password** (basic auth). See Decision D1 for the basic-vs-bearer caveat.

## 4. Implementation tasks

### Task 1 — Broker-socket tunnel LaunchAgent
Create `home/Library/LaunchAgents/com.leonardoacosta.mx-broker-tunnel.plist` (model on the existing `com.leonardoacosta.cloudpc-tunnel.plist`). Forward the unix socket:

```
ProgramArguments:
  /usr/bin/ssh
  -nNT
  -o ExitOnForwardFailure=yes
  -o StreamLocalBindUnlink=yes        # clear stale local socket on (re)bind
  -o ServerAliveInterval=30
  -o ServerAliveCountMax=3
  -L /Users/leonardoacosta/.mx/broker/broker.sock:/home/nyaptor/.mx/broker/broker.sock
  homelab
KeepAlive: true
RunAtLoad: true
EnvironmentVariables.PATH: /usr/local/bin:/opt/homebrew/bin:/usr/bin:/bin
StandardOutPath: /Users/leonardoacosta/.local/logs/mx-broker-tunnel.out.log
StandardErrorPath: /Users/leonardoacosta/.local/logs/mx-broker-tunnel.err.log
```
- Ensure `~/.mx/broker/` exists `0700` on the Mac *before* first bind (the credential helper or a `run_onchange` should `mkdir -p ~/.mx/broker && chmod 700`). `StreamLocalBindUnlink=yes` handles the stale-socket-on-restart case.
- Bootstrap it: it'll be auto-loaded by `run_after_doctor.sh`'s "load deployed-but-unloaded LaunchAgents" loop, and on a clean install by `run_once_install-packages.sh`'s LaunchAgent loop (add the label there for parity).

### Task 2 — git credential helper `scripts/git-credential-mxbroker.sh`
- Repo-only script (referenced by absolute path, like `scripts/mic-priority.sh`). `chmod +x`.
- Contract (git credential protocol): read key=value lines from stdin until blank line. Only act on `get`. If `host=dev.azure.com`, query the socket; else exit 0 silently.
- Sketch:
  ```sh
  #!/usr/bin/env bash
  set -uo pipefail
  [ "${1:-}" = "get" ] || exit 0
  # parse stdin
  host=""; while IFS='=' read -r k v; do [ -z "$k" ] && break; [ "$k" = host ] && host="$v"; done
  [ "$host" = "dev.azure.com" ] || exit 0
  SOCK="$HOME/.mx/broker/broker.sock"
  [ -S "$SOCK" ] || exit 0   # tunnel down → fall through to osxkeychain (PAT)
  resp=$(curl -s --max-time 5 --unix-socket "$SOCK" \
    "http://localhost/token?resource=ado&identity=o365") || exit 0
  tok=$(printf '%s' "$resp" | /usr/bin/python3 -c \
    'import sys,json;
try: print(json.load(sys.stdin)["access_token"])
except Exception: pass') || exit 0
  [ -n "$tok" ] || exit 0
  printf 'username=mxbroker\npassword=%s\n' "$tok"
  ```
- Note: do **not** log the token. Keep `--max-time` small so a hung tunnel doesn't stall git.

### Task 3 — git config (idempotent, survives restore)
Add a `home/run_onchange_after_configure-git-azure.sh.tmpl` (darwin-guarded) that runs:
```sh
git config --global credential."https://dev.azure.com".helper "$DOTFILES/scripts/git-credential-mxbroker.sh"   # broker first
git config --global --add credential."https://dev.azure.com".helper "osxkeychain"                              # PAT fallback
git config --global http."https://dev.azure.com/".proxy "socks5h://127.0.0.1:1080"                             # keep CAE routing
```
- Order matters: the broker helper must be listed first; `--add` appends osxkeychain as fallback. Verify with `git config --get-all credential.https://dev.azure.com.helper`.
- Use `{{ .chezmoi.workingTree }}` for `$DOTFILES`.

### Task 4 — Update the doctor (`home/run_after_doctor.sh.tmpl`)
Add a check (warn-only) in the darwin block:
```sh
[ -S "$HOME/.mx/broker/broker.sock" ] && curl -s --max-time 3 --unix-socket "$HOME/.mx/broker/broker.sock" \
    "http://localhost/token?resource=ado&identity=o365" | grep -q access_token \
  || GAPS+=("mx-broker ADO token unavailable — git→ADO will fall back to PAT (check mx-broker-tunnel + homelab az --as-o365)")
```

### Task 5 — Retire the PAT path (after verification)
Once Tasks 1–4 verified working: the keychain PAT becomes a pure fallback. Optionally remove it later (`git credential reject` / Keychain Access → delete `dev.azure.com`). Don't remove until broker path is confirmed across a reboot.

## 5. Verification

1. `launchctl kickstart -k gui/$(id -u)/com.leonardoacosta.mx-broker-tunnel` → `ls -l ~/.mx/broker/broker.sock` (socket exists).
2. `curl -s --unix-socket ~/.mx/broker/broker.sock http://localhost/health` → `ado/o365 SERVING`.
3. Helper smoke (no token printed): `printf 'protocol=https\nhost=dev.azure.com\n\n' | scripts/git-credential-mxbroker.sh get | grep -q '^password=' && echo OK`.
4. **End-to-end:** `git clone https://dev.azure.com/brownandbrowninc/B3/_git/B3 /tmp/b3test` with **no PAT in keychain** → must succeed via broker token. Then `rm -rf /tmp/b3test`.
5. Reboot test: confirm the tunnel comes back (KeepAlive + RunAtLoad) and clone still works.

## 6. Decisions / open questions

- **D1 (verify first):** Does Azure DevOps accept an Entra **access token as the git basic-auth password**? mx-ado uses these tokens as `Authorization: Bearer` for the ADO *REST* API, not git. If basic-password is rejected, switch the helper to inject `http.extraHeader=Authorization: Bearer <token>` — but a credential helper can't set headers, so use a tiny `git` wrapper or `GIT_CONFIG_PARAMETERS`/`-c http.extraHeader=...` per invocation. **Test D1 in Verification step 4 before building the fallback.**
- **D2:** The ADO line depends on homelab's `az --as-o365` session. If it lapses (interactive-required), the broker's ADO token goes `NOT_SERVING` and the Mac silently falls back to PAT. Acceptable; the doctor check (Task 4) surfaces it. Document the homelab re-login: `ssh homelab 'az login --use-device-code --as-o365'`.
- **D3:** Dependency chain for the Mac broker path: SSH mesh up → `mx-broker-tunnel` up → homelab `mx-broker` up → homelab `cloudpc` SOCKS up → homelab `az --as-o365` valid. Any break ⇒ PAT fallback. This is why the fallback is mandatory, not optional.
- **D4:** Same pattern trivially extends to Graph (`resource=graph&identity=o365`) for any Mac tool needing Graph — out of scope here but note it in the helper's comments.

## 7. Files touched (summary for the implementer)
- NEW `home/Library/LaunchAgents/com.leonardoacosta.mx-broker-tunnel.plist`
- NEW `scripts/git-credential-mxbroker.sh` (chmod +x)
- NEW `home/run_onchange_after_configure-git-azure.sh.tmpl`
- EDIT `home/run_after_doctor.sh.tmpl` (add broker check)
- EDIT `home/run_once_install-packages.sh.tmpl` (add `com.leonardoacosta.mx-broker-tunnel` to the LaunchAgent bootstrap loop)
- Commit on a branch; the current restore-fixes branch is `fix/restore-install-robustness`.
