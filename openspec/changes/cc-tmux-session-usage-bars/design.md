# Design: cc-tmux session/usage/beads bars + usage-cache consolidation

## Why file-based consolidation, not an HTTP call per render

`nexus-statusline` runs on every Claude Code render (potentially many times per minute across
every active pane). A network round-trip to `nexus-agent`'s `/credentials` on that cadence adds
latency to the hot path for data that changes at most every 5 minutes (the poller's own interval).
`nexus-agent` already writes usage snapshots to its `credentials` table on each poll tick — the
fix reuses that write, fanning it out to one more sink (a shared JSON file) instead of adding a
second network consumer. `nexus-statusline`'s existing `getApiUsage()` already had a 5-minute file
cache for the SAME reason (avoid hammering Anthropic on every render) — this design keeps that
shape, it just points the cache-fill step at a local file instead of a live Anthropic call.

## Why the "full parity" harvest shrank to one field

The original scope (this session's earlier design pass) targeted replicating every field
`nexus-statusline` shows today: cost, lines added/removed, session clock, model, output style,
context %, token speed, git branch/worktree, active spec. Two live checks collapsed that:

1. **`GET /sessions` (nexus-agent) is not a usable substitute.** Hoped to reuse it for session
   count instead of adding new cc-tmux logic. Live response: 94 rows, heavily stale/duplicate
   (`tmuxTarget` often `null`), and `model` is *always* the literal string `"claude"` — never the
   actual variant. Not just unhelpful for the model letter — actively worse data than what
   cc-tmux already has via `get_hop_panes()` (a single live `tmux list-panes -a`, no external
   dependency, no staleness).
2. **The `SessionStart` hook payload already carries `model`.** Verified via `strings` on the
   installed `claude` binary (2.1.206) — the same technique that found `session_title` earlier
   this session. `cmd_register()` already reads this payload for `session_title`; extending it to
   also capture `model` is a one-line addition to an existing branch, not new infrastructure.
3. **`context_window` genuinely has no shortcut.** Checked whether `Stop` or `PreCompact` hook
   payloads carry context/token data (same `strings` technique) hoping to avoid touching
   `nexus-statusline` at all for this session-usage field. Neither does — `context_window` only
   ever exists in the statusLine's own per-render stdin. This is the one field where the original
   "only nexus-statusline sees this" reasoning holds, so it's the one field kept in a (now
   single-field) harvest write.

Net effect: the nx-repo surface for this proposal is much smaller than originally scoped — one
consolidation swap (Pipeline 1) plus one minimal single-field write (the surviving sliver of
Pipeline 2) — not a general-purpose harvest mechanism.

## Layout: three bars, not one — and why it flipped twice

Mid-session the layout requirement changed direction three times, each time based on something
concrete rather than preference alone:

1. **Two bars** (original ask: "a bar under the tmux tabs").
2. **One bar** — folded into the existing `status-right` line when asked "can we not have this be
   a separate bar?" (simpler, no new tmux config surface).
3. **Two bars again** — the single combined line, once mocked up with real field values, was
   genuinely too cluttered to read (13 fields on one row).
4. **Three bars** — beads/proposals joined the ask as a value distinct enough (project-scoped,
   not session-scoped) to warrant its own row rather than crowding row 2's already-tight
   left/right split.

`docs/diagrams/cc-tmux-sources.html` carries the full revision history inline (each layout
decision documented with the reasoning at the time) rather than repeating it here — read that
file's "Planned Architecture" section for the blow-by-blow.

## Architecture

```
Pipeline 1 (nx) — usage consolidation:
  credential-usage-poller.ts (existing, unchanged)
    -> statusline-usage-file.ts (NEW) writes usage-cache.json
    -> nexus-statusline's getPolledUsage() (NEW) reads it, replacing getApiUsage()

Pipeline 2 (nx, minimal) — session/context %:
  CC stdin (context_window.used_percentage)
    -> nexus-statusline writeSessionContext() (NEW) writes session-context.<pane>.json
       (gated on $TMUX_PANE, fail-soft, atomic write)

cc-tmux (installfest):
  tmux.py:    get_window_top_pane()          — mirrors get_window_top_state()
  tmux.py:    session-count helper           — counts get_hop_panes() by project
  cli.py:     cmd_register() extended        — captures `model` from SessionStart payload -> @cc-model
  cli.py:     cmd_session_bar()              — row 2 CLI entrypoint (reads @cc-model, @cc-project,
                                                @cc-branch, session-context.<pane>.json, usage.py's
                                                account+5H/7D)
  cli.py:     cmd_beads_bar()                — row 3 CLI entrypoint (reads roadmap-pulse.<code>.line)
  render.py:  render_session_bar()           — pure: row 2 composition
  render.py:  render_beads_bar()             — pure: row 3 composition
  parser.py:  session-bar / beads-bar        — new subcommands

tmux config:
  tmux.conf.tmpl:  status 3
  *-theme.conf x4: status-format[1] (session-bar), status-format[2] (beads-bar)
```

## Key invariants carried over from the existing cc-tmux design

1. **Fail-open everywhere.** A missing/unreadable cache file, an nexus-agent that's down, or a
   stale roadmap-pulse cache all degrade to an empty segment — never a crash, never blocking
   render. Same convention as `usage.py`'s existing `_query()`/`build_segment()`.
2. **No new background process.** `session-context.<pane>.json` and `usage-cache.json` are both
   written by processes that already run on their own cadence (nexus-statusline per render,
   nexus-agent's poller per tick) — cc-tmux only ever reads, on tmux's own `status-interval`
   refresh, matching the animated-tab-icon precedent shipped earlier this session.
3. **Representative-pane selection is one function, reused.** `get_window_top_pane()` mirrors
   `get_window_top_state()` exactly (same scoped `list-panes -t <window>` shape) rather than a
   second bespoke lookup.

## Deploy safety (nx repo)

`nexus-statusline` is a compiled binary with no auto-deploy hook — pushing its source touches
nothing live until a manual `bun run build` + reinstall (snapshot the prior binary first for
instant rollback: `cp ~/.local/bin/nexus-statusline ~/.local/bin/nexus-statusline.bak`).
`nexus-agent` auto-deploys (rebuild + restart) on push-to-main via the same hook the
credential-refresh fix went through earlier this session — for this change, land it with
`SKIP_DEPLOY=1 git push`, verify `usage-cache.json` writes correctly on the next poll tick, then
deploy deliberately (manual `bun run build` + `systemctl --user restart nexus-agent`) rather than
letting the push trigger it automatically.

## Batch mapping (DB / API / UI / E2E doesn't map cleanly onto this domain)

This repo has no traditional database/API/UI/E2E layering — it's a dotfiles repo plus a tmux CLI
plugin. Prior cc-tmux proposals in this repo (`cc-tmux-plugin`, `cc-tmux-scout-adoptions`) used
domain-appropriate batch names (`Script Batch` / `Config Batch` / `Verification Batch`) instead of
the four literal headers `/feature`'s wave-plan-build gate expects. This proposal uses the literal
`## DB Batch` / `## API Batch` / `## UI Batch` / `## E2E Batch` headers `/feature` requires, mapped
by domain fit rather than a literal database/API/UI/E2E split:

| Literal header | What actually lives there |
| --- | --- |
| `## DB Batch` | nx-repo data producers (Pipeline 1 + 2 — the "data layer" this feature depends on) |
| `## API Batch` | cc-tmux backend logic (new Python functions/subcommands) |
| `## UI Batch` | tmux config wiring (the actual user-visible rendering surface) |
| `## E2E Batch` | self-test additions + live verification + the nx deploy step |
