# cc-tmux Bar Cleanup + Reliable Model Letter

Follow-up to `2026-07-11-cc-tmux-session-usage-bars`, driven by live feedback on the shipped
three-row status bar (screenshot review, 2026-07-11 14:42).

## Why

Four display defects were confirmed live within minutes of the prior spec shipping:

1. **Row 3 is cryptic**: `0o,2u,radar:stale` — the `o`/`u` abbreviations exist for a 40-char
   statusline budget, but full words (`0 open, 2 unarchived` = 20 chars) fit that budget anyway,
   and `radar:stale` is noise the user explicitly does not want surfaced on every bar.
2. **Row 3 drops its lead line**: the roadmap-pulse cache file has TWO lines (`next: …` +
   counts) but tmux `#()` renders only one — the actionable `next:` line is silently lost.
3. **The model letter never renders**: `@cc-model` is written only from the SessionStart hook
   payload's `model` field. Live probe across 7 tracked panes: `@cc-state` and `@cc-title` are
   set (hook stdin parsing works) yet `@cc-model` is empty everywhere — the field is
   absent/unusable at SessionStart. The path also misses `/model` mid-session switches by
   design, since no SessionStart fires.
4. **Claude usage stats duplicate across bars**: account label + SES/5H/7D render in the tmux
   tabs-bar `status-right` AND row 2 AND the in-pane Claude statusline. User decision: Claude
   usage stats live ONLY in the Claude statusline (nexus-statusline); tmux rows keep
   session/project identity only.

## What Changes

- **`roadmap-pulse --line` (cc repo)**: emit full-word counts (`N open, M unarchived`) and drop
  the `radar:stale` token from `--line` mode. Single-source fix — both consumers
  (nexus-statusline row and cc-tmux row 3) render the readable form.
- **cc-tmux `beads-bar`**: join both cached pulse lines onto one full-width row
  (`next: … | N open, M unarchived`) instead of losing everything after line 1.
- **nexus-statusline (nx repo)**: extend the per-pane `session-context.<pane>.json` writer to
  include the model family letter it already computes (`modelEffortToken`), alongside
  `context_used_pct`.
- **cc-tmux `session-bar`**: source the model letter from `session-context.<pane>.json`
  (fresh every statusline render, tracks `/model` switches). Remove the SessionStart-payload
  model path (`_model_letter`, `set_pane_model`, `@cc-model`) — confirmed broken, now
  redundant. Drop the right side of row 2 entirely (account label, SES/5H/7D gauges); keep the
  left side (session glyph, model letter, project > branch).
- **Theme confs**: remove the `#(cc-tmux usage)` segment from `status-right` in the three
  themes that wire it (tokyo-night-abyss, vercel, one-hunter-vercel). The `usage` subcommand
  itself is retained — still invocable on demand — it just loses its tabs-bar wiring.

## Non-Goals

- **nexus-agent data bugs are out of scope**, filed separately in the nx repo:
  `nx-6uzqi` (activeFingerprint stale after CC account switch), `nx-8ahjt` (usage poller 100%
  failure — `usagePolledAt` null everywhere), `nx-h5ur8` (credential store pollution: 2,708
  `/credentials` entries, 76 real). Those are why the account label and 5H/7D values were wrong
  or `--`; this spec removes those fields from tmux rows regardless, so the tmux surface no
  longer depends on them.
- No new status rows, no row-1 layout changes beyond deleting the usage segment, no changes to
  the animated tab icon or state tracking.
- No deletion of the `usage` subcommand or `usage.py` (user decision: keep invocable).

## Context

- touches: `apps/cc-tmux/src/cc_tmux/cli.py`, `apps/cc-tmux/src/cc_tmux/render.py`, `apps/cc-tmux/src/cc_tmux/tmux.py`, `apps/cc-tmux/src/cc_tmux/testing.py`, `home/dot_config/tmux/tokyo-night-abyss-theme.conf`, `home/dot_config/tmux/vercel-theme.conf`, `home/dot_config/tmux/one-hunter-vercel-theme.conf`

Cross-repo files (not part of this repo's conflict matrix, executed as tasks against their own
repos exactly like the prior spec's task 4.4 nx deploy):

- cc repo: `~/.claude/scripts/bin/roadmap-pulse` (`--line` mode only)
- nx repo: `apps/nexus-statusline/src/index.ts` (session-context writer + its test file)

Root causes were verified live during `/openspec:explore` (2026-07-11): pulse format traced to
`roadmap-pulse` lines ~385-389; `@cc-model` empty on all 7 live panes while `@cc-title` set on
the probe pane; `#(cc-tmux usage)` wired in 3 of 4 themes.

## Testing

| Seam | Coverage |
| --- | --- |
| `render_session_bar` left-only render (glyph/letter/project/branch, no right side) | cc-tmux self-test (`testing.py`) — unit tasks in API Batch |
| `render_beads_bar` two-line join + full-word passthrough | cc-tmux self-test — unit tasks in API Batch |
| Model letter read from `session-context.<pane>.json` | cc-tmux self-test with a fixture cache file — API Batch |
| nexus-statusline writes `model` letter into session-context JSON | nx repo vitest for the writer — DB Batch task |
| `roadmap-pulse --line` full words, no radar:stale | direct CLI run with pasted stdout — DB Batch task |
| Live end-to-end (rows 2+3 render, usage segment gone from row 1) | E2E Batch: chezmoi apply + nx deploy + fresh render capture, pasted |
