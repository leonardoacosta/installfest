#!/usr/bin/env bash
# dotenv-key-rotation-check.sh — periodic drift check + re-provision for the
# shared DOTENV_PRIVATE_KEY across ct/tc/tl/mv/oo (if-jhqf, follow-on to cc's
# onepassword-shared-secret-provisioning proposal).
#
# Vercel Sensitive env vars cannot be read back to diff (confirmed in that
# proposal's E2E notes: `vercel env ls` never returns the value) — so "drift"
# here means "has the 1Password-stored value changed since the last time THIS
# script re-provisioned it", tracked via a local hash cache, not a live
# Vercel-side comparison. On a hash mismatch (or no cache yet), re-run cc's
# manual provisioning script for that repo and update the cache on success.
#
# Exits 0 always — a missing `op`/cc checkout/state dir degrades to a skipped
# repo with a logged reason, never a hard failure (this runs unattended).

set -uo pipefail

if [[ "${1:-}" == "--help" || "${1:-}" == "-h" ]]; then
	cat <<'EOF'
Usage: bash scripts/dotenv-key-rotation-check.sh   (no args; run on a timer)

Checks each registered repo's 1Password-stored DOTENV_PRIVATE_KEY against a
local last-provisioned hash cache; on mismatch (including first run), calls
cc's onepassword-provision-secret to re-provision Vercel Preview+Production
and updates the cache. Exits 0 always.

Env: DOTENV_ROTATION_REPOS (default "ct tc tl mv oo"),
     DOTENV_ROTATION_VAULT (default "priceless"),
     DOTENV_ROTATION_CC_ROOT (default "$HOME/dev/cc")
EOF
	exit 0
fi

REPOS="${DOTENV_ROTATION_REPOS:-ct tc tl mv oo}"
VAULT="${DOTENV_ROTATION_VAULT:-priceless}"
CC_ROOT="${DOTENV_ROTATION_CC_ROOT:-$HOME/dev/cc}"
PROVISION_SCRIPT="$CC_ROOT/scripts/bin/onepassword-provision-secret"
STATE_DIR="$HOME/.local/state/dotenv-key-rotation"
mkdir -p "$STATE_DIR" 2>/dev/null || exit 0

if ! command -v op >/dev/null 2>&1; then
	echo "dotenv-key-rotation-check: 'op' not on PATH, skipping this run" >&2
	exit 0
fi

if [[ ! -x "$PROVISION_SCRIPT" ]]; then
	echo "dotenv-key-rotation-check: $PROVISION_SCRIPT not found/executable, skipping this run" >&2
	exit 0
fi

status=0
for repo in $REPOS; do
	# Check op read's own exit code directly -- sha256sum of empty stdin still
	# produces a valid-looking (non-empty) hash, so an emptiness check on the
	# piped result alone silently mistakes an op-read failure for "value is
	# the empty string" (found via a real dry run, not by inspection).
	secret_value=$(op read "op://$VAULT/$repo/password" 2>/dev/null)
	op_status=$?
	if [[ $op_status -ne 0 || -z "$secret_value" ]]; then
		echo "dotenv-key-rotation-check[$repo]: op read failed (exit $op_status), skipping" >&2
		status=1
		continue
	fi
	current_hash=$(printf '%s' "$secret_value" | sha256sum | awk '{print $1}')
	unset secret_value

	cache_file="$STATE_DIR/$repo.hash"
	stored_hash=""
	[[ -f "$cache_file" ]] && stored_hash=$(cat "$cache_file" 2>/dev/null)

	if [[ "$current_hash" == "$stored_hash" ]]; then
		continue
	fi

	echo "dotenv-key-rotation-check[$repo]: 1Password value changed since last provision, re-running $PROVISION_SCRIPT" >&2
	if "$PROVISION_SCRIPT" "$repo"; then
		printf '%s' "$current_hash" >"$cache_file.tmp" && mv "$cache_file.tmp" "$cache_file"
		echo "dotenv-key-rotation-check[$repo]: re-provisioned OK, cache updated" >&2
	else
		echo "dotenv-key-rotation-check[$repo]: onepassword-provision-secret failed, cache left stale so next run retries" >&2
		status=1
	fi
done

exit "$status"
