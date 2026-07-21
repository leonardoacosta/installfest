#!/usr/bin/env bash
# scripts/mac-autofs-dev.sh - macOS autofs mount of homelab's ~/dev over NFSv4 (idempotent).
#
# Client side of the "mount ~/dev as a network drive" feature. Uses macOS's
# native automountd (autofs) — no third-party software — via a dedicated
# direct map, so this never collides with the built-in "/-  -static" fstab
# map already present in /etc/auto_master.
#
# Server side: scripts/homelab/nfs-export.sh.
#
# Mounts (lazy on first access, unmounted again on idle — standard automount
# behavior):
#   /Volumes/dev-personal         -> homelab:/home/nyaptor/dev/personal
#   /Volumes/dev-priceless        -> homelab:/home/nyaptor/dev/priceless
#   /Volumes/dev-cc               -> homelab:/home/nyaptor/dev/cc
#   /Volumes/dev-central-planning -> homelab:/home/nyaptor/dev/central-planning
#
# Addressed via Tailscale MagicDNS (homelab.tail296462.ts.net) rather than a
# raw IP, so this keeps working regardless of whether Tailscale is currently
# negotiating direct-LAN or relaying via DERP.
#
# vers=4 is load-bearing, not cosmetic: nfs-export.sh exports NFSv4-only (no
# rpcbind/portmapper on the server), so a default mount_nfs negotiation that
# falls back to v3 would fail outright without it.
#
# Re-runs safely: /etc/auto_dev is content-compared before writing;
# /etc/auto_master (a shared system file with pre-existing entries) gets one
# grep-guarded append, never a wholesale rewrite; automount -vc reloads only
# when something actually changed.
#
# NOT verified end-to-end from this session — homelab has no macOS shell
# access. Written correct-by-inspection against documented autofs/auto_master
# syntax; the real "cd /Volumes/dev-personal" mount test is a pending manual
# step for a Mac-side session (see chezmoi apply + this script's own output).

set -uo pipefail

DOTFILES="${DOTFILES:-$HOME/dev/personal/installfest}"

# shellcheck source=utils.sh
. "$DOTFILES/scripts/utils.sh"

NFS_HOST="homelab.tail296462.ts.net"
DEV_ROOT="/home/nyaptor/dev"
MAP_FILE="/etc/auto_dev"
MASTER_FILE="/etc/auto_master"
MOUNT_OPTS="fstype=nfs,vers=4,resvport,nosuid,intr"
EXPORT_DIRS=(personal priceless cc central-planning)

# ---------------------------------------------------------------------------
# Preflight
# ---------------------------------------------------------------------------

assert_macos() {
    [[ "$(uname -s)" == "Darwin" ]] || { warning "Not macOS — skipping"; exit 0; }
}

require_sudo() {
    if [[ $EUID -ne 0 ]] && ! sudo -n true 2>/dev/null; then
        info "autofs setup needs sudo — may prompt once:"
        sudo -v || { error "sudo unavailable, aborting"; exit 1; }
    fi
}

# ---------------------------------------------------------------------------
# write_if_changed <path> <content>
#   Same content-compare-before-write idiom as scripts/homelab/harden.sh /
#   scripts/homelab/nfs-export.sh, adapted for macOS ownership (root:wheel).
# ---------------------------------------------------------------------------

write_if_changed() {
    local path="$1"
    local content="$2"
    local perm="${PERM:-0644}"

    if [[ -f "$path" ]] && [[ "$(sudo cat "$path" 2>/dev/null)" == "$content" ]]; then
        return 0  # unchanged
    fi

    printf '%s\n' "$content" | sudo tee "$path" >/dev/null
    sudo chmod "$perm" "$path"
    sudo chown root:wheel "$path"
    success "wrote $path"
    return 10  # sentinel: file changed
}

# ---------------------------------------------------------------------------
# Step 1: /etc/auto_dev — direct map, one line per exported ~/dev subtree
# ---------------------------------------------------------------------------

configure_auto_dev_map() {
    local lines=(
        "# Managed by scripts/mac-autofs-dev.sh — do not hand-edit."
        "# Direct-map mounts of homelab's ~/dev subtrees (NFSv4 over Tailscale)."
    )
    local d
    for d in "${EXPORT_DIRS[@]}"; do
        lines+=("/Volumes/dev-$d -${MOUNT_OPTS} ${NFS_HOST}:${DEV_ROOT}/$d")
    done
    local content
    content=$(printf '%s\n' "${lines[@]}")
    write_if_changed "$MAP_FILE" "$content"
}

# ---------------------------------------------------------------------------
# Step 2: /etc/auto_master — append the direct-map reference once
# ---------------------------------------------------------------------------

configure_auto_master_entry() {
    if [[ -f "$MASTER_FILE" ]] && grep -qE '^[[:space:]]*/-[[:space:]]+auto_dev([[:space:]]|$)' "$MASTER_FILE" 2>/dev/null; then
        success "$MASTER_FILE already references auto_dev"
        return 0
    fi
    info "appending auto_dev direct-map entry to $MASTER_FILE"
    printf '%s\n' "/-			auto_dev	-nosuid" | sudo tee -a "$MASTER_FILE" >/dev/null \
        || { error "failed to append to $MASTER_FILE"; exit 1; }
    success "appended auto_dev entry to $MASTER_FILE"
    return 10
}

# ---------------------------------------------------------------------------
# Step 3: reload
# ---------------------------------------------------------------------------

reload_automount() {
    info "reloading automount maps"
    sudo automount -vc || { error "automount -vc failed"; exit 1; }
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

main() {
    info "========================================"
    info "  autofs mount of homelab ~/dev (Tailscale + NFSv4)"
    info "========================================"

    assert_macos
    require_sudo

    local changed=0
    configure_auto_dev_map
    [[ $? -eq 10 ]] && changed=1
    configure_auto_master_entry
    [[ $? -eq 10 ]] && changed=1

    if [[ $changed -eq 1 ]]; then
        reload_automount
    else
        success "autofs config already up to date — no reload needed"
    fi

    success "autofs configured — first 'ls /Volumes/dev-personal' (etc.) will lazy-mount"
}

main "$@"
