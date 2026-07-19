#!/usr/bin/env bash
# dbpro.sh - Install DB Pro database client
# https://www.dbpro.app/
# Manual optional tool — run directly: bash scripts/dbpro.sh (macOS only)

set -uo pipefail

. "$DOTFILES/scripts/utils.sh"

DBPRO_VERSION="1.6.1"
DOWNLOAD_DIR="/tmp"
APP_NAME="DB Pro.app"

install_dbpro() {
    info "Installing DB Pro..."

    # Check if already installed
    if [[ -d "/Applications/$APP_NAME" ]]; then
        warning "DB Pro is already installed in /Applications"
        read -p "Reinstall? [y/n] " -n 1 -r
        echo
        if [[ ! $REPLY =~ ^[Yy]$ ]]; then
            return 0
        fi
    fi

    # Detect architecture
    local arch
    arch=$(uname -m)
    local download_url

    case "$arch" in
        arm64)
            download_url="https://releases.dbpro.app/macos-arm64/DB%20Pro-${DBPRO_VERSION}-arm64.dmg"
            info "Detected Apple Silicon (arm64)"
            ;;
        x86_64)
            download_url="https://releases.dbpro.app/macos-x64/DB%20Pro-${DBPRO_VERSION}-x64.dmg"
            info "Detected Intel (x64)"
            ;;
        *)
            error "Unsupported architecture: $arch"
            return 1
            ;;
    esac

    local dmg_path="$DOWNLOAD_DIR/DBPro-${DBPRO_VERSION}.dmg"

    # Download
    info "Downloading DB Pro ${DBPRO_VERSION}..."
    curl -L -o "$dmg_path" "$download_url"

    # Mount DMG
    info "Mounting disk image..."
    local mount_point
    mount_point=$(hdiutil attach "$dmg_path" -nobrowse -quiet | tail -1 | awk '{print $3}')

    # Find the .app in the mounted volume
    local app_path
    app_path=$(find "$mount_point" -maxdepth 1 -name "*.app" | head -1)

    if [[ -z "$app_path" ]]; then
        error "Could not find .app in mounted DMG"
        hdiutil detach "$mount_point" -quiet 2>/dev/null || true
        return 1
    fi

    # Copy to Applications
    info "Installing to /Applications..."
    rm -rf "/Applications/$APP_NAME" 2>/dev/null || true
    cp -R "$app_path" "/Applications/"

    # Unmount and cleanup
    info "Cleaning up..."
    hdiutil detach "$mount_point" -quiet 2>/dev/null || true
    rm -f "$dmg_path"

    success "DB Pro installed to /Applications"
}

# Run if executed directly
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
    install_dbpro
fi
