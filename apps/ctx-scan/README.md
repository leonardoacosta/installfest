# ctx-scan

A Bun/TypeScript scanner that walks a fleet of project roots (default `~/dev`)
and produces a JSON snapshot used by other tools in this repo (e.g. wavetui,
daily-brief) to reason about project context health — file discovery,
imports, refs, and rubric scoring.

Full behavior contract lives in `openspec/specs/ctx-scan/spec.md` — this file
only covers how to run and test it.

## CLI

```
ctx-scan scan [--root <path>] [--json <path>]
```

`bin: ctx-scan -> src/cli.ts`, built on `commander`. Default root is `~/dev`;
default output is stdout.

## Test

```bash
cd apps/ctx-scan
bun test
```

Also wired into the repo-wide gate as part of `section_apps_bun` in
`scripts/check.sh` (skipped with a warning if `bun` isn't installed).

## Layout

```
apps/ctx-scan/
  src/cli.ts          # commander entrypoint
  src/                # discovery, imports, refs, rubric, render, pipeline, ...
  test/                # unit + e2e specs (bun test)
```
