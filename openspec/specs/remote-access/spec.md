# remote-access Specification

## Purpose
TBD - created by archiving change add-mac-vnc-remote-access. Update Purpose after archive.
## Requirements
### Requirement: Mac runs a chezmoi-managed Screen Sharing (VNC) server
The Mac SHALL run macOS's built-in Screen Sharing service (Apple's RFB/VNC-compatible server,
authenticated against real macOS user accounts) enabled and kept enabled by a Darwin-only,
idempotent chezmoi script — not the classic `kickstart -activate -configure` Apple Remote
Desktop flow, which MUST NOT be relied on for enabling this, since that path is MDM-only for
full Remote Management on macOS 12.1+. The plain Screen Sharing toggle (`launchctl enable
system/com.apple.screensharing` + `launchctl load -w
/System/Library/LaunchDaemons/com.apple.screensharing.plist`) SHALL be used instead, and SHALL
be safe to re-run on every `chezmoi apply` without erroring or duplicating state.

#### Scenario: Fresh chezmoi apply on the Mac
- **WHEN** `chezmoi apply` runs on the Mac and Screen Sharing is not yet enabled
- **THEN** the Darwin-only run_onchange script enables Screen Sharing via `launchctl`, and the
  service is reachable on port 5900 afterward

#### Scenario: Screen Sharing already enabled
- **WHEN** `chezmoi apply` runs again and Screen Sharing is already enabled
- **THEN** the script is a no-op (idempotent) — it does not error, disable, or re-toggle the
  service

#### Scenario: Non-Darwin machine
- **WHEN** `chezmoi apply` runs on homelab (Linux) or is templated for cloudpc (Windows)
- **THEN** this script SHALL NOT run at all — it is gated to `.chezmoi.os == "darwin"` only

### Requirement: Homelab has a VNC client and a front-door launcher to reach the Mac
Homelab SHALL have a working VNC client (`vncviewer`, from the `tigervnc` Arch package) available
via `scripts/install-arch.sh`, and a `scripts/vnc-mac.sh` front-door script SHALL let Leo open a
VNC session to the Mac by its existing Tailscale MagicDNS hostname (reused from the `mac` SSH
Host alias, never a second hardcoded copy of the hostname) without hand-typing a `vncviewer`
invocation. Homelab SHALL NOT run its own VNC server as part of this capability, and cloudpc
SHALL NOT gain a VNC client or server as part of this capability.

#### Scenario: Fresh homelab install
- **WHEN** `install-arch.sh` runs on a fresh or updated homelab machine
- **THEN** the `tigervnc` package is installed, providing a working `vncviewer` binary

#### Scenario: Launching a session from homelab
- **WHEN** `scripts/vnc-mac.sh` is run on homelab with the Mac's Screen Sharing enabled
- **THEN** a `vncviewer` session opens against the Mac's Tailscale hostname, prompting for the
  Mac's real user-account credentials

#### Scenario: Mac unreachable or Screen Sharing not yet enabled
- **WHEN** `scripts/vnc-mac.sh` is run and the Mac's Screen Sharing service is not reachable
  (offline, service not yet enabled, or Tailscale down)
- **THEN** the script SHALL fail with a clear, actionable message rather than hanging silently

#### Scenario: cloudpc and homelab-as-server stay out of scope
- **WHEN** this capability ships
- **THEN** cloudpc gains neither a VNC client nor server, and homelab gains no VNC server — both
  remain explicitly out of scope for this capability

