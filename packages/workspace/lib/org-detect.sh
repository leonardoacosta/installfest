#!/usr/bin/env bash
# org-detect.sh — derive a project's org category from its git origin remote.
#
# Precedence (see openspec/changes/add-org-workspace-directories-and-detection
# for the confirmed live-remote table this rule is grounded in — every Mac +
# homelab ~/dev/* origin observed this session matches exactly one of these):
#   1. registry code "cc" -> "cc" (hardcoded — cc's remote namespace,
#      github.com/leonardoacosta/central-claude, is indistinguishable from an
#      ordinary personal repo by URL alone; a naming-pattern heuristic would
#      be fragile, so this is an explicit one-line exception instead)
#   2. origin contains "brownandbrowninc" (either dev.azure.com/brownandbrowninc
#      or brownandbrowninc.visualstudio.com) -> "b-and-b"
#   3. origin matches github.com[:/]Priceless-Development/ -> "priceless"
#   4. origin matches github.com[:/]leonardoacosta/ -> "personal"
#   5. no match (no origin, or an unrecognized host/owner) -> "unknown"
#      (callers MUST NOT auto-register an "unknown" org)
#
# cc-audit is explicitly excluded from derivation entirely (out of scope) —
# callers should skip it by name before calling org_detect_for_repo, not rely
# on this lib to special-case it.
(return 0 2>/dev/null) || set -euo pipefail

# org_detect_from_remote <origin_url> [registry_code]
# Echoes the derived org category. Always succeeds (exit 0) — "unknown" is a
# valid, expected result, never a failure.
org_detect_from_remote() {
  local origin="${1:-}"
  local code="${2:-}"

  if [[ "$code" == "cc" ]]; then
    echo "cc"
    return 0
  fi

  if [[ "$origin" == *brownandbrowninc* ]]; then
    echo "b-and-b"
    return 0
  fi

  if [[ "$origin" =~ github\.com[:/]Priceless-Development/ ]]; then
    echo "priceless"
    return 0
  fi

  if [[ "$origin" =~ github\.com[:/]leonardoacosta/ ]]; then
    echo "personal"
    return 0
  fi

  echo "unknown"
  return 0
}

# org_detect_for_repo <repo_dir> [registry_code]
# Reads the repo's own `origin` remote (git remote get-url origin) and
# derives its org. Echoes "unknown" (never fails) if the dir isn't a git
# repo or has no origin remote.
org_detect_for_repo() {
  local repo_dir="${1:?org_detect_for_repo: repo_dir required}"
  local code="${2:-}"
  local origin
  origin=$(git -C "$repo_dir" remote get-url origin 2>/dev/null || echo "")
  org_detect_from_remote "$origin" "$code"
}
