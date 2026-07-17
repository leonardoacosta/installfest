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
    wk-doctor           provenance inspector — what config is active here + which layer set it
    wsenv               resolver + activator (code/cwd -> org; emits env/PATH or claude flags)
    ws-claude           launch Claude with org profile inside a persistent zellij session
    generate-profiles   generator: reads the registry, scaffolds packages/workspace/profiles/<org>/
  lib/
    trackers/           per-tracker adapters: beads-ready, ado-ready, none-ready (+ README)
  profiles/<org>/       COMMITTED profile tree — ~/.config/workspace/<org> symlinks here
    profile.toml        tracker config (consumed by wk-ready)
    env.sh              portable env, sourced at activation by wsenv (+ overlay tail)
    claude/, wrappers/  --add-dir target + PATH overlay (wrappers/az -> executable_az)
    plugin/             org agents+skills bundle (--plugin-dir); agents/skills are
                        relative symlinks into ~/dev/cc, plugin.json is committed
    settings.json       installed-plugin enablement overlay (--settings; b-and-b only)
  integrations/         consumer glue (cmux, etc.)
  README.md
```

### Subcommand convention

The `wk` dispatcher follows the git / kubectl / gh pattern: any executable named
`wk-<subcommand>` on PATH is reachable as `wk <subcommand>`. There is no central
registry — new subcommands appear the moment they land on PATH. Run `wk` (or
`wk --list`) to see what's discovered. Today: `ready`, `doctor`.

## How it deploys (both machines, in sync)

- `bin/wsenv` is symlinked onto PATH by chezmoi: `home/dot_local/bin/symlink_wsenv.tmpl`
  → `~/.local/bin/wsenv` → `~/dev/personal/installfest/packages/workspace/bin/wsenv`. `~/.local/bin` is already
  on PATH (`.zshenv`), so bare `wsenv` works.
- `profiles/<org>/` are **committed dirs**; chezmoi symlinks each into place via
  `home/dot_config/workspace/symlink_<org>.tmpl` → `~/.config/workspace/<org>`. The live file IS
  the repo file — edit in place, review in git. `bin/generate-profiles` is a **scaffolder**: it
  creates a skeleton + symlink template for a *new* org category only (run it by hand when adding
  one); it never writes content into `~/.config` (the pre-rehome model did, and clobbered the
  symlink every apply — retired 2026-07-05).
- Machine-coupled bits (the SOCKS/cloudpc tunnel ensure-block) live in a chezmoi-**rendered**
  overlay `home/dot_config/workspace-local/<org>/env.local.sh.tmpl` → `~/.config/workspace-local/<org>/env.local.sh`
  (OS-branched `systemctl`/`launchctl`), sourced transitively by the committed `env.sh`.
- `sourceDir = ~/dev/personal/installfest` on BOTH machines + the `post-merge` → `chezmoi apply` hook means a
  `git pull` redeploys the symlinks + overlay locally. No SSH coordination, no per-machine manual step.

## Registry (source of truth)

Currently `~/dev/personal/installfest/home/projects.toml` (the `category` field), read in-place — it is also consumed
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

wk                          # list discovered subcommands
wk ready priceless          # 60 ready beads issues across oo/tc/ss/ct/mv/tl
wk ready personal           # 73 ready beads issues across the personal portfolio
wk ready b-and-b            # ADO work items (requires az devops login + project_id)
wk ready --table priceless  # column-aligned PRI/ID/TITLE/PROJECT
wk ready                    # resolves org from $PWD via wsenv

# Provenance inspection (new)
wk doctor                   # what config is active in $PWD + which layer set it
wk doctor ws                # inspect a specific repo's org (b-and-b)
wk doctor --json            # machine-readable provenance object
wk doctor -i                # fzf drill-down: pick a layer, preview its source file
```

### Provenance inspection (`wk doctor`)

`wk doctor` answers the runtime question the contract validator does not: **what
Claude-Code / shell config is active in this directory now, and which layer put it
there?** It is strictly read-only and reuses the same plumbing as everything else —
`wsenv` for code/cwd -> org resolution, and the cc-owned `workspace-profile-validate`
for the CONTRACT row. It never reimplements resolution and never mutates.

It shows the four stacked layers and what each contributes:

- **GLOBAL** `~/.claude` — the always-on baseline (agents / skills / hooks / mcp).
- **ORG** `~/.config/workspace/<org>/` — the per-org overlay `wsenv --flags` injects,
  one row per injection surface (`claude/` `--add-dir`, `mcp.json` `--mcp-config`,
  `prompt.txt` `--append-system-prompt-file`, `plugin/` `--plugin-dir` (the only
  hook-carrying unit), `settings.json` `--settings`) plus the shell overlay applied by
  `eval "$(wsenv)"` (`env.sh` exported vars, `wrappers/` PATH prepend). A `[x]`/`[ ]`
  marks present vs absent, so you see at a glance why `wsenv --flags` emitted what it did.
- **REPO** `<git-root>/.claude` — per-repo config, when present.
- **CONTRACT** — `valid` / `INVALID — <reason>` from the cc validator (or "no profile"
  when the org has no profile dir yet).

`--json` emits the full object (consumable by other tooling); `-i` opens an fzf
drill-down where selecting a layer previews its live source file (falls back to plain
when fzf is absent).

## Per-org profiles

`~/.config/workspace/<org>/` — `env.sh` (sourced), `wrappers/` (prepended to PATH),
`claude/` (`--add-dir` skills), plus `mcp.json` / `prompt.txt` when present. Each is a
chezmoi **symlink to the committed** `packages/workspace/profiles/<org>/` tree (see
§ How it deploys) — edit in place, review in git.

- **b-and-b:** `AZURE_CONFIG_DIR=~/.azure-bbadmin` + the SOCKS-proxy `az` wrapper.
- **priceless / personal:** native `az` (global), no org overlay yet.

## Profile contract (cc-owned, validated on demand)

The Claude-Code injection surface of each profile (plugin manifest, agent/skill symlinks,
`*.mcp.json`, `settings.json` keys, prompt file) is governed by a **versioned contract owned by
cc** — the narrow waist that keeps this generator and cc's injector from drifting:

- Schema: `~/.claude/scripts/state/schemas/workspace-profile.schema.json`
- Validator: `~/.claude/scripts/bin/workspace-profile-validate <profile-dir> [--json]`
  (JSON report; exit `0` valid / `1` invalid; internal error -> exit `0` + `error` key)

Two enforcement seams in this package call it:

- **`wk doctor`** runs the validator against the resolved org's profile and reports the
  result as its CONTRACT row (`valid` / `INVALID — <reason>`); a missing validator or
  profile is reported, never fatal (fail-soft). `generate-profiles` is a **scaffolder
  only** — it never validates (see its header; the pre-rehome staged-generation model
  was retired 2026-07-05).
- **`wsenv --validate <code>`** resolves the org and runs the validator without launching — an
  explicit, opt-in launch-time safety net for hand-edited profiles. The default
  `--flags`/`--activate` path is **never** gated by validation (fail-open at launch).

## Consumers

- `scripts/cmux-workspaces.sh` calls `wsenv` at pane spawn (activates env + launches claude with flags).
- Future: tmux/zellij persistent-session wiring (so CC sessions survive SSH disconnect).
