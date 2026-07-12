# cc-tmux Specification Delta

## MODIFIED Requirements

### Requirement: A dedicated tmux status row surfaces open/ready beads and proposals
The plugin SHALL render a second dedicated tmux status row (`status-format[2]`) showing the
current project's cached roadmap-pulse counts line ONLY, read directly from
`~/.claude/scripts/state/roadmap-pulse.<code>.line`. When the cache contains a `next:` line, it
SHALL NOT be rendered on this row — only the open/unarchived counts line renders. No new data
production mechanism SHALL be introduced for this row — it reads the cache nexus-statusline's
own `getRoadmapPulse()` already maintains.

#### Scenario: row 3 renders only the counts line
- Given: a cached roadmap-pulse file containing `next: /apply foo…` and `0 open, 2 unarchived`
  on separate lines
- When: the beads-bar row renders
- Then: it shows only `0 open, 2 unarchived` — the `next:` line does not appear anywhere on the
  row

#### Scenario: counts-only cache renders as-is
- Given: a cached roadmap-pulse file containing only a counts line (no `next:` line)
- When: the beads-bar row renders
- Then: it shows that line alone, unchanged

#### Scenario: no cache yet renders nothing
- Given: no roadmap-pulse cache file exists yet for the current project
- When: the beads-bar row renders
- Then: the row is empty — no error, no placeholder text

### Requirement: Animated tab icon reflects state via a wall-clock-driven refresh
The tab icon SHALL be rendered from a top-level status-format job (`#(cc-tmux tabs-row)`) that
composes the ENTIRE window-tabs row itself — icon, index, and name per window, with
active-window highlighting — rather than from the tmux-native per-window
`window-status-format`/`window-status-current-format` options. This relocation is required
because `#()` shell jobs nested inside tmux's default per-window `#{T:window-status-format}`
expansion do not execute on this fleet's tmux version (confirmed: a literal job embedded in
`window-status-format` and read back via `#{T:...}` never runs, across repeated timed retries),
while top-level status-format jobs are proven to execute (row 2 and row 3 already render
correctly via exactly this mechanism). No background process or timer SHALL be introduced by
this plugin to achieve the animation — the row is re-evaluated on tmux's existing
`status-interval` cadence, identical to how row 2/row 3 already refresh, just via a job placed
where jobs actually run. Each tracked state SHALL use the same distinct motion language as
before: `waiting` cycles a rising/falling shade pulse (`░▒▓█▓▒░`); `active` cycles a rotating
block edge (`▁▏▔▕`); `idle` renders a single static glyph, never animated. A window with no
tracked Claude pane MUST render no icon at all (not even the idle glyph).

#### Scenario: waiting state pulses through the shade sequence
- Given: a window's highest-priority tracked state is `waiting`
- When: the live tabs row is captured at two different wall-clock seconds one second apart
- Then: it shows two different frames from `░▒▓█▓▒░` for that window, advancing by one position

#### Scenario: active state rotates through the block sequence
- Given: a window's highest-priority tracked state is `active`
- When: the live tabs row is captured at two different wall-clock seconds one second apart
- Then: it shows two different frames from `▁▏▔▕` for that window, advancing by one position

#### Scenario: idle state never animates
- Given: a window's highest-priority tracked state is `idle`
- When: the live tabs row is captured at any two different wall-clock times
- Then: it shows the same static glyph for that window both times

#### Scenario: untracked window renders no icon
- Given: a window with no tracked Claude pane (a plain shell)
- When: the live tabs row renders
- Then: that window's entry shows no icon prefix

#### Scenario: the icon actually appears in the live render
- Given: the `tabs-row` job is wired into a top-level status-format slot
- When: the live rendered tab row is byte-captured (e.g. via `tmux display-message -F`)
- Then: the icon glyph is present in the captured output — not silently dropped the way the
  prior per-window `window-status-format` mechanism dropped it

## ADDED Requirements

### Requirement: cc-tmux register logs a hook-invocation trace for window-rename diagnostics
Every `cc-tmux register` invocation SHALL append one line to a debug trace log
(`~/.claude/scripts/state/cc-tmux-register-trace.log`) recording the invocation's timestamp,
`hook_event_name`, resolved pane id, whether a window-rename was attempted, and whether it
fired. The log SHALL be bounded (rotated or capped) so it never grows unbounded. This is
diagnostic-only — it MUST NOT alter `_maybe_rename_window`'s existing rename behavior.

#### Scenario: a register call is traced
- Given: `cc-tmux register` is invoked for any hook event
- When: the invocation completes
- Then: one new line appears in the trace log recording that event's hook name, pane, and
  rename attempt/fire outcome

#### Scenario: the trace log is bounded
- Given: the trace log has been written to over an extended period
- When: its size is inspected
- Then: it does not grow without bound — old entries are rotated or capped
