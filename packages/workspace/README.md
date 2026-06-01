# workspace (`wk`) — org-scoped environment & workspace management

Single home for routing dev work by **org category** (`b-and-b` / `priceless` / `personal`).
Resolves a repo to its workspace, activates the right env + shell wrappers + Claude Code config,
and keeps it all in sync across machines via chezmoi.

## Layout

```
packages/workspace/
  bin/
    wk                  umbrella dispatcher (git-style: wk <name> -> wk-<name> on PATH)
    wk-ready            portfolio "ready work" — dispatches per profile.toml tracker
    wsenv               resolver + activator (code/cwd -> org; emits env/PATH or claude flags)
    ws-claude           launch Claude with org profile inside a persistent zellij session
    generate-profiles   generator: reads the registry, scaffolds packages/workspace/profiles/<org>/
  lib/
    trackers/           per-tracker adapters: beads-ready, ado-ready, none-ready (+ README)
  profiles/<org>/
    profile.toml        tracker config (consumed by wk-ready)
    env.sh              sourced at activation by wsenv
    claude/, wrappers/  scaffold dirs (--add-dir + PATH overlay)
  integrations/         consumer glue (cmux, etc.)
  README.md
```

### Subcommand convention

The `wk` dispatcher follows the git / kubectl / gh pattern: any executable named
`wk-<subcommand>` on PATH is reachable as `wk <subcommand>`. There is no central
registry — new subcommands appear the moment they land on PATH. Run `wk` (or
`wk --list`) to see what's discovered. Today: `ready`.

## How it deploys (both machines, in sync)

- `bin/wsenv` is symlinked onto PATH by chezmoi: `home/dot_local/bin/symlink_wsenv.tmpl`
  → `~/.local/bin/wsenv` → `~/dev/if/packages/workspace/bin/wsenv`. `~/.local/bin` is already
  on PATH (`.zshenv`), so bare `wsenv` works.
- `bin/generate-profiles` runs on `chezmoi apply` via
  `home/run_onchange_after_generate-workspace-profiles.sh.tmpl` (hash-pinned to `projects.toml`).
- `sourceDir = ~/dev/if` on BOTH machines + the `post-merge` → `chezmoi apply` hook means a
  `git pull` regenerates everything locally. No SSH coordination, no per-machine manual step.

## Registry (source of truth)

Currently `~/dev/if/home/projects.toml` (the `category` field), read in-place — it is also consumed
by generate-raycast.sh / cmux-workspaces.sh / mux-remote.sh, so it stays there for now.
**Convergence (deferred):** fold cc's `projects.json` (deploy fields) in, and let cc derive from
this registry — IF becomes the single source of truth.

## Usage

```bash
# Environment activation (existing)
wsenv --org ws              # -> b-and-b
wsenv --list                # all code -> org mappings
eval "$(wsenv ws)"          # activate b-and-b in this shell (env + wrappers PATH)
claude $(wsenv --flags ws)  # launch claude with the org's CC profile flags

# Portfolio surface (new — W1)
wk                          # list discovered subcommands
wk ready priceless          # 60 ready beads issues across oo/tc/ss/ct/mv/tl
wk ready personal           # 73 ready beads issues across the personal portfolio
wk ready b-and-b            # ADO work items (requires az devops login + project_id)
wk ready --table priceless  # column-aligned PRI/ID/TITLE/PROJECT
wk ready                    # resolves org from $PWD via wsenv
```

## Per-org profiles (generated)

`~/.config/workspace/<org>/` — `env.sh` (sourced), `wrappers/` (prepended to PATH),
`claude/` (`--add-dir` skills), plus `mcp.json` / `prompt.txt` when present. Machine-local
output (not committed); regenerated from the registry.

- **b-and-b:** `AZURE_CONFIG_DIR=~/.azure-bbadmin` + the SOCKS-proxy `az` wrapper.
- **priceless / personal:** native `az` (global), no org overlay yet.

## Consumers

- `scripts/cmux-workspaces.sh` calls `wsenv` at pane spawn (activates env + launches claude with flags).
- Future: tmux/zellij persistent-session wiring (so CC sessions survive SSH disconnect).
