---
order: 0721a
---

# Proposal: Add tmux-resurrect session persistence

## Change ID
`add-tmux-resurrect`

## Summary
Install tmux-resurrect via the existing manual-plugin pattern so tmux window/pane layout and
working directories survive a server crash or machine reboot, closing a gap flagged in the
2026-03-25 tmux audit (Finding I4) and never addressed since.

## Context
- Extends: `home/dot_config/tmux/tmux.conf.tmpl` (plugin `if-shell` guard block, mirrors the
  `tmux-which-key`/`cc-tmux` blocks already there)
- Related: `docs/audit/tmux.md` Finding I4 / § "Important: Session Persistence
  (tmux-resurrect / tmux-continuum)"; `docs/audit/ecosystem-comparison.md:31`;
  `docs/recon/qeesung-tmux-scout.md` (cc-tmux's own separate pane-tracking persistence gap,
  out of scope here)
- touches: `home/dot_config/tmux/tmux.conf.tmpl`, `home/run_onchange_after_install-tmux-resurrect.sh.tmpl`, `docs/tmux-layout-keybindings.md`

## Motivation
No mechanism restores a tmux session (window layout, pane cwd, window names) after the tmux
server dies — crash, `tmux kill-server`, or a homelab reboot. Flagged as "Important" in the
March 2026 audit and unaddressed for 4 months (confirmed via `git log` on
`home/dot_config/tmux/tmux.conf.tmpl` — no resurrect/continuum-shaped commit ever landed). This
repo already has a proven, no-TPM manual install pattern (`tmux-which-key`, `cc-tmux`) for
exactly this kind of plugin; tmux-resurrect is a single-script plugin that fits it directly.

## Requirements

### Requirement: tmux-resurrect is installed via the manual no-TPM pattern
A new chezmoi `run_onchange_after_install-tmux-resurrect.sh.tmpl` script clones
`tmux-plugins/tmux-resurrect` into `~/.tmux/plugins/tmux-resurrect`, mirroring
`run_onchange_after_install-tmux-which-key.sh.tmpl`'s structure (idempotent clone, no manual
step beyond `chezmoi apply`).

#### Scenario: fresh machine installs the plugin on first apply
- Given: a machine has never run this repo's chezmoi config before
- When: `chezmoi apply` runs
- Then: `~/.tmux/plugins/tmux-resurrect/resurrect.tmux` exists on disk

### Requirement: tmux.conf.tmpl loads the plugin behind an if-shell guard
`home/dot_config/tmux/tmux.conf.tmpl` gains an `if-shell "test -f
~/.tmux/plugins/tmux-resurrect/resurrect.tmux" "run-shell
~/.tmux/plugins/tmux-resurrect/resurrect.tmux"` block in the existing Plugins section, matching
the guard shape already used for `tmux-which-key` and `cc-tmux` — a machine that hasn't run the
install script yet does not fail to load the rest of the config.

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
`docs/tmux-layout-keybindings.md` states plainly that tmux-resurrect restores window/pane layout
and working directories, but does NOT resume a live `claude` conversation — a restored pane
re-runs its last shell command, starting a new Claude Code session rather than continuing the
old one. Use `claude --resume` (or equivalent) in the restored pane to pick the conversation
back up.

#### Scenario: operator reads the caveat before relying on restore for an in-progress Claude session
- Given: `docs/tmux-layout-keybindings.md`
- When: the operator reads the tmux-resurrect keybindings section
- Then: the doc states the restore-does-not-resume-claude limitation and the `claude --resume`
  workaround

## Scope
- **IN**: tmux-resurrect install script + wiring, doc note on the claude-resume caveat.
- **OUT**: tmux-continuum (auto-save/restore) — deferred; its auto-restore-on-tmux-start could
  race cc-tmux's own window-rename job (`@cc-window-rename`), an untested interaction that
  deserves its own verification pass. cc-tmux's own AI-pane attention-tracking persistence
  (already self-heals via process-scan on inbox open — not a real gap). Any change to
  `cmux-workspaces.sh`.

## Done Means
- Operator can crash/kill the tmux server (or reboot the machine) and use `prefix+Ctrl-s` before
  / `prefix+Ctrl-r` after to restore window layout, pane working directories, and window names.
- tmux-resurrect installs cleanly via `chezmoi apply` on a fresh machine with no manual step
  beyond the existing chezmoi flow (same UX as `tmux-which-key`/`cc-tmux`).
- `docs/tmux-layout-keybindings.md` documents the claude-resume caveat so a restored pane's
  limitation isn't a surprise.

## Testing
| Affected seam | Unit task | E2E task |
|----------------|-----------|----------|
| `home/run_onchange_after_install-tmux-resurrect.sh.tmpl` (install script) | N/A — chezmoi `run_onchange` scripts have no unit-test harness in this repo (same as `tmux-which-key`'s install script) | `[2.1]` scripted verification that the clone lands at the expected path |
| `home/dot_config/tmux/tmux.conf.tmpl` (plugin wiring + save/restore) | N/A — tmux config has no unit-test harness | `[2.2]` scripted `tmux send-keys`/`capture-pane` round-trip: save a known layout, kill the server, restore, diff window/pane state |
| `docs/tmux-layout-keybindings.md` (doc note) | N/A — doc-only change | N/A — doc-only change |

## Impact
| Area | Change |
|------|--------|
| tmux config | New plugin wired behind existing if-shell guard pattern |
| chezmoi | New `run_onchange_after_install-tmux-resurrect.sh.tmpl` |
| docs | One new paragraph in `docs/tmux-layout-keybindings.md` |

## Risks
| Risk | Mitigation |
|------|-----------|
| Resurrect's default save/restore keys (`prefix+Ctrl-s`/`prefix+Ctrl-r`) could collide with an existing binding | Verified — grepped `tmux.conf.tmpl` for `C-s`/`C-r`, no existing binding uses either; `prefix+r` (bare `r`, reload config) is a different keystroke and unrelated |
| Restoring a pane re-runs its last shell command instead of resuming Claude Code conversation state | Documented as a known limitation in `docs/tmux-layout-keybindings.md`, not silently implied to "just work" |
