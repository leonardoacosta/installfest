---
order: 0722g
---

# Proposal: add-repo-app-test-gate — wire app test suites into the repo verification gate

## Change ID
`add-repo-app-test-gate`

## Summary
Add three new sections to `scripts/check.sh` — `section_apps_go`, `section_apps_bun`, and
`section_apps_cctmux` — that run `apps/wavetui`'s Go tests, `apps/ctx-scan` and
`apps/daily-brief`'s Bun tests, and `cc-tmux self-test`, each guarded to skip with a warning
when its toolchain is absent. Also add the missing `test` script to `apps/daily-brief/package.json`
and a `test` script to the root `package.json` that fronts the full gate.

## Why
`scripts/check.sh` is the repo's one verification command, and `scripts/hooks/pre-commit` blocks
commits on it — but check.sh only covers the shell/dotfiles layer (zsh syntax, sh syntax, chezmoi
template render, shellcheck at error severity, terraform validate). The `apps/` layer carries real
test suites that nothing executes automatically: `apps/wavetui` (~40 `_test.go` files), `apps/ctx-scan`
(~38 `test/*.test.ts` files), `apps/daily-brief` (3 test files, no `test` script even to run them
manually), and `apps/cc-tmux` (a built-in `cc-tmux self-test` suite). Worse, `scripts/hooks/post-commit`
and `scripts/hooks/wavetui-build.sh` run `go install ./cmd/wavetui` on every commit — so a commit
with red Go tests still deploys a possibly-broken wavetui binary onto the machines. This is the
root finding of a read-only `/improve` audit (`plans/001-app-test-gate.md`, priority 1 of 8) and
the prerequisite gate for three follow-on plans (005 cc-tmux load-buffer seeding, 006 ctx-scan scan
boundaries, 007 daily-brief hardening) that all need their respective app's test suite live in this
gate before their changes can be regression-checked by it.

## What Changes
- `scripts/check.sh`: add `section_apps_go` (guards on `command -v go`, runs `go test ./...` in
  `apps/wavetui`, reports `PASS`/`FAIL: wavetui go test`), `section_apps_bun` (guards on
  `command -v bun`, runs `bun test` in `apps/ctx-scan` and `apps/daily-brief` as two separately
  reported results), and `section_apps_cctmux` (guards on `command -v cc-tmux`, runs
  `cc-tmux self-test`) — each following the exact `section_terraform` skip-with-warning /
  indent-output-on-failure pattern already established in the file. Register all three in the
  invocation block after `section_terraform`, ordered go → bun → cc-tmux (slowest last).
- `apps/daily-brief/package.json`: add `"test": "bun test"` to the `scripts` object (currently has
  none), making its 3-test suite discoverable standalone.
- Root `package.json`: add `"test": "./scripts/check.sh"` to the `scripts` object (currently only
  `check` and `tf`), so `npm run test` invokes the full gate.

## Context
- touches: `scripts/check.sh`, `apps/daily-brief/package.json`, `package.json`

## Testing
| Affected seam | Task |
|----------------|------|
| `section_apps_go`/`section_apps_bun`/`section_apps_cctmux` syntax | `[2.2]` `bash -n scripts/check.sh` |
| Full gate runs and reports PASS/SKIP per app | `[2.1]` run `scripts/check.sh`, confirm PASS/SKIP lines and exit 0 |
| A red Go test actually fails the gate (not silently skipped) | `[2.2]` deliberately break a Go test locally, confirm `FAIL: wavetui go test` and exit 1, then revert (never commit the broken test) |
| `apps/daily-brief` test script | `[1.5]`, verified in `[2.1]` via `cd apps/daily-brief && bun run test` |
| root `test` script | `[1.6]`, verified in `[2.1]` via `npm run test` |

## Done Means
- Running `scripts/check.sh` reports PASS or FAIL for wavetui go test, ctx-scan bun test, and
  daily-brief bun test, plus PASS or an explicit SKIP-with-warning for cc-tmux self-test when the
  `cc-tmux` binary is absent — never a silent gap.
- `npm run test` at the repo root invokes the full gate (`scripts/check.sh`) and exits 0 when
  everything passes.
- `cd apps/daily-brief && bun run test` runs its 3 test files instead of failing with "missing
  script".
- A deliberately broken Go test in `apps/wavetui` makes `scripts/check.sh` exit 1 with
  `FAIL: wavetui go test`, proving the gate actually blocks — not just reports.
- `scripts/hooks/pre-commit` (unchanged, already calls `check.sh`) now blocks a commit that would
  otherwise ship a red app test suite.
