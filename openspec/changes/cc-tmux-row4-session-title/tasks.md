---
stack: cc-meta
---
<!-- beads:epic:if-bqw -->
<!-- beads:feature:if-cc88 -->

# Tasks: cc-tmux-row4-session-title

> Literal `## DB/API/UI/E2E Batch` headers per `/feature`'s wave-plan-build contract. Owner:
> general-purpose engineer agents. Verification is `python -c` function-level stdout + live
> `cc-tmux render-all` / theme capture — no pytest harness exists in this plugin.

## DB Batch

- [ ] [1.1] `apps/cc-tmux/src/cc_tmux/render.py`: add a pure `render_agents_row(title, bg_entries, now, busy_window, client_width)` (name per existing row-renderer convention) implementing the content contract: nonempty sub-agent activity -> glyph strip, one glyph per background entry in launch order (`◌`<->`○` wall-clock parity pulse inside the busy window, static `●` past it); else `@cc-title` text truncated to client width; else empty string. No I/O, mirrors `render_session_bar`'s pure-function shape. [beads:if-fm8p]
- [ ] [1.2] Function-level verification (paste stdout): `python -c` cases — title only -> title; one busy entry -> `◌` and `○` across two parities; one settled entry -> `●` static; mixed two entries -> strip in launch order; nothing -> empty string. [beads:if-n31t]

## API Batch

- [ ] [2.1] `apps/cc-tmux/src/cc_tmux/tmux.py`: register the new option names (`@cc-row-agents` global, `@cc-subagent-bg-busy-window` tunable with default below the existing bg age-out timeout), following the existing option-name constants pattern. [beads:if-223e]
- [ ] [2.2] `apps/cc-tmux/src/cc_tmux/cli.py`: in `cmd_render_all`, resolve the focused window's representative pane title + bg entries (existing pruning helper), call the new renderer, publish `@cc-row-agents`; extend `_publish_multirow_status` arithmetic to `tab_rows + 2 + (1 if payload else 0)` capped at 5, dropping the agents row first at the cap (portrait 3-row tab wrap keeps session/beads). [beads:if-5rsn]
- [ ] [2.3] Verification (paste stdout): run `cc-tmux render-all <window_id> <w> <h>` against a titled foreground-only pane and against a pane with a fresh bg entry; paste the published `@cc-row-agents` values (tmux show-options -gv) for both, plus the resulting `status` line count in each case. [beads:if-vkl3]

## UI Batch

- [ ] [3.1] `home/dot_config/tmux/tmux.conf.tmpl`: document/keep base `status 3` and add the agents-row comment block alongside the existing computed-index notes (render job owns the live count — confirm no static `status 4` is introduced). [beads:if-lhhw]
- [ ] [3.2] All four theme files (`vercel-theme.conf`, `one-hunter-vercel-theme.conf`, `tokyo-night-abyss-theme.conf`, `nord-theme.conf`): extend the `@cc-tab-rows` computed-index conditional chain with the `status-format[N+2]` agents-row mapping styled consistently with each theme's session/beads rows (DIM-leaning; title text unstyled, glyphs inherit theme accent). [beads:if-fwgy]
- [ ] [3.3] Deploy via `chezmoi apply` (halt and surface if unrelated drift blocks it, per repo memory `project_chezmoi_apply_blocked_by_unrelated_drift`) and reload the live plugin (`tmux run-shell ~/.tmux/plugins/cc-tmux/cc-tmux.tmux`). [beads:if-jljn]

## E2E Batch

- [ ] [4.1] Live acceptance (paste captures): titled session, no agents -> row 4 shows title at `status-format[3]` landscape and `status` is 4; no title, no agents -> `status` back to 3, no blank row; background dispatch -> pulsing `◌`/`○` across two parity captures, then `●` after the busy window elapses; portrait with 3 tab rows -> `status` capped at 5, agents row absent, session/beads intact. [beads:if-di8t]
- [ ] [4.2] Theme sweep (paste one capture per theme): switch each of the four themes and confirm the agents row renders at the computed index with no default pane-list fallback row appearing. [beads:if-wccb]
