#!/usr/bin/env bash
# tmux-sidebar-tree.sh — the directory listing rendered inside tmux-sidebar's
# pane (wired via `@sidebar-tree-command` in ~/.config/tmux/tmux.conf).
#
# Why a wrapper instead of the plugin's default bare `tree`: `tree` applies one
# global depth (`-L N`) to the whole walk — it has no per-directory depth knob —
# so a single invocation cannot both keep a repo root skimmable AND fully open
# one interesting subtree. This composes two invocations to get that: the repo
# root at a shallow depth, then `openspec/changes/` at a deeper one when it
# exists, so in-flight change proposals read as an expanded work queue while the
# rest of the tree stays a one-screen overview.
#
# Runs with cwd == the tracked pane's `#{pane_current_path}` (tmux-sidebar
# passes `-c` to the pane it spawns), and its stdout is piped into the sidebar's
# pager, so `-C` is required — the pager is invoked with --RAW-CONTROL-CHARS but
# `tree` auto-disables colour when stdout is not a tty.
#
# Depths are overridable via the environment for a one-off wide sidebar:
#   SIDEBAR_TREE_DEPTH=3 SIDEBAR_TREE_DEEP_DEPTH=5

set -euo pipefail

ROOT_DEPTH="${SIDEBAR_TREE_DEPTH:-2}"
DEEP_DEPTH="${SIDEBAR_TREE_DEEP_DEPTH:-4}"
DEEP_PATH="${SIDEBAR_TREE_DEEP_PATH:-openspec/changes}"

if ! command -v tree >/dev/null 2>&1; then
  echo "tmux-sidebar-tree: \`tree\` not installed" >&2
  ls -1
  exit 0
fi

# --noreport: the trailing "N directories, M files" line is pure noise in a
# 40-column pane.
tree -C --noreport -L "$ROOT_DEPTH" .

if [ -d "$DEEP_PATH" ]; then
  printf '\n'
  tree -C --noreport -L "$DEEP_DEPTH" "$DEEP_PATH"
fi
