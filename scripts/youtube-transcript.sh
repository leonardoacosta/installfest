#!/usr/bin/env bash
# youtube-transcript.sh - Install youtube_transcript CLI tool
# Fetches YouTube video transcripts without API keys
# https://github.com/Zibri/youtube_transcript
# Manual optional tool — run directly: bash scripts/youtube-transcript.sh

set -uo pipefail

if [[ "${1:-}" == "--help" || "${1:-}" == "-h" ]]; then
	cat <<'EOF'
Usage: bash scripts/youtube-transcript.sh

Clones, builds (gcc/clang + make), and installs the youtube_transcript CLI
(https://github.com/Zibri/youtube_transcript) to ~/.local/bin. Fetches
YouTube video transcripts without an API key. Manual, optional — re-run to
update an existing checkout.
EOF
	exit 0
fi

. "$DOTFILES/scripts/utils.sh"

REPO_URL="https://github.com/Zibri/youtube_transcript.git"
SRC_DIR="$HOME/.local/src/youtube_transcript"
BIN_DIR="$HOME/.local/bin"

install_youtube_transcript() {
    info "Installing youtube_transcript..."

    # Ensure directories exist
    mkdir -p "$HOME/.local/src"
    mkdir -p "$BIN_DIR"

    # Clone/update in subshell to preserve working directory
    (
        # Check if already installed and up to date
        if [[ -d "$SRC_DIR" ]]; then
            info "Updating existing installation..."
            cd "$SRC_DIR"
            git pull --ff-only || {
                warning "Git pull failed, attempting fresh clone..."
                cd ..
                rm -rf "$SRC_DIR"
                git clone "$REPO_URL" "$SRC_DIR"
                cd "$SRC_DIR"
            }
        else
            info "Cloning repository..."
            git clone "$REPO_URL" "$SRC_DIR"
            cd "$SRC_DIR"
        fi

        # Build
        info "Building youtube_transcript..."
        make clean 2>/dev/null || true

        # macOS strip doesn't support -s flag, so build manually
        if [[ "$(uname -s)" == "Darwin" ]]; then
            gcc -Wall -Wextra -std=c99 -O2 -c youtube_transcript.c -o youtube_transcript.o
            gcc -Wall -Wextra -std=c99 -O2 -c cJSON.c -o cJSON.o
            gcc youtube_transcript.o cJSON.o -o youtube_transcript -lcurl -lm
            strip youtube_transcript  # macOS strip without -s
        else
            make
        fi
    )

    # Install binary (outside subshell, use absolute path)
    if [[ -f "$SRC_DIR/youtube_transcript" ]]; then
        cp "$SRC_DIR/youtube_transcript" "$BIN_DIR/"
        chmod +x "$BIN_DIR/youtube_transcript"
        success "Installed youtube_transcript to $BIN_DIR"
    else
        error "Build failed - binary not found"
        return 1
    fi
}

# Check dependencies
check_dependencies() {
    local missing=()

    # Check for C compiler
    if ! command -v gcc &>/dev/null && ! command -v clang &>/dev/null; then
        missing+=("C compiler (gcc/clang)")
    fi

    # Check for make
    if ! command -v make &>/dev/null; then
        missing+=("make")
    fi

    # Check for curl (libcurl)
    if ! command -v curl &>/dev/null; then
        missing+=("curl")
    fi

    if [[ ${#missing[@]} -gt 0 ]]; then
        error "Missing dependencies: ${missing[*]}"
        info "Install them first via Homebrew (macOS) or pacman (Arch)"
        return 1
    fi

    return 0
}

# Main
if check_dependencies; then
    install_youtube_transcript
fi
