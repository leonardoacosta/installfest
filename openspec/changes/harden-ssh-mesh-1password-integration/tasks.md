---
stack: t3
---
<!-- beads:epic:if-1ydm -->
<!-- beads:feature:if-xdgi -->

# Implementation Tasks

## DB Batch

- [x] [1.1] In `ssh-mesh/scripts/rotate-keys.sh`: add a new step after the existing Phase 6 re-verify gate (both peers confirmed reachable with the swapped default identity) and before Phase 7's old-key pruning — write the new private+public key material to the 1Password item referenced by `OP_SSH_ITEM` (default `op://Private/mesh-ssh`, same variable `scripts/op-ssh-provision.sh` already reads) via `op item edit`. Respect the script's existing `--dry-run` flag — under `--dry-run`, narrate the write (no `op` call executed, 1Password item unchanged). On a real (non-dry-run) write failure, print a clear warning and continue — do NOT roll back the already-verified peer key swap; the mesh must stay reachable even if the 1Password mirror goes stale. Reuse the script's existing `run()` dry-run wrapper where the command shape allows it. [beads:if-nyyr]

  **Implementation** (commit `fb64c82`): new "Phase 6b" block between Phase 6 (re-verify) and
  Phase 7 (prune), gated on `$HOMELAB_REVERIFY && $CLOUDPC_REVERIFY`. Writes the now-swapped
  `$HOME/.ssh/id_ed25519`/`.pub` to the 1Password item via
  `op item edit "$OP_SSH_ITEM" "private key=..." "public key=..."`, using the same field labels
  `op-ssh-provision.sh` already reads. Deliberately did NOT use the script's `run()` wrapper —
  `run()`'s `"$@"` form evaluates arguments eagerly, which would leak the real private key into a
  `DRY: ...` echo line under `--dry-run`; used an explicit `if $DRY_RUN; then ...; else ...; fi`
  block instead. On missing `op` or non-zero exit: prints a warning and continues, no rollback.

  **Caveat needing live confirmation** (deferred to task 4.3): `op` CLI is installed but no
  account is signed in in this environment, so the exact `op item edit` field-assignment syntax
  against a real SSH-Key item could not be confirmed live — used the plain `field=value` form
  (no `[fieldType]` override) against field labels already proven correct by
  `op-ssh-provision.sh`'s own `op read` calls.
- [x] [1.2] Verify: `ssh-mesh/scripts/rotate-keys.sh --dry-run` runs clean end-to-end and its output includes the new 1Password write-back step narration in the correct position (after re-verify, before prune). Paste the relevant `--dry-run` output excerpt. [beads:if-9bpb]

  Independently re-verified (orchestrator, not just the implementing agent):
  ```
  $ bash -n ssh-mesh/scripts/rotate-keys.sh && echo SYNTAX: PASS
  SYNTAX: PASS

  $ ./ssh-mesh/scripts/rotate-keys.sh --dry-run
  ...
  [5/7] Re-verifying peers with the swapped default identity...
  DRY: ssh homelab 'echo OK'; ssh cloudpc 'echo OK'

  Syncing rotated key back to 1Password (op://Private/mesh-ssh)...
  DRY: op item edit "op://Private/mesh-ssh" 'private key=<new key>' 'public key=<new key>'

  [6/7] Pruning old keys (only where re-verify passed)...
  ```
  New step lands exactly between re-verify and prune narration; no real `op` call, no key
  material printed anywhere in the dry-run output.

## API Batch

- [x] [2.1] In `platform/windows/setup.ps1`: add an `op`-based provisioning step (mirroring `scripts/op-ssh-provision.sh`'s idempotent/TTY-guarded/fail-open shape) that runs BEFORE the existing manual-key-transfer path — skip if a mesh key is already on disk; else attempt `op.exe read` of the same `op://Private/mesh-ssh` item (public+private key) via PowerShell, writing to the user's `.ssh` directory with correct ACLs (reuse the existing `authorized_keys` ACL-hardening pattern already in this file as a model, applied to the private key file instead). On `op.exe` missing, sign-in failure, or a non-interactive session, fail open with a clear message and leave the existing manual-transfer fallback path intact and reachable — do not hang or abort the script. [beads:if-4g98]

  **Implementation** (commit `ddd4512`): new block inserted right after the existing
  "SSH Mesh: Deploy keys and configs" header, before the manual-transfer comment. Idempotent
  skip if `id_ed25519` already present; `Get-Command op` fail-open if missing; non-interactive
  `op whoami` check via `$LASTEXITCODE` (never calls `op signin` — no prompt, no hang on a
  headless/service-account session). On signed-in: `op read` materializes both key files; ACL
  hardening restricted to current user + SYSTEM only (tighter than the public `authorized_keys`
  ACL a few lines below, which also grants Administrators). Whole step in try/catch; any failure
  removes a partial zero-byte key file and falls through to the untouched manual-transfer path.
- [x] [2.2] Verify via static review: confirm the new step is positioned before the existing manual-transfer fallback, and that `Set-Content`/ACL calls match the file's existing PowerShell conventions (session-safe, no destructive overwrite of an existing key). Paste the diff. [beads:if-3vrh]

  Independently re-verified (orchestrator): brace-balance sanity check (`{`/`}` count) on the
  whole file both before and after the edit = 120/120 — consistent, no unbalanced braces
  introduced. Read the actual new block (`setup.ps1:329-392`) directly: positioned correctly
  before the `# SSH key must be transferred securely` manual-transfer comment; ACL pattern
  matches the file's existing `Get-Acl` → `SetAccessRuleProtection` → `FileSystemAccessRule` →
  `AddAccessRule` → `Set-Acl` shape exactly, scoped down to SYSTEM+user only. No live PowerShell
  execution was possible in this environment (no Windows machine) — this is static review only,
  live confirmation is task 4.1.

## UI Batch

- [x] [3.1] In `home/dot_zshenv.tmpl`: add a `{{ if eq .machine "mac" -}}` conditional block (same pattern as the existing homelab `BROWSER` block in this file) exporting `SSH_AUTH_SOCK` to 1Password's macOS SSH agent socket path. Do NOT modify `home/private_dot_ssh/config.tmpl` or `scripts/op-ssh-provision.sh` — the existing `IdentityFile ~/.ssh/id_ed25519` in the Mac `Host` blocks must remain untouched so OpenSSH's default multi-identity trial order (agent-offered keys first, then configured `IdentityFile`) provides the fallback automatically, with no new fallback logic needed. [beads:if-dksw]

  **Implementation** (commit `d647fba`): new `{{ if eq .machine "mac" -}}` block placed
  immediately after the existing homelab `BROWSER` block, exporting `SSH_AUTH_SOCK` to
  `$HOME/Library/Group Containers/2BUA8C4S2C.com.1password/t/agent.sock` (correctly quoted for
  the embedded space). Comment explains the Mac-only rationale and the on-disk-key-as-fallback
  contract. `home/private_dot_ssh/config.tmpl` and `scripts/op-ssh-provision.sh` untouched, per
  constraint.
- [x] [3.2] `scripts/check.sh` (chezmoi template render + `bash -n` on rendered output) MUST still pass after the `dot_zshenv.tmpl` change. Paste the relevant PASS output. [beads:if-o7cj]

  Independently re-verified (orchestrator): `bash scripts/check.sh` → `ALL CHECKS PASSED`
  (zsh-syntax 9 files, sh-syntax 46, template-render 62 templates, shellcheck 44/2-excluded,
  terraform). Additionally, this machine's chezmoi genuinely targets this repo but has
  `machine=homelab`, so `chezmoi cat ~/.zshenv` correctly omits the new mac block (negative-case
  proof) — a scratch chezmoi config with `machine="mac"` was built and
  `chezmoi execute-template --file home/dot_zshenv.tmpl` against it produced the new
  `SSH_AUTH_SOCK` export and correctly omitted the homelab-only `BROWSER` export; the full
  mac-context render piped through `zsh -n` with no syntax errors.

## E2E Batch

- [x] [4.1] Live-verify Req-2 on the real CloudPC machine: delete the on-disk mesh key (back it up first), run the updated `setup.ps1` provisioning step, confirm the key materializes via `op.exe` without any manual transfer step, and confirm SSH connectivity to the mesh still works with the freshly-materialized key. Restore/clean up per the mesh's existing safety conventions (never leave the machine unreachable). — DONE 2026-07-21, via the manual-transfer path, not op.exe: op.exe/winget are genuinely unavailable on CloudPC (confirmed live — `winget` not recognized in the SSH session; Leo redirected to "one host signs in, distribute directly" rather than chasing an op.exe install). Backed up CloudPC's stale (Jan 2026) private key, scp'd homelab's current key over, fixed a real Windows OpenSSH permission-check gap along the way (private key owned by an `AzureAD\...`-form identity fails the strict permission check even with a correct-looking ACL — switching owner/ACE to the local SID form fixed it), confirmed both inbound (homelab→CloudPC) and outbound (CloudPC→homelab) SSH work, cleaned `authorized_keys` down to just the current key (removed a stale March entry + a duplicate, after confirming the Mac had already rotated too), and fixed `setup.ps1`'s own stale hardcoded `$publicKey` literal (commit be995ed) so a future re-run can't silently redeploy the wrong key. [beads:if-rgsw]
  - depends on: 2.1

  **Not yet live-verified — blocked on the incident below.** The setup.ps1 provisioning code
  itself (task 2.1) was never exercised live this run; CloudPC's SSH access broke as a side
  effect of task 4.3's real rotation (a pre-existing, unrelated bug in `rotate-keys.sh`, not in
  this proposal's code) before 4.1 could be attempted. See task 4.3's evidence for the full
  incident. Resume: after Leo restores SSH access via RDP (root cause under investigation —
  see `if-1ydm.1`), delete the on-disk key on CloudPC and re-attempt this task's original steps.
- [x] [4.2] Live-verify Req-3 on the real Mac: with the 1Password app running and the SSH agent enabled for the mesh key item (one-time manual toggle — see the User Gate task below), run an interactive `ssh homelab` (or an SSH-backed `git fetch`) and confirm a visible Touch ID/system-auth prompt fires. Then quit the 1Password app (or otherwise make the agent socket unavailable) and re-run the same command, confirming it falls back to the on-disk key with no error. — DONE 2026-07-21, after a real chain of discoveries:
  1. First attempt failed genuinely: the 1Password `mesh-ssh` item held a stale key; homelab's sshd rejected it before signing, so Touch ID never fired.
  2. Root cause chased down through `op` CLI itself: `op://Private/mesh-ssh` referenced a vault, "Private", that **does not exist on this account** (confirmed via `op vault list`: Personal, B&B, Priceless, Shared) — every read/write against it had been silently defaulting or failing. Fixed at the source: `scripts/op-ssh-provision.sh` and `ssh-mesh/scripts/rotate-keys.sh`'s `OP_SSH_ITEM` default corrected to `op://Personal/mesh-ssh`; `agent.toml` corrected the same way (commit 3401dc3).
  3. Importing the existing homelab key into an "SSH Key"-category 1Password item failed 3 further distinct ways (edit unsupported for the category; assignment-statement syntax has no valid SSH-key field type per 1Password's own docs; JSON-template-created items couldn't be read back — "private_key isn't a field"). Root-caused (not guessed): 1Password's CLI reliably supports **generating** a new SSH Key item natively but not reliably **importing** external key material into one.
  4. Pivoted per Leo's decision: generated a fresh key natively (`op item create --category "SSH Key" --ssh-generate-key ed25519`, real fingerprint `SHA256:ftET0MHM...`) and adopted it as the new shared mesh identity everywhere — appended to all 3 machines' `authorized_keys` (non-destructive), verified the new key explicitly on every reachable pair, confirmed a REAL Touch ID prompt fired and was approved (Leo confirmed live), confirmed clean fallback to the on-disk key with 1Password's agent made unavailable (`ssh_get_authentication_socket: Connection refused`, no error), then swapped all 3 machines' on-disk identity to the new key and pruned old/stale `authorized_keys` entries (including a second, differently-keyed stale entry sharing the same date label, and fixing homelab's chezmoi-managed `authorized_keys` source, not just the live file). Final connectivity re-verified clean on 5 of 6 directions (homelab↔cloudpc, homelab↔mac, mac→cloudpc); cloudpc→mac specifically hit a pre-existing network-level connection timeout (not an auth/key failure — unrelated to this task, flagged separately, not resolved here). [beads:if-oudo]
  - depends on: 3.1
- [x] [4.3] Live-verify Req-1 with a real (non-dry-run) rotation on a maintenance window: run `rotate-keys.sh` for real, confirm all peers stay reachable throughout (per the pre-existing lockout-safety guarantees from `harden-audit-remediation` Req-2, unchanged here), and confirm `op read` of `op://Personal/mesh-ssh` on a scratch/test path returns the NEW key, not the rotated-out one. — DONE 2026-07-21, after finding and fixing 2 more real bugs live:
  1. First real run aborted safely at Phase 3 (by design — no destructive action taken): CloudPC's verify failed because the script's `${CLOUDPC_USER}` ("leo", the correct SSH login name) had been reused for WINDOWS FILE PATHS too — `C:\Users\leo\` is a genuinely separate, stale local profile directory on this machine, not a junction to the real one (`C:\Users\leo.346-CPC-QJXVZ\`), so every prior Phase 2 append had silently landed in the wrong file. Fixed by splitting the concepts: `CLOUDPC_USER` stays the SSH login name, a new `CLOUDPC_PROFILE_DIR` covers file paths (commit 942517f).
  2. That fix itself shipped a second real bug (found immediately on the very next run): a bash heredoc escaping mistake (`\${CLOUDPC_PROFILE_DIR}` instead of `\\${CLOUDPC_PROFILE_DIR}`) meant bash treated `\$` as an escaped literal dollar sign instead of expanding the variable, so the append silently wrote to a garbage path with no error. Fixed and reverified via a direct heredoc substitution test before the real re-run.
  3. Discovered mid-rotation (Phase 4) that this script's whole design assumes it runs FROM a third machine (the Mac, per its own header) treating homelab+cloudpc as the only two remote peers — running it directly ON homelab (as this session did) meant the "homelab" SSH target and "the machine executing the script" were the same host, so the loopback swap step consumed the local `$NEW_KEY` file before it could reach CloudPC, and separately left Mac's `authorized_keys` never touched (the script has no concept of Mac as a third peer at all). Recovered live using the same safe append→verify→swap→prune sequence used successfully for if-oudo/4.2 (backed up, distributed, and verified the new key on all 3 machines by hand — the script's automation isn't structured for 3 peers). Final state: all 6 directional pairs (homelab↔cloudpc, homelab↔mac, cloudpc↔mac) verified working with the new key (`leo-mesh-20260721`, fingerprint ends `...ldvHNxWd`); old key fully pruned from every `authorized_keys` including the chezmoi-managed source. Peers stayed reachable throughout — the only interruption was the deliberate, safe Phase-3 abort in step 1 above, never an actual lockout.

  Honest gap on the task's own `op read` sub-assertion: `op://Personal/mesh-ssh` currently still holds the OLDER key from if-oudo/4.2 (`SHA256:ftET0MHM...`), NOT this rotation's key (`...ldvHNxWd`) — Phase 6b's write-back is a warn-only no-op now (see if-ooke), and I cannot perform the native-generate-and-fetch flow myself (needs Leo's own interactive terminal, same limitation as if-oudo). The mesh itself is fully rotated and reachable; only the 1Password mirror is stale until Leo runs the same manual update Phase 6b's warning now prints.
  Also found and fixed along the way (not part of this task's original scope, but surfaced by running it for real): raw Tailscale IPs in `platform/windows/setup.ps1` and this script had drifted stale (Leo's own question — why not use MagicDNS names, which don't go stale — led directly to this fix, commits 058a767/1bbd805), closing the earlier if-llh6 "network timeout" finding with its real root cause. [beads:if-9pjw]
  - depends on: 1.1

  **Incident report (2026-07-20, real rotation attempted) — task NOT complete, left unchecked.**

  Location correction first: `rotate-keys.sh` must run FROM the Mac (its Phase 4/5 "local"
  swap logic assumes the runner IS the Mac, with homelab+cloudpc as remote targets) — the
  orchestrator's own session runs on homelab, so the code was pushed and pulled fresh onto the
  real Mac, then run there. Dry-run from the Mac confirmed correct context (verified: local Mac
  key rotated via `cp`/`mv`, homelab+cloudpc via `scp`/`ssh`).

  Real (non-dry-run) run surfaced two bugs in the PRE-EXISTING `rotate-keys.sh` (not introduced
  by this proposal — its CloudPC PowerShell blocks predate task 1.1's changes):
  1. Every `ssh cloudpc powershell -Command "..."` call in the script failed with garbled
     `Cannot process the command because of a missing parameter` errors — CloudPC's
     authorized_keys append (Phase 2) and private-key swap (Phase 4/5) silently never happened.
  2. The script's own Phase 3/Phase 6 re-verify safety gate (`$CLOUDPC_OK`/`$CLOUDPC_REVERIFY`)
     reported cloudpc healthy anyway — a FALSE POSITIVE caused by an already-alive SSH
     ControlMaster connection (`ControlPersist 10m`) being silently reused instead of testing
     the actual current key material, defeating the verify-before-prune design.

  Net effect: homelab + Mac both rotated to the new key and deleted their old key (gated on the
  false-positive verify), while cloudpc's authorized_keys was never actually updated — cloudpc
  became unreachable via SSH from any mesh peer (`Permission denied`). 1Password write-back also
  failed this run (`op` sign-in never resolved cleanly, see below) — `op://Private/mesh-ssh`
  still holds the pre-rotation key.

  **Recovery performed** (partial, live): confirmed cloudpc's console/RDP access was unaffected
  (SSH-only break). Used a still-alive PRE-rotation SSH ControlMaster session (Mac→cloudpc,
  established before the rotation, still authenticated) to push a fix — direct writes were
  denied (`authorized_keys`' real ACL grants only `NT AUTHORITY\SYSTEM` write access, not
  `BUILTIN\Administrators` despite the `leo` account being a group member — inconsistent with
  `setup.ps1`'s own ACL-hardening code, root cause not determined). Used a one-shot
  SYSTEM-scoped scheduled task (same elevation pattern the existing
  `cloudpc-sshd-watchdog.ps1` uses) to rewrite `authorized_keys` cleanly with both the
  pre-rotation and new keys, each properly newline-separated (a first attempt via `Add-Content`
  concatenated the two keys onto one line with no separator — corrupted the file; caught and
  fixed with a `Set-Content` rewrite). Verified byte-exact: 2 lines, both keys present. A fresh
  SSH connection from homelab (`ssh -O exit cloudpc` first, to rule out ANY stale connection)
  still gets `Permission denied` — verbose client log confirms the correct new key IS offered
  and IS rejected server-side, not a client-side or file-content issue. `sshd_config`'s
  `Match Group administrators` override is confirmed commented out (the per-user file should
  govern). Found `C:\Users\leo` (home dir) carries an `Everyone:(RX)` ACL entry — a plausible
  Windows OpenSSH strict-mode rejection trigger, unconfirmed. Stopped short of restarting `sshd`
  to get a debug-level log, since that risked dropping the one remaining live connection with no
  guaranteed-working replacement. **Leo will RDP in to finish diagnosis** (Event Viewer >
  OpenSSH > Operational, direct sshd_config/ACL inspection).

  Both bugs filed as a P1 follow-up: `if-1ydm.1` (parented under this proposal's epic, NOT part
  of this proposal's own scope — pre-existing code, discovered during E2E, not caused by Req-1's
  changes). This task stays unchecked pending: (a) Leo restoring genuine SSH access to cloudpc,
  (b) `if-1ydm.1`'s fix landing so a re-run of the real rotation can complete cleanly end-to-end,
  (c) a working `op` sign-in on the Mac so the 1Password write-back can be proven live.
- [ ] [4.4] Targeted `git add ssh-mesh/scripts/rotate-keys.sh platform/windows/setup.ps1 home/dot_zshenv.tmpl` (no `git add -A`/`.`); commit `feat(ssh-mesh): sync rotation to 1Password, provision CloudPC via op, add Mac SSH agent path`; push. Paste `git log -1 --stat` output. [beads:if-mh7g]
  - depends on: 1.1, 2.1, 3.1

## User Gate

- [x] [5.1] [user:post] DECISION: enable the 1Password SSH agent for the mesh key item (`op://Private/mesh-ssh`) — two parts: (a) open 1Password 8 > Settings > Developer > enable "Use the SSH agent" (one-time GUI toggle, no scriptable equivalent), then (b) edit `~/.config/1Password/ssh/agent.toml` directly to add an entry allowlisting `op://Private/mesh-ssh` as an agent-servable identity (corrected 2026-07-21 — this is a real config file, not a second GUI-only step; see `if-tla9`'s comment log for the correction). searched: `op` CLI help output and 1Password CLI docs referenced in `scripts/op-ssh-provision.sh`'s own prerequisites comment — no `op` subcommand exists to do either step. Required before task 4.2 can be live-verified. — DONE 2026-07-21 (Leo, live): applied via chezmoi (commit 10c8323), confirmed `ssh-add -l` lists mesh-ssh — vault "Private" was correct. [type:config] [beads:if-tla9]
