---
order: 0724a
---

# Proposal: herdr-token-tab — accounts + per-session token/cost tab for herdr

## Change ID
`herdr-token-tab`

## Summary
A new Go herdr plugin (`apps/herdr-token-tab`) that opens an owned herdr tab showing two
sections: **Accounts** (top — active-account indication, 5H/7D usage, reset countdowns, sourced
from cc-tmux's existing nx-agent HTTP pipeline) and **Sessions** (bottom — per-pane token usage,
cost, and a context-limit meter/warning). This is the concrete build-out of the design explored
in `docs/recon/davidcreador-herdr-token-dashboard.md`.

## Context
- depends on:
- touches: `apps/herdr-token-tab/go.mod (new)`, `apps/herdr-token-tab/herdr-plugin.toml (new)`,
  `apps/herdr-token-tab/internal/config/config.go (new)`,
  `apps/herdr-token-tab/internal/config/config_test.go (new)`,
  `apps/herdr-token-tab/cmd/herdr-token-tab/main.go (new)`,
  `apps/herdr-token-tab/internal/nxagent/client.go (new)`,
  `apps/herdr-token-tab/internal/nxagent/client_test.go (new)`,
  `apps/herdr-token-tab/internal/transcript/claude.go (new)`,
  `apps/herdr-token-tab/internal/transcript/claude_test.go (new)`,
  `apps/herdr-token-tab/internal/pricing/claude.go (new)`,
  `apps/herdr-token-tab/internal/pricing/claude_test.go (new)`,
  `apps/herdr-token-tab/internal/severity/context.go (new)`,
  `apps/herdr-token-tab/internal/severity/context_test.go (new)`,
  `apps/herdr-token-tab/internal/tui/model.go (new)`,
  `apps/herdr-token-tab/internal/tui/model_test.go (new)`,
  `apps/herdr-token-tab/scripts/build.sh (new)`,
  `docs/reference/herdr-integration-patterns.md (new)`
- **Origin**: this session's `/recon` on `Davidcreador/herdr-token-dashboard`
  (`docs/recon/davidcreador-herdr-token-dashboard.md`) plus a live design/mockup pass, refined
  further into this proposal by Leo's direct ask.
- **Capability Preflight**: not applicable — greenfield probe flagged `VERCEL_TOKEN` missing, but
  this is a local herdr terminal plugin with no Vercel/deploy component (same "generic
  template check on a non-web repo" posture wavetui/ctx-scan proposals cite for
  `stack: t3`-as-placeholder). Leo's explicit call this session: skip, no task added.
- **Data-source decision (Leo's ask, evaluated this session)**: split by section, not a single
  winner-take-all source —
  - **Accounts (top section)**: nx-agent HTTP only — `GET /credentials` (via the SAME dedupe/
    active-resolution logic `apps/cc-tmux/src/cc_tmux/usage.py` already implements:
    `dedupe_credentials`, `_extract_util`, `_extract_reset_at`, `_account_label`) and
    `GET /statusline?sessionId=`/`GET /sessions/:id/context` for 5H/7D. There is no
    transcript-based alternative for account-level rate limits — this data literally only
    exists via Anthropic's own OAuth usage API, which nx-agent already polls and caches. Ported
    to Go (new client, not a subprocess call into the Python `cc_tmux` package — Leo's
    "Go binary, new dir" answer) but the SEMANTICS (dedupe rules, active-row resolution,
    reset-time formatting) are a direct port of `usage.py`'s documented behavior, not
    reinvented.
  - **Sessions/SES (bottom section, context-limit meter)**: Card A's transcript-reading
    algorithm (`docs/recon/davidcreador-herdr-token-dashboard.md`'s Steal card), NOT nx-agent's
    existing SES pipeline. Evidence for this split, found live this session
    (`reference_cc_tmux_model_letter_pipeline_and_roadmap_pulse_sharing` memory,
    2026-07-19 entry): nx-agent's current SES pipeline round-trips context tokens through a
    clamped percentage (`usedPercentage = min(tokens*100/window, 100)`, then
    `raw_tokens = pct/100 * window` on read) — once real usage crosses 200k tokens, this
    reconstruction is lossy and **freezes at exactly "200.0k" regardless of true usage (could be
    900k)**, and the frozen value lands in the GREEN ("safe") severity tier, so the wrong number
    doesn't even look alarming. For a feature whose entire point is a correctness-sensitive
    context-limit **warning**, silently-wrong-and-green is worse than the extra code: transcript
    parsing sums real `input_tokens + cache_read_input_tokens + cache_creation_input_tokens`
    directly off the last main-thread assistant turn with no percentage round-trip, so it cannot
    hit this specific failure mode. Card A's `mungeClaudePath`/`claudeSessionPath` path
    resolution and `messageID+requestID` turn-dedup are ported as-is (MIT-licensed source,
    evidence-verified 6/6 in the recon). Card B's pricing table (longest-substring match,
    0.1x/1.25x cache multipliers, evidence-verified 4/4) is ported alongside it for the COST
    column, computed from the same raw token counts — nothing in the nx-agent pipeline currently
    produces a dollar estimate at all, so this is additive, not a second competing number.
  - The severity ramp + 63%-handoff-threshold warning text applied to the resulting raw token
    count is ported from `render.py`'s `_context_color_pair`/`_SES_HANDOFF_THRESHOLD` verbatim
    (six tiers: dim ≤100k, green >100k, yellow >200k, orange >300k, red >500k, red/bright-red
    pulsing >600k, dark-red/red pulsing >750k; `!handoff:/workflow:handoff` suffix at 63% of the
    resolved context window) — same thresholds, ported to Go, not redesigned.
- **Runtime host (Leo's ask)**: herdr replaces tmux for these sessions — this plugin targets
  herdr-hosted Claude Code panes (`AgentSession.Source == "herdr:claude"` per the recon's
  confirmed herdr-token-dashboard precedent). It does not modify or depend on `apps/cc-tmux`'s
  tmux-side tracking; that remains untouched by this proposal (decommissioning/coexistence is
  explicitly OUT of scope, see below).
- **Reuse-not-rebuild (Reader Gate)**: no existing Go nx-agent client or herdr client exists
  anywhere in this repo (`grep -rl "7400\|nx-agent" --include="*.go" .` and
  `grep -rl "herdr" --include="*.go" .` both empty) — genuinely new. `apps/wavetui` was checked
  as a candidate extension target (same Go+bubbletea stack, an existing `Pane`/`Source`/`Store`
  architecture) but its three tabs (Items/Memories/Context) are a beads/OpenSpec/ctx-scan project
  queue — a different domain with no session/account/token concept — and it is not herdr-plugin
  wired (no `herdr-plugin.toml`, standalone binary). Not an extension target; this proposal's
  architectural shape (poll loop, degrade-on-failure, `runJSON`-style subprocess helper) mirrors
  `apps/wavetui/internal/sources/beads.go`'s pattern for consistency, without sharing its module.
  On the herdr-plugin side, the manifest/CLI-subprocess-only integration pattern and the
  poll-primary (never trust `pane.agent_status_changed`) gotcha are both ported from
  `docs/reference/herdr-integration-patterns.md` (created by this proposal — see touches).

## Motivation
Leo now runs Claude Code sessions inside herdr instead of tmux. cc-tmux's tmux status bar (model
letter, project/branch, SES/5H/7D, the accounts popup) has no herdr equivalent — that live
picture disappears the moment a pane moves off tmux. Separately, cc-tmux's own SES pipeline has a
confirmed live bug (clamped-percentage round-trip, frozen "200.0k" past the true limit, shown in
a "safe" color) that this rebuild is also positioned to sidestep by computing tokens directly from
the transcript instead of through nx-agent's lossy reconstruction.

## Requirements

### Requirement: AccountsSource polls nx-agent and publishes deduped account usage
See `specs/herdr-token-tab/spec.md`.

### Requirement: SessionSource resolves each herdr pane's Claude transcript and computes token/cost/severity
See `specs/herdr-token-tab/spec.md`.

### Requirement: TokenTab renders Accounts (top) and Sessions (bottom) in one herdr tab
See `specs/herdr-token-tab/spec.md`.

## Scope
- **IN**: `apps/herdr-token-tab` Go module (bubbletea TUI); nx-agent HTTP client for
  `/credentials` (deduped, active-row resolved) and `/statusline`/`/sessions/:id/context`
  (5H/7D, reset epochs); Claude transcript reader (munge-cwd path resolution, glob-by-session-UUID
  fallback, `messageID+requestID` turn dedup) scoped to raw token extraction only; pricing table
  (longest-substring match, cache multipliers) for the COST column; severity ramp + handoff
  warning ported from `render.py`; herdr plugin manifest (`placement = "tab"`,
  `herdr plugin action invoke` entrypoint, poll-loop-as-primary status detection — no reliance on
  `pane.agent_status_changed`); `docs/reference/herdr-integration-patterns.md` (the recon's
  Card A content, now with a real caller).
- **OUT**: modifying or decommissioning `apps/cc-tmux`'s tmux-side tracking (coexistence/
  migration is a separate future proposal); Pi or OpenCode session support (Claude-only, per
  this fleet's actual agent roster); a fix to nx-agent's own SES clamp bug (worked around here by
  not depending on that pipeline for this specific metric, not patched at the source — nx is a
  separate repo); notifications/toasts on session completion (herdr-token-dashboard's `--notify`
  feature — not requested this round); Windows support (herdr-token-dashboard's own README marks
  this preview-only even upstream).

## Done Means
- Leo presses the configured keybinding (or `herdr plugin action invoke` the tab's open action)
  and a herdr tab opens showing an Accounts block per credential: active-account `*` marker,
  5H/7D percentages, identity line, reset countdowns — matching `cmd_accounts_popup`'s existing
  values for the same live nx-agent data.
- Below it, a Sessions table lists every herdr-tracked Claude pane with project, a raw
  context-token count (not "200.0k"-frozen past 200k — the transcript-derived number tracks real
  usage up to and past that point), 5H/7D from its active account, and an estimated cost.
- A pane whose context usage is at or above 63% of its resolved window shows the same
  `!handoff:/workflow:handoff` suffix cc-tmux's row-2 bar shows today, in the matching severity
  color.
- With nx-agent unreachable, the Accounts section shows an unavailable badge (never a crash,
  never stale data presented as live) while the Sessions section (transcript-based) keeps
  working — the two sections degrade independently since they have independent data sources by
  design.
- With the transcript file for a pane missing or unparseable, that pane's row shows an
  unavailable state instead of a wrong number or a crash.

## Testing
| Affected seam | Unit task | E2E task |
|----------------|-----------|----------|
| `internal/nxagent/client.go` (credentials dedupe/active-resolve, statusline/context fetch) | `[2.2]` | `[4.2]` |
| `internal/transcript/claude.go` (path resolution, glob fallback, turn dedup) | `[2.4]` | `[4.2]` |
| `internal/pricing/claude.go` (longest-substring match, cache multipliers) | `[2.6]` | `[4.2]` |
| `internal/severity/context.go` (6-tier ramp, 63% handoff threshold) | `[2.8]` | `[4.2]` |
| `internal/tui/model.go` (Accounts + Sessions render, degrade badges) | `[3.2]` | `[4.2]` |
| `herdr-plugin.toml` (manifest shape, poll-primary detection) | N/A (config, not code) | `[4.2]` |

## Preconditions
- `herdr --version` reports `0.7.0` or newer (this machine: confirmed `herdr 0.7.5` this
  session).
- `go version` reports Go 1.22+ (herdr-token-dashboard's own stated minimum; this repo's other Go
  app, `apps/wavetui`, already builds clean here, so the toolchain is confirmed present).
- `curl -sf http://localhost:7400/credentials -o /dev/null -w '%{http_code}'` returns `200` —
  nx-agent reachable (degrade-on-failure still applies at runtime; this just confirms the happy
  path is testable during `/apply`'s E2E batch).
- `herdr plugin list` succeeds (plugin subsystem functional — already confirmed this session via
  `herdr-file-viewer`'s live install).
- `~/.claude/projects/` exists and is non-empty (at least one real Claude Code transcript to
  test the reader against — true on this machine, this session's own transcript included).

## Impact
| Area | Change |
|------|--------|
| `apps/herdr-token-tab/` | New Go module — CLI-subprocess-only herdr integration (no `pane.report_metadata`, no raw socket — same posture as both recon'd precedents) |
| `docs/reference/herdr-integration-patterns.md` | New — the recon's Card A content (transcript algorithm, manifest shape, poll-primary gotcha), now with this plugin as its first real caller |
| `apps/cc-tmux/` | Untouched — tmux-side tracking stays as-is, explicitly out of scope |
| `openspec/specs/herdr-token-tab/spec.md` | New capability — `## ADDED Requirements` only |

## Risks
| Risk | Mitigation |
|------|-----------|
| nx-agent down/unreachable | Accounts section degrades to an unavailable badge; Sessions section (transcript-based, independent) keeps working — verified independently degradable per Done Means |
| Transcript file missing/unparseable for a pane | That row shows unavailable, never a crash or a wrong number — same malformed-line-skip posture the recon's evidence-verified tests already cover upstream |
| `herdr pane list`'s `AgentSession.Source`/`.Value` shape drifts from what the recon documented | Decode defensively (unknown/missing fields degrade that pane to unavailable, not a panic); `herdr --version` precondition catches a major version bump before it surprises the plugin |
| Severity/handoff thresholds drift from `render.py`'s if cc-tmux's own values are tuned later | Both live in the SAME repo — a future cc-tmux threshold change should update `internal/severity/context.go` too; flagged in that file's own header comment as a cross-reference, not a silent fork |
| Cost estimate is wrong for a model not in the pricing table | Matches Card B's evidence-verified behavior: unmatched model shows $0 cost but keeps real token counts — never drops data, never fabricates a number |
