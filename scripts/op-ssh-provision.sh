#!/usr/bin/env bash
(return 0 2>/dev/null) || set -uo pipefail  # sourced-lib guard — bare set would leak into callers
# op-ssh-provision.sh — materialize the SSH mesh keypair from 1Password.
#
# Sourced by run_once_install-packages.sh. Replaces the manual
# `scp <source>:~/.ssh/id_ed25519*` step: 1Password becomes the single source
# of truth for the shared mesh key, and `op read` writes it to disk on a fresh
# machine. On-disk keys are kept intentionally — the cloudpc / mx-broker tunnel
# LaunchAgents and the headless homelab node need unattended key access, which
# the interactive 1Password SSH *agent* cannot provide.
#
# PREREQUISITES (one-time):
#   1. Install the 1Password CLI (Brewfile cask "1password-cli").
#   2. 1Password 8 app: Settings > Developer > "Integrate with 1Password CLI".
#   3. Store the mesh keypair as an SSH Key item in 1Password. Default item
#      reference below; override by exporting OP_SSH_ITEM.
#
# Depends on info/success/warning from scripts/utils.sh (already sourced by the
# installer). TTY-guarded so mesh deploys (non-interactive chezmoi apply) skip
# cleanly instead of hanging on the biometric unlock prompt.

# 1Password secret reference for the SSH Key item. The ?ssh-format=openssh query
# makes `op read` emit a valid OpenSSH private key (not the raw PEM/JWK).
: "${OP_SSH_ITEM:=op://Private/mesh-ssh}"

provision_ssh_keys_from_op() {
    local priv="$HOME/.ssh/id_ed25519"
    local pub="$HOME/.ssh/id_ed25519.pub"

    # Already on disk? nothing to fetch (idempotent).
    if [ -f "$priv" ]; then
        success "SSH key already present ($priv) — skipping 1Password fetch."
        return 0
    fi

    command -v op >/dev/null 2>&1 || {
        warning "op (1Password CLI) not installed — cannot fetch the mesh key."
        return 0
    }
    # Biometric unlock needs a terminal; never block a non-interactive deploy.
    [ -t 0 ] || {
        warning "Non-interactive shell — skipping 1Password SSH key fetch."
        return 0
    }
    if ! op whoami >/dev/null 2>&1; then
        info "Signing in to the 1Password CLI (approve in the app / Touch ID)..."
        op signin >/dev/null 2>&1 || {
            warning "op signin failed — skipping. Enable Settings > Developer > Integrate with 1Password CLI."
            return 0
        }
    fi

    mkdir -p "$HOME/.ssh"
    chmod 700 "$HOME/.ssh"

    if op read "${OP_SSH_ITEM}/private key?ssh-format=openssh" >"$priv" 2>/dev/null && [ -s "$priv" ]; then
        chmod 600 "$priv"
        if op read "${OP_SSH_ITEM}/public key" >"$pub" 2>/dev/null && [ -s "$pub" ]; then
            chmod 644 "$pub"
        else
            # Derive the public key from the private one if the item lacks the field.
            ssh-keygen -y -f "$priv" >"$pub" 2>/dev/null && chmod 644 "$pub"
        fi
        success "SSH mesh key materialized from 1Password (${OP_SSH_ITEM})."
    else
        rm -f "$priv"
        warning "Could not read ${OP_SSH_ITEM} from 1Password — falling back to manual key copy."
    fi
}
