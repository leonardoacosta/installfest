# Proposal: Restructure repo with .chezmoiroot for onboarding

## Change ID
`restructure-repo-with-chezmoiroot`

## Summary
Use chezmoi's `.chezmoiroot` feature to move all chezmoi-managed files into a `home/` subdirectory, consolidate platform-specific non-deployed dirs into `platform/`, and reduce root clutter from 31 items to ~15 for easier onboarding.

## Context
- Extends: repo root structure, `.chezmoi.toml.tmpl`, `.chezmoiignore`, `run_once_*` and `run_onchange_*` scripts
- Related: no prior specs (first openspec change in this repo)

## Motivation
The repo root mixes chezmoi source files (`dot_*`, `run_*`, `private_dot_*`, `Library/`) with non-deployed tooling (`scripts/`, `ssh-mesh/`, `homebrew/`, `windows/`), documentation, and workflow config (`.beads/`, `.claude/`). A newcomer sees 31 root items with no clear separation of concerns. Team projects (ct, mv, oo, tl) average ~15 root items with clear bucket conventions. This restructure aligns the dotfiles repo with familiar patterns.

## Requirements

### Req-1: Move chezmoi source into home/ subdirectory
Create `.chezmoiroot` at repo root containing `home`. Move all chezmoi-managed items (5 dirs, 2 files, 4 scripts, 2 config files) into `home/`. All chezmoi commands (`apply`, `edit`, `re-add`, `diff`, `managed`) must work transparently after the move.

### Req-2: Fix sourceDir references in chezmoi scripts
Two `run_*` scripts use `{{ .chezmoi.sourceDir }}` to locate `scripts/`. After `.chezmoiroot`, `sourceDir` resolves to `~/dev/if/home/` instead of `~/dev/if/`. Update these to use `{{ .chezmoi.workingTree }}` which resolves to the git repo root.

### Req-3: Consolidate platform directories
Move `homebrew/`, `windows/`, `raycast-scripts/` into `platform/{homebrew,windows,raycast-scripts}`. Update `.chezmoiignore` to ignore `platform/` instead of three separate entries.

### Req-4: Update documentation
Update `CLAUDE.md` directory structure diagram and references to reflect new layout. Update `README.md` if it references file paths.

## Scope
- **IN**: File moves, `.chezmoiroot` creation, template variable fix, `.chezmoiignore` update, doc updates
- **OUT**: Changing shell config behavior, adding new chezmoi-managed files, modifying deployed file content, SSH mesh changes

## Impact
| Area | Change |
|------|--------|
| Repo root | 31 items → ~15 items |
| chezmoi source | Root → `home/` subdirectory |
| Platform dirs | 3 root dirs → `platform/` bucket |
| run_* scripts | `{{ .chezmoi.sourceDir }}` → `{{ .chezmoi.workingTree }}` |
| Daily workflow | Zero change — chezmoi resolves through `.chezmoiroot` |

## Risks
| Risk | Mitigation |
|------|-----------|
| `{{ .chezmoi.workingTree }}` not available in older chezmoi | Verify chezmoi version supports it; fallback to `{{ .chezmoi.sourceDir }}/..` |
| Forgot to move a chezmoi file | Run `chezmoi diff` after migration to verify zero drift |
| Existing clones break on pull | One-time: `chezmoi init` re-reads `.chezmoiroot` automatically |
| Scripts referencing `$DOTFILES/dot_*` paths | Grep confirmed: no scripts reference `$DOTFILES/dot_*` — all use `$DOTFILES/scripts/` |
| `{{ include "projects.toml" }}` in templates breaks | Move `projects.toml` into `home/` (add to `.chezmoiignore`); update script references to `$DOTFILES/home/projects.toml` |
