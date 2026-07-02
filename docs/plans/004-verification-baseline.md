# Plan 004: One-command verification baseline (scripts/check.sh) for shell, templates, and terraform

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `docs/plans/README.md`.
>
> **Drift check (run first)**: `git diff --stat 2068bad..HEAD -- package.json platform/homebrew/Brewfile scripts/install-arch.sh`
> Also confirm `scripts/check.sh` does not already exist. On mismatch, STOP.

## Status

- **Priority**: P1
- **Effort**: M
- **Risk**: LOW
- **Depends on**: none (plans 005+ depend on THIS)
- **Category**: tests
- **Planned at**: commit `2068bad`, 2026-07-02

## Why this matters

This repo's entire purpose is bootstrapping machines, and the failure it
guards against — a broken `chezmoi apply` on a fresh machine — is exactly what
nothing verifies. There is no CI (`.github/` absent), no test suite, and
`package.json` has a single script (`tf`). A zsh syntax slip, a Go-template
error in any of the 14 `home/run_*.sh.tmpl` scripts, or a shellcheck-grade bug
in `scripts/` is discovered only at apply time on a real machine — possibly
the new machine, after the old one is gone. This plan adds one command that
answers "is the repo healthy?" and becomes the gate every later refactor
(plans 005+) runs before and after.

## Current state

- `package.json` (root): `{"scripts": {"tf": "./infra/scripts/tf.sh"}}` — the
  only script. No Makefile.
- Shell inventory: ~24 `scripts/*.sh` + `scripts/hooks/*` + `scripts/homelab/`,
  `home/dot_zsh/**/*.zsh`, `home/dot_zshrc`, ~14 `home/run_*.sh.tmpl`
  (chezmoi Go templates that render to bash), `home/dot_local/bin/executable_*`
  (mixed sh/bash), `.githooks/pre-commit`, `ssh-mesh/scripts/*.sh`.
- Tooling present on this machine: `shellcheck` at `~/.local/bin/shellcheck`,
  `chezmoi` at `/usr/bin/chezmoi`, `zsh` at `/usr/bin/zsh`. NOTE: shellcheck
  is NOT declared in `platform/homebrew/Brewfile` nor installed by
  `scripts/install-arch.sh` (verified by grep) — a fresh machine would lack it.
- Template rendering: `chezmoi execute-template < file` renders a template
  with the real machine context (`.chezmoiroot` is `home/`, so chezmoi
  variables like `{{ .chezmoi.workingTree }}` resolve). Render failures exit
  non-zero.
- `infra/`: Cloudflare terraform (`infra/environments/prod/`,
  `infra/modules/cloudflare/`). `terraform validate` requires an initialized
  working dir — `.terraform/` exists locally but not on fresh clones, so the
  check must be conditional.
- Repo conventions: executed-only scripts use `set -euo pipefail` file-scope;
  utility scripts source `scripts/utils.sh` for colored `info/warning/error`
  helpers (see `scripts/utils.sh`, fan-in 22 — reuse it, don't re-implement
  colors).

## Commands you will need

| Purpose | Command | Expected on success |
|---------|---------|---------------------|
| The new gate | `scripts/check.sh` | exit 0, per-section PASS lines |
| Via npm | `npm run check` (or `pnpm run check`) | same |
| Syntax of the gate itself | `bash -n scripts/check.sh && shellcheck scripts/check.sh` | exit 0 |

## Scope

**In scope**:
- `scripts/check.sh` (create)
- `package.json` (add `"check": "./scripts/check.sh"`)
- `platform/homebrew/Brewfile` (add one line: `brew "shellcheck"`)
- `scripts/install-arch.sh` (add `shellcheck` to its package list)
- `README.md` (3-line "Verifying" subsection pointing at the command)

**Out of scope**:
- CI workflow files (`.github/`) — this repo has no CI by choice today;
  adding it is a separate decision.
- Fixing findings the new checks surface in OTHER files. Expect shellcheck to
  flag existing scripts — record the count, do NOT fix them here (that is
  plans 002/005+ territory). The gate must therefore start in a mode that
  passes on the current tree (see Step 2).
- `.githooks` / hook wiring (plan 003 owns that; wiring check.sh into
  pre-commit is a later follow-up).

## Git workflow

- Current branch; conventional commit, e.g.
  `feat(scripts): add check.sh verification baseline (zsh -n, template render, shellcheck, tf validate)`.
- Do NOT push unless instructed.

## Steps

### Step 1: Create scripts/check.sh skeleton

Bash, `set -uo pipefail` (NOT `-e` — the script must run ALL sections and
report, aborting on first failure would hide the rest), source
`scripts/utils.sh` for log helpers. Track a global `FAIL=0`; each section sets
it on failure and prints `PASS: <section>` / `FAIL: <section> (<detail>)`.
Exit `$FAIL`-derived code at the end (0 all pass, 1 any fail). Resolve repo
root via `git rev-parse --show-toplevel` and `cd` there.

Sections, each skippable-with-warning when its tool is absent
(`command -v X || { warning "skip: X not installed"; }`):

1. **zsh-syntax**: `zsh -n` over `home/dot_zshrc` and every `home/dot_zsh/**/*.zsh`.
2. **sh-syntax**: `bash -n` over every `*.sh` under `scripts/`, `ssh-mesh/scripts/`
   (exclude `ssh-mesh/scripts/remote/cmux-bridge/`), `platform/*.sh`, plus
   `sh -n` for `#!/bin/sh` files under `scripts/hooks/`.
3. **template-render**: for every `home/*.tmpl` and `home/**/*.tmpl` (exclude
   `.chezmoiignore`d content is unnecessary — render is read-only):
   `chezmoi execute-template < "$f" > /tmp/check-render.$$ 2>/tmp/check-render-err.$$`
   → non-zero exit = FAIL naming the file. For `*.sh.tmpl` files additionally
   `bash -n /tmp/check-render.$$`.
4. **shellcheck**: run over the same file set as section 2 with
   `--severity=error` (see Step 2).
5. **terraform**: only if `command -v terraform` AND
   `[ -d infra/environments/prod/.terraform ]`:
   `terraform -chdir=infra/environments/prod validate -no-color`.

**Verify**: `bash -n scripts/check.sh` → exit 0.

### Step 2: Calibrate so the current tree passes

Run `scripts/check.sh`. Two expected calibration issues:

- If shellcheck `--severity=error` still fails on existing files, list the
  failing files in a `SHELLCHECK_EXCLUDE` array at the top of check.sh with a
  comment `# pre-existing findings; burn down via docs/plans/005+` — do NOT
  silence via broader severity or fix the files.
- If any template fails to render, that is a REAL finding — STOP and report
  it (a currently-broken template is exactly what this gate exists to catch).

**Verify**: `scripts/check.sh; echo "exit=$?"` → `exit=0`, every section
prints PASS or an explicit `skip:`/excluded note.

### Step 3: Wire package.json + installers + README

- `package.json`: `"check": "./scripts/check.sh"` alongside `"tf"`.
- `platform/homebrew/Brewfile`: add `brew "shellcheck"` in the CLI-tools
  section (match neighbors' formatting).
- `scripts/install-arch.sh`: add `shellcheck` to the pacman package list
  (match the existing list style).
- `README.md`: under "Quick Start" or a new short "Verifying" heading, 2-3
  lines: run `scripts/check.sh` (or `npm run check`) before committing;
  what it covers.

**Verify**: `npm run check` → exit 0. `git diff platform/homebrew/Brewfile`
shows exactly one added line.

### Step 4: Prove the gate catches breakage

Temporarily plant a syntax error (add a stray `fi` to a COPY of a zsh file is
not enough — the gate must catch in-place breakage):

```
sed -i 's/^/}/' home/dot_zsh/rc/shared.zsh   # break it
scripts/check.sh; echo "exit=$?"              # expect exit=1, FAIL: zsh-syntax
git checkout -- home/dot_zsh/rc/shared.zsh   # restore
scripts/check.sh; echo "exit=$?"              # expect exit=0
```

## Test plan

Step 4 IS the test (gate fails on planted breakage, passes on clean tree).
Paste both outputs into the commit message body.

## Done criteria

- [ ] `scripts/check.sh` exits 0 on the clean tree, 1 on planted breakage
- [ ] `npm run check` works
- [ ] `shellcheck scripts/check.sh` itself: no errors
- [ ] shellcheck present in Brewfile AND install-arch.sh
- [ ] Any `SHELLCHECK_EXCLUDE` entries carry the burn-down comment
- [ ] No source files outside scope modified (`git status`)
- [ ] `docs/plans/README.md` status row updated

## STOP conditions

- A template fails `chezmoi execute-template` on the clean tree (live bug —
  report the file and error; it may need its own fix first).
- `chezmoi execute-template` is unavailable or errors on machine-context
  lookups for a class of templates (e.g. templates depending on
  `.chezmoi.hostname` render differently per machine — if any template
  CANNOT render on this machine, exclude with a comment and report).
- More than ~10 files need SHELLCHECK_EXCLUDE (signal the severity gate is
  wrong for this repo — report instead of shipping a mostly-excluded check).

## Maintenance notes

- Every future refactor plan (005+) runs `scripts/check.sh` as its
  before/after gate — keep it fast (<10s) and dependency-light.
- The `SHELLCHECK_EXCLUDE` list is a debt ledger: shrink it, never grow it
  silently. Reviewer should reject additions without a plan reference.
- Follow-ups deferred: wiring into the pre-commit chain (after plan 003), a
  GitHub Actions runner, `chezmoi doctor` (interactive-ish output, needs
  taming), and `zsh -i -c exit` startup-time budget check.
