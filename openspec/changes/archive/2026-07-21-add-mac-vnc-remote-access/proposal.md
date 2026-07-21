---
order: 0721b
---

# Proposal: Mac VNC Remote Access

## Change ID
`add-mac-vnc-remote-access`

## Summary
Enable macOS's built-in Screen Sharing (VNC/RFB server) on the Mac, chezmoi-managed so it stays
enabled across every `chezmoi apply`, and give homelab a VNC client plus a front-door launcher so
Leo can view/control the Mac from homelab over the existing Tailscale-only ssh-mesh.

## Context
- Extends: `platform/homebrew/Brewfile` (`tiger-vnc` already installed, currently unreferenced
  per `docs/audit/homebrew.md`), `scripts/install-arch.sh`, `home/private_dot_ssh/config.tmpl`
  (reuses the existing `mac` Host alias's Tailscale MagicDNS hostname).
- Related: `ssh-mesh/README.md` (the 3-machine Tailscale mesh this rides on top of, no new
  tunnel needed); `openspec/specs/ssh-mesh/spec.md` (adjacent capability — SSH key auth, not
  screen sharing, kept as a separate `remote-access` capability rather than folded in).
- touches: `home/run_onchange_darwin-screen-sharing.sh.tmpl`, `scripts/install-arch.sh`,
  `scripts/vnc-mac.sh`, `CLAUDE.md`

## Motivation
Leo wants to view/control the Mac's screen from the headless homelab box (or, later, from
elsewhere on the tailnet), managed the same declarative way the rest of this repo's cross-machine
tooling is (chezmoi + ssh-mesh), rather than a one-off manual toggle in System Settings that
config drift can silently undo.

## Requirements
### Requirement: Mac runs a chezmoi-managed Screen Sharing (VNC) server
See `specs/remote-access/spec.md` — macOS's built-in Screen Sharing service (Apple's RFB/VNC
server, authenticated with real macOS user accounts) is enabled via a chezmoi run_onchange
script, idempotently, on every apply — not the classic `kickstart`/ARD flow, which is
MDM-restricted for full Remote Management on macOS 12.1+; plain Screen Sharing is unaffected by
that restriction and is toggled via `launchctl enable system/com.apple.screensharing` +
`launchctl load -w`.

### Requirement: Homelab has a VNC client and a front-door launcher
See `specs/remote-access/spec.md` — the `tigervnc` package (providing `vncviewer`) is added to
homelab's Arch package list, and a `scripts/vnc-mac.sh` front-door script (matching the existing
`mac-open.sh`/`view.sh` conventions) launches a session against the Mac's Tailscale hostname.

## Scope
- **IN**: Mac-side Screen Sharing (VNC server) enablement via chezmoi; homelab-side `tigervnc`
  client install; a `vnc-mac` front-door script; a CLAUDE.md Front-Door Tools entry.
- **OUT**: Any VNC *server* on homelab (explicitly dropped — homelab is headless/CLI-only and
  the operator chose not to stand up a virtual X/Xvnc desktop for it). Any VNC client or server
  on cloudpc (Windows bastion) — explicitly excluded, cloudpc needs neither side of this.
  pf/firewall scoping of port 5900 to the Tailscale interface specifically (LAN is already
  trusted and there is no port-forward at the router) — filed as a follow-up bead, not bundled
  here.

## Done Means
- The Mac's Screen Sharing service is enabled and kept enabled by a chezmoi-tracked script —
  re-running `chezmoi apply` enforces the desired state, no manual toggle needed.
- Homelab has a working VNC client (`vncviewer` from the `tigervnc` package) installed via
  `install-arch.sh`.
- Running `vnc-mac.sh` from homelab opens a live, authenticated VNC session to the Mac over its
  Tailscale hostname (`macbook.tail296462.ts.net`, reused from the existing `mac` ssh alias).
- No new port-forwarding or non-Tailscale exposure is introduced.

## Testing
| Affected seam | Unit task | E2E task |
|----------------|-----------|----------|
| Mac Screen Sharing enable script (chezmoi run_onchange) | N/A — shell/config, no unit harness in this repo | `[4.1]` manual verification |
| homelab `tigervnc` package install | N/A — package list addition | `[4.1]` manual verification |
| `scripts/vnc-mac.sh` front-door launcher | N/A — shell/config, no unit harness in this repo | `[4.1]` manual verification |

## Impact
| Area | Change |
|------|--------|
| Mac | Screen Sharing (Apple's built-in VNC/RFB server) enabled, chezmoi-managed |
| Homelab | `tigervnc` package added (client only — `vncviewer`) |
| Repo | New `scripts/vnc-mac.sh` front-door script; CLAUDE.md doc entry |

## Risks
| Risk | Mitigation |
|------|-----------|
| Screen Sharing binds all interfaces (port 5900), not just Tailscale | No router port-forward exists; LAN is already the same trust boundary the rest of the mesh relies on. pf-scoping to the Tailscale interface is a documented, deliberate follow-up, not blocking. |
| `launchctl`-based enable behaves differently across macOS versions | Verified against 3 independent current sources (til.hashrocket.com, TechRepublic, Jamf community) that this path (distinct from the MDM-restricted `kickstart`/ARD flow) works on current macOS; task `[4.1]` is the real-machine confirmation. |
