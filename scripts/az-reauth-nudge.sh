#!/usr/bin/env bash
# az-reauth-nudge.sh — proactive re-auth reminder before the CA sign-in-frequency
# wall bites (Req-1, design.md D1/D3).
#
# The 60-day Conditional Access window only resets on an INTERACTIVE
# `az login` — background refresh never extends it (design.md forensics #3).
# So "token age" has to be measured from the last interactive login, not from
# MSAL cache metadata (cached_at/last_modification_time track silent refresh
# and would read near-zero forever). The az wrapper (executable_az) stamps a
# per-identity login-epoch file on every successful `az login`; that is the
# primary source. MSAL cache mtime is the fallback for a machine where the
# stamp doesn't exist yet (fresh install, pre-this-change logins).
#
# Fires (once per identity per window) from day ~55 of the 60-day window,
# naming `az-reauth <identity>` as the one command to run (Req-6/1.7).
# Exits 0 always — absent caches, missing state, anything unexpected
# degrades to silence, never a failure.

set -uo pipefail

WINDOW_DAYS="${AZ_REAUTH_WINDOW_DAYS:-60}"
LEAD_DAYS="${AZ_REAUTH_LEAD_DAYS:-5}"
STATE_DIR="$HOME/.local/state/az-reauth-nudge"
mkdir -p "$STATE_DIR" 2>/dev/null || exit 0

FIRE_AGE_DAYS=$((WINDOW_DAYS - LEAD_DAYS))
NOW=$(date +%s)

source "$HOME/.claude/scripts/lib/nx-send.sh" 2>/dev/null || true

# identity -> AZURE_CONFIG_DIR
declare -A IDENTITIES=(
  [bbadmin]="$HOME/.azure-bbadmin"
  [o365]="$HOME/.azure-o365"
)

identity_age_days() {
  local cfg_dir="$1" identity="$2" epoch=""

  # Primary: login-epoch stamp written by executable_az on `az login` success.
  local stamp="$STATE_DIR/${identity}.login-epoch"
  if [ -f "$stamp" ]; then
    epoch=$(cat "$stamp" 2>/dev/null)
  fi

  # Fallback: MSAL cache file mtime (best available signal pre-stamp).
  if [ -z "$epoch" ] || ! [[ "$epoch" =~ ^[0-9]+$ ]]; then
    local cache="$cfg_dir/msal_token_cache.json"
    [ -f "$cache" ] || return 1
    epoch=$(stat -c '%Y' "$cache" 2>/dev/null || stat -f '%m' "$cache" 2>/dev/null)
    [[ "$epoch" =~ ^[0-9]+$ ]] || return 1
  fi

  echo $(( (NOW - epoch) / 86400 ))
}

for identity in "${!IDENTITIES[@]}"; do
  cfg_dir="${IDENTITIES[$identity]}"
  [ -d "$cfg_dir" ] || continue

  age_days=$(identity_age_days "$cfg_dir" "$identity") || continue
  [[ "$age_days" =~ ^[0-9]+$ ]] || continue

  [ "$age_days" -lt "$FIRE_AGE_DAYS" ] && continue

  # Dedup: one nudge per identity per window. The window is keyed by the same
  # login-epoch/mtime source used above, so a fresh login naturally opens a
  # new window (the dedup marker's recorded epoch stops matching).
  dedup_file="$STATE_DIR/${identity}.notified-epoch"
  epoch_for_window=""
  if [ -f "$STATE_DIR/${identity}.login-epoch" ]; then
    epoch_for_window=$(cat "$STATE_DIR/${identity}.login-epoch" 2>/dev/null)
  else
    epoch_for_window=$(stat -c '%Y' "$cfg_dir/msal_token_cache.json" 2>/dev/null \
      || stat -f '%m' "$cfg_dir/msal_token_cache.json" 2>/dev/null)
  fi
  if [ -f "$dedup_file" ] && [ "$(cat "$dedup_file" 2>/dev/null)" = "$epoch_for_window" ]; then
    continue
  fi

  command -v nx_notify >/dev/null 2>&1 && nx_notify \
    "Azure ${identity} token is ${age_days} days old (60d CA window). Run: az-reauth ${identity}" \
    "az-reauth due"

  [ -n "$epoch_for_window" ] && printf '%s' "$epoch_for_window" > "$dedup_file" 2>/dev/null
done

exit 0
