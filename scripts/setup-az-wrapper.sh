#!/bin/bash
# setup-az-wrapper.sh — First-time setup for the smart az CLI wrapper
# Creates identity directories, verifies dependencies, and runs device-code login
set -uo pipefail

if [[ "${1:-}" == "--help" || "${1:-}" == "-h" ]]; then
	cat <<'EOF'
Usage: bash scripts/setup-az-wrapper.sh   (no args)

First-time setup for the smart az CLI wrapper: creates the BBAdmin/O365
identity config directories, verifies the real az binary + SOCKS tunnel,
then walks both identities through an interactive device-code login.
EOF
	exit 0
fi

DOTFILES="${DOTFILES:-$HOME/dev/personal/installfest}"
source "$DOTFILES/scripts/utils.sh" 2>/dev/null || {
    info() { echo "[INFO] $*"; }
    success() { echo "[OK] $*"; }
    warning() { echo "[WARN] $*"; }
}

echo "=== az CLI Wrapper Setup ==="
echo ""

# --- 1. Create identity config directories ---
mkdir -p "$HOME/.azure-bbadmin" "$HOME/.azure-o365"
success "Created ~/.azure-bbadmin and ~/.azure-o365"

# --- 2. Verify real az binary ---
# Candidate order MUST match home/dot_local/bin/executable_az (the deployed wrapper).
REAL_AZ=""
for candidate in \
    "$HOME/.local/share/pipx/venvs/azure-cli/bin/az" \
    "/usr/bin/az" \
    "/usr/local/bin/az" \
    "/opt/homebrew/bin/az"; do
    [ -x "$candidate" ] && REAL_AZ="$candidate" && break
done

if [ -z "$REAL_AZ" ]; then
    echo "ERROR: Azure CLI not found." >&2
    echo "Install via: brew install azure-cli (macOS) or pipx install azure-cli (Linux)" >&2
    exit 1
fi
success "Found az binary: $REAL_AZ"

# --- 3. Detect proxy method ---
# Linux: proxychains4 (LD_PRELOAD)
# macOS: HTTPS_PROXY env var (az natively supports it)
PROXYCHAINS_CONF="$HOME/.config/proxychains/proxychains.conf"
USE_PROXYCHAINS=false

if [ -f "$PROXYCHAINS_CONF" ] && command -v proxychains4 >/dev/null 2>&1; then
    USE_PROXYCHAINS=true
    success "Proxy method: proxychains4"
else
    success "Proxy method: HTTPS_PROXY=socks5h://127.0.0.1:1080"
fi

# Helper: run az through the tunnel
run_az() {
    if [ "$USE_PROXYCHAINS" = true ]; then
        proxychains4 -q -f "$PROXYCHAINS_CONF" "$REAL_AZ" "$@"
    else
        HTTPS_PROXY="socks5h://127.0.0.1:1080" HTTP_PROXY="socks5h://127.0.0.1:1080" "$REAL_AZ" "$@"
    fi
}

# --- 4. Verify SOCKS tunnel ---
if pgrep -f "ssh.*-D.*1080.*cloudpc" >/dev/null 2>&1; then
    success "SOCKS tunnel is running"
else
    warning "SOCKS tunnel not running"
    case "$(uname -s)" in
        Darwin) warning "Start with: launchctl start com.leonardoacosta.cloudpc-tunnel" ;;
        Linux)  warning "Start with: systemctl --user start cloudpc-tunnel" ;;
    esac
fi

# --- 5. Login: BBAdmin ---
echo ""
info "=== BBAdmin Login (BBAdminLAcosta@bbins.com) ==="
info "Sign in as: BBAdminLAcosta@bbins.com"
echo ""
AZURE_CONFIG_DIR="$HOME/.azure-bbadmin" run_az login --use-device-code

# --- 6. Login: O365 ---
echo ""
info "=== O365 Login (leonardo.acosta@bridgespecialty.com) ==="
info "Sign in as: leonardo.acosta@bridgespecialty.com"
echo ""
AZURE_CONFIG_DIR="$HOME/.azure-o365" run_az login --use-device-code

# --- 7. Verify both identities ---
echo ""
info "Verifying identities..."

BBADMIN_ACCOUNT=$(AZURE_CONFIG_DIR="$HOME/.azure-bbadmin" run_az account show --query "user.name" -o tsv 2>/dev/null || echo "FAILED")
O365_ACCOUNT=$(AZURE_CONFIG_DIR="$HOME/.azure-o365" run_az account show --query "user.name" -o tsv 2>/dev/null || echo "FAILED")

echo ""
echo "=== Summary ==="
echo "  BBAdmin: $BBADMIN_ACCOUNT"
echo "  O365:    $O365_ACCOUNT"
echo ""

if [ "$BBADMIN_ACCOUNT" != "FAILED" ] && [ "$O365_ACCOUNT" != "FAILED" ]; then
    success "Both identities configured successfully"
else
    warning "One or more identities failed — re-run setup or login manually"
fi
