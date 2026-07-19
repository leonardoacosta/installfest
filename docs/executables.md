# Executables Index

Every executable script under `scripts/`, `platform/`, and `home/dot_local/bin/`
(51 files total), what it does, and whether it currently supports `--help`/`-h`.
`platform/raycast-scripts/**` (generated Raycast launchers) is excluded — see
§ Raycast Scripts below for why.

Generated 2026-07-19 for bead `if-7cce.3`. Backfilled `--help` on 20 scripts
in the same pass (see § Backfill Summary); the remainder are documented
`N/A` with a reason (sourced library, git-hook/automation entrypoint,
passthrough wrapper, or already-covered by argparse).

## `scripts/`

| Path | Purpose | `--help`? |
| --- | --- | --- |
| `scripts/audit-projects.sh` | One-command drift audit for the project-management layer: cross-checks `home/projects.toml` against the filesystem, generated Raycast launchers, workspace symlinks, the ssh mesh, and systemd schedulers. Detector only. | Yes (added) |
| `scripts/az-reauth-nudge.sh` | Proactive re-auth reminder that fires (once per identity per 60-day Conditional Access window) from ~day 55, naming `az-reauth <identity>` as the fix. Exits 0 always. | Yes (added) |
| `scripts/az-reauth.sh` | One-tap re-auth orchestrator: drives `az login --use-device-code` per identity, surfaces device code + TOTP via nx_notify, clipboards to the Mac, opens the login URL, verifies success. | Yes (added) |
| `scripts/brew-install.sh` | Sourced-only lib defining `run_brew_bundle()` (installs the Brewfile app/CLI bundle). Not a standalone entrypoint. | N/A — sourced library |
| `scripts/check.sh` | One-command verification baseline: zsh/bash syntax, chezmoi template render, shellcheck, terraform validate. | Yes (added) |
| `scripts/cmux-bridge.py` | Talks to cmux via its forwarded Unix socket (browser-open, set-status, clear-status, notify, raw) since the cmux CLI isn't installed on the homelab. | Yes (added) |
| `scripts/cmux-evolve-refresh.sh` | Phase 1 data producer for `/cmux:evolve`: polls cmux's GitHub releases Atom feed, diffs against the persisted cursor, prints one JSON object. Always exits 0 (data-producer contract). | Yes (added) |
| `scripts/cmux-git-tree.py` | Renders `git log --graph --all` as an HTML commit graph and opens it in cmux's embedded browser panel (local or SSH-backed). | Yes (argparse, already worked) |
| `scripts/cmux-status-writer.py` | Periodic writer for the openspec/beads/usage fields smuggled into a cmux workspace's `description` (CC1 encoding). Darwin-only in practice. | Yes (argparse, already worked) |
| `scripts/cmux-workspaces.sh` (`mux`) | Launches dev workspaces in cmux over SSH, driven by `home/projects.toml`. | Yes (pre-existing) |
| `scripts/dbpro.sh` | Manual, optional installer for the DB Pro database client (macOS only). | Yes (added) |
| `scripts/docs-hygiene-daily.py` | Nightly `/improve:docs` sweep across active fleet repos (personal/priceless categories only); pushes a review branch per repo with drift, never lands on trunk. | Yes (added) |
| `scripts/file-server.py` | File server for cmux's embedded browser — serves local files over HTTP, renders markdown as styled HTML. | Yes (added) |
| `scripts/generate-raycast.sh` | Generates all of `platform/raycast-scripts/**` from `home/projects.toml`. The single source of truth for that generated tree — edit this, not the outputs. | Yes (added) |
| `scripts/git-credential-mxbroker.sh` | Git credential helper for `dev.azure.com`: fetches a fresh ADO token from homelab's mx-broker over a forwarded socket. Fails silently (exit 0, no output) on any error so git falls through to the next credential helper. | N/A — invoked via the git-credential `get`/`store`/`erase` protocol, not a human `-h` flag |
| `scripts/gk-github-auth.sh` | Shared `gk_attach_github()` definition for attaching the GitKraken CLI's GitHub provider token. Explicitly "SOURCE this; do not execute it" per its own header. | N/A — sourced library only |
| `scripts/homelab/harden.sh` | Arch/omarchy post-install hardening (idempotent): TPM2/LUKS auto-unlock, Snapper snapshots, snap-pac, Limine timeout, Tailscale, DB bootstrap, observability, swap tiers, crash recovery. | Yes (added) |
| `scripts/hooks/beads-jsonl-merge-driver.sh` | Git merge driver for `.beads/issues.jsonl` — passthrough to `bd dolt pull` + re-export instead of a real content merge (`bd merge` doesn't exist). | N/A — invoked by git's merge machinery, not directly |
| `scripts/hooks/post-commit` | Local deploy hook: imports beads changes, then `chezmoi apply`. Also symlinked as `post-merge`. | N/A — git hook |
| `scripts/hooks/pre-commit` | Pre-commit gate: secret scan, `check.sh` baseline, Raycast regen, beads flush. | N/A — git hook |
| `scripts/hooks/pre-push` | Bidirectional remote deploy: after push, SSHes to the other machine and pulls + applies. | N/A — git hook |
| `scripts/hooks/remote-apply.sh` | Runs on a deploy target after `git reset --hard origin/main`: `chezmoi apply`, diffs `raycast-scripts/`, notifies on Mac if changed. | N/A — invoked by `pre-push`/`post-commit`, not directly by a user |
| `scripts/hooks/zsa-firmware-build.sh` | Homelab-only: polls Oryx for a new keyboard layout, replicates the (disabled) GitHub Action locally — fetch, merge, build, ship `.bin` to the Mac. | N/A — cron/automation entrypoint, not a manual `-h` invocation |
| `scripts/hooks/zsa-firmware-check.sh` | Mac-only: notices a freshly staged ZSA Voyager firmware `.bin` in `~/Downloads` and drives the flash step via nx notifications. | N/A — automation entrypoint (invoked by `post-commit`/`remote-apply.sh`/`zsa-firmware-build.sh`, or a bare optional path arg) |
| `scripts/ideopen.sh` (`vopen`/`zopen`) | Opens Linux files/workspaces in a Mac IDE (VS Code / Zed) over Tailscale Remote-SSH. | Yes (pre-existing) |
| `scripts/install-arch.sh` | Sourced-only lib: Arch Linux package installation, invoked from `home/run_once_install-packages.sh.tmpl`. | N/A — sourced library |
| `scripts/lib/cmux_status_encoding.py` | Shared CC1 smuggled-field encode/decode helpers, imported by `cmux-status-writer.py`. | N/A — importable module, not a CLI |
| `scripts/lib/open-core.sh` | Shared engine for the VIEW-family `*open` commands (portal-aware HTTP file serving). | N/A — sourced library |
| `scripts/lib/registry.sh` | Shared resolver for `home/projects.toml` consumers (path + tomllib-capable python3). | N/A — sourced library |
| `scripts/mac-open.sh` | Single front door for "show this on my Mac" from the headless homelab — routes URLs/files to the Mac's browser, a cmux pane, or an iPhone push over Tailscale. | Yes (added) |
| `scripts/mesh-heartbeat.sh` | Probes Tailscale reachability + mx-broker health + SOCKS tunnel liveness; emits a JSON record per run and notifies only on state transitions. | Yes (added) |
| `scripts/mic-priority.sh` | Sets the active input device to the highest-priority available microphone via SwitchAudioSource (macOS). | Yes (added) |
| `scripts/mux-remote.sh` | Remote-invokable wrapper for `cmux-workspaces.sh`, callable via Apple Shortcuts/NFC/SSH. | Yes (added) |
| `scripts/op-ssh-provision.sh` | Sourced-only lib: materializes the SSH mesh keypair from 1Password, invoked by the run-once installer. | N/A — sourced library |
| `scripts/osx-defaults.sh` | Sourced-only lib: applies macOS system defaults (key repeat, trackpad gestures, etc.). | N/A — sourced library |
| `scripts/prerequisites.sh` | Sourced-only lib: headless Xcode Command Line Tools install. | N/A — sourced library |
| `scripts/ropen-server.py` | Multi-mount HTTP file server with client-side markdown rendering, spawned by `ropen` (background or `--serve` foreground). | N/A — internal server taking positional port/mounts-path args, not spawned with a `-h` flag |
| `scripts/ropen.sh` | Remote file/dir opener over Tailscale with multi-project mounts and Atlas-portal awareness. | Yes (pre-existing) |
| `scripts/setup-az-wrapper.sh` | First-time setup for the smart `az` CLI wrapper: identity dirs, dependency checks, interactive device-code logins for both BBAdmin and O365. | Yes (added) |
| `scripts/terminal.sh` | Sourced-only lib: adds `~/.hushlogin` to suppress the "last login" message. | N/A — sourced library |
| `scripts/utils.sh` | Sourced-only lib: shared `info`/`success`/`warning`/`error` color-output helpers used by most installer scripts. | N/A — sourced library |
| `scripts/validate-proxy.sh` | Periodic validation + remediation of the CloudPC proxy stack (SOCKS tunnel, ProxyBridge), notifies via nexus-agent. | Yes (added) |
| `scripts/viewopen.sh` (`gopen`/`sopen`/`mopen`/`iopen`) | Basename-dispatched wrapper for the four VIEW-family commands over `scripts/lib/open-core.sh`. | Yes (pre-existing) |
| `scripts/view.sh` | Front door for "render this file optimally" in a terminal — picks a renderer by file type, shows it in a tmux split. | Yes (pre-existing) |
| `scripts/youtube-transcript.sh` | Manual, optional installer for the `youtube_transcript` CLI (fetches transcripts without an API key). | Yes (added) |

## `platform/`

| Path | Purpose | `--help`? |
| --- | --- | --- |
| `platform/bootstrap.sh` | PHASE 2 of the documented 2-phase cold-start: supervised Apple gates (Remote Login, Xcode/2FA, signing cert), Tailscale hostname, Xcode first-launch, `gh auth`, and the `projects.toml` clone+install loop. | Yes (pre-existing) |

### Raycast Scripts (excluded from the per-file table)

`platform/raycast-scripts/**` (~100 files: `{code}.sh`, `local/{code}.sh`,
`cloudpc/{code}.sh`, plus the `open-project.sh` dropdown pickers) are **entirely
generated** by `scripts/generate-raycast.sh` from `home/projects.toml` — see
that script's own header and the `project_raycast_scripts_generator` memory
note. Each one is a fixed Raycast script-command template (`@raycast.*`
metadata block + a one-line `cursor --folder-uri ...`/`cursor ~/dev/...` body),
launched by double-click from Raycast's UI, not from a terminal with flags —
`--help` has no meaning in that invocation model. Edit the generator, not the
generated files; re-run it to regenerate all of them. This is why the "~48
executables" scoping for this bead did not intend for these ~100 files to be
indexed or flag-backfilled individually.

## `home/dot_local/bin/`

Executables only (`executable_*`); the `symlink_*.tmpl` entries are chezmoi-deployed
aliases pointing at scripts already covered above or at `packages/workspace/bin/`
(outside this bead's three named directories) — listed for completeness, not
separately indexed.

| Path | Purpose | `--help`? |
| --- | --- | --- |
| `home/dot_local/bin/executable_az` (`az`) | Smart Azure CLI wrapper: routes BB traffic through the CloudPC SOCKS5 tunnel, auto-selects identity (BBAdmin/O365/Personal). | N/A by design — a passthrough wrapper around the real `az` binary; intercepting `--help` here would shadow Azure CLI's own `az --help` output |
| `home/dot_local/bin/executable_copen` (`copen`) | Opens (or focuses) a registry project in Cursor on whichever machine it's run from; clones from homelab if missing. | Yes (pre-existing) |
| `home/dot_local/bin/executable_editor-open` (`code`/`cursor`/`zed` symlinks) | Shared remote-editor wrapper: delegates to the real binary locally, or SSHes back to the client to trigger its Remote-SSH open. | N/A by design — passthrough wrapper around the real editor CLIs; must not shadow their own `--help` |
| `home/dot_local/bin/executable_mx-token` (`mx-token`) | Thin client for the homelab mx-broker token socket — prints an access token for Graph/ADO REST calls, or nothing on any failure. | Yes (added) |
| `home/dot_local/bin/executable_weekly-cleanup` (`weekly-cleanup`) | Weekly cache cleanup (LaunchAgent/systemd-timer triggered, Sundays 03:00): OS/package-manager caches plus age-gated `.turbo`/`.next`/`*.bun-build` build output. | Yes (added) |

### Symlink aliases (not separately indexed)

| Symlink | Target |
| --- | --- |
| `az-reauth` | `scripts/az-reauth.sh` |
| `code`, `cursor`, `zed` | `executable_editor-open` |
| `gopen`, `iopen`, `mopen` | `scripts/viewopen.sh` |
| `mac-open` | `scripts/mac-open.sh` |
| `ropen` | `scripts/ropen.sh` |
| `view` | `scripts/view.sh` |
| `vopen`, `zopen` | `scripts/ideopen.sh` |
| `ws-claude`, `ws-doctor`, `wsenv`, `ws-ready`, `ws-scan` | `packages/workspace/bin/*` (separate directory, out of scope for this index) |

## Backfill Summary

20 scripts got a `--help`/`-h` flag added, following the two conventions
already in use in this repo:

- **Header-comment extraction** (`ropen.sh`/`viewopen.sh`/`copen` style): `sed -n
  'X,Yp' "$0" | sed 's/^# \{0,1\}//'` to print an existing usage comment block
  verbatim — used where the header already carried enough prose (`mac-open.sh`,
  `generate-raycast.sh`, `homelab/harden.sh`, `mux-remote.sh`).
- **Inline heredoc** (`platform/bootstrap.sh` style): `if [[ "${1:-}" == "--help"
  || "${1:-}" == "-h" ]]; then cat <<'EOF' ... EOF; exit 0; fi` — used where a
  fresh, tighter summary was clearer than the header comment (the rest).
- Python scripts: an explicit `-h`/`--help` check at the top of `main()` that
  prints `__doc__` and exits 0 (matching the docstring-as-usage convention
  `cmux-bridge.py`/`file-server.py` already partially had).

Backfilled (20): `scripts/audit-projects.sh`, `scripts/az-reauth-nudge.sh`,
`scripts/az-reauth.sh`, `scripts/check.sh`, `scripts/cmux-bridge.py`,
`scripts/cmux-evolve-refresh.sh`, `scripts/dbpro.sh`,
`scripts/docs-hygiene-daily.py`, `scripts/file-server.py`,
`scripts/generate-raycast.sh`, `scripts/homelab/harden.sh`,
`scripts/mac-open.sh`, `scripts/mesh-heartbeat.sh`, `scripts/mic-priority.sh`,
`scripts/mux-remote.sh`, `scripts/setup-az-wrapper.sh`,
`scripts/validate-proxy.sh`, `scripts/youtube-transcript.sh`,
`home/dot_local/bin/executable_mx-token`,
`home/dot_local/bin/executable_weekly-cleanup`.

Already had `--help` (7): `platform/bootstrap.sh`, `scripts/cmux-workspaces.sh`,
`scripts/ideopen.sh`, `scripts/ropen.sh`, `scripts/viewopen.sh`,
`scripts/view.sh`, `home/dot_local/bin/executable_copen`.

Already covered via Python `argparse` (2): `scripts/cmux-git-tree.py`,
`scripts/cmux-status-writer.py`.

Deliberately left without `--help` (20), each documented with a reason in the
tables above: 12 sourced-only libraries (`brew-install.sh`, `install-arch.sh`,
`osx-defaults.sh`, `prerequisites.sh`, `terminal.sh`, `utils.sh`,
`gk-github-auth.sh`, `op-ssh-provision.sh`, `scripts/lib/*.sh`,
`scripts/lib/cmux_status_encoding.py`), 7 git-hook/automation entrypoints
(`scripts/hooks/*`), and 1 internal server (`ropen-server.py`) — plus 2
passthrough wrappers (`executable_az`, `executable_editor-open`) that
intentionally do not intercept `--help` so as not to shadow the wrapped tool's
own help output, and `git-credential-mxbroker.sh` which speaks the git
credential protocol, not a `-h` flag.
