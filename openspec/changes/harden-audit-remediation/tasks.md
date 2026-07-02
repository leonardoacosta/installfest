# Implementation Tasks

<!-- beads:epic: if-is3 -->
<!-- beads:feature: if-is3 -->

Each batch maps to one plan under `docs/plans/`. The plan file is the source of
truth for exact steps, code excerpts, verification commands, and STOP
conditions — these lines track completion. Run `scripts/check.sh` (once Req-4
lands) before and after Req-5. Batches are independent except Req-5 depends on
Req-4. `[P-1]` = do first within a batch.

## File-Server Batch
<!-- [beads:if-ed1] -->


- [x] [1.1] [P-1] Add token load/generation to `scripts/file-server.py` — read `~/.local/state/file-server.token` (0600), generate via `secrets.token_hex(16)` if absent, log generation without the value (plan 001 Step 1) [owner:general-purpose] [beads:if-ed1]
- [x] [1.2] [P-1] Enforce token in `do_GET` — strip query string before path resolution; accept `t=` param or `fs_token` cookie via `hmac.compare_digest`; set `HttpOnly` cookie on param match; else 403 (plan 001 Step 2) [owner:general-purpose] [beads:if-ed1]
- [x] [1.3] Append token to `flink` URLs in `home/dot_zsh/rc/linux.zsh` with bare-URL fallback when token file absent (plan 001 Step 3) [owner:general-purpose] [beads:if-ed1]
- [x] [1.4] Deploy + live-verify: `chezmoi apply`, restart unit, confirm tokenless 403 and tokened 200 over the tailscale IP (plan 001 Step 4) [owner:general-purpose] [beads:if-ed1]

## Key-Rotation Batch
<!-- [beads:if-do3] -->


- [x] [2.1] [P-1] Add `--dry-run` flag + `run()` guard to `ssh-mesh/scripts/rotate-keys.sh` (keygen also gated) (plan 002 Step 1) [owner:general-purpose] [beads:if-do3]
- [x] [2.2] [P-1] Reorder to generate → append-new-key-everywhere → verify-new-key (`ssh -i $NEW_KEY -o IdentitiesOnly=yes`) → gate → swap → re-verify → remove-old (move unconditional local `.old` delete inside the gate); CloudPC uses `Add-Content` + dedup guard, not `Set-Content` (plan 002 Step 2) [owner:general-purpose] [beads:if-do3]
- [x] [2.3] Delete the `bfg` / `reflog expire` / `git push --force` block; preserve the chezmoi-source `authorized_keys` sync (plan 002 Step 3) [owner:general-purpose] [beads:if-do3]
- [x] [2.4] Static verify: `bash -n`, `shellcheck` (no new errors vs HEAD), `--dry-run` exits 0 and creates no files (plan 002 Step 4) [owner:general-purpose] [beads:if-do3]

## Pre-Commit Batch
<!-- [beads:if-m15] -->


- [x] [3.1] [P-1] Add secret-scan step 0 to `scripts/hooks/pre-commit` — invoke `.githooks/pre-commit`, `|| exit 1` (plan 003 Step 1) [owner:general-purpose] [beads:if-m15]
- [x] [3.2] [P-1] Inject marker-guarded `IF-PRECOMMIT v1` block into `.beads/hooks/pre-commit` via `home/run_onchange_set-git-hooks.sh.tmpl`; delegation propagates non-zero exit (NO `|| true`); update the tmpl header hash-trigger line (plan 003 Step 2) [owner:general-purpose] [beads:if-m15]
- [x] [3.3] `chezmoi apply` twice → block appears exactly once (idempotent) (plan 003 Step 3) [owner:general-purpose] [beads:if-m15]
- [x] [3.4] Live gate test: staged fake-AKIA file blocks commit (exit != 0); clean empty commit passes (plan 003 Step 4) [owner:general-purpose] [beads:if-m15] — found+fixed a pre-existing bug in `.githooks/pre-commit`: `grep -nEi "$COMBINED"` silently no-matched because `$COMBINED` starts with `-----BEGIN`, which grep parsed as an option string; added `-e` guard. This is why the scanner was previously orphaned/never validated.

## Verification-Baseline Batch
<!-- [beads:if-4pg] -->


- [x] [4.1] [P-1] Create `scripts/check.sh` — `set -uo pipefail`, source `scripts/utils.sh`, sections: zsh-syntax, sh-syntax, template-render, shellcheck (error severity), conditional terraform validate; `FAIL` accumulator, per-section PASS/FAIL, tool-absent skips (plan 004 Step 1) [owner:general-purpose] [beads:if-4pg]
- [x] [4.2] [P-1] Calibrate to pass on current tree — populate `SHELLCHECK_EXCLUDE` with burn-down comment for pre-existing findings; a template that fails to render is a STOP (real bug) (plan 004 Step 2) [owner:general-purpose] [beads:if-4pg]
- [x] [4.3] Wire `"check"` into `package.json`; add `shellcheck` to `platform/homebrew/Brewfile` and `scripts/install-arch.sh`; add README "Verifying" note (plan 004 Step 3) [owner:general-purpose] [beads:if-4pg]
- [x] [4.4] Prove the gate: planted zsh syntax error → exit 1 / FAIL: zsh-syntax; restored → exit 0 (plan 004 Step 4) [owner:general-purpose] [beads:if-4pg]

## Registry-Refactor Batch
<!-- [beads:if-8lu] -->


- [ ] [5.1] [P-1] Record gate baseline — `scripts/check.sh` exits 0 (STOP if Req-4 not landed) (plan 005 Step 0) [owner:general-purpose]
- [ ] [5.2] [P-1] Create `scripts/lib/registry.sh` — source-guard strict mode, `registry_path()` (with existence fallback), cached `registry_python()` (probe loop); leak check shows `errexit off` (plan 005 Step 1) [owner:general-purpose]
- [ ] [5.3] Convert Camp B (bug fixes) — `generate-raycast.sh`, `mux-remote.sh`, `executable_copen` — to source the lib; verify `generate-raycast.sh` output byte-identical and `copen` resolves without a tomllib error (plan 005 Step 2) [owner:general-purpose]
- [ ] [5.4] Convert Camp A (dedup) — `wsenv`, `generate-profiles`, `wk-ready`, `cmux-workspaces.sh` — delete inline probe, use lib, keep each script's own var name (plan 005 Step 3) [owner:general-purpose]
- [ ] [5.5] Sweep + gate — probe loop exists once (only in the lib); no bare `python3` heredoc remains; `scripts/check.sh` exits 0 (plan 005 Step 4) [owner:general-purpose]

## Cross-Cutting Verification

- [ ] [6.1] Update each plan's status row in `docs/plans/README.md` to DONE as its batch completes [owner:general-purpose]
- [ ] [6.2] [P-2] Follow-up (not in this change): after Req-3 + Req-4 land, wire `scripts/check.sh` into the pre-commit chain — file as a separate task, do not scope-creep here [owner:general-purpose]
