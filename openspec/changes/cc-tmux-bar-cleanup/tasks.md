<!-- beads:epic:if-bqw -->
<!-- beads:feature:if-q0g -->

# Tasks: cc-tmux-bar-cleanup

## DB Batch

- [x] 1.1 cc repo — `~/.claude/scripts/bin/roadmap-pulse` `--line` mode: emit full-word counts [beads:if-uye]
  (`N open, M unarchived`) instead of `No,Mu`, and delete the `radar:stale` token append.
  Verify with a direct run: paste `roadmap-pulse --line` stdout for the `if` project showing
  the new format and no radar token. Commit + push in the cc repo (ad-hoc lane).
  - touches: `~/.claude/scripts/bin/roadmap-pulse`
- [x] 1.2 nx repo — `apps/nexus-statusline/src/index.ts`: extend the per-pane [beads:if-baf]
  `session-context.<pane>.json` writer (~line 655-684) to include the model family letter
  (reuse the letter `modelEffortToken` derives; write letter only, not effort). Update/extend
  the writer's vitest coverage; paste passing test output. Commit in nx (push handled in 4.1).
  - touches: `apps/nexus-statusline/src/index.ts`

## API Batch

- [x] 2.1 `apps/cc-tmux/src/cc_tmux/cli.py`: replace `_read_session_context_pct` with a [beads:if-elf]
  `_read_session_context` helper returning the model letter (and pct if trivially kept) from
  `session-context.<pane>.json`; `cmd_session_bar` sources the model letter from it. Remove the
  SessionStart model path: `_model_letter`, the `set_pane_model` call in `cmd_register`, and the
  `_active_usage` right-side plumbing.
  - touches: `apps/cc-tmux/src/cc_tmux/cli.py`
- [x] 2.2 `apps/cc-tmux/src/cc_tmux/tmux.py`: remove `OPT_MODEL` / `set_pane_model` (no longer [beads:if-wwu]
  written or read).
  - touches: `apps/cc-tmux/src/cc_tmux/tmux.py`
- [x] 2.3 `apps/cc-tmux/src/cc_tmux/render.py`: `render_session_bar` drops the [beads:if-9v1]
  `account_label`/`ses_pct`/`five_h_pct`/`seven_d_pct` params and the `#[align=right]` usage
  block (left side only). `render_beads_bar` joins multi-line pulse content onto one row with a
  ` | ` separator (dim), keeping the `next:` cyan highlight; single-line content renders as
  today.
  - touches: `apps/cc-tmux/src/cc_tmux/render.py`
- [x] 2.4 `apps/cc-tmux/src/cc_tmux/testing.py`: update self-test cases for the new [beads:if-nh2]
  `render_session_bar` signature (left-only, no-model fail-open) and `render_beads_bar`
  (two-line join, single-line passthrough, empty). Add a fixture-file case for
  `_read_session_context`. Run the self-test; paste passing output.
  - touches: `apps/cc-tmux/src/cc_tmux/testing.py`

## UI Batch

- [x] 3.1 Remove the `#(~/.tmux/plugins/cc-tmux/bin/cc-tmux usage)` segment from `status-right` [beads:if-2jg]
  in `tokyo-night-abyss-theme.conf`, `vercel-theme.conf`, and `one-hunter-vercel-theme.conf`
  (nord has none). Do not otherwise reflow the status-right layout.
  - touches: `home/dot_config/tmux/tokyo-night-abyss-theme.conf`, `home/dot_config/tmux/vercel-theme.conf`, `home/dot_config/tmux/one-hunter-vercel-theme.conf`

## E2E Batch

- [ ] 4.1 Deploy: nx repo push with SKIP_DEPLOY=1 + rebuild/reinstall nexus-statusline [beads:if-kz1]
  (snapshot first, prior spec task 4.4 pattern); `chezmoi apply` for theme confs; reinstall the
  cc-tmux plugin copy if the installer does not symlink. Verify nexus-statusline service healthy
  post-restart.
- [ ] 4.2 Live verification with pasted evidence: (a) `session-context.<pane>.json` now carries [beads:if-me6]
  the model letter (cat the file); (b) row 2 shows `◉ F if > main`-style render with NO
  account/SES/5H/7D (capture `tmux display-message` or screenshot); (c) row 3 shows
  `next: … | N open, M unarchived` with no `radar:stale`; (d) row 1 status-right no longer
  shows the account/usage segment; (e) `cc-tmux usage` still renders when invoked manually.
- [ ] 4.3 cc-tmux self-test run in the deployed location; paste passing output. [beads:if-rx6]
