---
stack: cc-meta
---
<!-- beads:epic:if-yr4o -->
<!-- beads:feature:if-h18t -->

<!-- stack: one of t3 | cc-meta | effect | dotnet — see commands/apply/references/stacks.md § "Stack vocabulary crosswalk" for the full tasks.md-stack:/--stack-profile/detect_stack() mapping. installfest is a bash+chezmoi dotfiles repo with no compilation step, closest functional match is cc-meta (general-purpose dispatch, bash -n + openspec validate gate). -->

# Implementation Tasks

## API Batch

- [x] [1.1] Create `home/run_onchange_after_install-tmux-resurrect.sh.tmpl` cloning `tmux-plugins/tmux-resurrect` into `~/.tmux/plugins/tmux-resurrect`, mirroring `home/run_onchange_after_install-tmux-which-key.sh.tmpl`'s idempotent-clone structure [beads:if-2j6q]
- [x] [1.2] Wire an `if-shell "test -f ~/.tmux/plugins/tmux-resurrect/resurrect.tmux" "run-shell ~/.tmux/plugins/tmux-resurrect/resurrect.tmux"` block into `home/dot_config/tmux/tmux.conf.tmpl`'s existing Plugins section, matching the `tmux-which-key`/`cc-tmux` guard shape [beads:if-brgr]
  - depends on: 1.1
- [x] [1.3] Document the claude-resume limitation in `docs/tmux-layout-keybindings.md`: tmux-resurrect restores window/pane layout + cwd, not a live `claude` conversation — use `claude --resume` in the restored pane [beads:if-5f39]

## E2E Batch

- [ ] [2.1] Verify the install script: run it (or its clone step) against a scratch `~/.tmux/plugins/` dir, confirm `tmux-resurrect/resurrect.tmux` exists at the expected path afterward [beads:if-m2y3]
  - depends on: 1.1
- [ ] [2.2] Verify save/restore end-to-end via scripted tmux automation (`tmux new-session -d`, `send-keys` for `prefix+Ctrl-s`, `kill-server`, new server, `send-keys` for `prefix+Ctrl-r`, `capture-pane`/`list-windows`): confirm window count, window names, and pane working directories match pre-kill state [beads:if-fel6]
  - depends on: 1.2
