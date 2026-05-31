# workspace chpwd integration — auto-activate the org workspace on cd into a repo.
#
# Sourced from shared.zsh. On `cd` into any registered ~/dev/<code> repo (or a
# subdir of one), runs `eval "$(wsenv)"` so the shell falls into that org's
# workspace: WS_WORKSPACE, AZURE_CONFIG_DIR, the org wrapper PATH, etc.
#
# Behavior (per Leo 2026-05-31):
#   - Sticky: cd into a registered repo activates it; cd to an unregistered dir
#     leaves the current workspace in place (no deactivate).
#   - Fires on every cd (cheap: early-returns unless under ~/dev) AND once at
#     shell startup (so a shell born inside ~/dev/<code> is already activated).
#
# Requires `wsenv` on PATH (deployed by the workspace package's symlink).

_ws_chpwd() {
  # Fast guard: do nothing unless we're under ~/dev (keeps cd elsewhere free).
  case "$PWD/" in
    "$HOME/dev/"*) ;;
    *) return 0 ;;
  esac

  command -v wsenv >/dev/null 2>&1 || return 0

  # Only activate if the cwd resolves to a registered repo. wsenv --org exits
  # non-zero / empty otherwise — in that case keep the current workspace (sticky).
  local _org
  _org="$(wsenv --org 2>/dev/null)" || return 0
  [[ -n "$_org" ]] || return 0

  # Skip the eval if we're already in this workspace (avoids redundant PATH churn).
  [[ "${WS_WORKSPACE:-}" == "$_org" ]] && return 0

  eval "$(wsenv 2>/dev/null)"
}

# Register on cd + run once now (chpwd does not fire for the shell's initial pwd).
autoload -Uz add-zsh-hook 2>/dev/null
add-zsh-hook chpwd _ws_chpwd 2>/dev/null
_ws_chpwd
