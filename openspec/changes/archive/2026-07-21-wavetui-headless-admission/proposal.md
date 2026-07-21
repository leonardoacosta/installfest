---
order: 0721a
---

# Proposal: wavetui-headless-admission — wire the admission-trigger loop into HeadlessDispatcher

## Change ID
`wavetui-headless-admission`

## Summary
`HeadlessDispatcher`/`Controller`/`HeadlessBar` (shipped in `wavetui-daemon`) are fully built,
tested, and wired to the Store, but nothing in the running app ever calls
`HeadlessDispatcher.Dispatch()` on a real ready item — `Controller.OnSnapshot` only reacts to
rate-limit pause and zombie-slot-release today. This proposal adds the missing admission loop:
an explicit operator keybinding enables headless dispatch, and `OnSnapshot` then scans each fresh
`Snapshot` for unblocked, unclaimed items and dispatches them in fan-out-priority order up to the
existing concurrency cap.

## Context
- depends on: none (wavetui-daemon, the only prerequisite, is already archived/shipped)
- Follow-on to `wavetui-daemon` (archived 2026-07-21, capability epic `if-tkva`, feature bead
  `if-ugxa`). Filed as its own proposal, not reopening `wavetui-daemon`, because this is a genuine
  scope gap the original 7-proposal wavetui fan-out never sliced a task for (confirmed: no task
  in `wavetui-daemon/tasks.md` ever says "iterate ready items and dispatch via
  HeadlessDispatcher") — not a defect in anything that was implemented. Tracked as bead
  `if-ugxa.2`.
- **Reuse-not-rebuild (Reader Gate, non-negotiable) — verified via `/explore`, not guessed:**
  - `Item.Blocker *BlockerNote` (`apps/wavetui/internal/store/store.go`) is already the Store's
    single "unblocked/ready" definition (nil = unblocked). This proposal reuses it directly; it
    does NOT re-derive readiness from a fresh `bd ready` call, which would create a second,
    driftable definition of "ready" alongside the one `QueuePane` already renders.
  - `Item.FanOutScore int` (same file — "count of transitive dependents this item unblocks") is
    already computed and currently used only for display. This proposal reuses it as the
    admission-ordering key; it does NOT add a new priority/ordering field.
  - `HeadlessDispatcher.Dispatch(ctx, item, promptText) error`
    (`apps/wavetui/internal/daemon/headless_dispatcher.go`) already self-guards on capacity,
    returning `ErrConcurrencyCapReached` when every slot is in use. This proposal's admission loop
    calls `Dispatch` per eligible item and stops on that error; it does NOT reimplement slot
    counting.
  - `Controller.OnSnapshot` (`apps/wavetui/internal/daemon/daemon.go`) is already the single
    reactive entry point the two existing daemon signals (rate-limit pause, zombie-slot-release)
    hang off — per its own doc comment, "neither needs its own watcher or a second copy of the
    fan-out over snap.Items." This proposal extends the SAME function; it does NOT add a new
    watcher, goroutine, or timer.
  - `HeadlessBar`'s existing resume-on-`r` keybinding (`apps/wavetui/internal/ui/headlessbar.go`)
    is this codebase's established precedent that starting/resuming any automated dispatch is
    always an explicit, single operator keypress — never a config-only flag, never a timer. This
    proposal's enable/disable keybinding follows the identical pattern; it does NOT introduce a
    config-only auto-start.
- Capability Preflight (Phase 1): not applicable — local Go CLI, no hosting/deploy component,
  same precedent as every wavetui sibling proposal.
- touches: `apps/wavetui/internal/daemon/daemon.go`, `apps/wavetui/internal/daemon/daemon_test.go`,
  `apps/wavetui/internal/ui/headlessbar.go`, `apps/wavetui/internal/ui/headlessbar_test.go`

## Motivation
Without this proposal, `proposal.md`'s own Done Means bullet from `wavetui-daemon` ("Operator can
enable headless dispatch and see it process ready items up to the configured concurrency cap")
is unreachable from the real running app — every piece exists, but nothing connects them. An
operator has no way to actually get unattended `claude -p` workers processing the ready backlog
today; the only path from `QueuePane` is still one manual dispatch at a time.

## Requirements

### Requirement: An operator keybinding enables/disables headless admission
See `specs/wavetui/spec.md`.

### Requirement: Controller.OnSnapshot dispatches eligible ready items when admission is enabled
See `specs/wavetui/spec.md`.

### Requirement: Eligible items are unblocked and unclaimed, ordered by FanOutScore descending
See `specs/wavetui/spec.md`.

### Requirement: Admission stops cleanly on ErrConcurrencyCapReached with no retry loop
See `specs/wavetui/spec.md`.

## Scope
- **IN**: an `enabled` toggle on the daemon `Controller` (or `HeadlessBar`, wherever state
  naturally lives per the existing pause/resume pattern) flipped by a new keybinding on
  `HeadlessBar`; extending `OnSnapshot` to, when enabled, filter `snap.Items` for
  `Blocker == nil && Session == nil`, sort by `FanOutScore` descending, and call
  `dispatcher.Dispatch()` per item in that order until `ErrConcurrencyCapReached`; a visible
  indicator on `HeadlessBar` showing admission is enabled and roughly how many items are
  in-flight vs. capped.
- **OUT**: any change to `HeadlessDispatcher`'s own capacity/pause/exit-monitoring logic
  (`wavetui-daemon`'s concern, already shipped, unmodified here); any change to
  `Item.Blocker`/`Item.FanOutScore`'s computation (`wavetui-core`'s concern); `TmuxDispatcher`/
  `ClipboardDispatcher`/the candidate picker (`wavetui-dispatch`'s concern, already shipped);
  re-deriving readiness from `bd ready` directly (checked, deliberately not done — see Context).

## Done Means
- Operator can press the enable keybinding on `HeadlessBar` and see the headless queue begin
  dispatching real unblocked, unclaimed items, never exceeding the configured concurrency cap.
- Items are admitted in `FanOutScore`-descending order (items unblocking the most downstream work
  go first).
- No dispatch happens before the enable keybinding is pressed — the toggle defaults to disabled
  on every app start.
- Pressing the same keybinding again disables admission; in-flight dispatches are not killed, but
  no new item is admitted until re-enabled.

## Testing
| Affected seam | Unit task | E2E task |
|----------------|-----------|----------|
| `internal/daemon/daemon.go` (OnSnapshot admission loop, eligibility filter, FanOutScore ordering) | `[1.1]`, `[1.2]` | `[3.1]` |
| `internal/ui/headlessbar.go` (enable/disable keybinding, admission-state indicator) | `[2.1]` | `[3.1]` |

## Impact
| Area | Change |
|------|--------|
| `apps/wavetui/internal/daemon/daemon.go` | `Controller` gains an `enabled` toggle + admission loop inside `OnSnapshot` |
| `apps/wavetui/internal/ui/headlessbar.go` | New keybinding (toggle admission) + indicator render |
| Existing repo files outside the above | None modified |

## Risks
| Risk | Mitigation |
|------|-----------|
| `stack: t3` chosen (same non-ideal fit as every prior wavetui sibling) — no dedicated Go engineer agent exists yet | Same documented precedent (`wavetui-core` through `wavetui-decision-lanes`); tracked, not silently absorbed. |
| Enabling admission on a queue with many simultaneously-unblocked items could dispatch a burst up to the cap all at once | Acceptable — the cap itself is the safety bound (config-driven, conservative default per `wavetui-daemon`'s design.md), and this is the documented intended behavior ("process ready items up to the configured concurrency cap"), not a new risk this proposal introduces. |
