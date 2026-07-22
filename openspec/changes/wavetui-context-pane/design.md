# Design: wavetui-context-pane

## Decision 1: Shell out to ctx-scan; port only the arithmetic
ctx-scan is Bun/TS (~5.9k LOC), wavetui is Go. The scan pipeline (discovery, 5-layer settings
precedence, @import walker, truncation caps, Table A rubric) stays in ctx-scan behind a JSON
contract; Go holds only mirror structs and rendering. A Go rewrite was rejected: high surface,
guaranteed drift with the TS rubric constants. The existing `scan --json` was rejected as the
contract because it emits `bands: []` (bands are annotated only inside `audit`/`render`) —
merging `scan` + `audit` output in Go would duplicate rubric wiring. Hence the new
`view-model` subcommand: ~wiring-only upstream (`buildFleet` → `annotateFleetBands` →
`buildViewModel` all exist and are pure), one display-ready payload downstream.

## Decision 2: Poll + coalesced manual refresh, no fs watching (Leo, 2026-07-22)
Root is the current project only, but even single-root fsnotify was declined in favor of the
laziest thing that works: a ticker (default 60s, `ctx_scan_poll_seconds`) and an `r` key, both
feeding one coalesced trigger channel (`requeryLoop` pattern — at most one shellout in flight).
ctx-scan's own `watch.ts`/chokidar/history.jsonl is not reused: it adds a long-lived child
process for no benefit at this cadence. If 60s + `r` proves too slow, a follow-on adds a
single-root fsnotify feeding the same trigger channel — the seam is already there.

## Decision 3: Refresh key crosses the UI→source boundary as an injected func
The wavetui spec requires that the update path never calls a source or CLI. `ContextPane` is
constructed with `refresh func()` — main.go passes `src.TriggerRefresh`, which does a
non-blocking send into the source's trigger channel. The shellout stays in the source
goroutine; the pane only signals.

## Decision 4: Full-screen tab, not a bottom strip
ctx-scan's content is a 3-level drill-down — it needs the main content area. `[3] Context`
mirrors the `[2] Memories` precedent exactly (tab const, label, key, dispatch case,
`switchTab`/`paneVisible`/`renderTabContent`/`layout` branches, `EnableContextPane` helper),
keeping root.go changes mechanical.

## Decision 5: Degrade posture
Same as every other source: exec/decode/schemaVersion failure → error string on the snapshot,
last-good report retained and rendered as stale, exponential backoff. The `ctx-scan` binary
being a monorepo bun `bin` entry (not globally installed) makes the missing-binary path the
expected first-run experience, so the badge carries the exec error verbatim.
