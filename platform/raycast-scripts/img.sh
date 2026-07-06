#!/bin/bash

# Required parameters:
# @raycast.schemaVersion 1
# @raycast.title img
# @raycast.mode silent

# Optional parameters:
# @raycast.icon 📷
# @raycast.packageName Clipboard Image to Homelab

# Documentation:
# @raycast.description Paste clipboard image to homelab for Claude Code
# @raycast.author leonardoacosta
# @raycast.authorURL https://raycast.com/leonardoacosta

# Capture frontmost app now so we can restore focus before the auto-paste
# (ssh+scp below takes a beat; focus can drift to Raycast or notifications).
FRONTMOST=$(osascript -e 'tell application "System Events" to name of first application process whose frontmost is true' 2>/dev/null || echo "")

SSH_HOST="nyaptor@homelab.tail296462.ts.net"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
LOCAL_TMP="/tmp/clipboard_${TIMESTAMP}.png"
REMOTE_DIR="/home/nyaptor/tmp/images"
REMOTE_PATH="${REMOTE_DIR}/clipboard_${TIMESTAMP}.png"

# Save clipboard image to temp file
pngpaste "$LOCAL_TMP" 2>/dev/null

if [[ ! -f "$LOCAL_TMP" ]]; then
    osascript -e 'display notification "No image in clipboard" with title "img"'
    exit 1
fi

# Ensure remote directory exists and copy
ssh "$SSH_HOST" "mkdir -p $REMOTE_DIR"
scp -q "$LOCAL_TMP" "${SSH_HOST}:${REMOTE_PATH}"

# Clean up local temp
rm "$LOCAL_TMP"

# Copy path to clipboard, then auto-paste into the originating app
echo -n "$REMOTE_PATH" | pbcopy
if [[ -n "$FRONTMOST" ]]; then
    osascript -e "tell application \"System Events\" to set frontmost of process \"$FRONTMOST\" to true" 2>/dev/null
    # settle delay so the focus change reaches the WindowServer before keystroke
    sleep 0.08
fi
osascript -e 'tell application "System Events" to keystroke "v" using command down' 2>/dev/null

osascript -e "display notification \"Image saved: $REMOTE_PATH (pasted)\" with title \"img\""
