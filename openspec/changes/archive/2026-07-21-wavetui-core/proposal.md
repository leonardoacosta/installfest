---
order: 0720b
---

# Proposal: wavetui-core — event bus, Store, and the two lowest-risk sources

## Change ID
`wavetui-core`

## Summary
Add `apps/wavetui/`, a Go + bubbletea TUI that watches `.beads/` and `openspec/changes/` and
renders a live-updating queue of beads + openspec proposals with a detail pane. This proposal
ships only the core runtime: one event bus, a single-writer Store, `BeadsSource`,
`OpenSpecSource`, and the two lowest-risk panes (`QueuePane`, `DetailPane`).

## Context
- Extends: nothing (greenfield) — follows this repo's `apps/<name>/` convention already used by
  `apps/ctx-scan`, `apps/daily-brief`, `apps/cc-tmux`. This is the first Go app in this repo;
  every existing `apps/*` entry is TypeScript/Bun.
- Related: `openspec/specs/cc-tmux/spec.md` (28 requirements) is a live, actively-maintained
  tmux status-bar plugin with hook-driven pane-state tracking and a beads/openspec summary row
  sourced from nx-agent's roadmap-pulse endpoint. cc-tmux is an always-visible status STRIP;
  wavetui-core is a full-screen INTERACTIVE app — complementary, not a duplicate. See
  `design.md` § Alternatives / Related Work. This proposal does not integrate with cc-tmux
  directly; `BeadsSource`/`OpenSpecSource` shell `bd`/`openspec` CLIs directly for full per-item
  detail (nx-agent's endpoint is counts-only).
- Capability Preflight (Phase 1): not applicable — local dev tool, no hosting/deploy component.
  Both greenfield probes (`packages/db`, `packages/api`) returned empty as expected for a
  dotfiles repo; the gh/vercel/az/POSTGRES_URL/VERCEL_TOKEN preflight is irrelevant to a local
  Go CLI binary and was skipped per explicit operator authorization.
- **This is proposal 1 of 7 in a dependency spine**: `wavetui-core` -> {`wavetui-sessions`,
  `wavetui-dispatch`, `wavetui-memory-timeline`, `wavetui-flair`} -> {`wavetui-decision-lanes`,
  `wavetui-daemon`}. The six siblings are authored one at a time after this proposal and must
  all resolve to the SAME capability epic (`[CAPABILITY] wavetui`), found (not re-created) by
  `spec-sync`.
- touches: `apps/wavetui/go.mod`, `apps/wavetui/cmd/wavetui/main.go`,
  `apps/wavetui/internal/bus/bus.go`, `apps/wavetui/internal/bus/bus_test.go`,
  `apps/wavetui/internal/store/store.go`, `apps/wavetui/internal/store/store_test.go`,
  `apps/wavetui/internal/config/config.go`, `apps/wavetui/internal/blocker/blocker.go`,
  `apps/wavetui/internal/blocker/blocker_test.go`, `apps/wavetui/internal/sources/beads.go`,
  `apps/wavetui/internal/sources/openspec.go`, `apps/wavetui/internal/sources/sources_test.go`,
  `apps/wavetui/internal/ui/root.go`, `apps/wavetui/internal/ui/queuepane.go`,
  `apps/wavetui/internal/ui/detailpane.go`, `apps/wavetui/README.md`

## Motivation
Operator state for beads + openspec work is currently visible only via one-shot CLI calls
(`bd ready`, `openspec show`) or the always-visible-but-summary-only cc-tmux status strip. There
is no live, full-screen, per-item queue that stays current as files change on disk. `wavetui-core`
establishes the runtime shape (bus -> single-writer Store -> bubbletea `Program.Send()`) that the
six sibling proposals build on, and ships the two lowest-risk sources so the queue is useful on
its own before sessions/dispatch/decision-lanes/daemon/flair/memory-timeline land.

## Requirements

### Requirement: Typed event bus delivers source events to exactly one Store writer
See `specs/wavetui/spec.md`.

### Requirement: Store derives normalized queue state by re-querying CLIs, never by inferring from which file changed
See `specs/wavetui/spec.md`.

### Requirement: BeadsSource watches .beads/ and re-queries via bd after a trailing debounce
See `specs/wavetui/spec.md`.

### Requirement: OpenSpecSource watches openspec/changes/ and optionally plans/ and advisor-plans/
See `specs/wavetui/spec.md`.

### Requirement: Blocker-note convention is parsed from a structured "blocked:" line
See `specs/wavetui/spec.md`.

### Requirement: Store snapshots reach the bubbletea Program via Program.Send(), never via watcher logic in Update()
See `specs/wavetui/spec.md`.

### Requirement: QueuePane renders the live queue with blocker badges and fan-out score
See `specs/wavetui/spec.md`.

### Requirement: DetailPane renders full detail for the selected queue row
See `specs/wavetui/spec.md`.

### Requirement: A missing .beads/ or openspec/ directory degrades to an unavailable badge, never a crash
See `specs/wavetui/spec.md`.

### Requirement: Root model exposes a pluggable pane and focus-ring architecture for future sibling panes
See `specs/wavetui/spec.md`.

### Requirement: Source failures render as badges, never panics, with serialized and coalesced CLI invocations
See `specs/wavetui/spec.md`.

## Scope
- **IN**: event bus, single-writer Store with dep-graph + fan-out score derivation, `BeadsSource`,
  `OpenSpecSource`, blocker-note grammar formalization + parser, `QueuePane`, `DetailPane`,
  per-project TOML config, pluggable pane/focus-ring architecture for future panes, tmp+atomic
  `os.rename` write convention for any file this component writes.
- **OUT**: sessions pane, KPI bar, error feed (`wavetui-sessions`); wave-file format and whether
  it's itself a bead (`wavetui-dispatch` — the Store's data model is designed so a future
  wave-file feature can consume its snapshots without rework, but no wave-file code ships here);
  decision-lanes UI (`wavetui-decision-lanes`); daemon/background mode (`wavetui-daemon`);
  visual flair/theming beyond baseline lipgloss layout (`wavetui-flair`); memory-timeline pane
  (`wavetui-memory-timeline`); direct cc-tmux integration (adjacent, not this proposal's concern
  per Context above); a dedicated Go-aware `/apply` engineer agent (flagged in `## Risks`, not
  solved here).

## Done Means
- Operator can run the wavetui binary in any project directory and see a live-updating queue of
  beads + openspec proposals within 2 seconds of a bd/openspec change on disk, with no manual
  refresh.
- Operator can select a queue row and see its full detail (notes, blocker reason if any, task
  progress) in the detail pane.
- A missing `.beads/` or `openspec/` directory renders an "unavailable" badge, not a crash or
  blank screen.
- Killing and restarting the app reflects current on-disk state within one debounce window, no
  stale data survives a restart.

## Testing
| Affected seam | Unit task | E2E task |
|----------------|-----------|----------|
| `internal/bus` | `[4.1]` | `[4.5]` |
| `internal/store` | `[4.2]` | `[4.5]` |
| `internal/sources` (Beads + OpenSpec) | `[4.3]` | `[4.5]` |
| `internal/blocker` | `[4.4]` | `[4.5]` |
| `internal/ui` (QueuePane, DetailPane) | N/A — no pure-function render logic beyond Go compile | `[4.5]` (pty runtime verification) |

## Impact
| Area | Change |
|------|--------|
| `apps/wavetui/` | New Go module — first Go app in this repo |
| `openspec/specs/wavetui/` | New capability spec created (`## ADDED Requirements`) |
| Existing repo files | None modified — purely additive, no `touches:` overlap with any in-flight proposal |

## Risks
| Risk | Mitigation |
|------|-----------|
| No Go-aware `/apply` engineer agent exists in the fleet yet — `dotnet-azure-specialist`/`effect-engineer`/T3 engineers do not write idiomatic Go | `stack: t3` chosen in `tasks.md` as the closest sanctioned enum value per this repo's own precedent (`add-daily-brief-tui`, `harden-ssh-mesh-1password-integration` both used `stack: t3` for non-T3-Turbo work); `commands/apply/references/stacks.md`'s crosswalk table has no tasks.md-authorable value for `go-cli` today. `/apply` dispatch for this spec will fall back to `general-purpose`-equivalent execution until a dedicated Go engineer agent exists — tracked here, not silently absorbed. |
| fsnotify is not recursive and editor tmp+rename saves orphan inode-based watches | Walk-then-watch-per-dir at start, add watches on dir-create events, re-watch by path after rename events (tasks `[2.1]`, `[2.2]`) |
| SQLite WAL commits often touch only the `-wal` file, not the main db | Watch `db`/`db-wal`/`db-shm` together plus a 15s poll fallback; never parse the db directly, always re-query through `bd` after debounce (task `[2.1]`) |
| Six sibling proposals depend on this capability epic being created exactly once | Capability name fixed to `wavetui` (not `wavetui-core`); verified via `spec-sync`'s epic title at Phase 4 Gate 4.1 below |
| Repo location (installfest vs. a standalone wavetui repo) is an open question | Authored in `installfest` per explicit operator instruction for this run; flagged here for Leo's later call, not blocking |
