# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Repository Overview

Personal dotfiles and development environment configuration for macOS and Arch Linux. Cross-platform shell setup with SSH mesh networking between machines.

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
