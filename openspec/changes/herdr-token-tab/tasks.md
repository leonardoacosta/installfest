---
stack: t3
---
<!-- beads:epic:if-9xut -->
<!-- beads:feature:if-y23q -->

<!-- stack: t3 is the documented placeholder for installfest specs (rules/PATTERNS.md), same as
     every archived wavetui-*/ctx-scan-* spec — this is a Go repo with no deploy component. -->

# Implementation Tasks

## DB Batch

- [ ] [1.1] Create `apps/herdr-token-tab/go.mod` (module `herdr-token-tab`, Go 1.22+) and [beads:if-nac0]
  `apps/herdr-token-tab/internal/config/config.go`: `Config` struct (nx-agent base URL default
  `http://localhost:7400`, accounts poll interval default 45s per cc-tmux's own
  `USAGE_CACHE_TTL_SECS`, sessions poll interval default 3s per herdr-token-dashboard's
  precedent, handoff threshold default 0.63 per `_SES_HANDOFF_THRESHOLD`), loaded from an
  optional `.herdr-token-tab.toml` with all-defaults-when-missing semantics (mirrors
  `apps/wavetui/internal/config/config.go`'s own missing-file convention). Add
  `internal/config/config_test.go` covering defaults and override parsing.

## API Batch

- [ ] [2.1] Create `apps/herdr-token-tab/internal/nxagent/client.go`: `Client` with [beads:if-9zbq]
  `Credentials(ctx) ([]Account, error)` (GET /credentials, JSON decode, `DedupeCredentials`
  porting `apps/cc-tmux/src/cc_tmux/usage.py`'s `dedupe_credentials` rules verbatim:
  isActive-wins-within-group regardless of timestamp, data-presence-then-recency tiebreak,
  orphaned refresh_failed rows dropped pre-grouping) and `SessionUsage(ctx, sessionID) (fiveH,
  sevenD *float64, fiveHReset *time.Time, err error)` (GET /statusline?sessionId=, falling back
  to the freshest globally-active credential's usage when the session-scoped call is empty —
  mirrors `_resolve_session_usage`'s fallback order). Any non-2xx/timeout/parse failure returns
  a typed `ErrUnavailable`, never a panic.
  - depends on: 1.1
- [ ] [2.2] Add `apps/herdr-token-tab/internal/nxagent/client_test.go`: stub-HTTP tests for the [beads:if-7zlr]
  duplicate-row dedupe tiebreak (isActive wins regardless of timestamp; data-presence-first
  recency tiebreak; orphaned refresh_failed drop), the session-scoped-then-global-fallback usage
  resolution, and unavailable-on-non-2xx/timeout.
  - depends on: 2.1
- [ ] [2.3] Create `apps/herdr-token-tab/internal/transcript/claude.go`: `MungeClaudePath(cwd [beads:if-x4x6]
  string) string` (every rune outside `[A-Za-z0-9-]` -> `-`), `ResolveSessionPath(sessionID, cwd
  string) string` (munged-cwd path first, `filepath.Glob(~/.claude/projects/*/<sessionID>.jsonl)`
  fallback), and `ReadUsage(path string) (Usage, error)` — parses assistant-type JSONL lines
  only into a `map[string]Turn` keyed on `messageID+"\x00"+requestID` (repeated key overwrites —
  last-write-wins), skips malformed lines, returns aggregated input/output/cache-read/
  cache-creation tokens + message count + per-tool call counts (deduped by `tool_use` block ID).
  Ported from `docs/recon/davidcreador-herdr-token-dashboard.md`'s evidence-verified Card A
  citations.
  - depends on: 1.1
- [ ] [2.4] Add `apps/herdr-token-tab/internal/transcript/claude_test.go`: fixture tests for the [beads:if-gihg]
  glob-fallback-on-cwd-mismatch case, the repeated-message-id-and-requestId dedupe case
  (asserting the LATER record's usage wins), a malformed-line-skip case, and a >200k-tokens case
  asserting the raw count is NOT clamped/frozen — the specific nx-agent failure mode
  (`reference_cc_tmux_model_letter_pipeline_and_roadmap_pulse_sharing` memory) this proposal
  exists to avoid.
  - depends on: 2.3
- [ ] [2.5] Create `apps/herdr-token-tab/internal/pricing/claude.go`: `claudePricing` table [beads:if-cr59]
  (model-substring -> input/output USD-per-MTok) and `Cost(model string, input, output,
  cacheRead, cacheWrite int) float64` — longest-matching-substring selection (`len(substr) >
  best`, not first-match), cache-read at 0.1x and cache-write at 1.25x the matched input rate,
  unmatched model returns 0. Ported from the recon's evidence-verified Card B citations.
  - depends on: 1.1
- [ ] [2.6] Add `apps/herdr-token-tab/internal/pricing/claude_test.go`: longest-match-wins case [beads:if-eg7i]
  (a model string matching both a broad and a specific substring picks the specific one
  regardless of table order) and the unmatched-model-zero-cost-tokens-kept case.
  - depends on: 2.5
- [ ] [2.7] Create `apps/herdr-token-tab/internal/severity/context.go`: `Tier(rawTokens int) [beads:if-lueq]
  Severity` (six-tier ramp: dim <=100k, green >100k, yellow >200k, orange >300k, red >500k,
  red/bright-red pulsing >600k, dark-red/red pulsing >750k — ported verbatim from
  `apps/cc-tmux/src/cc_tmux/render.py`'s `_context_color_pair` thresholds, cited in this file's
  header comment as the source of truth to keep in sync) and `PastHandoff(rawTokens int,
  windowSize int) bool` (ratio >= 0.63, the `_SES_HANDOFF_THRESHOLD` value).
  - depends on: 1.1
- [ ] [2.8] Add `apps/herdr-token-tab/internal/severity/context_test.go`: one case per tier [beads:if-5hjt]
  boundary (100k/200k/300k/500k/600k/750k) and the handoff-threshold boolean at 0.62 vs 0.63
  ratio.
  - depends on: 2.7

## UI Batch

- [ ] [3.1] Create `apps/herdr-token-tab/herdr-plugin.toml`: `id = "leo.herdr-token-tab"`, [beads:if-3jq2]
  `[[panes]] placement = "tab"`, `[[actions]] id = "open-token-tab"`, `[[keys.command]] key =
  "prefix+t"` (herdr-token-dashboard's own `prefix+$` is that upstream plugin's convention — a
  distinct key here), plus a `[[events]] on = "pane.agent_status_changed"` hook declared for
  forward-compat only (per `docs/reference/herdr-integration-patterns.md`, never relied on as
  the working mechanism — see task 3.2's poll loop). Create
  `apps/herdr-token-tab/cmd/herdr-token-tab/main.go`: CLI-subprocess helpers (`fetchPanes()` via
  `herdr pane list --json`, `HERDR_BIN_PATH` env-override, no `pane.report_metadata`/socket usage
  anywhere in this binary — verified by a grep in task 4.1), wiring `nxagent.Client` +
  `transcript`/`pricing`/`severity` into the poll loop feeding the TUI model.
  - depends on: 2.2, 2.4, 2.6, 2.8
- [ ] [3.2] Create `apps/herdr-token-tab/internal/tui/model.go`: bubbletea `Model` rendering the [beads:if-mqj1]
  Accounts block (per-credential `*`-marked active indicator, 5H/7D, identity line, reset
  countdowns — matching `render_accounts_popup`'s layout) above the Sessions table (project, raw
  context tokens with severity coloring + handoff suffix, 5H/7D, cost), each section rendering
  an independent unavailable badge when its own data source reports unavailable (never both
  sections failing together). Add `apps/herdr-token-tab/internal/tui/model_test.go` covering
  both sections' badge-on-unavailable behavior independently and the handoff-suffix rendering
  at/above the 0.63 threshold.
  - depends on: 3.1
- [ ] [3.3] Create `apps/herdr-token-tab/scripts/build.sh` (mirrors herdr-token-dashboard's own [beads:if-uipb]
  `scripts/build.sh` shape: `go build ./cmd/herdr-token-tab`) and create
  `docs/reference/herdr-integration-patterns.md` (the recon's Card A content: transcript
  path-resolution + turn-dedup algorithm, the manifest shape reference, and the
  poll-primary/event-hook-doesn't-reliably-fire-on-0.7.x gotcha — now with this plugin as its
  first real caller, per the recon's own "no current caller" placement-verdict ceiling being
  lifted by this proposal).
  - depends on: 3.2

## E2E Batch

- [ ] [4.1] `go build ./... && go vet ./... && go test ./...` in `apps/herdr-token-tab`: full [beads:if-1psm]
  suite passes; grep the source tree for `report_metadata`/`net.Dial`/`socket` to confirm zero
  matches (CLI-subprocess-only integration, per this proposal's Context section).
  - depends on: 3.3
- [ ] [4.2] Runtime-verify: `herdr plugin link apps/herdr-token-tab` (local-plugin install, per [beads:if-pj0x]
  herdr-token-dashboard's documented `herdr plugin link .` flow), `herdr server reload-config`,
  invoke the open action (or press the configured keybinding) with at least one real Claude Code
  pane running in herdr, confirm the Accounts block renders live 5H/7D matching
  `cc-tmux accounts-popup`'s existing output for the same account, confirm the Sessions row for
  the active pane shows a live raw token count, stop nx-agent (or point `Config.NxAgentURL` at
  an unreachable port) and confirm the Accounts section degrades to a badge while the Sessions
  section keeps rendering — paste terminal/pty output as evidence for each check.
  - depends on: 4.1
