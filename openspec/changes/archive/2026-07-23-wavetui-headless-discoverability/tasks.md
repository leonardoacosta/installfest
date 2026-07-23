---
stack: t3
---
<!-- beads:feature:if-9d2n -->

<!-- beads:epic:if-tkva -->

<!-- stack: one of t3 | cc-meta | effect | dotnet — see commands/apply/references/stacks.md § "Stack vocabulary crosswalk" for the full tasks.md-stack:/--stack-profile/detect_stack() mapping -->

# Implementation Tasks

## UI Batch

- [x] [1.1] Add a method to `apps/wavetui/internal/ui/headlessbar.go` (e.g. [beads:if-f6e5]
  `AdmissionHint() string`) that renders "a: headless dispatch (off)"/"(on)" from
  `Controller.AdmissionEnabled()`, independent of `View()`'s existing empty-common-case
  contract — per this proposal's `specs/wavetui/spec.md` MODIFIED "An operator keybinding
  enables/disables headless admission" Requirement's new scenario
- [x] [1.2] Append `HeadlessBar.AdmissionHint()`'s output to the persistent strip in [beads:if-thdk]
  `apps/wavetui/internal/ui/root.go`'s `View()`, always rendered regardless of
  `HeadlessBar.View()`'s own empty/non-empty state
  - depends on: 1.1
- [x] [1.3] Add a dispatch-mechanism fallback to `apps/wavetui/internal/ui/queuepane.go`'s [beads:if-3zqc]
  `renderBlockerCell`: when no dispatch badge, lane badge, or blocker badge applies, render
  "tmux" if `item.Session != nil && item.Session.PaneID != ""`, else "clipboard" — per spec.md's
  "an unblocked item with no other badge shows its dispatch mechanism" / "an item with no
  linked pane shows the clipboard fallback" scenarios

## E2E Batch

- [x] [2.1] `go test` for all three touched files: confirm existing tests pass unmodified, plus [beads:if-khwy]
  new coverage for the admission hint's off/on text and the dispatch-mechanism tag's
  precedence-vs-existing-badges behavior, and a runtime pty verification pass (build
  `apps/wavetui/cmd/wavetui`, run in a real pty, paste terminal output showing the
  always-visible admission hint on a fresh launch, its text flipping after pressing "a", and
  the per-item mechanism tag on an item with no other badge)
  - depends on: 1.1, 1.2, 1.3
