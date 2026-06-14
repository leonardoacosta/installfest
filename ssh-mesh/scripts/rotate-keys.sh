#!/bin/bash
set -euo pipefail

# SSH Key Rotation Script
# Generates a new shared ED25519 key and deploys it to homelab + cloudpc
# Run from Mac. Uses existing (compromised) keys to deploy the new ones.

HOMELAB_USER="nyaptor"
HOMELAB_HOST="homelab"  # SSH alias or 100.73.182.4
CLOUDPC_USER="leo"
CLOUDPC_HOST="100.83.148.5"  # Tailscale IP (no SSH alias assumed)
KEY_COMMENT="leo-mesh-$(date +%Y%m%d)"
NEW_KEY="$HOME/.ssh/id_ed25519_new"

echo "=== SSH Key Rotation ==="
echo ""

# Step 1: Generate new key pair
echo "[1/5] Generating new ED25519 key pair..."
ssh-keygen -t ed25519 -f "$NEW_KEY" -N "" -C "$KEY_COMMENT"
NEW_PUB=$(cat "${NEW_KEY}.pub")
echo "  New public key: $NEW_PUB"
echo ""

# Step 2: Deploy to homelab
echo "[2/5] Deploying new key to homelab ($HOMELAB_HOST)..."
ssh "${HOMELAB_USER}@${HOMELAB_HOST}" bash -s <<REMOTE_HOMELAB
set -euo pipefail
# Backup old authorized_keys
cp ~/.ssh/authorized_keys ~/.ssh/authorized_keys.bak 2>/dev/null || true
# Write new public key (replaces old)
echo '${NEW_PUB}' > ~/.ssh/authorized_keys
chmod 600 ~/.ssh/authorized_keys
echo "  authorized_keys updated"
REMOTE_HOMELAB

# Copy new private key to homelab for outbound connections
scp "$NEW_KEY" "${HOMELAB_USER}@${HOMELAB_HOST}:~/.ssh/id_ed25519_new"
scp "${NEW_KEY}.pub" "${HOMELAB_USER}@${HOMELAB_HOST}:~/.ssh/id_ed25519_new.pub"
ssh "${HOMELAB_USER}@${HOMELAB_HOST}" bash -s <<REMOTE_HOMELAB2
set -euo pipefail
cp ~/.ssh/id_ed25519 ~/.ssh/id_ed25519.old
mv ~/.ssh/id_ed25519_new ~/.ssh/id_ed25519
mv ~/.ssh/id_ed25519_new.pub ~/.ssh/id_ed25519.pub
chmod 600 ~/.ssh/id_ed25519
echo "  private key rotated"
REMOTE_HOMELAB2
echo "  Homelab done."
echo ""

# Step 3: Deploy to CloudPC
echo "[3/5] Deploying new key to CloudPC ($CLOUDPC_HOST)..."
ssh "${CLOUDPC_USER}@${CLOUDPC_HOST}" powershell -Command "
  # User authorized_keys
  \$userAuthKeys = 'C:\\Users\\${CLOUDPC_USER}\\.ssh\\authorized_keys'
  New-Item -ItemType Directory -Force -Path (Split-Path \$userAuthKeys) | Out-Null
  Set-Content -Path \$userAuthKeys -Value '${NEW_PUB}'

  # Admin authorized_keys (required for admin users on Windows OpenSSH)
  \$adminAuthKeys = 'C:\\ProgramData\\ssh\\administrators_authorized_keys'
  Set-Content -Path \$adminAuthKeys -Value '${NEW_PUB}'
  icacls \$adminAuthKeys /inheritance:r /grant 'SYSTEM:(F)' /grant 'Administrators:(F)' | Out-Null

  Write-Output '  authorized_keys updated (user + admin)'
"

# Copy new private key to CloudPC for outbound connections
scp "$NEW_KEY" "${CLOUDPC_USER}@${CLOUDPC_HOST}:C:/Users/LeonardoAcosta/.ssh/id_ed25519_new"
scp "${NEW_KEY}.pub" "${CLOUDPC_USER}@${CLOUDPC_HOST}:C:/Users/LeonardoAcosta/.ssh/id_ed25519_new.pub"
ssh "${CLOUDPC_USER}@${CLOUDPC_HOST}" powershell -Command "
  \$sshDir = 'C:\\Users\\LeonardoAcosta\\.ssh'
  if (Test-Path \"\$sshDir\\id_ed25519\") {
    Copy-Item \"\$sshDir\\id_ed25519\" \"\$sshDir\\id_ed25519.old\"
  }
  Move-Item -Force \"\$sshDir\\id_ed25519_new\" \"\$sshDir\\id_ed25519\"
  Move-Item -Force \"\$sshDir\\id_ed25519_new.pub\" \"\$sshDir\\id_ed25519.pub\"
  Write-Output '  private key rotated'
"
echo "  CloudPC done."
echo ""

# Step 4: Rotate local (Mac) key
echo "[4/5] Rotating local Mac key..."
cp "$HOME/.ssh/id_ed25519" "$HOME/.ssh/id_ed25519.old"
mv "$NEW_KEY" "$HOME/.ssh/id_ed25519"
mv "${NEW_KEY}.pub" "$HOME/.ssh/id_ed25519.pub"
chmod 600 "$HOME/.ssh/id_ed25519"
echo "  Local key rotated."
echo ""

# Step 5: Verify and clean up
echo "[5/5] Verifying connections with new key..."
HOMELAB_OK=false
CLOUDPC_OK=false

echo -n "  Homelab: "
if ssh -o ConnectTimeout=5 "${HOMELAB_USER}@${HOMELAB_HOST}" "echo OK" 2>/dev/null; then
  HOMELAB_OK=true
else
  echo "FAILED"
fi

echo -n "  CloudPC: "
if ssh -o ConnectTimeout=5 "${CLOUDPC_USER}@${CLOUDPC_HOST}" "echo OK" 2>/dev/null; then
  CLOUDPC_OK=true
else
  echo "FAILED"
fi
echo ""

# Clean up old keys on verified machines
echo "Cleaning up old keys..."

rm -f "$HOME/.ssh/id_ed25519.old"
echo "  Mac: old key removed"

if $HOMELAB_OK; then
  ssh "${HOMELAB_USER}@${HOMELAB_HOST}" "rm -f ~/.ssh/id_ed25519.old ~/.ssh/authorized_keys.bak" 2>/dev/null
  echo "  Homelab: old keys removed"
fi

if $CLOUDPC_OK; then
  ssh "${CLOUDPC_USER}@${CLOUDPC_HOST}" powershell -Command "
    Remove-Item -Force -ErrorAction SilentlyContinue 'C:\Users\LeonardoAcosta\.ssh\id_ed25519.old'
  " 2>/dev/null
  echo "  CloudPC: old key removed"
fi

# Scrub compromised key from git history
echo ""
echo "Scrubbing old key from git history..."
cd "$(dirname "$0")/../.."

# Sync the chezmoi-managed authorized_keys source so `chezmoi apply` on any
# machine never reverts ~/.ssh/authorized_keys to a stale key after rotation.
printf '%s\n' "$NEW_PUB" >> home/private_dot_ssh/private_authorized_keys.new
# Preserve the comment header, replace only the key line.
grep -v '^ssh-' home/private_dot_ssh/private_authorized_keys > home/private_dot_ssh/private_authorized_keys.hdr 2>/dev/null || true
cat home/private_dot_ssh/private_authorized_keys.hdr <(printf '%s\n' "$NEW_PUB") > home/private_dot_ssh/private_authorized_keys
rm -f home/private_dot_ssh/private_authorized_keys.new home/private_dot_ssh/private_authorized_keys.hdr
echo "  chezmoi source: home/private_dot_ssh/private_authorized_keys synced to new key"
if command -v bfg &>/dev/null; then
  bfg --replace-text <(echo 'b3BlbnNzaC1rZXktdjEA') . --no-blob-protection
  git reflog expire --expire=now --all
  git gc --prune=now --aggressive
  git push --force
  echo "  Git history scrubbed and force-pushed."
else
  brew install bfg
  bfg --replace-text <(echo 'b3BlbnNzaC1rZXktdjEA') . --no-blob-protection
  git reflog expire --expire=now --all
  git gc --prune=now --aggressive
  git push --force
  echo "  Git history scrubbed and force-pushed."
fi

echo ""
echo "=== Key rotation complete ==="
