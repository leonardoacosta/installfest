---
order: 0720h
---

# Proposal: wavetui-daemon — HeadlessDispatcher, concurrency cap, rate-limit backpressure

## Change ID
`wavetui-daemon`

## Summary
Add `HeadlessDispatcher` — a `claude -p` process scheduler implementing `wavetui-dispatch`'s
`Dispatcher` interface — bounded by a config-driven concurrency semaphore, that pauses admission
on a rate-limit signal already emitted by `wavetui-sessions` and resumes only on an explicit
operator action, and that surfaces a non-zero child exit immediately with no automatic retry.

## Context
- depends on: `wavetui-core`, `wavetui-dispatch`, `wavetui-sessions`
- **Depends on `wavetui-core`** (spec dir `openspec/changes/wavetui-core/`, capability epic
  `if-tkva`, feature bead `if-3g1c`), **`wavetui-dispatch`** (spec dir
  `openspec/changes/wavetui-dispatch/`, feature bead `if-7mq2`), and **`wavetui-sessions`** (spec
  dir `openspec/changes/wavetui-sessions/`, feature bead `if-yufp`). Soft dependencies — all three
  are already authored and gate-clean; all three should land first in an apply wave since this
  proposal implements `wavetui-dispatch`'s `Dispatcher` interface verbatim and consumes
  `wavetui-sessions`' `Item.Session`/`Snapshot.RateLimitBanner` fields directly.
- **This is proposal 7 of 7, the last in the wavetui dependency spine**: `wavetui-core` ->
  {`wavetui-sessions`, `wavetui-dispatch`, `wavetui-memory-timeline`, `wavetui-flair`} ->
  {`wavetui-decision-lanes`, `wavetui-daemon`}. Resolves to the SAME capability epic (`if-tkva`,
  `[CAPABILITY] wavetui`) — verified at Phase 4 Gate 4.1 below, not re-created.
- **Reuse-not-rebuild (Reader Gate, non-negotiable) — verified against the two sibling artifacts
  this proposal implements against, not guessed:**
  - `wavetui-dispatch`'s `Dispatcher` interface (`design.md` § Dispatcher interface, `specs/wavetui/spec.md`
    Requirement "Dispatcher interface abstracts prompt delivery behind Dispatch(item, promptText)")
    is exactly `Dispatch(ctx context.Context, item store.Item, promptText string) error` — one
    method, deliberately shaped so a headless implementation needs no signature change. This
    proposal implements that exact signature; it does NOT invent a second dispatch interface.
  - `wavetui-sessions`' rate-limit signal (`design.md` § Rate-limit backpressure: emit only, never
    consume) is `Snapshot.RateLimitBanner *RateLimitSignal` (nil when no active signal), populated
    by `TranscriptSource` when it observes a rate-limit indicator in the transcript stream. That
    proposal's own Scope explicitly defers "building the headless-dispatch queue that would PAUSE
    on this signal" to a sibling — this proposal is that sibling. `HeadlessDispatcher` reads
    `Snapshot.RateLimitBanner`; it does NOT re-parse transcripts or invent a second detection path.
  - `wavetui-sessions`' zombie detection (`design.md` § Zombie detection: two independent signals,
    never either alone; `Item.Session.Zombie`/`ZombieSince` fields) is already shipped as
    "always an explicit one-key user action, never automatic" release, rendered by `SessionsPane`.
    This proposal consumes `Item.Session.Zombie` to stop counting a stalled headless child against
    the concurrency cap; it does NOT build a second zombie badge or auto-release the claim.
  - `wavetui-core`'s `Item.TaskProgress *TaskProgress` (`design.md` § Store data model) already
    distinguishes "not started" (nil) from "partially done" — reused to decide whether the composed
    prompt is `/apply <id>` or `/apply <id> --continue`, rather than this proposal tracking its own
    per-item resume state.
  - `apply.md`'s existing `--continue` resume contract (`commands/apply.md` § "--continue: resume
    an interrupted single-spec /apply") already implements "one bounded unit of work per session,
    then stop cleanly with a resumable state" — `HeadlessDispatcher` does not reimplement session
    boundaries; it delegates entirely to `/apply`'s own phase/wave gating by invoking `claude -p
    "/apply <id>[...--continue]"` and treating the child's own clean exit as the unit boundary.
- **KPI-history checked, deliberately not built here**: the seed brief flagged a possible gap in
  `wavetui-sessions`' `KPIBar` (real-time only, no trend rendering) that this proposal could fill
  with an `ntcharts` sparkline. Checked `wavetui-sessions/design.md` (`KPIBar` renders
  continue-count / rate-limit incidents / stale-claim minutes, no historical state) and
  `wavetui-flair/design.md` line 179-185 (`ntcharts v0.5.1` confirmed stable via proxy.golang.org,
  evaluated and explicitly **not adopted** for exactly this reason — "no historical-trend
  rendering claimed" by `KPIBar`, deferred to "a future proposal"). The gap is real, but building
  a new history view is not required by this proposal's Done Means and is scoped OUT below to
  avoid absorbing a sibling's deferred concern under cover of this one.
- Capability Preflight (Phase 1): not applicable, matching all prior siblings' precedent — local
  Go CLI, no hosting/deploy component. Skipped per explicit operator authorization.
- touches: `apps/wavetui/internal/daemon/daemon.go`, `apps/wavetui/internal/daemon/daemon_test.go`,
  `apps/wavetui/internal/daemon/headless_dispatcher.go`,
  `apps/wavetui/internal/daemon/headless_dispatcher_test.go`,
  `apps/wavetui/internal/ui/headlessbar.go`, `apps/wavetui/internal/ui/headlessbar_test.go`,
  `apps/wavetui/internal/config/config.go` (additive field only — see Risks for the coordination
  note with `wavetui-core` and `wavetui-flair`, both of which also touch this file),
  `apps/wavetui/internal/store/store.go` (additive field only — see Risks)

## Motivation
`wavetui-dispatch` lets an operator send one item to a live tmux pane or the clipboard, one
keypress at a time — but every dispatch still needs a human at the keyboard. `wavetui-daemon`
closes that gap for the ready backlog: a bounded pool of headless `claude -p` workers that pull
unblocked items, respect the same rate-limit signal `wavetui-sessions` already detects, and never
retry a failure on their own — because an unattended retry loop against a rate limit is the
specific failure mode this proposal exists to prevent, not a corner case to guard against later.

## Requirements

### Requirement: HeadlessDispatcher implements the Dispatcher interface with no signature change
See `specs/wavetui/spec.md`.

### Requirement: Admission is bounded by a config-driven concurrency semaphore
See `specs/wavetui/spec.md`.

### Requirement: Dispatched prompts embed /apply <id> so session linkage requires no new code
See `specs/wavetui/spec.md`.

### Requirement: A rate-limit signal from Snapshot.RateLimitBanner pauses admission until explicit operator resume
See `specs/wavetui/spec.md`.

### Requirement: A zombie-flagged headless session frees its concurrency slot without auto-release or auto-retry
See `specs/wavetui/spec.md`.

### Requirement: A non-zero or errored child exit surfaces immediately with no automatic retry
See `specs/wavetui/spec.md`.

## Scope
- **IN**: `HeadlessDispatcher` (implements `wavetui-dispatch`'s `Dispatcher` interface verbatim),
  concurrency-capped admission via semaphore (config-driven, conservative default), prompt
  composition embedding `/apply <id>`/`/apply <id> --continue`, async exit monitoring via a typed
  event on `wavetui-core`'s existing bus, an additive `Snapshot.HeadlessQueue` field, consumption
  (read-only) of `Snapshot.RateLimitBanner` to pause/resume admission with an explicit-operator-only
  resume action, a visible pause banner (`internal/ui/headlessbar.go`, a new focus-ring pane),
  consumption (read-only) of `Item.Session.Zombie` to free a concurrency slot, immediate no-retry
  failure surfacing on non-zero/errored exit.
- **OUT**: rate-limit signal *detection* (`wavetui-sessions`' concern, already shipped — this
  proposal only reads `Snapshot.RateLimitBanner`); zombie-detection *algorithm* (`wavetui-sessions`'
  concern, already shipped — this proposal only reads `Item.Session.Zombie`); any form of
  automatic retry (never, by design — see Motivation); `TmuxDispatcher`/`ClipboardDispatcher`
  changes (`wavetui-dispatch`'s concern, unmodified here); the wave-file format decision
  (`wavetui-dispatch`'s own `[1.1]` `[user]` task, unrelated); KPI history/trend visuals (checked,
  deliberately not built here — see Context); decision-lanes UI (`wavetui-decision-lanes`);
  memory-timeline pane (`wavetui-memory-timeline`); visual flair/theming (`wavetui-flair`).

## Done Means
- Operator can enable headless dispatch and see it process ready items up to the configured
  concurrency cap, never exceeding it.
- A rate-limit signal from `wavetui-sessions`' `TranscriptSource` pauses the headless queue and
  shows a visible banner; resuming requires an explicit operator keypress, never a bare timer.
- A headless-dispatched session that goes stale surfaces through the same zombie badge
  `wavetui-sessions`' `SessionsPane` already renders, not a second one.
- A non-zero exit from a headless-dispatched child surfaces immediately in the UI with no
  automatic retry attempted.

## Testing
| Affected seam | Unit task | E2E task |
|----------------|-----------|----------|
| `internal/daemon/headless_dispatcher.go` (Dispatch, semaphore admission, exit monitoring) | `[2.1]` | `[4.1]` |
| `internal/daemon/daemon.go` (rate-limit pause/resume, zombie slot release) | `[2.2]`, `[2.3]` | `[4.1]` |
| `internal/ui/headlessbar.go` (banner render, resume keybinding) | N/A — no pure-function render logic beyond Go compile | `[4.1]` (pty runtime verification) |
| `internal/config/config.go` additive `HeadlessConcurrencyCap` field | `[1.2]` | `[4.1]` |

## Impact
| Area | Change |
|------|--------|
| `apps/wavetui/internal/daemon/` | New package — `HeadlessDispatcher` + daemon controller (pause/resume, slot accounting) |
| `apps/wavetui/internal/ui/headlessbar.go` | New focus-ring pane — pause banner + resume keybinding |
| `apps/wavetui/internal/config/config.go` | Additive `HeadlessConcurrencyCap int` field (default 2) |
| `apps/wavetui/internal/store/store.go` | Additive `Snapshot.HeadlessQueue *HeadlessQueueState` field only |
| Existing repo files outside the above | None modified — `wavetui-dispatch`'s `Dispatcher` interface is implemented against, never edited |

## Risks
| Risk | Mitigation |
|------|-----------|
| `stack: t3` chosen (same non-ideal fit as every prior sibling) — no dedicated Go engineer agent exists yet | Same documented precedent (`wavetui-core`, `wavetui-sessions`, `wavetui-dispatch`); tracked, not silently absorbed. |
| This proposal's `- touches:` list includes `apps/wavetui/internal/config/config.go`, which `wavetui-core` (defines it) and `wavetui-flair` (adds `Flair.Enabled`/`Flair.CalmMode`) also touch | Purely additive on all three sides — no existing field renamed, removed, or re-typed. `wave-plan-build`'s conflict matrix serializes this proposal into a different wave than `wavetui-flair`; `wavetui-core` MUST land first per the Context dependency above. |
| This proposal's `- touches:` list includes `apps/wavetui/internal/store/store.go`, already touched additively by `wavetui-core`, `wavetui-sessions`, and `wavetui-dispatch` (fourth proposal to extend it) | Purely additive — `Snapshot.HeadlessQueue` is a new field, no existing `Item`/`Snapshot` field renamed or removed. Same serialization handled by the wave conflict matrix. |
| Headless children are long-running `claude -p` subprocesses; killing the daemon process (e.g. wavetui itself exits) orphans them mid-turn — **confirmed, not hypothetical**: `tasks.md` `[4.1]`'s `TestE2EChildProcessSurvivesWhenDaemonKilled` SIGKILLed a real daemon-standin process and observed both a real child and its grandchild survive, reparented to PID 1 (see `design.md` § Child-process lifecycle on daemon exit for the `ps` evidence) | Out of scope for a first cut. Follow-up filed as `if-ugxa.1` ("wavetui-daemon: register headless child PIDs for orphan cleanup after crash") to track registering running children's PIDs somewhere an operator can discover and reap them after a daemon crash. |
| KPI-history/trend visuals are a real, documented gap (see Context) this proposal deliberately does not fill | Flagged explicitly rather than silently absorbed or silently ignored; a future proposal can add `ntcharts` (confirmed stable, `v0.5.1`) against `KPIBar` without needing anything from this proposal's daemon package. |
| Repo location (installfest vs. a standalone wavetui repo) is an open question shared with every sibling in this fan-out | Authored in `installfest` per explicit operator instruction for this run; flagged here for Leo's later call, not blocking. |
