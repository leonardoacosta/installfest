---
order: 0722l
---

# Proposal: harden-ctx-scan-fs-boundaries — confine ctx-scan's filesystem reach to its scan root

## Change ID
`harden-ctx-scan-fs-boundaries`

## Why
`ctx-scan` walks a project fleet under `--root` and reads config files. Three reach-related gaps
let attacker-authored content (a cloned/downloaded repo) pull ctx-scan outside the intended root
or, with `--probe-hooks`, execute code:

1. The discovery walk (`discovery.ts`'s `walk`) follows symlinks with only a realpath
   cycle-guard, no root-containment check — a symlink pointing at `/` or a sibling directory
   makes ctx-scan stat/discover "projects" outside `--root`.
2. `@import` resolution (`imports.ts`'s `resolveImportChain`) joins an import token onto its
   containing file's directory with no containment check on the joined path — a CLAUDE.md
   containing `@../../../../etc/passwd` resolves and reads a path outside the project entirely.
3. `--probe-hooks` runs `bash -c <command>` (`assembly.ts`'s `probeHookStdoutBytes`) where
   `command` comes from resolved hooks including project-local `.claude/settings.json`
   (`settings-resolver.ts`), executed per-project fleet-wide by `buildFleet` (`cli.ts`). Opt-in,
   but the current warning text doesn't convey that the command source is untrusted per-project
   settings, not ctx-scan's own trusted config.

## What Changes
- Export `discovery.ts`'s existing `isWithin(child, root)` containment helper (it already exists
  as a private function at `discovery.ts:78` — reuse it verbatim per the source plan's grep
  finding, do not add a second implementation) so `imports.ts` can import it too.
- Confine `discovery.ts`'s walk: before recursing into a child directory, skip it (do not walk
  further, do not add it as a candidate) when its realpath is not within the root's realpath.
  The existing `visited` cycle guard is unaffected — this is an additional containment check,
  not a replacement.
- Confine `imports.ts`'s `resolveImportChain`: thread a `projectRoot` parameter through and skip
  (never push, never follow further) any resolved import whose absolute path is not within
  `projectRoot`. Update the one caller family (`calibrate.ts`, `pipeline.ts`, `refs.ts` all call
  `resolveImportChain` — verify at implementation time whether one shared call site can be
  updated or whether each needs its own root argument).
- Both containment checks degrade to silent-skip, never throw — matching `imports.ts`'s existing
  stated contract ("a dangling @import shouldn't crash the scan") and `discovery.ts`'s existing
  fail-open behavior on unreadable directories.
- Sharpen the `--probe-hooks` warning text (cli.ts's option help and `runScan`'s stderr warning)
  to explicitly name the untrusted source: "executes the `hook` shell command from each
  discovered project's own `.claude/settings.json` — only use on a root containing solely
  trusted repositories." Optionally (smallest-safe-change escape hatch, see source plan): restrict
  probed hooks to the global `~/.claude` settings layer by default, requiring an additional
  explicit flag to also probe project-local hooks. If threading that flag touches more than the
  probe call site plus its one caller, ship the warning-text change only and report the
  restriction as a follow-up.

## Context
- depends on: `add-repo-app-test-gate`
- touches: `apps/ctx-scan/src/discovery.ts`, `apps/ctx-scan/src/imports.ts`,
  `apps/ctx-scan/src/assembly.ts`, `apps/ctx-scan/src/settings-resolver.ts`,
  `apps/ctx-scan/src/cli.ts`
- Out of scope: the `view-model` subcommand and anything else belonging to the in-flight
  `wavetui-context-pane` change (`openspec/changes/wavetui-context-pane/`) — that change
  explicitly never passes `--probe-hooks`, and this proposal does not touch its files.
- Source: `plans/006-ctx-scan-boundaries.md` (written against commit `d441448`; findings #1-#3,
  priority 6 of 8).
- Capability Preflight: not applicable — local dev tool, no hosting/deploy component, same
  precedent `wavetui-context-pane`/`harden-open-core-applescript-injection` cite (`stack: t3`-as-
  placeholder per `rules/PATTERNS.md`).

## Testing
Maps to extended cases in the existing `apps/ctx-scan/test/discovery.test.ts` (a fixture tree
with a symlink pointing outside the scan root yields zero out-of-root discovered projects) and
`apps/ctx-scan/test/imports-chain.test.ts` (a CLAUDE.md containing `@../../../etc/hosts`
resolves to zero out-of-root imports, while a legitimate in-project `@rules/CORE.md`-style import
still resolves). A probe-hooks confinement test is added only if the optional global-only
restriction ships; if warning-text-only ships, that change needs no new test — grep `cli.ts` for
the updated wording instead.

## Done Means
- A symlink pointing outside `--root` is never discovered as a project.
- An `@import` chain containing `../` traversal resolves to zero imports outside the project
  root (or those imports are silently skipped, never read into scan output).
- Neither containment check throws — both degrade to silent-skip, matching the files' existing
  fail-open contracts.
- The `--probe-hooks` warning text explicitly names project-local `.claude/settings.json` as the
  untrusted per-project command source (or, if the optional restriction shipped, project-local
  hooks require an explicit additional flag beyond `--probe-hooks` alone).
- A plain `scan` with no flags still performs pure filesystem reads only — no behavior change to
  the default path.
- `cd apps/ctx-scan && bun test` passes, including the new containment test cases.
- `scripts/check.sh` exits 0.
