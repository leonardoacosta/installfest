#!/usr/bin/env bash
# cc-tmux.tmux — tmux-side plugin body (Req-11, task 1.12).
#
# Loaded via `run-shell` from tmux.conf (the `if-shell` presence guard that wraps
# this load line is applied by the Config batch in tmux.conf, mirroring the
# tmux-which-key precedent). This file:
#   * resolves its own dir + the bundled `bin/cc-tmux` CLI,
#   * binds the keybindings (all overridable via @cc-*-key options),
#   * (status rows are wired by tmux.conf status-format slots, not here),
#   * auto-discovers already-running Claude sessions on load.
#
# It doubles as its own display-menu helper: invoked as `cc-tmux.tmux __menu
# <inbox|picker-data>` (from a keybinding on tmux < 3.2 or without fzf) it builds
# and shows a `display-menu` fallback. Fail open: if the CLI is missing, exit 0.

set -eu

# ---------------------------------------------------------------------------
# Resolve this script's real directory (through chezmoi symlinks) + the CLI.
# ---------------------------------------------------------------------------
_self="${BASH_SOURCE[0]}"
while [ -h "$_self" ]; do
  _dir="$(cd -P "$(dirname "$_self")" >/dev/null 2>&1 && pwd)"
  _self="$(readlink "$_self")"
  case "$_self" in
    /*) ;;
    *) _self="$_dir/$_self" ;;
  esac
done
CURRENT_DIR="$(cd -P "$(dirname "$_self")" >/dev/null 2>&1 && pwd)"
CMD="$CURRENT_DIR/bin/cc-tmux"

# Fail open: no CLI -> nothing to wire.
[ -x "$CMD" ] || exit 0

# ---------------------------------------------------------------------------
# display-menu fallback mode (invoked from a keybinding on old / fzf-less tmux).
# ---------------------------------------------------------------------------
if [ "${1:-}" = "__menu" ]; then
  kind="${2:-inbox}"
  menu_args=()
  while IFS=$'\t' read -r label pid; do
    [ -n "$pid" ] || continue
    menu_args+=("$label" "" "run-shell \"$CMD switch --pane $pid\"")
  done < <("$CMD" "$kind" 2>/dev/null)
  if [ "${#menu_args[@]}" -gt 0 ]; then
    tmux display-menu -T "cc-tmux ${kind}" "${menu_args[@]}"
  else
    tmux display-message "cc-tmux: no tracked panes"
  fi
  exit 0
fi

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
# _opt <option> <default>  — read a global tmux option, falling back to default.
_opt() {
  local val
  val="$(tmux show-option -gqv "$1" 2>/dev/null || true)"
  if [ -n "$val" ]; then printf '%s' "$val"; else printf '%s' "$2"; fi
}

# supports_popup — true when fzf is present and tmux is >= 3.2 (display-popup).
supports_popup() {
  command -v fzf >/dev/null 2>&1 || return 1
  local ver
  ver="$(tmux -V 2>/dev/null | sed 's/[^0-9.]*//g')"
  awk -v v="$ver" 'BEGIN {
    n = split(v, a, ".");
    major = a[1] + 0; minor = (n >= 2 ? a[2] + 0 : 0);
    if (major > 3 || (major == 3 && minor >= 2)) exit 0;
    exit 1;
  }'
}

# ---------------------------------------------------------------------------
# Keybindings (defaults overridable via @cc-*-key).
# ---------------------------------------------------------------------------
cycle_key="$(_opt @cc-cycle-key o)"          # prefix + o  (not Space: which-key owns Space)
picker_key="$(_opt @cc-picker-key C-f)"      # prefix + C-f
inbox_key="$(_opt @cc-inbox-key i)"          # prefix + i
back_key="$(_opt @cc-back-key C-Space)"      # C-Space (root table, no prefix)

# Priority cycle + jump-back (plain run-shell, no popup needed).
tmux bind-key "$cycle_key" run-shell "$CMD cycle"
tmux bind-key -n "$back_key" run-shell "$CMD back"

if supports_popup; then
  # fzf popup: --with-nth=1 shows only the aligned label; field 2 is the pane id.
  # --preview renders the highlighted pane's live tail in a right-side panel
  # (field 2 = {2} drives it); enter still switches to that pane.
  tmux bind-key "$picker_key" display-popup -E \
    "$CMD picker-data | fzf --delimiter='\\t' --with-nth=1 --reverse --prompt='jump> ' --preview 'tmux capture-pane -ep -t {2} | tail -40' --preview-window='right:55%:wrap' | cut -f2 | xargs -I{} $CMD switch --pane {}"
  # inbox popup: ctrl-x dismisses (view filter) and reloads; enter switches.
  tmux bind-key "$inbox_key" display-popup -E \
    "$CMD inbox | fzf --delimiter='\\t' --with-nth=1 --reverse --prompt='inbox> ' --preview 'tmux capture-pane -ep -t {2} | tail -40' --preview-window='right:55%:wrap' --bind 'ctrl-x:execute-silent($CMD inbox-clear)+reload($CMD inbox)' | cut -f2 | xargs -I{} $CMD switch --pane {}"
else
  # Fallback: this script's own __menu mode builds a display-menu.
  tmux bind-key "$picker_key" run-shell "'$CURRENT_DIR/cc-tmux.tmux' __menu picker-data"
  tmux bind-key "$inbox_key" run-shell "'$CURRENT_DIR/cc-tmux.tmux' __menu inbox"
fi

# ---------------------------------------------------------------------------
# Account-switcher popup click (cc-tmux-account-switcher-popup, dismiss UX
# hardened by cc-tmux-accounts-popup-click-dismiss) — row 2's account-label
# segment carries a #[range=user|accounts] marker (render.render_session_bar).
# ALL status-bar ranges (user/session/window/pane) share the single
# MouseDown1Status key — there is no distinct per-range binding on this tmux
# version (3.6a, confirmed via the original task 1.1 spike) — so the specific
# range is read at click time from #{mouse_status_range}. Falls through to
# tmux's own default action (switch-client -t =) for every range OTHER than
# "accounts", which is what keeps cmd_status_inbox's existing
# #[range=pane|<id>] click-to-hop behavior (relying on that same implicit
# default) working unchanged. Positioned via `-y S -x M` (immediately above
# the status line, at the mouse's x position — the original spike's other
# confirmation).
#
# Dismiss mechanism (cc-tmux-accounts-popup-click-dismiss task 1.1 spike,
# confirmed live 2026-07-13 on tmux 3.6a): `display-popup` itself has NO
# native mouse-click dismissal — per tmux(1), only Escape/C-c or `-k` (any
# key) close it; there is no popup-internal click-target primitive. Real
# click-to-close comes from routing through fzf instead (same
# supports_popup-gated pattern the picker/inbox popups above already use —
# reuse, not a new mechanism): `--bind 'click-header:abort'` gives a genuine
# clickable close on the rendered `[x]` header text (verified: fzf accepts
# this bind syntax; an invalid action name is rejected with a distinct
# parse error, confirming the syntax is real, not silently ignored).
# `--no-input` hides and disables the query box entirely — the popup cannot
# be typed into, not just "one keystroke closes it" as the prior
# `read -n 1 -s` mechanism left ambiguous-looking (a blinking cursor that
# cosmetically read as an editable field). `--bind 'enter:ignore'` and
# `--bind 'left-click:ignore'` keep row clicks/Enter inert — read-only popup,
# no per-account switch action (proposal Non-Goals; nexus-agent has no
# `POST /credentials/swap` endpoint to switch to regardless — confirmed 404
# live). Falls back to the pre-existing static `read -n 1 -s` popup
# (any-keystroke dismiss) when fzf/tmux-3.2+ isn't available.
#
# `--header-border` (2026-07-13, Leo's request): draws a separator line
# between the `[x]` header and the account list so the close affordance
# reads as attached to the popup frame rather than a floating content line.
# Considered `--border-label` instead (renders literally ON the border) but
# fzf has NO click-bind event for a border label at all — only `click-header`/
# `click-footer` (content-area rows) support clicks. Moving `[x]` there would
# have made it look better but stop being clickable, which defeats the
# feature; Leo confirmed keeping the real click target over the cosmetic
# border placement.
# ---------------------------------------------------------------------------
if supports_popup; then
  accounts_popup_cmd="display-popup -y S -x M -E \"$CMD accounts-popup | fzf --no-input --header-border --header='[x] click here or press q to close' --prompt='' --pointer=' ' --bind 'click-header:abort' --bind 'q:abort' --bind 'enter:ignore' --bind 'left-click:ignore'\""
else
  accounts_popup_cmd="display-popup -y S -x M -E \"$CMD accounts-popup; read -n 1 -s\""
fi
tmux bind-key -T root MouseDown1Status if-shell -F '#{==:#{mouse_status_range},accounts}' \
  "$accounts_popup_cmd" \
  "switch-client -t ="

# ---------------------------------------------------------------------------
# Conductor keys — ONLY when @cc-conductor-enabled is on (conductor.py is owned
# by another engineer; we just wire the guarded bindings to its CLI).
# ---------------------------------------------------------------------------
case "$(_opt @cc-conductor-enabled off)" in
  on | 1 | true | yes)
    cond_key="$(_opt @cc-conductor-key y)"
    cond_respawn_key="$(_opt @cc-conductor-respawn-key Y)"
    tmux bind-key "$cond_key" run-shell "$CMD conductor --popup"
    tmux bind-key "$cond_respawn_key" run-shell "$CMD conductor --popup --respawn"
    ;;
esac

# ---------------------------------------------------------------------------
# One-shot discover of already-running Claude sessions (see bottom of file).
# ---------------------------------------------------------------------------

# ---------------------------------------------------------------------------
# MRU focus tracking — stamp @cc-visited on pane focus (recency tiebreak).
# The fixed hook slot [9909] is idempotent (overwrites itself on every reload)
# and coexists with any user-owned bare `pane-focus-in` hook. Opt out entirely
# via `@cc-track-focus off`, which unsets the slot.
# ---------------------------------------------------------------------------
case "$(_opt @cc-track-focus on)" in
  off | 0 | false | no)
    tmux set-hook -gu 'pane-focus-in[9909]'
    ;;
  *)
    tmux set-hook -g 'pane-focus-in[9909]' "run-shell -b '$CMD focus #{pane_id}'"
    ;;
esac

"$CMD" discover --quiet >/dev/null 2>&1 &
