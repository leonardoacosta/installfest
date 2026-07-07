#!/usr/bin/env bash
# sopen — force-open a file/dir/registry-code in Safari on the Mac.
# VIEW-family, Atlas-portal-aware (see scripts/lib/open-core.sh). Same
# target resolution + live-mount fallback as ropen; the only difference is
# the forced browser — ropen tries Chrome then falls back to Safari, sopen
# always targets Safari (refreshing an existing matching tab if found).
#
# Usage: sopen [OPTIONS] <file|dir|registry-code>
#
# Options:
#   -q, --quiet    Suppress output
#   -h, --help     Show this help
#   -v, --version  Show version
#
# Env: ATLAS_BASE_URL, OPEN_MAC_HOST — see scripts/lib/open-core.sh.
(return 0 2>/dev/null) || set -euo pipefail

VERSION="1.0.0"
OPEN_CORE_SELF="$0"

DOTFILES="${DOTFILES:-$HOME/dev/if}"
# shellcheck source=/dev/null
. "$DOTFILES/scripts/lib/open-core.sh"

usage() { sed -n '2,15p' "$0" | sed 's/^# \{0,1\}//'; }

QUIET=0; POSITIONAL=()
while [[ $# -gt 0 ]]; do
  case "$1" in
    -h|--help)    usage; exit 0 ;;
    -v|--version) echo "sopen $VERSION"; exit 0 ;;
    -q|--quiet)   QUIET=1; shift ;;
    -*)           echo "sopen: unknown option $1" >&2; exit 1 ;;
    *)            POSITIONAL+=("$1"); shift ;;
  esac
done

[[ ${#POSITIONAL[@]} -eq 0 ]] && { echo "sopen: missing file argument (try --help)" >&2; exit 1; }

# On macOS, we ARE the Mac — force-launch Safari directly, no Tailscale hop.
if [[ "$(uname)" == "Darwin" ]]; then
  exec open -a Safari "${POSITIONAL[0]}"
fi

open_core_resolve_target "${POSITIONAL[0]}" || exit 1
open_core_resolve_url "$OPEN_RESOLVED_PATH" "$OPEN_RESOLVED_IS_DIR" 1

[[ $QUIET -eq 0 ]] && {
  if [[ "$OPEN_VIA" == "atlas" ]]; then
    echo "sopen: '$OPEN_RESOLVED_PATH' → Atlas (portal-indexed)"
  else
    echo "sopen: mount → $OPEN_RESOLVED_PATH"
  fi
  echo "       → ${OPEN_URL}"
}

open_core_dispatch_browser "$OPEN_URL" "$OPEN_URL_PREFIX" safari

trap - EXIT INT TERM
