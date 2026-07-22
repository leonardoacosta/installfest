# daily-brief

A Bun/TypeScript + `ink` terminal widget that collects and renders a daily
snapshot — meetings, radar/triage items, open-items, doc-hygiene state — from
mx-gateway and other fleet sources, each source degrading independently
(fail-open) rather than aborting the whole collection run.

Full behavior contract lives in `openspec/specs/daily-brief/spec.md` — this
file only covers how to run and test it.

## CLI

```bash
bun run src/index.tsx collect   # write a dated snapshot + latest.json pointer
bun run src/index.tsx view      # render the ink widget from the latest snapshot
```

`bin: daily-brief -> src/index.tsx`. Package scripts: `collect` and `view`
(see `package.json`).

## Test

```bash
cd apps/daily-brief
bun test
```

Also wired into the repo-wide gate as part of `section_apps_bun` in
`scripts/check.sh` (skipped with a warning if `bun` isn't installed).

## Layout

```
apps/daily-brief/
  src/index.tsx        # entrypoint (collect/view subcommands)
  src/collect.ts        # snapshot collection
  src/sources/          # mx-gateway, open-items, doc-hygiene source adapters
  src/ui/                # ink widget components
  test/                  # unit specs (bun test)
```
