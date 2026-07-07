#!/usr/bin/env bash
# mopen — open a file/dir/registry-code on the Mac via a Nexus desktop
# notification instead of auto-opening. Atlas-portal-aware (see
# scripts/lib/open-core.sh) — prefers a durable Atlas URL over ropen's
# ephemeral mount when available, since a notification link that outlives
# your laptop session is strictly better than one that dies with it.
#
# Unlike ropen (auto-opens immediately via osascript), mopen waits for a
# click — the Mac counterpart to a phone push. No live-reload watcher is
# spawned: a notification link is a one-shot, not a live doc session.
#
# Usage: mopen <file|dir|registry-code>
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

usage() { sed -n '2,16p' "$0" | sed 's/^# \{0,1\}//'; }

POSITIONAL=()
while [[ $# -gt 0 ]]; do
  case "$1" in
    -h|--help)    usage; exit 0 ;;
    -v|--version) echo "mopen $VERSION"; exit 0 ;;
    -*)           echo "mopen: unknown option $1" >&2; exit 1 ;;
    *)            POSITIONAL+=("$1"); shift ;;
  esac
done

[[ ${#POSITIONAL[@]} -eq 0 ]] && { echo "mopen: missing file argument (try --help)" >&2; exit 1; }

open_core_resolve_target "${POSITIONAL[0]}" || exit 1
open_core_resolve_url "$OPEN_RESOLVED_PATH" "$OPEN_RESOLVED_IS_DIR" 0

# Source nx-send.sh for nx_mopen (owned by cc, degrades gracefully if absent).
NX_SEND="${HOME}/.claude/scripts/lib/nx-send.sh"
if [[ -f "$NX_SEND" ]]; then
  # shellcheck source=/dev/null
  . "$NX_SEND"
  command -v nx_mopen >/dev/null 2>&1 && nx_mopen "$OPEN_URL"
else
  echo "mopen: warning: nx-send.sh not found at $NX_SEND — notification skipped" >&2
fi

echo "$OPEN_URL"
