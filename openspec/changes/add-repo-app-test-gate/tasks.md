---
stack: t3
---

<!-- owner: homelab-specialist — this repo's non-T3-stack convention, see rules/PATTERNS.md -->

# Implementation Tasks

## API Batch

- [ ] [1.1] Add `section_apps_go` to `scripts/check.sh` (placed after `section_terraform`'s
  definition, before the invocation block): guard on `command -v go >/dev/null 2>&1 ||`
  skip-with-warning (`"SKIP: go not installed (apps/wavetui untested)"`); run
  `(cd "$ROOT/apps/wavetui" && go test ./... >"$TMP_ERR" 2>&1)`; report `PASS: wavetui go test` /
  `FAIL: wavetui go test` (indented output, `FAIL=1`) per plans/001-app-test-gate.md step 1.
  [type:config]
- [ ] [1.2] Add `section_apps_bun` to `scripts/check.sh`: guard on `command -v bun`; run
  `bun test` in `apps/ctx-scan` and `apps/daily-brief` as two separately reported results
  (`PASS: ctx-scan bun test`, `PASS: daily-brief bun test`), same `TMP_ERR`/indent/`FAIL=1`
  pattern as `section_apps_go`, per plans/001-app-test-gate.md step 2. [type:config]
- [ ] [1.3] Add `section_apps_cctmux` to `scripts/check.sh`: guard on `command -v cc-tmux`
  (skip-with-warning if absent — do NOT invoke it via python directly); run `cc-tmux self-test`,
  same reporting pattern; per plans/001-app-test-gate.md step 3. [type:config]
- [ ] [1.4] Register all three new sections in the invocation block after `section_terraform`,
  ordered `section_apps_go` → `section_apps_bun` → `section_apps_cctmux` (slowest last), per
  plans/001-app-test-gate.md step 4. [type:config]
- [ ] [1.5] Add `"test": "bun test"` to the `scripts` object in `apps/daily-brief/package.json`
  (currently has none), making its 3-test suite discoverable standalone, per
  plans/001-app-test-gate.md step 5. [type:config]
- [ ] [1.6] Add `"test": "./scripts/check.sh"` to the `scripts` object in the root `package.json`
  (currently only `check` and `tf`) so `npm run test` invokes the full gate — do not create a
  Makefile/justfile, per plans/001-app-test-gate.md step 6. [type:config]

## E2E Batch

- [ ] [2.1] Run the full gate: `scripts/check.sh` from the repo root. Confirm all pre-existing
  sections still PASS, plus PASS lines (or explicit SKIP warnings on a machine missing a
  toolchain) for wavetui go test, ctx-scan bun test, daily-brief bun test, and cc-tmux self-test.
  Confirm exit code 0. Also confirm `cd apps/daily-brief && bun run test` runs its 3 test files
  and `npm run test` at repo root invokes the gate. Paste terminal output as evidence per
  plans/001-app-test-gate.md step 7 and the Done criteria. [type:test]
- [ ] [2.2] Deliberate-red-test negative check: intentionally break a Go test in `apps/wavetui`
  locally (do NOT commit it), run `scripts/check.sh`, confirm it exits 1 with
  `FAIL: wavetui go test`, then revert the break — per plans/001-app-test-gate.md step 8 / Done
  criteria and this proposal's `repo-verification` spec's "a failing app test suite fails the
  gate" scenario. [type:test]
