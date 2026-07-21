#!/usr/bin/env bash
# scripts/homelab/nfs-export.sh - NFSv4 export of ~/dev subtrees, Tailscale-only (idempotent).
#
# Backing script for the "mount ~/dev as a network drive" feature: exposes 4
# top-level ~/dev directories over NFSv4 so the Mac can autofs-mount them (see
# scripts/mac-autofs-dev.sh for the client side):
#
#   personal, priceless, cc, central-planning
#
# Deliberately NOT exported:
#   - archive           (dead/stale clones)
#   - brown             (Brown & Wilson client work — kept off any network
#                         share on principle)
#
# NFSv4-only (no rpcbind/portmapper) means a single port to manage: 2049/tcp.
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
# systemctl is-active/is-enabled, and the ufw rule is checked before adding —
# same conventions as scripts/homelab/harden.sh's write_if_changed /
# service-guard idiom.

set -uo pipefail

DOTFILES="${DOTFILES:-$HOME/dev/personal/installfest}"

# shellcheck source=../utils.sh
. "$DOTFILES/scripts/utils.sh"

DEV_ROOT="$HOME/dev"
EXPORT_DIRS=(personal priceless cc central-planning)
TAILSCALE_CIDR="100.64.0.0/10"
NFS_PORT="2049"
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
        "# NFSv4 export of ~/dev subtrees, Tailscale mesh clients only."
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
# Step 4: ufw rule, scoped to the Tailscale CIDR only (never the general LAN)
# ---------------------------------------------------------------------------

configure_ufw() {
    command -v ufw &>/dev/null || { warning "ufw not installed — skipping firewall rule"; return 0; }
    if sudo ufw status | grep -qE "^${NFS_PORT}/tcp[[:space:]]+ALLOW[[:space:]]+${TAILSCALE_CIDR}([[:space:]]|\$)"; then
        success "ufw rule for NFS (${NFS_PORT}/tcp from ${TAILSCALE_CIDR}) already present"
        return 0
    fi
    info "adding ufw rule: allow ${NFS_PORT}/tcp from ${TAILSCALE_CIDR}"
    sudo ufw allow from "$TAILSCALE_CIDR" to any port "$NFS_PORT" proto tcp comment 'dev-nfs-export (Tailscale-only)' \
        || { error "ufw rule add failed"; exit 1; }
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
