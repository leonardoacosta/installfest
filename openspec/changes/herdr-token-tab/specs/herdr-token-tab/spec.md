## ADDED Requirements

### Requirement: Vendored base provides per-session token/cost via herdr-token-dashboard's own engine
`apps/herdr-token-tab` SHALL be forked from `Davidcreador/herdr-token-dashboard` at commit
`b8c9bbe5c46d976e32ed51146139b0996bf7f334` (MIT), retaining the upstream `LICENSE` file
unmodified and recording the fork commit + upstream URL in a header comment at the top of the
vendored `main.go`. The vendored code SHALL retain its existing Claude Code transcript
resolution (munge-cwd path construction, glob-by-session-UUID fallback), turn-level dedup
(`messageID + "\x00" + requestID` key, last-write-wins), and cost-estimation pricing table
(longest-matching-substring model lookup, cache-read at 0.1x and cache-write at 1.25x the
matched input rate) unmodified in behavior. Upstream's Pi and OpenCode reader code paths SHALL
be removed during the vendor step (dead code for this fleet's Claude-only agent roster) — the
Claude reader path SHALL NOT be altered by this removal.

#### Scenario: vendored transcript reading behaves identically to upstream
- Given: the vendored `claude_test.go` (carried over from upstream, unmodified)
- When: `go test ./...` runs against the vendored tree before any augmentation is added
- Then: every test passes, confirming the fork introduced no behavioral change

#### Scenario: Pi/OpenCode removal does not affect the Claude path
- Given: the vendor step has stripped Pi and OpenCode reader code
- When: `go test ./...` runs
- Then: all Claude-path tests still pass unmodified — the removal touched only dead branches

#### Scenario: the LICENSE and attribution survive the fork
- Given: `apps/herdr-token-tab` after vendoring
- When: `apps/herdr-token-tab/LICENSE` is inspected
- Then: it is byte-identical to upstream's MIT license text, and the vendored `main.go`'s header
  comment names the fork commit and upstream repository URL

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

### Requirement: SeverityRamp colors vendored token counts and flags the handoff threshold
A `severity.Tier(rawTokens int)` function SHALL classify the vendored code's own raw token count
(input + cache-read + cache-creation, as already computed by the vendored transcript reader)
into the same six severity tiers `apps/cc-tmux/src/cc_tmux/render.py`'s `_context_color_pair`
defines (dim ≤100k, green >100k, yellow >200k, orange >300k, red >500k, red/bright-red pulsing
>600k, dark-red/red pulsing >750k). A `severity.PastHandoff(rawTokens, windowSize int) bool`
SHALL flag a pane whose usage ratio is at or above 0.63 of its context window (the
`_SES_HANDOFF_THRESHOLD` value), carrying the same `!handoff:/workflow:handoff` label text
`render_session_bar` already renders. This severity coloring SHALL be rendered ALONGSIDE the
vendored code's existing dollar-cost coloring (green <$5, yellow <$25, red >$25) — it augments
the Sessions row, it does not replace or reinterpret the vendored cost column.

#### Scenario: real usage exceeds 200,000 tokens
- Given: the vendored transcript reader computes a real 620,000-token total for a pane
- When: `severity.Tier` classifies that count
- Then: it resolves to the red/bright-red pulsing (>600k) tier

#### Scenario: a pane crosses the handoff threshold
- Given: a pane's resolved usage ratio is 0.70 of its context window
- When: the Sessions row for that pane renders
- Then: it carries the `!handoff:/workflow:handoff` suffix in the red severity color, alongside
  its unmodified vendored cost-column coloring

### Requirement: TokenTab renders Accounts (top) above the vendored Sessions table (bottom)
The plugin SHALL open as a herdr-owned tab (`placement = "tab"` in a manifest rebranded from
upstream's `dave.token-dashboard` to this fork's own plugin id and keybinding) rendering an
Accounts block (per `AccountsSource`) above the vendored Sessions table (per the vendored
transcript/pricing engine, now decorated with `SeverityRamp`'s coloring). The plugin SHALL
integrate with herdr via CLI subprocess only (`herdr pane list`, `herdr plugin pane open`) — it
SHALL NOT call `pane.report_metadata` or open a raw socket connection, matching upstream's own
integration posture. Status/refresh detection SHALL be poll-loop-driven as the primary
mechanism; the manifest MAY declare a `pane.agent_status_changed` event hook but the plugin
SHALL NOT rely on it firing (per the confirmed herdr 0.7.x gap in
`docs/reference/herdr-integration-patterns.md`). When `AccountsSource` reports unavailable, the
Accounts section SHALL show a badge while the vendored Sessions section continues rendering
independently.

#### Scenario: opening the tab
- Given: nx-agent is reachable and at least one herdr pane is running Claude Code
- When: the operator invokes the tab's open action
- Then: a herdr tab opens showing the Accounts block above the Sessions table, both populated

#### Scenario: nx-agent is down but transcripts are readable
- Given: `AccountsSource` reports unavailable while the vendored Sessions engine is functioning
- When: the tab renders
- Then: the Accounts section shows an unavailable badge and the Sessions table still renders
  real per-pane token/cost/severity data, unaffected
