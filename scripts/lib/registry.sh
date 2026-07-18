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
