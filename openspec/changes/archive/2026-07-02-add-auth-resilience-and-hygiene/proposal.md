---
capability: workspace-resilience
---
# Proposal: Auth resilience, mesh/broker heartbeat, and workspace hygiene

## Change ID
`add-auth-resilience-and-hygiene`

## Summary
Reduce the dominant dev-loop friction (Azure auth churn: ~2,000 AADSTS errors + ~1,150 `az login`
prompts across 30 days of ws sessions) with a proactive re-auth nudge and broker-first token
consumption; add continuous heartbeat monitoring for the two zero-observability dependencies
everything else rides on (SSH mesh, mx-broker token path); and delete/collapse the duplication
the 2026-07-02 workspace audit flagged (stale nexus-dashboard unit, triple cmux bridge,
quadruplicated bootstrap installers, 4x `~/.mx/broker` dir creation).

## Context
- touches: `home/run_onchange_after_install-user-schedulers.sh.tmpl`, `home/run_after_doctor.sh.tmpl`, `home/dot_config/systemd/user/nexus-dashboard.service`, `home/dot_config/systemd/user/mesh-heartbeat.service`, `home/dot_config/systemd/user/mesh-heartbeat.timer`, `home/dot_config/systemd/user/az-reauth-nudge.service`, `home/dot_config/systemd/user/az-reauth-nudge.timer`, `home/dot_local/bin/executable_mx-token`, `scripts/mesh-heartbeat.sh`, `scripts/az-reauth-nudge.sh`, `scripts/cmux-bridge.py`, `ssh-mesh/scripts/remote/cmux-bridge/`, `scripts/brew-install.sh`, `scripts/prerequisites.sh`, `scripts/utils.sh`, `home/run_once_install-packages.sh.tmpl`, `home/run_onchange_after_configure-git-azure.sh.tmpl`, `docs/homelab-recovery.md`
- Extends: mx-broker client pattern (`scripts/git-credential-mxbroker.sh`, `docs/mx-broker-git-integration-plan.md` D4), nexus-agent notify path (`nx_notify`), chezmoi scheduler bootstrap
- Related (cross-repo, prose only — different repo, not a triage dep): cc change `unify-metrics-lanes` defines the metrics-outbox lanes this change's heartbeat/auth events feed. Heartbeat emission degrades gracefully (local JSONL) if that change hasn't shipped.

## Motivation
Session-history mining (30 days) ranked the friction: (1) AADSTS70043 sign-in-frequency
expiries dominate ws work — forensics (design.md) showed ~3 true expiry events amplified
into thousands of error lines by retry storms, so the fix is a day-55 nudge plus
fail-fast, not frequent re-auth; (2) mesh/Tailscale
flakiness (~150 hits) degrades every homelab-dependent project and has zero monitoring;
(3) the audit found three cmux-bridge implementations, four bootstrap entry points, a dead
`nexus-dashboard.service` pointing at a directory deleted 2026-05-17, and the
`~/.mx/broker` 0700 invariant duplicated in four files.

AADSTS70043 is a Conditional Access sign-in-frequency policy — it cannot be automated away
(that is the policy's purpose), and mx-broker explicitly does not provide an az MSAL session.
What CAN be done: (a) convert mid-task failures into one scheduled re-auth per window via a
proactive nudge; (b) bypass az MSAL entirely for resource-only calls (Graph/ADO REST) by
consuming broker tokens over the existing socket.

## Requirements

### Req-1: Proactive re-auth nudge (`az-reauth-nudge`)
A daily systemd user timer (homelab) runs `scripts/az-reauth-nudge.sh`. The script:
- Reads MSAL token cache metadata (timestamps only — never token values) for
  `~/.azure-bbadmin` and `~/.azure-o365` to determine each identity's token issue date
  (independent clocks — see design.md).
- Compares age against the CA window (60 days, proven — design.md D1) minus a lead
  margin (default 5 days; both env-configurable).
- From day ~55, notifies once per identity per window via nexus-agent (`nx_notify`)
  with the exact re-login command (`az login --use-device-code` + identity flag).
  Deduped by a state file per identity so it never nags repeatedly.
- Exits 0 always; absent caches or unreadable metadata degrade to silence.

### Req-6: One-tap re-auth orchestrator (`az-reauth`)
A script `scripts/az-reauth.sh` (exposed as `az-reauth [identity...]`, default: both due
identities) automates everything around the interactive auth moment:
- Runs `az login --use-device-code` per identity (correct `AZURE_CONFIG_DIR`, egress
  via the existing SOCKS routing), parses the device code + URL from stderr.
- Hands off to the Mac over existing primitives: code to clipboard (`ssh mac pbcopy`),
  browser to the device-login page via `mac-open`/Edge (ProxyBridge already routes Edge
  through cloudpc, satisfying the location control). Prefer the code-prefill URL form
  if it verifies during implementation; clipboard is the fallback.
- Waits for the login poll to complete, verifies with a token probe, clears the Req-5
  fail-fast marker, re-checks broker `/health` (the ADO line depends on `az --as-o365`),
  and notifies success/failure per identity.
- Human still performs account selection + MFA — by design. No credential storage, no
  headless completion of the sign-in; the automation target is ceremony, not the policy.
The Req-1 nudge names `az-reauth` as the action, so the day-55 path is:
notification -> run one command (or tap) -> approve MFA -> everything else self-heals.

### Req-5: Fail-fast on AADSTS70043 in the az wrapper
`home/dot_local/bin/executable_az` detects AADSTS70043 in a failing call's stderr,
emits ONE notify with the re-login command, and sets a per-identity state marker; while
the marker exists, subsequent calls for that identity short-circuit with a one-line
"re-auth required" error instead of retrying against Azure. Marker clears when a
`login` invocation for that identity exits 0. Converts the observed retry storms into
a single actionable failure (design.md D2).

### Req-2: Broker token helper (`mx-token`)
A thin client `home/dot_local/bin/executable_mx-token <resource> [identity]` wrapping the
existing broker socket query (`GET /token?resource=<r>&identity=<i>` at
`~/.mx/broker/broker.sock`), printing the access token to stdout. Same hardening contract
as `git-credential-mxbroker.sh`: socket-ownership check, `--max-time 5`, silent exit 0 on
every failure, never logs the token. This is the D4 extension the git helper already
documents — it lets resource-only tooling (Graph/ADO REST) skip az MSAL entirely.

### Req-3: Mesh + broker heartbeat (`mesh-heartbeat`)
A systemd user timer (homelab) runs `scripts/mesh-heartbeat.sh` every 5 minutes:
- Probes: Tailscale reachability of mac + cloudpc (`tailscale ping -c1 --timeout 2s`),
  broker `GET /health` (per-line serving state), SOCKS tunnel liveness (TCP connect
  127.0.0.1:1080).
- Emits one JSON record per run. Sink: `~/.claude/scripts/bin/metrics-outbox` enqueue when
  present, else append to `~/.local/state/mesh-heartbeat.jsonl` (graceful degradation).
- Notifies via `nx_notify` ONLY on state transition (up->down / down->up), never on steady
  state.

### Req-4: Hygiene — delete and collapse
- Delete `home/dot_config/systemd/user/nexus-dashboard.service` (target app deleted
  2026-05-17 per nx `retire-web-dashboard-infra`; unit cannot pass its own ExecStartPre)
  and remove its row from `docs/homelab-recovery.md`. nova-dashboard remains the sole
  web dashboard.
- Single cmux bridge: measure which of `scripts/cmux-bridge.py` / the Rust
  `ssh-mesh/scripts/remote/cmux-bridge/` is actually invoked by the live workspace flow;
  keep that one, delete the other, update callers.
- Bootstrap collapse: `run_once_install-packages.sh.tmpl` becomes the single install
  entry point; `scripts/brew-install.sh` and `scripts/prerequisites.sh` become thin
  delegates (or are deleted if nothing else calls them — verify with grep first).
- `~/.mx/broker` 0700 creation moves to one helper in `scripts/utils.sh`; the four
  duplicate sites source it.

## Non-goals
- Replacing `az login` for ARM/CLI work (impossible under the CA policy; broker has no
  MSAL session).
- Building any UI. Heartbeat/auth data lands in the metrics lane; rendering is nova/nx
  territory per the service-realignment discussion.
- o365<->gmail calendar sync, request triage (mx roadmap, separate proposals).
