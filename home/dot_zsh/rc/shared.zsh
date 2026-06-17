# rc/shared.zsh - Common shell configuration for all platforms
# Sourced by .zshrc on all platforms
#
# NOTE: Tool inits (compinit, starship, zoxide, fzf, plugins) are handled
# by dedicated files in ~/.zsh/functions/ - sourced from .zshrc ONLY.

# ============================================================
# Shell Options
# ============================================================

# History configuration
HISTFILE="${HOME}/.zsh_history"
HISTSIZE=100000
SAVEHIST=100000

# History options
setopt HIST_IGNORE_ALL_DUPS    # Remove older duplicate entries
setopt SHARE_HISTORY           # Share history between sessions
setopt INC_APPEND_HISTORY      # Add commands immediately
setopt HIST_REDUCE_BLANKS      # Remove superfluous blanks
setopt HIST_VERIFY             # Show command before executing from history
setopt EXTENDED_HISTORY        # Save timestamp and duration
setopt HIST_IGNORE_SPACE       # Don't save commands prefixed with space

# Directory navigation
setopt AUTO_CD                 # cd by typing directory name
setopt AUTO_PUSHD              # Push directories to stack
setopt PUSHD_IGNORE_DUPS       # Don't push duplicates
setopt PUSHD_SILENT            # Don't print stack after pushd/popd

# Completion
setopt COMPLETE_IN_WORD        # Complete from cursor position
setopt ALWAYS_TO_END           # Move cursor to end after completion
setopt AUTO_MENU               # Show menu on second tab

# Globbing
setopt EXTENDED_GLOB           # Extended globbing syntax
setopt NO_CASE_GLOB            # Case insensitive globbing

# Prompt
setopt PROMPT_SUBST            # Enable prompt substitution

# Input mode — lock emacs so ESC prefix (Option+Left) never activates vi-cmd keymap
bindkey -e

# ============================================================
# Common Aliases
# ============================================================

alias claude="claude --dangerously-skip-permissions --fallback-model claude-sonnet-4-6"
alias cs="~/dev/ccswitch.sh --switch"
alias ll="ls -lah"
alias la="ls -A"
alias l="ls -CF"
alias ..="cd .."
alias ...="cd ../.."
alias ....="cd ../../.."

# Git aliases
alias gs="git status"
alias gd="git diff"
alias gl="git log --oneline -20"
alias gp="git pull"
alias ga="git add"
alias gc="git commit"
alias gco="git checkout"
alias gb="git branch"

# Safety aliases (interactive shells only — avoids breaking scripts, CI, Claude Code)
if [[ -o interactive ]]; then
  alias rm="rm -i"
  alias cp="cp -i"
  alias mv="mv -i"
fi

# cmux workspaces
alias mux="$DOTFILES/scripts/cmux-workspaces.sh"

# workspace auto-activation: cd into a registered ~/dev/<code> repo -> activate its
# org workspace (env + wrappers PATH) via wsenv. Sticky; also fires once at startup.
[[ -f "$DOTFILES/packages/workspace/integrations/chpwd.zsh" ]] \
  && source "$DOTFILES/packages/workspace/integrations/chpwd.zsh"

# Search aliases — forgive muscle-memory typos for ripgrep
alias rgrep="rg"

# Misc aliases
alias path='echo $PATH | tr ":" "\n"'
alias reload="source ~/.zshrc"

# ============================================================
# Per-Project CLI Routing
# ============================================================
# Project-aware wrappers for CLIs that need different auth/config
# per project. Add new projects as case branches.

# Azure CLI identity is workspace-driven, not a shell function.
#   - Global default `az` resolves to ~/.local/bin/az (BBAdmin + SOCKS proxy;
#     supports --as-admin / --as-o365 / --as-personal).
#   - In the b-and-b workspace, `wsenv` prepends ~/.config/workspace/b-and-b/wrappers
#     to PATH so that same wrapper is used explicitly per-workspace.
#   - Civalent (ct) work: `az --as-personal` (-> ~/.azure-civalent, no proxy).
# The old az() function that hardcoded ct=civalent was a documented arg-mangling
# foot-gun (it word-split --as-admin etc.) and has been removed. Per-org default
# identity now flows from the workspace profile env.sh, not this file.

# SSH — notify on connection failure via nexus TTS
ssh() {
  command ssh "$@"
  local rc=$?
  if [[ $rc -ne 0 ]]; then
    echo "{\"event\":\"notification\",\"message\":\"SSH to $1 failed (exit $rc)\"}" \
      | socat - UNIX-CONNECT:/tmp/nexus-agent.sock 2>/dev/null
  fi
  return $rc
}

# Vercel CLI — per-project token routing
vercel() {
  case "$PWD" in
    */dev/ct|*/dev/ct/*)
      command vercel --token "$VERCEL_TOKEN_PRICELESS_" "$@" ;;
    *)
      command vercel "$@" ;;
  esac
}

# GitKraken CLI — (re)attach the GitHub provider token from the env.
# Thin wrapper over the shared gk_attach_github helper (also used by the chezmoi
# github-auth bootstrap), so the two never drift. Run after a PAT rotation
# (e.g. `gh auth refresh`) so gk drops the stale token.
gkauth() {
  source "${DOTFILES:-$HOME/dev/if}/scripts/gk-github-auth.sh" 2>/dev/null \
    || { print -u2 "gkauth: helper missing at \$DOTFILES/scripts/gk-github-auth.sh"; return 1; }
  gk_attach_github "$GH_TOKEN"
}
# Note: no gkado — GitKraken's CLI doesn't support Azure DevOps PATs ("azure pats
# not yet supported"). Connect gk's ADO provider via the app GUI (OAuth) instead.
# AZURE_DEVOPS_EXT_PAT (from ~/.zshenv) still auto-auths the az devops / ado CLIs.
