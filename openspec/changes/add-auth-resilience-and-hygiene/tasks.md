<!-- beads:epic:if-vit -->
<!-- beads:feature:if-49r -->
# Implementation Tasks

## Auth Batch

- [ ] [1.1] [P-1] Create `scripts/az-reauth-nudge.sh` — reads MSAL cache metadata (timestamps only) for `~/.azure-bbadmin` + `~/.azure-o365`, computes time-since-interactive-auth, notifies via `nx_notify` once per window when within lead margin of the CA sign-in-frequency wall. Window + margin configurable via env; default window from design.md forensics (fallback 12h), margin 30m. Dedup state in `~/.local/state/az-reauth-nudge/`. Exit 0 always. [owner:general-purpose] [beads:if-oc0]
- [ ] [1.2] [P-1] Create `home/dot_config/systemd/user/az-reauth-nudge.{service,timer}` (timer: every 15m, Persistent=true) and register both in `run_onchange_after_install-user-schedulers.sh.tmpl` (Linux block; embed sha256sums per house pattern) [owner:general-purpose] [beads:if-r4t]
- [ ] [1.3] [P-1] Create `home/dot_local/bin/executable_mx-token` — broker socket token client per Req-2, mirroring `git-credential-mxbroker.sh` hardening (ownership check, --max-time 5, silent-fail exit 0, no token logging) [owner:general-purpose] [beads:if-yhr]
- [ ] [1.4] [P-2] Extend `home/run_after_doctor.sh.tmpl` — warn when MSAL cache age is already past the CA window (login will fail on next az call) alongside the existing broker-socket check [owner:general-purpose] [beads:if-ean]

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
