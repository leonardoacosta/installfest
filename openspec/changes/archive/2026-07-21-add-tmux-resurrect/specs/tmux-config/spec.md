# tmux-config Specification

## Purpose
Manual, no-TPM plugin-managed tmux configuration for this dotfiles repo (chezmoi-deployed
`home/dot_config/tmux/tmux.conf.tmpl` + per-plugin `run_onchange_after_install-*.sh.tmpl`
install scripts).

## ADDED Requirements

### Requirement: tmux-resurrect is installed via the manual no-TPM pattern
A new chezmoi `run_onchange_after_install-tmux-resurrect.sh.tmpl` script SHALL clone
`tmux-plugins/tmux-resurrect` into `~/.tmux/plugins/tmux-resurrect`, mirroring
`run_onchange_after_install-tmux-which-key.sh.tmpl`'s structure (idempotent clone, no manual
step beyond `chezmoi apply`).

#### Scenario: fresh machine installs the plugin on first apply
- Given: a machine has never run this repo's chezmoi config before
- When: `chezmoi apply` runs
- Then: `~/.tmux/plugins/tmux-resurrect/resurrect.tmux` exists on disk

### Requirement: tmux.conf.tmpl loads the plugin behind an if-shell guard
`home/dot_config/tmux/tmux.conf.tmpl` SHALL gain an `if-shell "test -f
~/.tmux/plugins/tmux-resurrect/resurrect.tmux" "run-shell
~/.tmux/plugins/tmux-resurrect/resurrect.tmux"` block in the existing Plugins section, matching
the guard shape already used for `tmux-which-key` and `cc-tmux` — a machine that hasn't run the
install script yet MUST NOT fail to load the rest of the config.

#### Scenario: tmux config loads cleanly before the plugin is installed
- Given: `~/.tmux/plugins/tmux-resurrect/resurrect.tmux` does not exist on disk
- When: tmux starts and sources `tmux.conf`
- Then: the config loads without error, and the resurrect block is a silent no-op

#### Scenario: save/restore round-trips a window layout after the plugin is installed
- Given: the plugin is installed and tmux is running
- When: the operator saves state (`prefix+Ctrl-s`), kills the tmux server, restarts tmux, and
  restores state (`prefix+Ctrl-r`)
- Then: window count, window names, and pane working directories match the pre-kill state

### Requirement: the claude-resume limitation is documented
`docs/tmux-layout-keybindings.md` SHALL state plainly that tmux-resurrect restores window/pane
layout and working directories, but does NOT resume a live `claude` conversation — a restored
pane re-runs its last shell command, starting a new Claude Code session rather than continuing
the old one. It SHALL document that `claude --resume` (or equivalent) in the restored pane is
required to pick the conversation back up.

#### Scenario: operator reads the caveat before relying on restore for an in-progress Claude session
- Given: `docs/tmux-layout-keybindings.md`
- When: the operator reads the tmux-resurrect keybindings section
- Then: the doc states the restore-does-not-resume-claude limitation and the `claude --resume`
  workaround
