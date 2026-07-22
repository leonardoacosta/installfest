#!/usr/bin/env bash
# scripts/homelab/nfs-export.sh - NFSv3 export of ~/dev subtrees, Tailscale-only (idempotent).
#
# Backing script for the "mount ~/dev as a network drive" feature: exposes 4
# top-level ~/dev directories over NFS so the Mac can autofs-mount them (see
# scripts/mac-autofs-dev.sh for the client side):
#
#   personal, priceless, cc, central-planning
#
# Deliberately NOT exported:
#   - archive           (dead/stale clones)
#   - brown             (Brown & Wilson client work — kept off any network
#                         share on principle)
#
# CORRECTED 2026-07-22: this was originally NFSv4-only (single port, 2049).
# Live-verified against the real Mac client: v4 mounts fail with "RPC prog.
# not avail" from a kernel-level lockd/nfsdctl issue on this host's kernel
# (7.1.3-arch2-2) — `nfsdctl: lockd configuration failure` in
# `journalctl -u nfs-server` the moment lockd's port is pinned, and the
# in-kernel nlockmgr registration got stuck even after a clean service
# restart (confirmed via `rpcinfo -p localhost` and a failed `modprobe -r
# nfsd`, blocked by /proc/fs/nfsd holding a reference). NFSv3 mounts
# succeeded immediately with the same exports, no server-side change needed.
# The original "v4 = single port" rationale is moot anyway: getting NFSv3
# locking working still only needs 3 FIXED ports (rpcbind 111, mountd
# 20048 — both stable across restarts on this system, confirmed via
# `rpcinfo -p`), not the dynamic lockd/statd ports one might expect — the
# client mount uses `locallocks` (see scripts/mac-autofs-dev.sh), which
# keeps all file locking client-side and never touches lockd/statd over the
# network at all, so their unstable dynamic ports don't need firewall rules.
#
# Client scope is restricted to the Tailscale CGNAT range (100.64.0.0/10),
# never the general LAN — ufw is active on this host and this must not be
# reachable from the wider home network.
#
# all_squash,anonuid=1000,anongid=1000 maps every remote client to nyaptor's
# own uid/gid, sidestepping the Mac's leonardoacosta vs homelab's nyaptor UID
# mismatch without idmapd configuration.
#
# Re-runs safely: /etc/exports is content-compared before writing (exportfs
# only reloaded on change), nfs-server.service is guarded by
# systemctl is-active/is-enabled, and each ufw rule is checked before adding —
# same conventions as scripts/homelab/harden.sh's write_if_changed /
# service-guard idiom.

set -uo pipefail

DOTFILES="${DOTFILES:-$HOME/dev/personal/installfest}"

# shellcheck source=../utils.sh
. "$DOTFILES/scripts/utils.sh"

DEV_ROOT="$HOME/dev"
EXPORT_DIRS=(personal priceless cc central-planning)
TAILSCALE_CIDR="100.64.0.0/10"
# Three fixed ports — rpcbind (111) and mountd (20048) are stable across
# restarts on this system (confirmed via `rpcinfo -p localhost`), nfsd
# (2049) always is. lockd/statd are deliberately NOT opened: the client
# mounts with `locallocks`, so they're never reached over the network.
NFS_PORTS=(111/tcp 111/udp 20048/tcp 20048/udp 2049/tcp)
EXPORT_OPTS="rw,sync,no_subtree_check,all_squash,anonuid=1000,anongid=1000"

# ---------------------------------------------------------------------------
# Preflight
# ---------------------------------------------------------------------------

require_sudo() {
    if [[ $EUID -ne 0 ]] && ! sudo -n true 2>/dev/null; then
        info "NFS export setup needs sudo — may prompt once:"
        sudo -v || { error "sudo unavailable, aborting"; exit 1; }
    fi
}

assert_arch() {
    [[ -f /etc/arch-release ]] || { warning "Not Arch Linux — skipping"; exit 0; }
}

# ---------------------------------------------------------------------------
# write_if_changed <path> <content>
#   Same content-compare-before-write idiom as scripts/homelab/harden.sh.
# ---------------------------------------------------------------------------

write_if_changed() {
    local path="$1"
    local content="$2"
    local perm="${PERM:-0644}"

    if [[ -f "$path" ]] && [[ "$(sudo cat "$path" 2>/dev/null)" == "$content" ]]; then
        return 0  # unchanged
    fi

    local dir
    dir=$(dirname "$path")
    sudo install -d -m 0755 "$dir"
    printf '%s\n' "$content" | sudo tee "$path" >/dev/null
    sudo chmod "$perm" "$path"
    sudo chown root:root "$path"
    success "wrote $path"
    return 10  # sentinel: file changed (callers chain reload commands)
}

# ---------------------------------------------------------------------------
# Step 1: nfs-utils
# ---------------------------------------------------------------------------

install_nfs_utils() {
    if pacman -Qi nfs-utils &>/dev/null; then
        success "nfs-utils already installed"
        return 0
    fi
    info "installing nfs-utils"
    sudo pacman -S --noconfirm --needed nfs-utils \
        || { error "failed to install nfs-utils"; exit 1; }
}

# ---------------------------------------------------------------------------
# Step 2: /etc/exports (one stanza per exported dir, Tailscale CIDR-scoped)
# ---------------------------------------------------------------------------

configure_exports() {
    local lines=(
        "# Managed by scripts/homelab/nfs-export.sh — do not hand-edit."
        "# NFS export of ~/dev subtrees, Tailscale mesh clients only."
    )
    local d
    for d in "${EXPORT_DIRS[@]}"; do
        lines+=("$DEV_ROOT/$d $TAILSCALE_CIDR($EXPORT_OPTS)")
    done
    local content
    content=$(printf '%s\n' "${lines[@]}")

    write_if_changed /etc/exports "$content"
    if [[ $? -eq 10 ]]; then
        info "reloading exports"
        sudo exportfs -ra || { error "exportfs -ra failed"; exit 1; }
    fi
}

# ---------------------------------------------------------------------------
# Step 3: nfs-server.service
# ---------------------------------------------------------------------------

enable_nfs_server() {
    if ! systemctl is-enabled nfs-server.service &>/dev/null; then
        info "enabling nfs-server.service"
        sudo systemctl enable nfs-server.service \
            || { error "failed to enable nfs-server.service"; exit 1; }
    fi
    if ! systemctl is-active nfs-server.service &>/dev/null; then
        info "starting nfs-server.service"
        sudo systemctl start nfs-server.service \
            || { error "failed to start nfs-server.service"; exit 1; }
    else
        success "nfs-server.service already active"
    fi
}

# ---------------------------------------------------------------------------
# Step 4: ufw rules (rpcbind + mountd + nfsd), scoped to the Tailscale CIDR
# only (never the general LAN). No lockd/statd rules — see the header note
# on `locallocks`.
# ---------------------------------------------------------------------------

configure_ufw() {
    command -v ufw &>/dev/null || { warning "ufw not installed — skipping firewall rules"; return 0; }
    local entry port proto
    for entry in "${NFS_PORTS[@]}"; do
        port="${entry%/*}"
        proto="${entry#*/}"
        if sudo ufw status | grep -qE "^${port}/${proto}[[:space:]]+ALLOW[[:space:]]+${TAILSCALE_CIDR}([[:space:]]|\$)"; then
            success "ufw rule for ${port}/${proto} from ${TAILSCALE_CIDR} already present"
            continue
        fi
        info "adding ufw rule: allow ${port}/${proto} from ${TAILSCALE_CIDR}"
        sudo ufw allow from "$TAILSCALE_CIDR" to any port "$port" proto "$proto" comment 'dev-nfs-export (Tailscale-only)' \
            || { error "ufw rule add failed for ${port}/${proto}"; exit 1; }
    done
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

main() {
    info "========================================"
    info "  NFS export of ~/dev (Tailscale-only)"
    info "  Host: $(hostname)"
    info "========================================"

    assert_arch
    require_sudo

    install_nfs_utils
    configure_exports
    enable_nfs_server
    configure_ufw

    success "NFS export configured — verify with: sudo exportfs -v"
}

main "$@"
