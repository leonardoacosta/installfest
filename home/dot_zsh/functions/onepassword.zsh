#!/usr/bin/env zsh
# onepassword.zsh -- opt-in 1Password Environments loader for an interactive session.
#
# WHAT THIS IS
#   `opsh` ("1Password shell") opens a NEW shell with your cross-project secrets
#   loaded from a 1Password "Environment" that the desktop app mounts as a UNIX
#   named pipe (FIFO). The keys live only in that shell's process memory and its
#   children, and vanish when you `exit`. Nothing is ever written to disk.
#
# WHERE IT RUNS
#   On demand, when YOU type `opsh` in an interactive shell. It is deliberately
#   NOT auto-run from .zshenv/.zshrc: auto-sourcing the FIFO from every shell
#   would race the single-reader pipe across concurrent terminals and hang
#   non-interactive shells (scripts, cron, CI) whenever 1Password is locked.
#
# PREREQUISITE (one-time, in the 1Password 8 desktop app)
#   Settings > Developer > "Show 1Password Developer experience"
#   Developer > View Environments > New environment "shell-global"
#     -> add the cross-project keys you want (e.g. GH_TOKEN, VERCEL_TOKEN, OPENAI_API_KEY)
#   Destinations > Local .env file > mount it at the path below ($OPSH_ENV_FILE).
#   NOTE: do NOT mount at ~/.env -- .zshrc eager-sources that path in every
#   interactive shell, which would race the single-reader FIFO. Use the default below.
#
# LIFESPAN
#   - Seeded values: live at rest in the Environment until you edit them.
#       (Values are COPIES; rotating a key in 1Password means re-seeding it here.)
#   - Loaded keys:   live ONLY inside the `opsh` shell + its children, in memory,
#       from `opsh` until `exit`. Never on disk.
#   - Auth:          one Touch ID prompt per locked->unlocked transition;
#       silent while 1Password stays unlocked.

opsh() {
  emulate -L zsh

  # Mount path. Override per-machine by exporting OPSH_ENV_FILE before calling.
  local pipe="${OPSH_ENV_FILE:-$HOME/.config/op/shell.env}"

  # GUARD: the mount must exist as a FIFO. Fail loud rather than hang.
  if [[ ! -p "$pipe" ]]; then
    print -u2 "opsh: no 1Password mount at '$pipe'."
    print -u2 "      Open 1Password, unlock it, and mount the 'shell-global' Environment there."
    print -u2 "      (Settings > Developer > Environments > shell-global > Local .env file)"
    return 1
  fi

  print -u2 "opsh: loading secrets from 1Password (approve Touch ID if prompted)..."

  # Read the FIFO EXACTLY ONCE inside a subshell, then replace that subshell with
  # a fresh interactive shell that inherits the exported vars. The subshell keeps
  # the secrets OUT of your original (parent) shell -- `exit` returns you to a
  # clean, secret-free prompt.
  (
    set -a
    if ! source "$pipe"; then
      print -u2 "opsh: failed to read the mount (is 1Password unlocked?)."
      exit 1
    fi
    set +a
    print -u2 "opsh: secrets loaded into THIS shell only. Type 'exit' to discard them."
    exec "${SHELL:-zsh}"
  )
}
