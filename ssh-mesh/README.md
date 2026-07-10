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
- **Special:** Smart routing (probes LAN before Tailscale fallback)

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

All machines use the same ED25519 keypair:

- **Type:** ED25519
- **Fingerprint:** `SHA256:CBNRqlrElgBDWzg9bv6MdnYV2xnO21l+klwB4qdi2kY`
- **Public Key:** `ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFqT0bMXcrQGgWvYoLg66dCCvhgAPx1rmrJmzGpMeFVR`

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

### Mac

```bash
cd ~/dev/personal/installfest/ssh-mesh
bash scripts/setup-mac.sh
```

### Homelab

```bash
# From Mac, copy files and run setup
scp -r ~/dev/personal/installfest/ssh-mesh homelab:~/
ssh homelab "cd ~/ssh-mesh && bash scripts/setup-homelab.sh"
```

### CloudPC

```powershell
# Run as Administrator (from repo root)
powershell -ExecutionPolicy Bypass -File windows\setup.ps1
```

> **Note:** The CloudPC setup script was consolidated into `windows/setup.ps1` (handles SSH keys, sshd config, Tailscale, and dev tool installation).

## Mac SSH Config Explained

The Mac config uses smart routing to prefer LAN when available:

```
# Probe LAN with 0.5s timeout
Match host homelab exec "bash -c 'exec 3<>/dev/tcp/192.168.1.100/22' ..."
    HostName 192.168.1.100    # Use LAN if reachable

# Fallback to Tailscale
Host homelab
    HostName homelab.tail296462.ts.net
```

**Why bash instead of nc?**
macOS `nc -w` timeout flag doesn't work for connection timeouts, only idle timeouts. The bash `/dev/tcp` approach with background process + kill provides reliable sub-second timeouts.

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

### Mac LAN Probe Hanging

The old `nc -z -w1` approach hangs on macOS. Use the bash `/dev/tcp` method with explicit timeout.

## Security Notes

⚠️ **This setup uses a single shared key across all machines.** This is convenient but means:

- Compromise of any machine's private key compromises all
- Rotate keys with `ssh-mesh/scripts/rotate-keys.sh` (generates new keypair, deploys to all machines)
- Keep private key files secure (600 permissions)
- Never commit private keys to public repositories
