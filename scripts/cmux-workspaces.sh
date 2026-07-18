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
#   mux oo tc           # Launch specific projects
#   mux b               # Launch all B&B projects
#   mux c               # Launch all Priceless projects
#   mux p               # Launch all Personal projects
#   mux --local oo      # Launch locally instead of SSH
#   mux --list          # List available projects

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
cat_labels = {"b-and-b": "B&B", "priceless": "Priceless", "personal": "Personal"}

# Build parallel arrays by category
groups = {"b-and-b": [], "priceless": [], "personal": []}
for p in projects:
    cat = p["category"]
    if cat in groups:
        groups[cat].append(p["code"])

# Emit group arrays
print("GROUP_BB=(" + " ".join(groups["b-and-b"]) + ")")
print("GROUP_PRICELESS=(" + " ".join(groups["priceless"]) + ")")
print("GROUP_PERSONAL=(" + " ".join(groups["personal"]) + ")")

# Emit associative arrays
projects_entries = []
categories_entries = []
full_names_entries = []
for p in projects:
    code = p["code"]
    path = p["path"]
    # PROJECTS maps code -> path fragment (last component, or dotfile path)
    if path.startswith("."):
        projects_entries.append(f'[{code}]="{path}"')
    else:
        # path is like "dev/oo" — extract just the dir name
        projects_entries.append(f'[{code}]="{path.split("/")[-1]}"')
    categories_entries.append(f'[{code}]="{cat_labels.get(p["category"], "Personal")}"')
    full_names_entries.append(f'[{code}]="{p["name"]}"')

print("declare -A PROJECTS=(" + " ".join(projects_entries) + ")")
print("declare -A CATEGORIES=(" + " ".join(categories_entries) + ")")
print("declare -A FULL_NAMES=(" + " ".join(full_names_entries) + ")")
PYEOF
}

eval "$(TOML_FILE="$TOML_FILE" load_projects)"

# Canonical order (B&B → Priceless → Personal)
CANONICAL_ORDER=("${GROUP_BB[@]}" "${GROUP_PRICELESS[@]}" "${GROUP_PERSONAL[@]}")

get_color() {
  case "${CATEGORIES[$1]:-Personal}" in
    "B&B")      echo "$COLOR_BB" ;;
    "Priceless")   echo "$COLOR_PRICELESS" ;;
    "Personal") echo "$COLOR_PERSONAL" ;;
  esac
}

# Layout:
#   ┌──────────────┬──────────────┐
#   │  Claude Code │   nvim .      │
#   └──────────────┴──────────────┘

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

parse_surface() {
  echo "$1" | awk '{for(i=1;i<=NF;i++) if($i ~ /^surface:/) {print $i; exit}}'
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
  # Workspace activation: source the org profile (env + wrappers PATH) so every
  # pane (nvim/claude) inherits the correct identity. wsenv resolves the
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
    send_to "$ws" "$surface" "$env_exports && cd $full_path && ${ws_activate}$cmd"
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

  # Left pane: Claude Code. SSH mode launches natively (no zellij wrapper) — cmux's
  # own detachable SSH PTY daemon + persistent-server process now provides the
  # disconnect-survival guarantee ws-claude used to hand-roll, confirmed via a live
  # kill/reconnect test (if-vit.4 baseline, 2026-07-18). COLORTERM=truecolor is
  # exported because ws-claude's zellij layer used to set it (zellij sets
  # TERM=xterm-256color but not COLORTERM, so claude would otherwise downgrade to
  # monochrome); wsenv --flags supplies the org-specific launch flags ws-claude used
  # to inject via its generated layout. Local mode keeps ws-claude/zellij unchanged —
  # this was only tested against the SSH-disconnect case, not local persistence.
  # nvim stays a plain cmux pane (only the long-running claude session persists).
  if [[ "$MODE" == "ssh" ]]; then
    pane_exec "$ws_uuid" "$claude_surface" "$full_path" \
      "export COLORTERM=truecolor && claude \$(wsenv --flags $code 2>/dev/null)" "$code"
  else
    pane_exec "$ws_uuid" "$claude_surface" "$full_path" "ws-claude $code" "$code"
  fi
  sleep 0.3

  # Split right: nvim (editor / terminal pane)
  local split_out
  split_out=$($CMUX new-split right --workspace "$ws_uuid" --surface "$claude_surface" 2>&1)
  local editor_surface
  editor_surface=$(parse_surface "$split_out")
  sleep 0.3

  pane_exec "$ws_uuid" "$editor_surface" "$full_path" "nvim ." "$code"

  echo "  ✓ $code ready"
}

# --- Arg parsing ---

targets=()

while [[ $# -gt 0 ]]; do
  case "$1" in
    b)  targets+=("${GROUP_BB[@]}"); shift ;;
    c)  targets+=("${GROUP_PRICELESS[@]}"); shift ;;
    p)  targets+=("${GROUP_PERSONAL[@]}"); shift ;;
    --local)  MODE="local"; shift ;;
    --ssh)    MODE="ssh"; shift ;;
    --host)   SSH_HOST="$2"; shift 2 ;;
    --list)
      echo "Available projects:"
      echo ""
      echo "  B&B [b] (amber):"
      for code in "${GROUP_BB[@]}"; do
        printf "    %-4s %-25s %s\n" "$code" "${FULL_NAMES[$code]:-$code}" "${PROJECTS[$code]}"
      done
      echo ""
      echo "  Priceless [c] (green):"
      for code in "${GROUP_PRICELESS[@]}"; do
        printf "    %-4s %-25s %s\n" "$code" "${FULL_NAMES[$code]:-$code}" "${PROJECTS[$code]}"
      done
      echo ""
      echo "  Personal [p] (blue):"
      for code in "${GROUP_PERSONAL[@]}"; do
        printf "    %-4s %-25s %s\n" "$code" "${FULL_NAMES[$code]:-$code}" "${PROJECTS[$code]}"
      done
      echo ""
      echo "Modes: --ssh (default → $SSH_HOST) | --local"
      exit 0
      ;;
    --help|-h)
      bb_list=$(IFS=", "; echo "${GROUP_BB[*]}")
      priceless_list=$(IFS=", "; echo "${GROUP_PRICELESS[*]}")
      personal_list=$(IFS=", "; echo "${GROUP_PERSONAL[*]}")
      cat <<HELP
Usage: mux [OPTIONS] [b|c|p|PROJECT...]

Groups:
  b    B&B (amber)       — $bb_list
  c    Priceless (green) — $priceless_list
  p    Personal (blue)   — $personal_list

Options:
  --local        Run locally instead of SSH
  --ssh          SSH to homelab (default)
  --host HOST    SSH to a different host
  --list         List available projects

Examples:
  mux b              # Open all B&B projects
  mux c              # Open all Priceless projects
  mux oo mv          # Open specific projects
  mux b oo           # B&B group + oo

Layout per workspace:
  ┌──────────────┬──────────────┐
  │  claude      │  nvim .       │
  └──────────────┴──────────────┘
HELP
      exit 0
      ;;
    *) targets+=("$1"); shift ;;
  esac
done

wait_for_cmux

if [[ ${#targets[@]} -eq 0 ]]; then
  echo "Usage: mux [b|c|p|PROJECT...]"
  echo "  b = B&B, c = Priceless, p = Personal"
  echo "  Or specify project codes: mux oo tc mv"
  echo "  Run 'mux --list' for all projects"
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
