#!/bin/bash

# Required parameters:
# @raycast.schemaVersion 1
# @raycast.title img
# @raycast.mode silent

# Optional parameters:
# @raycast.icon 📷
# @raycast.packageName Clipboard Image to Homelab

# Documentation:
# @raycast.description Paste clipboard image to homelab (~/screenshots/YYYY-MM) for Claude Code
# @raycast.author leonardoacosta
# @raycast.authorURL https://raycast.com/leonardoacosta

# Destination redesign 2026-07-22: one fixed month-bucketed sink outside every
# repo, self-pruning at 90 days. Replaces both the old ~/tmp/images dump and
# paste-image.sh's 733-line focused-repo detection (deleted same day) — repos
# no longer receive screenshots at all, Claude Code reads the absolute path.

# Capture frontmost app now so we can restore focus before the auto-paste
# (ssh below takes a beat; focus can drift to Raycast or notifications).
FRONTMOST=$(osascript -e 'tell application "System Events" to name of first application process whose frontmost is true' 2>/dev/null || echo "")

SSH_HOST="nyaptor@homelab.tail296462.ts.net"
MONTH=$(date +%Y-%m)
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
LOCAL_TMP="/tmp/clipboard_${TIMESTAMP}.png"
REMOTE_DIR="/home/nyaptor/screenshots/${MONTH}"
REMOTE_PATH="${REMOTE_DIR}/img_${TIMESTAMP}.png"

# Save clipboard image to temp file (also our "is there an image?" check)
pngpaste "$LOCAL_TMP" 2>/dev/null

if [[ ! -f "$LOCAL_TMP" ]]; then
    osascript -e 'display notification "No image in clipboard" with title "img"'
    exit 1
fi

# Single connection: mkdir + upload + prune (>90d files, then empty month dirs)
ssh "$SSH_HOST" "mkdir -p '$REMOTE_DIR' && cat > '$REMOTE_PATH' \
  && find /home/nyaptor/screenshots -type f -mtime +90 -delete 2>/dev/null; \
  find /home/nyaptor/screenshots -mindepth 1 -type d -empty -delete 2>/dev/null" < "$LOCAL_TMP"
STATUS=$?

rm -f "$LOCAL_TMP"

if [[ $STATUS -ne 0 ]]; then
    osascript -e 'display notification "Upload failed — is homelab reachable?" with title "img"'
    exit 1
fi

# Copy path to clipboard, then auto-paste into the originating app
echo -n "$REMOTE_PATH" | pbcopy
if [[ -n "$FRONTMOST" ]]; then
    osascript -e "tell application \"System Events\" to set frontmost of process \"$FRONTMOST\" to true" 2>/dev/null
    # settle delay so the focus change reaches the WindowServer before keystroke
    sleep 0.08
fi
osascript -e 'tell application "System Events" to keystroke "v" using command down' 2>/dev/null

osascript -e "display notification \"Image saved: $REMOTE_PATH (pasted)\" with title \"img\""
