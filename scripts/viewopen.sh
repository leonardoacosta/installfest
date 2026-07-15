#!/usr/bin/env bash
# viewopen — basename-dispatched wrapper for the four thin VIEW-family *open
# commands over scripts/lib/open-core.sh (same pattern as ideopen.sh's
# vopen/zopen dispatch; see docs/open-family.md):
#
#   gopen  -> force-open in Google Chrome on the Mac        (browser mode)
#   sopen  -> force-open in Safari on the Mac               (browser mode)
#   mopen  -> clickable Nexus desktop notification on Mac   (notify mode)
#   iopen  -> clickable Nexus APNS push to iPhone           (notify mode)
#
# Invoked by basename — symlink gopen/sopen/mopen/iopen -> viewopen.sh
# (home/dot_local/bin/symlink_*.tmpl). ropen keeps its own script: it
# additionally owns server lifecycle (--serve/--mount/--list).
#
# NOT related to scripts/view.sh (tmux-split terminal renderer).
#
# Env: ATLAS_BASE_URL, OPEN_MAC_HOST — see scripts/lib/open-core.sh.
(return 0 2>/dev/null) || set -euo pipefail

VERSION="1.0.0"
OPEN_CORE_SELF="$0"
SELF="$(basename "$0")"

DOTFILES="${DOTFILES:-$HOME/dev/personal/installfest}"
# shellcheck source=/dev/null
. "$DOTFILES/scripts/lib/open-core.sh"

MODE=""; DISPATCH=""; DARWIN_APP=""; NX_FN=""; DESC=""
case "$SELF" in
  gopen) MODE=browser; DISPATCH=chrome; DARWIN_APP="Google Chrome"
         DESC="force-open a file/dir/registry-code in Google Chrome on the Mac" ;;
  sopen) MODE=browser; DISPATCH=safari; DARWIN_APP="Safari"
         DESC="force-open a file/dir/registry-code in Safari on the Mac" ;;
  mopen) MODE=notify; NX_FN=nx_mopen
         DESC="post a clickable Nexus desktop notification on the Mac (no auto-open)" ;;
  iopen) MODE=notify; NX_FN=nx_ropen
         DESC="post a clickable Nexus APNS push to iPhone (no auto-open)" ;;
  *) echo "viewopen: symlink me as gopen | sopen | mopen | iopen" >&2; exit 1 ;;
esac

usage() {
  cat <<EOF
$SELF — $DESC.
VIEW-family, Atlas-portal-aware (see scripts/lib/open-core.sh).

Usage: $SELF [OPTIONS] <file|dir|registry-code>

Options:
EOF
  [[ "$MODE" == browser ]] && echo "  -q, --quiet    Suppress output"
  cat <<EOF
  -h, --help     Show this help
  -v, --version  Show version

Env: ATLAS_BASE_URL, OPEN_MAC_HOST — see scripts/lib/open-core.sh.
EOF
}

QUIET=0; POSITIONAL=()
while [[ $# -gt 0 ]]; do
  case "$1" in
    -h|--help)    usage; exit 0 ;;
    -v|--version) echo "$SELF $VERSION"; exit 0 ;;
    -q|--quiet)
      if [[ "$MODE" == browser ]]; then QUIET=1; shift
      else echo "$SELF: unknown option $1" >&2; exit 1; fi ;;
    -*)           echo "$SELF: unknown option $1" >&2; exit 1 ;;
    *)            POSITIONAL+=("$1"); shift ;;
  esac
done

[[ ${#POSITIONAL[@]} -eq 0 ]] && { echo "$SELF: missing file argument (try --help)" >&2; exit 1; }

# On macOS, we ARE the Mac — force-launch the browser directly, no Tailscale
# hop. Notify mode has no Darwin shortcut (a notification link is still wanted).
if [[ "$MODE" == browser && "$(uname)" == "Darwin" ]]; then
  exec open -a "$DARWIN_APP" "${POSITIONAL[0]}"
fi

open_core_resolve_target "${POSITIONAL[0]}" || exit 1

if [[ "$MODE" == browser ]]; then
  open_core_resolve_url "$OPEN_RESOLVED_PATH" "$OPEN_RESOLVED_IS_DIR" 1
  [[ $QUIET -eq 0 ]] && {
    if [[ "$OPEN_VIA" == "atlas" ]]; then
      echo "$SELF: '$OPEN_RESOLVED_PATH' → Atlas (portal-indexed)"
    else
      echo "$SELF: mount → $OPEN_RESOLVED_PATH"
    fi
    echo "       → ${OPEN_URL}"
  }
  open_core_dispatch_browser "$OPEN_URL" "$OPEN_URL_PREFIX" "$DISPATCH"
else
  # Notification link is a one-shot — no live-reload watcher (arg 3 = 0).
  open_core_resolve_url "$OPEN_RESOLVED_PATH" "$OPEN_RESOLVED_IS_DIR" 0
  # nx-send.sh is owned by cc; degrade gracefully if absent.
  NX_SEND="${HOME}/.claude/scripts/lib/nx-send.sh"
  if [[ -f "$NX_SEND" ]]; then
    # shellcheck source=/dev/null
    . "$NX_SEND"
    command -v "$NX_FN" >/dev/null 2>&1 && "$NX_FN" "$OPEN_URL"
  else
    echo "$SELF: warning: nx-send.sh not found at $NX_SEND — notification skipped" >&2
  fi
  echo "$OPEN_URL"
fi
