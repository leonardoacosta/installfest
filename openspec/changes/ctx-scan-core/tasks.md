---
stack: t3
---
<!-- beads:epic:if-wfel -->
<!-- beads:feature:if-ebio -->

# Implementation Tasks

## DB Batch

- [x] [1.1] Scaffold `apps/ctx-scan/` (`package.json` name `@if/ctx-scan`, `tsconfig.json`, [beads:if-tf2b]
  `bin` entry) using `apps/daily-brief` as the sibling-app template (bun runtime, TypeScript,
  `bun test`), plus `commander` as a dependency for CLI parsing.
- [x] [1.2] Define the schema-versioned data model in `src/model.ts`: `Fleet`, `Project`, [beads:if-b7te]
  `Surface`, and `Node` types per the proposal's data-model requirement, including the 13-class
  `cls` union and a top-level `schemaVersion` constant.

## API Batch

- [x] [2.1] Implement `src/discovery.ts`: walk `--root` (default `~/dev`), identify project [beads:if-ke6q]
  roots (`CLAUDE.md` | `.claude/` | `.mcp.json` present), dedupe to the outermost git root,
  apply the hard-exclusion list at descent time, and guard symlink cycles via a realpath set.
  - depends on: 1.2
- [x] [2.2] Implement `src/settings-resolver.ts`: read `.claude/settings.json`, [beads:if-zffx]
  `.claude/settings.local.json`, `.mcp.json` (+ root `mcp.json`), and the user layer
  (`~/.claude/settings.json`), and resolve the winning layer per key in precedence order
  `managed → CLI → .claude/settings.local.json → .claude/settings.json →
  ~/.claude/settings.json`. A malformed JSON file reports a per-file parse error in the output
  rather than throwing.
  - depends on: 1.2
- [x] [2.3] Implement global-layer identification: resolve `~/.claude` via `realpath` (following [beads:if-t3ne]
  the `~/dev/cc` symlink target where present), scan it once as the `global` origin, and exclude
  it from the discovered-project list even when it structurally matches the discovery predicate.
  - depends on: 2.1

## UI Batch

- [x] [3.1] Wire the `ctx-scan scan [--root <path>] [--json <path>]` commander.js command: [beads:if-oain]
  invoke discovery + settings resolution, assemble the `Fleet` document (still empty per-Node
  `effective_chars`/`truncations`/`bands` at this stage — that logic lands in
  `ctx-scan-assembly`), and write JSON to `--json` or stdout.
  - depends on: 2.1, 2.2, 2.3

## E2E Batch

- [ ] [4.1] Fixture tree under `test/fixtures/` containing vendored marketplace dirs, [beads:if-l383]
  `.worktrees/`, and an `archive/` subdir; assert `ctx-scan scan --json` over the fixture root
  reports zero phantom projects from any of them.
  - depends on: 2.1
- [ ] [4.2] Fixture reproducing the `~/.claude` → `~/dev/cc` symlink shape (or an equivalent [beads:if-kzlk]
  temp-dir symlink); assert the global layer appears exactly once and never as a project entry.
  - depends on: 2.3
- [ ] [4.3] Layered fixture settings files (managed / CLI-simulated / local / project / user); [beads:if-7gyx]
  assert the resolver reports the correct winning layer per key for at least one key overridden
  at every layer.
  - depends on: 2.2
- [ ] [4.4] Snapshot fixture test locking the `Fleet`/`Project`/`Surface`/`Node` JSON shape and [beads:if-linf]
  `schemaVersion`; a shape change without a version bump fails the test.
  - depends on: 1.2
- [ ] [4.5] Timed run of `ctx-scan scan --json` against a realistic multi-project fixture tree; [beads:if-z61j]
  assert warm runtime stays within a documented budget (informs `ctx-scan-assembly`'s < 5s
  fleet-wide acceptance bar).
  - depends on: 3.1
- [ ] [4.6] `tsc --noEmit` and `bun test` both green — wired as this proposal's build/test gate. [beads:if-8lgo]
  - depends on: 3.1
