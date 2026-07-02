# Proposal: Harden dotfiles per 2026-07-02 audit (plans 001-005)

## Change ID
`harden-audit-remediation`

## Summary
Remediate the five highest-leverage findings from the 2026-07-02 codebase audit: close the unauthenticated file-server that exposes `~/dev` and `~/.claude` across the tailnet, make SSH key rotation lockout-safe and stop its unattended force-push, wire the orphaned secret-scan pre-commit hook into the active hook chain, add the first one-command verification baseline, and consolidate the seven hand-rolled `projects.toml` parsers (fixing a latent macOS python-3.9 break). Detailed executor steps live in `docs/plans/001-005`; this change tracks them as one remediation unit.

## Context
- depends on: none (self-contained; Req-5 depends on Req-4 internally)
- touches: `scripts/file-server.py`, `home/dot_config/systemd/user/file-server.service`, `home/dot_zsh/rc/linux.zsh`, `ssh-mesh/scripts/rotate-keys.sh`, `home/run_onchange_set-git-hooks.sh.tmpl`, `scripts/hooks/pre-commit`, `scripts/check.sh`, `scripts/lib/registry.sh`, `package.json`, `platform/homebrew/Brewfile`, `scripts/install-arch.sh`, `README.md`, `scripts/generate-raycast.sh`, `scripts/mux-remote.sh`, `scripts/cmux-workspaces.sh`, `home/dot_local/bin/executable_copen`, `packages/workspace/bin/wsenv`, `packages/workspace/bin/generate-profiles`, `packages/workspace/bin/wk-ready`
- Plans: `docs/plans/001-file-server-token-auth.md`, `docs/plans/002-rotate-keys-atomic.md`, `docs/plans/003-unify-pre-commit-hooks.md`, `docs/plans/004-verification-baseline.md`, `docs/plans/005-registry-parser-consolidation.md`, index at `docs/plans/README.md`
- Related: `home/dot_local/bin/executable_copen` overlaps the auth-resilience work — coordinate Req-5 with any in-flight edits to that file (plan 005 STOP condition covers this)
- Audit provenance: written against commit `2068bad`; each plan carries its own drift-check SHA

## Motivation
A four-category read-only audit (architecture/debt, correctness/security, DX/docs, direction) surfaced ~20 vetted findings. Five are high-leverage and independently shippable:

1. `scripts/file-server.py` binds `0.0.0.0:8787` with no auth, serving every file under `~/dev` and `~/.claude` — including every repo's gitignored `.env` — to the whole Tailscale tailnet, which includes the employer-managed CloudPC. This is the only live remote-exposure finding.
2. `ssh-mesh/scripts/rotate-keys.sh` overwrites each peer's `authorized_keys` before verifying the new key and deletes the old-key backup unconditionally, so any mid-run failure risks a mesh lockout; it also runs `bfg` + `git push --force` on every rotation, unattended.
3. The repo ships a secret-scanning pre-commit hook (`.githooks/pre-commit`) that never runs — `core.hooksPath` is `.beads/hooks`, which never invokes it. The same gap bypasses the raycast-regen guard.
4. There is no one-command way to know the repo is healthy — no CI, no `check` script, no template/zsh/shellcheck smoke — so a broken bootstrap is discovered only at `chezmoi apply` time on a fresh machine.
5. Seven scripts each hand-roll "find a python3 with tomllib" to read `home/projects.toml`; three still call bare `python3` and break on macOS shells where `/usr/bin/python3` (3.9, no `tomllib`) resolves first.

## Requirements

### Req-1: Token-authenticate the file-server (plan 001)
`scripts/file-server.py` MUST require a shared token (query param `t=` or `fs_token` cookie, compared with `hmac.compare_digest`) on every request; tokenless requests return 403. The token is generated to `~/.local/state/file-server.token` (0600) on first start. `flink` (`home/dot_zsh/rc/linux.zsh`) appends the token to the URLs it prints, with a bare-URL fallback when the token file is absent. Query-string is stripped before path resolution. `ALLOWED_ROOTS` and the 0.0.0.0 bind are unchanged (token is the fix; narrowing is deferred).

### Req-2: Lockout-safe key rotation, no auto force-push (plan 002)
`ssh-mesh/scripts/rotate-keys.sh` MUST append the new public key to every peer's `authorized_keys` and verify new-key connectivity to all peers BEFORE swapping any private key or removing any old key; the local `.old` deletion moves inside the both-peers-verified gate. A `--dry-run` flag prints the phase plan and touches nothing. The `bfg` + `git reflog expire` + `git push --force` block is removed entirely (rotation is not history surgery). The chezmoi-source `authorized_keys` sync is preserved.

### Req-3: One canonical pre-commit chain on every machine (plan 003)
`scripts/hooks/pre-commit` MUST run `.githooks/pre-commit` (secret scan) as its first step and block the commit on a hit. `home/run_onchange_set-git-hooks.sh.tmpl` MUST inject an idempotent, marker-guarded `IF-PRECOMMIT v1` delegation block into `.beads/hooks/pre-commit` that calls `scripts/hooks/pre-commit` and propagates a non-zero exit (no `|| true`), mirroring the existing IF-DEPLOY / IF-POSTMERGE pattern. `.githooks/pre-commit` stays the canonical scanner (now invoked, not orphaned).

### Req-4: One-command verification baseline (plan 004)
A new `scripts/check.sh` (wired as `npm run check`) MUST run, in one pass and reporting all sections: `zsh -n` over zsh config, `bash -n`/`sh -n` over the shell scripts, `chezmoi execute-template` render (plus `bash -n` on rendered `*.sh.tmpl`), `shellcheck` at error severity, and conditional `terraform validate`. It MUST exit 0 on the current tree (pre-existing shellcheck findings recorded in a `SHELLCHECK_EXCLUDE` burn-down list, not silenced globally) and non-zero on planted breakage. `shellcheck` is added to the Brewfile and `install-arch.sh`; README gains a short "Verifying" note.

### Req-5: Shared projects.toml resolver (plan 005, depends on Req-4)
A new `scripts/lib/registry.sh` (sourced with the source-guard strict-mode idiom) MUST expose `registry_path()` and a cached `registry_python()` implementing the tomllib-python probe loop. All seven consumers — `wsenv`, `generate-profiles`, `wk-ready`, `cmux-workspaces.sh` (dedup) and `generate-raycast.sh`, `mux-remote.sh`, `executable_copen` (bug fix) — MUST use the lib instead of an inline probe or bare `python3`. Each consumer's python payload logic is unchanged. `generate-raycast.sh` output MUST be byte-identical pre/post. `scripts/check.sh` (Req-4) is the before/after gate.

## Scope
- **IN**: The 19 touched files above plus two new files (`scripts/check.sh`, `scripts/lib/registry.sh`). Executor detail lives in `docs/plans/001-005`.
- **OUT**: Narrowing `ALLOWED_ROOTS` / binding the file-server to the tailscale interface (further hardening, deferred). Per-machine mesh keys (recorded design choice). Adding CI (`.github/`). The `az`-wrapper and `.notified-epoch` fixes (fold into the auth-resilience change per the audit backlog). Any `registry_resolve()` API or python-payload merge (speculative). The `paste-image.sh` refactor (investigate-only).

## Impact
| Area | Change |
|------|--------|
| file-server | Tokenless tailnet requests now 403; `flink` URLs carry `?t=` |
| Key rotation | Append-verify-remove ordering; `--dry-run`; no more auto force-push |
| Pre-commit | Secret scan + raycast-regen now run on beads machines (were dead) |
| New files | `scripts/check.sh`, `scripts/lib/registry.sh` |
| Bootstrap | `shellcheck` added to Brewfile + `install-arch.sh` |
| macOS launchers | `copen`/raycast-gen/`mux-remote` stop breaking on system python 3.9 |

## Risks
| Risk | Mitigation |
|------|-----------|
| `flink` has a consumer that can't carry a query param | Plan 001 STOP condition greps consumers before changing URL format |
| Rotation rewrite tested only statically | Plan 002 forbids live rotation; `--dry-run` + `bash -n` + shellcheck; Leo runs a real rotation manually next cycle |
| Adding `set -e`-grade rigor surfaces latent script failures | `check.sh` runs `set -uo pipefail` (reports all sections), starts green on current tree via exclude list |
| Registry refactor changes launcher behavior | `generate-raycast.sh` byte-identical-output check + per-consumer smokes; Req-4 gate before/after |
| `executable_copen` in-flight overlap | Plan 005 STOP condition: compare on-disk vs HEAD before editing the parse region |
