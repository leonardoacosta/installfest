# Project Context

## Purpose

Personal dotfiles and development environment configuration for macOS and Arch Linux. Cross-platform shell setup with SSH mesh networking between machines. Managed by chezmoi.

## Tech Stack

- **Dotfiles Manager**: chezmoi (Go templates, cross-platform)
- **Shell**: zsh with Starship prompt
- **Terminal**: Ghostty (macOS), tmux multiplexer
- **Package Manager**: Homebrew (macOS), pacman (Arch)
- **Tools**: mise (runtime versions), zoxide, atuin, fzf
- **Networking**: Tailscale SSH mesh, SOCKS5 proxy

## Project Conventions

- `dot_` prefix → deploys to `~/.` (chezmoi convention)
- `.tmpl` suffix → Go template (machine-specific)
- `private_dot_` prefix → deployed with restricted permissions
- `run_once_*` → one-time setup scripts
- `run_onchange_*` → re-run when content hash changes
- `$DOTFILES` env var points to repo root (`~/dev/personal/installfest`)

## Machines

| Hostname | OS | Role |
|----------|-----|------|
| leonardoacostas-MacBook-Pro | macOS | Primary dev |
| homelab | Arch Linux | Homelab |
