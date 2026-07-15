# rc/darwin.zsh - macOS-specific shell configuration
# Sourced by .zshrc on Darwin

# ============================================================
# PATH priority fix — macOS /etc/zprofile runs path_helper AFTER
# .zshenv, which rebuilds PATH and pushes user entries to the end.
# Re-prepend here (.zshrc runs after /etc/zprofile) to ensure
# ~/.local/bin takes priority over /opt/homebrew/bin.
# ============================================================
export PATH="$HOME/.claude/bin:$HOME/.local/bin:$PATH"

# ============================================================
# macOS Aliases (Homebrew, runtime paths moved to .zshenv)
# (ls handled by eza in load-tools.zsh)
# ============================================================

# Open current directory in Finder
alias finder="open ."

# Flush DNS cache
alias flushdns="sudo dscacheutil -flushcache; sudo killall -HUP mDNSResponder"

# Show/hide hidden files in Finder
alias showfiles="defaults write com.apple.finder AppleShowAllFiles YES; killall Finder"
alias hidefiles="defaults write com.apple.finder AppleShowAllFiles NO; killall Finder"

# Homebrew shortcuts
alias brewup="brew update && brew upgrade && brew cleanup"

# pbcopy/pbpaste available natively on macOS

# ============================================================
# edge-tunnel — Launch Edge in a dedicated profile routed through the
# cloudpc SSH SOCKS tunnel, with remote DNS resolution enabled.
#
# Why this function exists:
#   Some corporate hostnames (e.g. devops.southandwestern.com) use
#   split-horizon DNS — public DNS returns a Cloudflare-proxied IP that
#   won't serve you, while cloudpc's internal resolver returns the real
#   RFC 1918 IP. Browsing through a plain SOCKS5 proxy is not enough:
#   Chromium resolves DNS locally by default, so you still hit the wrong
#   public endpoint. --host-resolver-rules forces Chromium to defer all
#   name resolution to the proxy (cloudpc), which returns the correct
#   internal IP and routes the connection through the tunnel.
#
#   --user-data-dir is load-bearing on macOS: without it, `open --args`
#   silently delegates to the already-running Edge process and drops
#   your proxy flags. The dedicated profile runs as a distinct process
#   so the flags actually take effect, and keeps tunneled SSO/cookies
#   isolated from your regular Edge.
#
# Usage:
#   edge-tunnel                                   # blank window, default SOCKS port
#   edge-tunnel https://devops.southandwestern.com/tfs/SWTFVC
#   edge-tunnel localhost:1080 https://...        # explicit SOCKS host:port
# ============================================================
edge-tunnel() {
  local SOCKS_HOST="localhost:1080"
  local -a URLS

  if [[ "$1" == *:* && "$1" != *://* ]]; then
    SOCKS_HOST="$1"
    shift
  fi
  URLS=("$@")

  if ! nc -z ${SOCKS_HOST%:*} ${SOCKS_HOST#*:} 2>/dev/null; then
    echo "✗ SOCKS tunnel not listening on $SOCKS_HOST" >&2
    echo "  start it with: launchctl kickstart -k gui/$(id -u)/com.leonardoacosta.cloudpc-tunnel" >&2
    return 1
  fi

  local PROFILE_DIR="$HOME/.config/edge-tunnel"
  mkdir -p "$PROFILE_DIR"

  open -n -a "Microsoft Edge" --args \
    --user-data-dir="$PROFILE_DIR" \
    --proxy-server="socks5://$SOCKS_HOST" \
    --host-resolver-rules="MAP * ~NOTFOUND , EXCLUDE localhost" \
    "${URLS[@]}"
}

edge-tunnel-kill() {
  pgrep -lf "user-data-dir=.*edge-tunnel" | awk '{print $1}' | xargs -r kill 2>/dev/null
  echo "✓ tunneled Edge processes killed"
}
