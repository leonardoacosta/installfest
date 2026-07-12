# Spec: Repo Restructure

## ADDED Requirements

### Requirement: .chezmoiroot pointer file
A file `.chezmoiroot` MUST exist at repo root containing the single line `home`, redirecting chezmoi's source resolution.

#### Scenario: chezmoi apply after restructure
- Given: `.chezmoiroot` exists with content `home`
- And: all chezmoi source files are in `home/`
- When: user runs `chezmoi apply`
- Then: all files deploy to `$HOME` identically to before

#### Scenario: chezmoi edit resolves through .chezmoiroot
- Given: `.chezmoiroot` exists
- When: user runs `chezmoi edit ~/.zshrc`
- Then: editor opens `~/dev/if/home/dot_zshrc`

### Requirement: platform directory bucket
Non-deployed, platform-specific directories MUST be consolidated under `platform/`.

#### Scenario: platform directory structure
- Given: restructure is applied
- Then: `platform/homebrew/`, `platform/windows/`, `platform/raycast-scripts/` exist
- And: `homebrew/`, `windows/`, `raycast-scripts/` no longer exist at root

## MODIFIED Requirements

### Requirement: chezmoi scripts use workingTree
`run_once_*` and `run_onchange_*` templates SHALL use `{{ .chezmoi.workingTree }}` instead of `{{ .chezmoi.sourceDir }}` for repo root references.

#### Scenario: install script finds utils after restructure
- Given: `run_once_install-packages.sh.tmpl` is in `home/`
- And: `scripts/utils.sh` is at repo root
- When: chezmoi runs the install script
- Then: `DOTFILES` resolves to `~/dev/if` (repo root, not `~/dev/if/home/`)
- And: `$DOTFILES/scripts/utils.sh` is found

#### Scenario: git hooks script finds repo after restructure
- Given: `run_onchange_set-git-hooks.sh.tmpl` is in `home/`
- When: chezmoi runs the hooks script
- Then: `DOTFILES` resolves to `~/dev/if`
- And: `git -C "$DOTFILES" config core.hooksPath` succeeds

### Requirement: chezmoiignore updated for new structure
`.chezmoiignore` MUST move into `home/`; it no longer needs entries for `platform/`, since that directory is outside the chezmoi source root.

#### Scenario: chezmoiignore only lists home-relative paths
- Given: `.chezmoiignore` is in `home/`
- Then: it no longer lists `docs`, `scripts`, `ssh-mesh`, `windows`, `homebrew`, `raycast-scripts`
- Because: those directories are outside `home/` and invisible to chezmoi
