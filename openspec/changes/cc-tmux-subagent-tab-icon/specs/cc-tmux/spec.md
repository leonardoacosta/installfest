# cc-tmux Specification Delta

## ADDED Requirements

### Requirement: The animated tab icon reflects sub-agent activity
When a pane has one or more sub-agent dispatches tracked as active (foreground, via a matched
`PreToolUse`/`PostToolUse` pair on the `Task` tool; or background, via a time-boxed heuristic
since no hook signals a background dispatch's true completion), the animated tab icon SHALL
render one of four distinct glyphs reflecting that activity instead of its normal
`@cc-state`-driven animation. When no sub-agent activity is tracked for a pane, the tab icon SHALL
render exactly as the existing "Animated tab icon" Requirement already specifies (unchanged).
Foreground activity takes precedence over background activity when both are nonzero, since
foreground tracking is an exact signal and background tracking is a heuristic.

#### Scenario: no sub-agents tracked renders the existing icon unchanged
- Given: a tracked pane with `@cc-subagent-fg` at 0 and no unexpired `@cc-subagent-bg` entries
- When: the tab icon renders
- Then: it shows the existing `@cc-state`-driven glyph (waiting/idle/active), unaffected by this
  Requirement

#### Scenario: a foreground sub-agent dispatch increments and decrements the count
- Given: a pane whose Claude session dispatches a foreground (blocking) sub-agent
- When: the dispatch's `PreToolUse` (`Task` matcher) fires
- Then: `@cc-subagent-fg` increments; when the matching `PostToolUse` fires (the dispatch
  returned), it decrements back

#### Scenario: a background dispatch ages out of the active count
- Given: a pane's Claude session dispatches a background sub-agent, recorded in
  `@cc-subagent-bg` with a launch timestamp
- When: more than `@cc-subagent-bg-timeout` seconds have elapsed since that launch
- Then: that entry no longer counts toward the tab icon's sub-agent-activity glyph (pruned on
  read, not necessarily deleted immediately)

#### Scenario: foreground activity takes precedence over background
- Given: a pane with both a running foreground sub-agent and an unexpired background entry
- When: the tab icon renders
- Then: it reflects the foreground count's glyph, not the background one
