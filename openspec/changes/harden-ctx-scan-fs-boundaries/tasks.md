---
stack: t3
---

<!-- owner: homelab-specialist — this repo's non-T3-stack convention, see rules/PATTERNS.md -->

# Implementation Tasks

## API Batch

- [x] [1.1] Export `discovery.ts`'s existing `isWithin(child, root)` containment helper (already
  defined at `discovery.ts:78`, currently private) so `imports.ts` can import it. Do not add a
  second implementation — grep `rg "safeRealpath|rootReal|isWithin" apps/ctx-scan/src/` first to
  confirm reuse, per plans/006-ctx-scan-boundaries.md step 1. Add/extend a unit test:
  `isWithin("/a/b/c", "/a/b") === true`, `isWithin("/a/x", "/a/b") === false`. [type:security]
- [x] [1.2] Confine `discovery.ts`'s `walk`: before recursing into a child
  (`walk(join(dir, name), name)`), skip when the child's realpath is not within `rootReal`
  (`const childReal = safeRealpath(join(dir, name)); if (!childReal || !isWithin(childReal, rootReal)) continue;`).
  The existing `visited` cycle guard is unaffected. Per plans/006-ctx-scan-boundaries.md step 2.
  - depends on: 1.1
  [type:security]
- [x] [1.3] Confine `imports.ts`'s `resolveImportChain`: add a `projectRoot` parameter and skip
  (never push, never follow) any resolved import whose absolute path is not within `projectRoot`
  (same silent-skip contract as the existing dangling-import handling). Update the caller family
  (`calibrate.ts`, `pipeline.ts`, `refs.ts` — grep `rg "resolveImportChain" apps/ctx-scan/src`) to
  pass the root. If threading the parameter fans out to more than these known call sites, STOP
  and report the call graph rather than doing a wide refactor (source plan's escape hatch). Per
  plans/006-ctx-scan-boundaries.md step 3.
  - depends on: 1.1
  [type:security]
  - FIXED (orchestrator, wave 2 post-wave review MUST finding): the original implementation's
    containment check compared the raw lexical path only, never resolving symlinks — an
    in-project symlink whose lexical path is inside `projectRoot` but whose TARGET is outside
    it would pass the check and get `readFileSync`'d on the next hop, leaking arbitrary file
    content. Fixed by resolving both `abs` and `projectRoot` via `safeRealpath` (exported from
    `discovery.ts`) before the `isWithin` comparison, matching `isWithin`'s own doc contract
    ("callers are responsible for resolving symlinks"). Regression test added in
    `imports-chain.test.ts` — confirmed passing (3/3 in that file, 107/107 repo-wide).
- [x] [1.4] Sharpen the `--probe-hooks` warning text in `cli.ts` (both the option help and the
  runtime stderr warning in `runScan`) to explicitly name project-local `.claude/settings.json`
  as the untrusted per-project command source, and advise using the flag only on roots containing
  solely trusted repositories. Optionally (smallest-safe-change escape hatch): restrict
  `assembly.ts`/`settings-resolver.ts` probe-execution to hooks resolved from the global
  `~/.claude` layer by default, requiring an additional explicit `--probe-project-hooks` flag for
  project-local hooks. If threading that flag is more than ~30 lines, ship the warning-text
  change only and note the restriction as a follow-up in the PR/commit description — do not build
  a large confirmation UI. Report which option was taken. Per
  plans/006-ctx-scan-boundaries.md step 4. [type:security]

## E2E Batch

- [x] [2.1] Extend `apps/ctx-scan/test/discovery.test.ts` with a symlink-escape fixture case
  (symlink pointing outside `--root` yields zero out-of-root discovered projects) and
  `apps/ctx-scan/test/imports-chain.test.ts` with a traversal case (`@../../../etc/hosts`-style
  import resolves to zero out-of-root imports, while an in-project `@import` still resolves).
  Follow the existing tests' fixture-tree helpers (`test/helpers/tree.ts`). Add a probe-hooks
  confinement test only if the optional restriction (1.4's part b) shipped — if warning-text-only
  shipped, no new test is needed for that half. Per plans/006-ctx-scan-boundaries.md's Test plan.
  - depends on: 1.2, 1.3, 1.4
  [type:test]
- [x] [2.2] Run `cd apps/ctx-scan && bun test` — confirm all pass, including the new containment
  cases (grep test output for the new test names). Run `scripts/check.sh` from the repo root —
  confirm exit 0. If `bun test` is already red on a clean checkout before any of these changes,
  STOP and report (this proposal depends on `add-repo-app-test-gate` landing first, per
  plans/006-ctx-scan-boundaries.md's escape hatch). Paste terminal output as evidence.
  - depends on: 2.1
  [type:test]
