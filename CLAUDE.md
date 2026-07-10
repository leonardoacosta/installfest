# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Repository Overview

Personal dotfiles and development environment configuration for macOS and Arch Linux. Cross-platform shell setup with SSH mesh networking between machines.

## Directory Structure

```
if/                                    # repo root (~/dev/personal/installfest)
├── .chezmoiroot                       # points chezmoi at home/ subdirectory
├── home/                              # chezmoi source root (all deployed files)
│   ├── .chezmoi.toml.tmpl             # chezmoi config template (machine-specific data)
│   ├── .chezmoiignore                 # Files excluded from chezmoi management
│   ├── projects.toml                  # Project registry (used by templates + scripts)
│   ├── dot_zshenv.tmpl                # -> ~/.zshenv (env vars, templated)
│   ├── dot_zshrc                      # -> ~/.zshrc (interactive shell entry)
│   ├── dot_zsh/                       # -> ~/.zsh/ (shell modules)
│   │   ├── rc/
│   │   │   ├── shared.zsh             # Options + aliases (cross-platform)
│   │   │   ├── darwin.zsh             # macOS-specific (Homebrew, pnpm)
│   │   │   └── linux.zsh             # Arch-specific (pacman, docker)
│   │   ├── functions/
│   │   │   ├── setup-completions.zsh  # compinit, fpath
│   │   │   ├── load-plugins.zsh      # syntax-hl, autosuggestions
│   │   │   ├── load-tools.zsh        # zoxide, atuin, fzf, mise
│   │   │   └── init-starship.zsh     # prompt (load last)
│   │   └── completions/               # Custom completion scripts
│   ├── dot_config/                    # -> ~/.config/
│   │   ├── ghostty/config.tmpl        # Ghostty terminal (templated)
│   │   ├── starship/starship.toml.tmpl # Starship prompt (templated)
│   │   ├── tmux/tmux.conf             # Tmux config
│   │   └── karabiner/assets/...       # Karabiner-Elements
│   ├── Library/LaunchAgents/          # -> ~/Library/LaunchAgents/ (macOS)
│   ├── run_once_install-packages.sh.tmpl  # One-time package installer
│   └── run_onchange_*.sh.tmpl         # Change-triggered scripts
├── platform/                          # Platform-specific tooling (repo-only)
│   ├── homebrew/                      # Brewfile
│   ├── windows/                       # Windows/CloudPC setup scripts
│   └── raycast-scripts/               # Raycast automation
├── scripts/                           # Utility scripts (repo-only)
├── ssh-mesh/                          # Multi-machine SSH setup (repo-only)
├── docs/                              # Documentation (repo-only)
└── openspec/                          # Change specifications
```

## Key Concepts

### Shell Configuration Flow

```
~/.zshenv (ALL shells)      ~/.zshrc (interactive only)
        │                           │
        ├── DOTFILES export         ├── ~/.zsh/rc/shared.zsh (setopt, aliases)
        ├── PATH setup              ├── ~/.zsh/rc/darwin.zsh OR linux.zsh
        └── Theme exports           ├── ~/.zsh/functions/setup-completions.zsh
                                    ├── ~/.zsh/functions/load-plugins.zsh
                                    ├── ~/.zsh/functions/load-tools.zsh
                                    └── ~/.zsh/functions/init-starship.zsh
```

**Design principle**: `.zshenv` = environment variables ONLY. All tool inits (starship, zoxide, fzf, plugins) happen in `.zshrc` via dedicated function files. Shell sources from deployed paths (`~/.zsh/`), not repo paths.

### Platform Detection

```zsh
case "$(uname -s)" in
  Darwin) source darwin.zsh ;;
  Linux)  source linux.zsh ;;
esac
```

### SSH Mesh

Three-machine setup (Mac, Homelab, Arch) with Tailscale for connectivity. See `ssh-mesh/README.md` for topology and setup.

## Essential Commands

### Deploying Dotfiles (chezmoi)

```bash
chezmoi apply             # Deploy all managed files to ~
chezmoi diff              # Preview what would change
chezmoi managed           # List all managed files
chezmoi edit ~/.zshrc     # Edit source file for ~/.zshrc
chezmoi re-add ~/.zshrc   # Pull changes from deployed file back to source
```

### Installation (first-time setup)

```bash
chezmoi init --source=~/dev/personal/installfest   # Point chezmoi at this repo
chezmoi apply                     # Deploy dotfiles + run install script
```

### Testing Shell Config

```bash
time zsh -i -c exit       # Measure startup time
zsh -xv                   # Trace shell initialization
source ~/.zshrc           # Reload config (or: reload)
```

## Notes for Claude Code

- **Platform-aware**: Check `uname -s` before platform-specific advice
- **No duplicate inits**: Tool inits happen ONCE in `.zshrc`, never in `.zshenv`
- **chezmoi managed**: All dotfiles are deployed via `chezmoi apply`. Source lives in `home/` (via `.chezmoiroot`)
- **dot_ prefix**: `dot_foo` in source deploys to `~/.foo`; `.tmpl` suffix = Go template
- **$DOTFILES**: Set in `.zshenv`, points to `~/dev/personal/installfest` (repo root). Used for `scripts/` references
- **.chezmoiroot**: Tells chezmoi to read source from `home/` — all chezmoi commands resolve transparently
- **Deployed paths**: Shell config sources from `~/.zsh/` (deployed), not `$DOTFILES/home/dot_zsh/` (source)
- **projects.toml**: Lives in `home/` (for chezmoi template `{{ include }}`), scripts reference via `$DOTFILES/home/projects.toml`
