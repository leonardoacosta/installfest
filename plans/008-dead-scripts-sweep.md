# Plan 008: Dead scripts/ sweep — delete cmux-debug + ani-cli, gate dbpro/youtube-transcript/setup-az-wrapper, fix stale install-arch header

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**:
> ```bash
> git diff --stat 9399b92..HEAD -- scripts/cmux-debug.sh scripts/ani-cli.sh \
>   scripts/dbpro.sh scripts/youtube-transcript.sh scripts/setup-az-wrapper.sh \
>   scripts/install-arch.sh .claude/workflows/project-mgmt-audit.js \
>   docs/cloudpc-proxy-setup.md home/dot_local/bin/executable_az
> ```
> If any in-scope file changed since this plan was written, compare the
> "Current state" excerpts against the live code before proceeding; on a
> mismatch, treat it as a STOP condition.

## Status

- **Priority**: P2
- **Effort**: S
- **Risk**: LOW (deletions of files with verified-zero callers; one comment fix; the only judgment items sit behind an explicit operator gate)
- **Depends on**: none
- **Category**: tech-debt
- **Planned at**: commit `9399b92`, 2026-07-14

## Why this matters

An entropy audit of `scripts/` liveness found that 17 of 22 root scripts are live with a
concrete wiring anchor (chezmoi symlink, run_once/run_onchange source, systemd unit,
LaunchAgent, zsh alias, or a live script caller), but 5 sit entirely outside every wiring
surface: `cmux-debug.sh`, `ani-cli.sh`, `dbpro.sh`, `youtube-transcript.sh`, and
`setup-az-wrapper.sh`. Dead scripts in `scripts/` cost every future liveness audit a
re-derivation of their deadness (this is at least the second audit to find them), keep
shellcheck/`bash -n` cycles spent on unreachable code, and — in setup-az-wrapper's case —
carry a **drifted copy** of the deployed az wrapper's binary-discovery loop that can bless a
different `az` binary than the wrapper actually uses. This plan deletes the two zero-risk
files outright, fixes a header comment that actively misleads liveness tracing
(`install-arch.sh:3` names a caller that no longer exists in the repo), and puts the three
judgment-call files behind an explicit delete-vs-promote operator decision so they stop
living in the ambiguous middle.

## Current state

All excerpts below are from fresh reads at commit `9399b92` (clean tree).

### Files and their roles

- `scripts/cmux-debug.sh` — 53-line throwaway cmux debug harness (`# Debug version — shows every step`, line 2). Zero callers.
- `scripts/ani-cli.sh` — 48-line installer that defines `install_ani_cli()` (line 12) and **never calls it** — the file ends at line 48 with the function's closing brace, no self-exec block. Running the file is a no-op. Orphaned when the old `install.sh` (its only historical caller, severed at commit `b7f87f8`, chezmoi migration) was deleted.
- `scripts/dbpro.sh` — 83-line interactive macOS DB Pro DMG installer. Has a direct-run guard (lines 80–83) so it only acts when explicitly invoked; contains an interactive `read -p "Reinstall? [y/n]"` (line 19) that would hang any non-interactive run. Zero callers.
- `scripts/youtube-transcript.sh` — 97-line installer for the `youtube_transcript` C tool. Self-executes at file scope (lines 94–97: `if check_dependencies; then install_youtube_transcript; fi` — no BASH_SOURCE guard), but nothing sources or runs it. Zero callers.
- `scripts/setup-az-wrapper.sh` — 101-line first-time setup for the smart `az` wrapper (dual Azure identity device-code login). Referenced ONLY from documentation: `docs/cloudpc-proxy-setup.md:207`. Its lines 21–28 duplicate the deployed wrapper's REAL_AZ discovery loop with a **different precedence order** (see below).
- `home/dot_local/bin/executable_az` — the deployed az wrapper (chezmoi deploys it to `~/.local/bin/az`). Its REAL_AZ loop at lines 38–45 is the runtime source of truth. DO NOT modify this file — sync the setup script TO it, never the reverse.
- `scripts/install-arch.sh` — live Arch installer, sourced by `home/run_once_install-packages.sh.tmpl:124`. Line 3 header comment falsely says `# Sourced by install.sh on Linux` — `install.sh` does not exist anywhere in the repo (`git ls-files | grep install.sh` matches only `brew-install.sh` and `install-arch.sh`).
- `.claude/workflows/project-mgmt-audit.js` — audit workflow; its line 85 prompt lists `scripts/cmux-debug.sh` in a `Files:` inventory and itself states "cmux-debug.sh has no callers". Must be updated when cmux-debug.sh is deleted so no non-docs reference dangles.
- `docs/cloudpc-proxy-setup.md` — CloudPC proxy runbook; its "First-Time Setup" section (lines 204–211) tells the reader to run `scripts/setup-az-wrapper.sh`. Only needs editing in the delete branch of the operator gate.
- `scripts/check.sh` — the repo quality gate. Its `SH_FILES` set is built by `find scripts -name '*.sh'` (line 58), so every deleted script automatically leaves the lint set; no check.sh edit is needed. None of the five files is in its `SHELLCHECK_EXCLUDE` list (lines 41–44 — only `mux-remote.sh` and `gk-github-auth.sh`).

### Key excerpts

`scripts/ani-cli.sh:12` and end-of-file (no invocation anywhere after the definition):

```bash
install_ani_cli() {
    info "Installing ani-cli..."
    ...
    if command -v ani-cli &>/dev/null; then
        success "ani-cli installed: $(ani-cli --version 2>/dev/null || echo 'ok')"
    else
        error "Installation failed - ani-cli not found in PATH"
        return 1
    fi
}
```

`scripts/dbpro.sh:80-83` (direct-run guard — inert unless manually invoked):

```bash
# Run if executed directly
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
    install_dbpro
fi
```

`scripts/youtube-transcript.sh:94-97` (file-scope self-exec, but zero callers):

```bash
# Main
if check_dependencies; then
    install_youtube_transcript
fi
```

`scripts/setup-az-wrapper.sh:21-28` — the drifted copy (order: pipx, **homebrew**, /usr/local, /usr/bin):

```bash
REAL_AZ=""
for candidate in \
    "$HOME/.local/share/pipx/venvs/azure-cli/bin/az" \
    "/opt/homebrew/bin/az" \
    "/usr/local/bin/az" \
    "/usr/bin/az"; do
    [ -x "$candidate" ] && REAL_AZ="$candidate" && break
done
```

`home/dot_local/bin/executable_az:38-45` — the deployed truth (order: pipx, **/usr/bin**, /usr/local, homebrew):

```bash
REAL_AZ=""
for candidate in \
    "$HOME/.local/share/pipx/venvs/azure-cli/bin/az" \
    "/usr/bin/az" \
    "/usr/local/bin/az" \
    "/opt/homebrew/bin/az"; do
    [ -x "$candidate" ] && REAL_AZ="$candidate" && break
done
```

On a machine with both `/usr/bin/az` and `/opt/homebrew/bin/az` but no pipx install, setup
blesses homebrew's az while every subsequent wrapper call uses `/usr/bin/az` — version-skew
confusion, though not broken auth (AZURE_CONFIG_DIR state is shared).

`scripts/install-arch.sh:1-3` — the stale header:

```bash
#!/usr/bin/env bash
# install-arch.sh - Arch Linux specific installation
# Sourced by install.sh on Linux
```

Actual caller: `home/run_once_install-packages.sh.tmpl:124` — `. "$DOTFILES/scripts/install-arch.sh"`.

`docs/cloudpc-proxy-setup.md:204-211` (the section the delete branch must rewrite):

```markdown
### First-Time Setup

```bash
scripts/setup-az-wrapper.sh
```

This creates config directories, verifies dependencies, and runs device-code login for both
identities. Interactive — requires browser sign-in.
```

### Repo conventions that apply

- This is a chezmoi dotfiles repo; `scripts/` is repo-only (never deployed). Liveness for a
  root script means one of: `home/dot_local/bin/symlink_*.tmpl`, a `run_once/run_onchange_*.sh.tmpl`
  source line, a systemd user unit / LaunchAgent, a zsh alias in `home/dot_zsh/rc/`, git-config
  helper registration, or a live cross-script caller. None of the five files has any.
- Quality gate: `bash scripts/check.sh` (also `npm run check`) — zsh -n, bash -n, chezmoi
  template render, shellcheck severity=error, terraform validate when initialized. Exit 0 = pass.
- Commit style: conventional commits, e.g. `fix(cc-tmux): extract_active picks freshest isActive credential` (from `git log`). Single commit, targeted `git add <files> .beads/`, then push. Two commits = two CI builds — do not split.
- Git hooks: `core.hooksPath=.beads/hooks` on this machine (bd-managed). `.beads/issues.jsonl`
  is gitignored here (Dolt push is the sync mechanism); a stderr warning
  `auto-export: git add failed` from bd during commit is expected noise, not a failure.
- Deleted files stay recoverable via git history — deletion is not data loss.

## Commands you will need

| Purpose | Command | Expected on success |
|---------|---------|---------------------|
| Quality gate | `bash scripts/check.sh` | last line `==> ALL CHECKS PASSED`, exit 0 |
| Liveness grep (per basename) | `grep -rn "<basename>" --exclude-dir=.git --exclude-dir=docs --exclude-dir=.beads --exclude-dir=node_modules .` | see per-step expectations |
| Tree state | `git status --porcelain` | only the files this plan names |

Run everything from the repo root: `/home/nyaptor/dev/personal/installfest`.

## Scope

**In scope** (the only files you may modify/delete):

- `scripts/cmux-debug.sh` (delete)
- `scripts/ani-cli.sh` (delete)
- `.claude/workflows/project-mgmt-audit.js` (edit line 85 only — remove cmux-debug.sh refs)
- `scripts/install-arch.sh` (edit line 3 comment only; plus the line-57 comment only in gate branch 6A-yt)
- `scripts/dbpro.sh` (delete — ONLY behind the Step 6 operator gate)
- `scripts/youtube-transcript.sh` (delete — ONLY behind the Step 6 operator gate)
- `scripts/setup-az-wrapper.sh` (delete OR edit lines 22–26 — ONLY behind the Step 6 operator gate)
- `docs/cloudpc-proxy-setup.md` (edit lines 204–211 — ONLY if setup-az-wrapper.sh is deleted)
- `plans/README.md` (status row)

**Out of scope** (do NOT touch, even though they look related):

- `home/dot_local/bin/executable_az` — the deployed wrapper is the runtime source of truth;
  the sync direction is setup-script -> wrapper, never the reverse.
- `scripts/brew-install.sh:51-64` and `scripts/install-arch.sh:119-140` — the
  `install_azure_devops_extension` block is duplicated across the two platform installers
  (2 sites, below the 3+ consolidation threshold; the copies have already drifted slightly
  in the az-missing branch message). Recorded decision: leave the duplication alone. Note
  only — do not consolidate, do not "fix while you're in there".
- `scripts/gopen.sh`, `iopen.sh`, `ideopen.sh`, `mopen.sh`, `sopen.sh`, `mac-open.sh`,
  `ropen.sh`, `view.sh` — the *open family is owned by plan 009.
- `scripts/hooks/*` — owned by plan 012.
- `scripts/cmux-workspaces.sh`, `scripts/mux-remote.sh`, `scripts/cmux-bridge.py` — all
  verified LIVE (cmux-bridge.py is called by mac-open.sh; mac-open.sh coexistence with ropen
  is recorded decision if-34u). Do not sweep them in by pattern-matching on "cmux".
- `docs/audit/*.md`, `docs/plans/*.md` — historical audit/plan records; stale references to
  deleted scripts inside `docs/` are acceptable per the done criteria and MUST NOT be
  mass-edited here.
- `scripts/check.sh` — no edit needed; its file set is built by `find` at runtime.

## Git workflow

- Work directly on the current branch (`main`) — this repo commits ad-hoc work straight to
  main (see `git log`).
- ONE commit at the end covering all changes, message:
  `chore(scripts): delete dead scripts, fix stale install-arch header (plan 008)`
  — adjust the subject if the Step 6 gate changes what shipped.
- Stage with targeted paths only (`git add <each file> plans/README.md`), never `git add .`
  or a bare directory. `git rm` already stages deletions.
- Push after commit (repo convention: work is not done until push succeeds). If push fails
  3x, STOP and report.

## Steps

### Step 1: Pre-delete liveness re-verification

For each of the five candidate files, prove zero non-docs callers still holds at execution
time (the audit verified this at `9399b92`; re-verify in case of drift):

```bash
cd /home/nyaptor/dev/personal/installfest
for name in cmux-debug ani-cli dbpro youtube-transcript setup-az-wrapper; do
  echo "=== $name ==="
  grep -rn "$name" --exclude-dir=.git --exclude-dir=docs --exclude-dir=.beads --exclude-dir=node_modules .
done
```

**Verify** — expected hits, and NOTHING else:
- `cmux-debug`: `scripts/cmux-debug.sh` (self) + `.claude/workflows/project-mgmt-audit.js:85` (removed in Step 3) + possibly `plans/` files (this plan itself — plan-file self-references are fine).
- `ani-cli`: `scripts/ani-cli.sh` (self) only.
- `dbpro`: `scripts/dbpro.sh` (self) only.
- `youtube-transcript`: `scripts/youtube-transcript.sh` (self) only. (Note: `scripts/install-arch.sh:57` mentions `youtube_transcript` with an underscore — a comment about the C *tool*, handled in Step 6.)
- `setup-az-wrapper`: `scripts/setup-az-wrapper.sh` (self) only (the docs/ hit at `docs/cloudpc-proxy-setup.md:207` is excluded by `--exclude-dir=docs`; it is handled in Step 6).

If any OTHER file shows up (a new alias, a run_once source, a symlink template, a script
caller), that file is no longer dead — STOP and report.

### Step 2: Delete the two zero-risk files

```bash
git rm scripts/cmux-debug.sh scripts/ani-cli.sh
```

Rationale inline for the reviewer: cmux-debug.sh has no callers (the repo's own audit
workflow prompt says so); ani-cli.sh never invokes its own function, so even executing it
is a no-op — nothing can be depending on either.

**Verify**: `git status --porcelain` → exactly two `D ` lines for those paths (plus anything from later steps).

### Step 3: Remove the cmux-debug.sh references from the audit workflow prompt

Edit `.claude/workflows/project-mgmt-audit.js` line 85 (the `launchers` prompt). Two
surgical string edits inside that one line:

1. In the `Files:` list, delete the item `scripts/cmux-debug.sh, ` (currently reads
   `Files: scripts/generate-raycast.sh, scripts/cmux-workspaces.sh, scripts/mux-remote.sh, scripts/cmux-debug.sh, packages/workspace/integrations/.`).
2. In the `Judge:` clause, delete `dead code (cmux-debug.sh has no callers), ` (currently
   reads `Judge: registry-consumption consistency across the three scripts, dead code (cmux-debug.sh has no callers), and whether the prune gap...`).

Change nothing else in the file.

**Verify**: `grep -c "cmux-debug" .claude/workflows/project-mgmt-audit.js` → `0`
(grep exits 1 with output `0`; that is the pass state).

### Step 4: Fix the stale install-arch.sh header

Edit `scripts/install-arch.sh` line 3:

```bash
# Sourced by install.sh on Linux
```

becomes

```bash
# Sourced by home/run_once_install-packages.sh.tmpl on Linux (chezmoi run_once)
```

**Verify**: `sed -n '3p' scripts/install-arch.sh` → the new comment;
`grep -n "install.sh on Linux" scripts/install-arch.sh` → no match.

### Step 5: Mid-plan quality gate

```bash
bash scripts/check.sh
```

**Verify**: last line `==> ALL CHECKS PASSED`, `echo $?` → `0`. Section 2 (sh-syntax) and
Section 4 (shellcheck) will report 2 fewer files than before the deletions — that is expected.

### Step 6: OPERATOR GATE — dbpro.sh, youtube-transcript.sh, setup-az-wrapper.sh

**This is a STOP-and-report point, not a silent batch step.** If the operator's
delete-vs-promote decision was NOT provided in your dispatch instructions, stop here,
commit nothing yet, and report the following question with this recommendation:

> Three scripts have zero live callers but are plausibly manual tools. Per file:
> **delete** (git history retains) or **promote to documented runbook tool**?
>
> Recommendation:
> - `scripts/dbpro.sh` — **DELETE**. Interactive macOS-only DMG installer pinned to
>   DB Pro 1.6.1 (stale version, line 9); no doc references it; residual use case is
>   undocumented manual invocation only.
> - `scripts/youtube-transcript.sh` — **DELETE**. Source-build installer for an optional
>   C tool; its only documented caller (`install.sh` per docs/audit/bootstrap.md:59) was
>   deleted in the chezmoi migration; no doc tells anyone to run it.
> - `scripts/setup-az-wrapper.sh` — **KEEP + SYNC**. It already has a live documented
>   invocation path (`docs/cloudpc-proxy-setup.md:207`, the CloudPC First-Time Setup
>   runbook) and encodes non-trivial flow (proxy detection, tunnel check, dual
>   device-code login, identity verification) that would bloat the doc if inlined.
>   Keeping it requires syncing its REAL_AZ candidate order to the deployed wrapper.

Then execute the decided branch per file:

#### Branch DELETE — dbpro.sh

```bash
git rm scripts/dbpro.sh
```

**Verify**: `grep -rn "dbpro" --exclude-dir=.git --exclude-dir=docs --exclude-dir=.beads --exclude-dir=node_modules .` → no hits outside `plans/`.

#### Branch DELETE — youtube-transcript.sh (6A-yt)

```bash
git rm scripts/youtube-transcript.sh
```

Also edit the now-orphaned comment `scripts/install-arch.sh:57`
(`# Build tools (for compiling C tools like youtube_transcript)`) to
`# Build tools` — the example tool's installer no longer exists. Keep the
`base-devel`/`curl` package entries themselves; other things still need build tools.

**Verify**: `grep -rn "youtube" --exclude-dir=.git --exclude-dir=docs --exclude-dir=.beads --exclude-dir=node_modules .` → no hits outside `plans/`.

#### Branch DELETE — setup-az-wrapper.sh

```bash
git rm scripts/setup-az-wrapper.sh
```

Then rewrite `docs/cloudpc-proxy-setup.md` lines 204–211 ("First-Time Setup") so the doc
no longer points at a deleted script. Replace the fenced block and its trailing paragraph
with inlined manual steps (the deployed `az` wrapper handles identity routing and proxying
itself — see the doc's own "Override Flags" and "Manual Login" sections directly above/below):

```markdown
### First-Time Setup

```bash
mkdir -p ~/.azure-bbadmin ~/.azure-o365
az login --use-device-code --as-admin    # BBAdmin identity
az login --use-device-code --as-o365     # O365 identity
```

Interactive — requires browser sign-in. Ensure the SOCKS tunnel is running first
(see Platform Comparison below for the per-OS start command).
```

**Verify**: `grep -rn "setup-az-wrapper" --exclude-dir=.git --exclude-dir=.beads .` → no hits outside `plans/` (note: docs are NOT excluded on this one — the doc edit must remove the reference).

#### Branch KEEP — setup-az-wrapper.sh (recommended)

Edit `scripts/setup-az-wrapper.sh` lines 22–26 to match the deployed wrapper's candidate
order exactly (`home/dot_local/bin/executable_az:38-45` is the source of truth):

```bash
for candidate in \
    "$HOME/.local/share/pipx/venvs/azure-cli/bin/az" \
    "/usr/bin/az" \
    "/usr/local/bin/az" \
    "/opt/homebrew/bin/az"; do
```

Optionally add one comment line above the loop:
`# Candidate order MUST match home/dot_local/bin/executable_az (the deployed wrapper).`
Do not change anything else in the file. `docs/cloudpc-proxy-setup.md` stays untouched.

**Verify**:

```bash
diff <(sed -n '/^for candidate in/,/^done/p' scripts/setup-az-wrapper.sh | grep '"/.*az"' ) \
     <(sed -n '/^for candidate in/,/^done/p' home/dot_local/bin/executable_az | grep '"/.*az"')
```

→ empty output, exit 0 (identical candidate lists in identical order).

#### Branch KEEP — dbpro.sh / youtube-transcript.sh (only if operator overrides the recommendation)

A kept script MUST NOT stay in the ambiguous middle: add a one-line invocation note to the
script's own header comment stating it is a manual, optional tool run by hand
(e.g. `# Manual optional tool — run directly: bash scripts/dbpro.sh (macOS only)`).
Do not create new docs files for this.

### Step 7: Final gate, final grep, ledger, commit

```bash
bash scripts/check.sh
```

→ `==> ALL CHECKS PASSED`, exit 0.

```bash
git status --porcelain
```

→ only files named in the Scope "in scope" list.

Update `plans/README.md`: add (or update, if a wave-2 index section already exists from a
concurrent plan) a row for plan 008 with Status DONE and a `spec-impact: none` token per
the ledger rule at the top of that file. If the Step 6 gate is still unanswered and you
stopped there, set Status `BLOCKED (awaiting operator delete-vs-promote decision, Step 6)`
and commit only Steps 2–5.

Then single commit + push:

```bash
git add scripts/install-arch.sh .claude/workflows/project-mgmt-audit.js plans/README.md
# plus, per Step 6 branch taken: scripts/setup-az-wrapper.sh and/or docs/cloudpc-proxy-setup.md
# (git rm already staged the deletions)
git commit -m "chore(scripts): delete dead scripts, fix stale install-arch header (plan 008)"
git push
```

**Verify**: `git status --porcelain` → empty; `git log --oneline -1` → the new commit.

## Test plan

This repo has no unit-test suite for `scripts/` — the executable specification is
`scripts/check.sh` (the same gate every prior plan in `plans/README.md` used; see plan 001's
done criteria for the pattern). Coverage for this plan:

- `bash scripts/check.sh` after Step 5 and again after Step 7 → exit 0 both times. This
  exercises `bash -n` + shellcheck on every remaining `scripts/*.sh` (proving no deleted
  file was sourced by a surviving one — a dangling `. "$DOTFILES/scripts/<deleted>.sh"`
  would still pass syntax but the Step 1 grep already excludes that) and chezmoi template
  render on all of `home/` (proving no `run_once/run_onchange` template referenced a
  deleted script).
- The per-basename greps in Steps 1, 6, and Done criteria are the regression assertion
  that no live reference survives.
- cc-tmux is untouched by this plan, so `cc-tmux self-test` is not required here (it is
  required for plans touching `apps/cc-tmux/`).

## Done criteria

Machine-checkable. ALL must hold:

- [ ] `bash scripts/check.sh` exits 0 (`==> ALL CHECKS PASSED`)
- [ ] `scripts/cmux-debug.sh` and `scripts/ani-cli.sh` do not exist (`ls scripts/cmux-debug.sh scripts/ani-cli.sh` → both "No such file")
- [ ] For every deleted basename: `grep -rn "<basename>" --exclude-dir=.git --exclude-dir=docs --exclude-dir=.beads --exclude-dir=node_modules .` returns hits only inside `plans/` (this plan file / ledger). For a deleted `setup-az-wrapper.sh` specifically, the grep WITHOUT `--exclude-dir=docs` must also be clean outside `plans/` (the doc repoint is mandatory in that branch)
- [ ] `grep -c "cmux-debug" .claude/workflows/project-mgmt-audit.js` → 0
- [ ] `sed -n '3p' scripts/install-arch.sh` → mentions `run_once_install-packages.sh.tmpl`, not `install.sh`
- [ ] If setup-az-wrapper.sh was KEPT: the candidate-order diff in Step 6 Branch KEEP is empty
- [ ] Step 6 was either executed with an explicit operator decision or reported as BLOCKED — never silently defaulted
- [ ] `git status --porcelain` empty after commit; no files outside the in-scope list were modified
- [ ] `plans/README.md` status row updated (with `spec-impact: none`)

## STOP conditions

Stop and report back (do not improvise) if:

- The Step 1 grep finds any non-docs, non-plans reference to a candidate file beyond the
  expected self + workflow-prompt hits — the file has gained a caller since `9399b92`.
- The drift check shows any in-scope file changed since `9399b92` and its live content no
  longer matches the "Current state" excerpts (e.g. `executable_az`'s candidate order
  changed — the sync target moved).
- You reach Step 6 without an operator decision in your dispatch instructions (report the
  question + recommendation verbatim; commit Steps 2–5 only, mark the ledger row BLOCKED).
- `bash scripts/check.sh` fails twice after a reasonable fix attempt at any gate.
- The fix appears to require touching an out-of-scope file (e.g. an *open-family script,
  `scripts/hooks/*`, or `home/dot_local/bin/executable_az`).
- `git push` fails 3 times.
- Bash tool returns empty-output failures repeatedly (documented transient flakiness in
  this repo): verify file state with Read and retry — but if a `git rm`/`git commit`
  outcome cannot be confirmed after retries, stop and report rather than re-running
  mutations blind.

## Maintenance notes

- **az-devops-extension duplication (NOT fixed here, by design)**: the ~10-line
  check/add block exists in both `scripts/brew-install.sh:51-64`
  (`install_azure_devops_extension()`) and inline in `scripts/install-arch.sh:129-139`.
  Two sites is below the consolidation threshold; the copies have already drifted in the
  az-missing message ("not found" vs "not available"). Whoever next makes a REAL change to
  either installer should consolidate into `scripts/utils.sh` then — not before.
- **If setup-az-wrapper.sh was kept**: any future edit to `home/dot_local/bin/executable_az`'s
  REAL_AZ loop must be mirrored into `scripts/setup-az-wrapper.sh:21-28` (the comment added
  in Step 6 Branch KEEP marks this). A shared sourced snippet is only worth it if a third
  copy ever appears.
- **docs/ staleness is accepted**: `docs/audit/*.md`, `docs/plans/README.md:71` (which
  already tracked the cmux-debug deletion as a quick win — this plan closes it), and
  `docs/plans/006-project-mgmt-audit-workflow.md:364` still mention deleted scripts. They
  are historical records; a future docs sweep may prune them, this plan must not.
- **Reviewer scrutiny points**: (1) the `.claude/workflows/project-mgmt-audit.js` edit is
  string-surgery inside a JS template literal — confirm the file still parses
  (`node --check .claude/workflows/project-mgmt-audit.js` → exit 0); (2) in the
  setup-az-wrapper DELETE branch, confirm the rewritten doc section's login commands match
  the wrapper's actual flags (`--as-admin` / `--as-o365`, see `docs/cloudpc-proxy-setup.md`
  "Override Flags" section).
- **Deferred**: nothing else in `scripts/` is dead — the audit verified the remaining 17
  root scripts live with concrete wiring anchors. The next liveness audit can start from
  that baseline instead of re-sweeping.
