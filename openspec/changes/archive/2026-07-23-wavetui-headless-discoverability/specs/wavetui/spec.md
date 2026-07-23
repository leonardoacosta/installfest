## MODIFIED Requirements

### Requirement: An operator keybinding enables/disables headless admission
`HeadlessBar` SHALL expose a keybinding that toggles a `Controller`-owned `enabled` boolean. The
toggle SHALL default to `false` on every app start — no configuration flag or timer may enable
admission. The current admission state SHALL be visibly indicated in the UI at all times,
including before the keybinding has ever been pressed — not only once admission has been
toggled on.

#### Scenario: admission is disabled by default on app start
- Given: `wavetui` has just started
- When: no keybinding has been pressed
- Then: `Controller`'s admission state is disabled and `OnSnapshot` dispatches nothing

#### Scenario: pressing the toggle keybinding enables admission
- Given: admission is currently disabled
- When: the operator presses the toggle keybinding
- Then: `Controller`'s admission state becomes enabled and is visibly indicated on `HeadlessBar`

#### Scenario: pressing the toggle keybinding again disables admission
- Given: admission is currently enabled
- When: the operator presses the toggle keybinding again
- Then: `Controller`'s admission state becomes disabled; already-running headless children are
  not killed, but no new item is admitted

#### Scenario: the toggle keybinding and its off-state are discoverable before ever being pressed
- Given: `wavetui` has just started and admission has never been toggled
- When: the operator looks at the running UI
- Then: an always-visible hint names the toggle keybinding and shows admission is currently off
  — the operator does not need to already know the keybinding from source or spec to discover it

### Requirement: QueuePane renders the live queue with blocker badges and fan-out score
`QueuePane` SHALL render a bubbles table with columns for item, type, created-at, blocker badge,
and fan-out score, updating from each `Snapshot` it receives. When an item's Blocker/Stale
column has no dispatch badge, lane badge, or blocker badge to show, `QueuePane` SHALL render a
dispatch-mechanism tag indicating which mechanism ("tmux" or "clipboard") an "enter" press on
that row would use.

#### Scenario: a blocked item shows its badge
- Given: an item's `Blocker` field is populated
- When: `QueuePane` renders that row
- Then: the blocker badge is visible in the row

#### Scenario: an unblocked item with no other badge shows its dispatch mechanism
- Given: an item has no dispatch badge, lane badge, or blocker badge, and its linked session
  has a non-empty `PaneID`
- When: `QueuePane` renders that row's Blocker/Stale column
- Then: it shows "tmux" as the mechanism that would handle an "enter" press

#### Scenario: an item with no linked pane shows the clipboard fallback
- Given: an item has no dispatch badge, lane badge, or blocker badge, and either has no linked
  session or its linked session has an empty `PaneID`
- When: `QueuePane` renders that row's Blocker/Stale column
- Then: it shows "clipboard" as the mechanism that would handle an "enter" press

#### Scenario: an existing badge still takes precedence over the mechanism tag
- Given: an item has an active dispatch badge, lane badge, or blocker badge
- When: `QueuePane` renders that row
- Then: that existing badge is shown, not the dispatch-mechanism tag
