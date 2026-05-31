#!/usr/bin/env bash
# generate-workspace-profiles.sh — emit per-org workspace profiles from projects.toml.
#
# Source of truth: home/projects.toml (the `category` field defines the org set).
# Output: ~/.config/workspace/<org>/ with the artifacts wsenv consumes:
#   env.sh        sourced for env vars (e.g. AZURE_CONFIG_DIR)
#   wrappers/     prepended to PATH (org command wrappers) — created empty for now
#   claude/       --add-dir target (org-only skills) — created empty for now
#
# Consumed by: wsenv (~/.claude/scripts/bin/wsenv), cmux-workspaces.sh (Phase 4).
# Triggered by: home/run_onchange_after_generate-workspace-profiles.sh.tmpl on chezmoi apply.
#
# Idempotent: writes into a tmp tree, then atomically swaps each org dir. Pruning:
# orgs no longer in the registry get their profile dir removed.
#
# Current scope (Phase 3, B&B env-only): only b-and-b/env.sh carries real content
# (AZURE_CONFIG_DIR=~/.azure-bbadmin — the wrong-identity fix). client/personal get
# empty env.sh stubs. Wrappers/skills/MCP/prompt are scaffolded empty for later phases.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REGISTRY="${WSENV_REGISTRY:-$SCRIPT_DIR/../home/projects.toml}"
PROFILE_ROOT="${WSENV_PROFILE_DIR:-$HOME/.config/workspace}"

[[ -f "$REGISTRY" ]] || { echo "generate-workspace-profiles: registry not found: $REGISTRY" >&2; exit 1; }

# Distinct org set from the registry.
ORGS=$(python3 - "$REGISTRY" <<'PY'
import sys, tomllib
d = tomllib.load(open(sys.argv[1], "rb"))
cats = sorted({p.get("category", "personal") for p in d.get("projects", [])})
print("\n".join(cats))
PY
)

# Per-org env content. Keyed by org slug. Add new exports here as phases land.
emit_env() {
  local org="$1"
  case "$org" in
    b-and-b)
      cat <<'EOF'
# Workspace env: b-and-b (generated — edit generate-workspace-profiles.sh, not here)
# Correct default Azure identity for Brown & Brown work (was globally civalent).
export AZURE_CONFIG_DIR="$HOME/.azure-bbadmin"
EOF
      ;;
    priceless)
      cat <<'EOF'
# Workspace env: priceless (generated)
# (no shared priceless env yet — per-repo .env still applies)
EOF
      ;;
    personal)
      cat <<'EOF'
# Workspace env: personal (generated)
# (no shared personal env yet)
EOF
      ;;
    *)
      echo "# Workspace env: $org (generated, empty)"
      ;;
  esac
}

# Build into a tmp root, then swap.
TMP_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/wsprofiles.XXXXXX")"
trap 'rm -rf "$TMP_ROOT"' EXIT

while IFS= read -r org; do
  [[ -z "$org" ]] && continue
  d="$TMP_ROOT/$org"
  mkdir -p "$d/wrappers" "$d/claude"
  emit_env "$org" > "$d/env.sh"
done <<< "$ORGS"

mkdir -p "$PROFILE_ROOT"

# Atomic per-org swap + prune orgs no longer present.
for org in $ORGS; do
  [[ -z "$org" ]] && continue
  src="$TMP_ROOT/$org"
  dst="$PROFILE_ROOT/$org"
  rm -rf "$dst.old" 2>/dev/null || true
  [[ -d "$dst" ]] && mv "$dst" "$dst.old"
  mv "$src" "$dst"
  rm -rf "$dst.old" 2>/dev/null || true
done

# Populate per-org wrapper sets. These shadow native CLIs ONLY when the org's
# workspace is active (wsenv prepends <org>/wrappers/ to PATH). Phase 3.3 scope:
# the bbadmin+SOCKS-proxy `az` belongs to b-and-b — relocated here so the global
# default can become native (priceless) az with no wrapper/guard.
BBADMIN_AZ="$HOME/.local/bin/az"
if [[ -x "$BBADMIN_AZ" ]] && grep -q 'azure-bbadmin' "$BBADMIN_AZ" 2>/dev/null; then
  install -m 0755 "$BBADMIN_AZ" "$PROFILE_ROOT/b-and-b/wrappers/az"
  echo "installed b-and-b/wrappers/az (bbadmin + SOCKS proxy)"
fi

# Prune stale org dirs (present on disk but no longer in the registry).
if [[ -d "$PROFILE_ROOT" ]]; then
  for existing in "$PROFILE_ROOT"/*/; do
    [[ -d "$existing" ]] || continue
    name="$(basename "$existing")"
    if ! grep -qx "$name" <<< "$ORGS"; then
      rm -rf "$existing"
      echo "pruned stale workspace profile: $name"
    fi
  done
fi

echo "workspace profiles generated under $PROFILE_ROOT: $(echo $ORGS | tr '\n' ' ')"
