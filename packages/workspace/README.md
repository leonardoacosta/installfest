# workspace — org-scoped environment & physical workspace directories

Single home for routing dev work by **org category** (`b-and-b` / `priceless` / `cc` /
`personal`). Resolves a repo to its workspace, activates the right env + shell wrappers +
Claude Code config, and keeps it all in sync across machines via chezmoi. `mux`
(`scripts/cmux-workspaces.sh`) is the sole CLI surface for org-related commands — the earlier
`wk` umbrella dispatcher was retired in favor of it (see § History below).

## The four org directories

`~/dev/brown`, `~/dev/priceless`, `~/dev/cc`, `~/dev/personal` are the physical home for each
org going forward — provisioned idempotently on every chezmoi-managed machine by
`home/run_once_create-org-workspace-dirs.sh.tmpl`. Each is registered in `home/projects.toml`
as an ordinary self-referential project code (`brown`, `priceless`, `cc`, `personal`), so
`mux <org>` opens exactly one workspace at that root — no special launch mode, the same
mechanism as launching any other project.

`~/dev/cc` is the live Claude Code config repo (`~/.claude` symlinks to it) and dual-purposes
as the `cc` org home — nothing here treats it as an emptyable container.

## Layout

```
packages/workspace/
  bin/
    ws-ready            portfolio "ready work" — dispatches per profile.toml tracker (mux ready)
    ws-doctor           provenance inspector — what config is active here + which layer set it (mux doctor)
    ws-scan             filesystem detection — keeps home/projects.toml honest (mux scan)
    wsenv               resolver + activator (code/cwd -> org; emits env/PATH or claude flags)
    ws-claude           launch Claude with org profile inside a persistent zellij session
    generate-profiles   generator: reads the registry, scaffolds packages/workspace/profiles/<org>/
  lib/
    org-detect.sh       git-remote-based org derivation, shared by ws-scan
    trackers/           per-tracker adapters: beads-ready, ado-ready, none-ready (+ README)
  profiles/<org>/       COMMITTED profile tree — ~/.config/workspace/<org> symlinks here
    profile.toml        tracker config (consumed by ws-ready)
    env.sh              portable env, sourced at activation by wsenv (+ overlay tail)
    claude/, wrappers/  --add-dir target + PATH overlay (wrappers/az -> executable_az)
    plugin/             org agents+skills bundle (--plugin-dir); agents/skills are
                        relative symlinks into ~/dev/cc, plugin.json is committed
    settings.json       installed-plugin enablement overlay (--settings; b-and-b only)
  integrations/         consumer glue (cmux, etc.)
  README.md
```

### CLI convention

`mux` is the single CLI surface — it never bulk-launches (one workspace per invocation) and
dispatches three non-launch subcommands to the scripts above:

- `mux doctor [code]` -> `ws-doctor` (provenance inspection)
- `mux ready [org]` -> `ws-ready` (tracker-ready query)
- `mux scan` -> `ws-scan` (filesystem detection)

`ws-doctor`/`ws-ready`/`ws-scan` are also directly invocable outside `mux` (chezmoi symlinks
them onto PATH, matching `ws-claude`'s existing pattern) — useful for scripting.

## How it deploys (both machines, in sync)

- `bin/wsenv` is symlinked onto PATH by chezmoi: `home/dot_local/bin/symlink_wsenv.tmpl`
  → `~/.local/bin/wsenv` → `~/dev/personal/installfest/packages/workspace/bin/wsenv`. `~/.local/bin` is already
  on PATH (`.zshenv`), so bare `wsenv` works. `ws-doctor`/`ws-ready`/`ws-scan` deploy the same way.
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
- The four org directories deploy via `home/run_once_create-org-workspace-dirs.sh.tmpl` —
  idempotent `mkdir -p`, safe to re-run, never touches existing contents.

## Registry (source of truth)

`~/dev/personal/installfest/home/projects.toml` (the `category` field), read in-place — it is
also consumed by generate-raycast.sh / cmux-workspaces.sh / mux-remote.sh, so it stays there for
now. **Convergence (deferred):** fold cc's `projects.json` (deploy fields) in, and let cc derive
from this registry — IF becomes the single source of truth.

### Keeping the registry honest (`mux scan`)

`mux scan` walks `~/dev`, derives each git repo's org from its `origin` remote
(`packages/workspace/lib/org-detect.sh` — `brownandbrowninc` -> `b-and-b`,
`Priceless-Development` -> `priceless`, `leonardoacosta` -> `personal`, registry code `cc` ->
`cc` hardcoded), and:

- **auto-registers** genuinely new repos (dedup by remote URL, never by path/code)
- **reports, never auto-fixes**: category mismatches on already-registered entries, registered
  paths missing on disk, a registered code whose live repo was found at a *different* path than
  registered (`relocated` — common on this Mac, where several codes' `path` fields are still
  aspirational nested paths that don't match the actual flat layout), duplicate-origin clones,
  and the known `~/dev/priceless` collision (an unrelated `Priceless-Development/priceless`
  dashboard repo currently occupies the org-container path)

`cc-audit` is explicitly excluded from derivation — it's a distinct auditing tool, not a second
`cc`-org member.

**The `~/dev/priceless` collision**: `~/dev/priceless` already holds several correctly-nested
member projects (`card-scope`, `tribal-cities`, `bridging-biosciences`) as well as the
`Priceless-Development/priceless` dashboard repo's own top-level tracked files, sitting at the
same path the org container needs. Confirmed homelab remediation: move the dashboard repo's
top-level tracked content into `~/dev/priceless/priceless-app/` (preserving the already-nested
member projects in place) — this is a manual, human-executed step (multi-step git-history-
preserving restructure, potentially with GUI tools like GitKraken holding open file handles into
the nested repos), never automated by `mux scan` or anything else here.

## Usage

```bash
# Environment activation (existing)
wsenv --org ws              # -> b-and-b
wsenv --list                # all code -> org mappings
eval "$(wsenv ws)"          # activate b-and-b in this shell (env + wrappers PATH)
claude $(wsenv --flags ws)  # launch claude with the org's CC profile flags

# Launching workspaces — mux never bulk-launches, one code = one workspace
mux oo tc                   # two workspaces, one per project code
mux brown                   # one workspace at ~/dev/brown (the org root)
mux priceless personal      # two workspaces, one per org root
mux --local oo               # local instead of SSH
mux --list                   # every registered code, grouped by org

# Tracker-ready query (mux ready, dispatches to ws-ready)
mux ready priceless         # ready beads issues across the priceless portfolio
mux ready personal          # ready beads issues across the personal portfolio
mux ready b-and-b            # ADO work items (requires az devops login + project_id)
mux ready --table priceless  # column-aligned PRI/ID/TITLE/PROJECT
mux ready                    # resolves org from $PWD via wsenv

# Provenance inspection (mux doctor, dispatches to ws-doctor)
mux doctor                   # what config is active in $PWD + which layer set it
mux doctor ws                # inspect a specific repo's org (b-and-b)
mux doctor --json            # machine-readable provenance object
mux doctor -i                # fzf drill-down: pick a layer, preview its source file

# Registry detection (mux scan, dispatches to ws-scan)
mux scan --dry-run           # report only, no projects.toml writes
mux scan                     # report + auto-register genuinely new repos
```

### Provenance inspection (`mux doctor`)

`mux doctor` answers the runtime question the contract validator does not: **what
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
- **priceless / cc / personal:** native `az` (global), no org overlay yet.

## Profile contract (cc-owned, validated on demand)

The Claude-Code injection surface of each profile (plugin manifest, agent/skill symlinks,
`*.mcp.json`, `settings.json` keys, prompt file) is governed by a **versioned contract owned by
cc** — the narrow waist that keeps this generator and cc's injector from drifting:

- Schema: `~/.claude/scripts/state/schemas/workspace-profile.schema.json`
- Validator: `~/.claude/scripts/bin/workspace-profile-validate <profile-dir> [--json]`
  (JSON report; exit `0` valid / `1` invalid; internal error -> exit `0` + `error` key)

Two enforcement seams in this package call it:

- **`mux doctor`** runs the validator against the resolved org's profile and reports the
  result as its CONTRACT row (`valid` / `INVALID — <reason>`); a missing validator or
  profile is reported, never fatal (fail-soft). `generate-profiles` is a **scaffolder
  only** — it never validates (see its header; the pre-rehome staged-generation model
  was retired 2026-07-05).
- **`wsenv --validate <code>`** resolves the org and runs the validator without launching — an
  explicit, opt-in launch-time safety net for hand-edited profiles. The default
  `--flags`/`--activate` path is **never** gated by validation (fail-open at launch).

## Consumers

- `scripts/cmux-workspaces.sh` (`mux`) calls `wsenv` at pane spawn (activates env + launches
  claude with flags), and dispatches `doctor`/`ready`/`scan` to this package's `ws-*` scripts.
- Future: tmux/zellij persistent-session wiring (so CC sessions survive SSH disconnect).

## History

The `wk` umbrella CLI dispatcher (`wk <name>` -> any `wk-<name>` executable on PATH) was retired
in favor of collapsing everything into `mux` — `wk` (identity/status/tracker-ready) and `mux`
(workspace launch) had grown into two overlapping command-center surfaces reading the same
registry for adjacent purposes, and `mux` already had the deeper integration (`pane_exec`
already called `wsenv` per-pane). `wk-doctor`/`wk-ready` were renamed to `ws-doctor`/`ws-ready`
(bodies unchanged) and re-homed as `mux doctor`/`mux ready`. Physically `cd`-ing into
`~/dev/<org>` — not a CLI command — is now the "what workspace am I in" signal (`chpwd.zsh`
already auto-activates identity on `cd`, via `wsenv`).
