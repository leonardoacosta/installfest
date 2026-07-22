#!/usr/bin/env bash
# scripts/homelab/nfs-export-bindmounts.sh - build the curated NFS export root
# (idempotent, run as root by dev-export-bindmounts.service).
#
# NFS exports a single directory-subtree with no built-in way to exclude a
# child (e.g. keep ~/dev/personal but hide ~/dev/archive) — so exporting the
# real ~/dev directly can't honor the archive/ exclusion. This script builds
# a separate curated root (EXPORT_ROOT) containing bind-mounts of exactly
# the wanted subtrees, so nfs-export.sh can export ONE clean directory
# (with `crossmnt`) that mirrors ~/dev's structure on the client side
# (/Volumes/dev/personal, /Volumes/dev/brown, etc.) without archive/ ever
# being reachable.
#
# Bind mounts don't survive a reboot on their own — this script is the
# ExecStart of dev-export-bindmounts.service (RemainAfterExit=yes), which
# nfs-server.service depends on via a drop-in override (see
# scripts/homelab/nfs-export.sh), so this reruns at every boot before nfsd
# starts serving.
#
# Re-runs safely: checks `mountpoint -q` before each bind mount.

set -uo pipefail

# NOTE: this runs as root via systemd (dev-export-bindmounts.service), so
# $HOME resolves to /root, not nyaptor's home — the exact bug found earlier
# in this feature with `sudo bash nfs-export.sh`. Hardcode the real path
# instead of deriving from $HOME/utils.sh's helpers (which aren't essential
# in a systemd-journal context anyway — plain echo is fine here).
DEV_ROOT="/home/nyaptor/dev"
EXPORT_ROOT="/srv/nfs-dev-export"
EXPORT_DIRS=(personal priceless cc central-planning brown)

main() {
    if [[ $EUID -ne 0 ]]; then
        echo "nfs-export-bindmounts: must run as root (bind mounts require it) — invoked via systemd, not directly" >&2
        exit 1
    fi

    mkdir -p "$EXPORT_ROOT"

    local d src dst
    for d in "${EXPORT_DIRS[@]}"; do
        src="$DEV_ROOT/$d"
        dst="$EXPORT_ROOT/$d"
        if [[ ! -d "$src" ]]; then
            echo "nfs-export-bindmounts: WARN source $src does not exist — skipping" >&2
            continue
        fi
        mkdir -p "$dst"
        if mountpoint -q "$dst"; then
            echo "nfs-export-bindmounts: $dst already bind-mounted"
            continue
        fi
        echo "nfs-export-bindmounts: bind-mounting $src -> $dst"
        mount --bind "$src" "$dst" || { echo "nfs-export-bindmounts: ERROR bind mount failed for $d" >&2; exit 1; }
    done
}

main "$@"
