#!/usr/bin/env bash
# scripts/mac-autofs-dev.sh - macOS autofs mount of homelab's curated ~/dev root over NFSv3 (idempotent).
#
# Client side of the "mount ~/dev as a network drive" feature. Uses macOS's
# native automountd (autofs) — no third-party software — via a dedicated
# INDIRECT map mounted under /Volumes, with ONE key: "dev".
#
# CORRECTED 2026-07-22 (three changes, same day):
#
# 1. An earlier version of this script used a second "/-" DIRECT-map line
#    in /etc/auto_master, intending to avoid colliding with the built-in
#    "/-  -static" fstab-backed map already present there. That was wrong,
#    not just risky — macOS's auto_master only honors ONE "/-" (direct map)
#    entry; a second one is silently ignored by automountd, which is
#    exactly why "cd /Volumes/dev-personal" returned a plain "no such file
#    or directory" instead of an NFS-layer error on first live test. Fixed
#    with an indirect map under /Volumes instead — see
#    configure_auto_master_entry() below for the stale-line migration.
#
# 2. vers=4 -> vers=3+locallocks. v4 consistently failed with "RPC prog.
#    not avail" — root-caused to a kernel-level lockd/nfsdctl issue on
#    homelab's exact kernel version (see nfs-export.sh's header for the
#    full diagnostic trail). NFSv3 mounted successfully on first try.
#    locallocks is load-bearing, not cosmetic: it keeps all file-locking
#    operations client-side, so the mount never depends on lockd/statd
#    being reachable over the network — those run on unstable DYNAMIC
#    ports that change every nfs-server restart. nolocks (the more
#    obvious-looking option) is documented as NFSv2/v3-only too, but it
#    fully DISABLES locking rather than keeping it local — locallocks is
#    strictly better for a mount real editors/tools will touch.
#
# 3. 4 separate mounts -> 1 unified mount. Originally one autofs key per
#    exported dir (dev-personal, dev-priceless, ...), mounted as 4 SEPARATE
#    /Volumes/dev-* volumes. Leo wanted ONE /Volumes/dev that mirrors
#    ~/dev's structure, not 4 flat sibling mounts. Server side now exports
#    a single curated root (see nfs-export.sh's EXPORT_ROOT +
#    nfs-export-bindmounts.sh) — this script just needs ONE autofs key
#    ("dev") pointing at that root; /Volumes/dev/personal,
#    /Volumes/dev/brown, etc. all resolve correctly underneath it via the
#    server's `crossmnt` export option.
#
# Server side: scripts/homelab/nfs-export.sh.
#
# Mounts (lazy on first access, unmounted again on idle — standard automount
# behavior):
#   /Volumes/dev -> homelab:/srv/nfs-dev-export (personal/, priceless/, cc/,
#                   central-planning/, brown/ underneath — mirrors ~/dev)
#
# Addressed via Tailscale MagicDNS (homelab.tail296462.ts.net) rather than a
# raw IP, so this keeps working regardless of whether Tailscale is currently
# negotiating direct-LAN or relaying via DERP.
#
# Re-runs safely: /etc/auto_dev is content-compared before writing;
# /etc/auto_master (a shared system file with pre-existing entries) gets one
# grep-guarded append, never a wholesale rewrite; automount -vc reloads only
# when something actually changed.
#
# Verified end-to-end 2026-07-22 via SSH to the real Mac (4-mount version):
# mount, directory listing, and a write test all succeeded. Unified-mount
# version not yet independently re-verified — same mechanism, single key.
#
# CORRECTED 2026-07-24: MOUNT_PREFIX "/Volumes" -> "/System/Volumes/Data/Volumes".
# Root-caused live (if-y95z): every boot since 07-22
# actually failed to mount at all — `sudo automount -vc` on the real Mac
# returned `mount /System/Volumes/Data/Volumes: Operation not permitted`, not
# a network/NFS-layer error. Since macOS Catalina, `/Volumes` itself lives on
# the read-only sealed System volume; an autofs indirect map can't register
# ITS OWN mountpoint there — the map has to target the real writable path,
# `/System/Volumes/Data/Volumes`, directly (external confirmation: this is a
# documented macOS Catalina+ autofs restriction, not specific to this setup).
# `/Volumes/dev` still works fine as the day-to-day access path afterward —
# it's a firmlink to the same location — only the map's own registration
# needed the unaliased path. configure_auto_master_entry() below migrates the
# stale `/Volumes auto_dev` line the same way it already migrated the earlier
# stale `/-` line.

set -uo pipefail

DOTFILES="${DOTFILES:-$HOME/dev/personal/installfest}"

# shellcheck source=utils.sh
. "$DOTFILES/scripts/utils.sh"

NFS_HOST="homelab.tail296462.ts.net"
EXPORT_ROOT="/srv/nfs-dev-export"
MAP_FILE="/etc/auto_dev"
MASTER_FILE="/etc/auto_master"
MOUNT_PREFIX="/System/Volumes/Data/Volumes"
STALE_MOUNT_PREFIX="/Volumes"  # pre-2026-07-24 value — see configure_auto_master_entry() migration
MOUNT_OPTS="fstype=nfs,vers=3,resvport,nosuid,intr,locallocks"
MOUNT_KEY="dev"

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
# Step 1: /etc/auto_dev — indirect map, one key ("dev") for the whole
# curated export root
# ---------------------------------------------------------------------------

configure_auto_dev_map() {
    local content
    content=$(printf '%s\n' \
        "# Managed by scripts/mac-autofs-dev.sh — do not hand-edit." \
        "# Indirect-map mount of homelab's curated ~/dev root (NFSv3 over Tailscale)." \
        "# Mounted under ${MOUNT_PREFIX} — see auto_master's '${MOUNT_PREFIX} auto_dev' line." \
        "${MOUNT_KEY} -${MOUNT_OPTS} ${NFS_HOST}:${EXPORT_ROOT}")
    write_if_changed "$MAP_FILE" "$content"
}

# ---------------------------------------------------------------------------
# Step 2: /etc/auto_master — append the indirect-map reference once
# ---------------------------------------------------------------------------

configure_auto_master_entry() {
    # Migrate a stale DIRECT-map line from the earlier, broken version of
    # this script (see the header comment) — macOS only honors one "/-"
    # entry, and the built-in "/-  -static" line already occupies it. A
    # leftover second "/-  auto_dev" line is dead weight at best; remove it
    # before adding the correct indirect-map line.
    if [[ -f "$MASTER_FILE" ]] && grep -qE '^[[:space:]]*/-[[:space:]]+auto_dev([[:space:]]|$)' "$MASTER_FILE" 2>/dev/null; then
        info "removing stale direct-map auto_dev line (collided with the built-in /- -static line)"
        sudo sed -i '' -E '/^[[:space:]]*\/-[[:space:]]+auto_dev([[:space:]]|$)/d' "$MASTER_FILE" \
            || { error "failed to remove stale auto_master line"; exit 1; }
    fi

    # Migrate the pre-2026-07-24 "/Volumes auto_dev" line — that mountpoint
    # can never actually mount (see the header's CORRECTED 2026-07-24 note);
    # leaving it in place alongside the new correct line is dead weight at
    # best. Anchored on STALE_MOUNT_PREFIX specifically so this is a no-op
    # once already migrated (grep won't find it after the sed removes it).
    if [[ -f "$MASTER_FILE" ]] && grep -qE "^[[:space:]]*${STALE_MOUNT_PREFIX//\//\\/}[[:space:]]+auto_dev([[:space:]]|\$)" "$MASTER_FILE" 2>/dev/null; then
        info "removing stale auto_dev line under ${STALE_MOUNT_PREFIX} (mounts there are rejected by macOS — see header)"
        sudo sed -i '' -E "/^[[:space:]]*${STALE_MOUNT_PREFIX//\//\\/}[[:space:]]+auto_dev([[:space:]]|\$)/d" "$MASTER_FILE" \
            || { error "failed to remove stale ${STALE_MOUNT_PREFIX} auto_master line"; exit 1; }
    fi

    if [[ -f "$MASTER_FILE" ]] && grep -qE "^[[:space:]]*${MOUNT_PREFIX//\//\\/}[[:space:]]+auto_dev([[:space:]]|\$)" "$MASTER_FILE" 2>/dev/null; then
        success "$MASTER_FILE already references auto_dev under $MOUNT_PREFIX"
        return 0
    fi
    info "appending auto_dev indirect-map entry to $MASTER_FILE"
    printf '%s\n' "${MOUNT_PREFIX}		auto_dev	-nosuid" | sudo tee -a "$MASTER_FILE" >/dev/null \
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
    info "  autofs mount of homelab ~/dev (Tailscale + NFSv3)"
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

    success "autofs configured — first 'ls /Volumes/dev' will lazy-mount"
}

main "$@"
