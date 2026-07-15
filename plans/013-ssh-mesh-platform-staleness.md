# Plan 013: Retire the superseded ssh-mesh playbook lane and de-stale the platform/windows CloudPC flow

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**:
> `git diff --stat 9399b92..HEAD -- ssh-mesh/ platform/ infra/ .gitignore .claude/workflows/project-mgmt-audit.js`
> If any in-scope file changed since this plan was written, compare the
> "Current state" excerpts against the live code before proceeding; on a
> mismatch, treat it as a STOP condition.
>
> **NEVER run `bash platform/bootstrap.sh` with no arguments** during this
> work — it launches a full interactive machine bootstrap (Apple 2FA gates,
> repo clone loops). Only the `--help` / bad-arg invocations in Step 5's
> verification are safe, and only AFTER Step 5's edit lands.

## Status

- **Priority**: P2
- **Effort**: M
- **Risk**: LOW (docs, dead-lane deletions, one policy change — nothing here is on a runtime hot path; the live SSH lane is chezmoi-owned and untouched)
- **Depends on**: none
- **Category**: tech-debt / docs
- **Planned at**: commit `9399b92`, 2026-07-14

## Why this matters

The repo migrated its SSH mesh to chezmoi ownership (2026-06-14, commit `938e446`) and migrated terminals off WezTerm to Ghostty, and was renamed `dotfiles` -> `installfest` and relocated `~/dev/if` -> `~/dev/personal/installfest` — but `ssh-mesh/` and `platform/windows/` were never re-synced. The result is actively dangerous drift, not just cosmetic: `ssh-mesh/README.md` publishes a **rotated-away** public key + fingerprint as current, documents a superseded setup lane whose scripts overwrite the chezmoi-managed `~/.ssh/config`/`authorized_keys` (piping `setup-homelab.sh` over ssh — its documented usage — genuinely truncates the managed `authorized_keys`), and the one documented CloudPC setup command points at a path that does not exist. `platform/windows/setup.ps1` has a permanently-broken section (copies a file deleted repo-wide), prints cold-start instructions whose clone URL 404s and whose installer (`./install.sh`) was deleted, and hardcodes a git identity that diverges from the chezmoi-managed one. This plan deletes the superseded lane, repoints every stale claim at the live chezmoi sources, and closes two small infra hygiene gaps.

## Current state

All excerpts below are fresh reads at commit `9399b92`.

### The live lane (context — do NOT modify these)

- `home/private_dot_ssh/config.tmpl` — chezmoi-managed `~/.ssh/config` for all machines. Line 12: `# Homelab via Tailscale (always-on, no LAN probe needed)` — the LAN Match-probe was deliberately dropped.
- `home/private_dot_ssh/private_authorized_keys` — chezmoi-managed `~/.ssh/authorized_keys`. Line 10 carries the CURRENT shared pubkey, comment tag `leo-mesh-20260325`. Header (lines 8-9): "Rotation: ssh-mesh/scripts/rotate-keys.sh regenerates the shared key AND rewrites this file, so the two never drift."
- `ssh-mesh/scripts/rotate-keys.sh` — live rotation script (hardened 2026-07, `9f7f317`).
- `ssh-mesh/scripts/remote/` — cmux-bridge, live (chezmoi-built, launchd-run).

### The superseded lane (ENT-01/ENT-02 — to delete, gated)

- `ssh-mesh/scripts/setup-mac.sh:19` — `cp "$MESH_DIR/keys/id_ed25519" ~/.ssh/id_ed25519`; `ssh-mesh/keys/` does not exist on disk and is gitignored (`.gitignore:39: ssh-mesh/keys/`). Line 35: `cp "$MESH_DIR/configs/mac.config" ~/.ssh/config` — clobbers the chezmoi-managed config.
- `ssh-mesh/scripts/setup-homelab.sh:23` — `cp "$MESH_DIR/configs/homelab.config" ~/.ssh/config`; line 41: `cat ~/.ssh/id_ed25519.pub > ~/.ssh/authorized_keys` — truncates the chezmoi-managed file. Its keys-check is a soft if/else, so the documented remote-pipe usage (line 4: `cat setup-homelab.sh | ssh homelab bash`) proceeds to the truncation.
- `ssh-mesh/scripts/deploy-to-homelab.sh:20` — `scp "$MESH_DIR/configs/homelab.config" homelab:~/.ssh/config`.
- `ssh-mesh/configs/mac.config:6` — still carries the LAN Match-probe (`Match host homelab exec "bash -c 'exec 3<>/dev/tcp/192.168.1.100/22' ..."`) that the live template dropped. `homelab.config` / `cloudpc.config` are stale copies of what `config.tmpl` now renders; `cloudpc.config` has zero executable consumers anywhere.
- Caller sweep (verified at 9399b92): the ONLY live references to these six files are `ssh-mesh/README.md:110,118` and each other. `docs/audit/*.md` and `docs/plans/002-rotate-keys-atomic.md:104` mention them but are historical audit/plan documents, not callers — leave those untouched.
- `.claude/workflows/project-mgmt-audit.js:81` — an audit prompt says "Compare ssh-mesh/README.md ... and ssh-mesh/configs/*.config against live" — must be updated when the configs are deleted or the audit chases ghosts.

### The stale README (ENT-03 / PLAT-03)

`ssh-mesh/README.md` (229 lines):

- Lines 72-73 publish a fingerprint + full public key that do NOT match the live rotated key in `home/private_dot_ssh/private_authorized_keys:10` (tag `leo-mesh-20260325`). Drift-prone key material must not live in the README — replace with a pointer.
- Line 48: `- **Special:** Smart routing (probes LAN before Tailscale fallback)` — describes the dropped LAN probe.
- Lines 104-128 "Quick Setup" document the superseded scripts as canonical (`bash scripts/setup-mac.sh` at 110, `bash scripts/setup-homelab.sh` at 118) and line 125 invokes `windows\setup.ps1` "from repo root" — no root `windows/` dir exists; the file is `platform/windows/setup.ps1`. This is the only documented invocation surface for setup.ps1.
- Lines 130-145 "Mac SSH Config Explained" document the LAN-probe smart routing as the current config; lines 218-220 troubleshoot "Mac LAN Probe Hanging". Both describe config that no longer exists.
- Line 227 (Security Notes) correctly points at `rotate-keys.sh` — keep.

### The stale Windows flow (PLAT-01/02/04/05)

`platform/windows/setup.ps1` (845 lines):

- Line 10 (`.DESCRIPTION`): `- WezTerm terminal with Mac keyboard bindings`.
- Line 44: `"wez.wezterm"` in the winget app list.
- Lines 574-597: section-4 header comment + `Install-WezTermConfig`, which does `$sourceConfig = Join-Path $scriptDir "wezterm-windows.lua"` (line 588) then `Test-Path` (line 590). `wezterm-windows.lua` was deleted repo-wide in the Ghostty migration (commit `de5a969`; `git ls-files '*wezterm*'` returns only `docs/audit/wezterm.md`), so this section is a permanent no-op warning. The repo direction is delete (Ghostty is the terminal; it has no Windows build, and no Windows terminal config replaces this).
- Line 29: `DotfilesRepo   = "git@github.com:leonardoacosta/dotfiles.git"` — the `dotfiles` repo no longer exists (renamed `installfest`; GitHub does not redirect).
- Lines 518-522 print WSL2 bootstrap steps `git clone $($Config.DotfilesRepo) if` / `cd if && ./install.sh` — `install.sh` was deleted (commit `b7f87f8`); the live flow is chezmoi two-phase per root `README.md` § Quick Start: `chezmoi init --apply leonardoacosta/installfest --source ~/dev/personal/installfest`.
- Lines 650-651: `git config --global user.name "leonardoacosta"` / `user.email "leo@leonardoacosta.dev"` — diverges from the chezmoi-managed canonical identity `leo@priceless.dev` (see `platform/bootstrap.sh:169`: "~/.gitconfig now pins leonardoacosta <leo@priceless.dev>").
- Line 793: banner item `3. Windows apps (WezTerm, Cursor, VS Code, VS Studio, etc.)`; line 794: `4. WezTerm config (Mac keyboard bindings)`; line 808: sections-array entry `@{ Name = "WezTerm config";         Fn = { Install-WezTermConfig } }`. Numbered internal section headers exist at lines 132, 437, 538, 575, 600, 639, 682, 723, 754 (`# 1.` .. `# 9.`).

`platform/windows/install.cmd`:

- Lines 19-20: a comment + `powershell -Command "Set-ExecutionPolicy Bypass -Scope Process -Force"` — the policy dies with that child process; line 30 already passes `-ExecutionPolicy Bypass` to the real invocation. Dead code, flagged by `docs/audit/windows.md` in March and never fixed.

### The --help footgun (PLAT-06)

`platform/bootstrap.sh:43` advertises `bash platform/bootstrap.sh --help`, but the script parses NO arguments (verified: no `getopts`, no `case "$1"`, no top-level `$1` read). Running `--help` today executes the FULL interactive bootstrap. Strict-mode line is `set -uo pipefail` at line 47 (deliberately not `-e`).

### The dead debug contract (PLAT-07)

`platform/raycast-scripts/paste-image.sh:29`: comment `# Set PASTE_IMAGE_DEBUG=1 to also echo to terminal.` — `PASTE_IMAGE_DEBUG` is never read anywhere in the 710-line script (line 32 unconditionally `exec > >(tee -a "$LOG_FILE") 2>&1`). This file is hand-written, NOT generated by `scripts/generate-raycast.sh` (verified: zero `paste-image` matches in the generator).

### The dead twins (ENT-04)

`ssh-mesh/scripts/fetch-all-cloudpc.sh` (WSL paths) and `fetch-all-cloudpc.ps1` (native paths) implement identical fetch-all-repos logic. Repo-wide reference sweep at 9399b92: only `docs/audit/ssh-mesh.md` and `docs/audit/discovery.md` mention them (historical docs). Recommendation: keep `.ps1` (native to CloudPC's OpenSSH shell), delete `.sh`.

### The infra gaps (ENT-05 / ENT-06)

`infra/scripts/tf.sh:56-62`:

```bash
# Post-apply: write .tf-outputs.env
if [[ "$CMD" == "apply" ]]; then
  echo "Writing Terraform outputs to $OUTPUTS_FILE..."
  terraform output -json | jq -r 'to_entries[] | "TF_OUT_\(.key | ascii_upcase)=\(.value.value)"' > "$OUTPUTS_FILE"
  chmod 600 "$OUTPUTS_FILE"
  echo "Outputs written to $OUTPUTS_FILE"
fi
```

No code anywhere reads `.tf-outputs.env` or any `TF_OUT_` key (verified sweep: only `tf.sh` itself, `.gitignore:48`, and the 2026-04-12 archived spec). `OUTPUTS_FILE` is defined at line 10.

`.gitignore:53`: `.terraform.lock.hcl` is ignored, against Terraform's own convention of committing the lockfile for reproducible provider pins (`cloudflare ~> 5.0`). The file exists locally at `infra/environments/prod/.terraform.lock.hcl` (964 B) and is untracked. `.gitignore:48` is `infra/.tf-outputs.env`.

## Commands you will need

| Purpose | Command | Expected on success |
|---------|---------|---------------------|
| Full quality gate | `bash scripts/check.sh` | exit 0, `ALL CHECKS PASSED` (covers `ssh-mesh/scripts/*.sh`, `platform/*.sh`, templates, shellcheck severity=error, `terraform validate` when initialized) |
| Single-file shellcheck | `shellcheck --severity=error <file>` | exit 0, no output |
| Bash syntax | `bash -n <file>` | exit 0, no output |
| Reference sweep | `grep -rn "<pattern>" --exclude-dir=.git --exclude-dir=.worktrees --exclude-dir=.beads .` | as stated per step |

There is no PowerShell runtime on this machine — `.ps1`/`.cmd` edits are verified by grep assertions, not execution. Keep those edits surgical.

## Scope

**In scope** (the only files you may modify or delete):

- `platform/windows/setup.ps1` (edit)
- `platform/windows/install.cmd` (edit)
- `platform/bootstrap.sh` (edit)
- `platform/raycast-scripts/paste-image.sh` (edit: one comment line)
- `ssh-mesh/README.md` (edit)
- `ssh-mesh/scripts/setup-mac.sh`, `setup-homelab.sh`, `deploy-to-homelab.sh` (delete, gated)
- `ssh-mesh/configs/mac.config`, `homelab.config`, `cloudpc.config` (delete, gated)
- `ssh-mesh/scripts/fetch-all-cloudpc.sh` (delete, gated)
- `.claude/workflows/project-mgmt-audit.js` (edit: one prompt string)
- `infra/scripts/tf.sh` (edit, gated)
- `.gitignore` (edit: lines 48 and 53, gated)
- `infra/environments/prod/.terraform.lock.hcl` (newly tracked, gated)
- `plans/README.md` (status row only)

**Out of scope** (do NOT touch, even though they look related):

- `ssh-mesh/scripts/rotate-keys.sh` — live, hardened 2026-07; it is the rotation mechanism the new README points at.
- `ssh-mesh/scripts/remote/**` (cmux-bridge) — live: chezmoi-built, launchd-run.
- `ssh-mesh/scripts/fetch-all-cloudpc.ps1` — the KEPT twin.
- `scripts/mesh-heartbeat.sh` — live, owned elsewhere.
- `home/private_dot_ssh/**` — the live chezmoi lane; read-only reference for this plan.
- `platform/homebrew/**`, `platform/raycast-scripts/*` other than `paste-image.sh` line 29, `scripts/generate-raycast.sh` — healthy, settled.
- `docs/audit/**`, `docs/plans/**` — historical documents; stale mentions of deleted files there are expected and must NOT be "fixed".
- `apps/cc-tmux/**` — plan 014's territory.
- `platform/windows/mac-keyboard.ahk` — live (AHK section still installs it).

## Git workflow

- Work on the current branch (ad-hoc lane). Note: `core.hooksPath` is `.beads/hooks` on this machine — do not edit `.git/hooks/`.
- Stage with targeted paths only (`git add <file> <file> ...`) — never `git add .` / `-A` / a bare directory. Deleted files: `git rm <file>`.
- Single commit, conventional style, e.g.:
  `chore(ssh-mesh,platform): retire superseded playbook lane, de-stale CloudPC flow and mesh README`
- Do NOT push unless the operator instructed it at dispatch.

## Operator gates (resolve BEFORE Step 7)

Five decisions need operator sign-off. If the dispatching operator pre-approved "plan defaults", apply the defaults; otherwise STOP after Step 6, report the table below, and wait.

| Gate | Decision | Default (recommended) |
|------|----------|-----------------------|
| G1 (ENT-01+ENT-02) | Delete the 6-file legacy lane (3 scripts + 3 configs) vs rewrite as thin chezmoi-era pointer stubs | DELETE — chezmoi lane is the live replacement, `rotate-keys.sh` already syncs it, git history keeps the playbooks. This is a >5-file deletion, hence the mandatory gate. |
| G2 (ENT-04) | fetch-all-cloudpc twins: keep one or delete both | Keep `.ps1` (native to CloudPC OpenSSH shell), delete `.sh`. Remote manual use is unobservable from this repo — hence the gate. |
| G3 (ENT-05) | Delete tf.sh's write-only `.tf-outputs.env` block, or keep it and name its consumer | DELETE the block + the `.gitignore:48` line — zero readers repo-wide, speculative plumbing from the 2026-04 migration. |
| G4 (ENT-06) | Start tracking `.terraform.lock.hcl` (policy change) | YES — remove `.gitignore:53`, commit the prod lockfile; Terraform convention for reproducible provider pins. |
| G5 (PLAT-05) | Windows git identity: repoint `leo@leonardoacosta.dev` -> `leo@priceless.dev`, or keep as a deliberate Windows-side split | REPOINT to `leo@priceless.dev` (matches the chezmoi-managed canonical, `platform/bootstrap.sh:169`). A deliberate split is plausible — do NOT change without the gate answer. If unanswered, skip this one edit and note it in the report. |

## Steps

Steps 1-6 are ungated; do them first.

### Step 1: Remove the dead WezTerm section from setup.ps1 (PLAT-01)

In `platform/windows/setup.ps1`:

1. Delete line 10: `    - WezTerm terminal with Mac keyboard bindings`
2. Delete line 44: `        "wez.wezterm"` (keep the `# --- Terminal & Shell ---` comment; PowerToys/gsudo remain).
3. Delete the whole block lines 574-597 — the three header-comment lines (`# ===...`, `# 4. WezTerm Configuration (Windows-adapted)`, `# ===...`) plus the entire `Install-WezTermConfig` function through its closing `}`.
4. In the printed banner: change line 793 (now shifted up) from `  3. Windows apps (WezTerm, Cursor, VS Code, VS Studio, etc.)` to `  3. Windows apps (Cursor, VS Code, VS Studio, etc.)`; delete the `  4. WezTerm config (Mac keyboard bindings)` line; renumber the remaining banner items `5.`-`9.` to `4.`-`8.` (AutoHotKey, Git, PowerShell profile, Nerd Fonts, WSL2 resource limits).
5. Delete the sections-array entry: `    @{ Name = "WezTerm config";         Fn = { Install-WezTermConfig } }`
6. Renumber the internal section-header comments that followed the deleted section: `# 5. AutoHotKey Script...` -> `# 4.`, `# 6. Git Configuration` -> `# 5.`, `# 7. PowerShell Profile...` -> `# 6.`, `# 8. Nerd Fonts` -> `# 7.`, `# 9. WSL2 Configuration` -> `# 8.` (five one-character edits at what were lines 600, 639, 682, 723, 754).

**Verify**: `grep -ic "wezterm" platform/windows/setup.ps1` -> `0`

### Step 2: Fix the stale repo URL + WSL2 bootstrap instructions (PLAT-02)

In `platform/windows/setup.ps1`:

1. Line 29: change `DotfilesRepo   = "git@github.com:leonardoacosta/dotfiles.git"` to `DotfilesRepo   = "git@github.com:leonardoacosta/installfest.git"`.
2. Replace the printed step 4 (the five lines that currently read):

```
  4. Re-enter as your user and clone dotfiles:
     > Arch.exe
     $ mkdir -p ~/dev && cd ~/dev
     $ git clone $($Config.DotfilesRepo) if
     $ cd if && ./install.sh
```

with (this text sits inside an expandable PowerShell here-string; bare `$ ` prompt markers are literal, `$($Config...)` interpolates — mirror the surrounding lines):

```
  4. Re-enter as your user and bootstrap dotfiles (chezmoi two-phase flow):
     > Arch.exe
     $ sudo pacman -S --noconfirm chezmoi
     $ chezmoi init --apply leonardoacosta/installfest --source $($Config.DotfilesPath)
     (Phase 2, interactive, afterwards: $($Config.DotfilesPath)/platform/bootstrap.sh)
```

**Verify**: `grep -c "dotfiles.git\|install.sh" platform/windows/setup.ps1` -> `0`, and `grep -c "installfest.git" platform/windows/setup.ps1` -> `1`

### Step 3: Delete the no-op Set-ExecutionPolicy line (PLAT-04)

In `platform/windows/install.cmd`, delete lines 19-20:

```
:: Set execution policy for this process (non-persistent)
powershell -Command "Set-ExecutionPolicy Bypass -Scope Process -Force"
```

(Line 30's `powershell -ExecutionPolicy Bypass -File "%~dp0setup.ps1"` is the real, sufficient mechanism — keep it.)

**Verify**: `grep -c "Set-ExecutionPolicy" platform/windows/install.cmd` -> `0`

### Step 4: Delete the dead PASTE_IMAGE_DEBUG comment (PLAT-07)

In `platform/raycast-scripts/paste-image.sh`, delete line 29 only:

```
# Set PASTE_IMAGE_DEBUG=1 to also echo to terminal.
```

Do not touch anything else in this file.

**Verify**: `grep -c "PASTE_IMAGE_DEBUG" platform/raycast-scripts/paste-image.sh` -> `0`

### Step 5: Make bootstrap.sh --help real (PLAT-06)

In `platform/bootstrap.sh`, insert immediately AFTER line 47 (`set -uo pipefail`):

```bash

# --- arg parse (only --help is supported) ----------------------------------
if [[ "${1:-}" == "--help" || "${1:-}" == "-h" ]]; then
	cat <<'EOF'
Usage:
  bash platform/bootstrap.sh          # full interactive run (PHASE 2 of cold-start)
  bash platform/bootstrap.sh --help   # this text

PHASE 2 of the documented 2-phase cold-start. Run AFTER Phase 1
(chezmoi init --apply leonardoacosta/installfest). Interactive: pauses at
supervised Apple gates (Remote Login, Xcode/2FA, signing cert), then
Tailscale hostname, gh auth, and the projects.toml clone+install loop.
See the header comment of this script and README.md § Quick Start.
EOF
	exit 0
elif [[ $# -gt 0 ]]; then
	printf 'Unknown argument: %s (only --help is supported)\n' "$1" >&2
	exit 1
fi
```

Match the file's existing indentation style (tabs — see lines 62-70).

**Verify** (all three):
- `bash platform/bootstrap.sh --help` -> prints the usage text, exit 0, and does NOT print any `Step 1/8` / `====` section banner.
- `bash platform/bootstrap.sh --bogus; echo $?` -> `Unknown argument: --bogus ...`, then `1`.
- `shellcheck --severity=error platform/bootstrap.sh` -> exit 0, no output.

### Step 6: Repoint the Windows git identity (PLAT-05 — gate G5)

Only with G5 approval (or pre-approved defaults): in `platform/windows/setup.ps1`, change line (was 651) `git config --global user.email "leo@leonardoacosta.dev"` to `git config --global user.email "leo@priceless.dev"`, and add above the `git config --global user.name` line the comment:
`    # Identity mirrors the chezmoi-managed ~/.gitconfig canonical (see platform/bootstrap.sh Step 2)`

If G5 is unanswered: skip this step entirely and note it in the final report.

**Verify**: `grep -c "leo@leonardoacosta.dev" platform/windows/setup.ps1` -> `0` (or unchanged `1` if skipped)

### Step 7: Delete the superseded ssh-mesh lane (ENT-01 + ENT-02 — gate G1, >5-file deletion)

STOP here if G1 is not approved. With approval:

```bash
git rm ssh-mesh/scripts/setup-mac.sh ssh-mesh/scripts/setup-homelab.sh \
       ssh-mesh/scripts/deploy-to-homelab.sh \
       ssh-mesh/configs/mac.config ssh-mesh/configs/homelab.config \
       ssh-mesh/configs/cloudpc.config
```

Then in `.claude/workflows/project-mgmt-audit.js` (the `ssh-mesh` surface prompt at line 81), replace the phrase
`Compare ssh-mesh/README.md (topology, IPs, hostnames, key policy) and ssh-mesh/configs/*.config against live:`
with
`Compare ssh-mesh/README.md (topology, IPs, hostnames, key policy) and home/private_dot_ssh/config.tmpl against live:`

**Verify**:
- `ls ssh-mesh/configs/ 2>&1` -> `No such file or directory` (or empty); `ls ssh-mesh/scripts/` -> shows only `remote`, `fetch-all-cloudpc.ps1`, `rotate-keys.sh` (plus `fetch-all-cloudpc.sh` until Step 8).
- `grep -rn "setup-mac.sh\|setup-homelab.sh\|deploy-to-homelab.sh\|configs/mac.config\|configs/homelab.config\|configs/cloudpc.config" --exclude-dir=.git --exclude-dir=.worktrees --exclude-dir=.beads --exclude-dir=docs .` -> matches only in `plans/` (this file). `docs/` is excluded deliberately — historical audit docs keep their mentions.

### Step 8: Delete the fetch-all-cloudpc dead twin (ENT-04 — gate G2)

With G2 approval (default): `git rm ssh-mesh/scripts/fetch-all-cloudpc.sh` — keep `fetch-all-cloudpc.ps1` untouched. If the operator chose "delete both", also `git rm ssh-mesh/scripts/fetch-all-cloudpc.ps1`.

**Verify**: `ls ssh-mesh/scripts/` -> `remote`, `fetch-all-cloudpc.ps1`, `rotate-keys.sh` only.

### Step 9: Rewrite ssh-mesh/README.md to the live lane (ENT-03 + PLAT-03)

Edit `ssh-mesh/README.md` (do this after Step 7 so the doc matches reality):

1. **Line 48** (Mac "Special:" row): replace with
   `- **Special:** Tailscale-only routing — ~/.ssh/config is chezmoi-managed (home/private_dot_ssh/config.tmpl); the old LAN probe was removed`
2. **Shared SSH Key section (lines 67-73)**: delete the `Fingerprint:` and `Public Key:` bullet lines and replace the section body with a pointer — never publish drift-prone key material here:

```markdown
## Shared SSH Key

All machines use the same ED25519 keypair. The current public key is
chezmoi-managed — the single source of truth is
`home/private_dot_ssh/private_authorized_keys` in this repo (the key line's
comment tag, e.g. `leo-mesh-YYYYMMDD`, dates the last rotation). Do not copy
the key or fingerprint into docs: `ssh-mesh/scripts/rotate-keys.sh`
regenerates the pair and rewrites that file, and copies drift.
```

3. **Quick Setup section (lines 104-128)**: replace the whole section with:

```markdown
## Quick Setup

SSH config and authorized_keys are **chezmoi-managed**
(`home/private_dot_ssh/config.tmpl` + `private_authorized_keys`). The old
`ssh-mesh/scripts/setup-*.sh` playbooks were deleted — they predate chezmoi
ownership and clobbered managed files (git history has them if needed).

### Mac / Homelab

```bash
chezmoi apply   # lays down ~/.ssh/config and ~/.ssh/authorized_keys
```

Private key material is never in git (`ssh-mesh/keys/` is gitignored). To
generate/redeploy/rotate the shared keypair across all machines, run
`ssh-mesh/scripts/rotate-keys.sh`.

### CloudPC

```powershell
# Run as Administrator (from repo root)
powershell -ExecutionPolicy Bypass -File platform\windows\setup.ps1
```
```

   Keep the existing `> **Note:** The CloudPC setup script was consolidated...` line but change its path text `windows/setup.ps1` -> `platform/windows/setup.ps1`.
4. **Delete the "Mac SSH Config Explained" section (lines 130-145)** — it documents the removed LAN-probe config. Also delete the "Mac LAN Probe Hanging" troubleshooting entry (lines 218-220, header + 1 body line).
5. Leave everything else (topology diagram, machine tables, File Locations, Windows quirks, Tailscale notes, Security Notes) as-is.

**Verify**:
- `grep -c "AAAAC3Nza\|SHA256:" ssh-mesh/README.md` -> `0`
- `grep -c "setup-mac.sh\|setup-homelab.sh" ssh-mesh/README.md` -> `0`
- `grep -c "platform\\\\windows\\\\setup.ps1" ssh-mesh/README.md` -> `1` (and `grep -n "File windows" ssh-mesh/README.md` -> no matches)
- `grep -c "dev/tcp\|LAN Probe" ssh-mesh/README.md` -> `0`

### Step 10: Delete the write-only .tf-outputs.env block (ENT-05 — gate G3)

With G3 approval (default): in `infra/scripts/tf.sh`, delete lines 56-62 (the `# Post-apply: write .tf-outputs.env` comment through the closing `fi`) AND line 10 (`OUTPUTS_FILE="$INFRA_DIR/.tf-outputs.env"`). Then delete `.gitignore` line 48 (`infra/.tf-outputs.env`). A stale local `infra/.tf-outputs.env` may exist on machines — harmless; do not add cleanup logic.

**Verify**: `grep -rn "tf-outputs\|TF_OUT_\|OUTPUTS_FILE" infra/ .gitignore` -> no matches; `bash -n infra/scripts/tf.sh` -> exit 0.

### Step 11: Track the Terraform lockfile (ENT-06 — gate G4)

With G4 approval (default): delete `.gitignore` line 53 (`.terraform.lock.hcl`), then `git add infra/environments/prod/.terraform.lock.hcl`.

**Verify**: `git status --porcelain infra/environments/prod/.terraform.lock.hcl` -> `A  infra/environments/prod/.terraform.lock.hcl`; `git check-ignore infra/environments/prod/.terraform.lock.hcl; echo $?` -> `1` (not ignored).

### Step 12: Full gate + commit

1. `bash scripts/check.sh` -> exit 0, `ALL CHECKS PASSED` (the deleted `ssh-mesh/scripts/*.sh` simply drop out of `SH_FILES`; `terraform validate` must still pass since `infra/environments/prod/.terraform` is initialized on this machine).
2. `git status --porcelain` -> ONLY in-scope files listed (see Scope). Anything else -> STOP.
3. Stage targeted paths (including every `git rm`'d path, which `git rm` already staged), write the commit message to a temp file, `git commit -F <file>`. Push only if instructed.

## Test plan

This repo's test surface for these files IS `scripts/check.sh` (there is no unit-test suite for shell/PowerShell; `cc-tmux self-test` is plan 014's gate and not required here). Mimic the verification style of `scripts/check.sh` itself — syntax + shellcheck + render:

- `bash scripts/check.sh` -> exit 0 (run before starting for a baseline, and after Step 12).
- Behavior checks unique to this plan (from Step 5): `bash platform/bootstrap.sh --help` exits 0 printing usage without executing any bootstrap section; `bash platform/bootstrap.sh --bogus` exits 1.
- Grep assertions per step above are the regression checks for the non-executable (`.ps1`/`.cmd`/`.md`) surfaces.

## Done criteria

Machine-checkable. ALL must hold:

- [ ] `bash scripts/check.sh` exits 0 (`ALL CHECKS PASSED`)
- [ ] `grep -ric "wezterm" platform/windows/ | grep -v ':0'` -> no output
- [ ] `grep -rc "dotfiles.git" platform/windows/setup.ps1` -> `0`
- [ ] `grep -c "AAAAC3Nza\|SHA256:" ssh-mesh/README.md` -> `0`
- [ ] `grep -c "platform\\\\windows\\\\setup.ps1" ssh-mesh/README.md` -> `1`
- [ ] `test -f ssh-mesh/scripts/setup-mac.sh; echo $?` -> `1` (G1 executed; likewise the other 5 lane files)
- [ ] `bash platform/bootstrap.sh --help; echo $?` -> usage text then `0`
- [ ] `grep -rn "tf-outputs" infra/ .gitignore` -> no matches (G3)
- [ ] `git ls-files infra/environments/prod/.terraform.lock.hcl` -> prints the path (G4)
- [ ] `git status --porcelain` shows only in-scope files
- [ ] `plans/README.md` status row updated

(If any gate G1-G5 was answered differently from the default, the corresponding criterion follows the operator's answer instead — record which in the report.)

## STOP conditions

Stop and report back (do not improvise) if:

- The drift check shows any in-scope file changed since `9399b92`, and a "Current state" excerpt no longer matches the live file.
- Gates G1-G5 are unresolved and the operator did not pre-approve defaults (stop after Step 6).
- The Step 7 reference sweep finds a LIVE caller of the lane files outside `ssh-mesh/README.md`, `docs/`, and `plans/` — the zero-caller premise is false.
- `bash scripts/check.sh` fails twice after a reasonable fix attempt (note: pre-existing exclusions `scripts/mux-remote.sh` SC1071 and `scripts/gk-github-auth.sh` SC2148 are a documented burn-down list — a failure THERE is not caused by this plan, but any new failure is).
- A fix appears to require touching `rotate-keys.sh`, `ssh-mesh/scripts/remote/**`, `home/private_dot_ssh/**`, or any other out-of-scope file.
- `git status --porcelain` shows modifications outside the in-scope list before commit (another session may share this tree — do not commit others' work).

## Maintenance notes

- **ssh-mesh/README.md now carries zero key material by design.** Reviewers should reject any future PR that re-inlines a pubkey/fingerprint there — the pointer to `home/private_dot_ssh/private_authorized_keys` is the contract (rotation rewrites that file; docs copies drift silently).
- **`.claude/workflows/project-mgmt-audit.js`'s ssh-mesh surface** now audits `home/private_dot_ssh/config.tmpl` against live state. If the mesh topology changes, that prompt and the README table are the two doc surfaces to update.
- **Terraform lockfile**: after G4, a provider upgrade flow is `pnpm tf init -upgrade` -> commit the changed `.terraform.lock.hcl`. If a second environment is ever added under `infra/environments/`, commit its lockfile too.
- **`setup.ps1` remains untestable from this machine** (no PowerShell). The Windows flow's next real verification is a live CloudPC run — flagged as a known limitation, not deferred work.
- Deliberately deferred: restoring any Windows terminal config to replace WezTerm's (Ghostty has no Windows build — a replacement choice is a product decision, not drift cleanup); consolidating `docs/audit/*` mentions of deleted files (historical documents stay as written).
- Reviewer focus: Step 9's README diff (largest prose change — check the retained sections still read coherently) and Step 5's insertion point (must be after `set -uo pipefail`, before the repo-root resolution).
