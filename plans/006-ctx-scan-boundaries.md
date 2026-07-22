# Plan 006 — Confine ctx-scan's filesystem reach to its scan root

**Written against commit:** `d441448` — if excerpts no longer match, STOP and report drift.
**Findings:** #2 (symlink walk escapes `--root`, MED), #3 (`@import` chain resolves `..` outside the project, LOW), #1 (`--probe-hooks` executes untrusted project-local hook commands fleet-wide, MED).
**Priority:** 6 of 8. Depends on plan 001 (ctx-scan `bun test` must be in the gate). Land 001 first.

## Why this matters

`ctx-scan` walks a project fleet under `--root` and reads config files. Three reach-related
gaps let attacker-authored content (a cloned/downloaded repo) pull ctx-scan outside the
intended root or, with `--probe-hooks`, execute code:

1. The discovery walk follows symlinks with only a realpath cycle-guard, no root-containment
   check — a symlink `link -> /` makes ctx-scan stat/discover "projects" outside `--root`.
2. The `@import` chain resolves `@../../../../etc/passwd` and reads it into scan output —
   no containment check on the joined path.
3. `--probe-hooks` runs `bash -c <command>` where `command` comes from project-local
   `.claude/settings.json` across every discovered project. Opt-in, but the warning doesn't
   convey that the command source is untrusted per-project settings.

## Current state (verified excerpts)

`apps/ctx-scan/src/imports.ts:92-96` — no containment on the joined path:

```ts
      for (const rel of importPaths) {
        const abs = join(baseDir, rel);
        if (visited.has(abs)) continue; // cycle guard
        visited.add(abs);
        resolved.push({ path: abs, depth, importedFrom: filePath });
```

`apps/ctx-scan/src/discovery.ts:160-173` — walk descends into any dir-like child; the only
limiter is the `visited` realpath cycle guard; out-of-root candidates survive because
`outermostGitRoot(candidate, rootReal) ?? candidate` falls back to the candidate itself:

```ts
    for (const entry of entries) {
      if (!isDirLike(entry, dir)) continue;
      const name = entry.name;
      if (nameExcluded(name) || pathExcluded(parentName, name)) continue;
      walk(join(dir, name), name);
    }
```

`apps/ctx-scan/src/assembly.ts:393` — `Bun.spawn(["bash", "-c", command], ...)`; `command`
originates from resolved hooks incl. project-local settings (`settings-resolver.ts:126-127`),
run per-project by `buildFleet` (`cli.ts`).

## Conventions to match

- ctx-scan already has `safeRealpath(...)` (used in discovery.ts's walk). Reuse it — do not
  add a new realpath helper. Grep `rg "safeRealpath|rootReal|isWithin" apps/ctx-scan/src/`
  to see what containment helpers already exist before writing one.
- Failures degrade to skip, never throw — that is the file's stated contract (imports.ts
  docblock: "a dangling @import shouldn't crash the scan"). A containment violation should
  be treated the same way: skip + optional stderr warning, not an exception.
- Tests: `apps/ctx-scan/test/*.test.ts`, run by `bun test`. There are existing
  `discovery.test.ts` and `imports-chain.test.ts` — extend those.

## Steps

1. **Add a containment helper** (or reuse one if the grep finds it): `isWithin(child, root)`
   returning true iff `safeRealpath(child)` is `root` or under `root + sep`. Put it wherever
   discovery.ts's path helpers live so both discovery and imports can import it.
   Verify: unit test `isWithin("/a/b/c", "/a/b") === true`, `isWithin("/a/x", "/a/b") === false`,
   symlink-resolving case covered.

2. **Confine the discovery walk (finding #2).** In `discovery.ts`'s `walk`, before recursing
   into a child (`walk(join(dir, name), name)` at :166), skip when the child's realpath is
   not within `rootReal`: `const childReal = safeRealpath(join(dir, name)); if (!childReal || !isWithin(childReal, rootReal)) continue;`
   This stops symlink escapes while the existing `visited` guard still handles cycles.
   Verify: new discovery.test.ts case — a fixture tree with a symlink pointing outside root
   yields zero out-of-root projects.

3. **Confine `@import` resolution (finding #3).** In `imports.ts`, thread the project root
   into `resolveImportChain` (it currently takes only `rootPath` — add a `projectRoot` param,
   or derive the containing root from the first file's dir). Before pushing a resolved
   import (`:94`), skip when `abs` is not within the project root:
   `if (!isWithin(abs, projectRoot)) continue;` — same silent-skip contract. Update the one
   caller (grep `rg "resolveImportChain" apps/ctx-scan/src`) to pass the root.
   Verify: new imports-chain.test.ts case — a CLAUDE.md with `@../../../etc/hosts` resolves
   to zero imports (or is skipped), and a legitimate in-project `@rules/CORE.md` still resolves.

4. **Sharpen the `--probe-hooks` guard (finding #1).** Two-part, smallest safe change:
   - (a) Update the warning text (cli.ts around the `--probe-hooks` help, and the stderr
     warning in `runScan`) to state explicitly: "executes the `hook` shell command from
     each project's own `.claude/settings.json` — only use on roots containing solely
     trusted repositories."
   - (b) Confine which settings layer hooks are probed from: in `assembly.ts`/`settings-resolver.ts`,
     restrict probe-execution to hooks resolved from the **global** `~/.claude` layer by
     default, and require an additional explicit `--probe-project-hooks` flag to also run
     project-local ones. If threading that flag is more than ~30 lines, STOP and instead
     ship just (a) plus a `# ponytail:` note proposing (b) as follow-up — do not build a
     large confirmation UI. Report which option you took.
   Verify: `ctx-scan scan --root <fixture-with-malicious-project-hook> --probe-hooks` does
   NOT execute the project-local hook (only global), or, if you shipped (a)-only, the
   warning text now names the untrusted source.

5. **Run tests:** `cd apps/ctx-scan && bun test` → all pass incl. new cases. `scripts/check.sh` → exit 0.

## Boundaries

- **In scope:** `apps/ctx-scan/src/discovery.ts`, `apps/ctx-scan/src/imports.ts`, `apps/ctx-scan/src/assembly.ts` + `settings-resolver.ts` (probe-hooks confinement only), `apps/ctx-scan/src/cli.ts` (warning text / new flag), and the two test files.
- **Out of scope:** the rubric/truncation/telemetry logic, the render layer, the new `view-model` subcommand (that's `wavetui-context-pane`'s openspec change — do not touch it here), `pipeline.ts` beyond what threading the root requires.
- Do NOT change the default behavior of a plain `scan` (no hooks, no network) — it must stay pure filesystem reads.

## Done criteria (machine-checkable)

- `rg "isWithin" apps/ctx-scan/src/discovery.ts apps/ctx-scan/src/imports.ts` → present in both.
- `cd apps/ctx-scan && bun test` → passes, with new discovery + imports containment cases (grep test output for the new test names).
- Symlink-escape fixture yields zero out-of-root projects; `@../../` import yields zero resolved imports.
- probe-hooks: either the malicious project-hook fixture is not executed, or the warning text contains "each project's own" / "untrusted" — grep cli.ts.
- `scripts/check.sh` → exit 0.

## Test plan

Extend `discovery.test.ts` (symlink escape) and `imports-chain.test.ts` (`..` traversal) —
both files already exist and have the fixture-tree helpers (`test/helpers/tree.ts`). Add a
probe-hooks confinement test only if you shipped the (b) flag; if (a)-only, the warning-text
change needs no test. Follow the existing tests' fixture-tree setup.

## Maintenance note

`isWithin` becomes the canonical containment check — any future filesystem-walking feature
in ctx-scan (e.g. the references shelf, plugin surface discovery) must use it. Note it in
the helper's docblock. The probe-hooks confinement interacts with the `wavetui-context-pane`
change, which explicitly never passes `--probe-hooks` — keep that invariant.

## Escape hatch

- If threading `projectRoot` through `resolveImportChain` touches more than the one caller (unexpected fan-out): STOP, report the call graph, and ship steps 2 + 4 without 3 rather than doing a risky wide refactor.
- If `bun test` is red on clean `d441448`: STOP and report (plan 001 dependency).
