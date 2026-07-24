#!/usr/bin/env bash
# tmux-sidebar-open.sh — ensure tmux-sidebar's tree pane is OPEN for the calling
# pane. Idempotent: re-running never closes an already-open sidebar.
#
# tmux-sidebar only ships a toggle (scripts/toggle.sh, bound to prefix+Tab), so
# calling it blindly from a SessionStart hook would CLOSE the sidebar whenever a
# session starts in a pane that already has one (resume/clear/compact all fire
# SessionStart). This wrapper does the has-sidebar check first and delegates the
# actual open to the plugin — the pane-splitting, width restore, and
# registration bookkeeping all stay owned by the plugin.
#
# The open path reuses the exact argument string the plugin computed for its own
# prefix+Tab binding (`@sidebar-key-<key>`, written by sidebar.tmux at load), so
# the sidebar this opens is byte-identical to a hand-toggled one and inherits
# every @sidebar-* option without this script re-reading any of them.
#
# No-ops cleanly (exit 0) outside tmux, before `chezmoi apply` has cloned the
# plugin, or on a tmux too old for it.

set -euo pipefail

[ -n "${TMUX:-}" ] || exit 0
command -v tmux >/dev/null 2>&1 || exit 0

PLUGIN_DIR="$HOME/.tmux/plugins/tmux-sidebar"
[ -x "$PLUGIN_DIR/scripts/toggle.sh" ] || exit 0

# shellcheck source=/dev/null
. "$PLUGIN_DIR/scripts/helpers.sh"
# shellcheck source=/dev/null
. "$PLUGIN_DIR/scripts/variables.sh"

# $TMUX_PANE is exported per-pane by tmux and inherited by anything the pane
# spawns (including a Claude Code hook subprocess). The display-message fallback
# is best-effort only: without -t it resolves the attached client's current
# pane, which is not necessarily this one.
PANE_ID="${TMUX_PANE:-$(tmux display-message -p '#{pane_id}')}"

# Same check as toggle.sh's has_sidebar(): a live registration whose recorded
# sidebar pane still exists.
registration="$(get_tmux_option "${REGISTERED_PANE_PREFIX}-${PANE_ID}" "")"
if [ -n "$registration" ]; then
  sidebar_pane_id="${registration%%,*}"
  if tmux list-panes -F '#{pane_id}' | grep -qx -- "$sidebar_pane_id"; then
    exit 0
  fi
fi

tree_key="$(get_tmux_option "$TREE_OPTION" "$TREE_KEY")"
toggle_args="$(get_tmux_option "${VAR_KEY_PREFIX}-${tree_key}" "")"
[ -n "$toggle_args" ] || exit 0   # sidebar.tmux has not been sourced yet

exec "$PLUGIN_DIR/scripts/toggle.sh" "$toggle_args" "$PANE_ID"
