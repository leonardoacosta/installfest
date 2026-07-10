#!/bin/sh
#
# remote-apply.sh — runs on a deploy target after `git reset --hard origin/main`.
#
# Single source of truth for the post-pull side of the deploy chain:
#   1. chezmoi apply (deploys dotfiles)
#   2. Diff raycast-scripts/ between pre-deploy SHA and HEAD
#   3. If changed AND on Mac → terminal-notifier nudge (restart Raycast hint)
#
# Args:
#   $1 — pre-deploy git SHA (HEAD before the reset). Optional; no notify if absent.
#
# All output appended to ~/.local/state/if-deploy.log so silent failures stop
# being invisible. Exit 0 always — deploy chain must not abort on notify errors.

set +e
mkdir -p ~/.local/state
LOG="$HOME/.local/state/if-deploy.log"

PRE_SHA="${1:-}"

{
    echo "=== if-deploy $(date -u +%FT%TZ) ==="

    REPO_ROOT="$HOME/dev/personal/installfest"
    if [ ! -d "$REPO_ROOT" ]; then
        echo "ERR: $REPO_ROOT missing — bootstrap required"
        exit 0
    fi
    cd "$REPO_ROOT" || exit 0

    # --- chezmoi init --apply (regenerates config.tmpl cache, then deploys) ---
    # `chezmoi apply` alone re-renders dotfile templates but does NOT re-render
    # .chezmoi.toml.tmpl itself — that cached config (~/.config/chezmoi/chezmoi.toml)
    # only regenerates on `chezmoi init`. A plain `apply`-only hook silently deploys
    # against stale [data] values (e.g. $theme) whenever .chezmoi.toml.tmpl changes,
    # even though `git pull` picked up the new template correctly. Folding init into
    # this call via --apply closes that gap in one step instead of two.
    # --force overrides interactive conflict prompts. Required because some
    # apps mutate their config files at runtime (e.g. Zed writing back
    # extension-install state to settings.json), and an unattended deploy
    # has no tty to answer "diff/overwrite/skip/quit?". Source-controlled
    # config wins on every deploy; if you tweak via the app's own UI, run
    # `chezmoi re-add <path>` to pull the change back into the dotfiles.
    if command -v chezmoi >/dev/null 2>&1; then
        chezmoi init --source="$REPO_ROOT" --apply --no-tty --force
        rc=$?
        echo "chezmoi: exit $rc"
    else
        echo "chezmoi: not installed — skipping apply"
        rc=0
    fi

    # --- Raycast change detection + notification (Mac only) ---
    if [ -n "$PRE_SHA" ] && command -v terminal-notifier >/dev/null 2>&1; then
        CHANGED=$(git diff --name-only "$PRE_SHA" HEAD -- platform/raycast-scripts/ 2>/dev/null | wc -l | tr -d ' ')
        if [ "$CHANGED" -gt 0 ]; then
            echo "raycast: $CHANGED scripts changed — sending terminal-notifier ping"
            terminal-notifier \
                -title "Raycast scripts updated" \
                -message "$CHANGED file(s) changed — restart Raycast if icons look stale" \
                -sender com.raycast.macos \
                -group if-deploy-raycast \
                2>/dev/null || true
        else
            echo "raycast: no script changes in this deploy"
        fi
    fi

    echo "=== exit $rc ==="
} >> "$LOG" 2>&1

exit 0
