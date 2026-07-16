#!/bin/sh
#
# zsa-firmware-build.sh — Homelab-only: poll Oryx for a new layout
# revision, and if there is one, replicate the (now-disabled) GitHub
# Action locally: fetch source -> merge into the fork -> sync the
# qmk_firmware submodule -> docker build + qmk compile -> ship the .bin
# to the Mac's Downloads over the SSH mesh -> hand off to the Mac-side
# notify/poll flow (zsa-firmware-check.sh).
#
# Runs on Homelab specifically (not the Mac) because Homelab already has
# Docker (see dot_zsh/rc/linux.zsh); the Mac stays scoped to Keymapp +
# the physical keyboard. GitHub Actions was explicitly disabled for
# leonardoacosta/oryx-with-custom-qmk (2026-07-16, `gh api -X PUT
# repos/.../actions/permissions -F enabled=false`) — this script is the
# direct local replacement for that fork's own
# .github/workflows/fetch-and-build-layout.yml: same GraphQL fetch, same
# git branches (oryx -> main), same qmk_firmware submodule dance, same
# docker build, just run here instead of on GitHub's runners.
#
# DELIBERATELY DOES NOT bump installfest's own pin on apps/zsa-voyager-
# keymap. It pushes new commits to that submodule's own `main`/`oryx`
# branches on GitHub, but leaves installfest's gitlink alone — after a
# successful build, `git status` in installfest will show
# "apps/zsa-voyager-keymap (new commits)" until you deliberately run
# `git add apps/zsa-voyager-keymap && git commit` here. That's the
# reviewable-pin model you picked for the submodule, applied
# consistently: a firmware source change is worth a conscious look
# before installfest's own history calls it "current."
#
# UNVERIFIED — see beads if-cgf5:
#   - Never run end-to-end (no live Oryx/docker/scp pass from this
#     session). Layout ID (Br7g0) and geometry (voyager) are confirmed
#     from your Oryx URL; everything else mirrors the fork's own
#     workflow YAML line-for-line but hasn't executed for real.
#   - Assumes `git push` over https to leonardoacosta/oryx-with-custom-
#     qmk works using gh's cached credential helper (read access via
#     `git submodule add` already worked; write access is untested).

set +e

# --- Homelab-only guard ----------------------------------------------------
case "$(hostname -s)" in
    homelab) ;;
    *) exit 0 ;;
esac

# --- Config ------------------------------------------------------------
REPO_ROOT="${REPO_ROOT:-$HOME/dev/personal/installfest}"
ZSA_DIR="$REPO_ROOT/apps/zsa-voyager-keymap"
LAYOUT_ID="${ZSA_LAYOUT_ID:-Br7g0}"
LAYOUT_GEOMETRY="${ZSA_LAYOUT_GEOMETRY:-voyager}"
MAC_HOST="${ZSA_MAC_HOST:-mac}"
STATE_DIR="$HOME/.local/state"
STATE_FILE="$STATE_DIR/zsa-firmware-build.json"
LOG="$STATE_DIR/if-deploy.log"
mkdir -p "$STATE_DIR"

{
    echo "--- zsa-firmware-build $(date -u +%FT%TZ) ---"

    if [ ! -d "$ZSA_DIR/.git" ]; then
        echo "err: $ZSA_DIR not initialized — run: git -C \"$REPO_ROOT\" submodule update --init apps/zsa-voyager-keymap"
        exit 0
    fi
    for tool in curl jq unzip docker git ssh scp; do
        command -v "$tool" >/dev/null 2>&1 || { echo "skip: $tool not installed"; exit 0; }
    done

    # --- 1. Fetch latest layout revision from Oryx's GraphQL API --------
    QUERY='query getLayout($hashId: String!, $revisionId: String!, $geometry: String) {layout(hashId: $hashId, geometry: $geometry, revisionId: $revisionId) {  revision { hashId, qmkVersion, title }}}'
    PAYLOAD=$(jq -cn --arg q "$QUERY" --arg h "$LAYOUT_ID" --arg g "$LAYOUT_GEOMETRY" \
        '{query: $q, variables: {hashId: $h, geometry: $g, revisionId: "latest"}}')
    RESPONSE=$(curl -sf --location 'https://oryx.zsa.io/graphql' \
        --header 'Content-Type: application/json' \
        --data "$PAYLOAD" | jq '.data.layout.revision | [.hashId, .qmkVersion, .title]' 2>/dev/null)
    if [ -z "$RESPONSE" ] || [ "$RESPONSE" = "null" ]; then
        echo "err: Oryx GraphQL fetch failed (network? layout not public? bad layout ID $LAYOUT_ID?)"
        exit 0
    fi
    REVISION_HASH=$(echo "$RESPONSE" | jq -r '.[0]')
    FIRMWARE_VERSION=$(printf '%.0f' "$(echo "$RESPONSE" | jq -r '.[1]')" 2>/dev/null)
    CHANGE_DESC=$(echo "$RESPONSE" | jq -r '.[2]')
    [ -z "$CHANGE_DESC" ] || [ "$CHANGE_DESC" = "null" ] && CHANGE_DESC="latest layout modification made with Oryx"

    LAST_REVISION_HASH=""
    [ -f "$STATE_FILE" ] && LAST_REVISION_HASH=$(jq -r '.revision_hash // empty' "$STATE_FILE" 2>/dev/null)
    if [ "$REVISION_HASH" = "$LAST_REVISION_HASH" ]; then
        echo "skip: no new Oryx revision ($REVISION_HASH)"
        exit 0
    fi
    echo "new Oryx revision: ${LAST_REVISION_HASH:-<none>} -> $REVISION_HASH ($CHANGE_DESC)"

    # --- 2. Download + unpack the layout source --------------------------
    SOURCE_ZIP=$(mktemp /tmp/zsa-source.XXXXXX.zip)
    if ! curl -sfL "https://oryx.zsa.io/source/$REVISION_HASH" -o "$SOURCE_ZIP"; then
        echo "err: source download failed for revision $REVISION_HASH"
        rm -f "$SOURCE_ZIP"
        exit 0
    fi
    LAYOUT_DIR="$ZSA_DIR/$LAYOUT_ID"
    rm -rf "$LAYOUT_DIR"
    mkdir -p "$LAYOUT_DIR"
    unzip -oj "$SOURCE_ZIP" '*_source/*' -d "$LAYOUT_DIR" >/dev/null 2>&1
    rm -f "$SOURCE_ZIP"

    # --- 3. Commit fetched layout on oryx branch, merge into main --------
    (
        cd "$ZSA_DIR" || exit 1
        git checkout oryx >/dev/null 2>&1 || git checkout -b oryx >/dev/null 2>&1
        git add "$LAYOUT_ID"
        git -c user.name="zsa-firmware-build" -c user.email="zsa-firmware-build@local" \
            commit -m "oryx: $CHANGE_DESC" >/dev/null 2>&1 || echo "no layout change to commit on oryx"
        git push origin oryx >/dev/null 2>&1 || echo "warn: push to oryx branch failed"

        git checkout main >/dev/null 2>&1
        git pull --ff-only origin main >/dev/null 2>&1
        git merge -X ignore-all-space oryx -m "merge oryx layout into main" >/dev/null 2>&1
        git push origin main >/dev/null 2>&1 || echo "warn: push to main branch failed"
    )

    # --- 4. Sync qmk_firmware submodule to the matching firmware branch --
    (
        cd "$ZSA_DIR" || exit 1
        git submodule update --init --remote --depth=1 --no-single-branch qmk_firmware >/dev/null 2>&1
        cd qmk_firmware || exit 1
        git fetch origin "firmware${FIRMWARE_VERSION}" --depth=1 >/dev/null 2>&1
        git checkout -B "firmware${FIRMWARE_VERSION}" "origin/firmware${FIRMWARE_VERSION}" >/dev/null 2>&1
        git submodule update --init --recursive >/dev/null 2>&1
    )
    (
        cd "$ZSA_DIR" || exit 1
        git add qmk_firmware
        git -c user.name="zsa-firmware-build" -c user.email="zsa-firmware-build@local" \
            commit -m "qmk: update firmware submodule to firmware${FIRMWARE_VERSION}" >/dev/null 2>&1 \
            || echo "no qmk_firmware change to commit"
        git push origin main >/dev/null 2>&1 || echo "warn: push to main branch failed (qmk_firmware bump)"
    )

    # --- 5. Build (docker image + qmk compile) ----------------------------
    if ! docker build -t qmk "$ZSA_DIR" >/dev/null 2>&1; then
        echo "err: docker build failed"
        exit 0
    fi

    if [ "${FIRMWARE_VERSION:-0}" -ge 24 ] 2>/dev/null; then
        KEYBOARD_DIR="qmk_firmware/keyboards/zsa"
        MAKE_PREFIX="zsa/"
    else
        KEYBOARD_DIR="qmk_firmware/keyboards"
        MAKE_PREFIX=""
    fi
    KEYMAPS_DIR="${ZSA_DIR}/${KEYBOARD_DIR}/${LAYOUT_GEOMETRY}/keymaps"
    rm -rf "${KEYMAPS_DIR:?}/${LAYOUT_ID}"
    mkdir -p "$KEYMAPS_DIR"
    cp -r "$LAYOUT_DIR" "${KEYMAPS_DIR}/${LAYOUT_ID}"

    docker run -v "${ZSA_DIR}/qmk_firmware:/root" --rm qmk /bin/sh -c "
        qmk setup zsa/qmk_firmware -b firmware${FIRMWARE_VERSION} -y
        make ${MAKE_PREFIX}${LAYOUT_GEOMETRY}:${LAYOUT_ID}
    " >/dev/null 2>&1

    NORMALIZED_GEOMETRY=$(printf '%s' "$LAYOUT_GEOMETRY" | tr '/' '_')
    BUILT_FILE=$(find "${ZSA_DIR}/qmk_firmware" -maxdepth 1 -type f -regex ".*${NORMALIZED_GEOMETRY}.*\.\(bin\|hex\)$" 2>/dev/null | head -1)
    if [ -z "$BUILT_FILE" ]; then
        echo "err: build did not produce a .bin/.hex — check docker output manually (docker run without the >/dev/null redirect)"
        exit 0
    fi
    echo "built: $BUILT_FILE"

    # --- 6. Ship it to the Mac's Downloads over the SSH mesh -------------
    ARTIFACT_NAME="${NORMALIZED_GEOMETRY}_${LAYOUT_ID}_$(basename "$BUILT_FILE")"
    if scp -q "$BUILT_FILE" "${MAC_HOST}:~/Downloads/${ARTIFACT_NAME}" 2>/dev/null; then
        echo "shipped to ${MAC_HOST}:~/Downloads/${ARTIFACT_NAME}"
        # Hand off to the Mac-side notify/poll flow, passing the exact
        # file so it doesn't have to guess which .bin is new.
        ssh "$MAC_HOST" "REPO_ROOT=\$HOME/dev/personal/installfest \$HOME/dev/personal/installfest/scripts/hooks/zsa-firmware-check.sh \$HOME/Downloads/${ARTIFACT_NAME}" >/dev/null 2>&1 &
        disown 2>/dev/null || true
    else
        echo "err: scp to $MAC_HOST failed"
    fi

    # --- 7. Record state so the same revision doesn't rebuild every commit --
    jq -n --arg h "$REVISION_HASH" --arg t "$(date -u +%FT%TZ)" \
        '{revision_hash: $h, built_at: $t}' > "$STATE_FILE" 2>/dev/null

    echo "=== zsa-firmware-build done ==="
} >> "$LOG" 2>&1

exit 0
