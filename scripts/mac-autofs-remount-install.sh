#!/usr/bin/env bash
# scripts/mac-autofs-remount-install.sh - install the boot-time autofs-remount
# LaunchDaemon (idempotent).
#
# Writes /Library/LaunchDaemons/dev.leonardoacosta.autofs-remount.plist and
# loads it into the system domain. The daemon runs scripts/mac-autofs-remount.sh
# as root at every boot (RunAtLoad) to re-register the /Volumes/dev autofs map
# once Tailscale is up — see that script's header for the full rationale.
#
# Why a SYSTEM LaunchDaemon and not a user LaunchAgent: `automount -vc` needs
# root. A LaunchAgent runs as the user and would need non-interactive sudo at
# boot (fragile on this Mac); a system daemon runs as root directly.
#
# Why generate the plist here instead of shipping a static file: the daemon's
# Program is an absolute path into this repo ($DOTFILES), so the plist is
# rendered with the live repo location rather than a hardcoded home path — same
# content-compare-before-write idiom as scripts/homelab/harden.sh.
#
# Root-owned config chezmoi itself cannot own; invoked via the mac-gated
# wrapper home/run_onchange_mac-autofs-remount.sh.tmpl. Safe to run ad-hoc.

set -uo pipefail

DOTFILES="${DOTFILES:-$HOME/dev/personal/installfest}"

# shellcheck source=utils.sh
. "$DOTFILES/scripts/utils.sh"

LABEL="dev.leonardoacosta.autofs-remount"
PLIST="/Library/LaunchDaemons/${LABEL}.plist"
SCRIPT="$DOTFILES/scripts/mac-autofs-remount.sh"
LOG="/var/log/autofs-remount.log"

# ---------------------------------------------------------------------------
# Preflight
# ---------------------------------------------------------------------------

assert_macos() {
    [[ "$(uname -s)" == "Darwin" ]] || { warning "Not macOS — skipping"; exit 0; }
}

require_sudo() {
    if [[ $EUID -ne 0 ]] && ! sudo -n true 2>/dev/null; then
        info "autofs-remount daemon install needs sudo — may prompt once:"
        sudo -v || { error "sudo unavailable, aborting"; exit 1; }
    fi
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

main() {
    info "installing ${LABEL} LaunchDaemon"
    assert_macos
    require_sudo

    if [[ ! -f "$SCRIPT" ]]; then
        error "boot script not found: $SCRIPT"
        exit 1
    fi
    chmod +x "$SCRIPT" 2>/dev/null || true

    local plist_content
    plist_content=$(cat <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>${LABEL}</string>

    <!-- Re-registers the /Volumes/dev autofs map once Tailscale is up.
         See ${SCRIPT} for why. -->
    <key>ProgramArguments</key>
    <array>
        <string>/bin/bash</string>
        <string>${SCRIPT}</string>
    </array>

    <!-- Fire once at every boot. NOT KeepAlive: the script polls internally
         for Tailscale reachability and automount's registration persists for
         the rest of the boot session, so one successful run per boot suffices. -->
    <key>RunAtLoad</key>
    <true/>

    <key>StandardOutPath</key>
    <string>${LOG}</string>
    <key>StandardErrorPath</key>
    <string>${LOG}</string>
</dict>
</plist>
EOF
)

    local changed=0
    if [[ -f "$PLIST" ]] && [[ "$(sudo cat "$PLIST" 2>/dev/null)" == "$plist_content" ]]; then
        success "$PLIST already up to date"
    else
        printf '%s\n' "$plist_content" | sudo tee "$PLIST" >/dev/null
        sudo chown root:wheel "$PLIST"
        sudo chmod 0644 "$PLIST"
        changed=1
        success "wrote $PLIST"
    fi

    if ! sudo plutil -lint "$PLIST" >/dev/null; then
        error "plist failed plutil -lint: $PLIST"
        exit 1
    fi

    # (Re)load only when the plist changed or it is not currently loaded — avoid
    # remounting on every unrelated chezmoi apply. bootout-before-bootstrap
    # mirrors home/run_onchange_after_install-user-schedulers.sh.tmpl.
    if [[ $changed -eq 1 ]] || ! sudo launchctl print "system/${LABEL}" >/dev/null 2>&1; then
        sudo launchctl bootout "system/${LABEL}" 2>/dev/null || true
        if sudo launchctl bootstrap system "$PLIST"; then
            success "loaded system/${LABEL} (RunAtLoad fired it once)"
        else
            error "launchctl bootstrap failed for $PLIST"
            exit 1
        fi
    else
        success "system/${LABEL} already loaded — no reload needed"
    fi

    success "autofs-remount daemon installed — /Volumes/dev will self-heal after every reboot"
}

main "$@"
