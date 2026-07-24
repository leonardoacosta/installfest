## ADDED Requirements

### Requirement: AccountsSource polls nx-agent and publishes deduped account usage
An `AccountsSource` SHALL poll nx-agent's `GET /credentials` on an interval, dedupe rows by
`(accountEmail, orgUuid)` using the same rules `apps/cc-tmux/src/cc_tmux/usage.py`'s
`dedupe_credentials` documents (an `isActive: True` row always wins within a group regardless of
timestamp; among same-activity rows the one carrying real `usagePolledAt` data wins over one that
doesn't; orphaned `status: "refresh_failed"` rows with no `accountEmail` are dropped before
grouping), then extract each row's 5H/7D utilization ratios and reset epochs. It SHALL
additionally resolve the pane's own active-session 5H/7D via `GET /statusline?sessionId=` (or
`GET /sessions/:id/context`), falling back to the global freshest-active credential only when the
session-scoped value is unavailable — the same fallback order
`apps/cc-tmux/src/cc_tmux/cli.py`'s `_resolve_session_usage` uses. Any unreachable-agent,
non-2xx, or JSON-parse failure SHALL publish an unavailable state, never stale data presented as
live, never a panic.

#### Scenario: nx-agent returns duplicate rows for the same identity
- Given: `/credentials` returns two rows sharing `(accountEmail, orgUuid)`, one `isActive: true`
  with an older `usagePolledAt` and one `isActive: false` with a newer one
- When: `AccountsSource` dedupes the response
- Then: the `isActive: true` row survives regardless of the timestamp comparison

#### Scenario: nx-agent is unreachable
- Given: a request to `/credentials` times out or connection-refuses
- When: the poll fires
- Then: `AccountsSource` publishes an unavailable state and the tab's Accounts section shows a
  badge, not stale numbers

### Requirement: SessionSource resolves each herdr pane's Claude transcript and computes token/cost/severity
A `SessionSource` SHALL, for each herdr-tracked pane whose `AgentSession.Source` is
`"herdr:claude"` or `"claude"`, resolve that pane's transcript path by munging its cwd (every
character outside `[A-Za-z0-9-]` replaced with `-`) to
`~/.claude/projects/<munged-cwd>/<sessionID>.jsonl`, falling back to
`glob(~/.claude/projects/*/<sessionID>.jsonl)` when the munged-cwd path does not exist (session
UUIDs are globally unique, so the first glob match is safe). It SHALL parse assistant-type JSONL
lines only, aggregating usage into a map keyed on `messageID + "\x00" + requestID` where each
repeated key OVERWRITES the prior entry (last-write-wins, so streamed/retried duplicate records
collapse to the final usage numbers) — message count SHALL be the count of unique keys, not raw
line count. Malformed lines SHALL be skipped, never aborting the whole parse. The source SHALL
NOT depend on nx-agent's context-window API for this token count — it computes raw tokens
(`input + cache_read + cache_creation`, summed per deduped turn) directly from the transcript, so
it cannot inherit nx-agent's known percentage-clamp round-trip bug. It SHALL further compute an
estimated dollar cost via a longest-matching-substring model lookup against a pricing table
(input/output USD-per-MTok, cache-read billed at 0.1x and cache-write at 1.25x the matched input
rate) — an unmatched model SHALL contribute $0 cost while still retaining its token counts. It
SHALL resolve a severity tier for the raw token count using the same six-tier ramp
`apps/cc-tmux/src/cc_tmux/render.py`'s `_context_color_pair` defines (dim ≤100k, green >100k,
yellow >200k, orange >300k, red >500k, red/bright-red pulsing >600k, dark-red/red pulsing
>750k), and SHALL flag a pane as past the handoff threshold when its resolved usage ratio is at
or above 0.63 of the pane's context window, carrying the same `!handoff:/workflow:handoff`
label text `render_session_bar` already renders.

#### Scenario: a pane's cwd changed after the transcript path was first resolved
- Given: a transcript exists under a munged directory that does not match the pane's current cwd
- When: `SessionSource` resolves the pane's transcript path
- Then: the munged-cwd lookup misses and the glob-by-session-UUID fallback finds the file

#### Scenario: a streamed message is recorded twice with different token counts
- Given: two JSONL lines share the same `message.id` and `requestId` but the second carries
  higher token counts (the finalized record)
- When: `SessionSource` aggregates usage
- Then: the pane's token count reflects the SECOND (later) record, and the message count treats
  the pair as one turn

#### Scenario: real usage exceeds 200,000 tokens
- Given: a transcript whose true summed usage is 620,000 tokens
- When: `SessionSource` computes the raw token count
- Then: the reported value reflects the real ~620k figure (not a value frozen at "200.0k"), and
  its severity tier resolves to the red/bright-red pulsing (>600k) tier, not green

#### Scenario: an unmatched model still reports tokens
- Given: a transcript entry whose model id matches no pricing-table substring
- When: cost is computed for that turn
- Then: the turn's cost contribution is $0 while its token counts are retained unchanged

#### Scenario: a pane crosses the handoff threshold
- Given: a pane's resolved usage ratio is 0.70 of its context window
- When: the Sessions row for that pane renders
- Then: it carries the `!handoff:/workflow:handoff` suffix in the red severity color

### Requirement: TokenTab renders Accounts (top) and Sessions (bottom) in one herdr tab
A `TokenTab` SHALL open as a herdr-owned tab (`placement = "tab"` in the plugin manifest) showing
two stacked sections built from `AccountsSource` and `SessionSource`'s published state: an
Accounts block per deduped credential (leading `*` marker on the active account only, 5H/7D
percentages, an identity line, up to two reset-countdown lines — matching
`render_accounts_popup`'s existing layout and values for the same live data) above a Sessions
table (one row per tracked Claude pane: project, raw context tokens with severity coloring and
handoff suffix, 5H/7D from that pane's active account, and estimated cost). The plugin SHALL
integrate with herdr via CLI subprocess only (`herdr pane list`, `herdr plugin pane open`) — it
SHALL NOT call `pane.report_metadata` or open a raw socket connection. Status/refresh detection
SHALL be poll-loop-driven as the primary mechanism; the manifest MAY also declare a
`pane.agent_status_changed` event hook, but the plugin SHALL NOT rely on it firing (per the
confirmed herdr 0.7.x gap documented in `docs/reference/herdr-integration-patterns.md`). When
`AccountsSource` reports unavailable, the Accounts section SHALL show a badge while the Sessions
section continues rendering from its own (independent) data — the two sections SHALL degrade
independently, never both failing because one data source is down.

#### Scenario: opening the tab
- Given: nx-agent is reachable and at least one herdr pane is running Claude Code
- When: the operator invokes the tab's open action (keybinding or `herdr plugin action invoke`)
- Then: a herdr tab opens showing the Accounts block above the Sessions table, both populated

#### Scenario: nx-agent is down but transcripts are readable
- Given: `AccountsSource` reports unavailable while `SessionSource` is functioning normally
- When: the tab renders
- Then: the Accounts section shows an unavailable badge and the Sessions table still renders
  real per-pane token/cost/severity data

#### Scenario: the manifest's event hook does not fire
- Given: herdr 0.7.x, matching the documented gap
- When: a tracked pane's agent status changes
- Then: the tab's own poll loop (not the event hook) is what detects and reflects the change
