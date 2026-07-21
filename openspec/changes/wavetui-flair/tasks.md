---
stack: t3
---
<!-- beads:epic:if-tkva -->
<!-- beads:feature:if-u7ul -->

<!-- stack: one of t3 | cc-meta | effect | dotnet — see commands/apply/references/stacks.md § "Stack vocabulary crosswalk" for the full tasks.md-stack:/--stack-profile/detect_stack() mapping -->

# Implementation Tasks

## DB Batch

- [x] [1.1] Extend `wavetui-core`'s `internal/config/config.go` additively: add [beads:if-aecq]
  `Flair.Enabled bool` and `Flair.CalmMode bool` fields per `design.md` § Config + calm-mode +
  truecolor gating — no existing field renamed, removed, or re-typed
  - depends on: none (config.go already exists from `wavetui-core`)
- [x] [1.2] Add `apps/wavetui/go.mod` requires (corrected at implementation time — bubbletea and [beads:if-xpge]
  lipgloss are ALREADY v2 throughout the codebase per `apps/wavetui/go.mod`, and
  `github.com/lucasb-eyer/go-colorful v1.4.0` was already present as an indirect transitive dep;
  only `github.com/charmbracelet/harmonica v0.2.0` was genuinely new — added via `go get`, real
  checksums in `go.sum`. Import path verified live against proxy.golang.org: harmonica has NOT
  moved to a `charm.land` path)
  - depends on: none
- [x] [1.3] Implement `internal/flair/manager.go`'s `FlairManager` struct and `NeedsTick()` [beads:if-rch8]
  method per `design.md` § Tick-loop lifecycle: `active map[string]animState`, no unconditional
  `tea.Tick` scheduling anywhere in this file
  - depends on: 1.1

## API Batch

- [x] [2.1] Implement `internal/flair/manager.go`'s `Diff(prev, next core.Snapshot) []FlairEvent` [beads:if-x55p]
  pure function per `design.md` § Snapshot diffing: ID-set comparison for appeared/closed,
  per-item field-transition comparison for `Item.Blocker` (blocker-resolved) and `Item.Stale`
  (negative), zero side effects, zero mutation of either input
  - depends on: 1.3
- [x] [2.2] Implement `internal/flair/effects.go`'s event->effect map per `design.md` § Event -> [beads:if-nejf]
  effect map: `harmonica`-driven spring/decay for row flash, particle burst, slide-in, and shake;
  `go-colorful`-driven lerp for fade and pulse; the shake-plus-red-pulse effect wired exclusively
  to `EventNegative`, verified by a code-level assertion that no other event kind's dispatch
  path can reach it
  - depends on: 2.1
- [x] [2.3] Implement `internal/flair/overlay.go`'s lipgloss v2 `Layer`/`Canvas` compositor for [beads:if-gcoz]
  toast-banner spring-in/auto-dismiss, scoped to this package's own rendering path only — no
  change to any existing lipgloss v1 usage in `queuepane.go`/`detailpane.go`; include the
  terminal color-profile detection + `go-colorful` nearest-ANSI-equivalent fallback per
  `design.md` § Config + calm-mode + truecolor gating
  - depends on: 1.2, 2.2
- [x] [2.4] Implement calm-mode + disabled-mode gating in `manager.go`: `!cfg.Enabled` short- [beads:if-cajv]
  circuits before `Diff` is ever called (not merely before effects are applied); `cfg.CalmMode`
  routes every effect selection in `effects.go` to its static-glyph fallback per `design.md` §
  Config + calm-mode + truecolor gating
  - depends on: 2.2, 1.1

## UI Batch

- [ ] [3.1] Extend `internal/ui/queuepane.go` additively: accept an optional [beads:if-n9a8]
  `SetHighlights(map[string]HighlightState)` call from `FlairManager`, render unchanged when the
  map is nil or empty — no change to `QueuePane`'s existing rendering logic for the non-highlight
  path
  - depends on: 2.2
- [ ] [3.2] Wire `FlairManager` into `cmd/wavetui/main.go`'s root model: hold `prevSnapshot`, [beads:if-1vsf]
  call `Diff(prevSnapshot, next)` on each `SnapshotMsg` (gated per 2.4), pass the resulting
  highlight map into `QueuePane.SetHighlights`, composite the overlay from 2.3 over the root
  `View()` output, schedule `tea.Tick` only per `NeedsTick()` — append-only wiring, no reordering
  or removal of the existing pane slice
  - depends on: 3.1, 2.3, 2.4
- [ ] [3.3] Implement `internal/flair/reward.go` (streak counter, combo multiplier for items [beads:if-zut9]
  closed within a rolling window, all-clear state when the ready queue hits zero, rare
  variable-reward celebration variant) — later, lower-priority batch per the Scope section; real
  functionality but does not block any task above or below it
  - depends on: 2.1
- [ ] [3.4] Implement `internal/flair/sprite.go`'s conditional presence sprite per `design.md` § [beads:if-z7pm]
  Presence sprites: at implementation time, check whether `wavetui-core`'s `Item` struct (as
  possibly extended by `wavetui-sessions`) exposes a session-state accessor; if present,
  implement the 2-4 frame cycle sprite mapped to that field; if absent, skip this task entirely
  and file a follow-up bead rather than blocking — additive enhancement, never MVP-blocking
  - depends on: 2.1

## E2E Batch

- [ ] [4.1] `go test` for `internal/flair/manager.go`: `Diff` produces correct events for [beads:if-i5u4]
  appeared/closed/blocker-resolved/negative transitions, `Diff` never mutates its inputs, calling
  `Diff` twice with identical inputs produces identical output, `NeedsTick()` reflects `active`
  map state accurately across settle transitions
  - depends on: 2.1, 1.3
- [ ] [4.2] `go test` for `internal/flair/effects.go`: shake-plus-red-pulse effect is reachable [beads:if-lj9x]
  only from `EventNegative`'s dispatch path, every other event kind's effect selection never
  resolves to that effect, harmonica spring/decay math matches expected settling behavior
  - depends on: 2.2
- [ ] [4.3] `go test` for `internal/flair/overlay.go`: color-profile detection branches correctly [beads:if-1cgl]
  between truecolor and non-truecolor paths, `go-colorful` nearest-ANSI substitution produces a
  valid ANSI color for a range of truecolor inputs
  - depends on: 2.3
- [ ] [4.4] `go test` for `internal/flair/reward.go`: streak/combo counters accumulate correctly [beads:if-bnku]
  across a simulated sequence of closed-item events, all-clear state triggers only when the
  ready queue is genuinely empty
  - depends on: 3.3
- [ ] [4.5] `go test` for `internal/flair/sprite.go` (only if 3.4 was implemented rather than [beads:if-fmtp]
  skipped): sprite state maps 1:1 to the underlying session-state field with no second state
  machine; if 3.4 was skipped, this task is also skipped with the same follow-up-bead note
  - depends on: 3.4
- [ ] [4.6] Runtime-verify end-to-end: run `apps/wavetui/cmd/wavetui` against this repo's own [beads:if-thh6]
  live beads/openspec state, close a real bead and confirm a visible row-flash within one render
  cycle, confirm disabling flair via config produces identical rendering (minus animation) for
  the same snapshot sequence — capture and diff the two renders, confirm a non-truecolor
  `TERM` value (e.g. `TERM=xterm` without truecolor) degrades to ANSI colors rather than broken
  escape sequences, confirm calm mode shows static glyphs — paste the terminal/pty output and the
  disabled-vs-enabled render diff as evidence
  - depends on: 3.2, 4.1, 4.2, 4.3, 4.4
