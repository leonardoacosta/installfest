---
stack: t3
---
<!-- beads:feature:if-wp67 -->

<!-- beads:epic:if-tkva -->

<!-- stack: one of t3 | cc-meta | effect | dotnet — see commands/apply/references/stacks.md § "Stack vocabulary crosswalk" for the full tasks.md-stack:/--stack-profile/detect_stack() mapping -->

# Implementation Tasks

## DB Batch

- [ ] [1.1] Add additive `Description string` field to `store.Item` in `apps/wavetui/internal/ [beads:if-6rwu]
  store/store.go`, per this proposal's `specs/wavetui/spec.md` MODIFIED "Store derives
  normalized queue state..." Requirement — no existing field renamed, removed, or re-typed.

## API Batch

- [ ] [2.1] Add `Description string \`json:"description"\`` to `beadRecord` in [beads:if-yijz]
  `apps/wavetui/internal/sources/beads.go`, and thread it into `toItem`'s returned `store.Item`
  — per spec.md's "a bead's description is threaded through from bd's own output" scenario
  - depends on: 1.1
- [ ] [2.2] Add a `summaryRe` regex to `apps/wavetui/internal/sources/openspec.go` extracting [beads:if-m66z]
  the `## Summary` section's body (bounded to the next `## ` header or end-of-file, mirroring
  `titleRe`'s existing single-purpose-regex convention) and populate `item.Description` in
  `parseOneProposal` — per spec.md's "a proposal's Summary section is extracted" scenario
  - depends on: 1.1

## UI Batch

- [ ] [3.1] Render `Item.Description` (when non-empty) in `apps/wavetui/internal/ui/ [beads:if-jfhi]
  detailpane.go`'s `View()`, wrapped to `detailWidth` via the existing `lipgloss` styling — per
  spec.md's "an item with a description shows it" / "an item with no description shows no
  extra section" scenarios
  - depends on: 2.1, 2.2

## E2E Batch

- [ ] [4.1] `go test` for all three touched packages: confirm existing tests pass unmodified, [beads:if-o9kb]
  plus new coverage for `Description` round-tripping through `beads.go`'s `toItem`,
  `openspec.go`'s `summaryRe` extraction (including the "no Summary section" and "Summary
  followed by another ## header" boundary cases), and a runtime pty verification pass (build
  `apps/wavetui/cmd/wavetui`, run against this repo's own `.beads/`/`openspec/` tree, paste
  terminal output showing a bead's and a proposal's description both rendering in the detail
  pane)
  - depends on: 1.1, 2.1, 2.2, 3.1
