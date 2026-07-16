#!/bin/sh
#
# zsa-firmware-check.sh — Mac-only: notice a freshly staged ZSA Voyager
# firmware .bin in ~/Downloads and drive the (unavoidably manual) flash
# step via nx notifications.
#
# Two call shapes:
#   zsa-firmware-check.sh                  — generic check: look for the
#     newest .bin under ~/Downloads, skip if we've already notified about
#     that exact file (state file keyed by path+mtime). Called from
#     scripts/hooks/post-commit (local path: commit made directly on the
#     Mac) and scripts/hooks/remote-apply.sh (cross-machine path: pushed
#     from homelab). Self-gates on macOS, safe no-op everywhere else.
#   zsa-firmware-check.sh /path/to/file.bin — explicit handoff: skip the
#     "what's new" guesswork entirely. Called by
#     scripts/hooks/zsa-firmware-build.sh right after it scp's a freshly
#     built artifact over from Homelab.
#
# WHY IT CAN'T FULLY AUTOMATE THE FLASH: ZSA's scriptable API (kontroll,
# talking to Keymapp) exposes no flash RPC — see
# github.com/zsa/kontroll proto/keymapp.proto (GetStatus/GetKeyboards/
# ConnectKeyboard/SetLayer/LED control only). Entering bootloader mode
# (physical reset button, or a QK_BOOT key mapped in the layout) is a
# real keypress a human makes on the keyboard itself; nothing on the
# host side can inject that over USB. So this script automates
# everything AROUND that one manual step: notice the artifact -> notify
# "ready, go press reset + flash in Keymapp" -> poll for the firmware
# version to change -> announce completion (or a gentle nudge on
# timeout — see the design note above the poll loop).
#
# UNVERIFIED — see beads if-cgf5 for the live-verification follow-up:
#   - Exact `kontroll status` output/exit-code shape connected vs not
#     (no Mac access / kontroll install from this session to check).
#   - Whether $NEXUS_ATTACH_SECRET loads from ~/.env in this
#     non-interactive shell without the explicit source below.

set +e

# --- Mac-only guard -------------------------------------------------------
case "$(uname -s)" in
    Darwin) ;;
    *) exit 0 ;;
esac

# --- Config ----------------------------------------------------------------
DOWNLOADS_DIR="$HOME/Downloads"
POLL_TIMEOUT_SECS="${ZSA_POLL_TIMEOUT_SECS:-900}"   # 15 min
POLL_INTERVAL_SECS=15
STATE_DIR="$HOME/.local/state"
STATE_FILE="$STATE_DIR/zsa-firmware-check.json"
LOG="$STATE_DIR/if-deploy.log"
mkdir -p "$STATE_DIR"

# nx_notify needs NEXUS_ATTACH_SECRET; non-interactive SSH/git-hook shells
# don't source ~/.zshrc, so pull it from ~/.env directly (same file
# nexus-agent itself reads via its launchd/systemd EnvironmentFile).
[ -f "$HOME/.env" ] && . "$HOME/.env"
[ -f "$HOME/.claude/scripts/lib/nx-send.sh" ] && . "$HOME/.claude/scripts/lib/nx-send.sh"

{
    echo "--- zsa-firmware-check $(date -u +%FT%TZ) ---"

    if ! command -v kontroll >/dev/null 2>&1; then
        echo "skip: kontroll not installed (github.com/zsa/kontroll releases)"
        exit 0
    fi

    # --- Which file are we checking? ------------------------------------
    if [ -n "$1" ]; then
        FW_FILE="$1"
        if [ ! -f "$FW_FILE" ]; then
            echo "err: explicit firmware path $FW_FILE does not exist"
            exit 0
        fi
        echo "explicit handoff: $FW_FILE"
    else
        FW_FILE=""
        FW_MTIME=0
        for f in "$DOWNLOADS_DIR"/*.bin; do
            [ -f "$f" ] || continue
            m=$(stat -f '%m' "$f" 2>/dev/null) || continue
            if [ "$m" -gt "$FW_MTIME" ]; then
                FW_MTIME="$m"
                FW_FILE="$f"
            fi
        done
        if [ -z "$FW_FILE" ]; then
            echo "skip: no .bin under $DOWNLOADS_DIR"
            exit 0
        fi
        # Dedup: don't re-notify about a file we already handled.
        LAST_FILE=""
        [ -f "$STATE_FILE" ] && LAST_FILE=$(grep -o '"file":"[^"]*"' "$STATE_FILE" 2>/dev/null | cut -d'"' -f4)
        if [ "$FW_FILE" = "$LAST_FILE" ]; then
            echo "skip: already notified about $FW_FILE"
            exit 0
        fi
        echo "found: $FW_FILE"
    fi
    printf '{"file":"%s"}' "$FW_FILE" > "$STATE_FILE" 2>/dev/null

    # --- Keyboard connected? ----------------------------------------------
    # VERIFY LIVE (if-cgf5): assumes nonzero exit or "no keyboard"/"not
    # connected" text when nothing is attached, matching typical CLI
    # convention — adjust once kontroll's real output is in hand.
    KB_STATUS=$(kontroll status 2>&1)
    KB_RC=$?
    if [ "$KB_RC" -ne 0 ] || printf '%s' "$KB_STATUS" | grep -qi 'no keyboard\|not connected'; then
        echo "keyboard: not connected — firmware staged, skipping notify/flash-monitor"
        nx_notify "New Voyager firmware staged in Downloads, but no keyboard is connected. Plug it in and flash whenever." "ZSA Firmware" "desktop,tts"
        exit 0
    fi
    echo "keyboard connected: $KB_STATUS"

    PRE_FW_VERSION=$(printf '%s' "$KB_STATUS" | grep -i 'firmware' | head -1)

    nx_notify "New Voyager firmware ready: $(basename "$FW_FILE"). Press reset and flash it in Keymapp." "ZSA Firmware" "desktop,tts"

    # --- Poll for the flash to actually happen ----------------------------
    # A timeout here is a nudge, not a failure verdict — the human may
    # just not have gotten to it yet. Never announce "flash failed"; we
    # have no way to distinguish "hasn't flashed yet" from "flash errored"
    # since kontroll can't observe the bootloader/flash process itself.
    ELAPSED=0
    FLASHED=0
    CUR_FW_VERSION="$PRE_FW_VERSION"
    while [ "$ELAPSED" -lt "$POLL_TIMEOUT_SECS" ]; do
        sleep "$POLL_INTERVAL_SECS"
        ELAPSED=$((ELAPSED + POLL_INTERVAL_SECS))
        CUR_STATUS=$(kontroll status 2>/dev/null)
        CUR_FW_VERSION=$(printf '%s' "$CUR_STATUS" | grep -i 'firmware' | head -1)
        if [ -n "$CUR_FW_VERSION" ] && [ "$CUR_FW_VERSION" != "$PRE_FW_VERSION" ]; then
            FLASHED=1
            break
        fi
    done

    if [ "$FLASHED" -eq 1 ]; then
        echo "flash confirmed: $CUR_FW_VERSION"
        nx_notify "Voyager flash complete: $CUR_FW_VERSION" "ZSA Firmware" "desktop,tts"
    else
        echo "flash not detected within ${POLL_TIMEOUT_SECS}s — no verdict, just a nudge"
        nx_notify "Still waiting on the Voyager flash after $((POLL_TIMEOUT_SECS / 60)) min — no rush, flash whenever." "ZSA Firmware" "desktop,tts"
    fi

    echo "=== zsa-firmware-check done ==="
} >> "$LOG" 2>&1

exit 0
