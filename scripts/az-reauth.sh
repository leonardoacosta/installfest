#!/usr/bin/env bash
# az-reauth.sh — one-tap re-auth orchestrator (Req-6) with optional TOTP
# paste-not-phone assist (design.md D5-A / task 1.10).
#
# USAGE
#   az-reauth [identity...]     # bbadmin | o365 | personal, default: due identities
#
# Automates everything around the interactive auth moment EXCEPT the moment
# itself: account selection + MFA stay human, by design (no credential
# storage, no headless sign-in — design.md non-goals). Per identity:
#   1. `az login --use-device-code` (via the executable_az wrapper, so its
#      Req-5 marker-clear + login-epoch stamp fire automatically on success)
#   2. Parse the device code + URL from its stderr as they stream
#   3. If a 1Password TOTP seed exists for the identity: read it (gated on
#      an active `op` session — never attempts a headless op signin), print
#      + speak the 6-digit code via nx_notify so it's readable without a
#      phone. Degrades silently to phone flow when no seed / op not signed
#      in / oathtool missing.
#   4. Clipboard the device code to the Mac (`ssh mac pbcopy`)
#   5. Open the device-login URL in Edge on the Mac (ProxyBridge already
#      routes Edge through cloudpc — no manual SOCKS wiring needed)
#   6. Wait for the login poll to complete, verify with a token probe
#   7. Re-check broker /health (ADO line depends on `az --as-o365`)
#   8. Notify success/failure per identity

set -uo pipefail

DOTFILES="${DOTFILES:-$HOME/dev/if}"
STATE_DIR="$HOME/.local/state/az-reauth-nudge"
MAC_HOST="${CC_BROWSER_MAC_HOST:-mac}"

# shellcheck disable=SC1091
. "$HOME/.claude/scripts/lib/nx-send.sh" 2>/dev/null || true

identity_flag() {
    case "$1" in
        bbadmin)  printf -- '--as-admin' ;;
        o365)     printf -- '--as-o365' ;;
        personal) printf -- '--as-personal' ;;
        *)        return 1 ;;
    esac
}

# --- Resolve target identities ---------------------------------------------
IDENTITIES=("$@")
if [ ${#IDENTITIES[@]} -eq 0 ]; then
    for id in bbadmin o365; do
        if [ -f "$STATE_DIR/${id}.reauth-required" ] || [ -f "$STATE_DIR/${id}.notified-epoch" ]; then
            IDENTITIES+=("$id")
        fi
    done
fi
if [ ${#IDENTITIES[@]} -eq 0 ]; then
    echo "az-reauth: no identity is currently due (no fail-fast marker, no nudge fired)." >&2
    echo "           pass one explicitly to force: az-reauth bbadmin|o365|personal" >&2
    exit 0
fi

# --- 1Password TOTP assist (D5-A, task 1.10) --------------------------------
# Env-configurable so Leo's actual 1Password layout (task 1.9, not yet
# enrolled) can be wired up without touching code.
AZ_TOTP_VAULT="${AZ_TOTP_VAULT:-Private}"
AZ_TOTP_ITEM_PREFIX="${AZ_TOTP_ITEM_PREFIX:-az-totp}"
AZ_TOTP_FIELD="${AZ_TOTP_FIELD:-seed}"

totp_code_for() {
    local identity="$1"
    command -v oathtool >/dev/null 2>&1 || return 1
    command -v op >/dev/null 2>&1 || return 1
    # Gate: only ever read a seed against an ALREADY-authenticated op session
    # (Leo's own `op signin`, run once, ambient in the shell). We never
    # attempt a headless/non-interactive signin — that would just trade one
    # secret prompt for another.
    op whoami >/dev/null 2>&1 || return 1
    local seed
    seed=$(op read "op://${AZ_TOTP_VAULT}/${AZ_TOTP_ITEM_PREFIX}-${identity}/${AZ_TOTP_FIELD}" 2>/dev/null) || return 1
    [ -n "$seed" ] || return 1
    oathtool --totp -b "$seed" 2>/dev/null
}

# --- Device-code capture + Mac handoff --------------------------------------
run_identity() {
    local identity="$1" flag
    flag=$(identity_flag "$identity") || { echo "az-reauth: unknown identity '$identity'" >&2; return 1; }

    echo "az-reauth: starting device-code login for ${identity}..."

    local stderr_tmp
    stderr_tmp=$(mktemp)
    az login --use-device-code "$flag" >/dev/null 2> >(tee "$stderr_tmp" >&2) &
    local az_pid=$!

    # Poll the streaming stderr for the code + URL (standard az CLI text:
    # "...open the page https://microsoft.com/devicelogin and enter the
    # code XXXXXXXXX to authenticate.").
    local code="" url="" waited=0
    while [ -z "$code" ] && [ "$waited" -lt 30 ]; do
        kill -0 "$az_pid" 2>/dev/null || break
        code=$(grep -oE '[A-Z0-9]{8,10}' "$stderr_tmp" 2>/dev/null | head -1)
        url=$(grep -oE 'https://[^ ]*devicelogin[^ ]*' "$stderr_tmp" 2>/dev/null | head -1)
        [ -n "$code" ] && break
        sleep 1
        waited=$((waited + 1))
    done
    [ -n "$url" ] || url="https://microsoft.com/devicelogin"

    if [ -z "$code" ]; then
        echo "az-reauth: ${identity} — could not parse device code within ${waited}s; check output above." >&2
    else
        echo "az-reauth: ${identity} — code ${code}, clipboarding + opening Edge on ${MAC_HOST}..."
        printf '%s' "$code" | ssh -o BatchMode=yes -o ConnectTimeout=5 "$MAC_HOST" pbcopy 2>/dev/null || \
            echo "az-reauth: ${identity} — could not reach ${MAC_HOST} to clipboard the code; copy it manually: ${code}" >&2
        ( ssh -o BatchMode=yes -o ConnectTimeout=5 "$MAC_HOST" "open -a 'Microsoft Edge' '${url}'" >/dev/null 2>&1 & ) 2>/dev/null

        local totp
        if totp=$(totp_code_for "$identity"); then
            command -v nx_notify >/dev/null 2>&1 && nx_notify \
                "${identity} device code copied. TOTP: ${totp}" "az-reauth"
        else
            command -v nx_notify >/dev/null 2>&1 && nx_notify \
                "${identity} device code copied — approve MFA on your phone." "az-reauth"
        fi
    fi

    # Block until the login poll completes (success, deny, or timeout).
    wait "$az_pid"
    local exit_code=$?
    rm -f "$stderr_tmp" 2>/dev/null || true

    if [ "$exit_code" -ne 0 ]; then
        command -v nx_notify >/dev/null 2>&1 && nx_notify \
            "az-reauth failed for ${identity} (exit ${exit_code})." "az-reauth failed"
        return "$exit_code"
    fi

    # Token probe verify (the marker/epoch stamp are already handled by the
    # executable_az wrapper on this successful `login` call).
    if ! az account show "$flag" >/dev/null 2>&1; then
        command -v nx_notify >/dev/null 2>&1 && nx_notify \
            "az-reauth: ${identity} login reported success but token probe failed." "az-reauth failed"
        return 1
    fi

    # Re-check broker health (ADO line depends on `az --as-o365`).
    local health=""
    if [ -S "$HOME/.mx/broker/broker.sock" ]; then
        health=$(curl -s --max-time 3 --unix-socket "$HOME/.mx/broker/broker.sock" \
            "http://localhost/health" 2>/dev/null)
    fi

    command -v nx_notify >/dev/null 2>&1 && nx_notify \
        "az-reauth succeeded for ${identity}.${health:+ broker: $health}" "az-reauth done"
    echo "az-reauth: ${identity} — done. broker health: ${health:-unavailable}"
    return 0
}

STATUS=0
for id in "${IDENTITIES[@]}"; do
    run_identity "$id" || STATUS=1
done
exit "$STATUS"
