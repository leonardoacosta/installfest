#!/bin/bash
set -euo pipefail

# SSH Key Rotation Script
# Generates a new shared ED25519 key and deploys it to homelab + cloudpc.
# Run from Mac. Uses existing keys to deploy the new one.
#
# Safe order (no lockout window): append the new public key everywhere ->
# verify the NEW key works against every peer -> only then swap private keys
# and prune the old public keys. If any peer fails to verify, nothing
# destructive has happened and the old key stays fully valid.
#
# Usage: rotate-keys.sh [--dry-run]

HOMELAB_USER="nyaptor"
HOMELAB_HOST="homelab"  # SSH alias or 100.73.182.4
CLOUDPC_USER="leo"
CLOUDPC_HOST="100.83.148.5"  # Tailscale IP (no SSH alias assumed)
KEY_COMMENT="leo-mesh-$(date +%Y%m%d)"
NEW_KEY="$HOME/.ssh/id_ed25519_new"

DRY_RUN=false
case "${1:-}" in
  --dry-run) DRY_RUN=true ;;
  "") ;;
  *) echo "usage: $0 [--dry-run]" >&2; exit 2 ;;
esac

# run: execute a mutating command, or narrate it under --dry-run.
# Heredoc-fed `ssh ... bash -s` / powershell blocks can't route through this;
# those are guarded inline with `if $DRY_RUN`.
run() { if $DRY_RUN; then echo "DRY: $*"; else "$@"; fi; }

echo "=== SSH Key Rotation ==="
$DRY_RUN && echo "(dry-run: narrating steps, touching nothing)"
echo ""

# ---------------------------------------------------------------------------
# Phase 1: Generate new key pair
# ---------------------------------------------------------------------------
echo "[1/7] Generating new ED25519 key pair..."
run ssh-keygen -t ed25519 -f "$NEW_KEY" -N "" -C "$KEY_COMMENT"
if $DRY_RUN; then
  NEW_PUB="ssh-ed25519 AAAA...DRYRUN... $KEY_COMMENT"
else
  NEW_PUB=$(cat "${NEW_KEY}.pub")
fi
echo "  New public key: $NEW_PUB"
echo ""

# ---------------------------------------------------------------------------
# Phase 2: Append new key to every peer's authorized_keys (non-destructive)
# Does NOT overwrite and does NOT touch private keys. Old key still valid.
# ---------------------------------------------------------------------------
echo "[2/7] Appending new key to peers' authorized_keys (non-destructive)..."

# Homelab (bash): back up, then append only if the key is not already present.
if $DRY_RUN; then
  echo "DRY: ssh homelab -> backup authorized_keys; append NEW_PUB if absent"
else
  # shellcheck disable=SC2087  # $NEW_PUB is intentionally expanded client-side
  ssh "${HOMELAB_USER}@${HOMELAB_HOST}" bash -s <<REMOTE_APPEND
set -euo pipefail
cp ~/.ssh/authorized_keys ~/.ssh/authorized_keys.bak 2>/dev/null || true
if ! grep -qF '${NEW_PUB}' ~/.ssh/authorized_keys 2>/dev/null; then
  echo '${NEW_PUB}' >> ~/.ssh/authorized_keys
fi
chmod 600 ~/.ssh/authorized_keys
echo "  homelab authorized_keys appended"
REMOTE_APPEND
fi

# CloudPC (powershell): Add-Content (NOT Set-Content) with a Select-String
# dedup guard, on both the user and admin authorized_keys files.
if $DRY_RUN; then
  echo "DRY: ssh cloudpc -> append NEW_PUB to user + admin authorized_keys if absent"
else
  ssh "${CLOUDPC_USER}@${CLOUDPC_HOST}" powershell -Command "
    \$userAuthKeys = 'C:\\Users\\${CLOUDPC_USER}\\.ssh\\authorized_keys'
    New-Item -ItemType Directory -Force -Path (Split-Path \$userAuthKeys) | Out-Null
    if (-not (Test-Path \$userAuthKeys) -or -not (Select-String -Path \$userAuthKeys -SimpleMatch '${NEW_PUB}' -Quiet)) {
      Add-Content -Path \$userAuthKeys -Value '${NEW_PUB}'
    }

    # Admin authorized_keys (required for admin users on Windows OpenSSH)
    \$adminAuthKeys = 'C:\\ProgramData\\ssh\\administrators_authorized_keys'
    if (-not (Test-Path \$adminAuthKeys) -or -not (Select-String -Path \$adminAuthKeys -SimpleMatch '${NEW_PUB}' -Quiet)) {
      Add-Content -Path \$adminAuthKeys -Value '${NEW_PUB}'
    }
    icacls \$adminAuthKeys /inheritance:r /grant 'SYSTEM:(F)' /grant 'Administrators:(F)' | Out-Null

    Write-Output '  cloudpc authorized_keys appended (user + admin)'
  "
fi
echo ""

# ---------------------------------------------------------------------------
# Phase 3: Verify the NEW key against every peer, using -i explicitly so the
# default identity is not consulted (proves the appended key actually works).
# ---------------------------------------------------------------------------
echo "[3/7] Verifying NEW key connectivity to all peers..."
HOMELAB_OK=false
CLOUDPC_OK=false

if $DRY_RUN; then
  echo "DRY: ssh -i $NEW_KEY -o IdentitiesOnly=yes homelab 'echo OK'"
  echo "DRY: ssh -i $NEW_KEY -o IdentitiesOnly=yes cloudpc 'echo OK'"
  HOMELAB_OK=true
  CLOUDPC_OK=true
else
  echo -n "  Homelab (new key): "
  if ssh -i "$NEW_KEY" -o IdentitiesOnly=yes -o ConnectTimeout=5 "${HOMELAB_USER}@${HOMELAB_HOST}" "echo OK" 2>/dev/null; then
    HOMELAB_OK=true
  else
    echo "FAILED"
  fi

  echo -n "  CloudPC (new key): "
  if ssh -i "$NEW_KEY" -o IdentitiesOnly=yes -o ConnectTimeout=5 "${CLOUDPC_USER}@${CLOUDPC_HOST}" "echo OK" 2>/dev/null; then
    CLOUDPC_OK=true
  else
    echo "FAILED"
  fi
fi
echo ""

# ---------------------------------------------------------------------------
# Phase 4: Gate. Nothing destructive has happened yet — if any peer failed to
# verify the new key, abort with the old key still fully valid everywhere.
# ---------------------------------------------------------------------------
if ! { $HOMELAB_OK && $CLOUDPC_OK; }; then
  echo "ABORT: new key did not verify on all peers."
  $HOMELAB_OK || echo "  - homelab: new-key verify FAILED"
  $CLOUDPC_OK || echo "  - cloudpc: new-key verify FAILED"
  echo ""
  echo "Recovery: the old key is still fully valid on all peers. Re-run after"
  echo "fixing connectivity. The new public key was appended (harmless), but no"
  echo "private key was swapped and nothing was removed."
  exit 1
fi

# ---------------------------------------------------------------------------
# Phase 5: Swap private keys (only now that the new key verified everywhere).
# Distribute the new private key to each peer and swap it in, keeping .old.
# ---------------------------------------------------------------------------
echo "[4/7] Swapping private keys (new key verified on all peers)..."

# Homelab
run scp "$NEW_KEY" "${HOMELAB_USER}@${HOMELAB_HOST}:~/.ssh/id_ed25519_new"
run scp "${NEW_KEY}.pub" "${HOMELAB_USER}@${HOMELAB_HOST}:~/.ssh/id_ed25519_new.pub"
if $DRY_RUN; then
  echo "DRY: ssh homelab -> keep id_ed25519.old, move new key into place"
else
  ssh "${HOMELAB_USER}@${HOMELAB_HOST}" bash -s <<'REMOTE_SWAP'
set -euo pipefail
cp ~/.ssh/id_ed25519 ~/.ssh/id_ed25519.old
mv ~/.ssh/id_ed25519_new ~/.ssh/id_ed25519
mv ~/.ssh/id_ed25519_new.pub ~/.ssh/id_ed25519.pub
chmod 600 ~/.ssh/id_ed25519
echo "  homelab private key rotated"
REMOTE_SWAP
fi

# CloudPC
run scp "$NEW_KEY" "${CLOUDPC_USER}@${CLOUDPC_HOST}:C:/Users/LeonardoAcosta/.ssh/id_ed25519_new"
run scp "${NEW_KEY}.pub" "${CLOUDPC_USER}@${CLOUDPC_HOST}:C:/Users/LeonardoAcosta/.ssh/id_ed25519_new.pub"
if $DRY_RUN; then
  echo "DRY: ssh cloudpc -> keep id_ed25519.old, move new key into place"
else
  ssh "${CLOUDPC_USER}@${CLOUDPC_HOST}" powershell -Command "
    \$sshDir = 'C:\\Users\\LeonardoAcosta\\.ssh'
    if (Test-Path \"\$sshDir\\id_ed25519\") {
      Copy-Item \"\$sshDir\\id_ed25519\" \"\$sshDir\\id_ed25519.old\"
    }
    Move-Item -Force \"\$sshDir\\id_ed25519_new\" \"\$sshDir\\id_ed25519\"
    Move-Item -Force \"\$sshDir\\id_ed25519_new.pub\" \"\$sshDir\\id_ed25519.pub\"
    Write-Output '  cloudpc private key rotated'
  "
fi

# Local Mac
run cp "$HOME/.ssh/id_ed25519" "$HOME/.ssh/id_ed25519.old"
run mv "$NEW_KEY" "$HOME/.ssh/id_ed25519"
run mv "${NEW_KEY}.pub" "$HOME/.ssh/id_ed25519.pub"
run chmod 600 "$HOME/.ssh/id_ed25519"
echo "  local Mac private key rotated"
echo ""

# ---------------------------------------------------------------------------
# Phase 6: Re-verify each peer with the now-default swapped identity.
# ---------------------------------------------------------------------------
echo "[5/7] Re-verifying peers with the swapped default identity..."
HOMELAB_REVERIFY=false
CLOUDPC_REVERIFY=false

if $DRY_RUN; then
  echo "DRY: ssh homelab 'echo OK'; ssh cloudpc 'echo OK'"
  HOMELAB_REVERIFY=true
  CLOUDPC_REVERIFY=true
else
  echo -n "  Homelab: "
  if ssh -o ConnectTimeout=5 "${HOMELAB_USER}@${HOMELAB_HOST}" "echo OK" 2>/dev/null; then
    HOMELAB_REVERIFY=true
  else
    echo "FAILED"
  fi

  echo -n "  CloudPC: "
  if ssh -o ConnectTimeout=5 "${CLOUDPC_USER}@${CLOUDPC_HOST}" "echo OK" 2>/dev/null; then
    CLOUDPC_REVERIFY=true
  else
    echo "FAILED"
  fi
fi
echo ""

# ---------------------------------------------------------------------------
# Phase 7: Remove old keys — per peer only where re-verify passed. This is
# where authorized_keys is finally pruned to contain ONLY the new key.
# ---------------------------------------------------------------------------
echo "[6/7] Pruning old keys (only where re-verify passed)..."

# Homelab
if $HOMELAB_REVERIFY; then
  if $DRY_RUN; then
    echo "DRY: ssh homelab -> authorized_keys := NEW_PUB only; rm id_ed25519.old, authorized_keys.bak"
  else
    # shellcheck disable=SC2087  # $NEW_PUB is intentionally expanded client-side
    ssh "${HOMELAB_USER}@${HOMELAB_HOST}" bash -s <<REMOTE_CLEAN
set -euo pipefail
echo '${NEW_PUB}' > ~/.ssh/authorized_keys
chmod 600 ~/.ssh/authorized_keys
rm -f ~/.ssh/id_ed25519.old ~/.ssh/authorized_keys.bak
echo "  homelab: authorized_keys pruned to new key; old key removed"
REMOTE_CLEAN
  fi
else
  echo "  Homelab: SKIP prune (re-verify failed); old key + backups retained"
fi

# CloudPC — prune user + admin authorized_keys to the new key, keep icacls
# hardening on the admin file, then drop the old private key.
if $CLOUDPC_REVERIFY; then
  if $DRY_RUN; then
    echo "DRY: ssh cloudpc -> user+admin authorized_keys := NEW_PUB only; icacls admin; rm id_ed25519.old"
  else
    ssh "${CLOUDPC_USER}@${CLOUDPC_HOST}" powershell -Command "
      Set-Content -Path 'C:\\Users\\${CLOUDPC_USER}\\.ssh\\authorized_keys' -Value '${NEW_PUB}'
      \$adminAuthKeys = 'C:\\ProgramData\\ssh\\administrators_authorized_keys'
      Set-Content -Path \$adminAuthKeys -Value '${NEW_PUB}'
      icacls \$adminAuthKeys /inheritance:r /grant 'SYSTEM:(F)' /grant 'Administrators:(F)' | Out-Null
      Remove-Item -Force -ErrorAction SilentlyContinue 'C:\\Users\\LeonardoAcosta\\.ssh\\id_ed25519.old'
      Write-Output '  cloudpc: authorized_keys pruned to new key; old key removed'
    "
  fi
else
  echo "  CloudPC: SKIP prune (re-verify failed); old key retained"
fi

# Local Mac old key: gated on BOTH peers re-verified — the Mac must stay able
# to reach every peer before it discards its own rollback key.
if $HOMELAB_REVERIFY && $CLOUDPC_REVERIFY; then
  run rm -f "$HOME/.ssh/id_ed25519.old"
  echo "  Mac: old key removed"
else
  echo "  Mac: old key RETAINED (not all peers re-verified — keep rollback)"
fi
echo ""

# ---------------------------------------------------------------------------
# Phase 8: Sync the chezmoi-managed authorized_keys source so `chezmoi apply`
# on any machine never reverts ~/.ssh/authorized_keys to a stale key.
# ---------------------------------------------------------------------------
echo "[7/7] Syncing chezmoi source authorized_keys..."
if $DRY_RUN; then
  echo "DRY: rewrite home/private_dot_ssh/private_authorized_keys key line to NEW_PUB"
else
  cd "$(dirname "$0")/../.."
  printf '%s\n' "$NEW_PUB" >> home/private_dot_ssh/private_authorized_keys.new
  # Preserve the comment header, replace only the key line.
  grep -v '^ssh-' home/private_dot_ssh/private_authorized_keys > home/private_dot_ssh/private_authorized_keys.hdr 2>/dev/null || true
  cat home/private_dot_ssh/private_authorized_keys.hdr <(printf '%s\n' "$NEW_PUB") > home/private_dot_ssh/private_authorized_keys
  rm -f home/private_dot_ssh/private_authorized_keys.new home/private_dot_ssh/private_authorized_keys.hdr
  echo "  chezmoi source: home/private_dot_ssh/private_authorized_keys synced to new key"
fi

# NOTE: rotation does NOT imply git-history surgery. If a private key is ever
# committed, treat it as a dedicated incident (git-filter-repo + a coordinated
# force-push done deliberately), never as a routine step of key rotation.

echo ""
echo "=== Key rotation complete ==="
