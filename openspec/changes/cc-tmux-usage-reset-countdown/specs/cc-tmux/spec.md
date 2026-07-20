# cc-tmux — cc-tmux-usage-reset-countdown delta

## ADDED Requirements

### Requirement: Row 2 SHALL render a 5H reset countdown near the session limit
The row-2 usage tail SHALL, when the active credential's 5-hour utilization is at or above 80%
AND a parseable, future `usage5hResetAt` is present in the nexus-agent `/credentials` payload,
render the 5H segment as `5H:<pct>·<countdown>` — countdown in DIM, minutes form (`47m`)
under 60 minutes, hours+minutes form (`1h12m`) at or above. The reset epoch SHALL be cached
alongside the existing `(label, u5, u7)` usage-cache payload and the remaining time computed at
render, so the countdown ticks down between cache refreshes. Below the 80% threshold, or when
`usage5hResetAt` is absent, unparseable, or in the past, the segment SHALL render byte-identical
to the prior requirement version (fail-open — this requirement adds output only inside the
high-utilization band).

#### Scenario: Countdown rendered during a session-limit cooldown
- Given: the active credential reports 5H utilization 0.94 and `usage5hResetAt` 47 minutes
  in the future
- When: row 2 renders
- Then: the 5H segment reads `5H:94%·47m` with the countdown in DIM and the percent keeping
  its existing `color_for` coloring

#### Scenario: Hours form above 60 minutes
- Given: 5H utilization 0.85 and a reset 72 minutes away
- When: row 2 renders
- Then: the countdown renders `1h12m`

#### Scenario: No countdown below the threshold
- Given: 5H utilization 0.79 with a valid future `usage5hResetAt`
- When: row 2 renders
- Then: the 5H segment renders without any countdown, byte-identical to the prior format

#### Scenario: Fail-open on absent or past reset time
- Given: 5H utilization 0.94 and `usage5hResetAt` absent, unparseable, or in the past
- When: row 2 renders
- Then: the 5H segment renders without a countdown and no error is raised

#### Scenario: Cached reset epoch survives the TTL window
- Given: a fresh usage-cache write that included the reset epoch
- When: row 2 renders again within the 45s cache TTL
- Then: the countdown is computed from the cached epoch at render time (it decreases without
  a new HTTP fetch)
