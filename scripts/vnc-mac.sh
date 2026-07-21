#!/usr/bin/env bash
# vnc-mac.sh — the single front door for "view/control my Mac's screen" from the
# headless homelab. Opens a VNC session to the Mac's built-in Screen Sharing
# (RFB) server over the existing Tailscale ssh-mesh, using tigervnc's vncviewer.
#
# The Mac's Screen Sharing server is enabled/kept-on by the chezmoi-managed
# home/run_onchange_darwin-screen-sharing.sh.tmpl (the server side of this pair).
#
# The Mac's Tailscale hostname is NOT hardcoded here — it is resolved from the
# existing `mac` SSH Host alias via `ssh -G mac` (the `hostname` line), so this
# script inherits whatever hostname home/private_dot_ssh/config.tmpl already
# defines and never drifts from it. Change the alias in one place, both follow.
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
#   VNC_MAC_HOST    ssh host alias for the Mac        (default: mac)
#   VNC_MAC_PORT    Screen Sharing / RFB port         (default: 5900)

set -uo pipefail

MAC_HOST="${VNC_MAC_HOST:-mac}"
VNC_PORT="${VNC_MAC_PORT:-5900}"

# --- Arg parsing -------------------------------------------------------------
mode="connect"   # connect | print
passthrough=()
for arg in "$@"; do
  case "$arg" in
    -h|--help)
      sed -n '14,17p' "$0" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    --print) mode="print" ;;
    *) passthrough+=("$arg") ;;
  esac
done

# --- Resolve the Mac's real hostname from the `mac` ssh alias -----------------
# `ssh -G <alias>` prints the fully-resolved effective config; the `hostname`
# line is the Tailscale MagicDNS name the alias points at. No second hardcoded
# copy of macbook.tail296462.ts.net lives here.
resolve_mac_host() {
  ssh -G "$MAC_HOST" 2>/dev/null | awk '/^hostname /{print $2; exit}'
}

mac_hostname="$(resolve_mac_host)"
if [ -z "$mac_hostname" ]; then
  echo "vnc-mac: could not resolve a hostname for ssh alias '$MAC_HOST'." >&2
  echo "         Check the '$MAC_HOST' Host block in ~/.ssh/config (ssh -G $MAC_HOST)." >&2
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
exec vncviewer "${passthrough[@]}" "$target"
