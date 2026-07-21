## ADDED Requirements

### Requirement: Dispatcher interface abstracts prompt delivery behind Dispatch(item, promptText)
`apps/wavetui/internal/dispatch` SHALL define a `Dispatcher` interface with a single method,
`Dispatch(ctx context.Context, item store.Item, promptText string) error`, implemented by
`TmuxDispatcher` and `ClipboardDispatcher`. The signature MUST NOT assume a tmux pane exists, so
a future `HeadlessDispatcher` (out of scope here) can implement it without a breaking change.

#### Scenario: interface has exactly one method
- Given: the `Dispatcher` interface definition
- When: a new implementation is added
- Then: it only needs to implement `Dispatch(ctx, item, promptText) error` — no tmux-specific
  method leaks into the interface

#### Scenario: a future HeadlessDispatcher can implement the same signature
- Given: the `Dispatcher` interface as shipped by this proposal
- When: a later proposal adds `HeadlessDispatcher` (a `claude -p` scheduler)
- Then: it implements `Dispatch(ctx, item, promptText) error` with no change to the interface

### Requirement: TmuxDispatcher targets a linked or best-guess pane via bracketed paste, never a literal multi-line send-keys
`TmuxDispatcher` SHALL resolve a target pane by preferring `item.Session.PaneID` (set by
`wavetui-sessions`) when present, else scoring candidates from `cc-tmux conductor list --json`
same-window > same-session > other, with ties prompting the operator rather than picking
silently. Delivery MUST use `tmux load-buffer` + `paste-buffer -p` (bracketed paste) followed by
a SEPARATE `send-keys Enter` call — never a single `send-keys` invocation carrying a literal
multi-line prompt string.

#### Scenario: linked session pane is used directly
- Given: an item with `Session.PaneID` set by `wavetui-sessions`
- When: `TmuxDispatcher.Dispatch` runs
- Then: it targets that pane ID directly, skipping candidate scoring

#### Scenario: same-window candidate outranks same-session
- Given: no linked session, and two candidate panes — one in the same tmux window as the
  item's project, one merely in the same tmux session
- When: candidates are scored
- Then: the same-window candidate is selected

#### Scenario: a scoring tie prompts rather than picks silently
- Given: two candidate panes with equal score
- When: `TmuxDispatcher` resolves a target
- Then: the operator is prompted to choose; no candidate is picked automatically

#### Scenario: multi-line prompt is delivered via bracketed paste, not literal send-keys
- Given: a `promptText` containing embedded newlines
- When: `TmuxDispatcher` delivers it to a pane
- Then: it uses `load-buffer` + `paste-buffer -p`, followed by a separate `send-keys Enter` call
  — at no point is the full multi-line string passed to a single `send-keys -l` invocation

### Requirement: TmuxDispatcher refuses a pane whose linked session is mid-turn or in copy-mode
`TmuxDispatcher` MUST check `#{pane_in_mode}` before pasting and refuse (surfacing a warning,
never force-pasting) when the pane is in copy-mode. It MUST also refuse when the target item's
linked session (per `wavetui-sessions`) is actively streaming, queuing or warning rather than
blind-pasting into a generating REPL.

#### Scenario: copy-mode pane is refused
- Given: the target pane's `#{pane_in_mode}` reads `1`
- When: `TmuxDispatcher.Dispatch` runs
- Then: the dispatch is refused with a copy-mode-specific error, no paste is attempted

#### Scenario: a mid-turn session is refused, never blind-pasted
- Given: the target item's linked session is currently streaming (per `wavetui-sessions`'
  transcript-derived state)
- When: `TmuxDispatcher.Dispatch` runs
- Then: the dispatch is refused and `QueuePane` renders "queued — session busy"; no paste
  reaches the pane

#### Scenario: an idle linked session proceeds normally
- Given: the target item's linked session is idle (not streaming)
- When: `TmuxDispatcher.Dispatch` runs
- Then: the paste proceeds through the normal active-pane-state check

### Requirement: ClipboardDispatcher is the fallback when no tmux target exists
`ClipboardDispatcher` SHALL write `promptText` via an OSC52 escape sequence to `/dev/tty` when
OSC52 support is detected (or forced via a per-project config override), else fall back to a
pbcopy-equivalent resolved via `exec.LookPath` in the order `pbcopy` (Darwin) ->
`xclip -selection clipboard` -> `xsel --clipboard --input` -> `wl-copy`, surfacing failure rather
than silently no-op'ing if none resolve. It is used whenever `TmuxDispatcher` finds zero
candidate panes or no tmux session exists at all.

#### Scenario: OSC52 is used when detected
- Given: the terminal advertises OSC52 support (or the config override forces it)
- When: `ClipboardDispatcher.Dispatch` runs
- Then: the prompt is written as an OSC52 sequence to `/dev/tty`, no external process is spawned

#### Scenario: pbcopy-equivalent fallback probes real binaries, not shell aliases
- Given: OSC52 is not detected and no config override forces it, on a Linux host with `xclip`
  installed but no zsh alias in the invoking process's environment
- When: `ClipboardDispatcher.Dispatch` runs
- Then: it resolves `xclip -selection clipboard` via `exec.LookPath` directly — it does not
  attempt to exec a literal `pbcopy` command name on Linux

#### Scenario: zero tmux candidates falls back to clipboard
- Given: `cc-tmux conductor list --json` returns an empty pane array
- When: Start is triggered on an item with no linked session
- Then: `ClipboardDispatcher` is used instead of `TmuxDispatcher`

#### Scenario: no tmux session at all falls back to clipboard
- Given: `$TMUX` is unset and no tmux server is reachable
- When: Start is triggered
- Then: `ClipboardDispatcher` is used, never a hard failure

### Requirement: Dispatch-boundary values are validated against an id-shaped regex before crossing into a shell or tmux buffer
Any value interpolated into a dispatch command (pane IDs, item IDs) SHALL be validated against
`^[A-Za-z0-9_-]+$` before crossing the dispatch boundary. `promptText` is exempt from this check
(it is free-form prose delivered exclusively via paste-buffer/OSC52 payload, never via shell
argument interpolation) — bead/proposal titles and notes MUST NOT be substituted for an id at
this boundary.

#### Scenario: a non-id-shaped pane ID is rejected
- Given: a resolved pane target string containing a shell metacharacter
- When: `validateDispatchTarget` runs on it
- Then: it returns an error and no tmux command is issued

#### Scenario: promptText is never regex-validated
- Given: a `promptText` containing arbitrary punctuation and newlines
- When: `TmuxDispatcher.Dispatch` runs
- Then: `promptText` passes through unchanged to the paste buffer without being matched against
  the id-shaped regex

### Requirement: Dispatch failures surface immediately with no automatic retry
A `Dispatcher.Dispatch` failure SHALL surface as an immediate UI-visible failure badge with no
backoff loop, re-attempt, or queue-and-retry-later behavior in this proposal's code paths.

#### Scenario: a tmux paste failure surfaces once, not retried
- Given: `TmuxDispatcher.Dispatch` returns an error (e.g. the pane died mid-dispatch)
- When: the error propagates to the caller
- Then: `QueuePane` renders a failure badge and no automatic re-attempt occurs

#### Scenario: a clipboard write failure surfaces, not silently swallowed
- Given: `ClipboardDispatcher.Dispatch` fails (no resolvable binary and OSC52 undetected)
- When: the error propagates
- Then: the failure is rendered, not silently dropped

### Requirement: QueuePane Start dispatches the selected item with one keypress
`QueuePane` SHALL bind a "Start" key that dispatches the currently-selected item's rendered
prompt via the resolved `Dispatcher`, with the prompt landing in the target pane (or clipboard)
without additional operator steps.

#### Scenario: Start dispatches the highlighted item
- Given: one item is highlighted in `QueuePane`
- When: the operator presses the Start key
- Then: that item's prompt is dispatched via the resolved `Dispatcher` in one action

### Requirement: QueuePane select mode builds a wave plan with file-overlap conflict warnings
`QueuePane` SHALL support a multi-select mode that accumulates candidate items ordered by
`Item.FanOutScore`, computing file-overlap conflicts across candidates' `Item.TouchedFiles` and
rendering one warning row per conflicting path (naming both item IDs) before the operator
finalizes. Finalizing writes a wave file per the format decided in `design.md` § Open Question
(gated on operator confirmation, not implemented until then).

#### Scenario: multi-select accumulates candidates ordered by fan-out score
- Given: the operator multi-selects three items with differing `FanOutScore`
- When: the wave-builder view renders the selection
- Then: candidates are ordered by `FanOutScore` descending

#### Scenario: overlapping touched files are flagged before finalizing
- Given: two selected candidates whose `TouchedFiles` share a path
- When: the wave-builder view renders
- Then: a warning row names both item IDs and the shared path — neither candidate is silently
  dropped from the selection

#### Scenario: finalizing with no conflicts proceeds
- Given: a selection with zero file-overlap conflicts
- When: the operator finalizes
- Then: a wave file is written per the confirmed format
