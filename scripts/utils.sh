#!/bin/bash

# tput exits 2 when $TERM is unset (non-interactive chezmoi/mesh deploy over
# SSH). Under a sourcing script that has `set -e` (e.g. install-arch.sh), that
# nonzero status aborts the whole apply before any work runs. Swallow the
# failure and fall back to empty (no-color) so these assignments are always
# exit-0 regardless of TERM. 2>/dev/null also keeps the deploy log clean.
default_color=$(tput sgr 0 2>/dev/null || true)
red="$(tput setaf 1 2>/dev/null || true)"
yellow="$(tput setaf 3 2>/dev/null || true)"
green="$(tput setaf 2 2>/dev/null || true)"
blue="$(tput setaf 4 2>/dev/null || true)"

info() {
    printf "%s==> %s%s\n" "$blue" "$1" "$default_color"
}

success() {
    printf "%s==> %s%s\n" "$green" "$1" "$default_color"
}

error() {
    printf "%s==> %s%s\n" "$red" "$1" "$default_color"
}

warning() {
    printf "%s==> %s%s\n" "$yellow" "$1" "$default_color"
}

# --- sudo keep-alive --------------------------------------------------------
# Pre-authorize sudo ONCE and refresh the timestamp in the background so a run
# of privileged operations (pkg-based brew casks like windows-app /
# microsoft-teams / microsoft-outlook, the ProxyBridge installer, rosetta) does
# not re-prompt for the password each time. No-ops cleanly when sudo is absent
# or stdin is not a TTY (non-interactive chezmoi apply / mesh deploy).
SUDO_KEEPALIVE_PID=""

sudo_keepalive_start() {
    [[ -n "$SUDO_KEEPALIVE_PID" ]] && return 0          # already running
    [[ -t 0 ]] || { warning "Non-interactive shell — skipping sudo keep-alive."; return 0; }
    command -v sudo >/dev/null 2>&1 || return 0
    if ! sudo -v 2>/dev/null; then
        warning "sudo authentication failed — privileged installs may re-prompt."
        return 0
    fi
    # Refresh the timestamp every 50s until the parent script exits ($$ is the
    # sourcing script's PID even inside this subshell).
    ( while true; do sudo -n true 2>/dev/null; sleep 50; kill -0 "$$" 2>/dev/null || exit 0; done ) &
    SUDO_KEEPALIVE_PID=$!
    trap 'sudo_keepalive_stop' EXIT
    success "sudo authorized once; session kept warm for privileged installs."
}

sudo_keepalive_stop() {
    [[ -n "$SUDO_KEEPALIVE_PID" ]] || return 0
    kill "$SUDO_KEEPALIVE_PID" 2>/dev/null || true
    SUDO_KEEPALIVE_PID=""
}

# --- mx-broker socket dir --------------------------------------------------
# Single source of truth for the ~/.mx/broker 0700 invariant (was duplicated
# across run_once_install-packages, run_onchange_after_configure-git-azure,
# and run_after_doctor). The ssh -L tunnel LaunchAgent needs this dir to
# exist BEFORE it binds (ExitOnForwardFailure makes a missing dir a hard
# bind failure), so every deploy path that might run first calls this.
ensure_mx_broker_dir() {
    mkdir -p "$HOME/.mx/broker" 2>/dev/null && chmod 700 "$HOME/.mx/broker" 2>/dev/null || true
}