#!/bin/sh
#
# zsa-firmware-check.sh — Mac-only: check out the pinned ZSA Voyager
# firmware submodule commit, fetch its GH-Action-built artifact, stage it
# in ~/Downloads, and drive the (unavoidably manual) flash step via nx
# notifications.
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
# SOURCE LIVES AS A PINNED SUBMODULE, NOT A FREE-FLOATING CLONE:
# apps/zsa-voyager-keymap (leonardoacosta/oryx-with-custom-qmk, forked
# from poulainpi's template) is tracked by installfest itself at a
# specific commit SHA. Advancing to newer firmware source is a deliberate
# `git -C apps/zsa-voyager-keymap fetch && git submodule update --remote`
# + a commit in installfest, not something this script does on its own —
# its job is just to make sure whatever commit installfest's history
# currently records is actually checked out on disk, then go get that
# commit's built artifact.
#
# WHY IT CAN'T FULLY AUTOMATE THE FLASH: ZSA's scriptable API (kontroll,
# talking to Keymapp) exposes no flash RPC — see
# github.com/zsa/kontroll proto/keymapp.proto (GetStatus/GetKeyboards/
# ConnectKeyboard/SetLayer/LED control only). Entering bootloader mode
# (physical reset button, or a QK_BOOT key mapped in the layout) is a
# real keypress a human makes on the keyboard itself; nothing on the
# host side can inject that over USB. So this script automates
# everything AROUND that one manual step: sync the submodule -> fetch
# the built .bin for that commit -> stage it in ~/Downloads -> notify
# "ready, go press reset + flash in Keymapp" -> poll for the firmware
# version to change -> announce completion (or a gentle nudge on
# timeout — see the design note above the poll loop).
#
# UNVERIFIED — see beads if-cgf5 for the live-verification follow-up:
#   - Exact `kontroll status` output/exit-code shape connected vs not
#     (no Mac access / kontroll install from this session to check).
#   - GH Action artifact naming — the `gh run download --name firmware`
#     guess below needs confirming against the real workflow's
#     `actions/upload-artifact` step in leonardoacosta/oryx-with-custom-qmk.
#   - Whether $NEXUS_ATTACH_SECRET loads from ~/.env in this
#     non-interactive shell without the explicit source below.

set +e

# --- Mac-only guard -------------------------------------------------------
case "$(uname -s)" in
    Darwin) ;;
    *) exit 0 ;;
esac

# --- Config ----------------------------------------------------------------
REPO_ROOT="${REPO_ROOT:-$HOME/dev/personal/installfest}"
ZSA_SUBMODULE_PATH="apps/zsa-voyager-keymap"
ZSA_REPO_DIR="$REPO_ROOT/$ZSA_SUBMODULE_PATH"
ZSA_GH_REPO="${ZSA_GH_REPO:-leonardoacosta/oryx-with-custom-qmk}"
DOWNLOAD_DIR="$HOME/Downloads/zsa-voyager-firmware"
POLL_TIMEOUT_SECS="${ZSA_POLL_TIMEOUT_SECS:-900}"   # 15 min
POLL_INTERVAL_SECS=15
STATE_DIR="$HOME/.local/state"
LOG="$STATE_DIR/if-deploy.log"
mkdir -p "$STATE_DIR" "$DOWNLOAD_DIR"

# nx_notify needs NEXUS_ATTACH_SECRET; non-interactive SSH/git-hook shells
# don't source ~/.zshrc, so pull it from ~/.env directly (same file
# nexus-agent itself reads via its launchd/systemd EnvironmentFile).
[ -f "$HOME/.env" ] && . "$HOME/.env"
[ -f "$HOME/.claude/scripts/lib/nx-send.sh" ] && . "$HOME/.claude/scripts/lib/nx-send.sh"

{
    echo "--- zsa-firmware-check $(date -u +%FT%TZ) ---"

    if [ ! -d "$REPO_ROOT/.git" ]; then
        echo "err: $REPO_ROOT is not a git repo (REPO_ROOT=$REPO_ROOT)"
        exit 0
    fi

    if ! command -v kontroll >/dev/null 2>&1; then
        echo "skip: kontroll not installed (github.com/zsa/kontroll releases)"
        exit 0
    fi

    # --- Sync the submodule to whatever commit installfest pins --------
    PRE_SHA=""
    [ -d "$ZSA_REPO_DIR/.git" ] && PRE_SHA=$(git -C "$ZSA_REPO_DIR" rev-parse HEAD 2>/dev/null)

    if ! git -C "$REPO_ROOT" submodule update --init --recursive -- "$ZSA_SUBMODULE_PATH" >/dev/null 2>&1; then
        echo "err: git submodule update failed for $ZSA_SUBMODULE_PATH"
        exit 0
    fi
    POST_SHA=$(git -C "$ZSA_REPO_DIR" rev-parse HEAD 2>/dev/null)
    REPO_CHANGED=0
    [ "$PRE_SHA" != "$POST_SHA" ] && REPO_CHANGED=1

    # --- Best-effort: fetch the GH Action's built artifact for this commit ---
    # Artifact name "firmware" is a guess — confirm against the real
    # workflow's upload-artifact step (beads if-cgf5).
    if command -v gh >/dev/null 2>&1; then
        gh run download --repo "$ZSA_GH_REPO" --dir "$DOWNLOAD_DIR" --name firmware >/dev/null 2>&1 \
            && REPO_CHANGED=1
    fi

    if [ "$REPO_CHANGED" -eq 0 ]; then
        echo "skip: submodule unchanged ($POST_SHA) and no new artifact"
        exit 0
    fi
    echo "firmware updated: submodule $PRE_SHA -> $POST_SHA"

    # --- Locate the newest built artifact (BSD stat -f, macOS-only) -----
    FW_FILE=""
    FW_MTIME=0
    for f in "$DOWNLOAD_DIR"/*.bin "$DOWNLOAD_DIR"/*/*.bin; do
        [ -f "$f" ] || continue
        m=$(stat -f '%m' "$f" 2>/dev/null) || continue
        if [ "$m" -gt "$FW_MTIME" ]; then
            FW_MTIME="$m"
            FW_FILE="$f"
        fi
    done
    if [ -z "$FW_FILE" ]; then
        echo "skip: no .bin firmware artifact found under $DOWNLOAD_DIR"
        exit 0
    fi
    echo "firmware artifact: $FW_FILE"

    # --- Stage a flat copy directly in ~/Downloads for drag-into-Keymapp ---
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
