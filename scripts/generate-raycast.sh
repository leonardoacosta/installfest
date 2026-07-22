#!/usr/bin/env bash
# generate-raycast.sh — Generate Raycast scripts from projects.toml
# Source of truth: ~/dev/personal/installfest/home/projects.toml
#
# Usage:
#   bash scripts/generate-raycast.sh          # Generate + prune all scripts
#   bash scripts/generate-raycast.sh --dry-run # Show what would be generated/pruned
#   bash scripts/generate-raycast.sh --help    # Show this usage block
#
# Generates:
#   raycast-scripts/{code}.sh          — Open project on homelab via Cursor SSH Remote
#   raycast-scripts/local/{code}.sh    — Open project locally in Cursor
#   raycast-scripts/cloudpc/{code}.sh  — Open project on CloudPC via Cursor SSH Remote
#   raycast-scripts/open-project.sh    — Dropdown picker (remote, Cursor)
#   raycast-scripts/local/open-project.sh — Dropdown picker (local, Cursor)
#
# Prunes (add-launcher-registry-prune-pass): after generating, deletes any
# {code}.sh in the 3 output dirs above whose code is no longer a projects.toml
# key for that dir's tier — registry.sh's registry_orphan_codes() does the
# diff, so a removed project's stale launcher never lingers. --dry-run prints
# "Would prune: <path>" instead of deleting. One hand-maintained root-dir
# script (img.sh) is explicitly excluded — see the NON_REGISTRY_SCRIPTS block
# below. (paste-image.sh deleted 2026-07-22: repo-destination screenshots
# retired; img.sh's fixed ~/screenshots sink replaced it.)
#
# Editor migration (2026-07-08): reverted Mac/homelab editor from Zed back to
# Cursor — the 2026-04-26 one-week Zed trial ran its course. To try Zed again,
# swap `cursor ...` back to `zed ssh://...` / `zed ~/...` in the generators
# below and re-run this script.

set -uo pipefail

if [[ "${1:-}" == "--help" || "${1:-}" == "-h" ]]; then
	sed -n '5,14p' "$0" | sed 's/^# \{0,1\}//'
	exit 0
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
export REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
source "$SCRIPT_DIR/lib/registry.sh"
export TOML_FILE="$(registry_path)"
export RAYCAST_DIR="$REPO_ROOT/platform/raycast-scripts"
export DRY_RUN=false

[[ "${1:-}" == "--dry-run" ]] && export DRY_RUN=true

if [[ ! -f "$TOML_FILE" ]]; then
  echo "Error: $TOML_FILE not found" >&2
  exit 1
fi

# Parse TOML and generate scripts via Python
PY="$(registry_python)" || exit 1
"$PY" << 'PYTHON_SCRIPT'
import tomllib
import os
import sys
import json

repo_root = os.environ["REPO_ROOT"]
raycast_dir = os.environ["RAYCAST_DIR"]
dry_run = os.environ.get("DRY_RUN", "false") == "true"
toml_file = os.environ["TOML_FILE"]

with open(toml_file, "rb") as f:
    data = tomllib.load(f)

defaults = data["defaults"]
projects = data["projects"]

ssh_host = defaults["ssh_host"]
ssh_base = defaults["ssh_base"]
cloudpc_host = defaults["cloudpc_host"]
cloudpc_base = defaults["cloudpc_base"]
cloudpc_dev = defaults["cloudpc_dev"]

AUTHOR = "leonardoacosta"
AUTHOR_URL = "https://raycast.com/leonardoacosta"


def write_script(path, content):
    """Write a script file and make it executable."""
    if dry_run:
        print(f"  [dry-run] {os.path.relpath(path, repo_root)}")
        return
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w") as f:
        f.write(content)
    os.chmod(path, 0o755)


def resolve_remote_uri(project):
    """Build the Cursor SSH Remote URI for homelab.

    Cursor remote SSH reads ~/.ssh/config for the host alias (`homelab`).
    Form: vscode-remote://ssh-remote+[user@]host/absolute/path
    """
    path = project["path"]
    return f'vscode-remote://ssh-remote+{ssh_host}{ssh_base}/{path}/'


def resolve_local_path(project):
    """Build the local path for Mac."""
    path = project["path"]
    if path.startswith("."):
        return f"~/{path}"
    else:
        return f"~/{path}/"


def resolve_cloudpc_uri(project):
    """Build the Cursor SSH Remote URI for CloudPC."""
    path = project["path"]
    code = project["code"]
    if path.startswith("."):
        # Home-relative (e.g. .claude -> C:/Users/LeonardoAcosta/.claude/)
        return f'vscode-remote://ssh-remote+{cloudpc_host}/{cloudpc_base}/{path}/'
    else:
        # Dev projects on CloudPC use source/repos/
        return f'vscode-remote://ssh-remote+{cloudpc_host}/{cloudpc_dev}/{code}/'


def gen_remote_script(project):
    """Generate a remote (homelab) Raycast script — opens project in Cursor via SSH Remote."""
    code = project["code"]
    name = project["name"]
    icon = project["icon"]
    uri = resolve_remote_uri(project)

    return f'''#!/bin/bash

# Required parameters:
# @raycast.schemaVersion 1
# @raycast.title {code}
# @raycast.mode silent

# Optional parameters:
# @raycast.icon {icon}

# Documentation:
# @raycast.description {name}
# @raycast.author {AUTHOR}
# @raycast.authorURL {AUTHOR_URL}

cursor --folder-uri "{uri}"
'''


def gen_local_script(project):
    """Generate a local Raycast script — opens project in Cursor."""
    code = project["code"]
    name = project["name"]
    icon = project["icon"]
    local_path = resolve_local_path(project)

    return f'''#!/bin/bash

# Required parameters:
# @raycast.schemaVersion 1
# @raycast.title {code}l
# @raycast.mode silent

# Optional parameters:
# @raycast.icon {icon}

# Documentation:
# @raycast.description {name}
# @raycast.author {AUTHOR}
# @raycast.authorURL {AUTHOR_URL}

cursor {local_path}
'''


def gen_cloudpc_script(project):
    """Generate a CloudPC Raycast script."""
    code = project["code"]
    name = project["name"]
    icon = project["icon"]
    uri = resolve_cloudpc_uri(project)

    return f'''#!/bin/bash

# Required parameters:
# @raycast.schemaVersion 1
# @raycast.title {code}c
# @raycast.mode silent

# Optional parameters:
# @raycast.icon {icon}

# Documentation:
# @raycast.description {name} (CloudPC)
# @raycast.author {AUTHOR}
# @raycast.authorURL {AUTHOR_URL}

cursor --folder-uri "{uri}"
'''


def gen_dropdown_script(tier):
    """Generate the open-project dropdown picker script."""
    if tier == "remote":
        title = "open project"
        description = "Open project on homelab via Cursor SSH Remote"
        dir_name = ""
    elif tier == "local":
        title = "open project local"
        description = "Open project locally in Cursor"
        dir_name = "local"
    else:
        return None

    # Build dropdown data from all projects that have this tier
    dropdown_items = []
    for p in projects:
        if tier in p["tiers"]:
            dropdown_items.append({"title": p["name"], "value": p["code"]})

    dropdown_json = json.dumps(dropdown_items, ensure_ascii=False)
    arg_line = f'# @raycast.argument1 {{ "type": "dropdown", "placeholder": "project", "data": {dropdown_json} }}'

    if tier == "remote":
        body = f'''if [ "$1" = "cc" ]; then
  cursor --folder-uri "vscode-remote://ssh-remote+{ssh_host}{ssh_base}/.claude/"
else
  cursor --folder-uri "vscode-remote://ssh-remote+{ssh_host}{ssh_base}/dev/$1/"
fi'''
    else:
        body = '''if [ "$1" = "cc" ]; then
  cursor ~/.claude
else
  cursor ~/dev/$1/
fi'''

    return f'''#!/bin/bash

# Required parameters:
# @raycast.schemaVersion 1
# @raycast.title {title}
# @raycast.mode silent

# Optional parameters:
# @raycast.icon 🚀
{arg_line}

# Documentation:
# @raycast.description {description}
# @raycast.author {AUTHOR}
# @raycast.authorURL {AUTHOR_URL}

{body}
'''


# --- Generate individual project scripts ---

remote_count = 0
local_count = 0
cloudpc_count = 0

for p in projects:
    code = p["code"]
    tiers = p["tiers"]

    if "remote" in tiers:
        path = os.path.join(raycast_dir, f"{code}.sh")
        write_script(path, gen_remote_script(p))
        remote_count += 1

    if "local" in tiers:
        path = os.path.join(raycast_dir, "local", f"{code}.sh")
        write_script(path, gen_local_script(p))
        local_count += 1

    if "cloudpc" in tiers:
        path = os.path.join(raycast_dir, "cloudpc", f"{code}.sh")
        write_script(path, gen_cloudpc_script(p))
        cloudpc_count += 1

# --- Generate dropdown picker scripts ---

for tier in ("remote", "local"):
    content = gen_dropdown_script(tier)
    if content:
        if tier == "remote":
            path = os.path.join(raycast_dir, "open-project.sh")
        else:
            path = os.path.join(raycast_dir, "local", "open-project.sh")
        write_script(path, content)

# --- Summary ---

action = "Would generate" if dry_run else "Generated"
print(f"{action} {remote_count} remote + {local_count} local + {cloudpc_count} cloudpc + 2 dropdown = {remote_count + local_count + cloudpc_count + 2} scripts total")

PYTHON_SCRIPT

# --- Prune orphaned scripts ---
#
# Diff each output dir against the current registry (registry_orphan_codes,
# scripts/lib/registry.sh) and remove any {code}.sh no longer backed by a
# projects.toml key carrying that dir's tier. root.sh / open-project.sh are
# already excluded inside registry_orphan_codes itself (intentional launcher
# infra, not registry-derived).
#
# Tier mapping for the 3 output dirs:
#   platform/raycast-scripts/local/    -> "local"   (gen_local_script)
#   platform/raycast-scripts/cloudpc/  -> "cloudpc"  (gen_cloudpc_script)
#   platform/raycast-scripts/ (root)   -> "remote"  (gen_remote_script) — root/
#     is the dir gen_remote_script() + the "remote"-tier dropdown picker write
#     to; it is NOT a 4th tier of its own, so "remote" is the correct diff key.
#
# root/ ALSO holds a genuinely hand-maintained, non-registry Raycast script
# (img.sh — clipboard-image helper). It is never written by this generator
# (its body doesn't match any gen_*_script() template, and git history shows
# independent hand-edits), so a naive registry-diff would treat it as an
# orphan and delete a real file the first time this prune pass runs.
# scripts/audit-projects.sh's own orphan check (section 3, section_raycast)
# sidesteps this by never scanning root/ at all — only local/ and cloudpc/.
# This prune pass DOES scan root/ (the spec requires it), so it must exclude
# the name explicitly instead. (paste-image.sh removed from this list
# 2026-07-22 — file deleted, see img.sh header.)
NON_REGISTRY_SCRIPTS=(img)

_is_non_registry_script() {
  local code="$1" x
  for x in "${NON_REGISTRY_SCRIPTS[@]}"; do
    [[ "$code" == "$x" ]] && return 0
  done
  return 1
}

_prune_dir() {
  local dir="$1" tier="$2" label="$3" orphan code path
  while IFS= read -r orphan; do
    [[ -n "$orphan" ]] || continue
    code="${orphan%.sh}"
    _is_non_registry_script "$code" && continue
    path="$dir/$orphan"
    if $DRY_RUN; then
      echo "  Would prune: $label/$orphan"
    else
      rm -f "$path"
      echo "  Pruned: $label/$orphan"
    fi
  done < <(registry_orphan_codes "$dir" "$tier")
}

_prune_dir "$RAYCAST_DIR" "remote" "platform/raycast-scripts"
_prune_dir "$RAYCAST_DIR/local" "local" "platform/raycast-scripts/local"
_prune_dir "$RAYCAST_DIR/cloudpc" "cloudpc" "platform/raycast-scripts/cloudpc"
