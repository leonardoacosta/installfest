#!/bin/bash
(return 0 2>/dev/null) || set -uo pipefail  # sourced-lib guard — bare set would leak into callers

# Get the absolute path of the directory where the script is located
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

. $SCRIPT_DIR/utils.sh


run_brew_bundle() {
    printf "\n"
    info "===================="
    info "Apps"
    info "===================="
    
    # Brewfile lives under platform/homebrew/ (moved from homebrew/ during
    # platform-tooling reorg). Keep the legacy path as a fallback so older
    # clones or sibling repos that haven't pulled keep working.
    brewfile="$SCRIPT_DIR/../platform/homebrew/Brewfile"
    if [ ! -f "$brewfile" ]; then
        brewfile="$SCRIPT_DIR/../homebrew/Brewfile"
    fi
    if [ -f "$brewfile" ]; then
        # Trust every third-party tap the Brewfile declares BEFORE any bundle
        # operation. Homebrew refuses to load formulae from untrusted taps, and
        # that trust is per-machine state wiped by a restore — so a fresh machine
        # (or a newly-added tap) aborts `brew bundle`/`check` until trusted.
        # Idempotent: `brew trust` no-ops on an already-trusted tap.
        grep -E '^tap "' "$brewfile" | sed -E 's/^tap "([^"]+)".*/\1/' | while read -r _tap; do
            brew trust "$_tap" >/dev/null 2>&1 || true
        done

        # Run `brew bundle check`
        local check_output
        check_output=$(brew bundle check --file="$brewfile" 2>&1)

        # Check if "The Brewfile's dependencies are satisfied." is contained in the output
        if echo "$check_output" | grep -q "The Brewfile's dependencies are satisfied."; then
            warning "The Brewfile's dependencies are already satisfied."
        else
            info "Satisfying missing dependencies with 'brew bundle install'..."
            brew bundle install --file="$brewfile"
        fi
    else
        error "Brewfile not found"
        return 1
    fi

    install_azure_devops_extension
}

install_azure_devops_extension() {
    if ! command -v az &>/dev/null; then
        warning "Azure CLI not found, skipping DevOps extension"
        return
    fi

    if az extension show --name azure-devops &>/dev/null; then
        success "Azure DevOps extension already installed"
    else
        info "Adding Azure DevOps extension..."
        az extension add --name azure-devops
        success "Azure DevOps extension installed"
    fi
}

if [ "$(basename "$0")" = "$(basename "${BASH_SOURCE[0]}")" ]; then
    # Check if Homebrew is installed
    if ! command -v brew &>/dev/null; then
        error "Homebrew is not installed. Please install Homebrew first."
        exit 1
    fi

    read -p "Install Brew bundle? [y/n] " install_bundle

    if [[ "$install_bundle" == "y" ]]; then
        run_brew_bundle
    fi
fi