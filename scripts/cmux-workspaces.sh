#!/usr/bin/env bash
# cmux-workspaces.sh — Launch dev workspaces in cmux over SSH
# Source of truth: ~/dev/personal/installfest/scripts/cmux-workspaces.sh
# Project data: ~/dev/personal/installfest/home/projects.toml
#
# Requires bash 4+ (uses `declare -A`). macOS ships only bash 3.2 at /bin/bash and
# interactive PATH often resolves it ahead of Homebrew's, so re-exec under a bash 4+
# if we were started under an older one.
if [ -z "${BASH_VERSINFO:-}" ] || [ "${BASH_VERSINFO[0]}" -lt 4 ]; then
  for _b in /opt/homebrew/bin/bash /usr/local/bin/bash; do
    [ -x "$_b" ] && exec "$_b" "$0" "$@"
  done
  echo "cmux-workspaces: needs bash >= 4 (install: brew install bash)" >&2
  exit 1
fi
#
# Usage:
#   mux oo tc           # Launch specific projects (one workspace each)
#   mux brown           # Launch the b-and-b org root (one workspace)
#   mux priceless       # Launch the priceless org root (one workspace)
#   mux cc              # Launch the cc org root (one workspace)
#   mux personal        # Launch the personal org root (one workspace)
#   mux doctor [code]   # Provenance inspection (exec ws-doctor)
#   mux ready [org]     # Tracker-ready query (exec ws-ready)
#   mux scan            # Filesystem detection scan (exec ws-scan)
#   mux --local oo      # Launch locally instead of SSH
#   mux --list          # List available projects
#
# mux never bulk-launches — every invocation opens at most one workspace per
# named code. Each of the four org roots (brown/priceless/cc/personal) is an
# ordinary registered project code pointing at its ~/dev/<org> directory, not
# a special launch mode — see home/projects.toml.

set -euo pipefail

# mux orchestrates the cmux GUI app, which is macOS-only. Running it from a remote
# shell (e.g. inside a cmux-ssh tab on homelab) drives cmux through the relay and
# creates broken nested workspaces (surfaces never enumerate). Fail loudly instead.
if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "Error: mux must run on the Mac (native cmux), not a remote/cmux-ssh tab." >&2
  echo "       Detected $(uname -s). Open a Mac-native cmux tab and run mux there." >&2
  exit 1
fi

CMUX="${CMUX_CLI:-cmux}"
SSH_HOST="homelab"
REMOTE_DEV="~/dev"
LOCAL_DEV="$HOME/dev"
MODE="ssh"
# Mac's Tailscale IP — injected into remote sessions for cmux-bridge callback
MAC_TAILSCALE_IP="${CMUX_BRIDGE_HOST:-$(tailscale ip -4 2>/dev/null || echo "")}"

# --- Load project registry from projects.toml ---
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TOML_FILE="$SCRIPT_DIR/../home/projects.toml"

if [[ ! -f "$TOML_FILE" ]]; then
  echo "Error: $TOML_FILE not found" >&2
  exit 1
fi

# Category colors
COLOR_BB="#F59E0B"       # amber
COLOR_PRICELESS="#10B981"   # green
COLOR_CC="#8B5CF6"       # violet
COLOR_PERSONAL="#3B82F6" # blue

# shellcheck source=lib/registry.sh
source "$SCRIPT_DIR/lib/registry.sh"
WS_PY=$(registry_python) || exit 1

# Parse projects.toml with Python and emit shell variable assignments
load_projects() {
  "$WS_PY" << 'PYEOF'
import tomllib, os

toml_file = os.environ["TOML_FILE"]
with open(toml_file, "rb") as f:
    data = tomllib.load(f)

projects = data["projects"]

# Category label mapping
cat_labels = {"b-and-b": "B&B", "priceless": "Priceless", "cc": "CC", "personal": "Personal"}
cat_order = ["b-and-b", "priceless", "cc", "personal"]

# Every registered code, grouped by category (canonical launch/reorder order)
# then declaration order within each category. mux never bulk-launches by
# group anymore — this is display/reorder ordering only, not a selector.
by_cat = {c: [] for c in cat_order}
for p in projects:
    cat = p["category"]
    if cat in by_cat:
        by_cat[cat].append(p["code"])
all_codes = [code for cat in cat_order for code in by_cat[cat]]
print("ALL_CODES=(" + " ".join(all_codes) + ")")
for cat in cat_order:
    var = "GROUP_" + cat.upper().replace("-", "_")
    print(f"{var}=(" + " ".join(by_cat[cat]) + ")")

# Emit associative arrays
projects_entries = []
categories_entries = []
full_names_entries = []
for p in projects:
    code = p["code"]
    path = p["path"]
    # PROJECTS maps code -> path fragment resolve_path appends after
    # $REMOTE_DEV/$LOCAL_DEV (dotfile path, or the full dev/-relative
    # subpath). Previously truncated to just the basename (path.split("/")
    # [-1]), which silently discarded nested org structure — "dev/priceless/
    # otaku-odyssey" resolved to ~/dev/otaku-odyssey (wrong; doesn't exist
    # anywhere) instead of ~/dev/priceless/otaku-odyssey (correct — already
    # the live layout on the homelab, mux's default SSH target).
    if path.startswith("."):
        projects_entries.append(f'[{code}]="{path}"')
    elif path.startswith("dev/"):
        projects_entries.append(f'[{code}]="{path[len("dev/"):]}"')
    else:
        projects_entries.append(f'[{code}]="{path}"')
    categories_entries.append(f'[{code}]="{cat_labels.get(p["category"], "Personal")}"')
    full_names_entries.append(f'[{code}]="{p["name"]}"')

print("declare -A PROJECTS=(" + " ".join(projects_entries) + ")")
print("declare -A CATEGORIES=(" + " ".join(categories_entries) + ")")
print("declare -A FULL_NAMES=(" + " ".join(full_names_entries) + ")")
PYEOF
}

eval "$(TOML_FILE="$TOML_FILE" load_projects)"

# Canonical order (b-and-b → priceless → cc → personal, declaration order
# within each) — built directly from the full registry (ALL_CODES), not from
# the old per-letter GROUP_* bulk-launch arrays. Used only for the reorder
# pass below and the grouped --list/--help display; never a launch selector.
CANONICAL_ORDER=("${ALL_CODES[@]}")

get_color() {
  case "${CATEGORIES[$1]:-Personal}" in
    "B&B")      echo "$COLOR_BB" ;;
    "Priceless")   echo "$COLOR_PRICELESS" ;;
    "CC")       echo "$COLOR_CC" ;;
    "Personal") echo "$COLOR_PERSONAL" ;;
  esac
}

# Layout:
#   ┌───────────────────────────────┐
#   │           Claude Code          │
#   └───────────────────────────────┘
# (single pane — cmux's own sidebar file explorer covers editor/browse needs)

wait_for_cmux() {
  local retries=10
  while ! $CMUX ping &>/dev/null; do
    retries=$((retries - 1))
    if [[ $retries -le 0 ]]; then
      echo "Error: cmux not responding. Is it running?" >&2
      exit 1
    fi
    sleep 0.5
  done
}

send_to() {
  local ws="$1" surface="$2" cmd="$3"
  $CMUX send --workspace "$ws" --surface "$surface" "$cmd" >/dev/null 2>&1
  $CMUX send-key --workspace "$ws" --surface "$surface" enter >/dev/null 2>&1
}

# Find workspace UUID by name from list-workspaces output
find_workspace_uuid() {
  local name="$1" ws_list="$2"
  echo "$ws_list" | awk -v name="$name" '
    $0 ~ "  " name "  " || $0 ~ "  " name "$" {
      for(i=1;i<=NF;i++) if($i ~ /^workspace:/) {print $i; exit}
    }'
}

# Connect pane to its working dir and run command.
# SSH mode: the workspace is created via `cmux ssh` (see create_workspace), so every
#   pane already runs on $SSH_HOST — NO per-pane `ssh -t` wrapper. We still export
#   CMUX_WORKSPACE_ID/CMUX_SURFACE_ID/CMUX_BRIDGE_HOST so remote CC hooks can drive the
#   Mac's cmux browser over Tailscale, exactly as before.
# Local mode: cd directly, then run (Ghostty auto-sets CMUX_* env vars).
pane_exec() {
  local ws="$1" surface="$2" full_path="$3" cmd="$4" code="${5:-}"
  # Workspace activation: source the org profile (env + wrappers PATH) so the
  # claude pane inherits the correct identity. wsenv resolves the
  # org from $code via projects.toml; harmless no-op if the profile is absent.
  local ws_activate=""
  if [[ -n "$code" ]]; then
    ws_activate="eval \"\$(wsenv $code 2>/dev/null)\" && "
  fi
  if [[ "$MODE" == "ssh" ]]; then
    local env_exports="export CMUX_WORKSPACE_ID=$ws CMUX_SURFACE_ID=$surface"
    if [[ -n "$MAC_TAILSCALE_IP" ]]; then
      env_exports+=" CMUX_BRIDGE_HOST=$MAC_TAILSCALE_IP"
    fi
    # cmux tracks a workspace's cwd (sidebar-state, Files sidebar root) via an
    # OSC 7 escape the shell's own prompt normally emits on every render. Since
    # $cmd (claude) is a long-running foreground process chained onto the same
    # `cd && ...` line, the shell never returns to its own prompt to emit that
    # OSC 7 itself — cwd stays stuck at the SSH login home dir for the whole
    # session (confirmed live: sidebar-state cwd never left /home/nyaptor).
    # Emit it explicitly right after the cd, before handing off to $cmd.
    local osc7_report='printf "\033]7;file://%s%s\007" "$(hostname)" "$PWD"'
    # Record this workspace's directory so ANY future pane/split/tab in it —
    # not just the ones this script creates — can auto-cd. A fresh SSH channel
    # always lands at the login home dir with no cwd memory of sibling panes
    # (confirmed live: a manually-added split in an already-open workspace
    # landed at /home/nyaptor, not the project dir). CMUX_WORKSPACE_ID is
    # auto-set by cmux in every pane it spawns (confirmed live too), so the
    # remote shell startup hook (home/dot_zsh/rc/linux.zsh) can look this up.
    local record_cwd="mkdir -p ~/.cmux/workspace-cwd && printf '%s' \"$full_path\" > ~/.cmux/workspace-cwd/$ws"
    send_to "$ws" "$surface" "$env_exports && cd $full_path && $record_cwd && $osc7_report && ${ws_activate}$cmd"
  else
    send_to "$ws" "$surface" "cd $full_path && ${ws_activate}$cmd"
  fi
}

# Resolve project path (handles both ~/dev/X and ~/X patterns)
resolve_path() {
  local project="$1"
  if [[ "$project" == .* ]]; then
    # Home-relative (e.g. .claude → ~/.claude)
    if [[ "$MODE" == "ssh" ]]; then
      echo "~/$project"
    else
      echo "$HOME/$project"
    fi
  else
    # Dev-relative (e.g. oo → ~/dev/oo)
    if [[ "$MODE" == "ssh" ]]; then
      echo "$REMOTE_DEV/$project"
    else
      echo "$LOCAL_DEV/$project"
    fi
  fi
}

# Phase 1: Create workspace shell (sequential — preserves ordering)
create_workspace() {
  local code="$1"
  local project="${PROJECTS[$code]}"
  local category="${CATEGORIES[$code]:-Personal}"
  local color
  color=$(get_color "$code")

  # Skip creation if workspace already exists (reorder phase handles ordering)
  local ws_list
  ws_list=$($CMUX list-workspaces 2>&1)
  if echo "$ws_list" | grep -q "  $code\b\|  $code  "; then
    echo "  ⊘ $code — already open, skipping creation"
    return 1
  fi

  if [[ "$MODE" == "local" ]]; then
    local check_path
    check_path=$(resolve_path "$project")
    if [[ ! -d "$check_path" ]]; then
      echo "  ✗ $code — $check_path not found, skipping" >&2
      return 1
    fi
  fi

  local ws_uuid
  if [[ "$MODE" == "ssh" ]]; then
    # Native SSH-backed workspace: cmux uploads cmuxd-remote and binds ALL panes to
    # $SSH_HOST. Output: `OK workspace=workspace:N target=<host> state=connecting`.
    ws_uuid=$($CMUX ssh "$SSH_HOST" --name "$code" --no-focus 2>&1 \
      | grep -oE 'workspace=[^ ]+' | head -1 | cut -d'=' -f2)
  else
    ws_uuid=$($CMUX new-workspace 2>&1 | awk '{print $2}')
  fi
  if [[ -z "$ws_uuid" ]]; then
    echo "  ✗ $code — failed to create workspace" >&2
    return 1
  fi
  sleep 0.2

  $CMUX rename-workspace --workspace "$ws_uuid" "$code" >/dev/null 2>&1
  local full_name="${FULL_NAMES[$code]:-$code}"
  $CMUX set-status --workspace "$ws_uuid" category "$full_name" --color "$color" >/dev/null 2>&1

  echo "  ▸ $code created"
  WS_UUIDS[$code]="$ws_uuid"
}

# Wait for a surface to become available (retries)
wait_for_surface() {
  # cmux ssh needs ~3-5s to connect + spawn its first surface (first connect uploads
  # cmuxd-remote), so poll patiently — local `new-workspace` still resolves on retry 1.
  local ws_uuid="$1" retries=30
  local surface=""
  while [[ $retries -gt 0 ]]; do
    surface=$($CMUX list-pane-surfaces --workspace "$ws_uuid" 2>&1 \
      | awk '{for(i=1;i<=NF;i++) if($i ~ /^surface:/) {print $i; exit}}')
    if [[ -n "$surface" ]]; then
      echo "$surface"
      return 0
    fi
    retries=$((retries - 1))
    sleep 0.5
  done
  return 1
}

# SSH-mode readiness gate: poll cmux's own remote-connection state instead of a
# blind surface-existence loop + a fixed settle sleep before the first send.
# `cmux workspace list --json` exposes .remote.state ("connecting" -> "connected")
# per workspace (keyed by .ref); waiting for that exact flip is a real, confirmed
# signal — proven by killing a live SSH channel mid-job and observing output land
# in the same poll tick as the state transition (if-vit.6 baseline, 2026-07-18).
# This subsumes wait_for_surface's blind poll AND the old pre-first-send sleep.
# Local-mode workspaces have no .remote object, so this is SSH-mode only —
# local mode still uses wait_for_surface + its settle sleep, unchanged.
wait_for_remote_ready() {
  local ws_uuid="$1" retries=30
  while [[ $retries -gt 0 ]]; do
    local state
    state=$($CMUX workspace list --json 2>/dev/null | "$WS_PY" -c '
import json, sys
ws_ref = sys.argv[1]
try:
    data = json.load(sys.stdin)
except Exception:
    print("")
    sys.exit(0)
for ws in data.get("workspaces", []):
    if ws.get("ref") == ws_ref:
        print(ws.get("remote", {}).get("state") or "")
        sys.exit(0)
print("")
' "$ws_uuid" 2>/dev/null)
    if [[ "$state" == "connected" ]]; then
      local surface
      surface=$($CMUX list-pane-surfaces --workspace "$ws_uuid" 2>&1 \
        | awk '{for(i=1;i<=NF;i++) if($i ~ /^surface:/) {print $i; exit}}')
      if [[ -n "$surface" ]]; then
        echo "$surface"
        return 0
      fi
    fi
    retries=$((retries - 1))
    sleep 0.2
  done
  return 1
}

# Phase 2: Populate panes (parallel — the slow part)
populate_workspace() {
  local code="$1"
  local ws_uuid="$2"
  local project="${PROJECTS[$code]}"
  local full_path
  full_path=$(resolve_path "$project")

  local claude_surface
  if [[ "$MODE" == "ssh" ]]; then
    claude_surface=$(wait_for_remote_ready "$ws_uuid") || {
      echo "  ✗ $code — remote never connected, skipping populate" >&2
      return 1
    }
  else
    claude_surface=$(wait_for_surface "$ws_uuid") || {
      echo "  ✗ $code — no surface found, skipping populate" >&2
      return 1
    }
    sleep 0.2
  fi

  # Single pane: Claude Code. SSH mode launches natively (no zellij wrapper) — cmux's
  # own detachable SSH PTY daemon + persistent-server process now provides the
  # disconnect-survival guarantee ws-claude used to hand-roll, confirmed via a live
  # kill/reconnect test (if-vit.4 baseline, 2026-07-18). COLORTERM=truecolor is
  # exported because ws-claude's zellij layer used to set it (zellij sets
  # TERM=xterm-256color but not COLORTERM, so claude would otherwise downgrade to
  # monochrome); wsenv --flags supplies the org-specific launch flags ws-claude used
  # to inject via its generated layout. Local mode keeps ws-claude/zellij unchanged —
  # this was only tested against the SSH-disconnect case, not local persistence.
  # No editor split — cmux's own sidebar file explorer covers browse/edit needs.
  if [[ "$MODE" == "ssh" ]]; then
    pane_exec "$ws_uuid" "$claude_surface" "$full_path" \
      "export COLORTERM=truecolor && claude \$(wsenv --flags $code 2>/dev/null)" "$code"
  else
    pane_exec "$ws_uuid" "$claude_surface" "$full_path" "ws-claude $code" "$code"
  fi

  echo "  ✓ $code ready"
}

# --- Subcommand dispatch (doctor/ready/scan) ---
#
# These don't launch cmux workspaces at all — dispatch and exit before
# wait_for_cmux, so `mux doctor`/`mux ready`/`mux scan` work even when cmux
# itself isn't running. Only recognized as the very first argument.
case "${1:-}" in
  doctor) shift; exec "$SCRIPT_DIR/../packages/workspace/bin/ws-doctor" "$@" ;;
  ready)  shift; exec "$SCRIPT_DIR/../packages/workspace/bin/ws-ready" "$@" ;;
  scan)   shift; exec "$SCRIPT_DIR/../packages/workspace/bin/ws-scan" "$@" ;;
esac

# --- Arg parsing ---

targets=()

while [[ $# -gt 0 ]]; do
  case "$1" in
    --local)  MODE="local"; shift ;;
    --ssh)    MODE="ssh"; shift ;;
    --host)   SSH_HOST="$2"; shift 2 ;;
    --list)
      echo "Available projects (one workspace per code — mux never bulk-launches):"
      echo ""
      echo "  B&B (amber):"
      for code in "${GROUP_B_AND_B[@]}"; do
        printf "    %-14s %-25s %s\n" "$code" "${FULL_NAMES[$code]:-$code}" "${PROJECTS[$code]}"
      done
      echo ""
      echo "  Priceless (green):"
      for code in "${GROUP_PRICELESS[@]}"; do
        printf "    %-14s %-25s %s\n" "$code" "${FULL_NAMES[$code]:-$code}" "${PROJECTS[$code]}"
      done
      echo ""
      echo "  CC (violet):"
      for code in "${GROUP_CC[@]}"; do
        printf "    %-14s %-25s %s\n" "$code" "${FULL_NAMES[$code]:-$code}" "${PROJECTS[$code]}"
      done
      echo ""
      echo "  Personal (blue):"
      for code in "${GROUP_PERSONAL[@]}"; do
        printf "    %-14s %-25s %s\n" "$code" "${FULL_NAMES[$code]:-$code}" "${PROJECTS[$code]}"
      done
      echo ""
      echo "Modes: --ssh (default → $SSH_HOST) | --local"
      exit 0
      ;;
    --help|-h)
      cat <<HELP
Usage: mux [OPTIONS] [PROJECT...]

mux never bulk-launches — every code opens exactly one workspace. Each org
root (brown/priceless/cc/personal) is an ordinary registered code pointing
at its ~/dev/<org> directory, launched the same way as any other project.

Subcommands:
  doctor [code]  Provenance inspection (what config is active where)
  ready [org]    Tracker-ready work query
  scan           Filesystem detection scan — keeps home/projects.toml honest

Options:
  --local        Run locally instead of SSH
  --ssh          SSH to homelab (default)
  --host HOST    SSH to a different host
  --list         List all available project codes, grouped by org

Examples:
  mux oo mv          # Open specific projects (one workspace each)
  mux brown          # Open the b-and-b org root
  mux priceless personal   # Open two org roots
  mux doctor         # Inspect config provenance for \$PWD
  mux ready priceless      # Ready-work query for the priceless org

Layout per workspace:
  ┌───────────────────────────────┐
  │            claude              │
  └───────────────────────────────┘
HELP
      exit 0
      ;;
    *) targets+=("$1"); shift ;;
  esac
done

wait_for_cmux

if [[ ${#targets[@]} -eq 0 ]]; then
  echo "Usage: mux [PROJECT...]"
  echo "  Specify project codes: mux oo tc mv"
  echo "  Or an org root: mux brown | mux priceless | mux cc | mux personal"
  echo "  Run 'mux --list' for all projects, 'mux --help' for subcommands"
  exit 0
fi

# Deduplicate targets
declare -A seen
unique_targets=()
for code in "${targets[@]}"; do
  if [[ -z "${seen[$code]:-}" ]]; then
    seen[$code]=1
    unique_targets+=("$code")
  fi
done
targets=("${unique_targets[@]}")

echo "Mode: $MODE (host: ${SSH_HOST:-local})"
echo "Launching ${#targets[@]} workspace(s)..."
echo ""

# --- Phase 1: Create & order workspaces (sequential, fast) ---
declare -A WS_UUIDS
created=()

for code in "${targets[@]}"; do
  if [[ -z "${PROJECTS[$code]:-}" ]]; then
    echo "  ✗ Unknown project: $code (use --list)" >&2
    continue
  fi
  if create_workspace "$code"; then
    created+=("$code")
  fi
done

echo ""

# --- Phase 2: Populate panes (staggered parallel) ---
if [[ ${#created[@]} -gt 0 ]]; then
  echo "Populating ${#created[@]} workspace(s)..."
  pids=()
  for code in "${created[@]}"; do
    populate_workspace "$code" "${WS_UUIDS[$code]}" &
    pids+=($!)
    sleep 0.3  # stagger to avoid overwhelming cmux socket
  done
  wait "${pids[@]}"
fi

# --- Phase 3: Reorder + refresh labels on ALL workspaces ---
echo "Reordering workspaces..."
ws_list=$($CMUX list-workspaces 2>&1)
prev_uuid=""
for code in "${CANONICAL_ORDER[@]}"; do
  local_uuid=$(find_workspace_uuid "$code" "$ws_list")
  if [[ -n "$local_uuid" ]]; then
    # Refresh label and color on every run
    local_name="${FULL_NAMES[$code]:-$code}"
    local_color=$(get_color "$code")
    $CMUX set-status --workspace "$local_uuid" category "$local_name" --color "$local_color" >/dev/null 2>&1
    # Reorder
    if [[ -n "$prev_uuid" ]]; then
      $CMUX reorder-workspace --workspace "$local_uuid" --after "$prev_uuid" >/dev/null 2>&1
    fi
    prev_uuid="$local_uuid"
  fi
done

# Switch to first target workspace
first_uuid=$(find_workspace_uuid "${targets[0]}" "$ws_list")
if [[ -n "$first_uuid" ]]; then
  $CMUX select-workspace --workspace "$first_uuid" >/dev/null 2>&1
fi

echo ""
echo "Done. Use Cmd+1-9 to switch workspaces."
