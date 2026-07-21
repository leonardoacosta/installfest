# wavetui Specification

## ADDED Requirements

### Requirement: FlairManager derives animation triggers by diffing consecutive Store snapshots, never by intercepting the event bus
`FlairManager` SHALL derive every animation trigger by comparing two consecutive `Snapshot`
values (`Diff(prev, next) []FlairEvent`) — a pure function with no side effects. It MUST NOT
subscribe to `wavetui-core`'s internal event bus, and MUST NOT read, write, or otherwise touch
`Store` state directly.

#### Scenario: an item present in the previous snapshot and absent in the next produces a closed/archived event
- Given: item `X` is present in `prev.Items` and absent from `next.Items`
- When: `Diff(prev, next)` runs
- Then: the result includes a `FlairEvent` for item `X`, keyed by `Item.Kind` to select the
  bead-closed vs. proposal-archived effect

#### Scenario: an item present in the next snapshot and absent from the previous produces an appeared event
- Given: item `Y` is absent from `prev.Items` and present in `next.Items`
- When: `Diff(prev, next)` runs
- Then: the result includes an `EventItemAppeared` event for item `Y`

#### Scenario: a blocker clearing on an item present in both snapshots produces a blocker-resolved event
- Given: item `Z` has a non-nil `Blocker` in `prev` and a nil `Blocker` in `next`
- When: `Diff(prev, next)` runs
- Then: the result includes an `EventBlockerResolved` event for item `Z`

#### Scenario: Diff never mutates its inputs
- Given: any two `Snapshot` values
- When: `Diff` is called
- Then: neither `Snapshot` value is modified, and calling `Diff` twice with the same inputs
  produces the same output

### Requirement: The tick loop runs only while an animation is live and idles at zero cost otherwise
`FlairManager` SHALL schedule a `tea.Tick` only when at least one animation is active
(`NeedsTick() == true`). It MUST NOT run a fixed-rate loop (e.g. a permanent 30fps `tea.Tick`)
regardless of animation state.

#### Scenario: no active animation means no scheduled tick
- Given: `FlairManager.active` is empty
- When: the root model's `Update` completes processing the current message
- Then: no `tea.Tick` command is issued for flair

#### Scenario: an active animation schedules exactly one next tick
- Given: at least one entry exists in `FlairManager.active`
- When: the root model's `Update` completes processing the current message
- Then: exactly one `tea.Tick` command is scheduled for the next frame

#### Scenario: an animation settling removes its tick requirement
- Given: the last active animation entry settles (reaches its harmonica settling threshold or
  fixed duration) during a frame
- When: that frame's `Update` completes
- Then: `NeedsTick()` returns `false` and no further tick is scheduled

### Requirement: Disabling flair produces byte-for-byte-identical rendering minus animation frames
When `config.FlairConfig.Enabled` is `false`, the application SHALL render identically to the
same configuration with `Enabled` set to `true`, for the same sequence of `Snapshot` values,
except for the presence of animation frames/overlays themselves. `FlairManager.Diff` and the
overlay compositor MUST NOT be invoked at all when `Enabled` is `false` — not merely invoked and
suppressed.

#### Scenario: disabled flair skips Diff entirely
- Given: `cfg.Enabled == false`
- When: a new `Snapshot` arrives at the root model
- Then: `FlairManager.Diff` is never called for that snapshot

#### Scenario: base pane rendering is unaffected by flair's enabled state
- Given: the same `Snapshot` value
- When: `QueuePane.View()` is rendered once with flair enabled and once with flair disabled
- Then: the two renders are identical except for any highlight/overlay flair itself added — no
  underlying data, layout, or non-flair styling differs

### Requirement: Flair auto-degrades on non-truecolor terminals and respects a global calm-mode toggle
`FlairManager` SHALL detect terminal color-profile capability at startup (via `lipgloss/v2`'s
color-profile detection) and MUST substitute nearest-ANSI-equivalent colors (via `go-colorful`'s
distance-based color matching) instead of emitting truecolor escape sequences on a terminal that
does not support them. When `config.FlairConfig.CalmMode` is `true`, every effect MUST resolve to
its static-glyph fallback (no frame cycling, no spring motion) while still reflecting the
underlying state signal the effect represents.

#### Scenario: a non-truecolor terminal gets ANSI-equivalent colors, not broken escape codes
- Given: the detected terminal color profile is not truecolor
- When: a color-lerp effect (e.g. row flash fade) would otherwise emit a truecolor gradient
- Then: the nearest 16-color ANSI equivalent is substituted at each step instead

#### Scenario: calm mode replaces an animated presence sprite with a static glyph
- Given: `cfg.CalmMode == true` and an item has a "blocked-on-you" session state
- When: the presence sprite for that item is rendered
- Then: a single static glyph representing "blocked-on-you" is shown, with no frame cycling

### Requirement: Negative-attention effects are reserved exclusively for genuinely bad events
The horizontal-shake-plus-red-pulse effect SHALL be triggered only by `EventNegative` (an item's
`Stale` field transitioning from `false` to `true`). It MUST NOT be reused as the effect for any
other event kind.

#### Scenario: a zombie-adjacent stale transition triggers the negative effect
- Given: item `X`'s `Stale` field is `false` in `prev` and `true` in `next`
- When: `Diff(prev, next)` runs
- Then: the result includes an `EventNegative` event for item `X`, and only that event kind maps
  to the shake-plus-red-pulse effect

#### Scenario: no other event kind ever produces the negative effect
- Given: any `FlairEvent` whose `Kind` is not `EventNegative`
- When: `FlairManager` selects an effect for that event
- Then: the shake-plus-red-pulse effect is never selected

### Requirement: Victory-recap numbers are computed from the same Snapshot data the queue already renders, never a separate accounting path
Any future recap/summary display (e.g. items closed during a session) SHALL derive its counts by
accumulating `Diff`-produced events over time, never by issuing a separate query against `bd` or
`openspec` directly.

#### Scenario: a closed-item count matches the accumulated Diff events
- Given: a sequence of snapshots over which three `EventItemClosed` events were produced by
  `Diff`
- When: a recap display computes "items closed"
- Then: the displayed count equals the number of accumulated `EventItemClosed` events, with no
  independent `bd`/`openspec` query involved

### Requirement: Session-linked presence sprites and wave-progress triggers degrade gracefully when their source data is absent
The presence-sprite feature SHALL check, at build/execution time, whether a session-state field
is present on `Item` (added by `wavetui-sessions`, if landed) before rendering any sprite. If
absent, the feature MUST be skipped entirely — no placeholder state, no error. The same
gate applies to any future wave-progress trigger pending a progress event from
`wavetui-dispatch`.

#### Scenario: presence sprites render when session-state data is present
- Given: `wavetui-sessions` has landed and `Item` exposes a session-state accessor
- When: an item with an active session is rendered in the queue pane
- Then: a presence sprite reflecting that session's state is shown alongside the row

#### Scenario: presence sprites are absent, not broken, when session-state data is absent
- Given: `wavetui-sessions` has not landed and `Item` exposes no session-state accessor
- When: the queue pane renders
- Then: no presence sprite is shown for any row, and no error or placeholder state appears
