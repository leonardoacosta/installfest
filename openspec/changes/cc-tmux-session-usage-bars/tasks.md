<!-- beads:feature:if-by6 -->

<!-- beads:epic:if-bqw -->

# Tasks: cc-tmux-session-usage-bars

> Literal `## DB/API/UI/E2E Batch` headers per `/feature`'s wave-plan-build contract — mapped by
> domain fit, not a literal database/API/UI/E2E split (this repo has no such layering). See
> design.md § "Batch mapping" for the rationale. Owner: general-purpose engineer agents.

## DB Batch

- [x] [1.1] [P-1] Create `~/dev/personal/nexus/apps/agent/src/services/statusline-usage-file.ts`: read the active credential's row via `getActiveCredentialSnapshot().fingerprint`, write `usage-cache.json` in the exact `CachedUsage` shape nexus-statusline's existing cache reader expects (`{fetched_at, data:{five_hour:{utilization,resets_at}, seven_day:{...}}}`), computing utilization from the stored used/limit columns (fail-soft, never throws) [owner:general-purpose] [type:api] [beads:if-885]
- [x] [1.2] [P-1] Wire the new write helper into `~/dev/personal/nexus/apps/agent/src/index.ts`'s `onTickComplete` callback (the existing `startCredentialUsagePoller` startup block) [owner:general-purpose] [type:api] [beads:if-3cw]
- [x] [1.3] [P-1] In `~/dev/personal/nexus/apps/nexus-statusline/src/index.ts`: add `getPolledUsage()` (reads `usage-cache.json`, no Anthropic call), change `resolveUsage`'s default fetcher argument from `getApiUsage` to `getPolledUsage`, delete `getApiUsage()`/`fetchWithToken()`/`readAccessToken()` [owner:general-purpose] [type:api] [beads:if-3di]
- [x] [1.4] [P-1] In the same file: add `writeSessionContext()` — writes `session-context.<pane>.json` with `{context_used_pct, ts}` on every render, gated on `process.env.TMUX_PANE` being set, fail-soft; call from `main()` after context is resolved [owner:general-purpose] [type:api] [beads:if-pwd]
- [ ] [1.5] [P-2] Add opportunistic GC for `session-context.*.json` (unlink entries older than a few hours, 1-in-N probability, matching `skill-list-dedup.sh`'s prune pattern) so closed panes' files don't accumulate forever [owner:general-purpose] [type:api] [beads:if-ejq]

## API Batch

- [x] [2.1] [P-1] `apps/cc-tmux/src/cc_tmux/tmux.py`: add a session-count helper counting `get_hop_panes()` rows whose `@cc-project` matches a given project name — 0/1/2+ semantics matching nexus-statusline's `◉`/`◌` glyph logic [owner:general-purpose] [type:api] [beads:if-9pp]
- [x] [2.2] [P-1] `apps/cc-tmux/src/cc_tmux/tmux.py`: add `get_window_top_pane(window_target)` mirroring `get_window_top_state()` (same scoped `list-panes -t <window>` shape), returning the highest-priority pane's id instead of its state [owner:general-purpose] [type:api] [beads:if-9cz]
- [x] [2.3] [P-1] `apps/cc-tmux/src/cc_tmux/tmux.py`: add `OPT_MODEL = "@cc-model"` to the tracked pane-option table and `_ALL_OPTS` (cleared on `SessionEnd` like the other tracked options), plus `set_pane_model(pane_id, model)` mirroring `set_pane_title()` [owner:general-purpose] [type:api] [beads:if-23z]
- [x] [2.4] [P-1] `apps/cc-tmux/src/cc_tmux/cli.py`: extend `cmd_register()`'s existing `_read_hook_stdin()` handling — when `hook_event_name == "SessionStart"`, also extract `model` and store via `tmux.set_pane_model()`; map to a single letter (Fable=F, Opus=O, Haiku=H, Sonnet=S — Sonnet added, the original ask's F/O/H list omitted it) [owner:general-purpose] [type:api] [beads:if-isq]
- [x] [2.5] [P-1] New reader (in `apps/cc-tmux/src/cc_tmux/cli.py` or a new small module) reading `~/.claude/scripts/state/roadmap-pulse.<code>.line` for a project code resolved via `registry.resolve_project_code()` — returns the raw cached content or `""` on any error (missing file, unreadable, empty) [owner:general-purpose] [type:api] [beads:if-fzs]
- [x] [2.6] [P-1] `apps/cc-tmux/src/cc_tmux/render.py`: add `render_session_bar(session_count, model_letter, project, branch, account_label, ses_pct, five_h_pct, seven_d_pct)` — pure, left+right composition matching the mockup in `docs/diagrams/cc-tmux-sources.html`, reusing `usage.py`'s `color_for`/`pct_for` for the SES/5H/7D gauges [owner:general-purpose] [type:api] [beads:if-wu6]
- [x] [2.7] [P-1] `apps/cc-tmux/src/cc_tmux/render.py`: add `render_beads_bar(pulse_line)` — pure, formats the roadmap-pulse content into the row-3 tmux-styled string, empty string when there's nothing pending [owner:general-purpose] [type:api] [beads:if-vm2]
- [x] [2.8] [P-1] `apps/cc-tmux/src/cc_tmux/cli.py` + `parser.py`: add `cmd_session_bar(args)` (resolves the window's representative pane via `get_window_top_pane()`, reads `@cc-model`/`@cc-project`/`@cc-branch`, reads `session-context.<pane>.json`, calls existing `usage.py` account+5H/7D logic, calls `render_session_bar()`, prints) and `cmd_beads_bar(args)` (resolves project code, calls the roadmap-pulse reader, calls `render_beads_bar()`, prints) as two new subcommands, registered in `_DISPATCH` [owner:general-purpose] [type:api] [beads:if-780]

## UI Batch

- [x] [3.1] [P-1] `home/dot_config/tmux/tmux.conf.tmpl`: add `set -g status 3` (companion to the existing `status-interval`/`status-justify`/`status-position` lines) [owner:general-purpose] [type:config] [beads:if-5bz]
- [x] [3.2] [P-1] `home/dot_config/tmux/vercel-theme.conf`: add `status-format[1]` (`#(~/.tmux/plugins/cc-tmux/bin/cc-tmux session-bar #{window_id})`) and `status-format[2]` (`#(~/.tmux/plugins/cc-tmux/bin/cc-tmux beads-bar #{window_id})`), styled with this theme's real hex palette [owner:general-purpose] [type:config] [beads:if-e5p]
- [x] [3.3] [P-2] Same for `home/dot_config/tmux/one-hunter-vercel-theme.conf` [owner:general-purpose] [type:config] [beads:if-yf0]
- [x] [3.4] [P-2] Same for `home/dot_config/tmux/tokyo-night-abyss-theme.conf` [owner:general-purpose] [type:config] [beads:if-dkg]
- [x] [3.5] [P-2] Same for `home/dot_config/tmux/nord-theme.conf` [owner:general-purpose] [type:config] [beads:if-0hq]

## E2E Batch

- [x] [4.1] [P-1] Extend `apps/cc-tmux/src/cc_tmux/testing.py`: self-test cases for the session-count helper, `cmd_register()`'s model capture, `get_window_top_pane()`, `render_session_bar()`, `render_beads_bar()`, and the roadmap-pulse reader's fail-open behavior (missing file, malformed content). Run `cc-tmux self-test` and paste the passing stdout [owner:general-purpose] [type:testing] [beads:if-x7j]
- [ ] [4.2] [P-1] Live verification, row 2: after deploying, observe a real tmux pane's row 2 render session-count/model-letter/project/branch on the left and account+SES/5H/7D on the right with real data — paste the observed output (`cc-tmux session-bar` direct call or the rendered status line) [owner:general-purpose] [type:testing] [beads:if-2jq]
- [x] [4.3] [P-1] Live verification, row 3: observe row 3 render this project's real `roadmap-pulse.if.line` content — paste observed output [owner:general-purpose] [type:testing] [beads:if-9d6]
- [ ] [4.4] [P-1] nx-repo deploy per design.md's Deploy Safety section: push nexus-agent changes with `SKIP_DEPLOY=1`, verify `usage-cache.json` writes correctly on the next poll tick (paste file content), then deliberately rebuild + restart nexus-agent; separately snapshot `~/.local/bin/nexus-statusline`, `bun run build` + reinstall, verify a live pane's statusline still renders with no regression and `session-context.<pane>.json` appears — paste evidence for each step [owner:general-purpose] [type:infra] [beads:if-t11]
- [ ] [4.5] [P-2] Update `docs/diagrams/cc-tmux-sources.html`'s "Planned Architecture" section to reflect the shipped state — drop "PLANNED — NOT YET IMPLEMENTED" badges for what's now live, replace illustrative mockup values with real captured ones [owner:general-purpose] [type:docs] [beads:if-2nh]
