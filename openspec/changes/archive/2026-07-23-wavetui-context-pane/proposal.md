---
order: 0722b
---

# Proposal: wavetui-context-pane — real-time ctx-scan context breakdown as a wavetui tab

## Change ID
`wavetui-context-pane`

## Summary
Port ctx-scan's context-breakdown display into wavetui as a new full-screen `[3] Context` tab,
fed by a new `CtxScanSource` that shells out to a new band-annotated `ctx-scan view-model --json`
subcommand on a poll interval plus a manual refresh key. Today the two tools live in two
terminals: ctx-scan renders a static HTML report of what a project's config layer costs in
context tokens, while wavetui shows live session state with no visibility into the static
context baseline. This change puts ctx-scan's drill-down (project → class → document) live inside
the single TUI.

## Context
- depends on:
- touches: `apps/ctx-scan/src/cli.ts`, `apps/ctx-scan/test/view-model-json.test.ts`,
  `apps/wavetui/internal/store/store.go`, `apps/wavetui/internal/sources/ctxscan.go`,
  `apps/wavetui/internal/sources/ctxscan_test.go`, `apps/wavetui/internal/ui/ctxscanpane.go`,
  `apps/wavetui/internal/ui/ctxscanpane_test.go`, `apps/wavetui/internal/ui/root.go`,
  `apps/wavetui/internal/config/config.go`, `apps/wavetui/cmd/wavetui/main.go`
- **Overlap (wave-level, not logical)**: in-flight `wavetui-session-cwd` (order 0722a) also
  touches `apps/wavetui/internal/store/store.go`. Both changes are additive in disjoint areas
  (`SessionLink.CWD` there; a new `CtxScan` snapshot field + event case here) — no hard
  conflict, declared so the wave conflict matrix serializes them.
- **Origin**: `/explore` session 2026-07-22 auditing both apps. Key findings driving the design:
  ctx-scan is Bun/TS (~5.9k LOC), wavetui is Go (bubbletea v2) — so integration is shell-out,
  not a rewrite; `ctx-scan scan --json` emits a schema-versioned `Fleet` but with `bands: []`
  (rubric bands are annotated only inside `audit`/`render`), so the display-ready
  `RenderViewModel` (built by the pure `buildViewModel(fleet)` in `src/render/view-model.ts`) is
  not reachable from any existing CLI flag — hence the new upstream subcommand.
- **Reuse-not-rebuild (Reader Gate)**: the entire scan pipeline (discovery, settings precedence,
  @import chain, truncation caps, Table A rubric — ~2.9k LOC, tested) stays in ctx-scan;
  `buildViewModel` and `annotateFleetBands` already exist and are pure. The new subcommand is
  wiring, not logic. On the Go side, `runJSON` (beads.go:66), `requeryLoop`/`debounce`/
  `backoffDelay` (loop.go), the `Pane` interface, and the `r.memory` tab-wiring pattern
  (root.go) are all reused verbatim.
- **Scan root decision (Leo, 2026-07-22)**: current project only — the source passes wavetui's
  own repo root as `--root`, so ctx-scan's fleet level collapses to a single project and no
  fleet-wide watch surface exists. The fleet leaderboard stays in ctx-scan's HTML report.
- Capability Preflight: not applicable — local dev tool, no hosting/deploy component, same
  precedent `wavetui-core`/`wavetui-sessions`/`wavetui-session-cwd` cite (`stack: t3`-as-
  placeholder per `rules/PATTERNS.md`).

## Motivation
An operator tuning a project's context budget today runs `ctx-scan render`, opens an HTML file,
edits config, and re-runs — while wavetui sits in the next pane showing only the *live* side
(session `ContextPct` from `TranscriptSource`). The static baseline (what CLAUDE.md, skills,
MCP tools, hooks, and memory will cost every future session) is invisible in the TUI, and the
HTML report is stale the moment a file changes. One tab with a polling scan closes the loop:
edit config, watch the class bar and rubric bands move, no browser round-trip.

## Requirements

### Requirement: ctx-scan emits a band-annotated view model as JSON
See `specs/ctx-scan/spec.md`.

### Requirement: CtxScanSource polls the current project and publishes a context report
See `specs/wavetui/spec.md`.

### Requirement: ContextPane renders the context breakdown as a drill-down tab
See `specs/wavetui/spec.md`.

## Scope
- **IN**: `ctx-scan view-model` subcommand (band-annotated `RenderViewModel` JSON with a
  `schemaVersion` field); Go mirror structs for the payload; `CtxScanEvent` + `Snapshot.CtxScan`
  + `Apply` case; `CtxScanSource` (poll ticker, default 60s, config knob
  `ctx_scan_poll_seconds`, coalesced manual-refresh trigger, degrade-to-badge on any failure);
  `ContextPane` full-screen tab (`[3] Context`, keys `j`/`k`/`enter`/`esc`/`r`) with drill-down
  project → class → document; root.go tab wiring mirroring `r.memory`; main.go construction.
- **OUT**: fleet-wide scanning or the level-0 fleet leaderboard (root is the current project by
  explicit decision); fsnotify-driven re-scans (poll + manual refresh chosen — fleet-scale watch
  surface rejected, and a follow-on can add a single-root watcher if 60s proves too slow);
  ctx-scan's telemetry probe, calibrate, trim-plan, references shelf, history/diff/sparklines
  (HTML-report features, not ported); any rewrite of scan logic in Go; `--probe-hooks`
  (never passed — no hook execution from inside a TUI).

## Done Means
- Operator presses `3` in wavetui and sees the current project's context breakdown: one row per
  context class with estimated tokens and GREEN/AMBER/RED band coloring.
- Operator selects a class and presses `enter` to see its documents (tokens, band, truncation
  flags), and `enter` again on a document to see its detail (tier, origin, raw vs effective
  chars, violations); `esc` walks back up.
- Operator edits a config file (e.g. CLAUDE.md), presses `r`, and sees updated numbers without
  leaving the TUI.
- With the `ctx-scan` binary missing or failing, the tab shows an unavailable badge with the
  error — never a crash, never a stale-data masquerade.

## Testing
| Affected seam | Unit task | E2E task |
|----------------|-----------|----------|
| `apps/ctx-scan/src/cli.ts` `view-model` subcommand JSON shape | `[2.1]` (bun fixture test) | `[4.2]` |
| `apps/wavetui/internal/store/store.go` event + snapshot field | `[4.1]` | `[4.2]` |
| `apps/wavetui/internal/sources/ctxscan.go` (stub-CLI parse, degrade, coalesce) | `[2.3]` | `[4.2]` |
| `apps/wavetui/internal/ui/ctxscanpane.go` drill state machine | `[3.1]` | `[4.2]` |
| `apps/wavetui/internal/ui/root.go` tab wiring | `[3.2]` (root_test) | `[4.2]` |

## Impact
| Area | Change |
|------|--------|
| `apps/ctx-scan/src/cli.ts` | New `view-model` command: `buildFleet` → `annotateFleetBands` → `buildViewModel`, JSON to stdout/file — no change to existing commands |
| `apps/wavetui/internal/store/store.go` | Additive: `CtxScanReport` types, `CtxScanEvent`, `Snapshot.CtxScan` field, one `Apply` case |
| `apps/wavetui/internal/sources/ctxscan.go` | New file — canonical shellout source per beads.go pattern, injectable CLI interface for tests |
| `apps/wavetui/internal/ui/ctxscanpane.go` | New file — `Pane` + `Sizeable` + `HandleKey` |
| `apps/wavetui/internal/ui/root.go` | `tabScan` + `[3] Context` label + key `3` + handleKey case + `switchTab`/`paneVisible`/`renderTabContent`/`layout` branches + `EnableContextPane` helper — mirrors every existing `r.memory` reference |
| `apps/wavetui/internal/config/config.go` | One int knob: `ctx_scan_poll_seconds` (default 60) |
| `apps/wavetui/cmd/wavetui/main.go` | Construct source + pane, `go src.Run(ctx)`, wire refresh trigger |
| `openspec/specs/ctx-scan/spec.md`, `openspec/specs/wavetui/spec.md` | `## ADDED Requirements` deltas only |

## Risks
| Risk | Mitigation |
|------|-----------|
| `ctx-scan` binary not on PATH (it's a bun `bin` entry in this monorepo) | Source degrades to an unavailable badge with the exec error (same posture as the missing-`.beads/` requirement); wiring the bin (`bun link` or PATH) is a documented setup step, not a hidden dependency |
| A scan of a large project could take seconds; poll must not stack invocations | Reuse `requeryLoop`'s at-most-one-in-flight coalescing — ticker and `r` both feed the same trigger channel |
| `RenderViewModel` JSON shape drifts between TS and Go structs | `schemaVersion` field checked by the Go decoder (mismatch → unavailable badge, not garbage render); bun fixture test locks the TS shape per ctx-scan's existing "Data model is schema-versioned" requirement |
| Manual refresh key must not violate the "Update() never calls a source or CLI directly" spec requirement | `r` only pushes into the source's non-blocking trigger channel (a func injected at construction); the shellout stays in the source goroutine |
| `store.go` overlap with in-flight `wavetui-session-cwd` | Declared in `- touches:`; additive edits in disjoint regions; wave matrix serializes |
