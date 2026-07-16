#!/bin/sh
#
# zsa-firmware-check.sh — Mac-only: detect a freshly rebuilt ZSA Voyager
# firmware, stage it in ~/Downloads, and drive the (unavoidably manual)
# flash step via nx notifications.
#
# Called from BOTH scripts/hooks/post-commit (local path: commit made
# directly on the Mac) and scripts/hooks/remote-apply.sh (cross-machine
# path: pushed from homelab, this runs after SSH lands on the Mac) — it
# self-gates on macOS so it's a safe instant no-op everywhere else,
# mirroring remote-apply.sh's existing Mac-only raycast-notify block.
# This is what satisfies "ssh to the mac if not already on it": the
# script itself doesn't SSH — whichever hook path is already running
# (local on Mac, or post-SSH on Mac) just calls it in place.
#
# WHY IT CAN'T FULLY AUTOMATE THE FLASH: ZSA's scriptable API (kontroll,
# talking to Keymapp) exposes no flash RPC — see
# github.com/zsa/kontroll proto/keymapp.proto (GetStatus/GetKeyboards/
# ConnectKeyboard/SetLayer/LED control only). Entering bootloader mode
# (physical reset button, or a QK_BOOT key mapped in the layout) is a
# real keypress a human makes on the keyboard itself; nothing on the
# host side can inject that over USB. So this script automates
# everything AROUND that one manual step: pull latest firmware repo ->
# stage the built artifact in ~/Downloads -> notify "ready, go press
# reset + flash in Keymapp" -> poll for the firmware version to change
# -> announce success (or a gentle nudge on timeout, never a false
# failure — see the design note above the poll loop).
#
# UNVERIFIED — see beads if-cgf5 for the live-verification follow-up:
#   - Exact `kontroll status` output/exit-code shape connected vs not
#     (no Mac access / kontroll install from this session to check).
#   - GH Action artifact naming/location once poulainpi/oryx-with-
#     custom-qmk is actually forked — the `gh run download --name`
#     guess below needs confirming against the real workflow's
#     `actions/upload-artifact` step.
#   - Whether $NEXUS_ATTACH_SECRET loads from ~/.env in this
#     non-interactive shell without the explicit source below.

set +e

# --- Mac-only guard -------------------------------------------------------
case "$(uname -s)" in
    Darwin) ;;
    *) exit 0 ;;
esac

# --- Config ----------------------------------------------------------------
# Fork https://github.com/poulainpi/oryx-with-custom-qmk, then either:
#   - clone it yourself to $ZSA_REPO_DIR once, or
#   - set ZSA_GH_REPO ("you/oryx-with-custom-qmk") and let this script
#     clone it on first run.
ZSA_REPO_DIR="${ZSA_REPO_DIR:-$HOME/Downloads/oryx-with-custom-qmk}"
ZSA_GH_REPO="${ZSA_GH_REPO:-}"
POLL_TIMEOUT_SECS="${ZSA_POLL_TIMEOUT_SECS:-900}"   # 15 min
POLL_INTERVAL_SECS=15
STATE_DIR="$HOME/.local/state"
LOG="$STATE_DIR/if-deploy.log"
mkdir -p "$STATE_DIR"

# nx_notify needs NEXUS_ATTACH_SECRET; non-interactive SSH/git-hook shells
# don't source ~/.zshrc, so pull it from ~/.env directly (same file
# nexus-agent itself reads via its launchd/systemd EnvironmentFile).
[ -f "$HOME/.env" ] && . "$HOME/.env"
[ -f "$HOME/.claude/scripts/lib/nx-send.sh" ] && . "$HOME/.claude/scripts/lib/nx-send.sh"

{
    echo "--- zsa-firmware-check $(date -u +%FT%TZ) ---"

    # --- First-run bootstrap: clone if configured but not present yet ---
    if [ ! -d "$ZSA_REPO_DIR/.git" ]; then
        if [ -n "$ZSA_GH_REPO" ] && command -v gh >/dev/null 2>&1; then
            echo "bootstrap: cloning $ZSA_GH_REPO into $ZSA_REPO_DIR"
            gh repo clone "$ZSA_GH_REPO" "$ZSA_REPO_DIR" >/dev/null 2>&1 \
                || echo "err: clone of $ZSA_GH_REPO failed"
        fi
    fi
    if [ ! -d "$ZSA_REPO_DIR/.git" ]; then
        echo "skip: $ZSA_REPO_DIR not cloned yet (fork the template first, or set ZSA_GH_REPO)"
        exit 0
    fi

    if ! command -v kontroll >/dev/null 2>&1; then
        echo "skip: kontroll not installed (github.com/zsa/kontroll releases)"
        exit 0
    fi

    # --- Pull latest source + detect a real change -----------------------
    # --ff-only (never --hard-reset): this is a working repo you edit
    # keymap.c in directly, unlike the dotfiles remote-apply flow.
    PRE_SHA=$(git -C "$ZSA_REPO_DIR" rev-parse HEAD 2>/dev/null)
    if ! git -C "$ZSA_REPO_DIR" pull --ff-only -q; then
        echo "err: git pull --ff-only failed in $ZSA_REPO_DIR (local edits/diverged history?)"
        exit 0
    fi
    POST_SHA=$(git -C "$ZSA_REPO_DIR" rev-parse HEAD 2>/dev/null)
    REPO_CHANGED=0
    [ "$PRE_SHA" != "$POST_SHA" ] && REPO_CHANGED=1

    # --- Best-effort: pull the latest GH Action artifact too ------------
    # Artifact name "firmware" is a guess — confirm against the real
    # workflow's upload-artifact step (beads if-cgf5).
    if [ -n "$ZSA_GH_REPO" ] && command -v gh >/dev/null 2>&1; then
        gh run download --repo "$ZSA_GH_REPO" --dir "$ZSA_REPO_DIR" --name firmware >/dev/null 2>&1 \
            && REPO_CHANGED=1
    fi

    if [ "$REPO_CHANGED" -eq 0 ]; then
        echo "skip: no new commits or artifacts in $ZSA_REPO_DIR"
        exit 0
    fi
    echo "firmware repo updated: $PRE_SHA -> $POST_SHA"

    # --- Locate the newest built artifact (BSD stat -f, macOS-only) -----
    FW_FILE=""
    FW_MTIME=0
    for f in "$ZSA_REPO_DIR"/*.bin "$ZSA_REPO_DIR"/*/*.bin; do
        [ -f "$f" ] || continue
        m=$(stat -f '%m' "$f" 2>/dev/null) || continue
        if [ "$m" -gt "$FW_MTIME" ]; then
            FW_MTIME="$m"
            FW_FILE="$f"
        fi
    done
    if [ -z "$FW_FILE" ]; then
        echo "skip: no .bin firmware artifact found under $ZSA_REPO_DIR"
        exit 0
    fi
    echo "firmware artifact: $FW_FILE"

    # --- Stage a copy in Downloads (may already be there) ----------------
    DOWNLOADS_COPY="$HOME/Downloads/$(basename "$FW_FILE")"
    [ "$FW_FILE" != "$DOWNLOADS_COPY" ] && cp -f "$FW_FILE" "$DOWNLOADS_COPY"

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
