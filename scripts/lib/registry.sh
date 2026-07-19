#!/usr/bin/env bash
# registry.sh — shared resolver for home/projects.toml consumers.
#
# Every script that reads the project registry needs (1) the registry file
# path and (2) a python3 that has tomllib (stdlib only in 3.11+ — macOS
# resolves /usr/bin/python3 = 3.9, which lacks it). This used to be a
# copy-pasted probe loop in 4 scripts and a bare `python3` call (Mac-broken)
# in 3 more. One sourced lib, one fix.
#
# Sourced by: wsenv, generate-profiles, ws-ready, ws-scan, cmux-workspaces.sh,
# generate-raycast.sh, mux-remote.sh, copen, scripts/lib/open-core.sh.
(return 0 2>/dev/null) || set -euo pipefail

_REGISTRY_PY=""

# registry_path — echo the projects.toml path, or return 1 with a stderr
# message if it can't be found.
registry_path() {
  local reg="${PROJECTS_REGISTRY:-${DOTFILES:-$HOME/dev/personal/installfest}/home/projects.toml}"
  [[ -f "$reg" ]] || reg="$HOME/dev/personal/installfest/home/projects.toml"
  if [[ ! -f "$reg" ]]; then
    echo "registry: not found: $reg" >&2
    return 1
  fi
  echo "$reg"
}

# registry_python — echo a python3 with tomllib (probes python3.14 down to
# python3), caching the result in $_REGISTRY_PY so repeated calls don't
# re-probe. Returns 1 with a stderr message if none is found.
registry_python() {
  if [[ -n "$_REGISTRY_PY" ]]; then
    echo "$_REGISTRY_PY"
    return 0
  fi
  local _py
  for _py in python3.14 python3.13 python3.12 python3.11 python3; do
    if command -v "$_py" >/dev/null 2>&1 && "$_py" -c 'import tomllib' 2>/dev/null; then
      _REGISTRY_PY="$_py"
      echo "$_py"
      return 0
    fi
  done
  echo "registry: no python3 with tomllib found (need Python >= 3.11)" >&2
  return 1
}

# registry_orphan_codes DIR TIER — echo the basename (<code>.sh, one per line)
# of every <code>.sh file in DIR whose <code> is NOT a projects.toml [projects]
# key carrying TIER. root.sh / open-project.sh are intentional launcher infra
# and are never treated as orphans. This is the shared implementation of the
# registry-diff that scripts/audit-projects.sh section 3 (section_raycast)
# performs inline — extracted here so the auditor and both generators
# (generate-raycast.sh, cmux-workspaces.sh) share one copy instead of three.
#
# Echoes nothing and returns 0 when DIR is absent or holds no orphans. Returns 1
# (with a stderr message from the resolver) if the registry or a tomllib python
# can't be found.
registry_orphan_codes() {
  local dir="$1" tier="$2"
  [[ -d "$dir" ]] || return 0
  local reg py
  reg="$(registry_path)" || return 1
  py="$(registry_python)" || return 1
  # Codes that carry TIER, one per line — the valid set for this directory.
  local valid
  valid="$("$py" - "$reg" "$tier" <<'PYEOF'
import sys, tomllib
with open(sys.argv[1], "rb") as f:
    data = tomllib.load(f)
tier = sys.argv[2]
for p in data.get("projects", []):
    if tier in p.get("tiers", []):
        print(p["code"])
PYEOF
)" || return 1
  local f code
  for f in "$dir"/*.sh; do
    [[ -f "$f" ]] || continue          # no-match glob stays literal -> skipped
    code="$(basename "$f" .sh)"
    case "$code" in root|open-project) continue;; esac
    grep -qxF "$code" <<<"$valid" || printf '%s\n' "$code.sh"
  done
}
