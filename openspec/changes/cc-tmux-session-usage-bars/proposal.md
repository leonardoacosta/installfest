---
status: draft
---

# Proposal: cc-tmux-session-usage-bars

## Why

This session's work landed cc-tmux's account/5H/7D usage segment (`usage.py`) with correct port,
schema, and dedupe-gate fixes — but 5H/7D still render `--`. Root cause, found live: Anthropic's
`/api/oauth/usage` returns `429 rate_limit_error` even for a genuinely fresh token, because
`nexus-agent`'s poller AND `nexus-statusline` (Claude Code's own registered statusLine command)
independently call the same endpoint on the same ~5-min cadence, uncoordinated.

Separately, the original ask ("session, 5 hour, weekly [usage], or what account is currently being
used") grew through several live design passes — captured in `docs/diagrams/cc-tmux-sources.html`
— into: comprehensive enough to make Claude Code's own statusline unnecessary, then simplified to a
lean left/right layout, then a third bar for open/ready beads + proposals. Key findings that shape
the design below (all verified live, not assumed):

- `nexus-agent`'s `GET /sessions` is not a shortcut for session-count or model — 94 rows, heavily
  stale/duplicate, and `model` is *always* the literal string `"claude"` on every row checked.
- The real model variant, and `session_title`, both arrive in the `SessionStart` hook payload
  cc-tmux already reads (confirmed via `strings` on the `claude` binary) — no nexus-statusline
  involvement needed for the model letter.
- `~/.claude/scripts/state/roadmap-pulse.<code>.line` already exists per-project
  (stale-while-revalidate, `nexus-statusline`'s existing `getRoadmapPulse()`) — cc-tmux can read it
  directly with the project code it already resolves. Zero nx-repo changes for the beads/proposals
  row.
- `context_window` (session/context %) is the one field confirmed to exist ONLY in the
  statusLine's per-render stdin — checked `Stop` and `PreCompact` hook payloads for a shortcut
  (same binary-strings technique); neither carries it. This is the one field that still needs a
  minimal `nexus-statusline` write.

## What Changes

1. **Usage consolidation (nx)** — `nexus-agent`'s poller becomes the sole caller of
   `/api/oauth/usage`; a new write helper persists the active credential's polled 5H/7D to a
   shared cache file. `nexus-statusline` reads that cache instead of calling Anthropic itself
   (new `getPolledUsage()` replaces the deleted `getApiUsage()`/`fetchWithToken()`/
   `readAccessToken()`).
2. **Minimal per-render harvest (nx)** — `nexus-statusline` writes ONE field
   (`context_used_pct`) to a per-pane cache file on every render, gated on `$TMUX_PANE` being set,
   fail-soft. This is the sole surviving piece of the original 13-field "full parity" harvest idea.
3. **cc-tmux session/usage row** (tmux status row 2, left+right split):
   - Left: session-count glyph (`◉`/`◌`, computed from cc-tmux's own `get_hop_panes()` — no new
     data source), model letter (`F`/`O`/`H`/`S` — Sonnet added, the original spec omitted it),
     project code, git branch (both already tracked).
   - Right: account label + `SES:`/`5H:`/`7D:` (session/5-hour/7-day — the original three-window
     ask), reusing `usage.py`'s existing rendering conventions.
4. **cc-tmux beads/proposals row** (tmux status row 3): reads
   `~/.claude/scripts/state/roadmap-pulse.<code>.line` directly — the top actionable item plus
   compact open/updated counts. Zero nx-repo involvement.
5. **tmux config**: `status 3` (three-line status bar) in the shared `tmux.conf.tmpl`; each of
   the 4 theme `.conf` files defines `status-format[1]` (row 2) and `status-format[2]` (row 3).

## Non-Goals

- No cost, lines-added/removed, session clock, output-style, worktree, or spec-name fields —
  dropped from the original 13-field "full parity" ambition when the layout simplified to a lean
  left/right split. `usage.py`'s existing account+5H/7D rendering is reused, not rebuilt.
- No account-switcher popup — that was `add-tmux-credential-status`'s deferred task 4.1 against a
  now-superseded shell-script architecture (port 7402). Re-file fresh against the current
  Python `cc-tmux` plugin if still wanted; it does not belong on this proposal.
- No changes to cc-tmux's window-rename or animated-tab-icon features (both already shipped this
  session, unrelated to the status-bar rows this proposal adds).
- Tab styling "less jagged"/padding, raised earlier this session — never captured in the design
  diagram this proposal implements; explicitly out of scope here.
- No wiring of nexus-statusline's test suite into the nx push gate (flagged as a real gap during
  design, but a CI-policy decision for that repo, not bundled into this proposal).
- No removal of nexus-statusline's second Anthropic caller (`getAccountDomain`, the account-domain
  lookup) — same consolidation logic would apply, but it's a separate, lower-frequency endpoint
  and not the confirmed 429 source; left alone to keep this change's nexus-statusline surface
  minimal.

## Context

- touches: `apps/cc-tmux/src/cc_tmux/tmux.py`, `apps/cc-tmux/src/cc_tmux/cli.py`,
  `apps/cc-tmux/src/cc_tmux/render.py`, `apps/cc-tmux/src/cc_tmux/parser.py`,
  `apps/cc-tmux/src/cc_tmux/testing.py`, `home/dot_config/tmux/tmux.conf.tmpl`,
  `home/dot_config/tmux/vercel-theme.conf`,
  `home/dot_config/tmux/one-hunter-vercel-theme.conf`,
  `home/dot_config/tmux/tokyo-night-abyss-theme.conf`, `home/dot_config/tmux/nord-theme.conf`,
  `docs/diagrams/cc-tmux-sources.html`, and (different repo, absolute paths, tracked here per the
  cross-repo scoping decision made when this proposal was drafted) `~/dev/personal/nexus/apps/agent/src/services/statusline-usage-file.ts`,
  `~/dev/personal/nexus/apps/agent/src/index.ts`, `~/dev/personal/nexus/apps/nexus-statusline/src/index.ts`
- Related: supersedes `add-tmux-credential-status` (archived 2026-07-11 as superseded — see that
  proposal's closure note). Extends the `[CAPABILITY] cc-tmux` epic (`if-bqw`, currently showing
  2/2 children complete — this proposal reopens it with new scope). No dependency on `view-command`
  (unrelated files, `scripts/`/viewer only).
- Design record: `docs/diagrams/cc-tmux-sources.html` § "Planned: usage consolidation +
  full-parity harvest" — every function this proposal's tasks create is named there with its
  file and exactly what it owns, built up over several live corrections during design (documented
  in the diagram's own revision notes rather than repeated here).

## Testing

| Seam | Coverage |
| --- | --- |
| Session-count helper (pure, cc-tmux) | `cc-tmux self-test` case: counts `get_hop_panes()` rows matching a project, 0/1/2+ produce `◌`/`◉`/`◉ N` — task 2.6 |
| `cmd_register()` model capture | `cc-tmux self-test` case: `SessionStart` payload with a `model` field writes `@cc-model`; payload without one leaves it unset (fail-open) — task 2.6 |
| `get_window_top_pane()` | `cc-tmux self-test` case, mirrors the existing `get_window_top_state()` test shape (mocked `list-panes`) — task 2.6 |
| `render_session_bar()` / `render_beads_bar()` (pure) | `cc-tmux self-test` cases covering left/right composition and roadmap-pulse line parsing — task 2.6 |
| nx: `getPolledUsage()` / `writeSessionContext()` | nexus-statusline's own test suite (`bun test apps/nexus-statusline/`, run manually per the deploy-safety note — this repo's push gate does not currently execute it) — task 4.4 |
| End-to-end 3-row render | Live verification: `chezmoi apply` + tmux reload, observe all three rows populate with real data (session-count, model letter, project/git, account+SES/5H/7D, roadmap-pulse line) — task 4.2, task 4.3 |
| Usage consolidation | Live verification: confirm `usage-cache.json` is written by nexus-agent's poller and read by nexus-statusline without a direct Anthropic call from the statusline process — task 4.4 |
