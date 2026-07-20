---
order: 0720a
---

# Proposal: Harden 1Password-CLI SSH mesh key management

## Change ID
`harden-ssh-mesh-1password-integration`

## Summary
1Password CLI (`op`) is already the SSH mesh's key-distribution mechanism (`scripts/op-ssh-provision.sh`, commit `4c5002a`), but it has three gaps found during `/explore`: key rotation never writes the new key back to 1Password, CloudPC has no `op`-based provisioning at all, and there is no path for the 1Password *SSH agent* (as opposed to CLI-based file distribution) anywhere in the mesh. This proposal closes all three, scoped to what a personal 3-machine mesh actually needs.

## Context
- depends on: none
- touches: `ssh-mesh/scripts/rotate-keys.sh`, `scripts/op-ssh-provision.sh`, `platform/windows/setup.ps1`, `home/dot_zshenv.tmpl`, `home/private_dot_ssh/config.tmpl`
- Prior art: `scripts/op-ssh-provision.sh` (commit `4c5002a`, 2026-06-16) — existing `op read`-based key materialization for Mac + homelab. `openspec/changes/archive/2026-07-02-harden-audit-remediation` Req-2 already made `rotate-keys.sh` lockout-safe (append-verify-swap-prune ordering, no auto force-push) — this proposal extends that same script's Phase 8 (chezmoi-source sync) with a 1Password write-back step, it does not touch or re-litigate the lockout-safety mechanics Req-2 already fixed.
- Explicit prior decision honored: `harden-audit-remediation`'s own Scope OUT section already recorded "Per-machine mesh keys" as a deliberate exclusion. This proposal does not revisit that — the mesh stays on a single shared keypair; only where/how that keypair is distributed and used changes.
- `docs/audit/ssh-mesh.md` (2026-03-25) — baseline audit; rated Tailscale SSH #1, 1Password SSH agent #2 among alternatives to the (then-manual) key distribution. Its "same key double-duty for git signing" FAIL is stale — no `signingkey`/`gpgsign` is configured anywhere in this repo today; not treated as a finding here.

## Motivation
1. **Rotation/1Password desync (real bug)**: `rotate-keys.sh` rewrites `authorized_keys` on every peer and the chezmoi source, but never touches the 1Password item (`op://Private/mesh-ssh`). After any rotation, a fresh-machine bootstrap via `op-ssh-provision.sh` silently fetches the rotated-out, now-invalid private key.
2. **CloudPC has zero `op` integration**: `platform/windows/setup.ps1` explicitly requires manual key transfer ("SSH key must be transferred securely - do not embed in scripts"), unlike Mac/homelab which both bootstrap through `op-ssh-provision.sh`.
3. **1Password is used for distribution only, never for runtime key custody**: nothing in the mesh uses the 1Password SSH agent. For Mac's interactive outbound sessions (the one machine where a live, unlockable 1Password desktop app is actually running), the agent would let the private key stay out of plaintext-on-disk custody for interactive use, gated by Touch ID/system auth per connection — while leaving the on-disk key untouched everywhere it is structurally required.

## Requirements

### Req-1: Key rotation writes back to 1Password
`ssh-mesh/scripts/rotate-keys.sh` MUST, after the existing Phase 6 re-verify gate passes (both peers confirmed reachable with the swapped default identity) and before Phase 7 pruning completes, write the new private+public key material to the 1Password item referenced by `OP_SSH_ITEM` (default `op://Private/mesh-ssh`, same variable `op-ssh-provision.sh` already uses) via `op item edit`. This step MUST respect the script's existing `--dry-run` flag (narrate, touch nothing) and MUST be gated the same way the existing Phase 6 re-verify gate already is — if the 1Password write fails, the script reports it clearly but does NOT roll back the already-verified peer key swap (the mesh must stay usable even if the 1Password write-back fails; a failed write-back is a follow-up-fixable gap, not a mesh-breaking condition).

### Req-2: CloudPC gets `op`-based provisioning
`platform/windows/setup.ps1` MUST gain a provisioning step, run before the existing manual-transfer fallback, that installs/uses the 1Password CLI (`op.exe`) to materialize the mesh keypair the same way `scripts/op-ssh-provision.sh` does for Mac/homelab: idempotent (skip if the key is already on disk), fails open to the existing manual-transfer path with a clear message if `op` is unavailable or sign-in fails (no hang on a headless/service-account SSH session — same non-interactive guard `op-ssh-provision.sh` already applies via its TTY check).

### Req-3: Mac-only 1Password SSH agent, on-disk key preserved as fallback
`home/dot_zshenv.tmpl` MUST export `SSH_AUTH_SOCK` to 1Password's macOS agent socket inside a `{{ if eq .machine "mac" -}}` conditional block (matching the file's existing per-machine conditional pattern, e.g. the homelab `BROWSER` block). `home/private_dot_ssh/config.tmpl` and `scripts/op-ssh-provision.sh` MUST NOT change on Mac's on-disk-key path — the existing `IdentityFile ~/.ssh/id_ed25519` stays as OpenSSH's automatic fallback identity when the agent socket is unavailable (1Password app not running, first boot before setup). homelab and CloudPC key custody is unchanged by this requirement — both stay on-disk-only (homelab is headless with no GUI to host a live agent; CloudPC's SSH access runs as a session-0 service account with no interactive desktop, per the `bb-azure-ops` skill's already-documented DPAPI/no-interactive-app constraint on that same box).

## Scope
- **IN**: The 5 touched files above. Writing the new key to 1Password on rotation; CloudPC provisioning via `op`; Mac SSH agent env wiring with on-disk fallback preserved.
- **OUT**: Per-machine distinct keys (already a recorded exclusion from `harden-audit-remediation`, not revisited here). SSH agent forwarding across mesh hops (Mac→homelab→cloudpc) — a real future decision (widens the trust boundary), deliberately not bundled in. Adopting Tailscale SSH instead of the current key-based approach (the audit's own top-rated alternative, but a different technology path than "use 1Password CLI for SSH key management," which is what this proposal answers). Finishing the stalled Mosh client install on Mac/CloudPC (`mosh` is currently homelab-only via `scripts/install-arch.sh`, unused/unwired elsewhere) — unrelated to SSH key custody; Mosh's own bootstrap SSH call inherits whatever this proposal ships with zero additional work, so there is nothing here for Mosh to depend on.

## Impact
| Area | Change |
|------|--------|
| `ssh-mesh/scripts/rotate-keys.sh` | New post-verify, pre-prune step: write rotated key to 1Password; respects `--dry-run` |
| `platform/windows/setup.ps1` | New `op`-based provisioning step before the manual-transfer fallback |
| `home/dot_zshenv.tmpl` | New Mac-only `SSH_AUTH_SOCK` export block |
| `home/private_dot_ssh/config.tmpl`, `scripts/op-ssh-provision.sh` | Unchanged (on-disk fallback path preserved verbatim) |

## Risks
| Risk | Mitigation |
|------|-----------|
| 1Password write-back fails mid-rotation | Gated strictly after the existing peer re-verify gate; failure is reported but does not roll back an already-successful peer key swap — the mesh stays reachable, only the 1Password mirror is stale (same failure mode as today, not worse) |
| CloudPC `op.exe` sign-in hangs a headless/service-account session | Reuses `op-ssh-provision.sh`'s existing TTY-guard pattern; fails open to the pre-existing manual-transfer path |
| Mac agent adoption breaks interactive SSH if the 1Password app isn't running | On-disk `IdentityFile` is explicitly preserved as an automatic OpenSSH fallback — not removed, not made conditional on the agent working |
| Live testing requires a real key rotation across 3 real machines | Req-1's 1Password write-back is exercised via the existing `--dry-run` flag first; a real rotation is Leo-only per the existing script's design (unchanged by this proposal) |

## Testing
- Req-1: `rotate-keys.sh --dry-run` narrates the 1Password write-back step without touching any peer or the 1Password item; a scoped unit-style check (or manual `op item get` diff) confirms the real (non-dry-run) path only fires after Phase 6's re-verify gate passes, never before.
- Req-2: manual verification on the real CloudPC machine — delete the on-disk key, run the updated `setup.ps1` provisioning step, confirm the key materializes via `op.exe` without a manual transfer.
- Req-3: manual verification on the real Mac — with the 1Password app running and the SSH agent enabled, an interactive `ssh homelab`/`git fetch` over SSH triggers a visible Touch ID/system-auth prompt; with the 1Password app quit, the same command falls back to the on-disk key automatically with no error.
- `scripts/check.sh` (existing repo verification baseline) MUST still pass after all changes — shell syntax, chezmoi template render, and shellcheck cover every touched file.

## Done Means
- Rotating the mesh key via `rotate-keys.sh` (non-dry-run) leaves the 1Password item holding the new key — a subsequent `op read` on a fresh machine materializes the current, not the rotated-out, key.
- A fresh CloudPC bootstrap materializes the mesh key from 1Password without a manual `scp`/copy-paste step, falling open to the existing manual path only when `op` is genuinely unavailable.
- On Mac, an interactive SSH connection authenticates via the 1Password agent (visible auth prompt) when the app is running and unlocked, and transparently falls back to the existing on-disk key when it is not — with zero change to homelab's or CloudPC's key custody.
