# SSH Mesh Network

A 3-machine SSH mesh allowing passwordless SSH between Mac, Homelab, and CloudPC using a single shared ED25519 keypair over Tailscale.

## Network Topology

```
                                    ┌─────────────────┐
                                    │   TAILSCALE     │
                                    │    NETWORK      │
                                    │  100.x.x.x/32   │
                                    └────────┬────────┘
                                             │
           ┌─────────────────────────────────┼─────────────────────────────────┐
           │                                 │                                 │
           ▼                                 ▼                                 ▼
┌─────────────────────────┐     ┌─────────────────────────┐     ┌─────────────────────────┐
│        MAC              │     │       HOMELAB           │     │       CLOUDPC           │
│    macbook-pro          │     │       homelab           │     │    346-CPC-QJXVZ        │
├─────────────────────────┤     ├─────────────────────────┤     ├─────────────────────────┤
│ OS: macOS               │     │ OS: Arch Linux          │     │ OS: Windows 11          │
│ User: leonardoacosta    │     │ User: nyaptor           │     │ User: leo               │
├─────────────────────────┤     ├─────────────────────────┤     ├─────────────────────────┤
│ LAN: 192.168.1.50       │     │ LAN: 192.168.1.100      │     │ LAN: (remote only)      │
│ TS:  100.91.88.16       │     │ TS:  100.73.182.4       │     │ TS:  100.83.148.5       │
└─────────────────────────┘     └─────────────────────────┘     └─────────────────────────┘
```

## Connection Matrix

| From → To   | Mac   | Homelab   | CloudPC |
| ----------- | ----- | --------- | ------- |
| **Mac**     | —     | ✅ LAN/TS | ✅ TS   |
| **Homelab** | ✅ TS | —         | ✅ TS   |
| **CloudPC** | ✅ TS | ✅ TS     | —       |

**Legend:** LAN = Direct local network, TS = Tailscale VPN

## Machine Details

### Mac (macbook-pro)

- **OS:** macOS
- **SSH User:** `leonardoacosta`
- **Tailscale:** Native app
- **LAN IP:** `192.168.1.50`
- **Tailscale IP:** `100.91.88.16`
- **Special:** Tailscale-only routing — ~/.ssh/config is chezmoi-managed (home/private_dot_ssh/config.tmpl); the old LAN probe was removed

### Homelab (homelab)

- **OS:** Arch Linux
- **SSH User:** `nyaptor`
- **Tailscale:** Docker container (host network mode)
- **LAN IP:** `192.168.1.100`
- **Tailscale IP:** `100.73.182.4`
- **Special:** Advertises exit node, routes `172.20.0.0/16` and `172.21.0.0/16`

### CloudPC (346-CPC-QJXVZ)

- **OS:** Windows 11
- **SSH User:** `leo` (local account, not AzureAD)
- **Tailscale:** Native app
- **Tailscale IP:** `100.83.148.5`
- **Special:** Two user profiles (`leo` for SSH, `LeonardoAcosta` for desktop)

## Shared SSH Key

All machines use the same ED25519 keypair. The current public key is
chezmoi-managed — the single source of truth is
`home/private_dot_ssh/private_authorized_keys` in this repo (the key line's
comment tag, e.g. `leo-mesh-YYYYMMDD`, dates the last rotation). Do not copy
the key or fingerprint into docs: `ssh-mesh/scripts/rotate-keys.sh`
regenerates the pair and rewrites that file, and copies drift.

## File Locations

### Mac

| File            | Path                    | Purpose                    |
| --------------- | ----------------------- | -------------------------- |
| Private Key     | `~/.ssh/id_ed25519`     | Authenticate outbound      |
| Public Key      | `~/.ssh/id_ed25519.pub` | Reference                  |
| Config          | `~/.ssh/config`         | Host aliases               |
| Authorized Keys | N/A                     | Mac doesn't accept inbound |

### Homelab

| File            | Path                     | Purpose               |
| --------------- | ------------------------ | --------------------- |
| Private Key     | `~/.ssh/id_ed25519`      | Authenticate outbound |
| Public Key      | `~/.ssh/id_ed25519.pub`  | Reference             |
| Config          | `~/.ssh/config`          | Host aliases          |
| Authorized Keys | `~/.ssh/authorized_keys` | Accept inbound        |

### CloudPC

| File              | Path                                                | Purpose               |
| ----------------- | --------------------------------------------------- | --------------------- |
| Private Key       | `C:\Users\LeonardoAcosta\.ssh\id_ed25519`           | Authenticate outbound |
| Config            | `C:\Users\LeonardoAcosta\.ssh\config`               | Host aliases          |
| Auth Keys (leo)   | `C:\Users\leo\.ssh\authorized_keys`                 | Accept inbound        |
| Auth Keys (admin) | `C:\ProgramData\ssh\administrators_authorized_keys` | Accept admin inbound  |

## Quick Setup

SSH config and authorized_keys are **chezmoi-managed**
(`home/private_dot_ssh/config.tmpl` + `private_authorized_keys`). The old
`ssh-mesh/scripts/setup-*.sh` playbooks were deleted — they predate chezmoi
ownership and clobbered managed files (git history has them if needed).

### Mac / Homelab

```bash
chezmoi apply   # lays down ~/.ssh/config and ~/.ssh/authorized_keys
```

Private key material is never in git (`ssh-mesh/keys/` is gitignored). To
generate/redeploy/rotate the shared keypair across all machines, run
`ssh-mesh/scripts/rotate-keys.sh`.

### CloudPC

```powershell
# Run as Administrator (from repo root)
powershell -ExecutionPolicy Bypass -File platform\windows\setup.ps1
```

> **Note:** The CloudPC setup script was consolidated into `platform/windows/setup.ps1` (handles SSH keys, sshd config, Tailscale, and dev tool installation).

## Windows SSH Quirks

### Two User Accounts

- `leo` - Local account used for SSH login
- `LeonardoAcosta` - AzureAD account for desktop use

### Admin Authorized Keys

Windows OpenSSH requires admin users' keys in a special location:

```
C:\ProgramData\ssh\administrators_authorized_keys
```

This file must have specific permissions:

```powershell
icacls administrators_authorized_keys /inheritance:r /grant "SYSTEM:F" /grant "Administrators:F"
```

## Tailscale Notes

### Homelab Docker Setup

Tailscale runs in a Docker container with host networking:

```yaml
services:
  tailscale:
    image: tailscale/tailscale:stable
    network_mode: host
    cap_add:
      - NET_ADMIN
```

**Re-authenticate if logged out:**

```bash
docker exec tailscale tailscale up --accept-routes --accept-dns=false \
  --advertise-exit-node --advertise-routes=172.20.0.0/16,172.21.0.0/16 \
  --hostname=homelab
```

### Check Status

```bash
# Mac
tailscale status

# Homelab
docker exec tailscale tailscale status

# CloudPC (PowerShell)
tailscale status
```

## Troubleshooting

### Connection Timeout

1. Check Tailscale status on both machines
2. Verify the target is online: `tailscale ping <hostname>`
3. Check if Tailscale needs re-authentication

### Permission Denied

1. Verify public key is in `authorized_keys`
2. Check file permissions (600 for private key, config, authorized_keys)
3. On Windows, verify `administrators_authorized_keys` permissions

## Security Notes

⚠️ **This setup uses a single shared key across all machines.** This is convenient but means:

- Compromise of any machine's private key compromises all
- Rotate keys with `ssh-mesh/scripts/rotate-keys.sh` (generates new keypair, deploys to all machines)
- Keep private key files secure (600 permissions)
- Never commit private keys to public repositories
