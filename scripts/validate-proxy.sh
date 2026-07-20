#!/bin/bash
# validate-proxy.sh — Periodic validation + remediation of CloudPC proxy stack
# Checks: SOCKS tunnel (process + port), ProxyBridge running
# Remediates: kills zombie tunnels, restarts via launchctl
# Notifies: via nexus-agent (TTS + banner), deduped with attempt counter
#
# Note: ProxyBridge rule checking is skipped when running from launchd.
# macOS App Sandbox blocks LaunchAgents from reading container prefs
# (~/Library/Containers/). Run manually to check rules: bash validate-proxy.sh --rules
set -uo pipefail

DOTFILES="${DOTFILES:-$HOME/dev/personal/installfest}"
RULES_SOURCE="$DOTFILES/scripts/proxybridge-rules.json"
STATE_FILE="$HOME/.local/logs/validate-proxy.state"
LOG_FILE="$HOME/.local/logs/validate-proxy.err.log"
NEXUS_SOCKET="${NEXUS_SOCKET:-/tmp/nexus-agent.sock}"
ISSUES=()
FIXED=()
CHECK_RULES=false

for arg in "$@"; do
    case "$arg" in
        -h|--help)
            cat <<'EOF'
Usage: bash validate-proxy.sh [--rules]

Periodic validation + remediation of the CloudPC proxy stack: checks the
SOCKS tunnel and ProxyBridge, restarts a dead tunnel via launchctl, and
notifies via nexus-agent (deduped with an attempt counter).

  --rules   also check ProxyBridge rules (skipped automatically under
            launchd — App Sandbox blocks reading container prefs there;
            run manually with this flag to check rules).
EOF
            exit 0
            ;;
        --rules) CHECK_RULES=true ;;
    esac
done

# --- State: track consecutive failures ---
read_state() {
    if [ -f "$STATE_FILE" ]; then
        FAIL_COUNT=$(grep -c '' "$STATE_FILE" 2>/dev/null || echo 0)
    else
        FAIL_COUNT=0
    fi
}

write_fail() {
    mkdir -p "$(dirname "$STATE_FILE")"
    echo "$(date '+%Y-%m-%d %H:%M:%S') $1" >> "$STATE_FILE"
}

clear_state() { rm -f "$STATE_FILE"; }

# --- Notify via nexus-agent socket ---
nx_notify() {
    local msg="$1"
    local type="${2:-proxy-health}"
    [ -S "$NEXUS_SOCKET" ] || return 0
    printf '{"event":"notification","message":"%s","type":"%s","project":"if"}\n' "$msg" "$type" \
        | socat - UNIX-CONNECT:"$NEXUS_SOCKET" 2>/dev/null || true
}

# --- 1. Check SOCKS tunnel (port health is the source of truth) ---
# Found live 2026-07-20: with `ControlMaster auto` enabled globally
# (ssh/config.tmpl `Host *`), a fresh `ssh -D 1080 ... cloudpc` invocation
# registers its port-forward on the EXISTING multiplexed connection and
# exits immediately -- by design, not a failure. `pgrep -f "ssh.*-D.*1080"`
# then never matches even though port 1080 stays open and fully functional
# via the control-master process. Gating "is the tunnel up" on that pgrep
# match made this check false-positive "down" on every single run once
# ControlMaster went global (confirmed: ~/.local/logs/validate-proxy.state
# showed continuous false "SOCKS tunnel down — launchd restart failed"
# entries every ~63s for 4+ hours, none of which reflected a real outage).
# Port LISTEN state is what actually matters for "is SOCKS usable" --
# check that FIRST; only fall back to pgrep to find a stale/zombie process
# worth cleaning up when the port is NOT listening.
if lsof -i :1080 -P -n 2>/dev/null | grep -q LISTEN; then
    : # tunnel up -- port serving, nothing to do
else
    TUNNEL_PID=$(pgrep -f "ssh.*-D.*1080.*cloudpc" 2>/dev/null | head -1)
    if [ -n "$TUNNEL_PID" ]; then
        kill "$TUNNEL_PID" 2>/dev/null
        pkill -f "ssh.*-D.*1080.*cloudpc" 2>/dev/null || true
        sleep 1
    fi

    UID_NUM=$(id -u)
    if launchctl kickstart "gui/${UID_NUM}/com.leonardoacosta.cloudpc-tunnel" 2>/dev/null; then
        sleep 3
        if lsof -i :1080 -P -n 2>/dev/null | grep -q LISTEN; then
            FIXED+=("SOCKS tunnel restored via launchd")
        else
            ISSUES+=("SOCKS tunnel down — launchd restart failed")
        fi
    else
        ISSUES+=("SOCKS tunnel down — launchctl kickstart failed")
    fi
fi

# --- 2. Check ProxyBridge is running ---
if ! pgrep -f "ProxyBridge" >/dev/null 2>&1; then
    ISSUES+=("ProxyBridge is not running")
fi

# --- 3. Compare rules (interactive only) ---
if [ "$CHECK_RULES" = true ] && [ -f "$RULES_SOURCE" ]; then
    PREFS_PLIST="$HOME/Library/Containers/com.interceptsuite.ProxyBridge/Data/Library/Preferences/com.interceptsuite.ProxyBridge.plist"
    PLIST_BUDDY="/usr/libexec/PlistBuddy"

    if [ -f "$PREFS_PLIST" ]; then
        EXPECTED_NAMES=$(grep '"processNames"' "$RULES_SOURCE" | sed 's/.*: *"\(.*\)".*/\1/' | sort)
        LIVE_NAMES=""
        LIVE_PROTOCOLS=""
        i=0
        while true; do
            name=$("$PLIST_BUDDY" -c "Print :proxyRules:$i:processNames" "$PREFS_PLIST" 2>/dev/null) || break
            proto=$("$PLIST_BUDDY" -c "Print :proxyRules:$i:protocol" "$PREFS_PLIST" 2>/dev/null)
            LIVE_NAMES="${LIVE_NAMES}${name}\n"
            [ "$proto" != "TCP" ] && LIVE_PROTOCOLS="${LIVE_PROTOCOLS}${proto} "
            i=$((i+1))
        done

        if [ "$i" -eq 0 ]; then
            ISSUES+=("ProxyBridge has no rules configured")
        else
            MISSING=""
            while IFS= read -r expected; do
                [ -z "$expected" ] && continue
                if ! echo -e "$LIVE_NAMES" | grep -qF "$expected"; then
                    MISSING="${MISSING}${expected}, "
                fi
            done <<< "$EXPECTED_NAMES"
            [ -n "$MISSING" ] && ISSUES+=("Missing ProxyBridge rules: ${MISSING%, }")
            [ -n "$LIVE_PROTOCOLS" ] && ISSUES+=("ProxyBridge has non-TCP rules (${LIVE_PROTOCOLS% }) — re-import from source")
        fi
    else
        ISSUES+=("ProxyBridge preferences file not found")
    fi
fi

# --- 4. Report with dedup ---
read_state

if [ ${#FIXED[@]} -gt 0 ]; then
    for fix in "${FIXED[@]}"; do
        echo "[validate-proxy] Fixed: $fix" >&2
    done
    if [ "$FAIL_COUNT" -gt 0 ]; then
        nx_notify "CloudPC proxy recovered after $FAIL_COUNT checks"
        clear_state
    else
        nx_notify "${FIXED[0]}"
    fi
fi

if [ ${#ISSUES[@]} -gt 0 ]; then
    echo "[validate-proxy] Issues found:" >&2
    for issue in "${ISSUES[@]}"; do
        echo "  - $issue" >&2
    done

    write_fail "${ISSUES[0]}"
    read_state

    # TTS on 1st failure only; log-only on subsequent
    if [ "$FAIL_COUNT" -eq 1 ]; then
        nx_notify "${ISSUES[0]}"
    fi
    # Always log (but no notification spam)
    exit 1
fi

# All clear — recover if we were failing
if [ "$FAIL_COUNT" -gt 0 ]; then
    nx_notify "CloudPC proxy recovered after $FAIL_COUNT failed checks"
    clear_state
fi

exit 0
