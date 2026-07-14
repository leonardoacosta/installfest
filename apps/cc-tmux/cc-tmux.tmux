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
#
# `-h 80%` (2026-07-13, Leo's report): each account's body grew from one
# line to up to four (summary + 5H/7D reset lines + a closing `─` rule,
# render.render_accounts_popup) after the reset-time feature landed, but the
# popup kept relying on `display-popup`'s unspecified-height default (50% of
# the client, per tmux(1)) sized for the OLD one-line-per-account content.
# With 3+ tracked accounts the body now regularly exceeds that, and fzf
# scrolls its list to fit — the top account's summary row scrolls out of
# view before the user ever touches a key, reading as "broken", not "just
# scroll up". A fixed generous percentage (not a dynamic `#()`-computed
# line count) is deliberate: this bind-key string is already a deeply
# nested, multiply-quoted one-liner (rules/TOOLING.md's RTK
# quoted-command-token footgun is the same class of fragility), and tmux
# clamps any requested popup size to the actual client dimensions anyway, so
# a generous fixed percentage is both simpler and just as safe as computing
# an exact line count live. Applied to both the fzf and the plain-fallback
# branch — the same overflow applies to the `read -n 1 -s` popup too.
#
# `fzf --height=100%` (2026-07-14, cc-tmux-status-bar-popup-polish task 3.4,
# beads if-s1yu): even with the above `-h 80%`, Leo confirmed live that the
# rendered account list still truncates to roughly HALF the popup pane's
# actual height — i.e. the outer `display-popup` dimension is not the
# bottleneck here (already generous, already clamped to the real client size
# by tmux). The remaining gap is fzf's OWN height auto-detection: with no
# explicit `--height`, fzf tries to size itself from the controlling
# terminal, and that detection is known to be unreliable inside a
# `display-popup -E`-spawned pty (stale `$LINES`/`$COLUMNS` at fzf's startup,
# possibly compounded by `--header-border` reserving rows fzf doesn't
# re-measure against). `--height=100%` stops fzf from guessing and forces it
# to fill 100% of whatever pty it's actually handed — which is already the
# full 80%-of-client popup from the flag above. This is fix (a) from
# design.md Decision 5 only; per that doc's own reasoning ("the outer tmux
# popup dimension is not the bottleneck"), bumping `-h` further (fix (b))
# would grow the box without addressing the real cause, so it is NOT applied
# here. Still needs a real attached-tmux-client check (task 4.5) — this
# reasoning is static, not an observed fix.
# ---------------------------------------------------------------------------
#
# 2026-07-14 FOLLOW-UP (cc-tmux-status-bar-popup-polish task 3.4 correction,
# post-4.5 live verification): the `--height=100%` fix above was the right
# diagnosis (fzf's own auto-height detection is unreliable in a
# `display-popup -E`-spawned pty) but the wrong-shaped remedy — with only 3
# real accounts (~5 lines each) it now renders a tall, mostly-empty box.
# Fixed by computing an ABSOLUTE line count from the real
# `$CMD accounts-popup` output (`wc -l` on a throwaway invocation) instead of
# filling whatever pty fzf is handed. Overhead margin of 6 was measured
# empirically (isolated tmux test session, this fzf build, 0.71.0): the fixed
# chrome around the list is exactly 5 rows (2 for `display-popup`'s own
# outer border + 3 for `--header-border`'s inner header box: top rule,
# header text, bottom rule) with `--no-input` hiding the extra info/prompt
# row a non-`--no-input` invocation would otherwise add — +1 on top of that
# measured 5 is a safety cushion for cross-version/cross-terminal variance,
# not a guess. The outer `-h 80%` stays as a generous UPPER BOUND per the
# note above (tmux already clamps it to the real client size, and an
# absolute `fzf --height` larger than the pty it's given is itself clamped
# down automatically — verified empirically, so an oversized computed value
# degrades safely instead of erroring). `$CMD accounts-popup` runs twice
# (once piped to `wc -l`, once piped to `fzf`) rather than capturing its
# output once into a shell variable and re-quoting that variable through
# this already deeply-nested, multiply-escaped one-liner (rules/TOOLING.md's
# RTK quoted-command-token footgun is the same fragility class) — the extra
# invocation is cheap (local status read, no network) and "simpler, just as
# safe" is the same tradeoff call the `-h 80%` note above already made.
#
# Second bug found in the SAME live pass: fzf draws a persistent gutter
# glyph on every row EXCEPT the current one (the current row's `--pointer`
# glyph occupies that same column instead) — confirmed empirically by
# diffing rendered output with/without a `--gutter=' '` override. This is
# NOT a background-color highlight (a `--color` override on `bg+`/`fg+`
# alone does not remove it, since it is a drawn character, not a fill) — it
# is what actually produced the "looks selectable/scrollable" appearance the
# click-binds already disprove behaviorally. Fixed by blanking `--gutter` to
# match the already-blanked `--pointer`. `--color='fg+:-1,bg+:-1,gutter:-1'`
# is kept alongside as defense-in-depth (belt-and-suspenders per the
# dispatch), and every cursor-moving bind this fzf build (0.71.0) exposes
# via `--bind` — arrow up/down, `ctrl-j`/`ctrl-k` (down/up), `ctrl-n`/
# `ctrl-p` (down-match/up-match), `page-up`/`page-down` — is bound to
# `ignore`, mirroring the existing `left-click:ignore`/`enter:ignore`
# pattern (`j`/`k` bare letters are not bound to navigation by default in
# this fzf build with no vim-mode, and are already inert under
# `--no-input`, so no bind is needed for them). `--no-scrollbar` is also
# added (supported by this fzf build) so no residual scroll affordance
# survives even at the outer `-h 80%` clamp boundary.
# ---------------------------------------------------------------------------
#
# 2026-07-14 SECOND FOLLOW-UP (cc-tmux-status-bar-popup-polish task 3.4
# correction #2, post-live-testing-on-a-real-attached-client): the fix above
# correctly sized fzf's OWN box (Leo confirmed live: no more truncation, no
# more fake highlight/gutter) but left a NEW gap the live test surfaced — the
# SURROUNDING `display-popup -h 80%` floating pane is still a fixed 80% of
# the screen, much taller than fzf's now-small content box, leaving a large
# blank region between the bottom of the account list and the popup's own
# border. The `-h 80%` note two paragraphs up ("tmux already clamps it to
# the real client size") is true of a PERCENTAGE `-h` but does not mean the
# percentage itself shrinks to fit content — it never did; that was always
# fzf's job for its OWN box, and the outer pane was simply never touched.
#
# Root cause: `display-popup -h` sizes the FLOATING PANE itself, evaluated
# once when this bind-key's command string is built (tmux plugin LOAD time,
# i.e. right here, right now) — completely independent of whatever fzf does
# inside that pane at CLICK time. Making the outer `-h` dynamic requires a
# computation that runs AFTER this script has already returned, i.e. at
# click time, before `display-popup` itself is invoked — which necessarily
# means one more layer of indirection between the keybinding and the popup.
#
# Fix: delegate the whole fzf-popup launch to a NEW Python subcommand,
# `cc-tmux accounts-popup-launch` (`cli.cmd_accounts_popup_launch`), instead
# of building the `display-popup ...` string here. That subcommand computes
# the real content-line count IN-PROCESS (via the same body-builder
# `cmd_accounts_popup` prints, `_accounts_popup_body`, refactored out so both
# share it — no new subprocess needed for this half) and calls
# `tmux display-popup` itself with a freshly-computed `-h`, mirroring
# `conductor.py`'s `_popup()` (an established precedent for a subcommand
# invoking `display-popup` directly). This was picked over reconstructing
# the ENTIRE inner `-E` string inside a `run-shell` bash wrapper right here —
# that would have needed a THIRD level of shell-in-shell-in-shell escaping on
# top of the two this one-liner already has, exactly the RTK
# quoted-command-token fragility class this file's own comments keep
# flagging. Delegating to Python instead means this binding shrinks to a
# single `run-shell` call with zero nested quoting.
#
# Verified live (throwaway tmux 3.6a / fzf 0.71.0 server, no real attached
# client involved in production — the popup mechanism itself requires one,
# so a `script`/`pty.fork`-attached synthetic client stood in): `display-popup
# -h N` grants its `-E` command's own pty exactly `N - 2` rows (2-row border
# overhead, confirmed for six different requested heights). Computing outer
# `-h` as `content_lines + 6 (fzf's existing margin) + 2 (this border
# overhead)` therefore hands fzf a pty of EXACTLY the height it already
# needs. Measured two content-line counts end to end: 5 lines -> computed
# `-h 13` -> real popup pty `stty size` = 11 rows (= 5 + 6, exactly fzf's own
# required height, zero slack); 19 lines -> computed `-h 27` -> real popup
# pty `stty size` = 25 rows (= 19 + 6, same exact match). A direct
# `tmux capture-pane` of fzf rendering that same content in a pane resized
# to those exact heights (5, 12, and 19 content lines tested) showed every
# row visible, zero truncation, and only one small structural pad row below
# the list — proportionate chrome, not the dead-space gap this fix closes.
# See `cli.cmd_accounts_popup_launch`'s docstring for the full margin
# breakdown and the client-height clamp (absolute `-h` values do NOT
# self-clamp like a percentage does — confirmed live, `-h 27` against an
# 80x24 client errored "height too large" outright rather than shrinking, so
# the Python side clamps against `#{client_height}` explicitly).
# ---------------------------------------------------------------------------
if supports_popup; then
  accounts_popup_cmd="run-shell \"$CMD accounts-popup-launch\""
else
  # Plain fallback: no fzf, no inner box — `$CMD accounts-popup` writes
  # directly to the popup pane and `read -n 1 -s` just waits for a keypress.
  # There is no second sizing computation in this path (no auto-detected
  # inner height to get wrong), so the fzf `--height` truncation bug above
  # has no analogue here — left unchanged.
  accounts_popup_cmd="display-popup -y S -x M -h 80% -E \"$CMD accounts-popup; read -n 1 -s\""
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
