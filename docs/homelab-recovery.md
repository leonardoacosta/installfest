# Homelab Recovery Runbook

> Procedure for recovering the Arch/omarchy homelab after a btrfs snapshot rollback.
> Distilled from the Apr 2026 recovery session — each gotcha here cost real hours.
> Source of truth: this doc + `scripts/homelab/harden.sh`.

---

## When to use this

Symptoms that usually mean a rollback happened or is needed:

- System boots into an overlay instead of the real root (`findmnt /` shows `overlay`, not `btrfs subvol=/@`).
- Limine's `rootflags=subvol=/@/.snapshots/N/snapshot` in `/proc/cmdline` — you're booted into a snapshot.
- Userspace packages from a recent `pacman -Syu` are gone (e.g. `tailscale`, `snap-pac`, `proxychains-ng` missing).
- Services that worked yesterday fail with `exec: node: not found` or `connection refused`.
- `~/.pm2/dump.pm2` exists but `pm2 list` shows zero apps.

If any of those match, this runbook applies.

## Mental model: what survives a rollback

Btrfs subvolumes on this host:

| Subvol | Mount | Rolled back? | Contents |
|---|---|---|---|
| `@` | `/` | **Yes** | rootfs: `/etc`, `/usr`, `/var/lib/docker`, `/boot` excluded (vfat) |
| `@home` | `/home` | **No** | your dev repos, SSH keys, dotfiles, `~/.pm2/`, `~/.env`, `~/.config/` |
| `@log` | `/var/log` | **No** | journal, pacman log (long-term history survives) |
| `@pkg` | `/var/cache/pacman/pkg` | **No** | package cache (useful for offline reinstall) |
| `/boot` | `/boot` (vfat) | **No** | limine.conf, EFI bundles |

**Docker data lives in `@`** (under `/var/lib/docker/volumes/`), so named volumes are wiped. Bind-mount volumes under `/home/...` survive.

## Recovery playbook

Work top-down. Each step is a gate for the next.

### 1. Confirm you're on real `@`, not an overlay

```
findmnt /                    # want: /dev/mapper/root[/@] btrfs subvol=/@
cat /proc/cmdline | grep -o 'subvol=[^ ]*'   # want: subvol=@
```

If still on overlay, edit `/boot/limine.conf` so the main entry points at the real `@` subvol's matching kernel EFI bundle, reboot. See `limine.conf.pre-rollback` backup.

### 2. Boot verification + TPM auto-unlock

```
uname -r                     # matches a kernel in /usr/lib/modules/
systemctl status systemd-cryptsetup@root.service   # want: Finished in <2s with no passphrase prompt
```

If prompted for LUKS passphrase: initramfs lacks the sd-encrypt chain. `sudo chezmoi apply` then `sudo mkinitcpio -P` then `sudo limine-mkinitcpio -P`.

### 3. Run chezmoi apply

```
cd ~/dev/if
git pull --ff-only
chezmoi init --source=~/dev/if     # only if .chezmoi.toml.tmpl changed
chezmoi state delete-bucket --bucket=scriptState   # force run_once scripts to replay
TERM=xterm-256color chezmoi apply
```

This re-runs `run_once_install-packages.sh.tmpl` which sources `scripts/install-arch.sh` (packages + shell + user-service enable + SSH authorized_keys) AND `run_onchange_homelab-harden.sh.tmpl` which runs `scripts/homelab/harden.sh` (TPM2 + snapper + snap-pac + tailscale + DB bootstrap).

### 4. Restore service state

After chezmoi:

```
sudo systemctl start tailscaled        # if not already
sudo tailscale up --accept-routes      # re-auth URL, approve on another device
# expect same tailnet name, possibly different IP (free tier can't reserve)
```

User-scope systemd units (nexus-agent, nova-dashboard, file-server, elephant, pm2-nyaptor):

```
systemctl --user daemon-reload
systemctl --user --failed              # should be empty after chezmoi
```

PM2 apps (resurrect from `~/.pm2/dump.pm2`):

```
export PATH="$HOME/.local/share/mise/shims:$HOME/.local/share/pnpm:$PATH"
pm2 resurrect
pm2 list                               # t3-code-server, guardian-web, etc.
```

### 4.5. Observability stack — systemd-oomd + smartd + node_exporter + Vector

`scripts/homelab/harden.sh` now bootstraps the telemetry stack alongside hardening:

- **systemd-oomd** — OOM killer (system unit, ships with systemd)
- **smartd** — SMART disk monitoring (`smartmontools` package)
- **prometheus-node-exporter** — host metrics scraper
- **vector** — journald → Better Stack ingest pipeline

Vector ingest requires a token. Before running `chezmoi apply`, ensure `~/.env` contains:

```
VECTOR_BS_TOKEN=<better-stack-source-token>   # source 'homelab' (id 2396841)
```

If `VECTOR_BS_TOKEN` is missing, `harden.sh` logs a warning and skips Vector setup — the other three units still come up. Re-run `chezmoi apply` (or `scripts/homelab/harden.sh`) after populating the token to finish wiring Vector.

Better Stack references (for sanity-checking that logs land):

- Source: `homelab` (id `2396841`), ingest host `s2396841.us-east-9.betterstackdata.com`
- Dashboard: `Homelab Health` (id `999906`) — <https://telemetry.betterstack.com/team/t532144/dashboards/999906>

Verify locally:

```
systemctl is-active systemd-oomd smartd prometheus-node-exporter vector
# expect 4× active
sudo journalctl -u vector -n 30 --no-pager   # confirm ingest is happening, no auth errors
```

### 5. Databases — the step most likely to trap you

`scripts/homelab/harden.sh` now bootstraps these, but if you're running this manually:

```
cd ~/dev/hl/homelab
docker compose --env-file ~/.env up -d homelab-postgres   # shared postgres on :5436
docker exec homelab-postgres pg_isready -U cortex -d cortex
# Create per-project DBs that don't exist yet
docker exec homelab-postgres psql -U cortex -d cortex -c "CREATE DATABASE nexus OWNER cortex;"
# Run drizzle migrations (repeats are idempotent)
cd ~/dev/nx && POSTGRES_URL="$(grep ^POSTGRES_URL ~/.env | cut -d= -f2-)" pnpm --filter @nexus/db db:push
```

### 6. Verify

```
curl -s -o /dev/null -w "nova(3000):%{http_code} nexus(3100):%{http_code} guardian(3150):%{http_code}\n" \
  http://localhost:3000/ http://localhost:3100/ http://localhost:3150/
docker ps --format "{{.Names}}: {{.Status}}" | grep -v healthy || echo "all healthy"
```

All three dashboards should return 200/307. Any non-healthy container needs investigation.

## Gotchas that will bite you

1. **`libgcc_s.so.1` cascade during `pacman -Syu`** — if `/boot` isn't mounted, the post-install hooks fail silently and you get nonsensical post-upgrade state. Verify `findmnt /boot` before running the big upgrade.

2. **Node isn't on default `$PATH`** — this is a mise-managed runtime. Every systemd service that spawns node needs `Environment=PATH=%h/.local/share/mise/shims:...` (see committed `nova-dashboard.service`).

3. **AdGuard bind-mount → named-volume drift** — the running container can have bind mounts while the compose file defines named volumes. After `docker compose up --force-recreate adguardhome`, configs land in `/var/lib/docker/volumes/adguardhome-{conf,work}/_data/`, NOT in `~/homelab/adguardhome/`. Restore from `~/dev/hl/homelab/adguardhome/` with `sudo cp -a` into the named-volume path.

4. **Tailscale IP collision** — fresh `tailscale up` creates `homelab-1` if the old `homelab` node still exists in the admin console. Delete old node in admin UI FIRST. You won't get the old IP back on free-tier — expect a new one and update any hardcoded references (see commit `03d2166` for the pattern: grep for `100.x.x.x` across the repo).

5. **`chsh` is PAM-gated** — NOPASSWD sudo doesn't bypass it. Use `sudo usermod -s /usr/bin/zsh nyaptor` instead. `scripts/install-arch.sh` now does this by default.

6. **`$SHELL` env var is stale** — it's set at login. Detect shell via `getent passwd $USER | cut -d: -f7`, not `$SHELL`.

7. **`.pacnew` files are just warnings, not failures** — after big upgrades expect `/etc/pacman.d/mirrorlist.pacnew` etc. Resolve with `pacdiff` (from `pacman-contrib` package) or accept maintainer via `cp .pacnew → .`. Backups land at `*.bak-pre-pacdiff` if you use the pattern from this session.

8. **Vector loads `/etc/vector/vector.yaml` by default, not `.toml`.** The Arch package ships a demo `vector.yaml` that conflicts. The harden script renames it to `.demo-disabled` and uses `/etc/default/vector` to set `VECTOR_CONFIG=/etc/vector/vector.toml`. If Vector won't start with `status=78/CONFIG`, check that `/etc/default/vector` exists and the daemon is reading it (`systemctl cat vector` should show `EnvironmentFile=/etc/default/vector`).

## Reference material

- **Session commits**: `git log --oneline | grep -E "(harden|install-arch|tailscale|rollback)"` on the `if` repo.
- **Source-of-truth configs**: `~/dev/hl/homelab/` (configs), `~/dev/if/home/dot_config/systemd/user/` (user services), `~/dev/nx/packages/db/` (schema + migrations).
- **Backups per operation**: `limine.conf.pre-rollback`, `*.bak-pre-pacdiff`, `@.broken-YYYYMMDD` btrfs subvol (delete once recovery is confirmed).

## Quick-start appendix

If the machine is up and you want a one-shot "restore everything that chezmoi can":

```bash
ssh homelab
cd ~/dev/if && git pull
chezmoi state delete-bucket --bucket=scriptState
TERM=xterm-256color chezmoi apply
pm2 resurrect                          # after PATH has node
```

If you need NOPASSWD temporarily while diagnosing:

```bash
echo "$USER ALL=(ALL) NOPASSWD: ALL" | sudo tee /etc/sudoers.d/99-temp-recovery
sudo chmod 0440 /etc/sudoers.d/99-temp-recovery
# ... do work ...
sudo rm /etc/sudoers.d/99-temp-recovery
```
