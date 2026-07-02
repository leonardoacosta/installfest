# Plan 005: One shared python-with-tomllib resolver for all 7 projects.toml consumers

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `docs/plans/README.md`.
>
> **Drift check (run first)**: `git diff --stat 2068bad..HEAD -- scripts/cmux-workspaces.sh scripts/generate-raycast.sh scripts/mux-remote.sh packages/workspace/bin/wsenv packages/workspace/bin/generate-profiles packages/workspace/bin/wk-ready home/dot_local/bin/executable_copen`
> On any drift, compare the "Current state" excerpts against the live code
> before proceeding; on a mismatch, STOP.

## Status

- **Priority**: P2
- **Effort**: M
- **Risk**: MED
- **Depends on**: docs/plans/004-verification-baseline.md (check.sh is the gate)
- **Category**: tech-debt
- **Planned at**: commit `2068bad`, 2026-07-02

## Why this matters

`home/projects.toml` is the registry every launcher reads — and seven scripts
each hand-roll the "find a python3 with tomllib" problem. Four carry a
verbatim-identical probe loop (added because macOS resolves `/usr/bin/python3`
= 3.9, which lacks `tomllib`); three still call **bare `python3`** and hit
`ModuleNotFoundError: tomllib` on any Mac shell where Homebrew python is not
first in PATH — the exact bug the probe loop was written to fix, never
propagated. Every future registry consumer re-decides this. One sourced lib
kills the copy-paste and closes the latent Mac breakage in `copen`,
`generate-raycast.sh`, and `mux-remote.sh` in a single move.

## Current state

The seven consumers and their camps:

**Camp A — have the probe loop (verbatim duplicate x4):**
- `packages/workspace/bin/wsenv:71-84`
- `packages/workspace/bin/generate-profiles:~32`
- `packages/workspace/bin/wk-ready:~89`
- `scripts/cmux-workspaces.sh:58-70`

The canonical excerpt (from `wsenv:71-84`):

```bash
# Resolve a python3 that has tomllib (stdlib only in 3.11+). The ambient `python3`
# can be too old — e.g. macOS interactive shells resolve /usr/bin/python3 (3.9, no
# tomllib) ahead of Homebrew's. Probe known-good interpreters in order.
WS_PY=""
for _py in python3.14 python3.13 python3.12 python3.11 python3; do
  if command -v "$_py" >/dev/null 2>&1 && "$_py" -c 'import tomllib' 2>/dev/null; then
    WS_PY="$_py"; break
  fi
done
if [[ -z "$WS_PY" ]]; then
  echo "wsenv: no python3 with tomllib found (need Python >= 3.11)" >&2
  exit 1
fi
```

(`cmux-workspaces.sh` names its variable `MUX_PY` or similar — read the file;
the loop body is the same.)

**Camp B — bare `python3`, latent Mac bug (x3):**
- `scripts/generate-raycast.sh:38` — `python3 << 'PYTHON_SCRIPT'` (heredoc,
  `import tomllib` at line 39)
- `scripts/mux-remote.sh:29-30` — `python3 << 'PYEOF'` inside
  `generate_picker_applescript()`, `import tomllib` at line 30
- `home/dot_local/bin/executable_copen:66-67` —
  `RESOLVED="$(python3 - "$REGISTRY" "$ARG_CODE" "$ANCHOR" <<'PY'` with
  `import os, sys, tomllib`

Registry path resolution also varies: `wsenv:62-63` does
`REGISTRY="${WSENV_REGISTRY:-${DOTFILES:-$HOME/dev/if}/home/projects.toml}"`
with an existence fallback; others hardcode variants.

Repo conventions that bind this plan:
- Sourced libs MUST use the source-guard strict-mode idiom, never bare
  `set -euo pipefail` at file scope (a sourced `set -e` leaks into the caller):
  `(return 0 2>/dev/null) || set -euo pipefail`. Exemplar: `home/dot_local/bin/executable_copen:26`.
- `scripts/utils.sh` is the existing sourced-lib exemplar for style.
- `copen` is deployed via chezmoi (`home/dot_local/bin/executable_copen` →
  `~/.local/bin/copen`). It runs on machines where the lib must be found via
  `$DOTFILES` — it cannot assume a repo-relative path from `$0`.
- `copen` is part of in-flight work (`add-auth-resilience-and-hygiene` /
  recent commits) — coordinate: if `git status` shows it modified, include its
  current on-disk state, not HEAD.

## Commands you will need

| Purpose | Command | Expected on success |
|---------|---------|---------------------|
| Gate (before AND after) | `scripts/check.sh` | exit 0 |
| Lib syntax | `bash -n scripts/lib/registry.sh && shellcheck scripts/lib/registry.sh` | exit 0 |
| Leak check | `bash -c 'source scripts/lib/registry.sh; set -o \| grep errexit'` | `errexit off` |
| Per-consumer smokes | see Step 3 | listed there |

## Scope

**In scope**:
- `scripts/lib/registry.sh` (create)
- The 7 consumer files listed above (minimal edits: replace probe/bare-python3
  with lib usage)

**Out of scope**:
- `home/projects.toml` — schema untouched.
- Any consolidation of the consumers' python PAYLOADS (what each heredoc
  computes). Each script's business logic stays where it is. A shared
  `registry_resolve` API was considered and deliberately deferred — the
  payloads differ enough that forcing one API now is speculative abstraction.
- `packages/workspace/lib/trackers/*` — they don't parse projects.toml.
- `wk-doctor` — reads profiles, not the registry (verify; if it does parse
  the registry, add it and note the addition).

## Git workflow

- Current branch; conventional commit, e.g.
  `refactor(registry): shared tomllib-python resolver; fix bare-python3 Mac breakage`.
- One commit for the lib + Camp B fixes, a second for the Camp A dedup is
  acceptable; or one commit total. Do NOT push unless instructed.

## Steps

### Step 0: Record the gate baseline

`scripts/check.sh` → must exit 0 before you start. If plan 004 has not landed,
STOP (dependency).

### Step 1: Create scripts/lib/registry.sh

Contents (match utils.sh comment style):

- Source-guard strict mode line (see conventions above).
- `registry_path()` — echoes the registry file:
  `${PROJECTS_REGISTRY:-${DOTFILES:-$HOME/dev/if}/home/projects.toml}`, with
  the `$HOME/dev/if/home/projects.toml` existence fallback copied from
  `wsenv:62-63`; returns 1 with a stderr message if the file does not exist.
- `registry_python()` — the probe loop above, verbatim semantics
  (python3.14 → python3 order, `import tomllib` probe); caches in
  `_REGISTRY_PY` so repeated calls don't re-probe; echoes the interpreter;
  returns 1 with the "no python3 with tomllib found (need Python >= 3.11)"
  stderr message on failure.

No other functions. Keep it under ~40 lines.

**Verify**: `bash -n scripts/lib/registry.sh && shellcheck scripts/lib/registry.sh`
→ exit 0. Leak check command from the table → `errexit off`.

### Step 2: Convert Camp B (the bug fixes) one file at a time

For each of `generate-raycast.sh`, `mux-remote.sh`, `copen`:

1. Source the lib. For `scripts/*.sh`: `source "$(git rev-parse --show-toplevel 2>/dev/null || echo "${DOTFILES:-$HOME/dev/if}")/scripts/lib/registry.sh"`
   — match how the script already locates the repo (both use `SCRIPT_DIR` or
   `$DOTFILES`; reuse whichever the file already has rather than adding a new
   mechanism). For `copen` (deployed to ~/.local/bin, repo not implied by $0):
   `source "${DOTFILES:-$HOME/dev/if}/scripts/lib/registry.sh"` guarded by an
   `[ -r ... ] || { echo "copen: registry lib not found" >&2; exit 1; }`.
2. Replace the bare `python3` invocation with `"$(registry_python)"`
   (capture once into a local var; propagate its failure: `PY=$(registry_python) || exit 1`).
3. Where the file hardcodes the registry path, switch to `registry_path`.

**Verify after EACH file**:
- `generate-raycast.sh`: `bash scripts/generate-raycast.sh` → exit 0 and
  `git status --short platform/raycast-scripts/` shows no unexpected diff
  (output identical to before — regeneration is deterministic from the toml).
- `mux-remote.sh`: `bash -n scripts/mux-remote.sh` → exit 0 (its runtime is
  Mac-interactive AppleScript; syntax + `zsh -ic 'type mux-remote'`-level
  checks only — note this limitation in the commit).
- `copen`: `bash -n home/dot_local/bin/executable_copen` → 0; then
  `chezmoi apply && copen --help 2>&1 | head -3` (or its usage path) → prints
  usage, no tomllib error; and a resolve smoke:
  `cd ~/dev/if && copen . 2>&1 | head -2` should get past registry resolution
  (it may fail later on Mac-connectivity — resolution succeeding is the pass
  signal; "no python3 with tomllib" or ModuleNotFoundError is the fail signal).

### Step 3: Convert Camp A (the dedup)

For each of `wsenv`, `generate-profiles`, `wk-ready`, `cmux-workspaces.sh`:
delete the inline probe loop, source the lib (these all live in the repo /
resolve `DOTFILES`; `packages/workspace/bin/*` already compute the repo root —
reuse it), and set their existing variable from the lib:
`WS_PY=$(registry_python) || exit 1` (keep each script's own variable name so
downstream references need no edits).

**Verify after EACH file**:
- `wsenv`: `packages/workspace/bin/wsenv --help` → usage prints;
  `packages/workspace/bin/wsenv list` (or its list mode per --help) → project
  list prints.
- `generate-profiles`: run it (`packages/workspace/bin/generate-profiles`) →
  exit 0, `git status` shows only expected regenerated profile output (if it
  writes to `~/.config/workspace`, no repo diff at all).
- `wk-ready`: `packages/workspace/bin/wk-ready --help || packages/workspace/bin/wk-ready` → runs past registry parse.
- `cmux-workspaces.sh`: `bash scripts/cmux-workspaces.sh --help` → usage; if a
  generation mode exists per its usage, run it and confirm exit 0.

### Step 4: Sweep + gate

- `grep -rn "for _py in python3" scripts/ packages/ home/dot_local/bin/` →
  only `scripts/lib/registry.sh` matches.
- `grep -rn 'python3 <<\|python3 - ' scripts/generate-raycast.sh scripts/mux-remote.sh home/dot_local/bin/executable_copen` → no bare `python3` heredoc remains (they use the resolved var).
- `scripts/check.sh` → exit 0.

## Test plan

The per-consumer smokes in Steps 2-3 are the tests; the strongest is the
generate-raycast determinism check (same toml in → same scripts out proves the
parse path unchanged). Paste that and the copen smoke into the commit body.
No new test files — this repo has no harness; check.sh (plan 004) covers
syntax/lint regression permanently.

## Done criteria

- [ ] `scripts/check.sh` exits 0
- [ ] Probe loop exists exactly once in the repo (grep from Step 4)
- [ ] `generate-raycast.sh` output byte-identical pre/post (no artifact diff)
- [ ] All 7 consumers run their smoke without a tomllib error
- [ ] Lib sourced with source-guard idiom; leak check shows `errexit off`
- [ ] No payload-logic changes inside any consumer's python heredoc
- [ ] `docs/plans/README.md` status row updated

## STOP conditions

- Plan 004's `scripts/check.sh` absent or failing at Step 0.
- `copen` on disk differs from HEAD (in-flight work) in the registry-parse
  region — report and coordinate before editing.
- `generate-raycast.sh` output diff is non-empty after conversion (parse
  behavior changed — find why before proceeding).
- A consumer turns out to parse the registry WITHOUT python (pure awk/sed) —
  it is out of this plan's shape; report it.
- You feel the need to build `registry_resolve()` / merge python payloads —
  that is explicitly deferred; do not.

## Maintenance notes

- New registry consumers MUST source `scripts/lib/registry.sh` — add a line
  saying so to the header comment of `home/projects.toml`.
- If a shared `registry_resolve` API is ever wanted (see deferred note in
  Scope), the seven call sites are now uniform enough to survey in one pass.
- Reviewer should scrutinize: that each converted consumer propagates
  `registry_python` failure (no silent empty-var python invocation), and the
  copen sourcing guard (deployed script, repo may be absent on exotic hosts).
