#!/usr/bin/env bash
# scripts/mac-sudo-timeout.sh - extend sudo's credential cache on the Mac (idempotent).
#
# Default sudo behavior re-prompts every 5 minutes AND scopes the cached
# credential per-tty (tty_tickets) — so every new terminal window/tmux pane
# re-prompts even within that window. This installs a sudoers.d drop-in that:
#   - timestamp_timeout=1440 (24h): at most one prompt/day under normal use
#   - timestamp_type=global: one shared timestamp per user, not per-tty —
#     covers every terminal/tmux pane, not just the one that authenticated
#
# SAFETY: a malformed /etc/sudoers(.d/*) file can lock out sudo entirely.
# This script NEVER touches /etc/sudoers directly and NEVER installs the
# drop-in without first validating it via `visudo -c` against a staged temp
# file — a failed validation aborts with the file left untouched.
#
# Re-runs safely: content-compared before write, same idiom as
# scripts/homelab/nfs-export.sh / scripts/mac-autofs-dev.sh.

set -uo pipefail

DOTFILES="${DOTFILES:-$HOME/dev/personal/installfest}"

# shellcheck source=utils.sh
. "$DOTFILES/scripts/utils.sh"

DROPIN_FILE="/etc/sudoers.d/local-timestamp"
DROPIN_CONTENT='# Managed by scripts/mac-sudo-timeout.sh — do not hand-edit.
# timestamp_type=global shares one auth timestamp across all terminals/tmux
# panes for this user (not per-tty); timestamp_timeout=1440 (24h) means at
# most one sudo prompt/day under normal use.
Defaults timestamp_timeout=1440
Defaults timestamp_type=global'

assert_macos() {
    [[ "$(uname -s)" == "Darwin" ]] || { warning "Not macOS — skipping"; exit 0; }
}

require_sudo() {
    if [[ $EUID -ne 0 ]] && ! sudo -n true 2>/dev/null; then
        info "sudo timeout setup needs sudo — may prompt once:"
        sudo -v || { error "sudo unavailable, aborting"; exit 1; }
    fi
}

main() {
    info "========================================"
    info "  sudo credential-cache timeout (24h, global)"
    info "========================================"

    assert_macos
    require_sudo

    if [[ -f "$DROPIN_FILE" ]] && [[ "$(sudo cat "$DROPIN_FILE" 2>/dev/null)" == "$DROPIN_CONTENT" ]]; then
        success "$DROPIN_FILE already up to date"
        exit 0
    fi

    local tmp
    tmp=$(mktemp /tmp/sudoers-local-timestamp.XXXXXX)
    printf '%s\n' "$DROPIN_CONTENT" > "$tmp"

    if ! sudo visudo -c -f "$tmp" >/dev/null 2>&1; then
        error "visudo validation FAILED — not installing. Staged file left at $tmp for inspection."
        exit 1
    fi

    sudo install -o root -g wheel -m 0440 "$tmp" "$DROPIN_FILE" \
        || { error "failed to install $DROPIN_FILE"; rm -f "$tmp"; exit 1; }
    rm -f "$tmp"

    # Re-validate the installed file in place — belt-and-suspenders, since a
    # permissions/ownership mismatch (not content) is the other way a
    # sudoers.d file can silently fail to take effect.
    if ! sudo visudo -c >/dev/null 2>&1; then
        error "post-install visudo -c FAILED — removing $DROPIN_FILE to fail safe."
        sudo rm -f "$DROPIN_FILE"
        exit 1
    fi

    success "installed $DROPIN_FILE (validated) — sudo now caches for 24h, shared across terminals"
}

main "$@"
