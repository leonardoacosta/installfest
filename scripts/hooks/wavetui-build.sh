#!/bin/sh
#
# wavetui-build.sh — reinstall apps/wavetui's binary whenever a commit or
# merge actually touches its source tree. Mirrors zsa-firmware-build.sh's
# state-file gating idiom: record the last commit SHA we built from, diff
# apps/wavetui's paths between that SHA and HEAD, and only rebuild when
# something changed. Runs on any machine with `go` installed (plain local
# build — no Homelab-only gate like the firmware pipeline needs).
#
# Uses `go install` (not `go build -o`) so the single global-PATH copy at
# $(go env GOPATH)/bin/wavetui (~/go/bin, already on PATH — see
# dot_zshenv.tmpl's Go section) is the only artifact and it self-updates on
# every relevant commit — no separate local binary to drift out of sync.

set +e

REPO_ROOT="${REPO_ROOT:-$(git rev-parse --show-toplevel 2>/dev/null)}"
[ -z "$REPO_ROOT" ] && exit 0
WAVETUI_DIR="$REPO_ROOT/apps/wavetui"
[ -d "$WAVETUI_DIR" ] || exit 0

command -v go >/dev/null 2>&1 || exit 0
command -v jq >/dev/null 2>&1 || exit 0

STATE_DIR="$HOME/.local/state"
STATE_FILE="$STATE_DIR/wavetui-build.json"
LOG="$STATE_DIR/if-deploy.log"
mkdir -p "$STATE_DIR"

CURRENT_SHA=$(git -C "$REPO_ROOT" rev-parse HEAD 2>/dev/null)
[ -z "$CURRENT_SHA" ] && exit 0

LAST_SHA=""
[ -f "$STATE_FILE" ] && LAST_SHA=$(jq -r '.built_sha // empty' "$STATE_FILE" 2>/dev/null)

if [ "$CURRENT_SHA" = "$LAST_SHA" ]; then
    exit 0
fi

if [ -n "$LAST_SHA" ] && git -C "$REPO_ROOT" cat-file -e "$LAST_SHA" 2>/dev/null; then
    CHANGED=$(git -C "$REPO_ROOT" diff --name-only "$LAST_SHA" "$CURRENT_SHA" -- apps/wavetui 2>/dev/null)
else
    # No usable baseline (first run ever, or the recorded SHA no longer
    # exists e.g. after a rebase) — treat any existing source as changed
    # so the very first hook run after adoption always builds once.
    CHANGED="apps/wavetui"
fi

if [ -z "$CHANGED" ]; then
    # Nothing under apps/wavetui changed, but advance the recorded SHA
    # anyway so an unrelated commit doesn't force a re-diff from a stale
    # baseline forever.
    jq -n --arg s "$CURRENT_SHA" --arg t "$(date -u +%FT%TZ)" \
        '{built_sha: $s, built_at: $t, skipped: true}' > "$STATE_FILE" 2>/dev/null
    exit 0
fi

{
    echo "--- wavetui-build $(date -u +%FT%TZ) ---"
    echo "rebuilding: ${LAST_SHA:-<none>} -> $CURRENT_SHA"
    if (cd "$WAVETUI_DIR" && go install ./cmd/wavetui) 2>&1; then
        GOBIN_DIR=$(go env GOBIN)
        [ -z "$GOBIN_DIR" ] && GOBIN_DIR="$(go env GOPATH)/bin"
        echo "installed: $GOBIN_DIR/wavetui"
        jq -n --arg s "$CURRENT_SHA" --arg t "$(date -u +%FT%TZ)" \
            '{built_sha: $s, built_at: $t}' > "$STATE_FILE" 2>/dev/null
    else
        echo "err: go build failed — not advancing built_sha, will retry next hook run"
    fi
    echo "=== wavetui-build done ==="
} >> "$LOG" 2>&1

exit 0
