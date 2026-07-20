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

- [ ] [4.1] Live-verify Req-2 on the real CloudPC machine: delete the on-disk mesh key (back it up first), run the updated `setup.ps1` provisioning step, confirm the key materializes via `op.exe` without any manual transfer step, and confirm SSH connectivity to the mesh still works with the freshly-materialized key. Restore/clean up per the mesh's existing safety conventions (never leave the machine unreachable). [beads:if-rgsw]
  - depends on: 2.1
- [ ] [4.2] Live-verify Req-3 on the real Mac: with the 1Password app running and the SSH agent enabled for the mesh key item (one-time manual toggle — see the User Gate task below), run an interactive `ssh homelab` (or an SSH-backed `git fetch`) and confirm a visible Touch ID/system-auth prompt fires. Then quit the 1Password app (or otherwise make the agent socket unavailable) and re-run the same command, confirming it falls back to the on-disk key with no error. [beads:if-oudo]
  - depends on: 3.1
- [ ] [4.3] Live-verify Req-1 with a real (non-dry-run) rotation on a maintenance window: run `rotate-keys.sh` for real, confirm all peers stay reachable throughout (per the pre-existing lockout-safety guarantees from `harden-audit-remediation` Req-2, unchanged here), and confirm `op read` of `op://Private/mesh-ssh` on a scratch/test path returns the NEW key, not the rotated-out one. [beads:if-9pjw]
  - depends on: 1.1
- [ ] [4.4] Targeted `git add ssh-mesh/scripts/rotate-keys.sh platform/windows/setup.ps1 home/dot_zshenv.tmpl` (no `git add -A`/`.`); commit `feat(ssh-mesh): sync rotation to 1Password, provision CloudPC via op, add Mac SSH agent path`; push. Paste `git log -1 --stat` output. [beads:if-mh7g]
  - depends on: 1.1, 2.1, 3.1

## User Gate

- [ ] [5.1] [user:post] DECISION: enable the 1Password SSH agent for the mesh key item (`op://Private/mesh-ssh`) — open 1Password 8 > Settings > Developer > enable "Use the SSH agent", then flag the mesh key item itself as usable with the agent. This is a one-time GUI toggle with no scriptable equivalent. searched: `op` CLI help output and 1Password CLI docs referenced in `scripts/op-ssh-provision.sh`'s own prerequisites comment — no `op` subcommand exists to enable the SSH agent or flag an item for agent use; it is an app-settings-only toggle. Required before task 4.2 can be live-verified. [type:config] [beads:if-tla9]
