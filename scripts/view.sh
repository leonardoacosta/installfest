#!/usr/bin/env bash
# view.sh — the single front door for "render this file optimally" from a
# terminal session. Picks the right renderer for the file type and shows it in
# a horizontal tmux split below the current pane (inferring the session from the
# inherited $TMUX), so the working pane is never disrupted.
#
# Session inference is free: a command run inside a tmux pane (including one
# spawned by Claude Code's Bash tool) inherits $TMUX/$TMUX_PANE, so
# `tmux split-window` targets the current pane with no client querying.
#
# Everything ends in a pager — `glow -p` / `bat --paging=always` page even short
# files — so pressing `q` exits the renderer, its command exits, and tmux closes
# the pane. That IS the close mechanism; there is no cleanup code.
#
# One dispatcher, three renderers — picked by the file's type:
#   * .md / .markdown / .mdx  -> glow -p          (rendered markdown; bat fallback)
#   * .html / .htm            -> mac-open          (real Mac browser; never a split)
#   * everything else         -> bat --paging=always --color=always
#
# Three execution contexts — picked by $TMUX and a stdout-TTY check:
#   * In tmux            -> horizontal split below the current pane (reused if one
#                           was already opened by a previous `view`)
#   * No tmux, TTY out   -> inline paged render in the current pane
#   * No tmux, piped out -> inline plain render to stdout (the CI/smoke path)
# HTML always routes to mac-open regardless of context.
#
# Usage:
#   view.sh [-d] <file>     render <file> optimally
#     -d                    keep focus on the calling pane (passes -d to the split)
#
# Env:
#   VIEW_SPLIT_SIZE         tmux split size for the viewer pane  (default: 60%)

set -uo pipefail

readonly SPLIT_SIZE="${VIEW_SPLIT_SIZE:-60%}"

usage() {
  cat >&2 <<'EOF'
view — render a file optimally for its type

Usage:
  view [-d] <file>

Options:
  -d    keep focus on the calling pane (do not switch to the viewer)

Type dispatch:
  .md / .markdown / .mdx   -> glow (rendered markdown; bat fallback)
  .html / .htm             -> mac-open (real Mac browser, no split)
  everything else          -> bat (syntax-highlighted)
EOF
}

# --- Arg parsing -------------------------------------------------------------
detach=""        # "-d" keeps focus on the caller pane
target=""
while [ $# -gt 0 ]; do
  case "$1" in
    -d)            detach="-d" ;;
    -h|--help)     usage; exit 0 ;;
    --)            shift; [ -n "$1" ] && target="$1"; break ;;
    -*)            echo "view: unknown option: $1" >&2; usage; exit 1 ;;
    *)             [ -z "$target" ] && target="$1" ;;
  esac
  shift
done

[ -z "$target" ] && { echo "view: no file given" >&2; usage; exit 1; }

# --- Resolve to an absolute, readable path -----------------------------------
file="$(realpath -- "$target" 2>/dev/null || true)"
if [ -z "$file" ] || [ ! -e "$file" ]; then
  echo "view: no such file: $target" >&2
  exit 1
fi
if [ ! -r "$file" ]; then
  echo "view: file is not readable: $file" >&2
  exit 1
fi

# --- Type detection (extension-first, file --mime-type for extensionless) ----
# Lower-case the extension so .MD / .Html resolve the same as .md / .html.
ext=""
base="${file##*/}"
case "$base" in
  *.*) ext="$(printf '%s' "${base##*.}" | tr '[:upper:]' '[:lower:]')" ;;
esac

filetype="code"   # code | markdown | html
case "$ext" in
  md|markdown|mdx) filetype="markdown" ;;
  html|htm)        filetype="html" ;;
  "")
    # Extensionless: consult the mime type.
    mime="$(file --mime-type -b -- "$file" 2>/dev/null || true)"
    case "$mime" in
      text/markdown) filetype="markdown" ;;
      text/html)     filetype="html" ;;
      *)             filetype="code" ;;
    esac
    ;;
  *) filetype="code" ;;
esac

# --- HTML: hand off to the Mac browser, never a split ------------------------
if [ "$filetype" = "html" ]; then
  if command -v mac-open >/dev/null 2>&1; then
    exec mac-open "$file"
  fi
  echo "view: mac-open not found; cannot render HTML" >&2
  exit 1
fi

# --- Build the renderer command for the detected type ------------------------
# `paged` is the renderer used in a split / interactive pane (ends in a pager).
# `plain` is the renderer used for piped, non-interactive stdout (no pager).
have() { command -v "$1" >/dev/null 2>&1; }

paged_renderer() {
  if [ "$filetype" = "markdown" ]; then
    if have glow; then
      printf 'glow -p %s' "$(printf "%q" "$file")"
      return 0
    fi
    echo "view: glow not found — falling back to bat for markdown" >&2
  fi
  printf 'bat --paging=always --color=always %s' "$(printf "%q" "$file")"
}

render_plain() {
  # Non-paged render straight to the current stdout (the piped / CI path).
  if [ "$filetype" = "markdown" ]; then
    if have glow; then
      exec glow -- "$file"
    fi
    echo "view: glow not found — falling back to bat for markdown" >&2
  fi
  if have bat; then
    exec bat --color=auto --paging=never --style=plain -- "$file"
  fi
  # Last resort if neither renderer is installed.
  exec cat -- "$file"
}

render_inline_paged() {
  # Paged render in the current interactive pane (no tmux).
  if [ "$filetype" = "markdown" ]; then
    if have glow; then
      exec glow -p -- "$file"
    fi
    echo "view: glow not found — falling back to bat for markdown" >&2
  fi
  if have bat; then
    exec bat --paging=always --color=always -- "$file"
  fi
  exec cat -- "$file"
}

# --- Execution context dispatch ----------------------------------------------
if [ -n "${TMUX:-}" ] && have tmux; then
  cmd="$(paged_renderer)"

  # Reuse a viewer pane opened by a previous `view` call in this window.
  existing="$(tmux list-panes -F '#{pane_id} #{@view_pane}' 2>/dev/null \
    | awk '$2==1{print $1; exit}')"

  if [ -n "$existing" ]; then
    tmux respawn-pane -k -t "$existing" "$cmd"
    [ -z "$detach" ] && tmux select-pane -t "$existing" 2>/dev/null || true
  else
    pane="$(tmux split-window -v -l "$SPLIT_SIZE" $detach -P -F '#{pane_id}' \
      -c "#{pane_current_path}" "$cmd")"
    [ -n "$pane" ] && tmux set -p -t "$pane" @view_pane 1 2>/dev/null || true
  fi
  exit 0
fi

if [ -t 1 ]; then
  # Interactive terminal, not in tmux: render inline, paged.
  render_inline_paged
fi

# Non-interactive / piped: render inline, non-paged, to stdout.
render_plain
