#!/usr/bin/env bash
# install-arch.sh - Arch Linux specific installation
# Sourced by home/run_once_install-packages.sh.tmpl on Linux (chezmoi run_once)
#
# ~/.env keys consumed by downstream tooling (harden.sh, services, drizzle):
#   POSTGRES_URL=             # Postgres URL for nexus drizzle migrations (homelab)
#   POSTGRES_PASSWORD=        # Shared postgres password (homelab cortex role)
#   CX_POSTGRES_PASSWORD=     # Override for shared cortex role (optional)
#   IMMICH_DB_PASSWORD=       # Immich DB role password (homelab)
#   VECTOR_BS_TOKEN=          # Better Stack ingest token for Vector → telemetry pipeline (homelab)
#   TAILSCALE_AUTHKEY=        # Optional non-interactive tailnet auth (any host)
#
# ~/.env is gitignored and machine-local. Populate manually after first install;
# harden.sh and systemd units read it via _hl_env_get / EnvironmentFile.

(return 0 2>/dev/null) || set -uo pipefail  # sourced-lib guard — bare set would leak into callers

. "$DOTFILES/scripts/utils.sh"

install_arch_packages() {
    info "Installing Arch packages..."

    # Core shell tools
    local packages=(
        zsh
        zsh-syntax-highlighting
        zsh-autosuggestions
        starship
        fzf
        zoxide
        atuin
        bat
        glow
        eza
        ripgrep
        fd
        shellcheck        # static analysis for shell scripts (scripts/check.sh)
        git
        tmux
        zellij            # terminal multiplexer — ws-claude hard-depends on it (packages/workspace/bin/ws-claude)
        neovim
        # Container tools
        docker
        docker-buildx
        docker-compose
        # Git tools
        github-cli
        lazygit
        # Languages & runtimes (node/pnpm managed by mise)
        go
        rust
        # .NET — SDKs install side-by-side; pin per-project via global.json.
        # Each dotnet-sdk-X pulls its matching dotnet-runtime-X automatically.
        dotnet-sdk        # latest (currently 10)
        dotnet-sdk-9.0
        aspnet-runtime    # ASP.NET Core 10 (Web API templates)
        aspnet-runtime-9.0
        # Build tools (for compiling C tools like youtube_transcript)
        curl
        base-devel
        # Azure DevOps CLI
        python-pipx
        python-packaging
        # Dotfiles management
        chezmoi
        # Networking
        proxychains-ng
        mosh              # roaming/latency-tolerant SSH replacement (UDP 60000-61000)
        tigervnc          # VNC client (vncviewer) for scripts/vnc-mac.sh — Mac Screen Sharing over Tailscale
        # Nerd Fonts (required for starship icons)
        ttf-jetbrains-mono-nerd
        ttf-cascadia-mono-nerd
    )

    # Check which packages need to be installed
    local to_install=()
    for pkg in "${packages[@]}"; do
        if ! pacman -Qi "$pkg" &>/dev/null; then
            to_install+=("$pkg")
        fi
    done

    if [[ ${#to_install[@]} -gt 0 ]]; then
        info "Installing: ${to_install[*]}"
        sudo pacman -S --noconfirm "${to_install[@]}"
    else
        success "All packages already installed"
    fi
}

install_aur_packages() {
    info "Checking AUR packages..."

    # AUR packages (require yay or paru)
    local aur_packages=(
        mise
        git-credential-manager  # Cross-platform Git credential storage
        bun-bin                 # Fast JS runtime
        # direnv is in official repos
    )

    if command -v yay &>/dev/null; then
        for pkg in "${aur_packages[@]}"; do
            if ! pacman -Qi "$pkg" &>/dev/null; then
                info "Installing $pkg from AUR..."
                yay -S --noconfirm "$pkg"
            fi
        done
    elif command -v paru &>/dev/null; then
        for pkg in "${aur_packages[@]}"; do
            if ! pacman -Qi "$pkg" &>/dev/null; then
                info "Installing $pkg from AUR..."
                paru -S --noconfirm "$pkg"
            fi
        done
    else
        warning "yay or paru not found, skipping AUR packages"
        warning "Install yay: https://github.com/Jguer/yay"
    fi
}

install_azure_cli() {
    info "Checking Azure CLI..."

    if command -v az &>/dev/null; then
        success "Azure CLI already installed"
    else
        info "Installing Azure CLI via pipx..."
        pipx install azure-cli
    fi

    if command -v az &>/dev/null; then
        if az extension show --name azure-devops &>/dev/null; then
            success "Azure DevOps extension already installed"
        else
            info "Adding Azure DevOps extension..."
            az extension add --name azure-devops
            success "Azure DevOps extension installed"
        fi
    else
        warning "Azure CLI not available, skipping DevOps extension"
    fi
}

set_default_shell() {
    local zsh_path
    zsh_path="$(command -v zsh)" || { warning "zsh not installed"; return 0; }
    local current_shell
    current_shell="$(getent passwd "$USER" | cut -d: -f7)"
    if [[ "$current_shell" == "$zsh_path" ]]; then
        success "zsh is already the default shell"
        return 0
    fi
    info "Setting zsh as default shell (current: $current_shell)..."
    if sudo -n usermod -s "$zsh_path" "$USER" 2>/dev/null; then
        success "Default shell changed to zsh (effective next login)"
    else
        # Fallback to chsh if sudo unavailable (may prompt or fail in non-interactive contexts)
        if chsh -s "$zsh_path" 2>/dev/null; then
            success "Default shell changed to zsh via chsh"
        else
            warning "Could not change default shell (need passwordless sudo or interactive auth)"
            return 0  # Non-fatal — don't abort the script
        fi
    fi
}

# Main installation flow
info "=== Arch Linux Installation ==="

# Non-interactive when sourced by chezmoi run_once
# Interactive prompts only when run directly (./install-arch.sh)
if [[ "${CHEZMOI:-0}" == "1" ]] || [[ ! -t 0 ]]; then
    # Non-interactive: run everything
    install_arch_packages
    install_aur_packages
    install_azure_cli
    set_default_shell
else
    # Interactive: ask first
    read -p "Install packages? [y/n] " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        install_arch_packages
        install_aur_packages
        install_azure_cli
    fi

    read -p "Set zsh as default shell? [y/n] " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        set_default_shell
    fi
fi

success "Arch Linux setup complete"
