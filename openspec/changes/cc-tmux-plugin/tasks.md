<!-- beads:epic:if-bqw -->
<!-- beads:feature:if-2aj -->

# Implementation Tasks

## Script Batch

- [x] [1.1] [P-1] Create the `apps/` tree + `apps/cc-tmux/` skeleton: `pyproject.toml` (`requires-python >=3.10`, no deps), `LICENSE` (MIT, Leo), `bin/cc-tmux` (PYTHONPATH shim → `python3 -m cc_tmux`), `src/cc_tmux/{__init__,__main__,cli,parser,paths,log}.py`; `paths.py` detects tmux.conf + the manual clone dir (XDG, `~/.tmux.conf`) [owner:general-purpose] [beads:if-dyg]
- [x] [1.2] [P-1] Implement `src/cc_tmux/tmux.py`: `PaneInfo` dataclass, `get_hop_panes()` single-read, `set_pane_state()` (returns whether `@cc-state` changed), `set_pane_git_identity()` (hot path skips for `active`); pane options `@cc-{state,timestamp,task,wait-reason,project,branch}` [owner:general-purpose] [beads:if-12g]
- [x] [1.3] [P-1] Implement `src/cc_tmux/priority.py`: `STATE_PRIORITY` (waiting 0 > idle 1 > active 2), `PENDING_STATES={waiting,idle}`, group/sort (newest-first within group), `priority` + `flat` cycle modes [owner:general-purpose] [beads:if-0y6]
- [x] [1.4] [P-1] Create the Claude Code plugin manifest: `apps/cc-tmux/.claude-plugin/plugin.json` + `apps/cc-tmux/hooks/hooks.json` mapping SessionStart→idle, UserPromptSubmit→active, PreToolUse[AskUserQuestion/ExitPlanMode]→waiting, PostToolUse/PostToolUseFailure[same]→active, Notification[permission_prompt/elicitation_dialog/idle_prompt], Stop/StopFailure→idle, SessionEnd→clear; every command `timeout:10`; NOT hooking compact/subagent events [owner:general-purpose] [beads:if-8vo]
- [x] [1.5] [P-1] Implement `cli.py` register/cycle/back/switch/discover/clear handlers wiring hooks → `set_pane_state`; auto-discover already-running Claude sessions on load [owner:general-purpose] [beads:if-fso]
- [x] [1.6] [P-2] Implement the notification inbox (`cmd_inbox` + `cc-tmux.tmux` fzf-popup / display-menu fallback): aligned columns, enter=switch, ctrl-x=dismiss (view filter via global cleared-at stamp, never mutates state); self-heal stale state on open when the process scan confirms the Claude is gone [owner:general-purpose] [beads:if-1h2]
- [x] [1.7] [P-2] Implement `src/cc_tmux/notify/` Strategy: `__init__.py` registry + `macos.py`/`linux.py`/`windows.py`; `@cc-notify`/`@cc-focus-app` fire ONLY on a real transition, smart-suppress when terminal (and macOS tab) already focused, per-pane cooldown dedup on the OS-notification path [owner:general-purpose] [beads:if-nfe]
- [x] [1.8] [P-2] Implement status sources: `cmd_status` (counts via `@cc-status-format`), `cmd_status_inbox` (clickable `#[range=pane|<id>]` badges), and `@cc-window-rename` (window name `<state-icon> <dir basename>`, highest-priority icon, `automatic-rename` stays off) [owner:general-purpose] [beads:if-5od]
- [x] [1.9] [P-1] Implement `src/cc_tmux/usage.py` (`cc-tmux usage`): query nexus-agent `http://localhost:7402/credentials`, render `<active_account> 5H:xx% 7D:xx%` with the existing cyan(<0.50)/amber(>=0.50)/red(>0.80) thresholds byte-matching `tmux-nexus-creds`; silent on failure [owner:general-purpose] [beads:if-77l]
- [x] [1.10] [P-2] Implement `src/cc_tmux/conductor.py`: persistent detached session (`@cc-conductor-session`, `exec claude`), popup attach/respawn, four dispatch modes (switch/send-prompt/spawn-task/spawn-in-worktree), live pane-snapshot context injection guarded on `CC_TMUX_CONDUCTOR=1`, conductor session excluded from all pane views; disabled by default [owner:general-purpose] [beads:if-4uq]
- [x] [1.11] [P-2] Author the three bundled skills `apps/cc-tmux/skills/{cc-status,cc-config,cc-dispatch}/SKILL.md`; `cc-dispatch` is the single home of the dispatch CLI shape used by both conductor and ad-hoc sessions [owner:general-purpose] [beads:if-1ox]
- [x] [1.12] [P-1] Write `apps/cc-tmux/cc-tmux.tmux` entrypoint: `if-shell`-guarded load, keybindings (cycle `prefix + o` to avoid the which-key `prefix + Space` collision — see design.md, all `@cc-*-key` overridable), picker `prefix + C-f`, inbox `prefix + i`, back `C-Space` root, conductor `prefix + y`/`Y` when enabled; set `@cc-status`/`@cc-status-inbox`; auto-discover on load [owner:general-purpose] [beads:if-j9t]
- [x] [1.13] [P-2] Implement `src/cc_tmux/testing.py` (`cc-tmux self-test`): priority sort, `set_pane_state` transition detection, path detection — pure functions, no live tmux [owner:general-purpose] [beads:if-1es]

## Config Batch

- [x] [2.1] [P-1] Create `home/run_onchange_after_install-cc-tmux.sh.tmpl` (mirror `run_onchange_after_install-tmux-which-key.sh.tmpl`): `DOTFILES="{{ .chezmoi.workingTree }}"`, source `scripts/utils.sh`, source-guard strict mode; symlink `apps/cc-tmux` → `~/.tmux/plugins/cc-tmux`, run `claude plugin install` from the local clone (idempotent), warn (not fail) if `fzf`/`python3` absent [owner:general-purpose] [beads:if-9ot]
- [x] [2.2] [P-1] Add the `if-shell`-guarded `run-shell ~/.tmux/plugins/cc-tmux/cc-tmux.tmux` load to the Plugins block in `home/dot_config/tmux/tmux.conf.tmpl` (below the which-key block) [owner:general-purpose] [beads:if-9h4]
- [x] [2.3] [P-1] Switch `status-right` in `home/dot_config/tmux/vercel-theme.conf` from `#(tmux-nexus-creds)` to the cc-tmux usage segment; remove `home/dot_local/bin/executable_tmux-nexus-creds` (replaced, no dead duplicate) [owner:general-purpose] [beads:if-z6i]
- [x] [2.4] [P-2] Add `fzf` to `platform/homebrew/Brewfile` and the Arch package list in `scripts/install-arch.sh` if not already present [owner:general-purpose] [beads:if-1pk]
- [x] [2.5] [P-2] Update `docs/tmux-layout-keybindings.md` with the new cc-tmux keybindings (cycle/picker/inbox/back/conductor) and a pointer to `apps/cc-tmux/` [owner:general-purpose] [beads:if-156]

## Verification Batch

- [ ] [3.1] [P-1] Run `chezmoi apply`; verify `~/.tmux/plugins/cc-tmux` resolves, `claude plugin list` shows cc-tmux, and `cc-tmux self-test` passes (paste stdout) [owner:general-purpose] [beads:if-qtc]
- [ ] [3.2] [P-1] Drive a real Claude session in a pane; assert `@cc-state` flips `active`→`waiting`→`idle` across a permission prompt (paste `tmux show-options -p | grep @cc-state` at each step) [owner:general-purpose] [beads:if-dgp]
- [ ] [3.3] [P-1] Verify usage parity: diff the cc-tmux usage segment output against the retired `tmux-nexus-creds` output against a live nexus-agent — must byte-match (paste diff) [owner:general-purpose] [beads:if-zcj]
- [ ] [3.4] [P-1] Verify no `prefix + Space` double-bind: `tmux list-keys | grep -i space` shows which-key's binding intact and cc-tmux cycle on its own key (paste output) [owner:general-purpose] [beads:if-5zl]
- [ ] [3.5] [P-2] Open the inbox with ≥2 tracked panes; assert aligned columns render and `enter` switches to the selected pane [owner:general-purpose] [beads:if-4rk]
- [ ] [3.6] [P-2] Enable `@cc-conductor-enabled on`; open the popup (`prefix + y`); dispatch a `send-prompt` to a target idle pane and assert it arrives (paste evidence); confirm disabled-by-default inertness before enabling [owner:general-purpose] [beads:if-uhr]
- [ ] [3.7] [P-2] Verify graceful degradation: run a `cc-tmux` subcommand outside tmux (exits 0), and with nexus-agent down the usage segment renders empty (paste both) [owner:general-purpose] [beads:if-rwm]
