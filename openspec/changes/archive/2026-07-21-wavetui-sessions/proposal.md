---
order: 0720c
---

# Proposal: wavetui-sessions — TranscriptSource + TmuxSource, SessionsPane + KPIBar

## Change ID
`wavetui-sessions`

## Summary
Add `TranscriptSource` (tails Claude Code transcript JSONL files) and `TmuxSource` (reads
cc-tmux's existing `@cc-state` pane options) to `apps/wavetui/`, plus two new panes —
`SessionsPane` and `KPIBar` — that attach to `wavetui-core`'s existing focus ring. This proposal
gives the operator a live view of which queue items have an active Claude Code session behind
them, how close that session is to running out of context, and whether a claim looks abandoned.

## Context
- depends on: `wavetui-core`
- **Depends on `wavetui-core`** (spec dir `openspec/changes/wavetui-core/`, capability epic
  `if-tkva`, feature bead `if-3g1c`): this proposal's sources publish onto `wavetui-core`'s event
  bus and mutate state only through its single-writer `Store`; its panes implement `wavetui-core`'s
  `Pane` interface and attach to the existing focus ring. Soft dependency only — both proposals
  are independently authored/reviewable, but `wavetui-core` should land first in any apply wave
  since this proposal's `Item`/`Snapshot` extensions build on its data model.
- **This is proposal 2 of 7 in the wavetui dependency spine**: `wavetui-core` ->
  {`wavetui-sessions`, `wavetui-dispatch`, `wavetui-memory-timeline`, `wavetui-flair`} ->
  {`wavetui-decision-lanes`, `wavetui-daemon`}. Resolves to the SAME capability epic (`if-tkva`,
  `[CAPABILITY] wavetui`) — verified at Phase 4 Gate 4.1 below, not re-created.
- **Reuse-not-rebuild (Reader Gate, non-negotiable)**: `openspec/specs/cc-tmux/spec.md` (this
  repo, 28 requirements) is a live, actively-maintained tmux plugin that already tracks each
  Claude Code pane's state (`waiting`/`idle`/`active`) via Claude Code hooks writing a `@cc-state`
  tmux pane option, auto-deleted when the pane closes — see cc-tmux's "Claude pane state is
  tracked in tmux pane options" requirement. It also already surfaces a beads/openspec summary
  row ("A dedicated tmux status row surfaces open/ready beads and proposals") sourced from
  nx-agent's roadmap-pulse endpoint (counts-only, no per-item detail — not reused here for the
  same reason `wavetui-core` didn't reuse it: `wavetui`'s sources need full item detail, cc-tmux's
  endpoint is counts-only). `TmuxSource` in this proposal reads `@cc-state` directly for every
  pane cc-tmux has tagged (via `tmux show-options -p -v -t <pane> @cc-state`, the same primitive
  `cc_tmux.tmux.get_pane_option()` wraps — no dedicated per-pane JSON query subcommand exists in
  `cc_tmux`'s CLI as of this writing, confirmed by inspecting `apps/cc-tmux/src/cc_tmux/cli.py`'s
  subcommand list) rather than re-deriving pane state via a process-tree walk. Process-tree walk
  is kept ONLY as a fallback for panes cc-tmux hasn't tagged. See `design.md` § Alternatives /
  Related Work for the full citation and rationale.
- **Transcript format is internal and undocumented** — verified against a real transcript file in
  this repo's own `~/.claude/projects/-home-nyaptor-dev-personal-installfest/*.jsonl` during
  authoring (adversarial verification, not assumed): top-level line `type` values observed include
  `user`, `assistant`, `system`, `attachment`, `last-prompt`, `custom-title`, `agent-name`, `mode`,
  `permission-mode`, `file-history-snapshot`, `file-history-delta` — far more variety than a naive
  "user/assistant only" assumption. `assistant` lines carry `message.usage` with real keys
  `input_tokens`, `cache_creation_input_tokens`, `cache_read_input_tokens`, `output_tokens`, plus
  nested `cache_creation`/`iterations`/`service_tier`/`server_tool_use` objects — the specific
  fields `TranscriptSource` needs (input + cache-read tokens for the context gauge) are confirmed
  present under those exact names. `user` lines carry `cwd`, `sessionId`, `isSidechain`,
  `parentUuid`, `gitBranch`, `timestamp` — confirming the cwd-trust-over-flattening and
  subagent-sidechain-links-via-parent-id assumptions in the Requirements below. Full dump command
  and output are in `design.md` § Verified transcript fields.
- Capability Preflight (Phase 1): not applicable, matching `wavetui-core`'s precedent — local dev
  tool, no hosting/deploy component. Both greenfield probes returned empty as expected for a
  dotfiles repo; skipped per explicit operator authorization (same call `wavetui-core` made).
- touches: `apps/wavetui/internal/sources/transcript.go`,
  `apps/wavetui/internal/sources/transcript_test.go`, `apps/wavetui/internal/sources/tmux.go`,
  `apps/wavetui/internal/sources/tmux_test.go`, `apps/wavetui/internal/sources/session_link.go`,
  `apps/wavetui/internal/sources/session_link_test.go`, `apps/wavetui/internal/ui/sessionspane.go`,
  `apps/wavetui/internal/ui/kpibar.go`, `apps/wavetui/internal/store/store.go` (additive fields
  only — see Risks for the coordination note with `wavetui-core`)

## Motivation
`wavetui-core` ships a live queue of beads + openspec items, but gives no visibility into whether
a claimed item has a real Claude Code session working it, how close that session is to running
out of context (a silent productivity cliff — a session past ~70% context degrades badly before
it errors out), or whether a claim is actually abandoned (a "zombie" — claimed but the linked
session has gone quiet). Operators currently have no way to see this short of manually checking
tmux panes and eyeballing terminal output. `wavetui-sessions` closes that gap by tailing the same
transcript files Claude Code itself writes and cross-referencing cc-tmux's existing pane-state
tracking, surfacing the result as two new panes without requiring any change to how sessions or
tmux panes are used.

## Requirements

### Requirement: TranscriptSource tails Claude Code transcript files with tolerant, offset-based decoding
See `specs/wavetui/spec.md`.

### Requirement: A claimed item is linked to its session via an /apply reference or cwd+timestamp proximity
See `specs/wavetui/spec.md`.

### Requirement: Context gauge derives a percent-of-window estimate and badges at a 70% threshold
See `specs/wavetui/spec.md`.

### Requirement: Zombie detection flags a stale claim with a one-key, never-automatic release action
See `specs/wavetui/spec.md`.

### Requirement: Error feed attributes tool-result error classes to their item and agent
See `specs/wavetui/spec.md`.

### Requirement: Token meter tracks output tokens by model per session, item, and wave, and flags opus in an executor lane
See `specs/wavetui/spec.md`.

### Requirement: Rate-limit signals in the transcript stream surface a backpressure banner
See `specs/wavetui/spec.md`.

### Requirement: TmuxSource reads cc-tmux's @cc-state pane option as its primary source of pane state
See `specs/wavetui/spec.md`.

### Requirement: SessionsPane renders the pane map, context gauges, and zombie badges as a focus-ring pane
See `specs/wavetui/spec.md`.

### Requirement: KPIBar renders continue-count, rate-limit incidents, and stale-claim minutes as a focus-ring pane
See `specs/wavetui/spec.md`.

### Requirement: A malformed or truncated transcript line degrades the sessions pane, never the whole app
See `specs/wavetui/spec.md`.

## Scope
- **IN**: `TranscriptSource` (tail + offset tracking + tolerant decode + session linkage + context
  gauge + zombie detection + error feed + token meter + rate-limit signal), `TmuxSource`
  (`@cc-state`-primary, process-tree fallback), `SessionsPane`, `KPIBar`, the additive `Item`
  fields these sources need in `wavetui-core`'s `Store` data model.
- **OUT**: dispatch / wave-file format (`wavetui-dispatch`), decision-lanes UI
  (`wavetui-decision-lanes`), daemon/background mode (`wavetui-daemon`), visual flair/theming
  beyond baseline lipgloss layout (`wavetui-flair`), memory-timeline pane
  (`wavetui-memory-timeline`), building the headless-dispatch queue that would consume the
  rate-limit backpressure event this proposal emits (that queue is a sibling proposal's concern —
  this proposal only emits the event and renders the banner), automatic release of a zombie-badged
  claim (explicitly never built — one-key operator action only, per the Requirements above).

## Done Means
- Operator can see, for each queue item linked to a live Claude Code session, a live
  context-percent gauge that updates as the session's transcript grows.
- Operator can see a one-key release action on a zombie-badged item, and pressing it releases the
  bd claim without auto-releasing anything on its own.
- When cc-tmux is installed and tracking a pane, wavetui's `SessionsPane` reflects that pane's
  state without wavetui re-deriving it via process-tree walking.
- A malformed or truncated transcript line degrades the sessions pane to an "unavailable" badge,
  never crashes the app.

## Testing
| Affected seam | Unit task | E2E task |
|----------------|-----------|----------|
| `internal/sources/transcript.go` (tail, offset, decode, linkage, gauge, zombie, token meter, rate-limit) | `[4.1]` | `[4.5]` |
| `internal/sources/tmux.go` (`@cc-state` primary + process-tree fallback) | `[4.2]` | `[4.5]` |
| `internal/sources/session_link.go` (item-to-session linkage helper shared by both sources) | `[4.3]` | `[4.5]` |
| `internal/ui/sessionspane.go`, `internal/ui/kpibar.go` | N/A — no pure-function render logic beyond Go compile | `[4.5]` (pty runtime verification) |
| `internal/store/store.go` additive fields | `[4.4]` | `[4.5]` |

## Impact
| Area | Change |
|------|--------|
| `apps/wavetui/internal/sources/` | Two new sources (`transcript.go`, `tmux.go`) + shared linkage helper |
| `apps/wavetui/internal/ui/` | Two new panes (`sessionspane.go`, `kpibar.go`) |
| `apps/wavetui/internal/store/store.go` | Additive `Item`/`Snapshot` fields only — see Risks |
| `openspec/specs/wavetui/` | New capability spec created (`## ADDED Requirements`, same as `wavetui-core` — no parent spec exists yet) |
| Existing repo files outside `apps/wavetui/` | None modified |

## Risks
| Risk | Mitigation |
|------|-----------|
| This proposal's `- touches:` list includes `apps/wavetui/internal/store/store.go`, which `wavetui-core` also touches | This is an ADDITIVE-only edit (new struct fields for session linkage, context %, zombie state, token meter — no removal or signature change to `wavetui-core`'s existing `Item`/`Snapshot` types). `wave-plan-build`'s conflict matrix will still serialize the two specs into different waves since both declare this path; `wavetui-core` MUST land first per the Context dependency above, and this proposal's `[3.x]` UI tasks depend on that landing before compiling. |
| Transcript `usage` field names were a documented UNVERIFIED risk going into authoring | Resolved via direct inspection of a real transcript file during this session (see Context above and `design.md` § Verified transcript fields) — the fields this proposal's context gauge needs are confirmed present under the assumed names. Any FUTURE Claude Code release could still rename them; `TranscriptSource`'s tolerant-decode requirement (unknown fields ignored, missing fields -> degraded badge, never a panic) is the standing mitigation for that drift, not a one-time check. |
| No Go-aware `/apply` engineer agent exists in the fleet yet | Same `stack: t3` workaround `wavetui-core` used, for the same reason (`commands/apply/references/stacks.md`'s crosswalk has no `go-cli` value yet) — cited here rather than re-derived; tracked in `wavetui-core`'s own Risks table, not duplicated as a new tracked risk. |
| `cc-tmux`'s own capability epic (`if-bqw`) is separately in-progress | `TmuxSource` only READS `@cc-state` via `tmux show-options` — it does not modify any cc-tmux file, hook, or state, so no `- touches:` overlap and no coordination needed beyond the read-only contract already documented in cc-tmux's own spec. |
| No explicit session-end event exists in the transcript format; inactivity alone can lie (a slow tool call with no output for minutes looks like inactivity) and tmux process absence alone can lie (pane closed but process reparented, or process alive but pane killed) | Cross-check both signals per the Requirements below — zombie detection requires BOTH an inactivity threshold AND (when TmuxSource has data for that pane) a tmux-side confirmation, never either signal alone. |
