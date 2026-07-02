<!-- beads:epic:if-vit -->
<!-- beads:feature:if-49r -->
# Implementation Tasks

## Auth Batch

- [ ] [1.1] [P-1] Create `scripts/az-reauth-nudge.sh` — reads MSAL cache metadata (timestamps only) for `~/.azure-bbadmin` + `~/.azure-o365`, computes per-identity token age (independent clocks), notifies via `nx_notify` once per identity per window when within lead margin of the 60d CA wall. Window 60d + margin 5d, env-configurable (design.md D1). Dedup state in `~/.local/state/az-reauth-nudge/`. Exit 0 always. [owner:general-purpose] [beads:if-oc0]
- [ ] [1.2] [P-1] Create `home/dot_config/systemd/user/az-reauth-nudge.{service,timer}` (timer: OnCalendar=daily, Persistent=true — design.md D1) and register both in `run_onchange_after_install-user-schedulers.sh.tmpl` (Linux block; embed sha256sums per house pattern) [owner:general-purpose] [beads:if-r4t]
- [ ] [1.3] [P-1] Create `home/dot_local/bin/executable_mx-token` — broker socket token client per Req-2, mirroring `git-credential-mxbroker.sh` hardening (ownership check, --max-time 5, silent-fail exit 0, no token logging) [owner:general-purpose] [beads:if-yhr]
- [ ] [1.4] [P-2] Extend `home/run_after_doctor.sh.tmpl` — warn when MSAL cache age is already past the CA window (login will fail on next az call) alongside the existing broker-socket check [owner:general-purpose] [beads:if-ean]
- [ ] [1.5] [P-1] Fail-fast on AADSTS70043 in `home/dot_local/bin/executable_az` — detect 70043 in stderr of a failing call, notify once with the re-login command, set per-identity marker in `~/.local/state/az-reauth-nudge/`; while marker exists, short-circuit that identity's calls with a one-line error; clear marker on successful `login` invocation (design.md D2) [owner:general-purpose] [beads:if-xga]
- [ ] [1.6] [P-1] Create `scripts/az-reauth.sh` — per-identity device-code login orchestrator (Req-6): run `az login --use-device-code` with correct `AZURE_CONFIG_DIR`, parse code+URL from stderr, `ssh mac pbcopy` the code, open device-login page on Mac via `mac-open`/Edge (verify `?otc=` prefill form; clipboard fallback), wait for poll completion, token-probe verify, clear Req-5 marker, re-check broker `/health`, notify per identity [owner:general-purpose] [beads:if-6yj]
- [ ] [1.7] [P-2] Wire the Req-1 nudge message to name `az-reauth` as the action (exact command per identity); deploy `az-reauth` via `home/dot_local/bin/` so it's on PATH on homelab [owner:general-purpose] [beads:if-806]
- [x] [1.8] [P-2] [user] TOTP feasibility check — DONE: software-OATH/TOTP IS allowed (confirmed at security-info). 4-lens eval chose D5-A over D5-B unanimously (design.md) [owner:leo] [beads:if-y9m]
- [ ] [1.9] [P-1] [user] Enroll a software-OATH token at mysignins.microsoft.com/security-info per identity (BBAdmin, O365); store each base32 seed in 1Password as a distinct item (`op` item, e.g. `az-totp-bbadmin` / `az-totp-o365`). Gating physical step — az-reauth reads the seed via `op` at runtime, never plaintext in repo [owner:leo] [beads:if-zkk]
- [ ] [1.10] [P-1] Extend `scripts/az-reauth.sh` (D5-A): when a seed item exists in 1Password for the identity, run `oathtool --totp -b "$(op read ...)"` and place the code on the Mac clipboard next to the device code, so re-auth is paste-not-phone. Degrade cleanly (skip TOTP step, fall back to Req-6 phone flow) when no seed item is present [owner:general-purpose] [beads:if-qd5]

## Heartbeat Batch

- [ ] [2.1] [P-1] Create `scripts/mesh-heartbeat.sh` — probe tailscale peers (mac, cloudpc), broker `/health`, SOCKS 127.0.0.1:1080 TCP; emit one JSON record to `~/.claude/scripts/bin/metrics-outbox` if executable, else `~/.local/state/mesh-heartbeat.jsonl`; `nx_notify` on state transitions only (state file per probe) [owner:general-purpose] [beads:if-ao8]
- [ ] [2.2] [P-1] Create `home/dot_config/systemd/user/mesh-heartbeat.{service,timer}` (every 5m, Persistent=true) and register in `run_onchange_after_install-user-schedulers.sh.tmpl` [owner:general-purpose] [beads:if-50h]

## Hygiene Batch

- [ ] [3.1] [P-1] Delete `home/dot_config/systemd/user/nexus-dashboard.service`; remove its entry from `docs/homelab-recovery.md`; `chezmoi apply` removes the deployed unit (verify `systemctl --user status nexus-dashboard` reports not-found after) [owner:general-purpose] [beads:if-e72]
- [ ] [3.2] [P-2] Determine live cmux bridge: grep callers of `cmux-bridge.py` vs the Rust `ssh-mesh/scripts/remote/cmux-bridge/` binary across `scripts/cmux-workspaces.sh`, `scripts/mux-remote.sh`, deployed units; keep the invoked one, delete the other, update the losing path's callers/docs [owner:general-purpose] [beads:if-9sp]
- [ ] [3.3] [P-2] Collapse bootstrap entry points: grep for external callers of `scripts/brew-install.sh` + `scripts/prerequisites.sh`; convert to thin delegates of `run_once_install-packages.sh.tmpl` logic or delete if uncalled [owner:general-purpose] [beads:if-p00]
- [ ] [3.4] [P-3] Add `ensure_mx_broker_dir()` to `scripts/utils.sh` (mkdir -p + chmod 700); replace the four duplicate creation sites (`run_once_install-packages`, `run_onchange_after_configure-git-azure`, `run_after_doctor`, credential helper docs) [owner:general-purpose] [beads:if-d60]

## Verification Batch

- [ ] [4.1] [P-1] `chezmoi apply` deploys both new timers; `systemctl --user list-timers` shows az-reauth-nudge + mesh-heartbeat scheduled [owner:general-purpose] [beads:if-b44]
- [ ] [4.2] [P-1] Nudge smoke: set window env to 1m against a fresh cache timestamp, run service once, confirm exactly one nx_notify fires and a second run is silent (dedup) [owner:general-purpose] [beads:if-1jv]
- [ ] [4.3] [P-1] `mx-token graph o365` returns a non-empty token through the live broker socket; with the tunnel down it prints nothing and exits 0 [owner:general-purpose] [beads:if-1id]
- [ ] [4.4] [P-1] Heartbeat smoke: run once with all probes up (record emitted, no notify); stop SOCKS tunnel, run again (transition notify fires); restart, run again (recovery notify) [owner:general-purpose] [beads:if-tl0]
- [ ] [4.5] [P-2] Post-hygiene: `systemctl --user status nexus-dashboard` not-found; only one cmux bridge remains in-tree; grep shows zero remaining inline `~/.mx/broker` mkdir sites [owner:general-purpose] [beads:if-btg]
- [ ] [4.6] [P-1] Re-auth orchestration E2E — SANDBOXED, no 60d wait (design.md D6): run `az-reauth` on demand against the `--as-personal`/civalent identity (no CA, no proxy — unlimited free runs) OR force a real identity's precondition with `az logout` on its `AZURE_CONFIG_DIR`. Assert: code -> Mac clipboard, Edge opens device-login, post-MFA token verify, fail-fast marker cleared, success notify. Run ONCE against real o365 at first natural expiry only to confirm broker `/health` shows ado/o365 SERVING [owner:general-purpose] [beads:if-gge]
- [ ] [4.7] [P-1] Fail-fast unit test (no expiry): feed the canned AADSTS70043 stderr sample (design.md forensics) to `executable_az`, assert exactly one notify fires, the per-identity marker is set, and a subsequent call for that identity short-circuits; clearing the marker restores normal calls (design.md D2) [owner:general-purpose] [beads:if-6xg]
