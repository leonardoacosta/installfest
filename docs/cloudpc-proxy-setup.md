# Routing Apps Through CloudPC Network

Route Microsoft apps (Teams, Outlook, Edge) through cloudpc's network to pass Microsoft Conditional
Access Evaluation (CAE). CAE checks the originating IP — your local machine isn't trusted, but
cloudpc is.

## Architecture

```
macOS:  App (Teams) → ProxyBridge (per-app intercept) → SSH SOCKS5 → cloudpc → Microsoft
Linux:  App (Edge)  → proxychains-ng (LD_PRELOAD)     → SSH SOCKS5 → cloudpc → Microsoft
```

- **TCP traffic** (auth, SSO, API): routes through cloudpc — CAE passes
- **UDP traffic** (calls, video, media): goes direct — no latency penalty
- **All other apps**: unaffected — stay direct

## Prerequisites

- SSH access to `cloudpc` (configured in `~/.ssh/config`)
- **macOS**: [ProxyBridge](https://github.com/InterceptSuite/ProxyBridge) (installed by `chezmoi apply`)
- **Linux**: `proxychains-ng` (installed by `scripts/install-arch.sh`)

## Setup

### 1. SOCKS5 Tunnel (both platforms)

The tunnel auto-starts on login via platform services (deployed by chezmoi):

- **macOS**: `~/Library/LaunchAgents/com.leonardoacosta.cloudpc-tunnel.plist`
- **Linux**: `~/.config/systemd/user/cloudpc-tunnel.service`

To enable after first `chezmoi apply`:

```bash
# macOS
launchctl load ~/Library/LaunchAgents/com.leonardoacosta.cloudpc-tunnel.plist

# Linux
systemctl --user enable --now cloudpc-tunnel.service
```

Manual start (if needed):

```bash
ssh -D 1080 -f -N cloudpc
```

### 2a. macOS — ProxyBridge

ProxyBridge is installed automatically by `chezmoi apply` (via `run_once_install-packages.sh`).

**First-launch (one-time manual step):**
1. Open ProxyBridge
2. Approve Network Extension: System Settings → General → Login Items & Extensions → Network Extensions

**Add proxy server:**
- Type: `SOCKS5`
- Host: `127.0.0.1`
- Port: `1080`

**Import rules** (Menu Bar → Proxy → Proxy Rules → Import):

Use the standalone rules file: `scripts/proxybridge-rules.json`

Or import inline:

```json
[
  { "action": "PROXY", "enabled": true, "processNames": "MSTeams", "protocol": "TCP", "targetHosts": "*", "targetPorts": "*" },
  { "action": "PROXY", "enabled": true, "processNames": "Microsoft Teams WebView Helper", "protocol": "TCP", "targetHosts": "*", "targetPorts": "*" },
  { "action": "PROXY", "enabled": true, "processNames": "Microsoft Teams", "protocol": "TCP", "targetHosts": "*", "targetPorts": "*" },
  { "action": "PROXY", "enabled": true, "processNames": "Microsoft Outlook", "protocol": "TCP", "targetHosts": "*", "targetPorts": "*" },
  { "action": "PROXY", "enabled": true, "processNames": "Microsoft Edge", "protocol": "TCP", "targetHosts": "*", "targetPorts": "*" },
  { "action": "PROXY", "enabled": true, "processNames": "Microsoft Edge Helper", "protocol": "TCP", "targetHosts": "*", "targetPorts": "*" }
]
```

### 2b. Linux — proxychains-ng

proxychains-ng is installed by `scripts/install-arch.sh`. Config deployed to
`~/.config/proxychains/proxychains.conf`.

Wrap any command manually:

```bash
proxychains4 -f ~/.config/proxychains/proxychains.conf <command>
```

### 3. Restart proxied apps

Quit and reopen Teams, Outlook, and Edge. Both ProxyBridge (macOS) and proxychains (Linux)
intercept TCP connections on launch.

## Verification

```bash
# Verify tunnel is routing through CloudPC (both platforms)
curl --socks5-hostname localhost:1080 https://ifconfig.me
# Should return cloudpc's IP, not your machine's

# Linux — verify proxychains
proxychains4 -f ~/.config/proxychains/proxychains.conf curl -s https://ifconfig.me
# Should return cloudpc's IP

# Check tunnel process
pgrep -f "ssh.*-D.*1080.*cloudpc" && echo "Running" || echo "Not running"
```

**macOS — ProxyBridge logs** should show:
```
[INFO] SOCKS5 connection established to 51.116.253.170:443
```

If you see `→ Direct` on proxied connections, the rules aren't matching — check process names.

## Gotchas

### 1. Process Names (macOS ProxyBridge)

Microsoft apps spawn multiple processes. All need rules:

| Process | App | What it does |
|---------|-----|-------------|
| `MSTeams` | Teams | Main Teams process |
| `Microsoft Teams WebView Helper` | Teams | WebView2 content renderer |
| `Microsoft Teams` | Teams | Parent app process |
| `Microsoft Outlook` | Outlook | Mail/calendar process |
| `Microsoft Edge` | Edge | Main browser process |
| `Microsoft Edge Helper` | Edge | Renderer/utility subprocess |

### 2. TCP Only — No UDP

SSH SOCKS5 (`ssh -D`) only supports TCP. Both ProxyBridge (macOS) and proxychains-ng (Linux) are
configured for TCP only. UDP media (calls/video) goes direct with no latency penalty.

### 3. macOS: Network Extension Must Be Approved

ProxyBridge rules stay disabled until the macOS Network Extension is approved:
System Settings → General → Login Items & Extensions → Network Extensions

### 4. Linux: proxychains-ng Limitations

- Only works with **dynamically linked** binaries (most Electron apps are fine)
- Does NOT work with Flatpak/Snap apps (sandbox blocks LD_PRELOAD)
- If an app is statically linked, use `graftcp` (ptrace-based) as fallback

### 5. Tunnel Must Be Running First

Both platforms route to `127.0.0.1:1080`. If the tunnel isn't running, proxied connections fail.
The systemd/LaunchAgent services auto-restart on failure.

## Adding More Apps

**macOS:** Open ProxyBridge logs to see the actual process name, add a rule with that name + TCP.

**Linux:** Create a new wrapper in `~/.local/bin/`:
```bash
#!/bin/sh
exec proxychains4 -f "$HOME/.config/proxychains/proxychains.conf" <binary-name> "$@"
```

## az CLI Wrapper

The `az` CLI wrapper at `~/.local/bin/az` routes all Azure CLI traffic through the CloudPC tunnel
and auto-selects the correct Azure identity based on what you're calling.

### Dual Identity Architecture

| Identity | Config Dir | Account | Used For |
|----------|-----------|---------|----------|
| **BBAdmin** (default) | `~/.azure-bbadmin` | BBAdminLAcosta@bbins.com | Azure RM, ADO, PIM |
| **O365** | `~/.azure-o365` | leonardo.acosta@bridgespecialty.com | Microsoft Graph API |

Each identity has its own `AZURE_CONFIG_DIR` — both stay logged in simultaneously, no switching.

### Auto-Detection Logic

| Command Pattern | Identity Selected |
|----------------|------------------|
| `az rest --url https://graph.microsoft.com/*` | O365 |
| `az rest --resource https://graph.microsoft.com*` | O365 |
| `az devops *`, `az pipelines *`, `az repos *` | BBAdmin |
| `az group *`, `az vm *`, `az webapp *` | BBAdmin |
| Any unrecognized command | BBAdmin (default) |

### Override Flags

```bash
az account show --as-o365     # Force O365 identity
az account show --as-admin    # Force BBAdmin identity (explicit default)
```

Flags are stripped before passing to the real `az` binary.

### First-Time Setup

```bash
scripts/setup-az-wrapper.sh
```

This creates config directories, verifies dependencies, and runs device-code login for both
identities. Interactive — requires browser sign-in.

### Manual Login (token refresh)

```bash
az login --use-device-code --as-o365     # Refresh O365 token
az login --use-device-code --as-admin    # Refresh BBAdmin token
```

### Verification

```bash
az account show --as-o365     # Should show leonardo.acosta@bridgespecialty.com
az account show --as-admin    # Should show BBAdminLAcosta@bbins.com
which az                       # Should return ~/.local/bin/az (wrapper)
```

## Platform Comparison

| Feature | macOS (ProxyBridge) | Linux (proxychains-ng) |
|---------|--------------------|-----------------------|
| Mechanism | Network Extension (kernel) | LD_PRELOAD (userspace) |
| Per-app routing | Rule-based (process name) | Wrapper scripts |
| Always-on | Yes (intercepts matching apps) | Per-launch (must use wrapper) |
| TCP-only | Rule config | By design |
| Flatpak/Snap | N/A | Not supported |
| Setup | Auto-install + one manual approval | Auto-install via pacman |
