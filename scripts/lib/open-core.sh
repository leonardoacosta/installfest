#!/usr/bin/env bash
# open-core.sh — shared engine for the VIEW-family *open commands (ropen,
# plus gopen/sopen/mopen/iopen via viewopen.sh). Portal-aware: before falling back to the live-mount
# HTTP server, checks whether Atlas (docs portal) already has a durable URL
# for the target path — see at/docs/INDEX-CONTRACT.md for the manifest
# contract this implements against.
#
# EDIT-family commands (copen/vopen/zopen) do NOT source this — they always
# want the real source file open in an IDE, never a rendered portal copy.
#
# Sourced by: scripts/ropen.sh, scripts/viewopen.sh (as gopen/sopen/mopen/iopen)
(return 0 2>/dev/null) || set -euo pipefail

ROPEN_PORT="${ROPEN_PORT:-8889}"
STATE_DIR="/tmp/ropen-$(id -u)"
MOUNTS_JSON="$STATE_DIR/mounts.json"
PID_FILE="$STATE_DIR/server.pid"
LOCK_FILE="$STATE_DIR/mounts.lock"
LOG_FILE="$STATE_DIR/server.log"
OPEN_MAC_HOST="${OPEN_MAC_HOST:-mac}"
ATLAS_CACHE_TTL="${ATLAS_CACHE_TTL:-300}"
ATLAS_FETCH_TIMEOUT="${ATLAS_FETCH_TIMEOUT:-2}"

mkdir -p "$STATE_DIR"

DOTFILES="${DOTFILES:-$HOME/dev/personal/installfest}"
if [ -r "$DOTFILES/scripts/lib/registry.sh" ]; then
  # shellcheck source=/dev/null
  . "$DOTFILES/scripts/lib/registry.sh"
fi

# ── Server lifecycle ─────────────────────────────────────────────────────
open_core_server_running() {
  [[ -f "$PID_FILE" ]] || return 1
  local pid; pid="$(cat "$PID_FILE" 2>/dev/null || true)"
  [[ -n "$pid" ]] || return 1
  kill -0 "$pid" 2>/dev/null
}

open_core_require_server_or_die() {
  if ! open_core_server_running; then
    echo "$(basename "${OPEN_CORE_SELF:-open}"): ropen server not running — start it with \`systemctl --user start ropen\`" >&2
    exit 2
  fi
}

# ── Mount registration (flock + atomic write) ────────────────────────────
open_core_register_mount() {
  local mount="$1" serve_dir="$2" sentinel="$3"
  (
    flock 200
    python3 - "$MOUNTS_JSON" "$mount" "$serve_dir" "$sentinel" <<'PY'
import json, os, sys
path, mount, serve_dir, sentinel = sys.argv[1:5]
try:
    with open(path) as f:
        data = json.load(f)
    if "mounts" not in data:     data["mounts"] = {}
    if "sentinels" not in data:  data["sentinels"] = {}
except (FileNotFoundError, json.JSONDecodeError):
    data = {"mounts": {}, "sentinels": {}}
data["mounts"][mount] = serve_dir
data["sentinels"][mount] = sentinel
if not os.path.exists(sentinel):
    open(sentinel, "w").close()
tmp = path + ".tmp"
with open(tmp, "w") as f:
    json.dump(data, f, indent=2)
os.replace(tmp, path)
PY
  ) 200>"$LOCK_FILE"
}

# Resolve Tailscale IP (validate dotted-quad; fall back through interfaces).
open_core_resolve_ts_ip() {
  local ts_ip
  ts_ip="$(tailscale ip -4 2>/dev/null)" || ts_ip=""
  if ! echo "$ts_ip" | grep -qE '^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$' 2>/dev/null; then
    ts_ip="$(ip -4 addr show tailscale0 2>/dev/null | grep -oP '(?<=inet )[0-9.]+' | head -1)" || ts_ip=""
  fi
  [[ -z "$ts_ip" ]] && ts_ip="$(hostname -I | awk '{print $1}')"
  echo "$ts_ip"
}

# Derive mount slug: basename of serve dir, with a hash suffix only if
# another path has already claimed that slug in mounts.json.
open_core_derive_slug() {
  local base; base="$(basename "$1")"
  if [[ -f "$MOUNTS_JSON" ]]; then
    local existing
    existing=$(python3 -c '
import json, sys
try:
    data = json.load(open(sys.argv[1]))
except Exception:
    print(""); sys.exit(0)
print(data.get("mounts", {}).get(sys.argv[2], ""))
' "$MOUNTS_JSON" "$base")
    if [[ -n "$existing" && "$existing" != "$1" ]]; then
      local hash
      hash=$(echo -n "$1" | sha256sum | cut -c1-6)
      base="${base}-${hash}"
    fi
  fi
  echo "$base"
}

# ── Target resolution: existing path, or a projects.toml registry code ───
# Sets OPEN_RESOLVED_PATH (absolute) and OPEN_RESOLVED_IS_DIR (0|1).
open_core_resolve_target() {
  local arg="$1"
  OPEN_RESOLVED_PATH=""; OPEN_RESOLVED_IS_DIR=0
  if [[ -e "$arg" ]]; then
    OPEN_RESOLVED_PATH="$(realpath -- "$arg")"
  else
    if ! command -v registry_path >/dev/null 2>&1; then
      echo "open-core: not an existing path and registry.sh unavailable: $arg" >&2
      return 1
    fi
    local reg py proj_path
    reg="$(registry_path)" || return 1
    py="$(registry_python)" || return 1
    proj_path="$("$py" - "$reg" "$arg" <<'PY'
import sys, tomllib, os
reg, code = sys.argv[1:3]
with open(reg, "rb") as f:
    data = tomllib.load(f)
for p in data.get("projects", []):
    if p.get("code") == code:
        print(os.path.join(os.path.expanduser("~"), p["path"]))
        break
PY
)"
    if [[ -z "$proj_path" || ! -e "$proj_path" ]]; then
      echo "open-core: not an existing path and no registry code matches: $arg" >&2
      return 1
    fi
    OPEN_RESOLVED_PATH="$(realpath -- "$proj_path")"
  fi
  [[ -d "$OPEN_RESOLVED_PATH" ]] && OPEN_RESOLVED_IS_DIR=1
  return 0
}

# ── Atlas portal lookup (fail-open — at/docs/INDEX-CONTRACT.md) ──────────
# Echoes the full URL on a hit; returns 1 on ANY miss/failure (ATLAS_BASE_URL
# unset, unreachable, timeout, non-2xx, bad JSON, unrecognized version, key
# not present). Never raises past the caller. Caches the manifest for
# ATLAS_CACHE_TTL seconds so a repeated open doesn't refetch every time.
open_core_atlas_lookup() {
  local abs_path="$1"
  [[ -z "${ATLAS_BASE_URL:-}" ]] && return 1

  local dev_root="$HOME/dev" key=""
  case "$abs_path" in
    "$dev_root"/*) key="${abs_path#"$dev_root"/}" ;;
    *) return 1 ;;
  esac

  local cache="$STATE_DIR/atlas-index.json"
  local now age
  now="$(date +%s)"
  if [[ -f "$cache" ]]; then
    age=$(( now - $(stat -c %Y "$cache" 2>/dev/null || echo 0) ))
  else
    age=999999
  fi

  if [[ $age -ge $ATLAS_CACHE_TTL ]]; then
    local tmp="${cache}.tmp.$$"
    if curl -fsS --connect-timeout 1 --max-time "$ATLAS_FETCH_TIMEOUT" \
         "${ATLAS_BASE_URL%/}/index.json" -o "$tmp" 2>/dev/null; then
      mv -f "$tmp" "$cache"
    else
      rm -f "$tmp" 2>/dev/null
      # Never fetched successfully — no stale cache to fall back to either.
      [[ -f "$cache" ]] || return 1
    fi
  fi
  [[ -f "$cache" ]] || return 1

  local url_path
  url_path="$(python3 -c '
import json, sys
try:
    with open(sys.argv[1]) as f:
        data = json.load(f)
    if data.get("version") != 1:
        sys.exit(1)
    v = data.get("paths", {}).get(sys.argv[2])
    if not v:
        sys.exit(1)
    print(v)
except Exception:
    sys.exit(1)
' "$cache" "$key" 2>/dev/null)" || return 1
  [[ -z "$url_path" ]] && return 1
  echo "${ATLAS_BASE_URL%/}${url_path}"
}

# ── File-watch live-reload (mount path only) ─────────────────────────────
# One watcher per invocation. Touches the mount's sentinel when the target
# file changes; the server's SSE endpoint pushes a reload event to the
# browser tab. Backgrounded + disowned so the caller returns immediately.
open_core_spawn_watcher() {
  local file="$1" sentinel="$2"
  (
    if command -v inotifywait &>/dev/null; then
      while [[ -f "$file" ]] && open_core_server_running; do
        inotifywait -qq -e modify,move_self "$file" 2>/dev/null || sleep 2
        sleep 0.2
        touch "$sentinel" 2>/dev/null || break
      done
    else
      local last cur
      last=$(stat -c %Y "$file" 2>/dev/null || echo 0)
      while [[ -f "$file" ]] && open_core_server_running; do
        sleep 2
        cur=$(stat -c %Y "$file" 2>/dev/null || echo 0)
        if [[ "$cur" != "$last" ]]; then
          last="$cur"
          touch "$sentinel" 2>/dev/null || break
        fi
      done
    fi
  ) </dev/null >/dev/null 2>>"$LOG_FILE" & disown
}

# ── Resolve to an openable URL: Atlas first, ropen live-mount fallback ───
# Args: <resolved_path> <is_dir:0|1> <spawn_watcher:0|1>
# Sets: OPEN_URL, OPEN_URL_PREFIX, OPEN_VIA (atlas|mount)
open_core_resolve_url() {
  local path="$1" is_dir="$2" spawn_watcher="$3"
  OPEN_URL=""; OPEN_URL_PREFIX=""; OPEN_VIA=""

  local atlas_url
  if atlas_url="$(open_core_atlas_lookup "$path")"; then
    OPEN_URL="$atlas_url"; OPEN_URL_PREFIX="$atlas_url"; OPEN_VIA="atlas"
    return 0
  fi

  open_core_require_server_or_die

  local serve_dir rel_path git_root
  if [[ "$is_dir" == "1" ]]; then
    serve_dir="$path"
    rel_path=""
  else
    git_root="$(git -C "$(dirname "$path")" rev-parse --show-toplevel 2>/dev/null)" || git_root=""
    if [[ -n "$git_root" ]]; then
      serve_dir="$git_root"
    else
      serve_dir="$(dirname "$path")"
    fi
    rel_path="${path#"${serve_dir}/"}"
  fi

  local mount sentinel ts_ip
  mount="$(open_core_derive_slug "$serve_dir")"
  sentinel="$STATE_DIR/sentinel-$mount"
  open_core_register_mount "$mount" "$serve_dir" "$sentinel"

  ts_ip="$(open_core_resolve_ts_ip)"
  OPEN_URL="http://${ts_ip}:${ROPEN_PORT}/${mount}/${rel_path}"
  OPEN_URL_PREFIX="http://${ts_ip}:${ROPEN_PORT}/${mount}/"
  OPEN_VIA="mount"

  if [[ "$spawn_watcher" == "1" && "$is_dir" != "1" ]]; then
    open_core_spawn_watcher "$path" "$sentinel"
  fi
  return 0
}

# ── Mac dispatch: refresh/open a tab via AppleScript over ssh ────────────
# mode: auto (try Chrome then Safari — ropen's original behavior) | chrome | safari
open_core_dispatch_browser() {
  local url="$1" url_prefix="$2" mode="${3:-auto}"

  local chrome_block='
-- Try Google Chrome
try
  tell application "Google Chrome"
    repeat with w in windows
      set tabIdx to 0
      repeat with t in tabs of w
        set tabIdx to tabIdx + 1
        if URL of t starts with urlPrefix then
          set URL of t to theURL
          set active tab index of w to tabIdx
          set index of w to 1
          activate
          set tabFound to true
          exit repeat
        end if
      end repeat
      if tabFound then exit repeat
    end repeat
    if not tabFound then
      if (count of windows) > 0 then
        set newTab to make new tab at end of tabs of window 1
        set URL of newTab to theURL
        set active tab index of window 1 to (count of tabs of window 1)
      else
        make new window with properties {URL:theURL}
      end if
      activate
      set tabFound to true
    end if
  end tell
end try
'
  local safari_block='
-- Fall back to Safari
if not tabFound then
  try
    tell application "Safari"
      repeat with w in windows
        repeat with t in tabs of w
          if URL of t starts with urlPrefix then
            set current tab of w to t
            set URL of t to theURL
            set index of w to 1
            activate
            set tabFound to true
            exit repeat
          end if
        end repeat
        if tabFound then exit repeat
      end repeat
      if not tabFound then
        open location theURL
        activate
      end if
    end tell
  end try
end if
'
  local body=""
  case "$mode" in
    chrome) body="$chrome_block" ;;
    safari) body="$safari_block" ;;
    *)      body="${chrome_block}${safari_block}" ;;
  esac

  (
    ssh -o ConnectTimeout=3 -o StrictHostKeyChecking=no "$OPEN_MAC_HOST" "osascript -" <<APPLESCRIPT
set theURL to "$url"
set urlPrefix to "$url_prefix"
set tabFound to false
$body
APPLESCRIPT
  ) </dev/null >/dev/null 2>>"$LOG_FILE" & disown
}
