#!/usr/bin/env bash
# view.sh — the single front door for "render this file optimally" from a
# terminal session. Picks the right renderer for the file type and shows it in
# a tmux split below the current pane by default (inferring the session from
# the inherited $TMUX), so the working pane is never disrupted. -h splits
# side-by-side instead; -v is the pane-below default, made explicit.
#
# Session inference is free: a command run inside a tmux pane (including one
# spawned by Claude Code's Bash tool) inherits $TMUX/$TMUX_PANE, so
# `tmux split-window` targets the current pane with no client querying.
#
# Everything ends in a pager (or an equivalent "press a key to close" hold for
# chafa's image renderer, which doesn't page on its own) — so pressing `q` (or
# any key for images) exits the renderer, its command exits, and tmux closes
# the pane. That IS the close mechanism; there is no cleanup code.
#
# Dispatch table (v1 + v2), picked by the target's type:
#   * .md / .markdown / .mdx        -> glow -p          (rendered markdown; bat fallback)
#   * .html / .htm                  -> mac-open          (real Mac browser; never a split)
#   * .pdf / other binary content   -> mac-open          (never a split)          [v2]
#   * image (png/jpg/gif/webp/...)  -> chafa -f kitty     (true raster via tmux    [v2]
#                                       --passthrough tmux passthrough; half-block
#                                                          fallback when passthrough
#                                                          is off; direct kitty when
#                                                          not in tmux — Ghostty
#                                                          decodes Kitty natively)
#   * directory                     -> eza --tree | pager                          [v2]
#   * everything else (code/text)   -> bat --paging=always --color=always
#
# Multi-file (v2): `view a.md b.txt c.md` classifies each target, concatenates
# all non-markdown targets into ONE bat call (bat's native multi-file concat —
# single pager session, file-name headers between them), then runs glow
# sequentially (one call per file, `q` advances to the next) for the markdown
# targets. Code group renders before the markdown group. Multi-file mode only
# supports markdown/code targets — mixing in an image/pdf/binary/directory
# alongside another file is rejected (those types have no sane "concatenated"
# rendering and are single-target dispatches by design). Any html targets are
# peeled off first and opened individually via mac-open regardless of what
# else was passed.
#
# Three execution contexts — picked by $TMUX and a stdout-TTY check:
#   * In tmux            -> horizontal split below the current pane (reused if one
#                           was already opened by a previous `view` call)
#   * No tmux, TTY out   -> inline paged render in the current pane
#   * No tmux, piped out -> inline plain render to stdout (the CI/smoke path)
# HTML/PDF/binary always route to mac-open regardless of context, never a split.
#
# Usage:
#   view.sh [-d] [-h|-v] <file> [<file> ...]   render one or more files optimally
#     -d                                        keep focus on the calling pane (passes -d to the split)
#     -h                                        split side-by-side (tmux split-window -h)
#     -v                                        split pane-below (tmux split-window -v) — the default
#
# Env:
#   VIEW_SPLIT_SIZE         tmux split size for the viewer pane  (default: 60%)

set -uo pipefail

readonly SPLIT_SIZE="${VIEW_SPLIT_SIZE:-60%}"

usage() {
  cat >&2 <<'EOF'
view — render one or more files optimally for their type

Usage:
  view [-d] [-h|-v] <file> [<file> ...]

Options:
  -d    keep focus on the calling pane (do not switch to the viewer)
  -h    split side-by-side (tmux split-window -h)
  -v    split pane-below (tmux split-window -v) — the default
        (--help for this usage text; there is no bare -h help shorthand,
        since -h is the horizontal-split flag)

Type dispatch:
  .md / .markdown / .mdx   -> glow (rendered markdown; bat fallback)
  .html / .htm             -> mac-open (real Mac browser, no split)
  .pdf / binary content    -> mac-open (real Mac browser, no split)
  image (png/jpg/gif/...)  -> chafa -f kitty (true raster; half-block fallback)
  directory                -> eza --tree, paged
  everything else          -> bat (syntax-highlighted)

Multiple files: markdown/code targets only. Non-markdown targets concatenate
into one bat pager session; markdown targets render sequentially via glow.
EOF
}

have() { command -v "$1" >/dev/null 2>&1; }

# --- Arg parsing (multi-target) ----------------------------------------------
detach=""        # "-d" keeps focus on the caller pane
split_dir="v"    # "-h"/"-v" -> tmux split-window -h/-v; -v (pane below) is the v1 default
targets=()
while [ $# -gt 0 ]; do
  case "$1" in
    -d)            detach="-d" ;;
    -h)            split_dir="h" ;;
    -v)            split_dir="v" ;;
    --help)        usage; exit 0 ;;
    --)            shift; targets+=("$@"); break ;;
    -*)            echo "view: unknown option: $1" >&2; usage; exit 1 ;;
    *)             targets+=("$1") ;;
  esac
  shift
done

[ "${#targets[@]}" -eq 0 ] && { echo "view: no file given" >&2; usage; exit 1; }

# --- Resolve each target to an absolute, readable path ------------------------
resolved=()
for t in "${targets[@]}"; do
  f="$(realpath -- "$t" 2>/dev/null || true)"
  if [ -z "$f" ] || [ ! -e "$f" ]; then
    echo "view: no such file: $t" >&2
    exit 1
  fi
  if [ ! -r "$f" ]; then
    echo "view: file is not readable: $f" >&2
    exit 1
  fi
  resolved+=("$f")
done

# --- Type detection (extension-first, file --mime-type fallback) -------------
# Directories are classified before any extension parsing (a directory name
# may contain a dot; extension logic doesn't apply). Known extensions map
# directly. An unknown/empty extension falls back to a mime-type sniff so a
# binary file with no recognizable extension routes to mac-open instead of
# getting piped through bat as garbage (if-sj7) — known extensions are NOT
# re-sniffed here, so existing text/config files (.json, .toml, .py, ...)
# keep their established v1 "code" classification unchanged.
classify() {
  local f="$1" base ext mime
  if [ -d "$f" ]; then
    echo "directory"
    return 0
  fi
  base="${f##*/}"
  ext=""
  case "$base" in
    *.*) ext="$(printf '%s' "${base##*.}" | tr '[:upper:]' '[:lower:]')" ;;
  esac
  case "$ext" in
    md|markdown|mdx)                     echo "markdown"; return 0 ;;
    html|htm)                            echo "html"; return 0 ;;
    pdf)                                 echo "pdf"; return 0 ;;
    png|jpg|jpeg|gif|webp|bmp|tif|tiff)  echo "image"; return 0 ;;
    # Curated common binary-container extensions -- a deliberately bounded
    # list (not a mime re-sniff of every known extension) so text/config
    # files that happen to have an unrecognized extension (.json, .toml, a
    # dotfile) keep their established v1 "code" classification unchanged.
    bin|exe|dll|so|dylib|a|o|obj|class|jar|whl|wasm| \
    zip|tar|gz|tgz|bz2|xz|7z|rar|dmg|iso|deb|rpm|pkg)
      echo "binary"; return 0 ;;
    "")
      mime="$(file --mime-type -b -- "$f" 2>/dev/null || true)"
      case "$mime" in
        text/markdown)          echo "markdown" ;;
        text/html)              echo "html" ;;
        application/pdf)        echo "pdf" ;;
        image/*)                echo "image" ;;
        text/*|inode/x-empty)   echo "code" ;;
        *)                      echo "binary" ;;
      esac
      return 0
      ;;
    *) echo "code"; return 0 ;;
  esac
}

types=()
for f in "${resolved[@]}"; do
  types+=("$(classify "$f")")
done

# --- Shared tmux dispatch: reuse the tagged viewer pane, or open a new split -
# One helper for all three tmux dispatch sites (image / directory / generic
# code-markdown) so the pane-reuse + @view_pane tagging logic lives in exactly
# one place instead of three copies drifting apart.
open_in_pane() {
  local cmd="$1" existing pane
  existing="$(tmux list-panes -F '#{pane_id} #{@view_pane}' 2>/dev/null \
    | awk '$2==1{print $1; exit}')"
  if [ -n "$existing" ]; then
    tmux respawn-pane -k -t "$existing" "$cmd"
    [ -z "$detach" ] && tmux select-pane -t "$existing" 2>/dev/null || true
  else
    pane="$(tmux split-window "-$split_dir" -l "$SPLIT_SIZE" $detach -P -F '#{pane_id}' \
      -c "#{pane_current_path}" "$cmd")"
    [ -n "$pane" ] && tmux set -p -t "$pane" @view_pane 1 2>/dev/null || true
  fi
}

# --- Peel off html targets: always mac-open, never a split --------------------
# Single-target html preserves the exact v1 behavior (process replacement via
# exec, so mac-open's exit code propagates directly). Multi-target html is a
# v2 addition: each one opens independently (best-effort loop) while the
# remaining non-html targets continue through the normal dispatch below.
if [ "${#resolved[@]}" -eq 1 ] && [ "${types[0]}" = "html" ]; then
  if have mac-open; then
    exec mac-open "${resolved[0]}"
  fi
  echo "view: mac-open not found; cannot render HTML" >&2
  exit 1
fi

other_files=()
other_types=()
html_seen=0
for i in "${!resolved[@]}"; do
  if [ "${types[$i]}" = "html" ]; then
    html_seen=1
    if have mac-open; then
      mac-open "${resolved[$i]}" || echo "view: mac-open failed for ${resolved[$i]}" >&2
    else
      echo "view: mac-open not found; cannot render HTML: ${resolved[$i]}" >&2
    fi
  else
    other_files+=("${resolved[$i]}")
    other_types+=("${types[$i]}")
  fi
done

if [ "${#other_files[@]}" -eq 0 ]; then
  # Nothing left to render (all targets were html, already opened above).
  exit 0
fi

# --- Single-target: pdf / binary -> mac-open, never a split ------------------
if [ "${#other_files[@]}" -eq 1 ] && { [ "${other_types[0]}" = "pdf" ] || [ "${other_types[0]}" = "binary" ]; }; then
  if have mac-open; then
    exec mac-open "${other_files[0]}"
  fi
  echo "view: mac-open not found; cannot render ${other_types[0]} file: ${other_files[0]}" >&2
  exit 1
fi

# --- Multi-target guard: image/pdf/binary/directory can't mix with others ----
# These types have no sane "concatenated with other files" rendering — each is
# a single-target dispatch by design (chafa, mac-open, eza --tree are all
# whole-pane renderers, not part of a bat/glow multi-file stream).
if [ "${#other_files[@]}" -gt 1 ]; then
  for i in "${!other_types[@]}"; do
    case "${other_types[$i]}" in
      image|pdf|binary|directory)
        echo "view: multi-file mode only supports markdown/code files; ${other_files[$i]} is ${other_types[$i]}" >&2
        exit 1
        ;;
    esac
  done
fi

# --- Single-target: image -> chafa (context-aware kitty/half-block dispatch) -
image_split_cmd() {
  # In a tmux split, chafa doesn't page on its own (unlike glow/bat), so the
  # pane would close instantly after drawing the image. Hold it open with a
  # keypress, mirroring the "q closes the pane" convention for pagers.
  #
  # The pane runs the user's LOGIN shell (zsh in this dotfiles setup), not
  # necessarily bash, and zsh's `read` builtin rejects bash's combined
  # `-rsn1` flag syntax ("bad option: -1") -- so the held-open command is
  # wrapped in an explicit `bash -c` to guarantee `read -rsn1` behaves the
  # same regardless of the pane's default shell.
  local f="$1" mode inner
  if [ -n "${TMUX:-}" ] && have tmux; then
    mode="$(tmux show -gv allow-passthrough 2>/dev/null || echo off)"
    if [ "$mode" = "on" ]; then
      inner="chafa -f kitty --passthrough tmux -- $(printf '%q' "$f"); read -rsn1 -p \"-- press any key to close --\""
    else
      inner="chafa --fit-width -- $(printf '%q' "$f"); read -rsn1 -p \"-- press any key to close --\""
    fi
    printf 'bash -c %s' "$(printf '%q' "$inner")"
  else
    # Not in tmux -- Ghostty decodes the Kitty graphics protocol natively, no
    # passthrough wrapper needed.
    printf 'chafa -f kitty -- %s' "$(printf '%q' "$f")"
  fi
}

if [ "${#other_files[@]}" -eq 1 ] && [ "${other_types[0]}" = "image" ]; then
  if ! have chafa; then
    echo "view: chafa not found; cannot render image" >&2
    exit 1
  fi
  img="${other_files[0]}"
  if [ -n "${TMUX:-}" ] && have tmux; then
    open_in_pane "$(image_split_cmd "$img")"
    exit 0
  fi
  if [ -t 1 ]; then
    exec chafa -f kitty -- "$img"
  fi
  # Piped/non-interactive: kitty escape codes would be meaningless to a
  # non-terminal consumer, so always fall back to plain half-block symbols.
  exec chafa --fit-width -- "$img"
fi

# --- Single-target: directory -> eza --tree, paged ---------------------------
if [ "${#other_files[@]}" -eq 1 ] && [ "${other_types[0]}" = "directory" ]; then
  if ! have eza; then
    echo "view: eza not found; cannot render directory" >&2
    exit 1
  fi
  dir="${other_files[0]}"
  if [ -n "${TMUX:-}" ] && have tmux; then
    open_in_pane "$(printf 'eza --tree --color=always -- %s | less -R' "$(printf '%q' "$dir")")"
    exit 0
  fi
  if [ -t 1 ]; then
    exec sh -c "eza --tree --color=always -- \"\$1\" | less -R" -- "$dir"
  fi
  exec eza --tree --color=auto -- "$dir"
fi

# --- Remaining case: 1+ markdown/code targets --------------------------------
# Partition into a code group (rendered as one native bat multi-file concat)
# and a markdown group (rendered sequentially, one glow call per file). The
# code group renders first, then the markdown group. For a single target this
# degenerates to exactly the v1 single-file behavior.
code_files=()
md_files=()
for i in "${!other_types[@]}"; do
  if [ "${other_types[$i]}" = "markdown" ]; then
    md_files+=("${other_files[$i]}")
  else
    code_files+=("${other_files[$i]}")
  fi
done

quote_all() {
  local out="" f
  for f in "$@"; do
    out="$out $(printf '%q' "$f")"
  done
  printf '%s' "$out"
}

paged_renderer() {
  local parts=()
  if [ "${#code_files[@]}" -gt 0 ]; then
    parts+=("bat --paging=always --color=always --$(quote_all "${code_files[@]}")")
  fi
  if [ "${#md_files[@]}" -gt 0 ]; then
    if have glow; then
      local f
      for f in "${md_files[@]}"; do
        parts+=("glow -p -- $(printf '%q' "$f")")
      done
    else
      echo "view: glow not found — falling back to bat for markdown" >&2
      parts+=("bat --paging=always --color=always --$(quote_all "${md_files[@]}")")
    fi
  fi
  local IFS=";"
  printf '%s' "${parts[*]}"
}

render_plain() {
  # Non-paged render straight to the current stdout (the piped / CI path).
  # Only meaningful for a single target — multi-file plain dumps would be
  # ambiguous about ordering/framing, so just render each in sequence.
  local f
  for f in "${code_files[@]}"; do
    if have bat; then
      bat --color=auto --paging=never --style=plain -- "$f"
    else
      cat -- "$f"
    fi
  done
  for f in "${md_files[@]}"; do
    if have glow; then
      glow -- "$f"
    else
      echo "view: glow not found — falling back to bat for markdown" >&2
      if have bat; then bat --color=auto --paging=never --style=plain -- "$f"; else cat -- "$f"; fi
    fi
  done
}

render_inline_paged() {
  # Paged render in the current interactive pane (no tmux).
  if [ "${#code_files[@]}" -gt 0 ]; then
    if have bat; then
      bat --paging=always --color=always -- "${code_files[@]}"
    else
      cat -- "${code_files[@]}"
    fi
  fi
  local f
  for f in "${md_files[@]}"; do
    if have glow; then
      glow -p -- "$f"
    else
      echo "view: glow not found — falling back to bat for markdown" >&2
      if have bat; then bat --paging=always --color=always -- "$f"; else cat -- "$f"; fi
    fi
  done
}

# --- Execution context dispatch ----------------------------------------------
if [ -n "${TMUX:-}" ] && have tmux; then
  open_in_pane "$(paged_renderer)"
  exit 0
fi

if [ -t 1 ]; then
  # Interactive terminal, not in tmux: render inline, paged.
  render_inline_paged
  exit 0
fi

# Non-interactive / piped: render inline, non-paged, to stdout.
render_plain
