#!/usr/bin/env bash
# mesh-heartbeat.sh — probe the two zero-observability dependencies everything
# else rides on: the Tailscale mesh and the mx-broker token path (Req-3).
#
# Probes per run:
#   - Tailscale reachability of mac + cloudpc (tailscale ping)
#   - mx-broker GET /health (per-line serving state)
#   - SOCKS tunnel liveness (TCP connect 127.0.0.1:1080)
#
# Emits one JSON record per run to the metrics outbox if present, else a
# local JSONL (graceful degradation). Notifies via nx_notify ONLY on a
# state transition (up->down / down->up) per probe — steady state is silent.

set -uo pipefail

if [[ "${1:-}" == "--help" || "${1:-}" == "-h" ]]; then
	cat <<'EOF'
Usage: bash scripts/mesh-heartbeat.sh   (no args; run on a timer)

Probes Tailscale reachability of mac + cloudpc, mx-broker GET /health, and
the local SOCKS tunnel. Emits one JSON record per run to the metrics
outbox (or a local JSONL fallback) and notifies via nx_notify only on a
state transition (up->down / down->up).
EOF
	exit 0
fi

STATE_DIR="$HOME/.local/state/mesh-heartbeat"
mkdir -p "$STATE_DIR" 2>/dev/null || exit 0

OUTBOX="$HOME/.claude/scripts/bin/metrics-outbox"
FALLBACK_LOG="$HOME/.local/state/mesh-heartbeat.jsonl"

# shellcheck disable=SC1091
. "$HOME/.claude/scripts/lib/nx-send.sh" 2>/dev/null || true

NOW=$(date +%s)

probe_tailscale() {
    # tailscale peer hostnames, NOT the ssh-config aliases (mac/cloudpc) —
    # `tailscale status` is the source of truth: macbook-pro, cloud-pc.
    # --until-direct=false matters: this tailscale version's default exits
    # non-zero when a ping succeeds over DERP but no direct path formed yet,
    # which reads as a false "down" for a mesh probe that only cares about
    # reachability, not path quality. --c (not -c) is this CLI's flag form.
    local peer="$1"
    tailscale ping --c 1 --timeout 2s --until-direct=false "$peer" >/dev/null 2>&1 && echo up || echo down
}

probe_broker() {
    local sock="$HOME/.mx/broker/broker.sock"
    [ -S "$sock" ] || { echo down; return; }
    curl -s --max-time 3 --unix-socket "$sock" "http://localhost/health" >/dev/null 2>&1 \
        && echo up || echo down
}

probe_socks() {
    (exec 3<>/dev/tcp/127.0.0.1/1080) 2>/dev/null && { exec 3>&-; echo up; } || echo down
}

# probe-name -> status
declare -A STATUS=(
    [tailscale_mac]="$(probe_tailscale macbook-pro)"
    [tailscale_cloudpc]="$(probe_tailscale cloud-pc)"
    [broker_health]="$(probe_broker)"
    [socks_tunnel]="$(probe_socks)"
)

# --- Transition-only notify --------------------------------------------------
for probe in "${!STATUS[@]}"; do
    state_file="$STATE_DIR/${probe}.state"
    prev=""
    [ -f "$state_file" ] && prev=$(cat "$state_file" 2>/dev/null)
    curr="${STATUS[$probe]}"

    if [ -n "$prev" ] && [ "$prev" != "$curr" ]; then
        command -v nx_notify >/dev/null 2>&1 && nx_notify \
            "mesh-heartbeat: ${probe} ${prev} -> ${curr}" "mesh-heartbeat"
    fi
    printf '%s' "$curr" > "$state_file" 2>/dev/null || true
done

# --- Emit record --------------------------------------------------------------
RECORD=$(printf '{"id":"mesh-heartbeat-%s","ts":%s,"tailscale_mac":"%s","tailscale_cloudpc":"%s","broker_health":"%s","socks_tunnel":"%s"}' \
    "$NOW" "$NOW" "${STATUS[tailscale_mac]}" "${STATUS[tailscale_cloudpc]}" \
    "${STATUS[broker_health]}" "${STATUS[socks_tunnel]}")

if [ -x "$OUTBOX" ]; then
    "$OUTBOX" enqueue --table mesh_heartbeat --payload "$RECORD" >/dev/null 2>&1 \
        || printf '%s\n' "$RECORD" >> "$FALLBACK_LOG" 2>/dev/null || true
else
    printf '%s\n' "$RECORD" >> "$FALLBACK_LOG" 2>/dev/null || true
fi

exit 0
