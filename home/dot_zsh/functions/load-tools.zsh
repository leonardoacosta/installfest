# functions/load-tools.zsh - Modern CLI tool initialization
# Uses command -v guards for defensive loading

# zoxide (smart cd replacement)
if command -v zoxide &>/dev/null; then
  export _ZO_DOCTOR=0  # Suppress config warnings in non-interactive shells
  eval "$(zoxide init zsh)"
  # Only alias cd→z in interactive shells (avoids breaking CI, scripts, Claude Code)
  [[ -o interactive ]] && alias cd="z"
fi

# atuin (better shell history)
if command -v atuin &>/dev/null; then
  eval "$(atuin init zsh --disable-up-arrow)"
  # Keep up arrow for normal history, use Ctrl+R for atuin
fi

# fzf (fuzzy finder)
if command -v fzf &>/dev/null; then
  # Try to load fzf shell integration
  if [[ -f "${HOMEBREW_PREFIX:-/opt/homebrew}/opt/fzf/shell/completion.zsh" ]]; then
    source "${HOMEBREW_PREFIX}/opt/fzf/shell/completion.zsh"
    source "${HOMEBREW_PREFIX}/opt/fzf/shell/key-bindings.zsh"
  elif [[ -f "/usr/share/fzf/completion.zsh" ]]; then
    source "/usr/share/fzf/completion.zsh"
    source "/usr/share/fzf/key-bindings.zsh"
  else
    # Fallback: let fzf generate its own bindings
    source <(fzf --zsh) 2>/dev/null || true
  fi

  # Restore atuin Ctrl+R — fzf key-bindings.zsh overwrites it, reclaim here
  bindkey '^R' atuin-search

  # fzf configuration
  export FZF_DEFAULT_OPTS="--height 40% --layout=reverse --border --info=inline"
  export FZF_CTRL_T_OPTS="--preview 'bat -n --color=always {} 2>/dev/null || cat {}'"
  export FZF_ALT_C_OPTS="--preview 'tree -C {} | head -100'"

  # Use fd if available (faster than find)
  if command -v fd &>/dev/null; then
    export FZF_DEFAULT_COMMAND="fd --type f --hidden --follow --exclude .git"
    export FZF_CTRL_T_COMMAND="$FZF_DEFAULT_COMMAND"
    export FZF_ALT_C_COMMAND="fd --type d --hidden --follow --exclude .git"
  fi
fi

# mise (polyglot version manager - replaces nvm, pyenv, rbenv)
if command -v mise &>/dev/null; then
  eval "$(mise activate zsh)"
fi

# thefuck (command correction)
if command -v thefuck &>/dev/null; then
  eval "$(thefuck --alias)"
fi

# bat (better cat)
if command -v bat &>/dev/null; then
  alias cat="bat --paging=never"
  alias catp="bat"  # With paging
fi

# lsd (better ls: icons + hyperlinks + colors)
if command -v lsd &>/dev/null; then
  alias ls="lsd --icon=auto --hyperlink=auto --group-directories-first"
  alias la="lsd -la --icon=auto --hyperlink=auto --group-directories-first --git"
  alias lt="lsd --tree --depth=2 --icon=auto --hyperlink=auto --group-directories-first"
  # tree view, default depth 1; override with e.g. `ll --depth=3`
  alias ll='lsd -la --tree --depth=1 --icon=auto --hyperlink=auto --group-directories-first --blocks=name,date,size --date="+%a %m/%d/%y %I:%M %p"'
fi

# ripgrep config
if command -v rg &>/dev/null; then
  export RIPGREP_CONFIG_PATH="$HOME/.config/ripgrep/config"
fi
