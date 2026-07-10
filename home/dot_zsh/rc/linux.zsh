# rc/linux.zsh - Linux (Arch) specific shell configuration
# Sourced by .zshrc on Linux

# ============================================================
# Aliases (runtime paths moved to .zshenv)
# ============================================================

# Color support (ls handled by eza in load-tools.zsh)
# alias ls="ls --color=auto"  # Superseded by eza
alias grep="grep --color=auto"
alias fgrep="fgrep --color=auto"
alias egrep="egrep --color=auto"
alias diff="diff --color=auto"
alias ip="ip -color=auto"

# Homelab shortcuts
alias hl="cd ~/dev/personal/homelab/homelab"
alias hls="cd ~/dev/personal/homelab/homelab && ./homelab.sh status"
alias hll="cd ~/dev/personal/homelab/homelab && ./homelab.sh logs"
alias hlr="cd ~/dev/personal/homelab/homelab && ./homelab.sh restart"

# Docker shortcuts
alias dc="docker compose"
alias dps="docker ps --format 'table {{.Names}}\t{{.Status}}\t{{.Ports}}'"
alias dlog="docker compose logs -f"
alias dexec="docker exec -it"
alias dprune="docker system prune -af"

# Systemctl shortcuts
alias sc="sudo systemctl"
alias scs="sudo systemctl status"
alias scr="sudo systemctl restart"
alias sce="sudo systemctl enable"
alias scd="sudo systemctl disable"
alias jctl="journalctl -xe"

# Pacman/yay shortcuts
alias pacup="sudo pacman -Syu"
alias pacin="sudo pacman -S"
alias pacrm="sudo pacman -Rns"
alias pacss="pacman -Ss"
alias pacqi="pacman -Qi"
alias yayup="yay -Syu"

# Project switching
[[ -f "$HOME/dev/ccswitch.sh" ]] && alias cs="$HOME/dev/ccswitch.sh --switch"

# Clipboard (xclip/xsel fallback)
if command -v xclip &>/dev/null; then
  alias pbcopy="xclip -selection clipboard"
  alias pbpaste="xclip -selection clipboard -o"
elif command -v xsel &>/dev/null; then
  alias pbcopy="xsel --clipboard --input"
  alias pbpaste="xsel --clipboard --output"
elif command -v wl-copy &>/dev/null; then
  alias pbcopy="wl-copy"
  alias pbpaste="wl-paste"
fi

# Open — on a headless box (homelab over SSH) there's no local browser, so route
# URLs/files to the Mac via mac-open (scripts/mac-open.sh, symlinked onto PATH).
# On a Linux desktop with a display, keep native xdg-open.
if [[ -z "${DISPLAY:-}" && -z "${WAYLAND_DISPLAY:-}" ]] && command -v mac-open &>/dev/null; then
  alias open="mac-open"
else
  alias open="xdg-open"
fi

# Nexus CLI shortcut
alias nx="nexus"

# ============================================================
# File server (cmux embedded browser)
# ============================================================

# fview - open a file in the cmux embedded browser.
# Thin wrapper over mac-open --cmux (scripts/mac-open.sh), which serves the file
# on the reserved port and dispatches to cmux (local socket or forwarded
# cmux-bridge), falling through to the Mac's default browser if no cmux is present.
# Usage: fview path/to/file.md
fview() {
  : "${1:?usage: fview <file>}"
  mac-open --cmux "$@"
}

# flink - print OSC 8 clickable hyperlink (for terminal output)
# Usage: flink path/to/file.md [label]
flink() {
  local file="${1:?usage: flink <file> [label]}"
  local abs_path
  abs_path=$(realpath "$file" 2>/dev/null || echo "$file")
  local label="${2:-$abs_path}"
  local host
  host=$(tailscale ip -4 2>/dev/null \
    || ip -4 addr show tailscale0 2>/dev/null | grep -oP 'inet \K[\d.]+' \
    || echo "localhost")
  local port="${FILE_SERVER_PORT:-8787}"
  local url="http://${host}:${port}${abs_path}"
  # Append the shared auth token if the server has generated one; fall back to
  # the bare URL so flink never breaks before the service has started.
  local token_file="$HOME/.local/state/file-server.token"
  [[ -r "$token_file" ]] && url="${url}?t=$(<"$token_file")"
  printf '\e]8;;%s\e\\%s\e]8;;\e\\\n' "$url" "$label"
}
