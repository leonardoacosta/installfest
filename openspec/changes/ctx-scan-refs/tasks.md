---
stack: t3
---
<!-- beads:epic:if-wfel -->
<!-- beads:feature:if-9ib5 -->

# Implementation Tasks

## DB Batch

- [ ] [1.1] Define the shelf-entry view model in `src/refs.ts`: `{path, owner, reachable: [beads:if-whij]
  boolean, citation?: string, size, tocBand, nestingBand}`, sourced from `ctx-scan-assembly`'s
  discovery output and `ctx-scan-budgets`' A5/A6 `Node.bands` entries.

## API Batch

- [ ] [2.1] Implement reachability detection: for each `references/` file, un-imported rules [beads:if-kj0w]
  file, and memory topic file, search owning skill/command/agent bodies for a markdown-link
  citation to that path; record the citing document + line when found, else mark `orphan`.
  - depends on: 1.1
- [ ] [2.2] Implement group-by-owner assembly: bucket shelf entries by their owning [beads:if-qeuu]
  skill/command/agent.
  - depends on: 2.1
- [ ] [2.3] Implement per-skill scoping: filter the shelf to entries owned by a single named [beads:if-48sp]
  skill.
  - depends on: 2.2

## UI Batch

- [ ] [3.1] Wire the shelf view into `ctx-scan render`'s output: a project-scoped shelf panel [beads:if-a4es]
  and a `--skill <name>` scoping flag, each entry linking to `ctx-scan-render`'s level-3 document
  detail view.
  - depends on: 2.2, 2.3

## E2E Batch

- [ ] [4.1] Fixture with one reference file cited via a markdown link from its owning SKILL.md [beads:if-8mly]
  and one genuinely orphaned reference file; assert the linked one reports its citing line and
  the orphaned one reports `orphan`. Include a third reference cited only via prose (no markdown
  link) to establish the detection boundary explicitly.
  - depends on: 2.1
- [ ] [4.2] Assert shelf entries' ToC/nesting bands match `ctx-scan-budgets`' `Node.bands` output [beads:if-akzw]
  exactly for the same fixture files — proves reuse, not re-derivation.
  - depends on: 1.1
- [ ] [4.3] Multi-skill fixture; assert `--skill <name>` scoping excludes every other skill's [beads:if-rm5i]
  references.
  - depends on: 2.3
- [ ] [4.4] Run the shelf view against the real `~/dev/cc` scan; confirm any genuinely orphaned [beads:if-vyxh]
  reference files are flagged, the 79 known no-ToC files (per
  `docs/context-budget-rubric.md` Part 2's A5 row) appear AMBER, and every listed entry
  click-opens into the detail view.
  - depends on: 3.1
- [ ] [4.5] `tsc --noEmit` and `bun test` both green. [beads:if-bl3s]
  - depends on: 3.1
