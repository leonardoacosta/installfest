#!/usr/bin/env bash
# run_once_set-git-identity.sh — pin global git identity idempotently.
#
# ~/.gitconfig is INTENTIONALLY NOT chezmoi-managed (see
# run_onchange_after_configure-git-azure.sh, which appends the Azure DevOps
# credential-broker + proxy block via `git config --global`). A static
# chezmoi-managed gitconfig collides with that append and re-prompts
# "changed since chezmoi last wrote it" on every apply (the 2026-06-14
# regression). So we set identity the same idiomatic way — additive
# `git config --global`, which merges into ~/.gitconfig without owning the
# whole file. Fixes the restore gap where the Mac committed as
# leonardoacosta@<Hostname>.local with no identity configured.

set -uo pipefail

git config --global user.name "leonardoacosta"
git config --global user.email "leo@priceless.dev"
git config --global init.defaultBranch main
git config --global pull.rebase true

echo "set-git-identity: pinned leonardoacosta <leo@priceless.dev>"
