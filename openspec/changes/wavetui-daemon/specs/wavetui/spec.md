## ADDED Requirements

### Requirement: HeadlessDispatcher implements the Dispatcher interface with no signature change
`apps/wavetui/internal/daemon` SHALL define `HeadlessDispatcher` implementing
`wavetui-dispatch`'s `Dispatcher` interface exactly as shipped —
`Dispatch(ctx context.Context, item store.Item, promptText string) error` — with no additional
exported method and no change to that interface's signature.

#### Scenario: HeadlessDispatcher satisfies the existing Dispatcher interface
- Given: `wavetui-dispatch`'s `Dispatcher` interface as already shipped
- When: `HeadlessDispatcher` is compiled against it
- Then: it satisfies the interface with `Dispatch(ctx, item, promptText) error` and no interface
  change is required in `internal/dispatch`

#### Scenario: Dispatch is an admission decision, not a completion wait
- Given: a `HeadlessDispatcher.Dispatch` call that successfully spawns a child process
- When: `Dispatch` returns
- Then: it returns `nil` immediately after a successful spawn — it does not block until the
  child process exits

### Requirement: Admission is bounded by a config-driven concurrency semaphore
`HeadlessDispatcher` SHALL bound concurrent headless children to `Config.HeadlessConcurrencyCap`
(additive field in `internal/config/config.go`, default `2` when unset or `<= 0`) via a
semaphore. `Dispatch` SHALL return `ErrConcurrencyCapReached` immediately, without spawning a
process, when no slot is available.

#### Scenario: admission is refused at the cap, not queued internally
- Given: `HeadlessConcurrencyCap` is `2` and two children are already running
- When: `Dispatch` is called for a third item
- Then: it returns `ErrConcurrencyCapReached` immediately and no third process is spawned

#### Scenario: a completed child frees its slot for the next admission
- Given: two of two concurrency slots are in use
- When: one running child's process exits (success or failure)
- Then: its slot becomes available and the next `Dispatch` call for a different item succeeds

#### Scenario: default cap of 2 applies when unset
- Given: `Config.HeadlessConcurrencyCap` is unset (zero value)
- When: `HeadlessDispatcher` is constructed
- Then: it enforces a concurrency cap of `2`, never unbounded admission

### Requirement: Dispatched prompts embed /apply <id> so session linkage requires no new code
`HeadlessDispatcher` SHALL compose the `claude -p` prompt as `/apply <item.ID>` when
`item.TaskProgress` is nil or not started, else `/apply <item.ID> --continue`, reusing
`wavetui-sessions`' existing exact-match session-linkage algorithm (a literal `/apply <id>`
substring scan) with no new linkage code in this proposal.

#### Scenario: a fresh item is dispatched with a plain /apply prompt
- Given: an item with `TaskProgress == nil`
- When: `HeadlessDispatcher` composes its prompt
- Then: the prompt is exactly `/apply <item.ID>`

#### Scenario: a partially-started item is dispatched with --continue
- Given: an item with `TaskProgress` indicating work already started
- When: `HeadlessDispatcher` composes its prompt
- Then: the prompt is exactly `/apply <item.ID> --continue`

#### Scenario: the spawned child's session links to the item with no new linkage code
- Given: a headless child spawned with a composed `/apply <id>` prompt
- When: `wavetui-sessions`' `TranscriptSource` observes that child's transcript
- Then: it links the session to the item via its existing exact-match algorithm — no code in
  `internal/daemon` performs its own session-linkage matching

### Requirement: A rate-limit signal from Snapshot.RateLimitBanner pauses admission until explicit operator resume
`HeadlessDispatcher` SHALL read `Snapshot.RateLimitBanner` (populated by `wavetui-sessions`'
`TranscriptSource`) and, on transition from nil to non-nil, pause new admission (`Dispatch`
returns `ErrQueuePaused` for every new call) without terminating already-running children.
Resuming SHALL require an explicit operator action in `internal/ui/headlessbar.go`; no
timer-based or automatic resume path SHALL exist.

#### Scenario: a rate-limit signal pauses new admission only
- Given: `Snapshot.RateLimitBanner` transitions from nil to non-nil while one child is running
- When: a new `Dispatch` call is made for a different ready item
- Then: it returns `ErrQueuePaused` and no new process is spawned, while the already-running
  child continues uninterrupted

#### Scenario: the pause banner renders while paused
- Given: the headless queue is paused due to a rate-limit signal
- When: `headlessbar.go` renders
- Then: it shows a visible banner naming the pause and the resume keybinding

#### Scenario: resume requires an explicit keypress, never a timer
- Given: the headless queue is paused
- When: any amount of wall-clock time elapses with no operator keypress
- Then: admission remains paused — no code path resumes it automatically

#### Scenario: an explicit operator resume clears the pause
- Given: the headless queue is paused
- When: the operator presses the bound resume key in `headlessbar.go`
- Then: `Dispatch` accepts new admissions again, up to the concurrency cap

### Requirement: A zombie-flagged headless session frees its concurrency slot without auto-release or auto-retry
`HeadlessDispatcher` SHALL stop counting a headless-dispatched item against its concurrency
accounting once `Item.Session.Zombie` becomes `true` (per `wavetui-sessions`' existing
two-signal zombie detection), without killing the underlying process, without releasing the
item's bd claim, and without re-dispatching it. The zombie state SHALL be surfaced only through
`wavetui-sessions`' existing `SessionsPane` badge — this proposal SHALL NOT render a second
zombie indicator.

#### Scenario: a zombied headless item frees its slot for new admission
- Given: a headless-dispatched item whose linked session becomes zombie-flagged
- When: the daemon controller observes the next `Snapshot`
- Then: that item's concurrency slot is freed for a new `Dispatch` call, while the underlying
  process is not killed

#### Scenario: a zombied headless item does not auto-release its claim
- Given: a headless-dispatched item whose linked session is zombie-flagged
- When: the concurrency slot is freed per the scenario above
- Then: the item's bd claim remains held — only the existing one-key operator action in
  `SessionsPane` releases it

#### Scenario: zombie state renders through the existing badge only
- Given: a headless-dispatched item is zombie-flagged
- When: the UI renders
- Then: `SessionsPane`'s existing zombie badge shows it; `headlessbar.go` renders no separate
  zombie indicator for the same item

### Requirement: A non-zero or errored child exit surfaces immediately with no automatic retry
`HeadlessDispatcher` SHALL publish a `HeadlessExitEvent{ItemID, ExitCode, Err, Failed}` onto
`wavetui-core`'s event bus when a headless child's process exits, whether success or failure.
A `Failed` event SHALL surface as an immediate UI-visible failure indication with no backoff loop,
re-attempt, or queue-and-retry-later behavior anywhere in this proposal's code paths.

#### Scenario: a non-zero exit publishes a failed event
- Given: a headless child process exits with a non-zero status
- When: `awaitExit` observes the exit
- Then: it publishes `HeadlessExitEvent{Failed: true, ExitCode: <non-zero>}` and the slot is
  released

#### Scenario: a failed exit is never automatically retried
- Given: a `HeadlessExitEvent` with `Failed: true`
- When: the event is observed by the daemon controller
- Then: no new `Dispatch` call for the same item is issued automatically — a human decides
  whether to re-dispatch

#### Scenario: a successful exit publishes a non-failed event
- Given: a headless child process exits with status `0`
- When: `awaitExit` observes the exit
- Then: it publishes `HeadlessExitEvent{Failed: false, ExitCode: 0}` and the slot is released
