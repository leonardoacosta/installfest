## ADDED Requirements

### Requirement: CtxScanSource polls the current project and publishes a context report
A `CtxScanSource` SHALL shell out to `ctx-scan view-model --root <repo-root>` (the project
wavetui is running in — never a fleet-wide root), decode the band-annotated payload, verify its
`schemaVersion`, and publish it to the bus as a typed event applied by the single Store writer
onto the snapshot. Invocations SHALL be driven by a poll ticker (interval from the
`ctx_scan_poll_seconds` config knob, default 60) and by a coalesced manual-refresh trigger, with
at most one shellout in flight — a burst of triggers produces one invocation. Any exec, decode,
or schema-version failure SHALL publish an error string while retaining the last-good report,
and SHALL back off exponentially — never a panic, never fabricated data. The manual-refresh
trigger SHALL be a non-blocking signal into the source's goroutine; no UI code invokes the CLI
directly.

#### Scenario: a poll tick refreshes the report
- Given: the source is running and `ctx-scan` succeeds
- When: the poll interval elapses
- Then: a new report event reaches the Store and the snapshot's context report updates

#### Scenario: rapid manual refreshes coalesce into one invocation
- Given: a scan is already in flight
- When: the refresh trigger fires multiple times before it completes
- Then: exactly one follow-up invocation runs after the current one finishes

#### Scenario: a missing or failing ctx-scan binary degrades to a badge
- Given: the `ctx-scan` executable is absent from PATH or exits non-zero
- When: an invocation is attempted
- Then: the snapshot carries the error string and the last-good report (if any) is retained —
  no panic, no partial decode rendered

#### Scenario: a schemaVersion mismatch is an error, not a garbage render
- Given: the CLI emits a payload with an unexpected `schemaVersion`
- When: the source decodes it
- Then: an error is published and the payload is discarded

### Requirement: ContextPane renders the context breakdown as a drill-down tab
A `ContextPane` SHALL render the current project's context report as the full-screen
`[3] Context` tab, implementing the shared pane interface. It SHALL present three drill levels —
class breakdown (one row per context class with estimated tokens and GREEN/AMBER/RED band
styling), documents within a selected class (tokens, band, truncation flags), and single-document
detail (tier, origin, raw vs effective chars, violations) — navigated with `j`/`k` cursor,
`enter` to descend, `esc` to ascend. The `r` key SHALL invoke the injected refresh trigger only;
per the existing snapshot-delivery requirement, the pane's update path never calls a source or
CLI. When the snapshot carries a context-scan error, the pane SHALL render an unavailable badge
with that error instead of report content.

#### Scenario: pressing 3 shows the class breakdown
- Given: a snapshot carrying a context report
- When: the operator presses `3`
- Then: the Context tab renders one row per context class with token estimates and band colors

#### Scenario: drill-down reaches document detail and walks back
- Given: the class breakdown is shown
- When: the operator selects a class, presses `enter`, selects a document, presses `enter`,
  then presses `esc` twice
- Then: the pane shows class documents, then document detail, then returns to the class
  breakdown with cursor position preserved

#### Scenario: manual refresh updates the numbers in place
- Given: the operator edits a config file that changes a document's size
- When: the operator presses `r` and the rescan completes
- Then: the affected rows re-render with updated tokens and bands, on whatever drill level is
  currently shown

#### Scenario: scan failure renders a badge, not a crash
- Given: the snapshot carries a context-scan error string
- When: the Context tab renders
- Then: an unavailable badge with the error is shown; if a last-good report exists it remains
  visible, marked stale
