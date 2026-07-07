#!/usr/bin/env bash
# iopen — open a file/dir/registry-code on iPhone via a Nexus APNS
# notification instead of auto-opening. Atlas-portal-aware (see
# scripts/lib/open-core.sh) — prefers a durable Atlas URL over ropen's
# ephemeral mount when available, since a push link that outlives your
# laptop session is strictly better than one that dies with it.
#
# The iPhone counterpart to mopen (Mac desktop notification) — same
# one-shot, click-to-open shape, no live-reload watcher spawned.
#
# Usage: iopen <file|dir|registry-code>
#
# Options:
#   -h, --help     Show this help
#   -v, --version  Show version
(return 0 2>/dev/null) || set -euo pipefail

VERSION="1.0.0"
OPEN_CORE_SELF="$0"

DOTFILES="${DOTFILES:-$HOME/dev/if}"
# shellcheck source=/dev/null
. "$DOTFILES/scripts/lib/open-core.sh"

usage() { sed -n '2,15p' "$0" | sed 's/^# \{0,1\}//'; }

POSITIONAL=()
while [[ $# -gt 0 ]]; do
  case "$1" in
    -h|--help)    usage; exit 0 ;;
    -v|--version) echo "iopen $VERSION"; exit 0 ;;
    -*)           echo "iopen: unknown option $1" >&2; exit 1 ;;
    *)            POSITIONAL+=("$1"); shift ;;
  esac
done

[[ ${#POSITIONAL[@]} -eq 0 ]] && { echo "iopen: missing file argument (try --help)" >&2; exit 1; }

open_core_resolve_target "${POSITIONAL[0]}" || exit 1
open_core_resolve_url "$OPEN_RESOLVED_PATH" "$OPEN_RESOLVED_IS_DIR" 0

# Source nx-send.sh for nx_ropen (owned by cc, degrades gracefully if absent).
# nx_ropen is the iPhone/APNS counterpart to nx_mopen (Mac desktop) — see
# cc's scripts/lib/nx-send.sh for both.
NX_SEND="${HOME}/.claude/scripts/lib/nx-send.sh"
if [[ -f "$NX_SEND" ]]; then
  # shellcheck source=/dev/null
  . "$NX_SEND"
  command -v nx_ropen >/dev/null 2>&1 && nx_ropen "$OPEN_URL"
else
  echo "iopen: warning: nx-send.sh not found at $NX_SEND — notification skipped" >&2
fi

echo "$OPEN_URL"
