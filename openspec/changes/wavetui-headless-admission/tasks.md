---
stack: t3
---
<!-- beads:epic:if-tkva -->
<!-- beads:feature:if-d48l -->

# Implementation Tasks

## API Batch

- [ ] [1.1] Add an `enabled bool` toggle to `daemon.Controller` (default `false`), plus a [beads:if-75ks]
      `ToggleAdmission()` method that flips it. searched: `apps/wavetui/internal/daemon/daemon.go`
      — `Controller` currently has no state beyond `dispatcher`; `Resume()` is the existing
      precedent for a single explicit-action method. [type:api]
- [ ] [1.2] Extend `Controller.OnSnapshot` to, when `enabled`, filter `snap.Items` for [beads:if-pznc]
      `Blocker == nil && Session == nil`, sort by `FanOutScore` descending, and call
      `dispatcher.Dispatch(ctx, item, composePrompt(item))` per item until it returns
      `ErrConcurrencyCapReached` (stop silently, no retry, no log). Any other error from
      `Dispatch` surfaces via the existing failure path (check how a manual dispatch failure
      surfaces today in `internal/ui/queuepane.go` and reuse the same mechanism/event, not a new
      one). Unit test: eligibility filter (blocked/claimed items excluded), FanOutScore ordering,
      and the cap-reached stop condition. [type:api]

## UI Batch

- [ ] [2.1] Add a keybinding on `HeadlessBar` (pick an unused key — check [beads:if-yw5j]
      `apps/wavetui/internal/ui/headlessbar.go`'s existing `HandleKey`, which currently only
      handles `r` for resume) that calls `Controller.ToggleAdmission()`, and render a visible
      indicator on the bar showing whether admission is currently enabled (and roughly how many
      slots are in-flight vs. the cap, if that's cheaply available from existing state). Unit
      test: keybinding toggles the state, indicator reflects both enabled/disabled states.
      [type:ui]

## E2E Batch

- [ ] [3.1] Verify end-to-end: with a real (or realistically stubbed) `Snapshot` containing a mix [beads:if-adpc]
      of blocked, claimed, and eligible items, toggle admission on via the keybinding and confirm
      `Dispatch` is called for eligible items in `FanOutScore` order, stops at the concurrency
      cap, and that toggling admission back off stops further admission on the next snapshot
      without touching already-dispatched items. Paste the test output.
      - depends on: 1.1, 1.2, 2.1
