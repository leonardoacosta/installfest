#!/usr/bin/env bash
# =============================================================================
# ideopen — open Linux files/workspaces in a Mac IDE over Tailscale.
#
# EDIT-family (see docs/open-family.md) — portal-unaware by design: you
# always want the real source file, never a rendered portal copy. Sibling to
# the VIEW-family ropen/sopen/gopen/mopen (which serve files to a Mac
# BROWSER instead). Opens files in a Mac IDE via the IDE's Remote-SSH back
# to this box:
#
#   vopen  -> VS Code   (code   --remote ssh-remote+<this-box> ...)
#   zopen  -> Zed       (zed    ssh://<this-box><path> ...)
#
# NOTE: `copen` (Cursor) is NOT dispatched from here — it has its own
# canonical, registry-aware implementation at scripts/copen semantics (see
# home/dot_local/bin/executable_copen), which resolves a registered project
# (cloning it on the Mac if missing) rather than a bare file/dir path. This
# script intentionally only covers the two editors that don't have that
# richer registry-project behavior.
#
# Invoked by basename — symlink vopen/zopen -> ideopen.sh.
#
# Usage:  vopen <dir|workspace>      open a folder / .code-workspace in VS Code
#         zopen <file...|dir>        open in Zed
#
# Mac SSH alias:        $IDEOPEN_MAC   (default: mac)
# This box's address    $IDEOPEN_REMOTE (default: resolved from Tailscale; the
# the Mac connects to:                   Mac must be able to ssh here — it can)
# =============================================================================
set -uo pipefail

SELF="$(basename "$0")"
MAC="${IDEOPEN_MAC:-mac}"

# --- this box's remote authority (what the Mac's IDE ssh's back to) ----------
resolve_remote() {
  local r="${IDEOPEN_REMOTE:-}"
  if [[ -z "$r" ]]; then
    r="$(tailscale status --self --json 2>/dev/null \
      | python3 -c 'import sys,json;print(json.load(sys.stdin)["Self"]["DNSName"].rstrip("."))' 2>/dev/null || true)"
  fi
  [[ -z "$r" ]] && r="$(tailscale ip -4 2>/dev/null | head -1)"
  [[ -z "$r" ]] && r="$(hostname -I 2>/dev/null | awk '{print $1}')"
  [[ "$r" == *@* ]] || r="$(whoami)@$r"   # ensure user@host
  printf '%s' "$r"
}

case "$SELF" in
  vopen) CLI=code; KIND=vscode ;;
  zopen) CLI=zed;  KIND=zed ;;
  copen)
    echo "ideopen: 'copen' has its own canonical implementation — see home/dot_local/bin/executable_copen (registry-project-aware, not this script)." >&2
    exit 1 ;;
  *) echo "ideopen: symlink me as vopen | zopen" >&2; exit 1 ;;
esac

DRY=0; ARGS=()
for a in "$@"; do
  case "$a" in
    -n|--dry-run) DRY=1 ;;
    -h|--help) echo "usage: $SELF [-n] <file...|dir|workspace>"; exit 0 ;;
    *) ARGS+=("$a") ;;
  esac
done
[[ ${#ARGS[@]} -eq 0 ]] && { echo "usage: $SELF <file...|dir|workspace>" >&2; exit 1; }

REMOTE="$(resolve_remote)"

# absolute paths on THIS box
declare -a ABS=()
for a in "${ARGS[@]}"; do ABS+=("$(realpath -m -- "$a")"); done

sq() { printf "'%s'" "${1//\'/\'\\\'\'}"; }   # single-quote-escape for the remote shell

# Prepend the common CLI locations so we don't depend on the Mac's login PATH.
PRE='export PATH="$HOME/.local/bin:/opt/homebrew/bin:/usr/local/bin:$PATH";'

if [[ "$KIND" == vscode ]]; then
  RCMD="$PRE $CLI --remote $(sq "ssh-remote+${REMOTE}")"
  for p in "${ABS[@]}"; do RCMD+=" $(sq "$p")"; done
else
  RCMD="$PRE $CLI"
  for p in "${ABS[@]}"; do RCMD+=" $(sq "ssh://${REMOTE}${p}")"; done
fi

if [[ "$DRY" == 1 ]]; then
  echo "ssh $MAC -> $RCMD"
  exit 0
fi

# fire-and-forget; capture stderr for post-mortem
if ssh -o ConnectTimeout=6 -o StrictHostKeyChecking=no "$MAC" "$RCMD" >/tmp/ideopen.log 2>&1; then
  echo "$SELF -> $MAC : $CLI  (remote ssh-remote+${REMOTE})"
  for p in "${ABS[@]}"; do echo "   $p"; done
else
  echo "$SELF: dispatch to '$MAC' failed (see /tmp/ideopen.log)" >&2
  tail -3 /tmp/ideopen.log >&2
  exit 1
fi
