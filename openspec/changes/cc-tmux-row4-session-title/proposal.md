---
order: 0717b
---

# Change: cc-tmux-row4-session-title

## Why

The window tabs can only carry one glyph per window, so sub-agent detail (how many, busy vs
settled) either overloads the tab icon — the four braille pairs `cc-tmux-glyph-unification`
just collapsed to a single `◇`/`◆` presence swap — or lives nowhere. Meanwhile the custom
Claude Code session title (`@cc-title`, captured from the SessionStart `session_title` payload
and already tracked per pane) renders nowhere in the status bar at all, despite being the one
human-named handle on what a session is doing.

A fourth status row solves both: when the focused window's tracked pane is running alone
(foreground only), the row shows the session title; when sub-agents are in flight, the row
shows per-agent detail glyphs the tab overlay no longer attempts.

## What Changes

- **New status row (row 4)** for the focused window's representative tracked pane, published as
  a `@cc-row-agents` global option by `render-all` (same publish mechanism rows 2/3 use) and
  wired at the computed index `@cc-tab-rows + 2` in all four theme files.
- **Content contract**: foreground-only (no tracked sub-agent activity) -> the pane's
  `@cc-title` session title (row omitted when no title is set). With sub-agent activity -> a
  glyph strip, one glyph per tracked background dispatch: `◌` <-> `○` wall-clock pulse while
  the dispatch is inside its busy window (not-idle), `●` once it has settled (idle) but not
  yet aged out.
- **Busy-vs-idle heuristic**: background tracking today is launch timestamps with a single
  age-out timeout (`@cc-subagent-bg-timeout`); no hook signals true completion (`SubagentStop`
  never fires on this CC fleet). Busy = entry younger than a new `@cc-subagent-bg-busy-window`
  option (default well under the age-out timeout); idle = older than the busy window but not
  yet aged out. Documented as a heuristic in the requirement — same honesty posture as the
  existing background age-out rule.
- **Line-count arithmetic**: total status lines become `tab_rows + 2 + (1 if row-4 content else
  0)`, capped at tmux's hard 5-line ceiling — in portrait when tabs wrap to 3 rows, the agents
  row is the one dropped (lowest priority). Landscape common case: `status 4` when content
  exists, `status 3` otherwise — byte-identical rows 1-3 either way.

## Impact

- Affected specs: `cc-tmux` (1 ADDED requirement, 1 MODIFIED requirement — "The tmux status
  bar is three lines" becomes the computed 3-or-4-line contract)
- Affected code: `apps/cc-tmux/src/cc_tmux/render.py` (new pure row renderer),
  `apps/cc-tmux/src/cc_tmux/cli.py` (`cmd_render_all` publish + busy-window resolution +
  `_publish_multirow_status` arithmetic), `apps/cc-tmux/src/cc_tmux/tmux.py` (new option
  names), `home/dot_config/tmux/tmux.conf.tmpl` + the four theme `.conf` files (row wiring).

## Context

- depends on: `cc-tmux-glyph-unification`
- touches: `apps/cc-tmux/src/cc_tmux/render.py`, `apps/cc-tmux/src/cc_tmux/cli.py`, `apps/cc-tmux/src/cc_tmux/tmux.py`, `home/dot_config/tmux/tmux.conf.tmpl`, `home/dot_config/tmux/vercel-theme.conf`, `home/dot_config/tmux/one-hunter-vercel-theme.conf`, `home/dot_config/tmux/tokyo-night-abyss-theme.conf`, `home/dot_config/tmux/nord-theme.conf`
- The dependency is logical (this row absorbs the sub-agent detail that proposal removed from
  the tab overlay) AND physical (shared `render.py`/`cli.py` touches serialize the waves).
- `@cc-title` capture already ships (SessionStart `session_title`, repo memory
  `reference_claude_code_hook_session_title`); this proposal only renders it.

## Testing

- Unit-level (no pytest harness — direct `python -c` invocations of the new pure row renderer,
  pasted stdout): title-only pane -> title text; one busy bg entry -> `◌`/`○` by parity; one
  settled bg entry -> `●`; mixed entries -> per-agent strip in launch order; no title + no
  agents -> empty string (row omitted).
- E2E (live tmux): focus a window with a titled session -> row 4 shows the title at
  `status-format[3]` landscape; dispatch a background sub-agent -> row flips to the glyph
  strip and pulses across two parity captures; portrait 3-row tab wrap -> `status` stays 5 and
  the agents row is absent; all four themes render the row (capture per theme). N/A — no
  browser surface.
