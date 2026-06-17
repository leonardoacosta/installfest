#!/usr/bin/env bash
# cc-browser-open.sh — route browser-opens from the headless homelab to the Mac,
# with a working OAuth loopback callback, falling back to printing the URL.
#
# Why this exists:
#   The homelab has no local browser. When `claude` (Claude Code) needs to log in
#   it builds a claude.ai OAuth authorize URL whose redirect_uri points at a
#   *dynamic* loopback port on THIS machine (http://localhost:<PORT>/callback) and
#   then opens it via $BROWSER. On a headless box that open goes nowhere and CC
#   drops to the "paste the code" fallback.
#
#   This script (wired in as $BROWSER on the homelab) instead:
#     1. opens a reverse SSH tunnel  ssh mac -R PORT:localhost:PORT  so the Mac's
#        own 127.0.0.1:PORT forwards back to the homelab's CC callback listener,
#     2. opens the URL in the Mac's default browser (ssh mac -- open URL).
#   The Mac browser completes auth, redirects to its localhost:PORT, and the bytes
#   tunnel home — a seamless loopback OAuth flow across two machines. This is the
#   Claude Code analogue of `fview` (rc/linux.zsh), which routes file-opens to the
#   Mac's cmux over the same Tailscale link.
#
#   Tiers:
#     Mac reachable  -> tunnel (when a loopback callbackPort is present) + open on Mac
#     Mac down       -> print a clickable (OSC 8) URL + paste-the-code hint
#   (An iPhone-over-Tailscale middle tier is planned but not yet wired; see if-ox3.)
#
# Usage (normally invoked by CC as the program named in $BROWSER):
#   cc-browser-open.sh <url>
#
# Env:
#   CC_BROWSER_MAC_HOST   ssh host alias for the Mac        (default: mac)
#   CC_BROWSER_TUNNEL_TTL seconds the reverse tunnel lives  (default: 180)
#   CC_BROWSER_DRYRUN=1   print the actions instead of running ssh (for testing)

set -uo pipefail

MAC_HOST="${CC_BROWSER_MAC_HOST:-mac}"
TUNNEL_TTL="${CC_BROWSER_TUNNEL_TTL:-180}"
DRYRUN="${CC_BROWSER_DRYRUN:-}"

# CC passes the URL as the first argument. Some openers use a "%s" template or
# append extra args; take the first thing that looks like a URL, else $1.
url=""
for arg in "$@"; do
  case "$arg" in
    http://*|https://*) url="$arg"; break ;;
  esac
done
[ -z "$url" ] && url="${1:-}"

if [ -z "$url" ]; then
  echo "cc-browser-open: no URL given" >&2
  exit 1
fi

# --- Extract a loopback callback port, if this is an OAuth-style URL ---------
# Matches both percent-encoded (localhost%3A49207) and plain (localhost:49207)
# forms anywhere in the URL — CC carries it inside the redirect_uri parameter.
callback_port="$(printf '%s' "$url" \
  | grep -oiE '(localhost|127\.0\.0\.1)(%3a|:)[0-9]{2,5}' \
  | grep -oE '[0-9]{2,5}$' \
  | head -1)"

# POSIX-safe single-quote of an arbitrary string for the remote shell.
shq() { printf "'%s'" "$(printf '%s' "$1" | sed "s/'/'\\\\''/g")"; }

# --- OSC 8 clickable hyperlink (matches flink in rc/linux.zsh) ---------------
print_link() {
  local target="$1" label="${2:-$1}"
  printf '\e]8;;%s\e\\%s\e]8;;\e\\\n' "$target" "$label"
}

print_fallback() {
  echo "cc-browser-open: Mac unreachable — open this URL yourself:" >&2
  print_link "$url" >&2
  if [ -n "$callback_port" ]; then
    echo "cc-browser-open: this is a loopback OAuth URL (port $callback_port); the" >&2
    echo "                 browser will show a code — paste it back into the prompt." >&2
  fi
}

mac_reachable() {
  ssh -o BatchMode=yes -o ConnectTimeout=3 "$MAC_HOST" true 2>/dev/null
}

open_tunnel() {
  # Bind mac:127.0.0.1:PORT -> homelab:127.0.0.1:PORT for TUNNEL_TTL seconds.
  # The remote `sleep` is what keeps the forward alive; -f backgrounds it so the
  # tunnel self-reaps and we never accumulate dangling forwards. A pre-existing
  # bind (rare with dynamic ports) makes ssh exit non-zero via ExitOnForwardFailure;
  # we treat that as "already forwarded" and continue.
  local port="$1"
  if [ -n "$DRYRUN" ]; then
    echo "[dryrun] ssh -f -R ${port}:localhost:${port} ${MAC_HOST} sleep ${TUNNEL_TTL}"
    return 0
  fi
  ssh -f -o BatchMode=yes -o ExitOnForwardFailure=yes \
      -R "${port}:localhost:${port}" "$MAC_HOST" "sleep ${TUNNEL_TTL}" 2>/dev/null
}

open_on_mac() {
  local target="$1"
  if [ -n "$DRYRUN" ]; then
    echo "[dryrun] ssh ${MAC_HOST} open $(shq "$target")"
    return 0
  fi
  ssh -o BatchMode=yes -o ConnectTimeout=5 "$MAC_HOST" "open $(shq "$target")" 2>/dev/null
}

# --- Route -------------------------------------------------------------------
if [ -n "$DRYRUN" ] || mac_reachable; then
  if [ -n "$callback_port" ]; then
    open_tunnel "$callback_port"  # best-effort; continue even if already bound
  fi
  if open_on_mac "$url"; then
    if [ -n "$callback_port" ]; then
      echo "cc-browser-open: opened on $MAC_HOST (loopback :$callback_port tunneled home)"
    else
      echo "cc-browser-open: opened on $MAC_HOST"
    fi
    print_link "$url"   # leave a clickable record in the terminal too
    exit 0
  fi
fi

# Mac unreachable, or the open failed.
print_fallback
exit 0   # exit clean so CC proceeds to its own manual code-paste fallback
