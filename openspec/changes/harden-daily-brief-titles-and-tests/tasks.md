---
stack: t3
---

<!-- owner: homelab-specialist — this repo's non-T3-stack convention, see rules/PATTERNS.md -->

# Implementation Tasks

## API Batch

- [ ] [1.1] Add a `stripControlChars(s: string): string` helper to
  `apps/daily-brief/src/ui/format.ts` (beside the other shared, framework-free formatting
  functions) — strips C0 (`\x00-\x1F`, including tab) and C1 (`\x7F-\x9F`) control characters,
  e.g. `s.replace(/[\x00-\x1F\x7F-\x9F]/g, "")`. Pure function, no I/O. Per
  `plans/007-daily-brief-hardening-and-tests.md` step 1. [type:security]
- [ ] [1.2] Apply `stripControlChars` at a single choke point covering both render paths.
  Preferred: inside `ui/format.ts`'s own row-building functions (`formatMeetingsSummary`'s
  `eventLines` title/location, `toRadarRows`, `formatOpenItemsRepos`) since that module is
  already the shared, framework-free layer both `plainRender.ts` and the ink render path
  (`ui/sections.tsx`) consume for every title (per `format.ts`'s own header comment). If a
  source/collect-layer choke point (`src/sources/mx.ts` or `collect.ts`) turns out cleaner
  during implementation, that is an acceptable alternative — note whichever choice is made in
  the task-completion evidence. Escape hatch: if titles are already sanitized somewhere
  upstream (check `collect.ts`/`mx.ts` first), report that and skip adding a redundant second
  strip — add only the tests in the E2E batch below. Per
  `plans/007-daily-brief-hardening-and-tests.md` step 2.
  - depends on: 1.1
  - [type:security]
- [ ] [1.3] Add/confirm `"test": "bun test"` in `apps/daily-brief/package.json`'s `scripts`
  object. The sibling proposal `add-repo-app-test-gate` may already add this line — check
  `apps/daily-brief/package.json` first; if the script is already present, this task is a
  confirm-only no-op (note that in the task-completion evidence rather than re-adding it). Per
  `plans/007-daily-brief-hardening-and-tests.md` step 3. [type:config]

## E2E Batch

- [ ] [2.1] Add `apps/daily-brief/test/plainRender.test.ts` (renders a snapshot fixture through
  `renderPlainSnapshot` and asserts on output shape, plus the required security-regression case:
  a title fixture containing `\x1b[` renders with the escape removed) and
  `apps/daily-brief/test/format.test.ts` (`stripControlChars` unit cases: empty, plain, ANSI
  escape, C1 control char, tab). Follow `openItems.test.ts`'s import-and-assert style. Per
  `plans/007-daily-brief-hardening-and-tests.md` step 4 / Test plan. [type:test]
- [ ] [2.2] Add `apps/daily-brief/test/docsState.test.ts` covering `src/sources/docsState.ts`'s
  main transform (`collectDocsState`) — missing-file fail-open, stale-threshold detection, and
  the hygiene/sweep entry shapes — following `collect.test.ts`'s style (env-override paths,
  scratch fixture files, never touching real `~/.local/state`/`~/.claude/state`). Skip
  `widgetOpen.ts` and `index.tsx` — both are wiring/entry-point glue per
  `plans/007-daily-brief-hardening-and-tests.md`'s own guidance (don't write brittle render/
  process-spawn tests for pure glue); note this explicitly in the task-completion evidence
  rather than silently omitting it. [type:test]
- [ ] [2.3] Run `cd apps/daily-brief && bun test` (all pass, including the new
  `plainRender.test.ts`, `format.test.ts`, `docsState.test.ts`) and `bun run test` (confirms the
  `test` script from [1.3] actually invokes the suite). Then run `scripts/check.sh` from the
  repo root (exit 0). Paste terminal output as evidence per
  `plans/007-daily-brief-hardening-and-tests.md`'s Done criteria. If `bun test` is red on a
  clean checkout before any of this proposal's changes, STOP and report — that is the
  `add-repo-app-test-gate`/plan-001 dependency surfacing, not something to work around here.
  [type:test]
  - depends on: 2.1, 2.2
