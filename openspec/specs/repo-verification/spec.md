# repo-verification Specification

## Purpose
TBD - created by archiving change add-repo-app-test-gate. Update Purpose after archive.
## Requirements
### Requirement: check.sh runs every app's test suite
`scripts/check.sh` SHALL run `go test ./...` in `apps/wavetui`, `bun test` in `apps/ctx-scan`, and
`bun test` in `apps/daily-brief`, and SHALL invoke `cc-tmux self-test`, reporting a `PASS: <name>`
or `FAIL: <name>` line per app (matching the `success`/`error` reporting convention already used
by every existing section) with any failing tool's output indented on failure. Each check SHALL
be skipped with an explicit `SKIP: <reason>` warning — never silently omitted and never failing
the gate — when its required toolchain (`go`, `bun`, or `cc-tmux`) is not present on `PATH`. A
failure in any one app section SHALL NOT prevent the remaining sections from running (the file is
intentionally not `set -e`), and any app-section failure SHALL set the same `FAIL=1` accumulator
that determines the script's final exit code.

#### Scenario: all suites pass
- Given: `go`, `bun`, and `cc-tmux` are all installed, and every app's tests pass
- When: `scripts/check.sh` runs
- Then: it prints `PASS: wavetui go test`, `PASS: ctx-scan bun test`, `PASS: daily-brief bun test`,
  and `PASS: cc-tmux self-test`, alongside the pre-existing five sections, and exits 0

#### Scenario: a missing toolchain skips with a warning, not a failure
- Given: `cc-tmux` is not installed on the machine running the gate
- When: `scripts/check.sh` runs
- Then: it prints a `SKIP: cc-tmux not installed` warning for that section, does not set
  `FAIL=1` on account of the missing toolchain, and the overall exit code is unaffected by that
  section alone

#### Scenario: a failing app test suite fails the gate
- Given: `apps/wavetui` has a failing Go test
- When: `scripts/check.sh` runs
- Then: it prints `FAIL: wavetui go test` with the indented `go test` output, sets `FAIL=1`, still
  runs every other section, and the script's final exit code is 1

