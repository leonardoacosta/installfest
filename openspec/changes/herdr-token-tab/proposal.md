---
order: 0724a
---

# Proposal: herdr-token-tab — vendored token dashboard + nx-agent accounts augmentation

## Change ID
`herdr-token-tab`

## Summary
Vendor `Davidcreador/herdr-token-dashboard` (MIT, fork point commit `b8c9bbe5c46d976e32
ed51146139b0996bf7f334`) into `apps/herdr-token-tab` as the base for the **Sessions** section
(per-pane token/cost, already implemented and tested upstream), then augment it with two things
it doesn't have: an **Accounts** section (top — active-account indication, 5H/7D, reset
countdowns, sourced from nx-agent, ported from cc-tmux's `usage.py`) and a context-limit
severity ramp + 63%-handoff warning on the Sessions rows (ported from cc-tmux's `render.py`).
Supersedes this proposal's own prior "build every piece from scratch" plan — Leo's direct ask
this session, after `openspec validate` and beads-sync had already landed on the from-scratch
version with zero code written.

## Context
- depends on:
- touches: `apps/herdr-token-tab/LICENSE (new)`, `apps/herdr-token-tab/go.mod (new)`,
  `apps/herdr-token-tab/go.sum (new)`, `apps/herdr-token-tab/herdr-plugin.toml (new)`,
  `apps/herdr-token-tab/cmd/herdr-token-tab/main.go (new)`,
  `apps/herdr-token-tab/cmd/herdr-token-tab/claude_test.go (new)`,
  `apps/herdr-token-tab/internal/nxagent/client.go (new)`,
  `apps/herdr-token-tab/internal/nxagent/client_test.go (new)`,
  `apps/herdr-token-tab/internal/severity/context.go (new)`,
  `apps/herdr-token-tab/internal/severity/context_test.go (new)`,
  `apps/herdr-token-tab/scripts/build.sh (new)`,
  `docs/reference/herdr-integration-patterns.md (new)`
- **Origin**: this session's `/recon` on `Davidcreador/herdr-token-dashboard`
  (`docs/recon/davidcreador-herdr-token-dashboard.md`), a design/mockup pass, an initial
  from-scratch spec (validated + beads-synced), then Leo's direct ask to vendor instead —
  captured here as a revision of the SAME proposal, not a second one (nothing had shipped).
- **Capability Preflight**: not applicable — see prior ruling, unchanged (Leo: skip).
- **Vendoring approach (this session's revision)**: `apps/herdr-token-tab` starts as a fork of
  upstream's tree at commit `b8c9bbe5c46d976e32ed51146139b0996bf7f334`
  (`cmd/token-dashboard/{main.go,claude_test.go}`, `LICENSE`, `go.mod`/`go.sum`), retargeted to
  our own module path/binary name/plugin id/keybinding. MIT requires the copyright + permission
  notice survive in copies — `LICENSE` is vendored verbatim (unmodified) at the app root; the
  fork commit + upstream URL are recorded in a header comment at the top of the vendored
  `main.go` so a future re-sync from upstream has a clear diff base. This gives the Sessions
  section its transcript reader (`mungeClaudePath`/`claudeSessionPath`, `messageID+requestID`
  turn dedup), pricing table (longest-substring match, cache multipliers), and Bubble Tea
  table/detail-card rendering for free, already tested (the recon's Card A/B, evidence-verified
  6/6 and 4/4) — **this REPLACES the prior plan's from-scratch `internal/transcript` and
  `internal/pricing` packages**; that code no longer needs writing, it needs vendoring.
  Upstream's Pi and OpenCode reader code paths are stripped during the vendor step — genuinely
  dead code for this fleet (Claude-only agent roster, confirmed in the recon's Monitor
  verdicts) — kept only as a documented revert-if-needed note (the fork commit) rather than
  live, untested-by-us code paths.
- **What still gets built new (the actual augmentation)**:
  1. **Accounts section** — `internal/nxagent/client.go`, unchanged from the prior plan: `GET
     /credentials` with `usage.py`'s `dedupe_credentials` rules ported verbatim (isActive-wins-
     within-group, data-presence-then-recency tiebreak, orphaned-refresh_failed drop), plus
     `GET /statusline?sessionId=`/`GET /sessions/:id/context` for 5H/7D with the same
     session-scoped-then-global-fallback order `_resolve_session_usage` uses. Wired into the
     vendored `main.go`'s Bubble Tea `View()` as a new block ABOVE the existing summary table —
     the vendored code owns Sessions, this proposal's own code owns Accounts, one TUI.
  2. **Severity ramp + handoff warning** — `internal/severity/context.go`, unchanged from the
     prior plan: the same six-tier ramp (dim ≤100k, green >100k, yellow >200k, orange >300k,
     red >500k, red/bright-red pulsing >600k, dark-red/red pulsing >750k) and 63%-of-window
     handoff threshold ported from `apps/cc-tmux/src/cc_tmux/render.py`'s
     `_context_color_pair`/`_SES_HANDOFF_THRESHOLD`. The vendored code already computes raw
     input/output/cache tokens per pane (that's the whole point of vendoring it) but only colors
     its COST column by a dollar-threshold ramp (green <$5, yellow <$25, red >$25) — this
     augmentation adds a SEPARATE token-severity color + handoff suffix to the existing token
     count, it does not touch or replace the vendored cost coloring.
  - **Why this is still correct given the earlier nx-agent-SES-bug finding**: unchanged —
    vendoring doesn't reintroduce that bug. The vendored code's token counts come from direct
    transcript summation (upstream's own algorithm, evidence-verified), never from nx-agent's
    lossy percentage round-trip. nx-agent is used ONLY for the Accounts section here, exactly as
    already decided.
- **Runtime host**: unchanged — herdr replaces tmux for these sessions; this targets
  herdr-hosted Claude Code panes (`AgentSession.Source == "herdr:claude"`); `apps/cc-tmux`
  untouched, out of scope.
- **Reuse-not-rebuild (Reader Gate), revised**: the prior version of this proposal already
  established no existing Go nx-agent/herdr client exists in this repo, and that `apps/wavetui`
  is a different domain (project/beads queue, not session/account telemetry) and not
  herdr-wired — both findings stand unchanged. What changes here is the Sessions section's
  origin: rather than re-deriving Card A/B's algorithms as new files (the prior plan), the
  actual upstream source is vendored directly — a stronger application of the same Reader Gate
  principle (reuse existing code ranks above writing new code, and vendoring proven+tested
  upstream code ranks above re-deriving the same algorithm from a recon's citations).

## Motivation
Unchanged from the prior version: cc-tmux's tmux status bar has no herdr equivalent once
sessions move off tmux, and cc-tmux's own SES pipeline has a confirmed live clamp bug this
design already sidesteps. Revised this session: rather than re-implement herdr-token-dashboard's
already-working, already-tested transcript/pricing engine from citations, fork it directly and
spend the actual engineering effort only on what's genuinely new — the Accounts section and the
token-severity ramp.

## Requirements

### Requirement: Vendored base provides per-session token/cost via herdr-token-dashboard's own engine
See `specs/herdr-token-tab/spec.md`.

### Requirement: AccountsSource polls nx-agent and publishes deduped account usage
See `specs/herdr-token-tab/spec.md`.

### Requirement: SeverityRamp colors vendored token counts and flags the handoff threshold
See `specs/herdr-token-tab/spec.md`.

### Requirement: TokenTab renders Accounts (top) above the vendored Sessions table (bottom)
See `specs/herdr-token-tab/spec.md`.

## Scope
- **IN**: vendoring `Davidcreador/herdr-token-dashboard` at commit `b8c9bbe5...` into
  `apps/herdr-token-tab` (LICENSE retained, module/binary/plugin-id/keybinding retargeted to
  ours, Pi/OpenCode paths stripped); `internal/nxagent` client (credentials dedupe, 5H/7D
  resolution); `internal/severity` package (6-tier ramp, 63% handoff threshold) wired onto the
  vendored code's existing token totals; Accounts section wired into the vendored TUI's render
  path above the existing Sessions table; `docs/reference/herdr-integration-patterns.md`.
- **OUT**: unchanged from the prior version — `apps/cc-tmux` modification/decommission, Pi/
  OpenCode session support (stripped, not built), a fix to nx-agent's own clamp bug,
  notifications/toasts, Windows support. Additionally OUT this revision: any ongoing
  upstream-sync mechanism (this is a one-time fork at a recorded commit, not a tracked
  dependency — a future re-vendor is a manual, deliberate act, not automated).

## Done Means
- Leo presses the configured keybinding and a herdr tab opens showing an Accounts block per
  credential (active `*` marker, 5H/7D, identity, reset countdowns) above the vendored Sessions
  table (per-pane project, tokens, cost — upstream's own rendering, unmodified in shape).
- Each Sessions row additionally shows a token-severity color and, at/above 63% of that pane's
  context window, the `!handoff:/workflow:handoff` suffix — new coloring on top of the vendored
  token count, not a replacement of the vendored cost column.
- With nx-agent unreachable, the Accounts section shows an unavailable badge while the Sessions
  section (vendored, transcript-based, independent) keeps working.
- `apps/herdr-token-tab/LICENSE` is present and unmodified from upstream; the vendored
  `main.go`'s header comment names the fork commit and upstream URL.

## Testing
| Affected seam | Unit task | E2E task |
|----------------|-----------|----------|
| Vendored `readClaudeSession`/pricing (upstream's own tests, carried over) | `[1.2]` (vendored `claude_test.go` still passes) | `[4.2]` |
| `internal/nxagent/client.go` (credentials dedupe/active-resolve, statusline/context fetch) | `[2.2]` | `[4.2]` |
| `internal/severity/context.go` (6-tier ramp, 63% handoff threshold) | `[2.4]` | `[4.2]` |
| Accounts section wiring into vendored `main.go`'s `View()` | `[3.2]` | `[4.2]` |
| `herdr-plugin.toml` (rebranded manifest, poll-primary detection) | N/A (config, not code) | `[4.2]` |

## Preconditions
- `herdr --version` reports `0.7.0` or newer (confirmed `herdr 0.7.5` this session).
- `go version` reports Go 1.26+ (upstream's own `go.mod` states `go 1.26.3` — stricter than this
  proposal's prior `1.22+` precondition; verify the installed toolchain meets this before
  vendoring, not after).
- `curl -sf http://localhost:7400/credentials -o /dev/null -w '%{http_code}'` returns `200`.
- `herdr plugin list` succeeds (confirmed working this session via `herdr-file-viewer`).
- `~/.claude/projects/` exists and is non-empty.
- `gh api repos/Davidcreador/herdr-token-dashboard/commits/main --jq '.sha'` still resolves to
  `b8c9bbe5c46d976e32ed51146139b0996bf7f334` at vendor time — if upstream has moved, re-confirm
  the fork point deliberately (fetch and review the diff) rather than silently vendoring a newer
  unreviewed commit.

## Impact
| Area | Change |
|------|--------|
| `apps/herdr-token-tab/` | New — vendored fork (LICENSE + main.go + claude_test.go, retargeted) plus two new-written packages (`internal/nxagent`, `internal/severity`) |
| `docs/reference/herdr-integration-patterns.md` | New — unchanged from the prior version (manifest shape, poll-primary gotcha; the transcript-algorithm content now doubles as vendoring context rather than a from-scratch build guide) |
| `apps/cc-tmux/` | Untouched |
| `openspec/specs/herdr-token-tab/spec.md` | Revised `## ADDED Requirements` — vendored-base requirement replaces the from-scratch SessionSource requirement |

## Risks
| Risk | Mitigation |
|------|-----------|
| Vendored `go.mod` pins Go 1.26.3 / bubbletea v2 — heavier toolchain requirement than a from-scratch build would need | Stated as an explicit Precondition, checked before vendoring, not discovered mid-build |
| Vendored code's Pi/OpenCode strip introduces a bug (dead-code removal touching live paths) | `go test ./...` on the vendored tree runs immediately after stripping, before any augmentation — a failing test here is caught before new code is layered on top |
| Upstream fixes a bug or ships a feature after this fork point | Fork commit is recorded explicitly (proposal + `main.go` header) — a deliberate future re-vendor is possible; not silently missed since there's no auto-sync to silently skip either |
| nx-agent down/unreachable | Unchanged — Accounts section degrades independently, Sessions section (vendored, transcript-based) keeps working |
| Severity/handoff thresholds drift from `render.py`'s if tuned later | Unchanged — same repo, cross-referenced in `internal/severity/context.go`'s header comment |
| Cost estimate wrong for an unmatched model | Unchanged — vendored behavior, evidence-verified: $0 cost, tokens retained |
