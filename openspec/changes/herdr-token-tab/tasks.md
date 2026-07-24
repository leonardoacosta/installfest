---
stack: t3
---
<!-- beads:epic:if-9xut -->
<!-- beads:feature:if-y23q -->

<!-- stack: t3 is the documented placeholder for installfest specs (rules/PATTERNS.md), same as
     every archived wavetui-*/ctx-scan-* spec — this is a Go repo with no deploy component. -->

# Implementation Tasks

## DB Batch

- [ ] [1.1] Vendor `Davidcreador/herdr-token-dashboard` at commit [beads:if-fgiz]
  `b8c9bbe5c46d976e32ed51146139b0996bf7f334` into `apps/herdr-token-tab`: fetch `LICENSE`
  (verbatim, unmodified), `go.mod`/`go.sum`, `cmd/token-dashboard/main.go` ->
  `cmd/herdr-token-tab/main.go`, `cmd/token-dashboard/claude_test.go` ->
  `cmd/herdr-token-tab/claude_test.go`. Retarget `go.mod`'s module path to
  `github.com/leonardoacosta/installfest/apps/herdr-token-tab` (matching
  `apps/wavetui/go.mod`'s convention). Rename plugin id / binary references throughout
  `main.go` from `dave.token-dashboard`/`token-dashboard` to `leo.herdr-token-tab`/
  `herdr-token-tab`. Add a header comment block at the top of `main.go` citing the fork commit
  and upstream repository URL (`docs/recon/davidcreador-herdr-token-dashboard.md` for context).
- [ ] [1.2] Strip Pi and OpenCode reader code paths from the vendored `main.go` [beads:if-zbft]
  (`readPiSession`, `readOpenCodeLive`/`readOpenCodeDisk`, their switch-case dispatch arms in
  `extractStats`, and any now-unused helper types) — dead code for this fleet's Claude-only
  agent roster. Run `go test ./...` immediately after the strip, before any new code is added,
  to confirm the vendored `claude_test.go`'s Claude-path tests still pass unmodified.
  - depends on: 1.1

## API Batch

- [ ] [2.1] Create `apps/herdr-token-tab/internal/nxagent/client.go`: `Client` with [beads:if-exuf]
  `Credentials(ctx) ([]Account, error)` (GET /credentials, JSON decode, `DedupeCredentials`
  porting `apps/cc-tmux/src/cc_tmux/usage.py`'s `dedupe_credentials` rules verbatim:
  isActive-wins-within-group regardless of timestamp, data-presence-then-recency tiebreak,
  orphaned refresh_failed rows dropped pre-grouping) and `SessionUsage(ctx, sessionID) (fiveH,
  sevenD *float64, fiveHReset *time.Time, err error)` (GET /statusline?sessionId=, falling back
  to the freshest globally-active credential's usage when the session-scoped call is empty —
  mirrors `_resolve_session_usage`'s fallback order). Any non-2xx/timeout/parse failure returns
  a typed `ErrUnavailable`, never a panic.
  - depends on: 1.2
- [ ] [2.2] Add `apps/herdr-token-tab/internal/nxagent/client_test.go`: stub-HTTP tests for the [beads:if-w2yd]
  duplicate-row dedupe tiebreak (isActive wins regardless of timestamp; data-presence-first
  recency tiebreak; orphaned refresh_failed drop), the session-scoped-then-global-fallback usage
  resolution, and unavailable-on-non-2xx/timeout.
  - depends on: 2.1
- [ ] [2.3] Create `apps/herdr-token-tab/internal/severity/context.go`: `Tier(rawTokens int) [beads:if-fboz]
  Severity` (six-tier ramp: dim <=100k, green >100k, yellow >200k, orange >300k, red >500k,
  red/bright-red pulsing >600k, dark-red/red pulsing >750k — ported verbatim from
  `apps/cc-tmux/src/cc_tmux/render.py`'s `_context_color_pair` thresholds, cited in this file's
  header comment as the source of truth to keep in sync) and `PastHandoff(rawTokens int,
  windowSize int) bool` (ratio >= 0.63, the `_SES_HANDOFF_THRESHOLD` value). This colors the
  vendored code's EXISTING token totals — it does not recompute tokens itself.
  - depends on: 1.2
- [ ] [2.4] Add `apps/herdr-token-tab/internal/severity/context_test.go`: one case per tier [beads:if-7cnz]
  boundary (100k/200k/300k/500k/600k/750k) and the handoff-threshold boolean at 0.62 vs 0.63
  ratio.
  - depends on: 2.3

## UI Batch

- [ ] [3.1] Create `apps/herdr-token-tab/herdr-plugin.toml` (new manifest, not vendored — [beads:if-vy6w]
  upstream's own manifest fields replaced with ours): `id = "leo.herdr-token-tab"`,
  `[[panes]] placement = "tab"`, `[[actions]] id = "open-token-tab"`, `[[keys.command]] key =
  "prefix+t"` (distinct from upstream's own `prefix+$`), plus a `[[events]] on =
  "pane.agent_status_changed"` hook declared for forward-compat only (per
  `docs/reference/herdr-integration-patterns.md`, never relied on — the vendored poll loop
  stays the working mechanism). Wire `internal/nxagent.Client` and `internal/severity` into the
  vendored `main.go`'s existing poll loop and `HERDR_BIN_PATH`-aware subprocess helpers.
  - depends on: 2.2, 2.4
- [ ] [3.2] Add an Accounts render block to the vendored `main.go`'s Bubble Tea `View()`, [beads:if-35pz]
  positioned ABOVE the existing Sessions summary table (per-credential `*`-marked active
  indicator, 5H/7D, identity line, reset countdowns — matching `render_accounts_popup`'s
  layout); extend `Update()`/`Init()` to poll `AccountsSource` alongside the vendored pane-list
  poll; apply `severity.Tier`/`PastHandoff` coloring onto each Sessions row's existing token
  count (additive to the vendored dollar-cost coloring, never replacing it). Add or extend a
  test file covering: the Accounts section's independent unavailable-badge behavior (nx-agent
  down, Sessions keeps rendering), and the handoff-suffix rendering at/above the 0.63 threshold.
  - depends on: 3.1

## E2E Batch

- [ ] [4.1] `go build ./... && go vet ./... && go test ./...` in `apps/herdr-token-tab`: full [beads:if-6ybq]
  suite passes, including the vendored `claude_test.go`'s carried-over cases; grep the source
  tree for `report_metadata`/`net.Dial`/`socket` to confirm zero matches (CLI-subprocess-only
  integration, matching upstream's own posture).
  - depends on: 3.2
- [ ] [4.2] Runtime-verify: `herdr plugin link apps/herdr-token-tab`, `herdr server [beads:if-3ctl]
  reload-config`, invoke the open action (or press the configured keybinding) with at least one
  real Claude Code pane running in herdr, confirm the Accounts block renders live 5H/7D matching
  `cc-tmux accounts-popup`'s existing output for the same account, confirm the Sessions row for
  the active pane shows a live token count with severity coloring, stop nx-agent (or point the
  client at an unreachable port) and confirm the Accounts section degrades to a badge while the
  Sessions section keeps rendering — paste terminal/pty output as evidence for each check.
  - depends on: 4.1
