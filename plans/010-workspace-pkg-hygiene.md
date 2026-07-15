# Plan 010: workspace package hygiene — wk-doctor deploy-or-delete, check.sh blind spot, registry.sh dupe, README/help drift

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**:
> `git diff --stat 9399b92..HEAD -- scripts/check.sh packages/workspace home/dot_local/bin .claude/workflows/project-mgmt-audit.js`
> If any in-scope file changed since this plan was written, compare the
> "Current state" excerpts against the live code before proceeding; on a
> mismatch, treat it as a STOP condition.

## Status

- **Priority**: P2
- **Effort**: M
- **Risk**: LOW (MED only for Step 1 Option B — deletion path, operator-gated)
- **Depends on**: none (docs/plans/005-registry-parser-consolidation.md is DONE and is cited as a mandate, not a dependency)
- **Category**: tech-debt (+ one real correctness bug found en route: ado-ready SC2259)
- **Planned at**: commit `9399b92`, 2026-07-14

## Why this matters

`packages/workspace/` is the org-routing layer for every dev machine (wsenv/wk/wk-ready
are deployed onto PATH via chezmoi and called by shell integration and cmux), yet it sits
entirely outside the repo's quality gate: none of its 9 bash scripts get `bash -n` or
shellcheck from `scripts/check.sh`. Extending the gate immediately surfaces one **real
runtime bug**: `ado-ready`'s final normalization pipes `$RAW` into a python heredoc, and
the heredoc redirection overrides the pipe (SC2259) — `sys.stdin.read()` is always empty,
so the b-and-b ADO tracker emits `[]` even when the `az boards query` succeeded
(empirically reproduced, see Step 3). Separately: the README-documented `wk doctor`
entry point exits 127 on every machine because its symlink template was never created
(operator decision: deploy or delete); the README's §Profile-contract section describes
staging/validation behavior `generate-profiles` does not have — and
`.claude/workflows/project-mgmt-audit.js:89` diffs that README against the code, so the
drift generates recurring false audit findings; `wsenv --help` prints a wrong org
vocabulary; two comments cite a tracker adapter (`bd-ready`) that does not exist; and the
tomllib probe loop that `scripts/lib/registry.sh` was created to centralize (docs/plans/005,
DONE) survives as two copy-pasted instances in the trackers.

## Current state

All excerpts are fresh reads at commit `9399b92`. Repo root: `/home/nyaptor/dev/personal/installfest`.

### Repo facts (inline — the executor has no other context)

- Dotfiles/dev-env repo managed by chezmoi; `.chezmoiroot` = `home/`, so everything under
  `home/` deploys to `~`. `home/dot_local/bin/symlink_<name>.tmpl` files become
  `~/.local/bin/<name>` symlinks on `chezmoi apply`.
- Quality gate: `bash scripts/check.sh` (also `npm run check`) — zsh syntax, bash syntax,
  chezmoi template render, shellcheck `--severity=error`, terraform validate. Exit 0 = all pass.
  **Baseline at 9399b92 (verified by running it)**:
  `PASS: sh-syntax (42 bash + hooks)`, `PASS: shellcheck (40 checked, 2 excluded)`, exit 0.
- Git on this machine: `core.hooksPath=.beads/hooks` (bd-managed). `.beads/issues.jsonl`
  is gitignored (Dolt push only); `.beads/interactions.jsonl` is committed explicitly if changed.
- Commit pattern: targeted `git add <files>`, ONE commit, push.
- The `wk` dispatcher (packages/workspace/bin/wk:72-78) resolves `wk <sub>` strictly by
  PATH lookup of an executable named `wk-<sub>` — no per-subcommand code exists or is
  needed in `wk` itself:

  ```bash
  # packages/workspace/bin/wk:72-78
  if ! TARGET=$(command -v "wk-$SUBCMD" 2>/dev/null); then
    echo "wk: unknown subcommand '$SUBCMD' (no executable wk-$SUBCMD on PATH)" >&2
    echo "wk: run 'wk --list' to see what's available" >&2
    exit 127
  fi
  exec "$TARGET" "$@"
  ```

### Files in play

| File | Role |
| --- | --- |
| `scripts/check.sh` | quality gate; `SH_FILES` set built at lines 57-62 |
| `scripts/lib/registry.sh` | shared registry lib (`registry_path` 18-26, `registry_python` 31-46) |
| `packages/workspace/bin/wk` | umbrella dispatcher (PATH-based, see above) |
| `packages/workspace/bin/wk-doctor` | 11K provenance inspector — NOT deployed (WSP-01) |
| `packages/workspace/bin/wk-ready` | tracker dispatcher; sources registry.sh at 87-89 (exemplar) |
| `packages/workspace/bin/wsenv` | resolver/activator; help drift at 9/25/42; dead export at 201 |
| `packages/workspace/bin/generate-profiles` | pure scaffolder (its header, lines 9-14, says so) |
| `packages/workspace/lib/trackers/beads-ready` | beads adapter; inline probe loop 46-52 (WSP-02) |
| `packages/workspace/lib/trackers/ado-ready` | ADO adapter; probe loop 59-65 + SC2259 bug at 125 |
| `packages/workspace/lib/trackers/none-ready` | 325-byte fallback adapter — do NOT delete |
| `packages/workspace/README.md` | package doc; §Profile contract drift 149-167, §Per-org 140-144 |
| `packages/workspace/profiles/priceless/profile.toml` | stale `bd-ready` name at line 8 |
| `home/dot_local/bin/` | chezmoi symlink templates; has `symlink_{wk,wk-ready,ws-claude,wsenv}.tmpl`, NO `symlink_wk-doctor.tmpl` |
| `.claude/workflows/project-mgmt-audit.js` | line 89 invokes `packages/workspace/bin/wk-doctor --json` by repo path and diffs README vs code |

### WSP-01 — wk-doctor undeployed (operator gate)

Verified at plan time: `command -v wk-doctor` finds nothing; `ls ~/.local/bin/wk-doctor`
→ "No such file or directory"; `home/dot_local/bin/` contains symlink templates for the
4 siblings but none for wk-doctor. `grep -n doctor packages/workspace/bin/wk` → no matches
(exit 1) — correct, because dispatch is PATH-based; the ONLY missing piece is the symlink
template. `packages/workspace/README.md` documents the entry point as live:

```
# README.md:37
registry — new subcommands appear the moment they land on PATH. Run `wk` (or
`wk --list`) to see what's discovered. Today: `ready`, `doctor`.

# README.md:82-86
# Provenance inspection (new)
wk doctor                   # what config is active in $PWD + which layer set it
wk doctor ws                # inspect a specific repo's org (b-and-b)
wk doctor --json            # machine-readable provenance object
wk doctor -i                # fzf drill-down: pick a layer, preview its source file
```

Sibling template content (this is the exact shape to clone — verified by reading
`home/dot_local/bin/symlink_wk.tmpl`):

```
{{ .chezmoi.sourceDir }}/../packages/workspace/bin/wk
```

The only live caller today is the repo-path invocation in
`.claude/workflows/project-mgmt-audit.js:89` ("Run packages/workspace/bin/wk-doctor --json
for one repo per org ...").

### WSP-04 — check.sh blind spot + the SC2259 bug it exposes

```bash
# scripts/check.sh:57-62 — SH_FILES feeds BOTH section_sh (bash -n) and section_shellcheck
mapfile -d '' SH_FILES < <(
    find scripts -name '*.sh' -type f -print0
    find ssh-mesh/scripts -name '*.sh' -type f \
        -not -path 'ssh-mesh/scripts/remote/cmux-bridge/*' -print0
)
for f in platform/*.sh; do [ -f "$f" ] && SH_FILES+=("$f"); done
```

The string `packages` appears nowhere in check.sh. The 9 extensionless scripts
(`bin/`: generate-profiles, wk, wk-doctor, wk-ready, ws-claude, wsenv; `lib/trackers/`:
ado-ready, beads-ready, none-ready — all with `#!/usr/bin/env bash`, verified) get zero
coverage. `lib/trackers/` also contains a `README.md`, so the new find clause must exclude
`*.md`.

**Pre-run at plan time** (so there are no surprises): all 9 pass `bash -n`; shellcheck
`--severity=error` over all 9 reports exactly ONE finding:

```
In packages/workspace/lib/trackers/ado-ready line 125:
echo "$RAW" | "$PY" - "$ORG_URL" <<'PY'
                                   ^--^ SC2259 (error): This redirection overrides piped input.
```

This is a genuine bug, not noise — reproduced at plan time:

```bash
$ RAW='[{"id":1}]'; echo "$RAW" | python3 - "url" <<'PY'
import sys
print("STDIN_SEEN:", repr(sys.stdin.read()))
PY
STDIN_SEEN: ''
```

The heredoc becomes python's stdin (the program source); the piped `$RAW` is discarded;
`sys.stdin.read()` inside the script returns `''` → `data = []` → the normalizer prints
`[]` unconditionally. `ado-ready`'s own sibling `beads-ready` already documents and uses
the correct pattern (beads-ready:55-57: "Captured into vars so we can pass them with
`-c \"$VAR\"` while keeping stdin free for data piping").

### WSP-02 — registry.sh dupe in the trackers

`scripts/lib/registry.sh` exists (docs/plans/005, status DONE per docs/plans/README.md:26)
and its maintenance note mandates: "New registry consumers MUST source
`scripts/lib/registry.sh`". Plan 005 excluded the trackers on the premise
(docs/plans/005 § Out of scope) "`packages/workspace/lib/trackers/*` — they don't parse
projects.toml" — which is false for beads-ready (line 106 tomllib-loads `$REGISTRY`,
i.e. projects.toml).

```bash
# packages/workspace/lib/trackers/beads-ready:42-52
REGISTRY="${WSENV_REGISTRY:-${DOTFILES:-$HOME/dev/personal/installfest}/home/projects.toml}"
[[ -f "$REGISTRY" ]] || { echo "beads-ready: registry not found: $REGISTRY" >&2; exit 1; }

# Resolve python3 with tomllib (>=3.11). Same probe pattern as wsenv.
PY=""
for _py in python3.14 python3.13 python3.12 python3.11 python3; do
  if command -v "$_py" >/dev/null 2>&1 && "$_py" -c 'import tomllib' 2>/dev/null; then
    PY="$_py"; break
  fi
done
[[ -n "$PY" ]] || { echo "beads-ready: need python3 with tomllib (>=3.11)" >&2; exit 1; }
```

```bash
# packages/workspace/lib/trackers/ado-ready:58-65
# Probe a python3 with tomllib (same pattern as wsenv / generate-profiles / bd-ready).
PY=""
for _py in python3.14 python3.13 python3.12 python3.11 python3; do
  if command -v "$_py" >/dev/null 2>&1 && "$_py" -c 'import tomllib' 2>/dev/null; then
    PY="$_py"; break
  fi
done
[[ -n "$PY" ]] || { echo "ado-ready: need python3 with tomllib (>=3.11)" >&2; echo "[]"; exit 0; }
```

The exemplar to match is `wsenv` itself: it keeps its OWN `WSENV_REGISTRY`-based
`REGISTRY=` resolution (wsenv:96-97) and sources registry.sh only for `registry_python`
(wsenv:104-106). **Deliberate decision, do not "improve" it**: keep beads-ready's
`REGISTRY=` line 42 unchanged — `WSENV_REGISTRY` is the adapter's documented env override
(beads-ready header line 36, wk-ready header line 24), whereas registry.sh's
`registry_path()` reads a DIFFERENT variable (`PROJECTS_REGISTRY`, registry.sh:19).
Swapping to `registry_path` would silently rename a documented override. Only the probe
loop is replaced.

```bash
# packages/workspace/bin/wk-ready:87-89 — the sourcing pattern to copy
# shellcheck source=../../../scripts/lib/registry.sh
source "${DOTFILES:-$HOME/dev/personal/installfest}/scripts/lib/registry.sh"
PY=$(registry_python) || exit 1
```

(registry.sh:12 carries the source-guard `(return 0 2>/dev/null) || set -euo pipefail`,
so sourcing it from a `set -e` script is safe.)

### WSP-03 — README §Profile contract describes behavior generate-profiles does not have

`packages/workspace/bin/generate-profiles` is a pure scaffolder — its own header
(generate-profiles:9-14): "It only SCAFFOLDS a skeleton for any org category in the
registry that does not yet have a committed profile dir + its chezmoi symlink template.
Existing profiles are never touched". It never calls `workspace-profile-validate` (grep
confirms zero references), has no staging/promote logic, and no 0/1/2 exit contract.
The only validator call sites in the package are `wsenv` (line 173-183, `--validate`
mode) and `wk-doctor` (lines 80-85, fail-soft CONTRACT row).

But the README claims (149 / 159-167):

```
# README.md:149
## Profile contract (cc-owned, validated at generation)

# README.md:159-167
Two enforcement seams in this package call it:

- **`generate-profiles`** stages each org, validates the staged dir, and **promotes only on
  pass** — a failing org is never finalized (existing profile left intact), other orgs still
  generate. Aggregate exit: `0` all-ok / `1` all-fail / `2` partial (so chezmoi/CI can tell
  "nothing worked" from "one org drifted").
- **`wsenv --validate <code>`** resolves the org and runs the validator without launching — an
  explicit, opt-in launch-time safety net for hand-edited profiles. The default
  `--flags`/`--activate` path is **never** gated by validation (fail-open at launch).
```

And §Per-org profiles (140-144) still carries the retired pre-rehome model,
contradicting lines 44-49 of the SAME README ("profiles/<org>/ are **committed dirs** ...
The live file IS the repo file ... retired 2026-07-05"):

```
# README.md:140-144
## Per-org profiles (generated)

`~/.config/workspace/<org>/` — `env.sh` (sourced), `wrappers/` (prepended to PATH),
`claude/` (`--add-dir` skills), plus `mcp.json` / `prompt.txt` when present. Machine-local
output (not committed); regenerated from the registry.
```

**Two possible fixes — the default is chosen; the alternative is recorded for the
reviewer**:
- **Default (this plan implements it): correct the README** to the post-rehome reality.
  The rehome (repo commit `2092111`) rewrote §How it deploys but left these two sections
  stale; the scaffolder-only model is the deliberate, documented design.
- Alternative (NOT taken): implement staging + validation + 0/1/2 exits in
  generate-profiles to make the README true. Rejected as speculative — the committed-profile
  model made generation-time validation moot (profiles are hand-edited in git, validated
  on demand via `wsenv --validate` / `wk doctor`), and it contradicts the script's own
  header design note. If the operator wants this instead, STOP and report.

### WSP-05 — wsenv --help org-set drift

`wsenv -h` awk-dumps the header comment block (wsenv:67), so these comment lines ARE the
user-facing help:

```
# packages/workspace/bin/wsenv:9
# Source of truth: projects.toml `category` field (b-and-b / client / personal).
# packages/workspace/bin/wsenv:25
#   wsenv --org <code>      # print just the org slug (b-and-b|client|personal)
# packages/workspace/bin/wsenv:42
#   wsenv --org oo                   # -> client
```

The real vocabulary has no `client`: `home/projects.toml:7` — `category  One of:
"b-and-b", "priceless", "personal"` — and wsenv's own cat2org maps (lines 136 and 151)
contain exactly `{"b-and-b": "b-and-b", "priceless": "priceless", "personal": "personal"}`.
Runtime check at plan time: `wsenv --org oo` → `priceless`.

### WSP-06 — stale `bd-ready` adapter name

`ls packages/workspace/lib/trackers/` → `ado-ready, beads-ready, none-ready, README.md`.
No `bd-ready` exists. A repo-wide grep finds exactly two stale citations (both comments):

```
# packages/workspace/profiles/priceless/profile.toml:8-9
# wk-ready dispatches to packages/workspace/lib/trackers/bd-ready, which fans
# out `bd ready --json` across every priceless project in projects.toml.

# packages/workspace/lib/trackers/ado-ready:58
# Probe a python3 with tomllib (same pattern as wsenv / generate-profiles / bd-ready).
```

(The ado-ready:58 line is deleted wholesale by Step 4's probe-loop replacement; only
profile.toml needs a standalone word fix.)

### WSP-07 — dead `export WS_CODE`

```bash
# packages/workspace/bin/wsenv:200-201 (activate mode)
    echo "export WS_WORKSPACE=$ORG"
    echo "export WS_CODE=$CODE"
```

`WS_WORKSPACE` has a real consumer (`packages/workspace/integrations/chpwd.zsh:31`).
`WS_CODE` has zero readers — swept at audit time across this repo, ~/dev/cc, deployed
~/.zsh, starship/tmux configs, and the profile root; the only occurrence anywhere in this
repo is the emission line itself (re-verified at plan time:
`grep -rn WS_CODE packages/ scripts/ home/` → only `wsenv:201`). Ownership brief for this
plan: **delete the export** (see Maintenance notes for the residual-risk caveat).

## Commands you will need

| Purpose | Command | Expected on success |
| --- | --- | --- |
| Quality gate | `bash scripts/check.sh` | exit 0, `ALL CHECKS PASSED` |
| Gate baseline (pre-change) | `bash scripts/check.sh` | `sh-syntax (42 bash + hooks)`, `shellcheck (40 checked, 2 excluded)` |
| Single-file lint | `shellcheck --severity=error <file>` | exit 0, no output |
| Single-file syntax | `bash -n <file>` | exit 0, no output |
| Deploy dotfiles | `chezmoi apply` | exit 0 |
| wsenv smoke | `~/.local/bin/wsenv --org oo` | `priceless` |
| wk-ready smoke | `~/.local/bin/wk ready priceless \| head -c 200` | starts with `[` (JSON array) |

All commands run from the repo root `/home/nyaptor/dev/personal/installfest` unless noted.
Note: `wsenv` on PATH is a symlink to the repo file, so edits are live immediately —
no `chezmoi apply` needed except for the NEW symlink in Step 1 Option A.

## Suggested executor toolkit

- `verification-before-completion` skill before claiming done — every done criterion below
  is a command, run them all.
- `systematic-debugging` skill if `scripts/check.sh` fails on something this plan did not
  predict (see STOP conditions first).

## Scope

**In scope** (the only files you may modify):
- `home/dot_local/bin/symlink_wk-doctor.tmpl` (create — Step 1 Option A)
- `packages/workspace/lib/trackers/ado-ready`
- `packages/workspace/lib/trackers/beads-ready`
- `packages/workspace/bin/wsenv`
- `packages/workspace/README.md`
- `packages/workspace/profiles/priceless/profile.toml`
- `scripts/check.sh` — **ONLY the SH_FILES block (lines 57-62)**; no other section
- `plans/README.md` (status row only)
- Option B only (operator-chosen): deletion set listed in Step 1 — do NOT touch it under Option A

**Out of scope** (do NOT touch, even though they look related):
- `scripts/check.sh` sections 1/3/4/5, `SHELLCHECK_EXCLUDE` (lines 41-44), and the
  hooks sweep (lines 86-93) — other plans/owners; this plan owns only the SH_FILES glob.
- `scripts/lib/registry.sh` — consumed as-is; no edits, no new functions.
- `packages/workspace/bin/wk`, `wk-ready`, `ws-claude`, `generate-profiles` — no code
  changes (generate-profiles stays a scaffolder; the README is corrected instead).
- `packages/workspace/lib/trackers/none-ready` — live default-fallback adapter.
- `home/projects.toml` — registry schema/content untouched.
- `.claude/workflows/project-mgmt-audit.js` — its README-vs-code diff instruction is the
  MOTIVATION for WSP-03, not an edit target (Option B is the sole exception, operator-gated).
- The `WSENV_REGISTRY` env-override name in beads-ready — documented API; do not rename
  to `PROJECTS_REGISTRY` (see WSP-02 decision above).
- `docs/plans/**` — the older ledger; plan 005's false out-of-scope premise is corrected
  by THIS plan's work, not by editing a DONE plan document.

## Git workflow

- Work on the current branch (`main` — this repo commits small hygiene units directly).
- ONE commit at the end (two if the operator chose Option B and wants the deletion
  isolated). Conventional style, e.g.:
  `fix(workspace): deploy wk-doctor, gate workspace scripts in check.sh, fix ado-ready stdin bug + doc drift`
- Targeted `git add` of exactly the in-scope files; never `git add .`. Include
  `.beads/interactions.jsonl` only if bd changed it (`.beads/issues.jsonl` is gitignored
  here — do not force-add it).
- Push only if the operator instructed; this repo's normal pattern is commit + push in
  the same unit of work.

## Steps

### Step 0: Baseline

Run the gate and record the baseline counts.

**Verify**: `bash scripts/check.sh; echo "exit=$?"` →
`PASS: sh-syntax (42 bash + hooks)`, `PASS: shellcheck (40 checked, 2 excluded)`,
`ALL CHECKS PASSED`, `exit=0`. (Template/terraform sections may print `skip:` warnings on
machines missing those tools — that is fine; a `FAIL:` line is not.)

### Step 1: WSP-01 — wk-doctor deploy-or-delete (OPERATOR GATE)

This step is Leo's call. If the dispatching operator has not already selected an option
(in the dispatch message or a note on this plan's `plans/README.md` row), **STOP here and
report both options with the recommendation — do not pick silently.**

**Option A — deploy (RECOMMENDED)**: one new one-line file; makes the existing README,
`wk --list` discovery, and `project-mgmt-audit.js` all true.
1. Create `home/dot_local/bin/symlink_wk-doctor.tmpl` with exactly this content
   (clone of `symlink_wk.tmpl`, path suffix changed):
   ```
   {{ .chezmoi.sourceDir }}/../packages/workspace/bin/wk-doctor
   ```
2. `chezmoi apply`
3. No change to `packages/workspace/bin/wk` is needed — dispatch is PATH-based
   (wk:72-78, quoted in Current state).

**Verify (Option A)**:
- `command -v wk-doctor` → `/home/nyaptor/.local/bin/wk-doctor`
- `readlink -f ~/.local/bin/wk-doctor` → `/home/nyaptor/dev/personal/installfest/packages/workspace/bin/wk-doctor`
- `wk --list | grep -x doctor` → `doctor`
- `wk doctor if --json | python3 -c 'import sys,json; d=json.load(sys.stdin); print(d["code"])'` → `if`

**Option B — delete (only on explicit operator instruction)**: remove
`packages/workspace/bin/wk-doctor`; remove the README's doctor surface (line 14 layout
row, line 37 `, doctor`, usage lines 82-86, the whole §Provenance inspection section
89-112); rewrite the `workspace-pkg` surface prompt in
`.claude/workflows/project-mgmt-audit.js:89` to drop the wk-doctor invocation. This
deletes a whole module + edits a live workflow — per repo rules that is a STOP-and-report
item in its own right: confirm the operator explicitly approved THIS list before touching
it, and re-run `bash scripts/check.sh` after. If Option B is chosen, Step 8's expected
counts drop by one file (50 bash + hooks / 48 checked).

**Recommendation to present**: Option A. Cost is one line vs a multi-file deletion; the
tool is maintained, README-documented, and invoked by a live audit workflow
(project-mgmt-audit.js:89). Option B would also orphan the workflow prompt's instruction.

### Step 2: WSP-03 — correct README §Per-org profiles and §Profile contract

In `packages/workspace/README.md`:

2a. Replace lines 140-144 (the `## Per-org profiles (generated)` heading + body quoted in
Current state) with:

```markdown
## Per-org profiles

`~/.config/workspace/<org>/` — `env.sh` (sourced), `wrappers/` (prepended to PATH),
`claude/` (`--add-dir` skills), plus `mcp.json` / `prompt.txt` when present. Each is a
chezmoi **symlink to the committed** `packages/workspace/profiles/<org>/` tree (see
§ How it deploys) — edit in place, review in git.
```

Keep the two org bullets that follow (`- **b-and-b:** ...`, `- **priceless / personal:** ...`)
unchanged.

2b. Change the heading at line 149 from
`## Profile contract (cc-owned, validated at generation)` to
`## Profile contract (cc-owned, validated on demand)`.

2c. Replace the `generate-profiles` bullet (lines 161-164, quoted in Current state) —
keep the `Two enforcement seams` intro line and the `wsenv --validate` bullet verbatim —
with:

```markdown
- **`wk doctor`** runs the validator against the resolved org's profile and reports the
  result as its CONTRACT row (`valid` / `INVALID — <reason>`); a missing validator or
  profile is reported, never fatal (fail-soft). `generate-profiles` is a **scaffolder
  only** — it never validates (see its header; the pre-rehome staged-generation model
  was retired 2026-07-05).
```

(If the operator chose Option B in Step 1, write the bullet around `wsenv --validate`
being the only seam and drop the `wk doctor` sentence — but Option B requires explicit
operator wording anyway.)

**Verify**:
- `grep -n 'promotes only on pass' packages/workspace/README.md` → no output, exit 1
- `grep -n 'Machine-local' packages/workspace/README.md` → no output, exit 1
- `grep -n 'validated at generation' packages/workspace/README.md` → no output, exit 1

### Step 3: Fix the ado-ready SC2259 stdin bug (real correctness bug)

In `packages/workspace/lib/trackers/ado-ready`, convert the final normalization
(lines 123-151) from the heredoc form to the capture-var + `-c` form its sibling
`beads-ready` already uses (beads-ready:54-83 is the pattern; note the `|| true` after
`read -r -d ''` — `read` returns non-zero at EOF and the script runs `set -euo pipefail`).

Replace:

```bash
echo "$RAW" | "$PY" - "$ORG_URL" <<'PY'
<python body>
PY
```

with:

```bash
# Pass the python source via -c "$VAR" so stdin stays free for the piped $RAW.
# The previous heredoc form OVERRODE the pipe (SC2259): sys.stdin.read() was
# always empty, so this step emitted [] regardless of query results.
read -r -d '' NORMALIZE <<'PY' || true
<python body — unchanged, verbatim>
PY
echo "$RAW" | "$PY" -c "$NORMALIZE" "$ORG_URL"
```

The python body itself (lines 126-150: `import sys, json` ... `print(json.dumps(out))`)
is copied verbatim — only the delivery mechanism changes.

**Verify**:
- `bash -n packages/workspace/lib/trackers/ado-ready` → exit 0
- `shellcheck --severity=error packages/workspace/lib/trackers/ado-ready` → exit 0, no output
- Pattern repro proving stdin now flows (generic harness, no ADO auth needed):
  ```bash
  read -r -d '' N <<'PY' || true
  import sys
  print("STDIN_SEEN:", len(sys.stdin.read()))
  PY
  echo '[{"id":7}]' | python3 -c "$N" x
  ```
  → `STDIN_SEEN: 11` (non-zero; the old `python3 - x <<'PY'` form printed 0)
- Only if ADO auth is configured on this machine (`az devops login` done or
  `AZURE_DEVOPS_EXT_PAT` set): `wk ready b-and-b | head -c 200` → JSON array that is
  non-empty when real work items exist. If auth is not configured, skip — the adapter's
  documented behavior is `[]` + a stderr auth warning, which does not exercise line 125.

### Step 4: WSP-02 — replace the trackers' probe loops with registry.sh

4a. In `packages/workspace/lib/trackers/beads-ready`, replace lines 45-52 (the
`# Resolve python3 ...` comment, the probe loop, and the `[[ -n "$PY" ]] ||` guard —
quoted in Current state) with:

```bash
# Resolve python3 with tomllib (>=3.11) via the shared registry lib
# (docs/plans/005 maintenance note: registry consumers MUST source it).
# shellcheck source=../../../../scripts/lib/registry.sh
source "${DOTFILES:-$HOME/dev/personal/installfest}/scripts/lib/registry.sh"
PY=$(registry_python) || exit 1
```

Keep line 42 (`REGISTRY="${WSENV_REGISTRY:-...`) exactly as-is — see the WSP-02 decision
in Current state.

4b. In `packages/workspace/lib/trackers/ado-ready`, replace lines 58-65 (the
`# Probe a python3 ...` comment, probe loop, and guard) with:

```bash
# Resolve python3 with tomllib via the shared registry lib (same seam as
# wsenv / generate-profiles / beads-ready). Adapter contract: never fail
# hard — emit [] and exit 0 when no suitable python exists.
# shellcheck source=../../../../scripts/lib/registry.sh
source "${DOTFILES:-$HOME/dev/personal/installfest}/scripts/lib/registry.sh"
PY=$(registry_python) || { echo "[]"; exit 0; }
```

(Note the two adapters keep their DIFFERENT failure modes on purpose: beads-ready exits 1;
ado-ready honors the never-fail-hard contract with `[]` + exit 0. `registry_python` prints
its own stderr diagnostic. This step also removes ado-ready:58's stale `bd-ready` mention —
half of WSP-06. The `source=` path is 4 levels up from `lib/trackers/`, one more than
wk-ready's 3 from `bin/`.)

**Verify**:
- `grep -c 'python3.14' packages/workspace/lib/trackers/ado-ready packages/workspace/lib/trackers/beads-ready` → `...:0` for both files
- `bash -n packages/workspace/lib/trackers/beads-ready && bash -n packages/workspace/lib/trackers/ado-ready` → exit 0
- `shellcheck --severity=error packages/workspace/lib/trackers/beads-ready packages/workspace/lib/trackers/ado-ready` → exit 0
- Functional smoke: `packages/workspace/lib/trackers/beads-ready priceless | python3 -c 'import sys,json; d=json.load(sys.stdin); print(type(d).__name__, len(d))'` → `list <N>` (N >= 0; stderr may show per-project warnings — those are contract-legal)
- Env-override still honored: `WSENV_REGISTRY=/nonexistent packages/workspace/lib/trackers/beads-ready priceless; echo "exit=$?"` → stderr `beads-ready: registry not found: /nonexistent`, `exit=1`

### Step 5: WSP-05 — fix wsenv --help org vocabulary

In `packages/workspace/bin/wsenv`, three single-line comment edits:
- line 9: `(b-and-b / client / personal)` → `(b-and-b / priceless / personal)`
- line 25: `(b-and-b|client|personal)` → `(b-and-b|priceless|personal)`
- line 42: `#   wsenv --org oo                   # -> client` → `# -> priceless`

**Verify**:
- `grep -n 'client' packages/workspace/bin/wsenv` → no output, exit 1
- `~/.local/bin/wsenv --help | grep -c 'priceless'` → `3`
- `~/.local/bin/wsenv --org oo` → `priceless`

### Step 6: WSP-07 — delete the dead WS_CODE export

In `packages/workspace/bin/wsenv`, delete line 201 (`    echo "export WS_CODE=$CODE"`)
in the `activate)` branch. Do NOT touch the adjacent `WS_WORKSPACE` line (it has a live
consumer, chpwd.zsh:31) or the PATH-strip line below it.

**Verify**:
- `grep -rn 'WS_CODE' packages/ scripts/ home/` → no output, exit 1
- `~/.local/bin/wsenv if | grep -c 'WS_WORKSPACE'` → `1` (activate output intact)
- `~/.local/bin/wsenv if | grep -c 'WS_CODE'` → `0` (grep exits 1)
- `bash -n packages/workspace/bin/wsenv` → exit 0

### Step 7: WSP-06 — fix the remaining stale bd-ready name

In `packages/workspace/profiles/priceless/profile.toml` line 8, change
`packages/workspace/lib/trackers/bd-ready` to `packages/workspace/lib/trackers/beads-ready`.
(ado-ready:58 was already removed by Step 4b.)

**Verify**: `grep -rn 'bd-ready' --exclude-dir=.git --exclude-dir=plans .` → no output,
exit 1. (`plans/` is excluded because THIS plan file cites the stale name.)

### Step 8: WSP-04 — extend check.sh SH_FILES to cover the workspace package

In `scripts/check.sh`, extend ONLY the mapfile block (lines 57-62) by adding one find
clause inside the process substitution:

```bash
mapfile -d '' SH_FILES < <(
    find scripts -name '*.sh' -type f -print0
    find ssh-mesh/scripts -name '*.sh' -type f \
        -not -path 'ssh-mesh/scripts/remote/cmux-bridge/*' -print0
    find packages/workspace/bin packages/workspace/lib/trackers \
        -type f -not -name '*.md' -print0
)
```

The `platform/*.sh` loop below the block stays untouched. `-not -name '*.md'` matters:
`lib/trackers/README.md` would otherwise be fed to `bash -n`. Do NOT add anything to
`SHELLCHECK_EXCLUDE` — the pre-run found only SC2259, which Step 3 fixed for real.

**Verify**: `bash scripts/check.sh; echo "exit=$?"` →
- `PASS: sh-syntax (51 bash + hooks)` (42 baseline + 9 workspace scripts; 50 if Option B deleted wk-doctor)
- `PASS: shellcheck (49 checked, 2 excluded)` (48 if Option B)
- `ALL CHECKS PASSED`, `exit=0`

If shellcheck reports NEW error-severity findings not predicted here (e.g. a different
shellcheck version than the plan-time one): triage each finding on its merits — fix real
bugs in in-scope files, and STOP and report if a finding is in an out-of-scope file, is
not understood, or would need a `SHELLCHECK_EXCLUDE` entry. Never silence to get green.

### Step 9: Full gate + smoke + commit

1. `bash scripts/check.sh` → exit 0 (final confirmation after all edits).
2. Smoke runs (all listed under Done criteria) pass.
3. `git status --short` → ONLY in-scope files modified/created.
4. Update this plan's row in `plans/README.md` (status DONE + `spec-impact: none`,
   matching the ledger rule at the top of that file).
5. Stage exactly the touched files + `plans/README.md` (+ `.beads/interactions.jsonl` if
   bd changed it), single commit, push per operator instruction.

**Verify**: `git log --oneline -1` shows the commit; `git status` clean (or clean except
intentionally-unpushed state).

## Test plan

This repo has no unit-test framework for shell — the gate IS `scripts/check.sh`, and this
plan extends that gate to cover the code it touches (Step 8), which is the repo's
established pattern (see check.sh's own header, lines 1-12, and its sections as the
structure to mimic if any new check were ever needed — none is here). Concretely:

- **Regression coverage added**: the 9 workspace scripts enter `SH_FILES`, so every
  future edit to them gets `bash -n` + `shellcheck --severity=error` on every
  `bash scripts/check.sh` run. The SC2259 bug class this plan fixes is exactly what that
  coverage catches (verified: plan-time shellcheck flags the pre-fix ado-ready:125).
- **Behavioral checks** (no framework, direct invocation):
  - stdin-flow repro in Step 3 (old form prints 0 bytes seen; new form prints 11),
  - `beads-ready priceless` emits a parseable JSON list (Step 4),
  - `wsenv --org oo` → `priceless`; `wsenv if` activate output drops WS_CODE, keeps
    WS_WORKSPACE (Steps 5-6),
  - Option A: `wk doctor if --json` resolves code `if` (Step 1).
- Final: `bash scripts/check.sh` → exit 0 with the new counts (51 / 49-checked, or
  50 / 48 under Option B).

## Done criteria

Machine-checkable. ALL must hold (from repo root):

- [ ] `bash scripts/check.sh; echo $?` → `ALL CHECKS PASSED`, `0`, with
      `sh-syntax (51 bash + hooks)` and `shellcheck (49 checked, 2 excluded)`
      (50/48 iff operator chose Option B)
- [ ] Step 1 resolved via explicit operator choice; Option A: `command -v wk-doctor`
      → `/home/nyaptor/.local/bin/wk-doctor` and `wk doctor if --json` exits 0
- [ ] `shellcheck --severity=error packages/workspace/lib/trackers/ado-ready; echo $?` → `0`
- [ ] `grep -c 'python3.14' packages/workspace/lib/trackers/ado-ready packages/workspace/lib/trackers/beads-ready` → `0` in both (probe loops gone)
- [ ] `grep -rn 'bd-ready' --exclude-dir=.git --exclude-dir=plans .; echo $?` → `1` (no matches)
- [ ] `grep -n 'client' packages/workspace/bin/wsenv; echo $?` → `1`
- [ ] `grep -rn 'WS_CODE' packages/ scripts/ home/; echo $?` → `1`
- [ ] `grep -nE 'promotes only on pass|Machine-local|validated at generation' packages/workspace/README.md; echo $?` → `1`
- [ ] `~/.local/bin/wsenv --org oo` → `priceless`; `~/.local/bin/wsenv if | grep -c WS_WORKSPACE` → `1`
- [ ] `~/.local/bin/wk ready priceless | head -c 1` → `[`
- [ ] `git status --short` shows no files outside the Scope in-scope list
- [ ] `plans/README.md` status row for 010 updated (with `spec-impact:` token)

## STOP conditions

Stop and report back (do not improvise) if:

- The operator has not chosen Option A or B for Step 1 — present both (recommendation:
  A) and wait. Never delete wk-doctor without an explicit instruction naming Option B.
- The drift check shows any in-scope file changed since `9399b92`, and its live content
  no longer matches a "Current state" excerpt this plan edits by line number.
- `scripts/check.sh` at Step 0 does NOT report the 42/40 baseline (another concurrent
  plan may have landed a change to a covered file — re-baseline only if the gate still
  exits 0; a failing baseline is a hard STOP).
- Step 8 surfaces a shellcheck error-severity finding in a file OUTSIDE this plan's
  in-scope list, or one you cannot confidently classify as a real bug vs false positive.
- The Step 3 repro shows the NEW `-c "$VAR"` form still losing stdin (would mean an
  unexpected shell/python behavior on this machine — do not ship the rewrite blind).
- Fixing anything appears to require touching `scripts/lib/registry.sh`, `wk-ready`,
  `generate-profiles`, or any check.sh section other than the SH_FILES block.
- A verification fails twice after a reasonable fix attempt.

## Maintenance notes

- **WS_CODE residual risk** (why the audit tempered confidence): an exported env var is
  the one artifact a static sweep can miss — an unseen child process could read it. The
  sweep covered this repo, ~/dev/cc, deployed ~/.zsh, starship/tmux configs, and the
  profile root; nothing reads it, and it arrived (repo commit `94983db`) with no consumer
  ever landing. If a future prompt segment or tool wants "current project code in env",
  re-add the export next to `WS_WORKSPACE` (wsenv activate branch) — one line.
- **Trackers now source registry.sh**: any NEW tracker adapter must do the same
  (docs/plans/005 maintenance mandate). The `WSENV_REGISTRY` override in beads-ready is
  intentionally distinct from registry.sh's `PROJECTS_REGISTRY`; if these are ever
  unified, update the header docs in beads-ready:36, wk-ready:24, and wsenv:46 together.
- **check.sh coverage**: `packages/workspace/**` scripts are now gate-covered. If a
  non-bash file (other than `*.md`) ever lands in `bin/` or `lib/trackers/`, the find
  clause in Step 8 will feed it to `bash -n` — extend the exclusion then, not now.
- **README truthfulness**: `.claude/workflows/project-mgmt-audit.js:89` diffs
  `packages/workspace/README.md` against the code on every audit run — future README
  edits about generate-profiles/validation must stay behavior-accurate or that workflow
  re-flags them.
- **Reviewer scrutiny points**: (1) the ado-ready python body must be byte-identical
  across the Step 3 mechanism change; (2) beads-ready's exit-1 vs ado-ready's
  `[]`+exit-0 failure modes must survive Step 4 (they are contractually different);
  (3) confirm no `SHELLCHECK_EXCLUDE` additions snuck in.
- **Deferred, deliberately**: renaming `WSENV_REGISTRY` → `PROJECTS_REGISTRY` (documented
  API, separate decision); making generate-profiles validate (rejected alternative in
  Step 2); any Option B deletion work beyond an explicit operator instruction.
