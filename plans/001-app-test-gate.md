# Plan 001 — Wire app test suites into the verification gate

**Written against commit:** `d441448` — if HEAD has moved and any excerpt below no longer matches, STOP and report drift instead of adapting.
**Finding:** No gate runs any app test suite (audit finding #1, HIGH confidence).
**Priority:** 1 of 8 — land this FIRST; it is the safety net for every other plan.

## Why this matters

`scripts/check.sh` is the repo's one verification command, and `scripts/hooks/pre-commit`
blocks commits on it. But check.sh only covers the shell/dotfiles layer (zsh syntax, sh
syntax, chezmoi template render, shellcheck at error severity, terraform validate). The
apps/ layer carries real test suites that **nothing executes automatically**:

- `apps/wavetui` — ~40 `_test.go` files, run by `go test ./...`
- `apps/ctx-scan` — ~38 `test/*.test.ts` files, run by `bun test`
- `apps/daily-brief` — 3 test files in `test/`, run by `bun test` (its package.json has NO test script)
- `apps/cc-tmux` — a built-in suite run by `cc-tmux self-test` (see `apps/cc-tmux/src/cc_tmux/testing.py:1-9`)

Worse: `scripts/hooks/post-commit` and `scripts/hooks/wavetui-build.sh` run
`go install ./cmd/wavetui` on commit — so a commit with red Go tests still deploys a
possibly-broken wavetui binary onto the machines.

## Current state (verified excerpts)

`scripts/check.sh:169-175` (the whole gate — five sections, no apps):

```bash
info "check.sh — verifying repo (root: $ROOT)"
section_zsh
section_sh
section_template
section_shellcheck
section_terraform
```

`scripts/hooks/pre-commit:20-28` (check.sh blocks the commit; nothing else tests):

```bash
# === 1. check.sh baseline (blocks commit on failure; if-ev7) ===
# NOTE: no `|| true` — a failing check MUST block the commit. ~2.3s warm.
if [ -n "$REPO_ROOT" ] && [ -x "$REPO_ROOT/scripts/check.sh" ]; then
    if ! "$REPO_ROOT/scripts/check.sh" >/tmp/if-precommit-check.$$ 2>&1; then
```

## Conventions to match

check.sh's own header (`scripts/check.sh:9-10`) states the design rules you MUST keep:

- **Intentionally NOT `set -e`** — every section runs and reports; one failure must not hide the rest. It uses `set -uo pipefail` and a `FAIL=1` accumulator.
- **Sections whose tool is absent are skipped with a warning** — see how `section_terraform` guards on the tool existing before running. Follow that exact pattern: `command -v go >/dev/null 2>&1 || { info "SKIP: go not installed"; return; }`.
- Log helpers `info`/`success`/`error` come from `scripts/utils.sh` (sourced at check.sh:31-36). Use them; do not invent new output formatting.
- Each section prints `PASS: <name>` via `success` or `FAIL: <name>` via `error` and sets `FAIL=1`, with tool output indented via `sed 's/^/    /'` on failure (see the terraform section at check.sh:160-166 as the exemplar).

## Steps

1. **Add a `section_apps_go` function to `scripts/check.sh`** (place it after `section_terraform`'s definition, before the invocation block at :169):
   - Guard: skip with `info "SKIP: go not installed (apps/wavetui untested)"` if `command -v go` fails.
   - Run: `(cd "$ROOT/apps/wavetui" && go test ./... >"$TMP_ERR" 2>&1)`.
   - On failure: `error "FAIL: wavetui go test"`, indent output, `FAIL=1`. On success: `success "PASS: wavetui go test"`.
   - Verify: `bash -n scripts/check.sh` exits 0.

2. **Add a `section_apps_bun` function** covering both Bun apps:
   - Guard on `command -v bun`.
   - Run `bun test` in `apps/ctx-scan` and `apps/daily-brief` as two separately-reported results (`PASS: ctx-scan bun test`, `PASS: daily-brief bun test`), same TMP_ERR/indent pattern.
   - Verify: `bash -n scripts/check.sh`.

3. **Add a `section_apps_cctmux` function**:
   - Guard on `command -v cc-tmux` (fall back to skip-with-warning if not installed — do NOT try to invoke it via python directly).
   - Run `cc-tmux self-test`, same reporting pattern.
   - Verify: `bash -n scripts/check.sh`.

4. **Register the three sections** in the invocation block after `section_terraform` (check.sh:169-175). Order: go, bun, cc-tmux (slowest last).

5. **Add a `test` script to `apps/daily-brief/package.json`**: `"test": "bun test"` in the `scripts` object (it currently has none — verify by reading the file first). This makes step 2's suite also discoverable standalone.

6. **Add an aggregate root target**: in the root `package.json` (currently only `check` and `tf` scripts), add `"test": "./scripts/check.sh"`. Do NOT create a Makefile/justfile — the repo's convention is npm scripts fronting shell scripts.

7. **Run the full gate**: `scripts/check.sh` from the repo root. Expected: all existing sections still PASS, plus three new PASS lines (or SKIP warnings on machines missing a toolchain). Exit code 0.

8. **Timing check**: `time scripts/check.sh`. The pre-commit comment says "~2.3s warm" — the new sections will add real time (go test ~5-20s, bun tests a few seconds). If total exceeds ~60s, STOP and report back with the measured breakdown instead of shipping a slow commit gate (see escape hatches).

## Boundaries

- **In scope:** `scripts/check.sh`, `apps/daily-brief/package.json` (scripts block only), root `package.json` (scripts block only).
- **Out of scope:** `scripts/hooks/pre-commit` (it already calls check.sh — no change needed), `scripts/hooks/post-commit`, `wavetui-build.sh`, any test file, any app source code, `.github/` (do NOT introduce CI — this repo deliberately uses hooks).
- Do not fix failing tests you encounter — report them (escape hatch below).

## Done criteria (machine-checkable)

- `scripts/check.sh; echo $?` → prints `PASS` lines for wavetui/ctx-scan/daily-brief/cc-tmux sections (or explicit SKIPs), exits 0.
- `grep -c "section_apps" scripts/check.sh` → ≥ 6 (3 definitions + 3 invocations).
- `cd apps/daily-brief && bun run test` → runs the 3 test files (no "missing script" error).
- `npm run test` at repo root → runs check.sh.
- Intentionally break a Go test locally (do NOT commit), run `scripts/check.sh` → exits 1 with `FAIL: wavetui go test`. Revert.

## Test plan

The gate is itself the test infrastructure; the verification above (deliberate red test → gate fails) is the required negative test. No new test files.

## Maintenance note

Any new app under `apps/` with a test suite needs a section here — note that in the check.sh header comment (update the comment at :4-7 to mention app tests). Plans 005/006/007 rely on this gate being live.

## Escape hatches

- If `go test ./...` fails on a clean checkout at `d441448`: STOP, report the failing package/test verbatim. Do not fix or skip it.
- If `cc-tmux` is not installed on the machine running the gate and there's no obvious install path: ship the section with the skip-warning guard (that IS the repo pattern) and note it in your report.
- If total gate time exceeds ~60s: STOP and report timings; the maintainer may want app tests in pre-push rather than pre-commit — that is their call, not yours.
