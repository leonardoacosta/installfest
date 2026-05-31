# workspace (`wk`) — org-scoped environment & workspace management

Single home for routing dev work by **org category** (`b-and-b` / `priceless` / `personal`).
Resolves a repo to its workspace, activates the right env + shell wrappers + Claude Code config,
and keeps it all in sync across machines via chezmoi.

## Layout

```
packages/workspace/
  bin/wsenv             resolver + activator (code/cwd -> org; emits env/PATH or claude flags)
  bin/generate-profiles generator: reads the registry, emits ~/.config/workspace/<org>/
  integrations/         consumer glue (cmux, etc.)
  README.md
```

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
wsenv --org ws            # -> b-and-b
wsenv --list              # all code -> org mappings
eval "$(wsenv ws)"        # activate b-and-b in this shell (env + wrappers PATH)
claude $(wsenv --flags ws)  # launch claude with the org's CC profile flags
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
