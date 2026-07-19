#!/usr/bin/env bash
set -uo pipefail
# ropen — remote file/dir opener over Tailscale (multi-project mounts,
# Atlas-portal-aware). Refreshes an existing Chrome/Safari tab (or opens a
# new one) on the Mac.
#
# Resolution order (see scripts/lib/open-core.sh):
#   1. Atlas docs portal — if ATLAS_BASE_URL is set and the target is
#      portal-indexed, use that durable URL directly.
#   2. ropen's own live-mount HTTP server (systemd-managed) — multi-project
#      mounts + live-reload via file watcher + SSE.
# Atlas is a durability optimization on top of ropen, not a dependency —
# any Atlas failure (unset, unreachable, timeout, bad response) fails open
# straight to today's live-mount behavior.
#
# Usage: ropen [OPTIONS] <file|dir|registry-code>
#        ropen --mount <dir>
#        ropen --list
#
# Server lifecycle is owned by systemd:
#   systemctl --user start|stop|restart|status ropen
#
# Options:
#   -p, --port PORT    Server port (default: 8889, env: ROPEN_PORT)
#   -q, --quiet        Suppress output
#       --mount DIR    Register DIR as a mount and exit (no Mac dispatch)
#       --list         List active mounts
#   -h, --help         Show this help
#   -v, --version      Show version
#
# Env:
#   ATLAS_BASE_URL     Atlas docs-portal base URL. Unset/empty = skip the
#                       portal check entirely. See at/docs/INDEX-CONTRACT.md.
#   OPEN_MAC_HOST       ssh alias for the Mac (default: mac)
(return 0 2>/dev/null) || set -euo pipefail

VERSION="1.0.0"
OPEN_CORE_SELF="$0"

DOTFILES="${DOTFILES:-$HOME/dev/personal/installfest}"
# shellcheck source=/dev/null
. "$DOTFILES/scripts/lib/open-core.sh"

usage() { sed -n '2,24p' "$0" | sed 's/^# \{0,1\}//'; }

list_mounts() {
  if [[ -f "$MOUNTS_JSON" ]]; then
    python3 -c '
import json, sys
try:
    data = json.load(open(sys.argv[1]))
except Exception:
    print("ropen: mounts.json unreadable", file=sys.stderr); sys.exit(1)
mounts = data.get("mounts", {})
if not mounts:
    print("ropen: no mounts registered"); sys.exit(0)
width = max(len(k) for k in mounts) if mounts else 0
for k, v in sorted(mounts.items()):
    print(f"  {k:<{width}}  {v}")
' "$MOUNTS_JSON"
  else
    echo "ropen: no mounts registered"
  fi
  if [[ -f "$PID_FILE" ]] && kill -0 "$(cat "$PID_FILE" 2>/dev/null)" 2>/dev/null; then
    echo "ropen: server pid $(cat "$PID_FILE") on :$ROPEN_PORT"
  else
    echo "ropen: server not running"
  fi
}

QUIET=0; POSITIONAL=(); DO_LIST=0; DO_SERVE=0; DO_MOUNT=0; MOUNT_DIR=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    -h|--help)    usage; exit 0 ;;
    -v|--version) echo "ropen $VERSION"; exit 0 ;;
    -q|--quiet)   QUIET=1; shift ;;
    -p|--port)    ROPEN_PORT="$2"; shift 2 ;;
    --list)       DO_LIST=1; shift ;;
    --mount)      DO_MOUNT=1; MOUNT_DIR="${2:-}"; shift 2 ;;
    --serve)      DO_SERVE=1; shift ;;
    -*)           echo "ropen: unknown option $1" >&2; exit 1 ;;
    *)            POSITIONAL+=("$1"); shift ;;
  esac
done

[[ $DO_LIST -eq 1 ]] && { list_mounts; exit 0; }

# --serve: foreground server for systemd-user supervision. No mount
# registration, no Mac dispatch — just exec python so systemd manages the
# process directly.
if [[ $DO_SERVE -eq 1 ]]; then
  export DOTFILES
  mkdir -p "$STATE_DIR"
  [[ -f "$MOUNTS_JSON" ]] || echo '{"mounts":{},"sentinels":{}}' > "$MOUNTS_JSON"
  : > "$LOG_FILE"
  # Write PID before exec — exec keeps $$ stable, so this is the PID python3
  # runs as. Lets open_core_server_running() find us.
  echo $$ > "$PID_FILE"
  PY="python3"
  command -v registry_python >/dev/null 2>&1 && PY="$(registry_python)" || true
  exec "$PY" "$DOTFILES/scripts/ropen-server.py" "$ROPEN_PORT" "$MOUNTS_JSON"
fi

# --mount <dir>: register a mount without dispatching to Mac. Bypasses the
# Atlas check deliberately — this is an explicit pre-warm request.
if [[ $DO_MOUNT -eq 1 ]]; then
  [[ -z "$MOUNT_DIR" ]] && { echo "ropen: --mount requires a directory argument" >&2; exit 1; }
  open_core_require_server_or_die
  DIR="$(realpath "$MOUNT_DIR" 2>/dev/null)" || DIR=""
  [[ -z "$DIR" || ! -d "$DIR" ]] && { echo "ropen: not a directory: $MOUNT_DIR" >&2; exit 1; }
  MOUNT="$(open_core_derive_slug "$DIR")"
  SENTINEL="$STATE_DIR/sentinel-$MOUNT"
  open_core_register_mount "$MOUNT" "$DIR" "$SENTINEL"
  TS_IP="$(open_core_resolve_ts_ip)"
  [[ $QUIET -eq 0 ]] && {
    echo "ropen: mount '$MOUNT' → $DIR"
    echo "       → http://${TS_IP}:${ROPEN_PORT}/${MOUNT}/"
  }
  exit 0
fi

[[ ${#POSITIONAL[@]} -eq 0 ]] && { echo "ropen: missing file argument (try --help)" >&2; exit 1; }

# On macOS, use the native opener — we ARE the Mac, no Tailscale hop needed.
if [[ "$(uname)" == "Darwin" ]]; then
  exec /usr/bin/open "${POSITIONAL[0]}"
fi

open_core_resolve_target "${POSITIONAL[0]}" || exit 1
open_core_resolve_url "$OPEN_RESOLVED_PATH" "$OPEN_RESOLVED_IS_DIR" 1

[[ $QUIET -eq 0 ]] && {
  if [[ "$OPEN_VIA" == "atlas" ]]; then
    echo "ropen: '$OPEN_RESOLVED_PATH' → Atlas (portal-indexed)"
  else
    echo "ropen: mount → $OPEN_RESOLVED_PATH"
  fi
  echo "       → ${OPEN_URL}"
}

open_core_dispatch_browser "$OPEN_URL" "$OPEN_URL_PREFIX" auto
