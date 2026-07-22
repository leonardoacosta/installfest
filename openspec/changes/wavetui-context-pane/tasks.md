---
stack: t3
---

<!-- stack: t3 is the documented placeholder for installfest specs (rules/PATTERNS.md), same as
     every archived wavetui-* spec — this is a Go+Bun repo with no deploy component. -->

# Implementation Tasks

## DB Batch

- [x] [1.1] Add additive `CtxScanReport` Go types (project → class → document mirror of
  ctx-scan's `RenderViewModel`, incl. `SchemaVersion`, per-class/document `EstTokens`, band,
  tier, origin, truncation and violation fields), `CtxScanEvent` (with `EventName()`),
  `Snapshot.CtxScan *CtxScanReport` + `Snapshot.CtxScanErr string`, and one `Store.Apply` case
  in `apps/wavetui/internal/store/store.go`, per `specs/wavetui/spec.md` ADDED "CtxScanSource
  polls..." Requirement — no existing field renamed, removed, or re-typed.

## API Batch

- [x] [2.1] Add `view-model` subcommand to `apps/ctx-scan/src/cli.ts`: `--root <path>`
  `[--json <path>]`, runs `buildFleet` → `annotateFleetBands` → `buildViewModel`, emits the
  band-annotated `RenderViewModel` wrapped in `{ schemaVersion: 1, viewModel }` to stdout or
  file, per `specs/ctx-scan/spec.md` ADDED Requirement. Add
  `apps/ctx-scan/test/view-model-json.test.ts` locking the emitted shape against a fixture tree
  (same pattern as the existing schema snapshot test).
- [x] [2.2] Create `apps/wavetui/internal/sources/ctxscan.go`: `CtxScanSource` with
  `Run(ctx) error` — poll ticker (default 60s from `cfg`), a non-blocking `TriggerRefresh()`
  feeding the same coalesced `requeryLoop` trigger (at most one shellout in flight), shellout
  via the `runJSON` pattern to `ctx-scan view-model --root <repo-root>`, decode with
  `schemaVersion` check, publish `CtxScanEvent`; any exec/decode/version failure publishes the
  error string (degrade-to-badge), keeps last-good report, and backs off per `backoffDelay`.
  CLI behind an injectable interface per `beadsCLI` precedent.
  - depends on: 1.1, 2.1
- [x] [2.3] Add `apps/wavetui/internal/sources/ctxscan_test.go`: stub-CLI tests for decode of a
  fixture payload, schemaVersion mismatch → error event with last-good retained, exec failure →
  error event, and trigger coalescing (N rapid triggers → one invocation).
  - depends on: 2.2

## UI Batch

- [x] [3.1] Create `apps/wavetui/internal/ui/ctxscanpane.go`: `ContextPane` implementing `Pane`
  + `Sizeable` + `HandleKey`; three drill levels (project classes → class documents → document
  detail) with `j`/`k` cursor, `enter` descend, `esc` ascend, `r` invoking the injected refresh
  func; class rows show est tokens + GREEN/AMBER/RED band styling, document detail shows tier,
  origin, raw/effective chars, violations; renders the unavailable badge from
  `Snapshot.CtxScanErr` when set. Add `apps/wavetui/internal/ui/ctxscanpane_test.go` covering
  the drill state machine and badge rendering.
  - depends on: 1.1
- [x] [3.2] Wire the tab in `apps/wavetui/internal/ui/root.go`: `tabScan` const, `[3] Context`
  in `tabLabels`, key `3` in `handleKey`, `*ContextPane` case in the focused-pane key-dispatch
  switch, and branches in `switchTab`/`paneVisible`/`renderTabContent`/`layout` mirroring every
  existing `r.memory` reference; add `EnableContextPane(...)` helper per
  `EnableMemoryTimeline` precedent. Extend `root_test.go` for tab switching and key routing.
  - depends on: 3.1
- [x] [3.3] Add `ctx_scan_poll_seconds` int knob (default 60) to
  `apps/wavetui/internal/config/config.go` (+ its test), and wire construction in
  `apps/wavetui/cmd/wavetui/main.go`: build `CtxScanSource` with repo root + cfg, construct
  `ContextPane` with `src.TriggerRefresh`, `root.EnableContextPane(...)`,
  `go src.Run(ctx)` alongside the existing sources.
  - depends on: 2.2, 3.2

## E2E Batch

- [x] [4.1] `go test ./...` in `apps/wavetui`: full suite passes including the new store,
  source, and pane tests; confirm existing store/root tests pass unmodified (additive-only
  check).
  - depends on: 2.3, 3.3
- [x] [4.2] Runtime-verify: `bun test` in `apps/ctx-scan` passes; run `ctx-scan view-model
  --root .` against installfest and confirm valid JSON; run `apps/wavetui/cmd/wavetui` in this
  repo, press `3`, confirm the class breakdown renders with band colors, drill to a document
  detail and back, edit a config file and press `r`, confirm numbers update; rename the
  `ctx-scan` binary temporarily and confirm the unavailable badge renders instead of a crash —
  paste terminal/pty output as evidence.
  - depends on: 4.1
