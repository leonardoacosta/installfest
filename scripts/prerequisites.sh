#!/bin/bash

# Get the absolute path of the directory where the script is located
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

. $SCRIPT_DIR/utils.sh

install_xcode() {
    info "Installing Apple's CLI tools (prerequisites for Git and Homebrew)..."
    if xcode-select -p >/dev/null 2>&1; then
        warning "Xcode Command Line Tools already installed"
    else
        # Headless CLT install. `xcode-select --install` only opens a GUI dialog
        # and can't complete unattended, so drive softwareupdate directly: the
        # placeholder file makes softwareupdate list the CLT package, which we
        # then install without any prompt.
        info "Installing Xcode Command Line Tools (headless via softwareupdate)..."
        local placeholder="/tmp/.com.apple.dt.CommandLineTools.installondemand.in-progress"
        touch "$placeholder"
        local prod
        prod=$(softwareupdate -l 2>/dev/null \
            | grep -oE 'Command Line Tools for Xcode-[0-9.]+' \
            | sort -V | tail -1)
        if [ -n "$prod" ]; then
            softwareupdate -i "$prod" --verbose || warning "CLT softwareupdate install failed"
        else
            warning "CLT label not found via softwareupdate; falling back to GUI installer"
            xcode-select --install 2>/dev/null || true
        fi
        rm -f "$placeholder"
    fi
    # Accept the license non-interactively only when full Xcode.app is present
    # (CLT-only installs accept implicitly; `xcodebuild -license` errors without Xcode.app).
    if [ -d /Applications/Xcode.app ]; then
        sudo xcodebuild -license accept 2>/dev/null || warning "xcodebuild license accept failed"
    fi
}

install_rosetta() {
    # Apple Silicon only. x86-only packages (e.g. ProxyBridge) refuse to install
    # without Rosetta 2, and a fresh macOS restore does NOT ship it. oahd is the
    # Rosetta daemon — its presence means Rosetta is already installed.
    if [ "$(uname -m)" != "arm64" ]; then
        return 0
    fi
    if /usr/bin/pgrep -q oahd; then
        warning "Rosetta 2 already installed"
        return 0
    fi
    info "Installing Rosetta 2 (required for x86 packages on Apple Silicon)..."
    softwareupdate --install-rosetta --agree-to-license
}

install_homebrew() {
    info "Installing Homebrew..."
    export HOMEBREW_CASK_OPTS="--appdir=/Applications"
    if hash brew &>/dev/null; then
        warning "Homebrew already installed"
    else
        sudo --validate
        NONINTERACTIVE=1 /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/master/install.sh)"
    fi
}

setup_touchid_sudo() {
    # Enable TouchID for sudo via /etc/pam.d/sudo_local. This file is the
    # Apple-sanctioned drop-in (sudo_local.template ships on Sonoma+) and
    # SURVIVES macOS updates, unlike editing /etc/pam.d/sudo directly.
    #
    # pam_reattach MUST come first: without it, pam_tid cannot show the TouchID
    # prompt inside tmux/screen (the sudo process is detached from the GUI
    # session). It is marked 'optional' so a missing module degrades to a
    # password prompt instead of locking sudo out.
    local f="/etc/pam.d/sudo_local"
    local reattach="${HOMEBREW_PREFIX:-/opt/homebrew}/lib/pam/pam_reattach.so"

    if [ "$(uname -s)" != "Darwin" ]; then
        return 0
    fi
    if [ -f "$f" ] && grep -q "pam_tid.so" "$f"; then
        success "TouchID for sudo already configured."
        return 0
    fi

    local content="# Managed by dotfiles (run_once_install-packages.sh) — TouchID for sudo.\n"
    if [ -f "$reattach" ]; then
        content="${content}auth       optional       ${reattach}\n"
    else
        warning "pam-reattach not found at $reattach — TouchID may not prompt inside tmux."
    fi
    content="${content}auth       sufficient     pam_tid.so\n"

    if printf "%b" "$content" | sudo tee "$f" >/dev/null 2>&1; then
        success "TouchID for sudo enabled ($f). Open a new sudo session to use it."
    else
        warning "Could not write $f — TouchID for sudo not enabled."
    fi
}

if [ "$(basename "$0")" = "$(basename "${BASH_SOURCE[0]}")" ]; then
    install_xcode
    install_homebrew
fi