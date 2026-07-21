---
order: 0720e
---

# Proposal: wavetui-decision-lanes — blocker-note-driven decision/spike/design lanes

## Change ID
`wavetui-decision-lanes`

## Summary
Add lane badges to `QueuePane` for any item whose blocker note matches `wavetui-core`'s
`blocked: <type> - <reason> (see <ref>)` grammar, plus a lane-trigger action that spawns a live
`claude` session targeting that blocker. The spawned session's contract is capture-back: it must
write its resolution into a bd note or the openspec delta before exiting — the bead/openspec
note landing on disk (observed through the existing fsnotify+re-query pipeline) is the ONLY
completion signal that clears the badge, deliberately avoiding any new IPC or exit-code polling.

## Context
- depends on: `wavetui-core`, `wavetui-dispatch`, `wavetui-sessions`
- **Depends on `wavetui-core`** (spec dir `openspec/changes/wavetui-core/`, capability epic
  `if-tkva`) for the `blocked: <type> - <reason> (see <ref>)` grammar (already formalized —
  see its `design.md` § Blocker-note grammar, cited not re-derived) and the `Item`/`Snapshot`
  Store this proposal's lane state extends additively.
- **Depends on `wavetui-dispatch`** (spec dir `openspec/changes/wavetui-dispatch/`) for the
  `Dispatcher` interface and `TmuxDispatcher` pane-targeting primitives this proposal's spawn
  action extends, per the reuse instruction below.
- **Depends on `wavetui-sessions`** (spec dir `openspec/changes/wavetui-sessions/`) for
  `TmuxSource`'s `@cc-state` pane-liveness signal, reused to distinguish a genuinely-stuck lane
  session from one actively being worked.
- **This is proposal 4 of 7 in the wavetui dependency spine**: `wavetui-core` ->
  {`wavetui-sessions`, `wavetui-dispatch`, `wavetui-memory-timeline`, `wavetui-flair`} ->
  {`wavetui-decision-lanes`, `wavetui-daemon`}. Resolves to the SAME capability epic (`if-tkva`,
  `[CAPABILITY] wavetui`) — verified at Phase 4 Gate 4.1 below, not re-created.
- **Reuse-not-rebuild (Reader Gate) — documented coordination gap, not silently worked around**:
  the operator instruction for this proposal is to spawn the ephemeral session by calling into
  `wavetui-dispatch`'s existing `Dispatcher` interface's "create a new pane and start a fresh
  claude session" mode rather than writing new tmux-split-creation code. Reading
  `wavetui-dispatch`'s shipped `design.md` (§ Dispatcher interface, § TmuxDispatcher primitive
  choice): `Dispatcher.Dispatch(ctx, item store.Item, promptText string) error` resolves to an
  EXISTING pane (via `item.Session.PaneID` or best-guess `Resolver` scoring against
  `conductor list --json`) or falls back to `ClipboardDispatcher` — there is no code path that
  creates a NEW pane. `wavetui-dispatch`'s own proposal.md cites `cc-tmux conductor dispatch
  --mode <switch|send-prompt|spawn-task|spawn-worktree>` as the CLI primitive it shells out to,
  but only wires up `switch` (pane-focus, no paste) and a hardened re-implementation of
  `send-prompt` (bracketed paste into an existing pane) — `spawn-task`/`spawn-worktree` are
  cited in its own design.md but never wired into the `Dispatcher` interface or any concrete
  type. **This is a confirmed gap, not an oversight this proposal can silently work around**:
  per the operator's explicit fallback instruction, this proposal documents the gap in
  `design.md` § Spawn gap and proceeds with a task that EXTENDS `wavetui-dispatch`'s package
  with a new `Spawner` interface (`Spawn(ctx, promptText string) (paneID string, err error)`)
  implemented by a `TmuxSpawner` that shells `cc-tmux conductor dispatch --mode spawn-task`
  (the already-cited, already-existing CLI primitive) — this is extending the established
  Dispatcher-family abstraction, not inventing a second, unrelated pane-creation mechanism.
- **Reuses `wavetui-sessions`'s `TmuxSource`/`@cc-state` signal** for lane-session liveness — no
  new liveness mechanism. See `design.md` § Lane liveness.
- **Blocker-notes grammar is consumed, never changed** — cites `wavetui-core`'s `design.md` §
  Blocker-note grammar verbatim; this proposal adds no new file, frontmatter field, or grammar
  variant.
- Capability Preflight (Phase 1): not applicable, matching all three siblings' precedent — local
  Go CLI, no hosting/deploy component. Skipped per explicit operator authorization.
- touches: `apps/wavetui/internal/lanes/lanes.go`, `apps/wavetui/internal/lanes/lanes_test.go`,
  `apps/wavetui/internal/dispatch/spawn.go`, `apps/wavetui/internal/dispatch/spawn_test.go`,
  `apps/wavetui/internal/ui/queuepane.go`, `apps/wavetui/internal/store/store.go`

## Motivation
`wavetui-core` already renders a warning-worthy blocker note as plain text and `wavetui-dispatch`
lets an operator paste a prompt into an existing session, but neither closes the loop on the most
common real blocker shape observed in this repo's own beads/proposals: a `blocked: decision - ...`
note that needs a human answer before the item can proceed. Today that means the operator
manually opens a terminal, starts `claude`, types the context, and separately remembers to write
the answer somewhere durable. `wavetui-decision-lanes` turns that into one keypress that spawns a
targeted session with a capture-back contract baked into its prompt, and clears the badge purely
by observing the same disk state the app already watches.

## Requirements

### Requirement: A queue item whose blocker note matches the blocked:<type> grammar renders a lane badge naming the type
See `specs/wavetui/spec.md`.

### Requirement: Pressing the lane action spawns a claude session via a Spawner extension of the Dispatcher family, never a second pane-creation mechanism
See `specs/wavetui/spec.md`.

### Requirement: The spawned session's prompt template mandates writing its resolution into a bd note or openspec delta before exiting
See `specs/wavetui/spec.md`.

### Requirement: A lane badge clears only when the linked item's blocker-note text changes, observed via the existing fsnotify+re-query pipeline
See `specs/wavetui/spec.md`.

### Requirement: Lane-session liveness is shown by reusing wavetui-sessions' TmuxSource @cc-state signal, never a new liveness mechanism
See `specs/wavetui/spec.md`.

### Requirement: A lane session that exits without writing a resolution leaves the badge in place and surfaces a manual-cleanup prompt after a configurable idle window
See `specs/wavetui/spec.md`.

## Scope
- **IN**: lane-badge detection (reusing `wavetui-core`'s existing blocker-note parse), lane
  button/keypress in `QueuePane`, the `Spawner` interface + `TmuxSpawner` extension of
  `wavetui-dispatch`'s Dispatcher family, the spawn prompt template and its capture-back
  contract, badge-clear-on-note-change (no new completion signal), lane-session liveness display
  (reusing `wavetui-sessions`' `@cc-state` signal), stale-lane manual-cleanup prompt after a
  configurable idle window.
- **OUT**: any new IPC/callback channel for the spawned session (explicitly rejected — the
  fsnotify+re-query pipeline is the only completion signal); automatic badge clearing or
  automatic release of a stale lane (manual action only, never automatic); changing or
  re-deriving the `blocked: <type> - <reason> (see <ref>)` grammar (`wavetui-core`'s concern,
  consumed as-is); `HeadlessDispatcher`/rate-limit-aware retry (`wavetui-daemon`'s concern);
  memory-timeline pane (`wavetui-memory-timeline`); visual flair/theming (`wavetui-flair`).

## Done Means
- Operator sees a lane badge on any queue item whose blocker note matches the
  `blocked: <type> - ...` grammar, with the type visible.
- Pressing the lane button spawns a live `claude` session targeting that blocker, using a
  `Spawner` extension of `wavetui-dispatch`'s existing Dispatcher family, not a new mechanism.
- When that session writes its resolution into the bead note or openspec delta, the badge clears
  on the next fsnotify-triggered re-render, with no manual "mark resolved" action needed.
- A lane session that dies without writing a resolution leaves the badge in place and, after a
  configurable idle window, surfaces a manual-cleanup prompt — never an automatic release.

## Testing
| Affected seam | Unit task | E2E task |
|----------------|-----------|----------|
| `internal/lanes/lanes.go` (badge detection, idle-window staleness) | `[3.1]` | `[4.1]` |
| `internal/dispatch/spawn.go` (`Spawner`, `TmuxSpawner`) | `[3.2]` | `[4.1]` |
| `internal/ui/queuepane.go` (lane badge render, lane button, cleanup prompt) | `[3.3]` | `[4.1]` (pty runtime verification) |

## Impact
| Area | Change |
|------|--------|
| `apps/wavetui/internal/lanes/` | New package — lane detection + liveness + idle-window staleness |
| `apps/wavetui/internal/dispatch/spawn.go` | New file — `Spawner` interface + `TmuxSpawner`, extends `wavetui-dispatch`'s package |
| `apps/wavetui/internal/ui/queuepane.go` | Extended with lane badge render + lane-trigger key handling (shared file with `wavetui-dispatch`) |
| `apps/wavetui/internal/store/store.go` | Additive `Item.LaneState` field only (shared file with `wavetui-core`/`wavetui-sessions`/`wavetui-dispatch`) |
| Existing repo files | None modified — `apps/cc-tmux/` is shelled out to via the already-cited `spawn-task` mode, never edited |

## Risks
| Risk | Mitigation |
|------|-----------|
| `wavetui-dispatch`'s `Dispatcher` interface does not support spawning a new pane — confirmed gap, not assumed | Documented in `design.md` § Spawn gap; this proposal adds a `Spawner` interface as a sibling extension of the same package rather than inventing an unrelated mechanism, per explicit operator fallback instruction. |
| `queuepane.go` and `store.go` are touched by three prior sibling proposals already | Both edits here are additive (new key handler branch, new struct field) — `wave-plan-build`'s conflict matrix will serialize this proposal into a later wave behind `wavetui-core`/`wavetui-sessions`/`wavetui-dispatch`, consistent with the dependency order already declared above. |
| A spawned session could exit "cleanly" (zero error code) without ever writing a resolution — a well-behaved-looking failure | Explicitly treated as a lane-session-not-resolved: the badge stays regardless of exit code, since the ONLY completion signal is the note/delta text changing. Session liveness (via `wavetui-sessions`' `@cc-state`) plus an idle-window timer distinguishes "still working" from "exited/abandoned, needs manual cleanup." |
| `stack: t3` chosen (same non-ideal fit as all three siblings) — no dedicated Go engineer agent exists yet | Same documented precedent (`wavetui-core`, `wavetui-sessions`, `wavetui-dispatch`); tracked, not silently absorbed. |
