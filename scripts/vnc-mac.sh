#!/usr/bin/env bash
# vnc-mac.sh — the single front door for "view/control my Mac's screen" from the
# headless homelab. Opens a VNC session to the Mac's built-in Screen Sharing
# (RFB) server over Tailscale, using tigervnc's vncviewer.
#
# The Mac's Screen Sharing server is enabled/kept-on by the chezmoi-managed
# home/run_onchange_darwin-screen-sharing.sh.tmpl (the server side of this pair).
#
# The Mac's Tailscale hostname is NOT hardcoded here. It is resolved live, in
# order:
#   1. Tailscale's own device registry (`tailscale status --json`) — the peer
#      whose HostName/DNSName contains $MAC_HOST (default "mac"). Portable from
#      ANY tailnet device (incl. the Mac itself); Tailscale's own state is the
#      single source of truth, no machine-local ssh alias required.
#   2. Fallback: the `mac` SSH Host alias (`ssh -G mac`, the `hostname` line) —
#      for a machine with ssh-config aliasing but no reachable tailscale CLI.
# Either way, no second hardcoded copy of macbook.tail296462.ts.net lives here.
#
# Usage:
#   vnc-mac.sh [vncviewer-args...]    connect to the Mac's screen (:5900)
#   vnc-mac.sh --print                print the resolved host:port and exit
#   vnc-mac.sh -h | --help            show this usage
#
# Any extra args are passed through verbatim to vncviewer (e.g. -FullScreen,
# -Shared, -passwordFile <f>).
#
# Env:
#   VNC_MAC_HOST    ssh alias / tailscale name match for the Mac (default: mac)
#   VNC_MAC_PORT    Screen Sharing / RFB port                    (default: 5900)

set -uo pipefail

MAC_HOST="${VNC_MAC_HOST:-mac}"
VNC_PORT="${VNC_MAC_PORT:-5900}"

# --- Arg parsing -------------------------------------------------------------
mode="connect"   # connect | print
passthrough=()
for arg in "$@"; do
  case "$arg" in
    -h|--help)
      sed -n '19,22p' "$0" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    --print) mode="print" ;;
    *) passthrough+=("$arg") ;;
  esac
done

# --- Resolve the Mac's real Tailscale hostname -------------------------------
# Strategy 1 (preferred): live Tailscale peer lookup. Tailscale's own device
# registry is authoritative and portable — this resolves the same from ANY
# tailnet device, including the Mac itself, unlike a machine-local ssh alias.
# Strategy 2 (fallback): the `ssh -G <alias>` hostname line, for a box that has
# ssh-config aliasing but no reachable tailscale CLI.

# Locate the tailscale CLI. On macOS it is frequently GUI-app-only with no PATH
# entry, so fall back to the binary bundled inside Tailscale.app.
find_tailscale_bin() {
  if command -v tailscale >/dev/null 2>&1; then
    command -v tailscale
    return 0
  fi
  local app="/Applications/Tailscale.app/Contents/MacOS/Tailscale"
  if [ -x "$app" ]; then
    printf '%s\n' "$app"
    return 0
  fi
  return 1
}

# Strategy 1: find the tailnet peer (Self or any Peer) whose HostName or DNSName
# contains $MAC_HOST (case-insensitive substring); emit its DNSName sans trailing
# dot. Empty output when tailscale/jq are unavailable or nothing matches.
resolve_via_tailscale() {
  local ts_bin
  command -v jq >/dev/null 2>&1 || return 0
  ts_bin="$(find_tailscale_bin)" || return 0
  "$ts_bin" status --json 2>/dev/null | jq -r --arg needle "$MAC_HOST" '
    ([.Self] + [.Peer[]?])
    | map(select(
        ((.HostName // "") | ascii_downcase | contains($needle | ascii_downcase))
        or ((.DNSName // "") | ascii_downcase | contains($needle | ascii_downcase))
      ))
    | (.[0].DNSName // "")
    | rtrimstr(".")
  ' 2>/dev/null
}

# Strategy 2: the resolved `hostname` from the ssh Host alias.
resolve_via_ssh_alias() {
  ssh -G "$MAC_HOST" 2>/dev/null | awk '/^hostname /{print $2; exit}'
}

resolve_mac_host() {
  local h
  h="$(resolve_via_tailscale)"
  if [ -n "$h" ]; then
    printf '%s\n' "$h"
    return 0
  fi
  h="$(resolve_via_ssh_alias)"
  if [ -n "$h" ]; then
    printf '%s\n' "$h"
    return 0
  fi
  return 1
}

mac_hostname="$(resolve_mac_host)"
if [ -z "$mac_hostname" ]; then
  echo "vnc-mac: could not resolve a hostname for '$MAC_HOST'." >&2
  echo "         Tried: tailscale peer lookup (tailscale status --json), then the" >&2
  echo "         '$MAC_HOST' ssh Host alias (ssh -G $MAC_HOST). Neither produced a name." >&2
  echo "         Check that Tailscale is running, or that a '$MAC_HOST' Host block exists." >&2
  exit 1
fi

# tigervnc: `host::PORT` (double colon) is an explicit raw RFB port, vs a single
# colon which means a display number. Screen Sharing answers on 5900 (display 0).
target="${mac_hostname}::${VNC_PORT}"

if [ "$mode" = "print" ]; then
  echo "$target"
  exit 0
fi

# --- Launch the viewer -------------------------------------------------------
if ! command -v vncviewer >/dev/null 2>&1; then
  echo "vnc-mac: vncviewer not found — install the tigervnc package" >&2
  echo "         (it is in scripts/install-arch.sh; run: sudo pacman -S --needed tigervnc)." >&2
  exit 1
fi

echo "vnc-mac: connecting to $MAC_HOST ($target) ..." >&2
exec vncviewer ${passthrough[@]+"${passthrough[@]}"} "$target"
