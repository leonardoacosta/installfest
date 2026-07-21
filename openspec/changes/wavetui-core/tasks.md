---
stack: t3
---
<!-- beads:epic:if-tkva -->
<!-- beads:feature:if-3g1c -->

<!-- stack: one of t3 | cc-meta | effect | dotnet — see commands/apply/references/stacks.md § "Stack vocabulary crosswalk" for the full tasks.md-stack:/--stack-profile/detect_stack() mapping -->

# Implementation Tasks

## DB Batch

- [x] [1.1] Scaffold `apps/wavetui/` Go module: `go.mod` (module path, Go version pin), `cmd/wavetui/main.go` entrypoint stub, `internal/{bus,store,config,blocker,sources,ui}/` package layout, `.gitignore` for build artifacts (binary output, no committed build cache) [beads:if-mcf5]
- [x] [1.2] Implement `internal/bus/bus.go`: typed `Event` interface, `Publish`/`Subscribe`, one goroutine per subscriber threaded from a `context.Context`, no shared mutable state between publish and delivery [beads:if-lod2]
- [x] [1.3] Implement `internal/store/store.go`: single-writer `Store` with `Item`/`Snapshot`/`SourceError` types (per `design.md` § Store data model), `Apply(Event)` mutates internal state only from the bus-delivery goroutine, `Snapshot()` returns an immutable copy-on-write value, dep-graph + fan-out-score derivation [beads:if-3xmu]
  - depends on: 1.2
- [x] [1.4] Implement `internal/config/config.go`: per-project TOML config loader scoped by cwd, `plans`/`advisor-plans` visibility flag (default off), a `tmp`+atomic-`os.rename` write helper for any future writer to reuse [beads:if-7gg1]
- [x] [1.5] Formalize the blocker-note grammar in `design.md` (already drafted) and implement `internal/blocker/blocker.go`: parse `blocked: <type> - <reason> (see <ref>)` per the regex in `design.md` § Blocker-note grammar, tolerant of malformed/missing lines (no error, no badge) [beads:if-sh3y]

## API Batch

- [ ] [2.1] Implement `internal/sources/beads.go`: fsnotify watch on `.beads/{*.db,*.db-wal,*.db-shm}` plus parent-dir watch for creation, 300-500ms trailing debounce, serialized in-flight `bd list --json` + `bd ready --json` shellouts with pending-request coalescing, 15s poll fallback, tolerant JSON decode (unknown fields ignored, missing optional fields -> zero value + degraded badge), non-zero exit or malformed JSON keeps the last-good snapshot and badges stale with retry backoff [beads:if-1brf]
  - depends on: 1.2, 1.3, 1.4
- [ ] [2.2] Implement `internal/sources/openspec.go`: fsnotify watch on `openspec/changes/` (non-recursive walk-then-watch, dir-create re-arm) plus `plans/`/`advisor-plans/` behind the `[1.4]` config flag, parse each proposal's `proposal.md` header + `tasks.md` checkbox counts + a blocker-note line via `internal/blocker` [beads:if-6fgg]
  - depends on: 1.2, 1.3, 1.4, 1.5
- [ ] [2.3] Wire missing-directory degradation for both sources: emit an "unavailable" event (not an error) when `.beads/` or `openspec/changes/` is absent at startup, and watch the parent directory so a later creation transitions the badge live without a restart [beads:if-0xr7]
  - depends on: 2.1, 2.2
- [ ] [2.4] Thread `context.Context` from `main` into both sources for graceful shutdown on SIGINT/SIGTERM; audit that no goroutine in either source runs without a cancellable context [beads:if-w4jw]
  - depends on: 2.1, 2.2

## UI Batch

- [ ] [3.1] Implement `internal/ui/root.go`: bubbletea root model wired to Store snapshots via `Program.Send()`, a `Pane` interface (`Update(Snapshot) Pane`, `View() string`, `Focusable() bool`) plus an ordered pane slice + focus index (the focus ring), render-coalescing to roughly a 10fps cap [beads:if-aleg]
  - depends on: 1.3
- [ ] [3.2] Implement `internal/ui/queuepane.go`: bubbles table with columns item/type/created-at/blocker-badge/fan-out-score, implementing the `Pane` interface from `[3.1]` [beads:if-7c4o]
  - depends on: 3.1
- [ ] [3.3] Implement `internal/ui/detailpane.go`: renders notes, blocker reason, and task progress for `QueuePane`'s currently selected row; lipgloss layout splitting the two panes [beads:if-se54]
  - depends on: 3.1, 3.2
- [ ] [3.4] Wire `cmd/wavetui/main.go` end-to-end: instantiate bus, store, config, both sources, and the root model; run the `tea.Program`; capture runtime evidence rendering against this repo's own `.beads/` and `openspec/changes/` state (paste rendered pty output) [beads:if-xj6u]
  - depends on: 2.3, 2.4, 3.2, 3.3

## E2E Batch

- [ ] [4.1] `go test` for `internal/bus`: publish/subscribe fan-out to multiple subscribers, context-cancellation stops delivery cleanly [beads:if-t0ma]
  - depends on: 1.2
- [ ] [4.2] `go test` for `internal/store`: event application correctness, snapshot immutability/copy-on-write, dep-graph + fan-out-score derivation, and the never-infer-from-filename invariant (a re-query call is made regardless of which specific path changed) [beads:if-0tab]
  - depends on: 1.3
- [ ] [4.3] `go test` for `internal/sources/beads.go` and `internal/sources/openspec.go`: fixture directories, tolerant decoding of malformed/missing JSON fields, debounce coalescing under a simulated event burst (assert exactly one CLI invocation), missing-directory startup degradation, non-zero-exit CLI keeps last-good snapshot [beads:if-asml]
  - depends on: 2.1, 2.2, 2.3
- [ ] [4.4] `go test` for `internal/blocker`: valid grammar variants (with and without the optional `(see <ref>)` suffix, unknown `<type>` values), malformed/missing blocker lines produce no `Blocker` and no error [beads:if-v3fb]
  - depends on: 1.5
- [ ] [4.5] Runtime-verify end-to-end: run `apps/wavetui/cmd/wavetui` against this repo's own `openspec/changes/` and `.beads/`, confirm the queue updates within 2 seconds of a real `bd`/openspec file change (inside the debounce window), select a row and confirm `DetailPane` populates, kill and restart the process and confirm current on-disk state is reflected within one debounce window, temporarily rename `.beads/` aside and confirm an "unavailable" badge renders (not a crash) — paste the terminal/pty output as evidence [beads:if-nai4]
  - depends on: 3.4, 4.1, 4.2, 4.3, 4.4
