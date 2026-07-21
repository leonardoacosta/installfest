# ssh-mesh

## ADDED Requirements

### Requirement: Key rotation stays synchronized with 1Password
The mesh's SSH key rotation tooling SHALL keep the 1Password item that fresh-machine provisioning reads from in sync with the currently-active keypair, so that a rotation never leaves the 1Password-sourced material stale.

#### Scenario: Rotation succeeds and writes back to 1Password
- **WHEN** `rotate-keys.sh` completes its existing peer re-verify gate successfully (both peers reachable with the swapped default identity)
- **THEN** the new private and public key material is written to the 1Password item referenced by `OP_SSH_ITEM`, before old-key pruning completes

#### Scenario: Dry-run narrates without touching 1Password
- **WHEN** `rotate-keys.sh --dry-run` is run
- **THEN** the 1Password write-back step is narrated but no `op` write command is executed and the 1Password item is unchanged

#### Scenario: 1Password write-back failure does not roll back a successful rotation
- **WHEN** the peer re-verify gate has passed and the 1Password write-back call fails (network, auth, or `op` error)
- **THEN** the script reports the failure clearly but does not revert the already-verified peer key swap — the mesh remains reachable on the new key

### Requirement: CloudPC provisions its SSH key via 1Password CLI
CloudPC (Windows) setup SHALL materialize the mesh SSH keypair via the 1Password CLI on a fresh machine, matching the existing Mac/homelab provisioning behavior, before falling back to manual key transfer.

#### Scenario: Fresh CloudPC machine with 1Password CLI available and signed in
- **WHEN** `platform/windows/setup.ps1` runs on a machine with no on-disk mesh key and a working, signed-in `op.exe`
- **THEN** the mesh keypair is materialized from the 1Password item without any manual key-transfer step

#### Scenario: 1Password CLI unavailable or sign-in fails
- **WHEN** `op.exe` is missing, or sign-in fails, or the session is non-interactive
- **THEN** the provisioning step fails open with a clear message and the existing manual-transfer path remains available — the setup script does not hang or abort

#### Scenario: Key already present on disk
- **WHEN** `platform/windows/setup.ps1` runs and a mesh key already exists on disk
- **THEN** the 1Password provisioning step is skipped (idempotent, no overwrite)

### Requirement: Mac interactive SSH uses the 1Password SSH agent with on-disk fallback
On Mac, interactive outbound SSH connections SHALL prefer the 1Password SSH agent for key custody, while the existing on-disk key remains available as an automatic fallback identity. homelab and CloudPC key custody SHALL NOT change as a result of this requirement.

#### Scenario: 1Password app running and unlocked
- **WHEN** an interactive SSH connection is initiated from Mac (e.g. `ssh homelab`, a `git` operation over SSH) with the 1Password app running and the SSH agent enabled
- **THEN** the connection authenticates via the 1Password agent, surfacing a Touch ID / system-auth prompt, without requiring the on-disk private key

#### Scenario: 1Password app not running or agent socket unavailable
- **WHEN** the same interactive SSH connection is initiated and the 1Password agent socket is unavailable
- **THEN** OpenSSH automatically falls back to the existing on-disk `IdentityFile ~/.ssh/id_ed25519` with no error and no manual intervention

#### Scenario: homelab and CloudPC unaffected
- **WHEN** this requirement ships
- **THEN** homelab's and CloudPC's SSH key custody (on-disk key, `op-ssh-provision.sh`/CloudPC provisioning per the other requirements above) is unchanged — no agent dependency is introduced on either machine
