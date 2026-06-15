#!/bin/bash
# proxybridge-import-rules.sh — Load proxy rules from source JSON into ProxyBridge.
#
# ProxyBridge keeps its rule set under the :proxyRules key of its sandboxed
# preferences domain (com.interceptsuite.ProxyBridge). A fresh machine
# (post-reset) starts with ZERO rules even though ProxyBridge is installed, so
# Edge/Teams/Outlook traffic isn't proxied until rules are imported.
#
# IMPORTANT: rules MUST be written through cfprefsd (`defaults import`), NOT by
# editing the plist file directly. macOS cfprefsd caches the domain and will
# clobber any direct file write the next time ProxyBridge launches. cfprefsd is
# also why ProxyBridge must be quit during import — a running app holds the
# cached (possibly empty) state and overwrites the import on quit.
#
# Usage: bash proxybridge-import-rules.sh [--force]
set -uo pipefail

DOTFILES="${DOTFILES:-$HOME/dev/if}"
RULES_SOURCE="$DOTFILES/scripts/proxybridge-rules.json"
DOMAIN="com.interceptsuite.ProxyBridge"

FORCE=false
for arg in "$@"; do [ "$arg" = "--force" ] && FORCE=true; done

if [ ! -f "$RULES_SOURCE" ]; then
    echo "[import-rules] Source not found: $RULES_SOURCE" >&2
    exit 1
fi

if pgrep -f "ProxyBridge" >/dev/null 2>&1 && [ "$FORCE" = false ]; then
    echo "[import-rules] ProxyBridge is running — cfprefsd would clobber the import on quit." >&2
    echo "  Quit ProxyBridge, re-run this script, then relaunch. (Override: --force)" >&2
    exit 1
fi

TMP_PLIST="$(mktemp -t pb-rules).plist"
trap 'rm -f "$TMP_PLIST"' EXIT

# Export the current domain (preserves window frame etc.), merge :proxyRules
# from the JSON source, then re-import through cfprefsd. defaults export of a
# never-written domain fails; fall back to an empty plist in that case.
defaults export "$DOMAIN" "$TMP_PLIST" 2>/dev/null || printf '<?xml version="1.0"?><!DOCTYPE plist><plist version="1.0"><dict/></plist>' > "$TMP_PLIST"

/usr/bin/python3 - "$RULES_SOURCE" "$TMP_PLIST" <<'PY'
import json, plistlib, sys

rules_path, plist_path = sys.argv[1], sys.argv[2]
with open(rules_path) as f:
    rules = json.load(f)
with open(plist_path, "rb") as f:
    prefs = plistlib.load(f)
prefs["proxyRules"] = rules
with open(plist_path, "wb") as f:
    plistlib.dump(prefs, f)
print(f"[import-rules] Importing {len(rules)} rules:")
for r in rules:
    print(f"  - {r.get('processNames')} [{r.get('protocol')}] -> {r.get('action')}")
PY

defaults import "$DOMAIN" "$TMP_PLIST"

# Verify cfprefsd accepted the rules.
COUNT=$(/usr/bin/python3 -c "
import subprocess, plistlib, io
out = subprocess.run(['defaults','export','$DOMAIN','-'], capture_output=True).stdout
print(len(plistlib.load(io.BytesIO(out)).get('proxyRules', [])))
")
echo "[import-rules] cfprefsd now holds $COUNT rules. Relaunch ProxyBridge to apply."
