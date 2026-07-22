#!/usr/bin/env bash
# scripts/mac-autofs-remount.sh - boot-time re-registration of the /Volumes/dev
# autofs map, gated on homelab being reachable over Tailscale.
#
# WHY THIS EXISTS
#
# scripts/mac-autofs-dev.sh installs the autofs map (/etc/auto_master +
# /etc/auto_dev) so /Volumes/dev lazy-mounts homelab's curated ~/dev export
# over NFSv3 via Tailscale MagicDNS. That config is correct and persists across
# reboots — but on a COLD BOOT macOS's automountd registers the map BEFORE
# Tailscale has finished negotiating connectivity to homelab.tail296462.ts.net.
# At that instant the NFS host is unresolvable/unreachable, the /Volumes/dev
# trigger never gets set up, and nothing retries. Symptom: after a reboot
# `ls /Volumes/dev` returns "No such file or directory"; a manual
# `sudo automount -vc` (run once Tailscale is up) fixes it immediately.
# Verified live 2026-07-22 on the real Mac.
#
# This script is the automatic replacement for that manual `automount -vc`.
# It is the Program of the `dev.leonardoacosta.autofs-remount` LaunchDaemon
# (installed by scripts/mac-autofs-remount-install.sh, wired via
# home/run_onchange_mac-autofs-remount.sh.tmpl). RunAtLoad fires it every boot;
# it waits until the NFS host is actually reachable, then runs automount once.
#
# REACHABILITY PROBE — deliberately variant-agnostic
#
# It probes `nc -z homelab.tail296462.ts.net 2049` (the NFS port), NOT
# `tailscale status`. A successful TCP connect proves BOTH that MagicDNS
# resolves the name (so Tailscale's DNS is up) AND that the NFS server is
# reachable — which is exactly the precondition `automount -vc` needs. Probing
# nc:2049 also sidesteps the question of WHICH Tailscale is installed: the
# Mac App Store build runs tailscaled inside a GUI-session sandbox whose CLI
# socket a root daemon can't reliably reach, so `tailscale status` is
# unreliable from here; nc uses the system resolver + a plain socket and works
# regardless of the Tailscale variant.
#
# Runs as root under launchd, so `automount` needs no sudo. One-shot per boot
# (no KeepAlive): once automount registers the trigger it persists for the rest
# of the boot session, so a single successful run suffices. Exits 0 even on
# timeout so launchd never flags a crash or respawn-storms.
#
# Ad-hoc: safe to run directly any time (e.g. `sudo scripts/mac-autofs-remount.sh`)
# to re-register the map after a Tailscale blip.

set -uo pipefail

NFS_HOST="homelab.tail296462.ts.net"
NFS_PORT=2049
MOUNT="/Volumes/dev"
MAX_WAIT=600          # seconds to wait for Tailscale/NFS reachability
INTERVAL=5            # reachability poll interval
MAX_ATTEMPTS=4        # automount -vc retries (re-registration is async)
PROBE_WINDOW=8        # seconds to probe the mount after each automount attempt
NC=/usr/bin/nc
AUTOMOUNT=/usr/sbin/automount

log() { printf '%s autofs-remount: %s\n' "$(date '+%Y-%m-%dT%H:%M:%S%z')" "$*"; }

# Only meaningful on macOS; a stray invocation elsewhere is a clean no-op.
if [[ "$(uname -s)" != "Darwin" ]]; then
    log "not Darwin — nothing to do"
    exit 0
fi

log "waiting up to ${MAX_WAIT}s for ${NFS_HOST}:${NFS_PORT} (Tailscale + NFS) to be reachable"
waited=0
until "$NC" -z -G 4 "$NFS_HOST" "$NFS_PORT" >/dev/null 2>&1; do
    if (( waited >= MAX_WAIT )); then
        log "TIMEOUT after ${MAX_WAIT}s — ${NFS_HOST}:${NFS_PORT} never reachable; giving up (exit 0)"
        exit 0
    fi
    sleep "$INTERVAL"
    waited=$(( waited + INTERVAL ))
done
log "reachable after ${waited}s — re-registering autofs maps"

# `automount -vc` flushes automountd's caches so the next access to the
# (already-registered) /Volumes/dev indirect map re-triggers the NFS mount —
# the exact manual fix the original reboot diagnosis proved. autofsd keeps the
# map registered across a reboot; only the mount needs re-triggering once
# Tailscale is up. The mount can settle a moment after the flush returns, so we
# flush then probe the listing in a short window, and retry a few times as cheap
# insurance. Verified live 2026-07-22: against a registered-map/unmounted state
# (the boot case) it heals on the first attempt in ~2s.
attempt=0
while (( attempt < MAX_ATTEMPTS )); do
    attempt=$(( attempt + 1 ))
    log "automount -vc (attempt ${attempt}/${MAX_ATTEMPTS})"
    "$AUTOMOUNT" -vc 2>&1 | sed 's/^/  automount: /' || log "'automount -vc' returned non-zero"
    for _ in $(seq 1 "$PROBE_WINDOW"); do
        if /bin/ls "$MOUNT" >/dev/null 2>&1; then
            log "OK: ${MOUNT} available after ${attempt} automount attempt(s)"
            exit 0
        fi
        sleep 1
    done
    log "${MOUNT} not listable yet after attempt ${attempt}"
done
log "WARN: ${MOUNT} still not listable after ${MAX_ATTEMPTS} automount attempts"
exit 0
