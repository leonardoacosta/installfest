# Plan 002: Make SSH key rotation append-verify-remove (no lockout window) and drop the auto force-push

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `docs/plans/README.md`.
>
> **Drift check (run first)**: `git diff --stat 2068bad..HEAD -- ssh-mesh/scripts/rotate-keys.sh`
> If the file changed since this plan was written, compare the "Current state"
> excerpts against the live code before proceeding; on a mismatch, STOP.
>
> **DO NOT RUN the rotation script during this work.** It mutates live SSH
> trust on three machines. All verification here is static (bash -n,
> shellcheck, dry-run flag).

## Status

- **Priority**: P1
- **Effort**: M
- **Risk**: MED
- **Depends on**: none
- **Category**: bug
- **Planned at**: commit `2068bad`, 2026-07-02

## Why this matters

`ssh-mesh/scripts/rotate-keys.sh` rotates the shared ED25519 mesh key across
Mac, homelab, and CloudPC. It runs under `set -euo pipefail` and performs
destructive steps in an order that makes any mid-run failure a potential
lockout: it **overwrites** each peer's `authorized_keys` with only the new key
(line 32) before the local key is rotated (line 84), deletes the local `.old`
backup unconditionally (line 114) regardless of whether verification passed,
and finishes by running `bfg` + `git reflog expire` + `git push --force` on
every rotation (lines 142-155) — unattended history rewriting that fires
whether or not a private key was ever committed. A half-failed rotation
currently requires out-of-band console recovery; the force-push can clobber
remote history from a clone with unpushed work.

The fix restructures to the standard safe order: append new key everywhere,
verify new key works everywhere using `-i` explicitly, only then swap private
keys and remove old public keys — and removes history surgery from the
rotation path entirely.

## Current state

`ssh-mesh/scripts/rotate-keys.sh` (158 lines, bash, `set -euo pipefail`).
Structure today:

1. Line 20: generate `$HOME/.ssh/id_ed25519_new`.
2. Lines 27-35: homelab — backs up then **overwrites** `authorized_keys`:

```bash
# ssh-mesh/scripts/rotate-keys.sh:29-33
# Backup old authorized_keys
cp ~/.ssh/authorized_keys ~/.ssh/authorized_keys.bak 2>/dev/null || true
# Write new public key (replaces old)
echo '${NEW_PUB}' > ~/.ssh/authorized_keys
chmod 600 ~/.ssh/authorized_keys
```

3. Lines 38-48: copies new private key to homelab and swaps it into place.
4. Lines 53-78: same overwrite+swap on CloudPC via powershell
   (`Set-Content` on both user and admin authorized_keys).
5. Lines 84-87: swaps the local Mac private key.
6. Lines 92-108: verification — `ssh -o ConnectTimeout=5` to each peer, sets
   `HOMELAB_OK` / `CLOUDPC_OK`.
7. Line 114: `rm -f "$HOME/.ssh/id_ed25519.old"` — **unconditional**, even if
   both verifications failed.
8. Lines 117-127: per-peer cleanup gated on the OK flags (this part is fine).
9. Lines 134-141: syncs `home/private_dot_ssh/private_authorized_keys` in the
   chezmoi source to the new pub key (KEEP this logic).
10. Lines 142-155: the history-scrub block:

```bash
# ssh-mesh/scripts/rotate-keys.sh:142-147
if command -v bfg &>/dev/null; then
  bfg --replace-text <(echo 'b3BlbnNzaC1rZXktdjEA') . --no-blob-protection
  git reflog expire --expire=now --all
  git gc --prune=now --aggressive
  git push --force
```

(and an `else` branch that `brew install bfg` then does the same).

Conventions: plain bash, `set -euo pipefail` at file scope is correct here
(executed-only script). Heredocs to `ssh ... bash -s` for remote steps.
Machine constants at the top of the file (lines 8-13) — keep them.

## Commands you will need

| Purpose | Command | Expected on success |
|---------|---------|---------------------|
| Syntax | `bash -n ssh-mesh/scripts/rotate-keys.sh` | exit 0 |
| Lint | `shellcheck ssh-mesh/scripts/rotate-keys.sh` | no errors (warnings OK if pre-existing class) |
| Dry-run | `bash ssh-mesh/scripts/rotate-keys.sh --dry-run` | prints the step plan, touches nothing |

## Scope

**In scope**:
- `ssh-mesh/scripts/rotate-keys.sh` (rewrite in place)

**Out of scope**:
- `ssh-mesh/scripts/setup-mac.sh`, `setup-homelab.sh`, `deploy-to-homelab.sh` —
  provisioning, not rotation.
- `home/private_dot_ssh/private_authorized_keys` — only ever modified BY the
  script at runtime; do not hand-edit.
- Any new "history scrub" replacement script — deliberately not created; if a
  key ever lands in git history that is a one-off incident response, not a
  routine.
- Actually executing a rotation.

## Git workflow

- Current branch, conventional commit, e.g.
  `fix(ssh-mesh): append-verify-remove key rotation; drop auto force-push`.
- Do NOT push unless the operator instructed it.

## Steps

### Step 1: Add a --dry-run flag

At the top (after the constants), parse `--dry-run` into `DRY_RUN=true|false`.
Wrap every mutating call (ssh, scp, cp/mv/rm on key files, powershell) in a
`run()` helper: `run() { if $DRY_RUN; then echo "DRY: $*"; else "$@"; fi; }` —
heredoc-fed `ssh bash -s` blocks can instead be guarded with
`if $DRY_RUN; then echo "DRY: <description>"; else ... fi`.

**Verify**: `bash -n ssh-mesh/scripts/rotate-keys.sh` → exit 0.

### Step 2: Reorder to append-first

Restructure the phases to:

1. **Generate** new keypair (unchanged, keep `$NEW_KEY`).
2. **Append phase** — for homelab (bash heredoc) and CloudPC (powershell
   `Add-Content` instead of `Set-Content`): append `$NEW_PUB` to
   `authorized_keys` (both CloudPC files) only if not already present
   (grep guard on homelab; `Select-String` guard on CloudPC). Do NOT remove
   or overwrite anything. Do NOT touch private keys yet. Keep the `.bak`
   backups.
3. **Verify-new phase** — from the Mac, test each peer using the NEW key
   explicitly, without touching the default identity:
   `ssh -i "$NEW_KEY" -o IdentitiesOnly=yes -o ConnectTimeout=5 <peer> "echo OK"`.
   Set `HOMELAB_OK` / `CLOUDPC_OK` from these results.
4. **Gate**: if NOT both OK — print which peer failed, print recovery note
   ("old key still fully valid on all peers; re-run after fixing
   connectivity; new pub key appended but harmless"), and `exit 1`. Nothing
   destructive has happened at this point.
5. **Swap phase** (only both OK) — distribute the new private key to homelab
   and CloudPC and swap into place keeping `.old` copies (reuse existing
   heredoc/powershell logic from lines 38-48 / 68-78), then swap the local
   Mac key keeping `.old` (existing lines 84-87).
6. **Re-verify** with the default identity (plain `ssh <peer> "echo OK"`,
   as verification that the swapped default key works).
7. **Remove-old phase** (only if step 6 passed for that peer) — on each peer,
   rewrite `authorized_keys` to contain only `$NEW_PUB` (this is where the
   old overwrite logic moves to), and remove `.old` / `.bak` files — INCLUDING
   the local `rm -f "$HOME/.ssh/id_ed25519.old"`, which must move inside this
   gated phase (currently unconditional at line 114).
8. **Chezmoi source sync** — keep lines 134-141 verbatim.

### Step 3: Delete the history-scrub block

Remove lines 142-155 (both bfg branches, reflog expire, gc, `git push --force`,
and the `brew install bfg`). Replace with a two-line comment: rotation does not
imply history surgery; if a private key is ever committed, handle it as a
dedicated incident (bfg/filter-repo + coordinated force-push), not here.

**Verify**: `grep -n 'bfg\|push --force\|reflog' ssh-mesh/scripts/rotate-keys.sh`
→ no matches (except within the explanatory comment, if worded that way —
prefer wording that avoids the literal strings so this grep stays clean).

### Step 4: Static verification

**Verify**:
- `bash -n ssh-mesh/scripts/rotate-keys.sh` → exit 0
- `shellcheck ssh-mesh/scripts/rotate-keys.sh` → no new errors vs. `git stash`
  baseline (run shellcheck on HEAD version first to record pre-existing output)
- `bash ssh-mesh/scripts/rotate-keys.sh --dry-run` → prints the phase plan in
  the order generate/append/verify/swap/re-verify/remove/sync; exits 0; and
  `ls ~/.ssh/id_ed25519_new` → No such file (nothing generated in dry-run —
  put keygen behind the dry-run guard too).

## Test plan

No harness exists; static checks + `--dry-run` output ARE the test. Paste the
full dry-run output into the commit message body. A live rotation should be
performed by Leo manually at the next scheduled rotation, not by the executor.

## Done criteria

- [ ] `bash -n` exits 0; shellcheck shows no new errors
- [ ] `--dry-run` exits 0 and creates/modifies no files (`git status` clean,
      `ls ~/.ssh/id_ed25519_new*` absent)
- [ ] Script contains no `bfg`, `git push --force`, or `reflog expire` calls
- [ ] Local `.old` deletion is inside the both-peers-verified gate
- [ ] `authorized_keys` is never overwritten before new-key verification
      (append in phase 2, overwrite only in phase 7)
- [ ] Chezmoi-source sync logic (old lines 134-141) preserved
- [ ] No files outside scope modified; `docs/plans/README.md` row updated

## STOP conditions

- The live file no longer matches the excerpts (drift — e.g. someone already
  reworked rotation).
- You find an external caller of this script that passes arguments
  (`grep -rn 'rotate-keys' home/ scripts/ platform/ ssh-mesh/ docs/`) — the
  new `--dry-run` arg parsing must not break it; report what you find.
- Any step tempts you to actually SSH to a peer to "test" — do not; static
  only.

## Maintenance notes

- If a fourth machine joins the mesh, it must be added to BOTH the append and
  the remove phases, and to the verify gate (all-or-nothing).
- Reviewer should scrutinize: the CloudPC powershell append-not-overwrite
  (`Add-Content` + dedup guard) and that `icacls` hardening still runs on the
  admin file after the final overwrite.
- Deferred deliberately: per-machine keys instead of one shared mesh key
  (bigger design change; the shared-key model is a recorded choice in
  `ssh-mesh/README.md`).
