## ADDED Requirements

### Requirement: An operator keybinding enables/disables headless admission
`HeadlessBar` SHALL expose a keybinding that toggles a `Controller`-owned `enabled` boolean. The
toggle SHALL default to `false` on every app start — no configuration flag or timer may enable
admission.

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

### Requirement: Controller.OnSnapshot dispatches eligible ready items when admission is enabled
`Controller.OnSnapshot` SHALL, when admission is enabled, scan `snap.Items` for eligible items
(per the eligibility Requirement below) and call `HeadlessDispatcher.Dispatch` for each, in
`FanOutScore`-descending order, stopping at the first `ErrConcurrencyCapReached`. This SHALL
reuse the existing `OnSnapshot` reactive entry point — no new watcher, goroutine, or timer.

#### Scenario: OnSnapshot dispatches nothing while admission is disabled
- Given: admission is disabled
- When: `OnSnapshot` receives a `Snapshot` containing eligible items
- Then: `Dispatch` is not called for any item

#### Scenario: OnSnapshot dispatches eligible items when admission is enabled
- Given: admission is enabled and the snapshot contains two eligible items
- When: `OnSnapshot` runs
- Then: `Dispatch` is called for both items (assuming capacity), each with `/apply <id>` per
  the existing prompt-composition rule

#### Scenario: admission stops at the concurrency cap with no retry
- Given: admission is enabled, the concurrency cap is reached partway through the eligible list
- When: `OnSnapshot` calls `Dispatch` and receives `ErrConcurrencyCapReached`
- Then: it stops attempting further items in that snapshot and does not retry — the next
  `Snapshot` (not a timer, not a retry loop) is the next admission opportunity

### Requirement: Eligible items are unblocked and unclaimed, ordered by FanOutScore descending
The admission loop SHALL treat an item as eligible for headless dispatch iff `Item.Blocker ==
nil` (the Store's existing unblocked definition) AND `Item.Session == nil` (not already linked
to a live session), and SHALL consider eligible items in `Item.FanOutScore` descending order —
no new readiness or priority field is introduced.

#### Scenario: a blocked item is never admitted
- Given: an item with a non-nil `Blocker`
- When: `OnSnapshot`'s eligibility filter runs
- Then: that item is excluded from admission regardless of `FanOutScore`

#### Scenario: an already-claimed item is never re-admitted
- Given: an item with a non-nil `Session` (already linked to a live session)
- When: `OnSnapshot`'s eligibility filter runs
- Then: that item is excluded from admission

#### Scenario: higher FanOutScore items are admitted first
- Given: two eligible items, one with `FanOutScore` 5 and one with `FanOutScore` 1, and only one
  concurrency slot available
- When: `OnSnapshot` runs the admission loop
- Then: the `FanOutScore` 5 item is dispatched and the `FanOutScore` 1 item is not (this
  snapshot)

### Requirement: Admission stops cleanly on ErrConcurrencyCapReached with no retry loop
The admission loop SHALL treat `ErrConcurrencyCapReached` as an expected, non-error stopping
condition for the current snapshot — not a failure to log or retry. Any other error returned by
`Dispatch` SHALL surface the same way a manual dispatch failure already surfaces (no new error
path invented).

#### Scenario: ErrConcurrencyCapReached is not treated as a dispatch failure
- Given: the concurrency cap is reached mid-loop
- When: `Dispatch` returns `ErrConcurrencyCapReached`
- Then: the admission loop stops silently for this snapshot — no failure badge, no retry, no log
  noise

#### Scenario: a genuine Dispatch error still surfaces
- Given: `Dispatch` returns a non-`ErrConcurrencyCapReached` error for an eligible item
- When: the admission loop encounters it
- Then: it surfaces through the same failure-reporting path manual dispatch failures already use
