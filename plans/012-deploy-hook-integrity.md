# Plan 012: Auto-deploy hooks — revive commit-time deploy under beads hooksPath and de-drift the post-merge injection

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**:
> `git -C /home/nyaptor/dev/personal/installfest diff --stat 9399b92..HEAD -- home/run_onchange_set-git-hooks.sh.tmpl home/run_onchange_after_ado-auth.sh.tmpl scripts/hooks/post-commit .beads/hooks/post-merge`
> If any of those files changed since this plan was written, compare the
> "Current state" excerpts against the live code before proceeding; on a
> mismatch, treat it as a STOP condition.

## Status

- **Priority**: P1
- **Effort**: S
- **Risk**: MED (mutates live git hooks on the executing machine; runtime verification is mandatory)
- **Depends on**: none
- **Category**: bug
- **Planned at**: commit `9399b92`, 2026-07-14

## Why this matters

Commit-triggered chezmoi auto-deploy is **silently dead on every beads-managed
machine** (which is every machine that matters: this repo has
`core.hooksPath=.beads/hooks`, verified live). `.beads/hooks/` contains
post-checkout, post-merge, pre-commit, prepare-commit-msg, pre-push — **no
post-commit** — so the canonical `scripts/hooks/post-commit` (layer 1 of the
designed 3-layer deploy pipeline: commit → local apply) never fires. A local
commit that changes dotfiles leaves the local deployment stale until a pull or
a manual `chezmoi apply`, with no error surfaced anywhere. (ENTROPY-02,
adversarially CONFIRMED.)

Separately, the post-merge injection that DOES exist is a **drifted copy**: it
runs plain `chezmoi apply --no-tty`, while the canonical hook deliberately uses
`chezmoi init --source=... --apply` because plain `apply` never re-renders
`.chezmoi.toml.tmpl` `[data]` — so a pulled change to `$theme` (or any other
`[data]` value) deploys against a stale cached config. It also drops the
canonical hook's `CHEZMOI_SOURCE` guard. (ENTROPY-01, CONFIRMED.)

This plan makes both commit-time and merge-time deploy delegate to the one
canonical hook (`scripts/hooks/post-commit`), the same delegation pattern the
IF-DEPLOY and IF-PRECOMMIT blocks in the same template already use, and deletes
three assigned-never-read heredoc-marker variables plus one dead `DOTFILES`
assignment (ENTROPY-03).

## Current state

All excerpts are from commit `9399b92`; the working tree was clean at planning
time.

### Repo facts you need

- Dotfiles/dev-env repo managed by chezmoi; `.chezmoiroot` = `home/`, so
  `chezmoi source-path` returns `/home/nyaptor/dev/personal/installfest/home`
  (verified live).
- Quality gate: `bash scripts/check.sh` (also `npm run check`) = zsh -n on
  dot_zsh, bash -n on shell files, chezmoi template render + bash -n on every
  rendered `*.sh.tmpl`, shellcheck severity=error, terraform validate. Exit 0
  = all pass. **Verified green at 9399b92** (`PASS: template-render (55
  templates)`, `ALL CHECKS PASSED`, exit 0).
- Git hooks on this machine: `git config --get core.hooksPath` →
  `.beads/hooks` (bd-managed). Editing `.git/hooks/` is a silent no-op here.
- `.beads/hooks/*` hook files ARE git-tracked (`git ls-files .beads/` lists
  post-checkout, post-merge, pre-commit, prepare-commit-msg, pre-push) —
  mutations to them belong in the commit. `.beads/issues.jsonl` is gitignored
  (Dolt push is the sync mechanism); `.beads/interactions.jsonl` is tracked
  and committed explicitly when modified.
- Beads hook files carry a managed block
  (`# --- BEGIN BEADS INTEGRATION v0.63.3 ---` … `# --- END BEADS INTEGRATION
  v0.63.3 ---`). Beads only rewrites content between its own markers; injected
  content must sit AFTER the END marker (the template's existing pattern —
  `cat >>` appends to end of file — already satisfies this).

### File roles

- `home/run_onchange_set-git-hooks.sh.tmpl` — chezmoi run_onchange script;
  re-runs on any `chezmoi apply` after its rendered content changes. Sets
  `core.hooksPath` and idempotently appends marker-guarded delegation blocks
  into `.beads/hooks/*`. **Primary edit target.**
- `scripts/hooks/post-commit` — canonical local-deploy hook (beads import +
  `chezmoi init --apply`). `scripts/hooks/post-merge` is a symlink to it
  (verified executable). **Read-only for this plan — do not edit.**
- `.beads/hooks/post-merge` — live deployed hook containing the drifted
  IF-POSTMERGE v1 block. Will be rewritten at runtime by the fixed template.
- `.beads/hooks/post-commit` — **does not exist** at 9399b92. Will be created
  at runtime by the fixed template.
- `home/run_onchange_after_ado-auth.sh.tmpl` — 1Password→ADO PAT wiring;
  contains the dead `DOTFILES` assignment (line 21). One-line deletion only.

### Excerpt 1 — the template's dead marker vars and injection blocks

`home/run_onchange_set-git-hooks.sh.tmpl:26-28`:

```bash
BEADS_PREPUSH="$DOTFILES/.beads/hooks/pre-push"
MARKER_BEGIN="# --- BEGIN IF-DEPLOY v1 ---"
MARKER_END="# --- END IF-DEPLOY v1 ---"
```

`MARKER_END` (line 28) is assigned and never read — the heredoc at line 49
embeds the literal END string. Same pattern for `POSTMERGE_END` (line 59) and
`PRECOMMIT_END` (line 80). Verify yourself: `grep -n 'MARKER_END\|POSTMERGE_END\|PRECOMMIT_END' home/run_onchange_set-git-hooks.sh.tmpl`
returns exactly three lines (28, 59, 80) — the assignments, no reads.

### Excerpt 2 — the drifted IF-POSTMERGE block (ENTROPY-01)

`home/run_onchange_set-git-hooks.sh.tmpl:54-73`:

```bash
    # Also inject into post-merge so chezmoi apply fires after `git pull`
    # (mirrors the old scripts/hooks/post-commit behavior now that beads
    # owns the hooks dir).
    BEADS_POSTMERGE="$DOTFILES/.beads/hooks/post-merge"
    POSTMERGE_BEGIN="# --- BEGIN IF-POSTMERGE v1 ---"
    POSTMERGE_END="# --- END IF-POSTMERGE v1 ---"
    if [ -f "$BEADS_POSTMERGE" ] && ! grep -Fq "$POSTMERGE_BEGIN" "$BEADS_POSTMERGE"; then
        cat >> "$BEADS_POSTMERGE" <<'HOOK'

# --- BEGIN IF-POSTMERGE v1 ---
# Managed by chezmoi — runs chezmoi apply after successful merge/pull
# so downstream dotfile changes deploy without manual intervention.
if command -v chezmoi >/dev/null 2>&1; then
    chezmoi apply --no-tty 2>/dev/null && echo "chezmoi: applied dotfiles" || \
        echo "chezmoi: apply failed (run 'chezmoi apply' manually)" >&2
fi
# --- END IF-POSTMERGE v1 ---
HOOK
        echo "chezmoi: injected if-postmerge v1 into $BEADS_POSTMERGE"
    fi
```

Contrast with the canonical hook it claims to mirror,
`scripts/hooks/post-commit:33-47`:

```sh
if command -v chezmoi >/dev/null 2>&1; then
    REPO_ROOT="$(git rev-parse --show-toplevel 2>/dev/null)"
    CHEZMOI_SOURCE="$(chezmoi source-path 2>/dev/null)"
    # .chezmoiroot makes source-path return home/ subdir — match prefix
    case "$CHEZMOI_SOURCE" in "$REPO_ROOT"*)
        # init --apply (not plain apply): `apply` alone never re-renders
        # .chezmoi.toml.tmpl, so a commit that changes $theme (or any other
        # [data] value) would deploy against a stale cached config otherwise.
        if chezmoi init --source="$REPO_ROOT" --apply --no-tty 2>/dev/null; then
```

And with the delegation pattern the same template already uses for pre-push,
`home/run_onchange_set-git-hooks.sh.tmpl:44-48` (inside the IF-DEPLOY heredoc):

```bash
_if_deploy_root=$(git rev-parse --show-toplevel 2>/dev/null)
if [ -n "$_if_deploy_root" ] && [ -x "$_if_deploy_root/scripts/hooks/pre-push" ]; then
    "$_if_deploy_root/scripts/hooks/pre-push" "$@" || true
fi
unset _if_deploy_root
```

### Excerpt 3 — no post-commit anywhere under beads (ENTROPY-02)

Live state verified at planning time:

```
$ git -C /home/nyaptor/dev/personal/installfest config --get core.hooksPath
.beads/hooks
$ ls /home/nyaptor/dev/personal/installfest/.beads/hooks/
post-checkout  post-merge  pre-commit  prepare-commit-msg  pre-push     # no post-commit
```

The template injects delegations for pre-push (IF-DEPLOY), post-merge
(IF-POSTMERGE), and pre-commit (IF-PRECOMMIT) — never post-commit. The only
path that reaches `scripts/hooks/post-commit` is the no-beads fallback at
`home/run_onchange_set-git-hooks.sh.tmpl:97-100`:

```bash
else
    # No beads in this clone — fall back to scripts/hooks as the hooks dir
    git -C "$DOTFILES" config core.hooksPath scripts/hooks
fi
```

### Excerpt 4 — live drifted block in the deployed hook

`.beads/hooks/post-merge:26-31` (live file; injected by an older template
revision, so it lacks the two "Managed by chezmoi" comment lines the current
template would emit — markers are identical, behavior is identical):

```sh
# --- BEGIN IF-POSTMERGE v1 ---
if command -v chezmoi >/dev/null 2>&1; then
    chezmoi apply --no-tty 2>/dev/null && echo "chezmoi: applied dotfiles" || \
        echo "chezmoi: apply failed (run 'chezmoi apply' manually)" >&2
fi
# --- END IF-POSTMERGE v1 ---
```

The beads managed block occupies lines 2-24 of that file
(`# --- BEGIN BEADS INTEGRATION v0.63.3 ---` … `# --- END BEADS INTEGRATION v0.63.3 ---`).

### Excerpt 5 — dead DOTFILES in ado-auth (ENTROPY-03 tail)

`home/run_onchange_after_ado-auth.sh.tmpl:21`:

```bash
DOTFILES="{{ .chezmoi.workingTree }}"
```

Never referenced again in the file (`grep -n DOTFILES` on it returns only
line 21). Contrast: `home/run_onchange_after_github-auth.sh.tmpl` assigns the
same var at line 36 and USES it at line 143 — do not touch github-auth.

### Injection-marker gotcha (why a version bump is required)

The template only appends a block when its BEGIN marker string is absent from
the target file. The live `.beads/hooks/post-merge` already contains
`# --- BEGIN IF-POSTMERGE v1 ---`, so simply changing the v1 heredoc body
would never land on already-deployed machines. The fix must (a) bump the
marker to v2 and (b) strip any stale v1 block before injecting.

## Commands you will need

| Purpose | Command | Expected on success |
|---------|---------|---------------------|
| Quality gate | `bash scripts/check.sh` | `ALL CHECKS PASSED`, exit 0 |
| Deploy + run changed run_onchange scripts | `chezmoi apply` | exit 0; prints this template's injection echoes |
| Pending-deploy preview | `chezmoi status` | (baseline check, see Step 1) |
| Hooks path | `git -C /home/nyaptor/dev/personal/installfest config --get core.hooksPath` | `.beads/hooks` |
| chezmoi source check | `chezmoi source-path` | `/home/nyaptor/dev/personal/installfest/home` |

All commands assume cwd `/home/nyaptor/dev/personal/installfest` unless a
`-C`/absolute path is given. `bd` pre-commit hooks run automatically on
commit; never run `bd sync` (does not exist in bd 1.0.3).

## Scope

**In scope** (the only files you should modify):

- `home/run_onchange_set-git-hooks.sh.tmpl` — the fix (Steps 2-4)
- `home/run_onchange_after_ado-auth.sh.tmpl` — delete line 21 only (Step 5)
- `.beads/hooks/post-merge` and `.beads/hooks/post-commit` — mutated/created
  **at runtime by chezmoi apply**, never hand-edited; their resulting state is
  committed (they are git-tracked)
- `plans/README.md` — status row only

**Out of scope** (do NOT touch, even though they look related):

- `scripts/hooks/post-commit`, `scripts/hooks/post-merge` (symlink),
  `scripts/hooks/pre-push`, `scripts/hooks/pre-commit`,
  `scripts/hooks/remote-apply.sh` — canonical hooks are correct as-is;
  pre-push content belongs to other work.
- `scripts/install-arch.sh` — owned by plans 008/011 (ENTROPY-06).
- `home/run_once_install-packages.sh.tmpl`, `home/run_after_doctor.sh.tmpl`,
  `home/run_onchange_after_install-user-schedulers.sh.tmpl` — ENTROPY-04
  (launchctl-bootstrap triplication) is note-only, deferred (see Maintenance
  notes).
- `home/run_onchange_after_github-auth.sh.tmpl` — its `DOTFILES` IS used
  (line 143); ENTROPY-05 preamble dedup is note-only, deferred.
- `.beads/hooks/pre-push`, `.beads/hooks/pre-commit`,
  `.beads/hooks/post-checkout`, `.beads/hooks/prepare-commit-msg` — this plan
  must not alter them; if `git status` shows them modified after Step 6,
  STOP.
- `.git/hooks/` — silent no-op under `core.hooksPath=.beads/hooks`.

## Git workflow

- Work on the current branch (`main`) — ad-hoc lane, no branch creation
  observed for prior plan executions in this repo.
- Single commit, targeted adds (never `git add .` / `-A`), then push.
  Message style from `git log` of the same file:
  `fix(hooks): set core.hooksPath in set-git-hooks beads branch`. Use:
  `fix(hooks): chain post-commit deploy under beads hooksPath, delegate post-merge (plan 012)`
- Write the commit message to a temp file and use `git commit -F <file>` as a
  separate Bash call from the push (RTK HEREDOC/chaining footgun documented in
  this environment).
- The final commit itself fires the newly-wired post-commit hook — capture its
  output as the second piece of runtime evidence.

## Steps

### Step 1: Preconditions and baseline

From `/home/nyaptor/dev/personal/installfest`:

1. `git config --get core.hooksPath` → must print `.beads/hooks`. Anything
   else: STOP (the defect this plan fixes does not exist in that form here).
2. `chezmoi source-path` → must print
   `/home/nyaptor/dev/personal/installfest/home`. Anything else: STOP.
3. `ls .beads/hooks/` → must NOT contain `post-commit`. If it exists already,
   STOP (another session got here first).
4. `grep -c 'BEGIN IF-POSTMERGE v1' .beads/hooks/post-merge` → `1`, and
   `grep -c 'END IF-POSTMERGE v1' .beads/hooks/post-merge` → `1`. If BEGIN
   exists without END: STOP (strip logic would eat the rest of the file).
5. `chezmoi status` — record the output. If it lists pending changes to
   deployed files unrelated to this plan, STOP and report (running
   `chezmoi apply` in Step 6 would deploy them as a side effect).
6. `bash scripts/check.sh` → `ALL CHECKS PASSED`, exit 0 (green baseline;
   verified true at 9399b92).

**Verify**: all six checks match → proceed.

### Step 2: Rewrite the IF-POSTMERGE section as a v2 delegation (ENTROPY-01)

In `home/run_onchange_set-git-hooks.sh.tmpl`, replace the entire block quoted
in Excerpt 2 (lines 54-73: from the comment `# Also inject into post-merge...`
through the closing `fi` after the `echo "chezmoi: injected if-postmerge v1..."`
line) with:

```bash
    # Also inject into post-merge so the canonical local-deploy hook fires
    # after `git pull`. v2: v1 inlined a drifted copy of the deploy logic
    # (plain `chezmoi apply`, which never re-renders .chezmoi.toml.tmpl
    # [data] and skipped the CHEZMOI_SOURCE guard); v2 delegates to
    # scripts/hooks/post-merge (symlink to post-commit), matching the
    # IF-DEPLOY and IF-PRECOMMIT blocks below.
    BEADS_POSTMERGE="$DOTFILES/.beads/hooks/post-merge"
    POSTMERGE_BEGIN="# --- BEGIN IF-POSTMERGE v2 ---"
    if [ -f "$BEADS_POSTMERGE" ]; then
        # Strip the superseded v1 block if present (marker-guarded append
        # alone would never land v2 on machines that already carry v1).
        if grep -Fq "# --- BEGIN IF-POSTMERGE v1 ---" "$BEADS_POSTMERGE" \
            && grep -Fq "# --- END IF-POSTMERGE v1 ---" "$BEADS_POSTMERGE"; then
            awk '/^# --- BEGIN IF-POSTMERGE v1 ---$/{skip=1} !skip{print} /^# --- END IF-POSTMERGE v1 ---$/{skip=0}' \
                "$BEADS_POSTMERGE" > "$BEADS_POSTMERGE.tmp"
            cat "$BEADS_POSTMERGE.tmp" > "$BEADS_POSTMERGE"
            rm -f "$BEADS_POSTMERGE.tmp"
            echo "chezmoi: removed stale if-postmerge v1 from $BEADS_POSTMERGE"
        fi
        if ! grep -Fq "$POSTMERGE_BEGIN" "$BEADS_POSTMERGE"; then
            cat >> "$BEADS_POSTMERGE" <<'HOOK'

# --- BEGIN IF-POSTMERGE v2 ---
# Managed by chezmoi (home/run_onchange_set-git-hooks.sh.tmpl).
# Delegates to scripts/hooks/post-merge (symlink to post-commit): beads
# import + `chezmoi init --apply` local deploy (init --apply re-renders
# .chezmoi.toml.tmpl [data]; plain apply does not).
_if_pm_root=$(git rev-parse --show-toplevel 2>/dev/null)
if [ -n "$_if_pm_root" ] && [ -x "$_if_pm_root/scripts/hooks/post-merge" ]; then
    "$_if_pm_root/scripts/hooks/post-merge" "$@" || true
fi
unset _if_pm_root
# --- END IF-POSTMERGE v2 ---
HOOK
            echo "chezmoi: injected if-postmerge v2 into $BEADS_POSTMERGE"
        fi
    fi
```

Notes for the executor:
- Keep the 4-space indentation of the surrounding `if [ -f "$BEADS_PREPUSH" ]`
  branch, exactly as the replaced block had (the heredoc body itself starts at
  column 0 — heredoc content is literal; the closing `HOOK` must be at column
  0).
- `cat tmp > file && rm tmp` (not `mv`) deliberately preserves the hook file's
  executable bit.
- The `|| true` matches IF-DEPLOY: a deploy failure must never make the git
  hook itself fail.

**Verify**: `grep -n 'IF-POSTMERGE' home/run_onchange_set-git-hooks.sh.tmpl`
→ shows v2 marker lines and the two v1 literals only inside the strip logic
and heredoc-free grep conditions; no `POSTMERGE_END=` assignment remains.

### Step 3: Add the IF-POSTCOMMIT injection (ENTROPY-02 — the live defect)

In the same file, immediately AFTER the block you just wrote in Step 2 (still
inside the `if [ -f "$BEADS_PREPUSH" ]` branch, BEFORE the
`# Also inject into pre-commit...` comment), insert:

```bash
    # Also inject into post-commit so the local deploy fires on every commit.
    # beads installs NO post-commit hook, so with core.hooksPath=.beads/hooks
    # the canonical scripts/hooks/post-commit never ran on commit — commit-time
    # deploy was silently dead on beads machines. We create the file if absent;
    # if a future beads version ships its own post-commit, our marker-guarded
    # block appends after its managed section (beads only rewrites content
    # between its own BEGIN/END markers).
    BEADS_POSTCOMMIT="$DOTFILES/.beads/hooks/post-commit"
    POSTCOMMIT_BEGIN="# --- BEGIN IF-POSTCOMMIT v1 ---"
    if [ ! -f "$BEADS_POSTCOMMIT" ]; then
        printf '#!/usr/bin/env sh\n' > "$BEADS_POSTCOMMIT"
        chmod +x "$BEADS_POSTCOMMIT"
    fi
    if ! grep -Fq "$POSTCOMMIT_BEGIN" "$BEADS_POSTCOMMIT"; then
        cat >> "$BEADS_POSTCOMMIT" <<'HOOK'

# --- BEGIN IF-POSTCOMMIT v1 ---
# Managed by chezmoi (home/run_onchange_set-git-hooks.sh.tmpl).
# Delegates to scripts/hooks/post-commit (beads import + `chezmoi init
# --apply` local deploy). `|| true`: post-commit must never fail the commit.
_if_postc_root=$(git rev-parse --show-toplevel 2>/dev/null)
if [ -n "$_if_postc_root" ] && [ -x "$_if_postc_root/scripts/hooks/post-commit" ]; then
    "$_if_postc_root/scripts/hooks/post-commit" "$@" || true
fi
unset _if_postc_root
# --- END IF-POSTCOMMIT v1 ---
HOOK
        echo "chezmoi: injected if-postcommit v1 into $BEADS_POSTCOMMIT"
    fi
```

The `#!/usr/bin/env sh` shebang matches the beads-authored hook files
(`.beads/hooks/post-merge:1`).

**Verify**: `bash -n <(chezmoi execute-template < home/run_onchange_set-git-hooks.sh.tmpl)`
→ exit 0, no output (syntax-clean render). If `chezmoi execute-template`
errors, fall back to `bash scripts/check.sh` (its template-render section
covers this file) and confirm `PASS: template-render`.

### Step 4: Delete the dead marker vars and stamp the header (ENTROPY-03)

Still in `home/run_onchange_set-git-hooks.sh.tmpl`:

1. Delete line 28: `MARKER_END="# --- END IF-DEPLOY v1 ---"` (keep lines
   26-27). `POSTMERGE_END` is already gone via Step 2's rewrite. Delete the
   `PRECOMMIT_END="# --- END IF-PRECOMMIT v1 ---"` assignment (line 80 at
   9399b92, now shifted) from the pre-commit section — leave the rest of that
   section untouched.
2. Update the version-tag comment on line 5 from
   `# {{ "scripts/hooks" }} {{ "if-deploy-v1" }} {{ "if-precommit-v1" }}`
   to
   `# {{ "scripts/hooks" }} {{ "if-deploy-v1" }} {{ "if-postmerge-v2" }} {{ "if-postcommit-v1" }} {{ "if-precommit-v1" }}`
   (documentation stamp; any content change already re-triggers this
   run_onchange script).

**Verify**: `grep -n 'MARKER_END=\|POSTMERGE_END=\|PRECOMMIT_END=' home/run_onchange_set-git-hooks.sh.tmpl`
→ no output, exit 1.

### Step 5: Delete the dead DOTFILES assignment in ado-auth (ENTROPY-03 tail)

In `home/run_onchange_after_ado-auth.sh.tmpl`, delete line 21
(`DOTFILES="{{ .chezmoi.workingTree }}"`) and the blank line pairing is fine
to leave as-is. Touch nothing else in that file.

Side effect to expect: this run_onchange script will re-run on the next
interactive `chezmoi apply` (content hash changed). It is TTY-guarded
(`[[ ! -t 0 || -n "${CI:-}" ]]` → clean skip) and idempotent by design, so a
non-interactive apply just prints its skip line.

**Verify**: `grep -cn 'DOTFILES' home/run_onchange_after_ado-auth.sh.tmpl`
→ `0`. Then `bash scripts/check.sh` → `ALL CHECKS PASSED`, exit 0.

### Step 6: Deploy — run the fixed script via chezmoi apply

```
chezmoi apply
```

Expected stdout includes (order may interleave with other script output):

```
chezmoi: removed stale if-postmerge v1 from /home/nyaptor/dev/personal/installfest/.beads/hooks/post-merge
chezmoi: injected if-postmerge v2 into /home/nyaptor/dev/personal/installfest/.beads/hooks/post-merge
chezmoi: injected if-postcommit v1 into /home/nyaptor/dev/personal/installfest/.beads/hooks/post-commit
```

If `run_onchange_after_ado-auth` also re-runs, its non-interactive skip line
(`ado-auth: non-interactive shell — skipping...`) is expected noise.

**Verify** (all four):

1. `grep -c 'BEGIN IF-POSTMERGE v2' .beads/hooks/post-merge` → `1`
2. `grep -c 'BEGIN IF-POSTMERGE v1' .beads/hooks/post-merge; echo rc=$?` →
   `0` + `rc=1` (v1 gone)
3. `test -x .beads/hooks/post-commit && grep -c 'BEGIN IF-POSTCOMMIT v1' .beads/hooks/post-commit` → `1`
4. `grep -c 'BEGIN BEADS INTEGRATION' .beads/hooks/post-merge` → `1` (beads
   managed block untouched)

### Step 7: Idempotency check

```
chezmoi apply
```

**Verify**: output contains NO `chezmoi: injected ...` / `chezmoi: removed ...`
lines this time (markers present → all guards skip), and the Step 6 greps
still hold.

### Step 8: Runtime evidence — a real commit fires the deploy (MANDATORY)

Do NOT skip this and do NOT substitute source-reading. Paste the actual
output in your completion report.

```
git commit --allow-empty -m "test: verify commit-triggered deploy chain (temporary)"
```

Expected: commit succeeds and its output includes the line

```
chezmoi: applied dotfiles
```

(emitted by `scripts/hooks/post-commit:42` via the new
`.beads/hooks/post-commit` delegation; the commit will take a few extra
seconds — `chezmoi init --apply` runs for real). The bd pre-commit /
prepare-commit-msg hooks also print their usual output; that is expected.

Then remove the throwaway commit **with --soft only**:

```
git reset --soft HEAD~1
```

NEVER `git reset --hard` here — your template edits are still uncommitted in
the working tree and `--hard` would destroy them.

**Verify**: `git log --oneline -1` → back to the pre-test HEAD;
`git status --short` still shows your in-scope modifications (two `home/`
templates modified, `.beads/hooks/post-merge` modified,
`.beads/hooks/post-commit` untracked-new, plus this plan file /
`plans/README.md` and possibly `.beads/interactions.jsonl`).

### Step 9: Commit and push

1. `git status --short` — confirm ONLY in-scope paths are dirty (see Step 8
   verify list). Anything else dirty that you did not create: STOP.
2. Stage explicitly (no bare dirs, no `-A`):

```
git add home/run_onchange_set-git-hooks.sh.tmpl home/run_onchange_after_ado-auth.sh.tmpl .beads/hooks/post-merge .beads/hooks/post-commit plans/012-deploy-hook-integrity.md plans/README.md
```

   Add `.beads/interactions.jsonl` too if `git status` shows it modified.
3. Write the commit message to `/tmp/commit-msg-012.txt` (Write tool, not a
   HEREDOC chained through the shell):

```
fix(hooks): chain post-commit deploy under beads hooksPath, delegate post-merge (plan 012)

- ENTROPY-02: .beads/hooks/ had no post-commit, so commit-time chezmoi
  deploy was silently dead under core.hooksPath=.beads/hooks; inject an
  IF-POSTCOMMIT v1 delegation to scripts/hooks/post-commit (created file,
  marker-guarded, appended after beads' managed block)
- ENTROPY-01: IF-POSTMERGE v1 was a drifted inline copy (plain chezmoi
  apply, no [data] re-render, no CHEZMOI_SOURCE guard); v2 delegates to
  scripts/hooks/post-merge and strips stale v1 blocks on deployed machines
- ENTROPY-03: drop assigned-never-read MARKER_END/POSTMERGE_END/
  PRECOMMIT_END heredoc-marker vars + dead DOTFILES in ado-auth
```

4. `git commit -F /tmp/commit-msg-012.txt` — the commit output should AGAIN
   include `chezmoi: applied dotfiles` (post-commit firing on a real commit:
   your second piece of runtime evidence — paste it).
5. `git push` (separate Bash call).

**Verify**: `git status --short` → clean (except gitignored noise);
`git log --oneline -1` shows the new commit; push exited 0.

## Test plan

This repo has no shell unit-test harness; the applicable gates are:

- **Static**: `bash scripts/check.sh` (renders all 55 templates and `bash -n`s
  them, shellcheck severity=error) — run after Steps 5 and before Step 9;
  must exit 0. This is the gate every deletion/change in this repo must pass.
- **Runtime** (the part static checks cannot prove — the whole point of this
  plan): Step 6 injection echoes, Step 7 idempotency, Step 8 + Step 9 live
  hook fires printing `chezmoi: applied dotfiles` on real commits. Model the
  evidence format on the drift-check/verify pattern used by
  `docs/plans/003-unify-pre-commit-hooks.md` (this repo's prior hook plan):
  paste command + observed output, not paraphrase.
- Not applicable: `cc-tmux self-test` (this plan touches no cc-tmux code —
  required only for plan 014's scope).

## Done criteria

Machine-checkable. ALL must hold:

- [ ] `bash scripts/check.sh` → exit 0, `ALL CHECKS PASSED`
- [ ] `git config --get core.hooksPath` → `.beads/hooks` (unchanged)
- [ ] `test -x .beads/hooks/post-commit && grep -c 'BEGIN IF-POSTCOMMIT v1' .beads/hooks/post-commit` → `1`
- [ ] `grep -c 'BEGIN IF-POSTMERGE v2' .beads/hooks/post-merge` → `1` and
      `grep -q 'BEGIN IF-POSTMERGE v1' .beads/hooks/post-merge` → exit 1
- [ ] `grep -q 'BEGIN BEADS INTEGRATION' .beads/hooks/post-merge` → exit 0
      (managed block intact)
- [ ] `grep -n 'MARKER_END=\|POSTMERGE_END=\|PRECOMMIT_END=' home/run_onchange_set-git-hooks.sh.tmpl` → no matches
- [ ] `grep -c 'DOTFILES' home/run_onchange_after_ado-auth.sh.tmpl` → `0`
- [ ] Runtime evidence pasted: Step 8's empty-commit output AND Step 9's real
      commit output both contain `chezmoi: applied dotfiles`
- [ ] Second `chezmoi apply` (Step 7) printed zero injection lines
- [ ] Commit contains only in-scope files; pushed; `git status` clean
- [ ] `plans/README.md` status row for 012 updated

## STOP conditions

Stop and report back (do not improvise) if:

- Step 1 preconditions fail: `core.hooksPath` is not `.beads/hooks`,
  `chezmoi source-path` is not `/home/nyaptor/dev/personal/installfest/home`,
  `.beads/hooks/post-commit` already exists, or the v1 BEGIN marker exists
  without its END marker in `.beads/hooks/post-merge`.
- `chezmoi status` at Step 1 shows pending changes to deployed files this
  plan does not touch (an apply would ship them as a side effect).
- The template's live content does not match the Excerpt 1/2 line ranges
  (another session edited it — this file is a shared injection surface).
- Step 6's `chezmoi apply` errors, or the three injection echo lines do not
  appear, after one debug attempt.
- Step 8's commit output does NOT contain `chezmoi: applied dotfiles` after
  one debug attempt (check: `.beads/hooks/post-commit` executable? delegation
  path `-x` test passing? `chezmoi source-path` prefix-matching repo root?).
- `git status` after Step 6 shows modifications to `.beads/hooks/pre-push`,
  `.beads/hooks/pre-commit`, `.beads/hooks/post-checkout`, or
  `.beads/hooks/prepare-commit-msg` (this plan must not alter those).
- `bash scripts/check.sh` fails on anything outside the two edited templates.
- `git push` fails 3 times.

## Maintenance notes

- **Fleet propagation is self-healing but staged**: sibling machines (Mac,
  Homelab) still run the OLD v1 post-merge block until they pull. Their next
  `git pull` fires v1's plain `chezmoi apply`, which re-runs this (now
  changed) run_onchange script, which strips v1 and injects v2 + the new
  post-commit hook. The FIRST post-pull apply on those machines therefore
  still uses plain `apply` (no `[data]` re-render) — one-pull lag, acceptable;
  a manual `chezmoi init --apply` there closes it immediately.
- **`bd hooks install` / beads upgrades**: beads rewrites only its own
  BEGIN/END-marked section and, per current behavior, ships no post-commit
  hook. If a future beads version creates/overwrites `.beads/hooks/post-commit`
  wholesale, the marker-guarded injection re-appends on the next
  `chezmoi apply` — but verify with a real commit after any beads upgrade
  (config presence is not runtime liveness).
- **Commit latency**: every commit in this repo now runs
  `chezmoi init --apply` (a few seconds). That is the designed layer-1
  behavior (see the deploy-hooks design memory) being restored, not new cost.
  If it ever needs to be skippable, add an env-var guard to
  `scripts/hooks/post-commit` — a separate, operator-approved change.
- **Deferred, note-only (do not do in this plan)**:
  - ENTROPY-04: three divergent launchctl-bootstrap implementations —
    `home/run_once_install-packages.sh.tmpl:102-117` (simple bootstrap; fully
    subsumed by `run_after_doctor.sh.tmpl:39-45`'s generic loop later in the
    same apply) and `home/run_onchange_after_install-user-schedulers.sh.tmpl:44-71`
    (the only copy with the bootout+retry-backoff race fix). Consolidation is
    an operator decision; propose separately if wanted.
  - ENTROPY-05: ~30-line duplicated preamble (TTY guard + info/success/warn +
    op presence/signin/read) between `run_onchange_after_ado-auth.sh.tmpl:23-53`
    and `run_onchange_after_github-auth.sh.tmpl`. Two sites, platform-specific
    tails, not yet behaviorally diverged — per the deletion bar, do not force
    a shared abstraction unless a third auth script appears.
- **Reviewer focus**: (1) the awk strip range in Step 2 — it deletes
  BEGIN..END inclusive and nothing else; (2) exec-bit preservation
  (`cat tmp > file`, not `mv`); (3) the new post-commit heredoc uses
  `|| true` (never fail a commit) while IF-PRECOMMIT correctly keeps
  `|| exit 1` (gate) — do not "unify" them.
