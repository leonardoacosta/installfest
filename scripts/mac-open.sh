#!/usr/bin/env bash
# mac-open.sh — the single front door for "show this on my Mac" from the headless
# homelab. Routes URLs and files to the Mac's default browser (or a cmux pane),
# over Tailscale, with a working OAuth loopback callback. Replaces three older,
# overlapping mechanisms: cc-browser-open (OAuth URLs), ropen (files via a
# long-lived systemd server), and fview (files into cmux).
#
# The homelab has no display, so a bare `open`/`xdg-open` does nothing. This is
# wired in as both `$BROWSER` and the `open` alias on the homelab (rc/linux.zsh).
#
# One primitive, three transports — picked by what you hand it:
#   * Remote URL                -> ssh mac open URL                (Mac fetches it)
#   * Loopback URL (OAuth/dev)  -> reverse-tunnel the *dynamic* port, open
#                                  localhost:PORT on the Mac so its callback
#                                  tunnels back to the homelab listener
#   * Local file/dir            -> serve it over an on-demand HTTP server on a
#                                  RESERVED port bound to the Tailscale IP, then
#                                  open http://<ts-ip>:<port>/<rel> on the Mac.
#                                  The server is started on first use, reused if
#                                  already live, and self-reaps via `timeout` —
#                                  no systemd daemon, no random port to chase.
#
# Mac unreachable -> print a clickable (OSC 8) link/path and exit 0.
#
# Usage:
#   mac-open.sh <url|file>            open on the Mac's default browser
#   mac-open.sh --cmux <url|file>     open in the cmux embedded browser (old fview)
#   mac-open.sh --print <url|file>    just print the clickable link, no dispatch
#
# Env:
#   CC_BROWSER_MAC_HOST    ssh host alias for the Mac        (default: mac)
#   MAC_OPEN_PORT          reserved file-server port         (default: 8790)
#   MAC_OPEN_TTL           file-server lifetime, seconds     (default: 3600)
#   CC_BROWSER_TUNNEL_TTL  loopback reverse-tunnel seconds   (default: 180)
#   MAC_OPEN_ROOT          file-server root                  (default: $HOME)
#   CC_BROWSER_DRYRUN=1    print actions instead of running them (for tests)

set -uo pipefail

MAC_HOST="${CC_BROWSER_MAC_HOST:-mac}"
FILE_PORT="${MAC_OPEN_PORT:-8790}"
FILE_TTL="${MAC_OPEN_TTL:-3600}"
TUNNEL_TTL="${CC_BROWSER_TUNNEL_TTL:-180}"
FILE_ROOT="${MAC_OPEN_ROOT:-$HOME}"
DRYRUN="${CC_BROWSER_DRYRUN:-}"

DOTFILES="${DOTFILES:-$HOME/dev/personal/installfest}"

# --- Arg parsing -------------------------------------------------------------
mode="browser"   # browser | cmux | print
target=""
for arg in "$@"; do
  case "$arg" in
    --cmux|--split) mode="cmux" ;;
    --print)        mode="print" ;;
    --) ;;
    *) [ -z "$target" ] && target="$arg" ;;
  esac
done
[ -z "$target" ] && { echo "mac-open: no URL or file given" >&2; exit 1; }

# --- Helpers -----------------------------------------------------------------
# POSIX-safe single-quote of an arbitrary string for the remote shell.
shq() { printf "'%s'" "$(printf '%s' "$1" | sed "s/'/'\\\\''/g")"; }

# OSC 8 clickable hyperlink (matches flink in rc/linux.zsh).
print_link() { printf '\e]8;;%s\e\\%s\e]8;;\e\\\n' "$1" "${2:-$1}"; }

ts_ip() {
  tailscale ip -4 2>/dev/null | head -1 \
    || ip -4 addr show tailscale0 2>/dev/null | grep -oP '(?<=inet )[0-9.]+' | head -1
}

mac_reachable() { ssh -o BatchMode=yes -o ConnectTimeout=3 "$MAC_HOST" true 2>/dev/null; }

# Extract a loopback callback port from a URL (encoded or plain), else empty.
loopback_port() {
  printf '%s' "$1" \
    | grep -oiE '(localhost|127\.0\.0\.1)(%3a|:)[0-9]{2,5}' \
    | grep -oE '[0-9]{2,5}$' | head -1
}

# Start the reserved-port file server if it isn't already serving. Bound to the
# Tailscale IP (tailnet-only), self-reaped by `timeout` after FILE_TTL.
ensure_file_server() {
  local ip="$1"
  if [ -n "$DRYRUN" ]; then
    echo "[dryrun] ensure server: timeout $FILE_TTL python3 -m http.server $FILE_PORT --bind $ip --directory $FILE_ROOT"
    return 0
  fi
  if curl -s -o /dev/null --max-time 1 "http://${ip}:${FILE_PORT}/" 2>/dev/null; then
    return 0   # already serving — reuse
  fi
  setsid bash -c "timeout '$FILE_TTL' python3 -m http.server '$FILE_PORT' --bind '$ip' --directory '$FILE_ROOT'" \
    >/dev/null 2>&1 < /dev/null &
  disown 2>/dev/null || true
  local i=0
  while [ $i -lt 10 ]; do
    curl -s -o /dev/null --max-time 1 "http://${ip}:${FILE_PORT}/" 2>/dev/null && return 0
    i=$((i + 1)); sleep 0.1
  done
  return 0
}

# --- Resolve the target into a URL the Mac can open --------------------------
open_url=""
callback_port=""

resolve_target() {
  case "$target" in
    http://*|https://*)
      open_url="$target"
      callback_port="$(loopback_port "$target")"
      ;;
    *)
      local abs rel ip groot
      abs="$(realpath -- "$target" 2>/dev/null || printf '%s' "$target")"
      if [ ! -e "$abs" ]; then
        # Relative path didn't resolve against $PWD — retry against the git repo
        # root, so `open docs/diagrams/x.html` works from anywhere in the repo
        # (not just the toplevel). Falls through to the error if still missing.
        groot="$(git rev-parse --show-toplevel 2>/dev/null)"
        if [ -n "$groot" ] && [ -e "$groot/$target" ]; then
          abs="$(realpath -- "$groot/$target")"
        else
          echo "mac-open: not a URL and not an existing path: $target" >&2
          exit 1
        fi
      fi
      case "$abs" in
        "$FILE_ROOT"/*) rel="${abs#"$FILE_ROOT"/}" ;;
        "$FILE_ROOT")   rel="" ;;
        *)
          echo "mac-open: file is outside the served root ($FILE_ROOT): $abs" >&2
          echo "          move it under \$HOME, or set MAC_OPEN_ROOT." >&2
          exit 1 ;;
      esac
      ip="$(ts_ip)"
      [ -z "$ip" ] && { echo "mac-open: no Tailscale IP — cannot serve files" >&2; exit 1; }
      ensure_file_server "$ip"
      open_url="http://${ip}:${FILE_PORT}/${rel}"
      ;;
  esac
}

# --- Dispatch surfaces -------------------------------------------------------
open_reverse_tunnel() {
  local port="$1"
  if [ -n "$DRYRUN" ]; then
    echo "[dryrun] ssh -f -R ${port}:localhost:${port} ${MAC_HOST} sleep ${TUNNEL_TTL}"; return 0
  fi
  ssh -f -o BatchMode=yes -o ExitOnForwardFailure=yes \
      -R "${port}:localhost:${port}" "$MAC_HOST" "sleep ${TUNNEL_TTL}" 2>/dev/null
}

open_on_mac() {
  if [ -n "$DRYRUN" ]; then echo "[dryrun] ssh ${MAC_HOST} open $(shq "$open_url")"; return 0; fi
  # fire-and-forget: detach so we return immediately (ropen parity)
  ( ssh -o BatchMode=yes -o ConnectTimeout=5 "$MAC_HOST" "open $(shq "$open_url")" >/dev/null 2>&1 & ) 2>/dev/null
}

open_in_cmux() {
  if [ -n "$DRYRUN" ]; then echo "[dryrun] cmux browser open-split $open_url (or cmux-bridge)"; return 0; fi
  if [ -S /tmp/cmux.sock ]; then
    cmux browser open-split "$open_url" >/dev/null 2>&1
  elif [ -S "${CMUX_SOCKET_PATH:-/tmp/cmux-remote.sock}" ]; then
    python3 "$DOTFILES/scripts/cmux-bridge.py" browser-open "$open_url" >/dev/null 2>&1
  else
    return 1
  fi
}

# --- Route -------------------------------------------------------------------
resolve_target

if [ "$mode" = "print" ]; then
  print_link "$open_url"; exit 0
fi

if [ "$mode" = "cmux" ]; then
  if [ -n "$DRYRUN" ] || open_in_cmux; then
    echo "mac-open: opened in cmux"; print_link "$open_url"; exit 0
  fi
  # no cmux socket — fall through to the Mac browser
fi

if [ -n "$DRYRUN" ] || mac_reachable; then
  [ -n "$callback_port" ] && open_reverse_tunnel "$callback_port"
  open_on_mac
  if [ -n "$callback_port" ]; then
    echo "mac-open: opened on $MAC_HOST (loopback :$callback_port tunneled home)"
  else
    echo "mac-open: opened on $MAC_HOST"
  fi
  print_link "$open_url"
  exit 0
fi

# Mac unreachable.
echo "mac-open: Mac unreachable — open this yourself:" >&2
print_link "$open_url" >&2
if [ -n "$callback_port" ]; then
  echo "mac-open: loopback OAuth URL (port $callback_port) — the browser will show a" >&2
  echo "          code; paste it back into the prompt." >&2
fi
exit 0   # exit clean so callers (e.g. CC) proceed to their own fallback
