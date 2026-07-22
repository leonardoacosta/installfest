#!/usr/bin/env bash
# scripts/homelab/nfs-export.sh - NFSv3 export of a curated ~/dev root, Tailscale-only (idempotent).
#
# Backing script for the "mount ~/dev as a network drive" feature. Exports
# ONE directory tree so the Mac can mount it as ONE /Volumes/dev that
# mirrors ~/dev's structure (personal/, priceless/, cc/, central-planning/,
# brown/ — see scripts/mac-autofs-dev.sh for the client side).
#
# Deliberately NOT exported: archive/ (dead/stale clones).
#
# CORRECTED 2026-07-22 (two changes, same day):
#
# 1. NFSv4 -> NFSv3+locallocks. This was originally NFSv4-only (single
#    port, 2049). Live-verified against the real Mac client: v4 mounts fail
#    with "RPC prog. not avail" from a kernel-level lockd/nfsdctl issue on
#    this host's kernel (7.1.3-arch2-2) — `nfsdctl: lockd configuration
#    failure` in `journalctl -u nfs-server` the moment lockd's port is
#    pinned, and the in-kernel nlockmgr registration got stuck even after a
#    clean service restart. NFSv3 mounted immediately with the same
#    exports. Getting v3 locking working still only needs 3 FIXED ports
#    (rpcbind 111, mountd 20048 — both stable across restarts, confirmed
#    via `rpcinfo -p`), not the dynamic lockd/statd ports one might expect
#    — the client mounts with `locallocks` (see scripts/mac-autofs-dev.sh),
#    keeping file locking entirely client-side.
#
# 2. 4 separate exports -> 1 curated export root. Originally each top-level
#    dir (personal, priceless, cc, central-planning) got its own /etc/exports
#    stanza, mounted on the Mac as 4 SEPARATE /Volumes/dev-* volumes. Leo
#    wanted ONE /Volumes/dev that mirrors ~/dev itself, not 4 flat sibling
#    mounts — and separately wanted brown/ (Brown & Wilson client work)
#    included this time, only archive/ excluded. NFS exports a single
#    directory-subtree with no way to exclude a child path, so exporting the
#    real ~/dev directly can't honor the archive/ exclusion. The fix:
#    scripts/homelab/nfs-export-bindmounts.sh builds EXPORT_ROOT
#    (/srv/nfs-dev-export) as bind-mounts of exactly the wanted subtrees —
#    a separate curated tree that mirrors ~/dev's shape without ~/dev
#    itself ever being exported. dev-export-bindmounts.service runs that
#    script at boot (RemainAfterExit=yes), and nfs-server.service depends on
#    it via a drop-in override so the bind mounts are always up before nfsd
#    starts serving — including after a reboot, not just right now.
#    `crossmnt` on the export lets the client traverse from EXPORT_ROOT into
#    each bind-mounted subtree (they're separate mount points even though
#    bind mounts of the same underlying filesystem).
#
# Client scope: ufw (Step 5) opens the NFS ports to the whole Tailscale CGNAT
# range (100.64.0.0/10), never the general LAN. But /etc/exports itself (Step
# 3) is scoped tighter than that — rw only to specific named mesh peer IPs
# (see NFS_PEERS below), not the whole /10 — since NFSv3 has no user auth and
# any tailnet device reaching the open ports could otherwise mount rw.
#
# all_squash,anonuid=1000,anongid=1000 maps every remote client to nyaptor's
# own uid/gid, sidestepping the Mac's leonardoacosta vs homelab's nyaptor UID
# mismatch without idmapd configuration.
#
# CORRECTED 2026-07-22 (scope-homelab-nfs-export): /etc/exports was rw to the
# entire 100.64.0.0/10 CGNAT range — any tailnet device (or a single
# compromised one) could mount and write into these repos, which later
# execute locally (hooks, deploy scripts, cron/systemd units) — a
# supply-chain path. Operator decision (Option 2, decisions.jsonl): the Mac
# genuinely writes over this mount (edit-on-Mac-save), so `ro` wasn't viable
# — scoped rw to NFS_PEERS' named IPs instead of the whole CIDR. NOTE: `ssh-
# mesh/README.md`'s documented Mac IP (100.91.88.16) was confirmed STALE
# against live `tailscale status` (actual: 100.82.80.88, hostname
# "macbook") — always re-verify via `tailscale status` before editing
# NFS_PEERS, never trust the doc alone.
#
# Re-runs safely: /etc/exports and the systemd units are content-compared
# before writing (exportfs/daemon-reload only run on change), nfs-server
# .service is guarded by systemctl is-active/is-enabled, and each ufw rule
# is checked before adding — same conventions as scripts/homelab/harden.sh's
# write_if_changed / service-guard idiom.

set -uo pipefail

DOTFILES="${DOTFILES:-$HOME/dev/personal/installfest}"

# shellcheck source=../utils.sh
. "$DOTFILES/scripts/utils.sh"

EXPORT_ROOT="/srv/nfs-dev-export"
TAILSCALE_CIDR="100.64.0.0/10"
# Known mesh peers granted rw on the export — "ip|comment" pairs, one per
# machine that actually mounts EXPORT_ROOT. Resolve/verify via `tailscale
# status` (NOT ssh-mesh/README.md alone — its Mac IP was found stale
# 2026-07-22). homelab itself is excluded (it's the server, not an NFS
# client of its own export); cpc (CloudPC/Windows bastion) is excluded —
# no NFS client wiring for it exists (see scripts/mac-autofs-dev.sh, the
# only client-side script, macOS-only).
NFS_PEERS=(
    "100.82.80.88|mac (leo's MacBook — scripts/mac-autofs-dev.sh client)"
)
# Three fixed ports — rpcbind (111) and mountd (20048) are stable across
# restarts on this system (confirmed via `rpcinfo -p localhost`), nfsd
# (2049) always is. lockd/statd are deliberately NOT opened: the client
# mounts with `locallocks`, so they're never reached over the network.
NFS_PORTS=(111/tcp 111/udp 20048/tcp 20048/udp 2049/tcp)
# crossmnt: let the client traverse from EXPORT_ROOT into each bind-mounted
# subtree — without it, NFS stops at the first mount-point boundary and the
# client would only ever see an empty EXPORT_ROOT.
EXPORT_OPTS="rw,sync,no_subtree_check,all_squash,anonuid=1000,anongid=1000,crossmnt"
BINDMOUNT_SCRIPT="$DOTFILES/scripts/homelab/nfs-export-bindmounts.sh"
BINDMOUNT_SERVICE_FILE="/etc/systemd/system/dev-export-bindmounts.service"
NFS_SERVER_DROPIN="/etc/systemd/system/nfs-server.service.d/10-dev-export-bindmounts.conf"

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
# Step 2: dev-export-bindmounts.service — builds EXPORT_ROOT as bind-mounts
# of the curated subtree list (see nfs-export-bindmounts.sh), and wires
# nfs-server.service to depend on it via a drop-in override so this survives
# reboots, not just the current session.
# ---------------------------------------------------------------------------

configure_bindmount_service() {
    if [[ ! -x "$BINDMOUNT_SCRIPT" ]]; then
        if [[ -f "$BINDMOUNT_SCRIPT" ]]; then
            chmod +x "$BINDMOUNT_SCRIPT"
        else
            error "$BINDMOUNT_SCRIPT not found"
            exit 1
        fi
    fi

    local unit_content
    unit_content=$(cat <<EOF
[Unit]
Description=Bind-mount curated ~/dev subtrees for NFS export
Before=nfs-server.service

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=$BINDMOUNT_SCRIPT

[Install]
WantedBy=multi-user.target
EOF
)

    local dropin_content
    dropin_content=$(cat <<EOF
[Unit]
Requires=dev-export-bindmounts.service
After=dev-export-bindmounts.service
EOF
)

    local changed=0
    write_if_changed "$BINDMOUNT_SERVICE_FILE" "$unit_content"
    [[ $? -eq 10 ]] && changed=1
    write_if_changed "$NFS_SERVER_DROPIN" "$dropin_content"
    [[ $? -eq 10 ]] && changed=1

    if [[ $changed -eq 1 ]]; then
        info "reloading systemd units"
        sudo systemctl daemon-reload || { error "daemon-reload failed"; exit 1; }
    fi

    if ! systemctl is-enabled dev-export-bindmounts.service &>/dev/null; then
        info "enabling dev-export-bindmounts.service"
        sudo systemctl enable dev-export-bindmounts.service \
            || { error "failed to enable dev-export-bindmounts.service"; exit 1; }
    fi
    if ! systemctl is-active dev-export-bindmounts.service &>/dev/null; then
        info "starting dev-export-bindmounts.service"
        sudo systemctl start dev-export-bindmounts.service \
            || { error "failed to start dev-export-bindmounts.service"; exit 1; }
    else
        success "dev-export-bindmounts.service already active"
    fi
}

# ---------------------------------------------------------------------------
# Step 3: /etc/exports (one export line per known mesh peer in NFS_PEERS,
# rw scoped to those specific IPs — never the whole Tailscale CIDR)
# ---------------------------------------------------------------------------

configure_exports() {
    local lines peer ip comment
    if [[ ${#NFS_PEERS[@]} -eq 0 ]]; then
        error "NFS_PEERS is empty — refusing to write /etc/exports (would silently unexport EXPORT_ROOT for everyone). Add at least one peer or fix the array."
        exit 1
    fi
    lines=(
        "# Managed by scripts/homelab/nfs-export.sh — do not hand-edit."
        "# NFS export of a curated ~/dev root, rw scoped to named mesh peers only (see NFS_PEERS)."
    )
    for peer in "${NFS_PEERS[@]}"; do
        ip="${peer%%|*}"
        comment="${peer#*|}"
        lines+=("# $comment")
        lines+=("$EXPORT_ROOT $ip($EXPORT_OPTS)")
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
# Step 4: nfs-server.service
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
# Step 5: ufw rules (rpcbind + mountd + nfsd), scoped to the Tailscale CIDR
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
    info "  NFS export of a curated ~/dev root (Tailscale-only)"
    info "  Host: $(hostname)"
    info "========================================"

    assert_arch
    require_sudo

    install_nfs_utils
    configure_bindmount_service
    configure_exports
    enable_nfs_server
    configure_ufw

    success "NFS export configured — verify with: sudo exportfs -v"
}

main "$@"
