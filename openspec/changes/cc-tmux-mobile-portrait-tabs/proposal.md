---
order: 0716d
---

# Proposal: cc-tmux-mobile-portrait-tabs

## Why

Leo's ask (`/openspec:explore`, this session): on a portrait/tall-narrow screen (mobile), the
window-tabs row (row 1, `render_tabs_row` in `render.py`) should be roughly 3x bigger, with
row-wrap so tabs don't overflow off the right edge.

**Confirmed via research this session, not assumed:**
- No existing logic anywhere in cc-tmux reacts to `client_width`/`client_height` or aspect ratio
  for any status row — the only two reads of those tmux format variables in the whole app are
  unrelated (accounts-popup sizing, `cli.py:1458`/`1470`).
- tmux's `status-format[N]` array entries are each a SINGLE physical line — there is no native
  wrapping of one slot's content across multiple physical lines.
- `client_width`/`client_height` CAN be passed as job arguments to the render-all command exactly
  like `#{window_id}` already is (`tmux display-message -p '#{client_width}x#{client_height}'`
  confirmed live).
- **"3x the size" has a real candidate mechanism, not just padding**: Kitty terminal's Text
  Sizing Protocol (OSC 66, an `ESC ] 6 6 ; s=3 ; <text> BEL`-shaped sequence for literal
  triple-size text) is a real,
  documented escape sequence. Ghostty (this fleet's terminal) has an OSC 66 parser landed as of
  a recent PR (`ghostty-org/ghostty#10333`/`#10315`, discussion #5563, ~Feb 2025) — maturity and
  exact version support are UNVERIFIED from this session (no `ghostty` binary reachable from
  Homelab to test). This repo's tmux.conf already has `allow-passthrough on` (enabled for Kitty
  graphics protocol), which is the mechanism that would need to also let OSC 66 through — untested
  for this specific protocol.
- Leo's call, given the above uncertainty: attempt OSC 66 first, with a REQUIRED live-verification
  task on the Mac (where Ghostty actually runs) before committing further; fall back to a
  padding/spacing-only approach if OSC 66 doesn't render correctly or corrupts tmux's own layout
  math (tmux allocates status rows by physical line count — it has no native awareness that an
  OSC-66-scaled tab visually claims more vertical space than tmux thinks that row occupies; this
  is a real, not yet observed, corruption risk).
- Leo's call on row-wrap scope: build the FULL dynamic multi-row wrap now, not a phase-1-only
  detection+padding version. This means growing tmux's currently-fixed `status 3` dynamically
  based on how many tab-rows are needed, and re-deriving where the session-bar/beads-bar rows
  land (today hardcoded as `status-format[1]`/`[2]` in `tmux.conf.tmpl` AND in all four theme
  `.conf` files) instead of fixed indices.

## What Changes

- **`apps/cc-tmux/src/cc_tmux/cli.py`** (`_build_tabs_row` / `cmd_render_all`): pass
  `client_width`/`client_height` through from the render-all job's arguments (mirroring
  `#{window_id}`'s existing pattern — add `#{client_width}` `#{client_height}` to
  `tmux.conf.tmpl`'s `status-format[0]` job string). Compute portrait mode
  (`client_height > client_width`, or a documented threshold) and, when portrait, compute how
  many tab-rows are needed to fit all windows without horizontal overflow at the wider/OSC-66
  tab size.
- **`apps/cc-tmux/src/cc_tmux/render.py`** (`render_tabs_row` and friends): accept a rendering
  mode/size parameter; when in "mobile" mode, wrap each tab's index/icon/name segment in an OSC
  66 `s=3` escape sequence (new small helper, e.g. `_osc66_scale(text, scale=3)`), falling back
  to plain unscaled text (matching the padding-only design) if a documented feature flag/detected
  incompatibility disables OSC 66. Split the composed row content across N sub-rows instead of
  one, when the computed tab-row count is >1.
- **`home/dot_config/tmux/tmux.conf.tmpl`**: the render-all job dynamically issues
  `tmux set-option -g status <N>` (N = tab-rows-needed + 2) as a side effect of rendering,
  BEFORE emitting its own `status-format[0..tab-rows-1]` content, so tmux's own row-count and
  the content being written stay in sync within the same tick. `status-format[tab-rows]`/
  `[tab-rows+1]` (previously fixed `[1]`/`[2]`) become computed indices instead.
- **All four theme `.conf` files** (`nord-theme.conf`, `one-hunter-vercel-theme.conf`,
  `vercel-theme.conf`, `tokyo-night-abyss-theme.conf`): replace the hardcoded
  `status-format[1]`/`[2]` `@cc-row-session`/`@cc-row-beads` assignments with whatever
  computed-index mechanism task 2.x settles on (e.g. reading a `@cc-tab-rows` global option the
  render job sets, rather than a literal `[1]`/`[2]` index) — these currently-static theme files
  need to become row-count-aware.

## Non-Goals

- No change to the session-bar or beads-bar rows' OWN content, only which physical
  `status-format[N]` index they occupy.
- No change to non-portrait (landscape/desktop) rendering — the existing single-row tab layout,
  current sizing, and current `status 3` fixed count are BYTE-IDENTICAL when `client_height <=
  client_width` (or whatever the portrait threshold resolves to).
- No new runtime dependency — OSC 66 is a raw escape sequence, emitted as a plain string, no
  library needed (matching this plugin's stdlib-only design).
- This proposal does NOT guarantee OSC 66 ships in the final implementation — the fallback path
  (padding/spacing only, no escape-sequence scaling) is an explicit, sanctioned outcome if live
  Mac verification (task 1.1, below) finds it unreliable. That is not scope creep or a deferred
  task; it is this proposal's own documented contingency.

## Context
- touches: `apps/cc-tmux/src/cc_tmux/cli.py`, `apps/cc-tmux/src/cc_tmux/render.py`,
  `home/dot_config/tmux/tmux.conf.tmpl`, `home/dot_config/tmux/nord-theme.conf`,
  `home/dot_config/tmux/one-hunter-vercel-theme.conf`, `home/dot_config/tmux/vercel-theme.conf`,
  `home/dot_config/tmux/tokyo-night-abyss-theme.conf`
- Note: also touches `render.py` and (indirectly, via the shared `status-format[1]`/`[2]`
  reassignment) the session-bar/beads-bar rows that `cc-tmux-row2-model-color-usage-format` and
  `cc-tmux-braille-flash-and-permission-pulse` (both in-flight, same session) also touch — a
  file-level wave conflict on `render.py`, not a logical dependency; `wave-plan-build` serializes
  automatically. The theme `.conf` files are NOT touched by any other in-flight proposal.

## Testing

- `apps/cc-tmux/src/cc_tmux/testing.py`: unit tests for the portrait-detection function (pure,
  given width/height), the tab-row-count computation (pure, given tab count + available width
  at 3x sizing), and `_osc66_scale`'s exact escape-sequence output.
- **REQUIRED live verification on the Mac** (task 1.1, gates the rest of this spec): a literal
  OSC 66 escape sequence rendered through this repo's actual Ghostty + tmux config, confirmed by
  eye (or a screenshot) to actually render at 3x size with no visual corruption of the
  surrounding status bar or tmux's own layout. This is genuinely not verifiable from Homelab —
  Ghostty is not installed there. If this fails, fall back to the padding-only contingency
  (Non-Goals) and continue the spec with that path instead.
