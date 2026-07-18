---
order: 0718a
---

# Proposal: Org Workspace Directories + Filesystem Project Detection

## Change ID
`add-org-workspace-directories-and-detection`

## Summary
Give each org (`b-and-b` / `priceless` / `cc` / `personal`) a real, physical `~/dev/<org>` home
directory — matching the layout already live on the homelab machine — back it with a
git-remote-based detection scan that keeps `home/projects.toml` honest, and retire the `wk`
umbrella CLI entirely: its provenance-inspection and tracker-query jobs fold into `mux`
(`scripts/cmux-workspaces.sh`), the one remaining workspace-facing command, alongside a new
`mux scan` for the detection work. Physically `cd`-ing into `~/dev/<org>` (chpwd already
auto-activates identity via `wsenv`) becomes the actual "which workspace am I in" signal —
no separate CLI needed to answer that question.

## Context
- Extends: `packages/workspace/bin/wsenv`, `home/projects.toml`,
  `scripts/cmux-workspaces.sh` (the `mux` command)
- Deprecates: `packages/workspace/bin/wk` (umbrella dispatcher, including this session's
  `print_workspace` addition, commit `5870fee` — never reaches a released state, superseded
  before its next use), `packages/workspace/bin/wk-doctor`, `packages/workspace/bin/wk-ready`
  (bodies retained, renamed and re-homed — see Requirements)
- Related: beads epic `if-kiy` ([WORKSPACE-CMDCENTER] Workspace as command center) — W1
  (`if-319`, tracker adapters) shipped 2026-06-01; W2+ were marked "SPECULATIVE-DEFERRED...
  revisit only if pain emerges." This proposal is that pain point surfacing AND a course
  correction on the CLI shape — `wk` as a second, parallel command-center surface duplicated
  what `mux` already does (both read `projects.toml`, both group by org); collapsing to one
  surface is the resolution.
- touches: `home/projects.toml`, `scripts/cmux-workspaces.sh`,
  `packages/workspace/bin/{wk,wk-doctor,wk-ready,ws-doctor,ws-ready,ws-scan}`,
  `packages/workspace/lib/org-detect.sh`, `home/run_once_create-org-workspace-dirs.sh.tmpl`,
  `home/dot_local/bin/symlink_{wk,wk-doctor,wk-ready,ws-doctor,ws-ready,ws-scan}.tmpl`,
  `packages/workspace/README.md`

## Motivation
Two problems surfaced in the same session. First, `home/projects.toml` has drifted from
reality: several `path` fields already describe a nested `dev/<org>/<project>` layout (e.g.
`ba` -> `dev/brown/b3admin`) that was never realized on the Mac — `~/dev` there is a flat pile
of ~50 sibling directories, while the **homelab machine already completed this exact
reorganization** (`~/dev/{brown,priceless,personal,cc,archive}`, confirmed live via SSH this
session). Second, once `wk` started growing a "show me every org" display, it became clear `wk`
and `mux` were becoming two overlapping command-center surfaces reading the same registry for
adjacent purposes (`wk`: identity/status/tracker-ready; `mux`: bulk-launch workspaces) — and
`mux` already calls `wsenv` per-pane for identity activation, so it's the one with the deeper
existing integration. Leo's call: fold `wk`'s provenance (`wk doctor`) and tracker-query
(`wk ready`) jobs into `mux`, and let the physical `~/dev/<org>` directories — not a CLI command
— be the answer to "what workspace am I in."

## Requirements

### Requirement: Four org-level workspace directories are provisioned
`~/dev/brown`, `~/dev/priceless`, `~/dev/cc`, and `~/dev/personal` SHALL exist on every
chezmoi-managed machine, created idempotently (`mkdir -p`, safe to run against a directory that
already exists or already holds content) via a `run_once_` chezmoi script. `~/dev/cc` is the
existing live Claude Code config repo (`~/.claude` symlinks to it) and dual-purposes as the `cc`
org home — the provisioning script MUST NOT attempt to move, empty, or otherwise treat it as a
generic container.

#### Scenario: fresh machine gets all four org dirs on first apply
- Given: a machine has none of the four org directories yet
- When: `chezmoi apply` runs the provisioning script
- Then: all four directories exist, and none of their preexisting contents (if any) were altered

#### Scenario: re-running provisioning is a no-op
- Given: all four org directories already exist (e.g. the homelab machine, or a second `chezmoi
  apply` on the Mac after first creation)
- When: the provisioning script runs again
- Then: it exits cleanly with no changes and no error

### Requirement: `category` registry field supports a fourth `cc` org
`home/projects.toml`'s `category` field SHALL accept `"cc"` as a fourth valid value alongside
`"b-and-b"`, `"priceless"`, and `"personal"`. The `cc` project entry (code `cc`) SHALL be
reclassified from `category = "personal"` to `category = "cc"`, and its `path` field corrected
from `.claude` (a symlink alias) to `dev/cc` (the real, canonical location the symlink resolves
to). The pre-existing `cs` (card-scope) entry SHALL be corrected from `category = "personal"` to
`category = "priceless"` — its `path` (`dev/priceless/card-scope`) and its git remote
(`github.com/Priceless-Development/card-scope`) already agree with `priceless`; only the
`category` field was wrong.

#### Scenario: wsenv resolves cc's org as cc, not personal
- Given: the corrected registry
- When: `wsenv --org cc` runs
- Then: it prints `cc`, not `personal`

### Requirement: git-remote-based org derivation
A shared helper SHALL derive a project's org from its git `origin` remote URL, applying rules in
this precedence order: (1) the project's registry `code` is `cc` -> org `cc` (hardcoded, never
remote-derived, since `cc`'s remote namespace (`leonardoacosta/central-claude`) is
indistinguishable from an ordinary personal repo by URL alone); (2) origin host/path contains
`brownandbrowninc` (either `dev.azure.com/brownandbrowninc` or
`brownandbrowninc.visualstudio.com`) -> org `b-and-b`; (3) origin contains
`github.com[:/]Priceless-Development/` -> org `priceless`; (4) origin contains
`github.com[:/]leonardoacosta/` -> org `personal`; (5) no match (no origin, or an unrecognized
host/owner) -> `unknown`, never auto-registered. The `cc-audit` project is explicitly excluded
from derivation entirely (out of scope for this effort) — it stays wherever it is, uncategorized
by this tooling.

#### Scenario: a Brown & Brown AzDO repo derives b-and-b
- Given: a git repo whose `origin` is `https://dev.azure.com/brownandbrowninc/Fireball/_git/fireball`
- When: the org-derivation helper runs against it
- Then: it returns `b-and-b`

#### Scenario: an unrecognized remote derives unknown, not a guess
- Given: a git repo whose `origin` is `git@some-other-host.example:foo/bar.git`
- When: the org-derivation helper runs against it
- Then: it returns `unknown`, and the detection scan does not auto-register it

### Requirement: `mux scan` reports and safely auto-registers discovered projects
A new `mux scan` subcommand SHALL walk `~/dev`, stopping descent at each git repository's root
(never recursing into `.git/`, `node_modules/`, or similar) and skipping any path under a literal
`archive/` directory entirely. It dispatches to `packages/workspace/bin/ws-scan` (new file). For
every git repo found, it SHALL:
- derive its org via the org-derivation helper above;
- auto-append a new `[[projects]]` entry to `home/projects.toml` ONLY when the repo's origin URL
  does not already match any existing registry entry's resolved remote (dedup by remote, not by
  path or code) and its derived org is not `unknown`;
- report (stdout, never auto-modify) any already-registered project whose derived org disagrees
  with its stored `category`;
- report (stdout, never auto-modify) any already-registered project whose `path` does not resolve
  to an existing directory;
- report (stdout, never auto-modify) any set of 2+ discovered repos sharing the same origin URL
  as duplicate clones;
- report (stdout) the known `~/dev/priceless` name collision specifically — an existing,
  unrelated `priceless` git repo (`github.com/Priceless-Development/priceless`) already occupies
  the path meant to be the `priceless` org container — as a user-actionable item, since resolving
  it requires a human decision (rename/relocate) this tooling MUST NOT make automatically.

#### Scenario: a genuinely new, unregistered repo gets auto-added
- Given: a git repo exists under `~/dev` with a resolvable org, and no existing `projects.toml`
  entry matches its origin URL
- When: `mux scan` runs
- Then: a new `[[projects]]` entry is appended to `home/projects.toml` with the derived
  `category`, a `path` relative to `$HOME`, and a `code` that does not collide with any existing
  code

#### Scenario: a duplicate clone is reported, not double-registered
- Given: two directories under `~/dev` both have `origin` `github.com/Priceless-Development/
  tribal-cities.git` (one already registered as code `tc`)
- When: `mux scan` runs
- Then: the second directory is reported as a duplicate of `tc`'s remote, and no second registry
  entry is created for it

#### Scenario: a category mismatch on an existing entry is flagged, not silently rewritten
- Given: a registered project's stored `category` disagrees with what the org-derivation helper
  computes from its live git remote
- When: `mux scan` runs
- Then: the mismatch is printed as a warning naming both values; `home/projects.toml` is not
  modified for that entry

### Requirement: `wk` is retired; provenance and tracker-query fold into `mux`
`packages/workspace/bin/wk` (the umbrella dispatcher) SHALL be deleted, along with its chezmoi
symlink template. Its two real subcommands are retained under new names, re-homed as `mux`
dispatch targets rather than `wk-*`-prefixed PATH-discovered commands:
- `packages/workspace/bin/wk-doctor` -> `packages/workspace/bin/ws-doctor` (same provenance-
  inspection body — GLOBAL/ORG/REPO/CONTRACT layers — unchanged logic, new filename), invoked as
  `mux doctor [code]`.
- `packages/workspace/bin/wk-ready` -> `packages/workspace/bin/ws-ready` (same tracker-adapter
  dispatch body, unchanged logic, new filename), invoked as `mux ready [org]`.
Both `wk-doctor` and `wk-ready` behavior (flags, JSON mode, interactive fzf drill-down for
doctor) SHALL be preserved verbatim under their new names and new `mux` entry points — this is a
rename-and-rehome, not a rewrite. `mux`'s existing chezmoi symlink template stays; `ws-doctor`/
`ws-ready` get new symlink templates (matching the existing `ws-claude` naming precedent) so they
remain directly invocable outside `mux` too, for scripting.

#### Scenario: mux doctor shows the same provenance report wk doctor used to
- Given: a cwd inside a registered project
- When: `mux doctor` runs
- Then: the output matches what `wk doctor` produced before this change (GLOBAL/ORG/REPO/CONTRACT
  layers, same detail), sourced from `ws-doctor`

#### Scenario: mux ready dispatches to the same tracker adapter wk ready used to
- Given: an org with a `beads` tracker in its `profile.toml`
- When: `mux ready <org>` runs
- Then: the output matches what `wk ready <org>` produced before this change

### Requirement: `mux`'s bulk-launch groups gain a fourth `cc` bucket
`scripts/cmux-workspaces.sh`'s category grouping (`GROUP_BB`/`GROUP_PRICELESS`/`GROUP_PERSONAL`,
selected via `mux b`/`mux c`/`mux p`) SHALL gain a fourth `GROUP_CC` bucket selected via `mux cc`,
so a project registered under the new `cc` category is discoverable via bulk-launch the same way
the other three orgs are, not silently dropped from every group (today's `groups = {"b-and-b":
[], "priceless": [], "personal": []}` dict has no `cc` key, so a `cc`-category project would be
invisible to `mux b`/`c`/`p` even though `mux <code>` direct-launch would still work). `mux
--list` and `mux --help` SHALL be extended to show the `cc` group alongside the existing three.
`scripts/mux-remote.sh`'s AppleScript picker (3-bucket, and already using the stale `"client"`
label instead of `"priceless"`) is OUT of scope for this proposal — flagged as pre-existing drift,
not fixed here.

#### Scenario: mux cc bulk-launches every cc-org project
- Given: `home/projects.toml` has at least one entry with `category = "cc"`
- When: `mux cc` runs
- Then: every `cc`-category project is launched, the same way `mux b`/`mux c`/`mux p` launch
  their respective groups

## Scope
- **IN**: `run_once_` provisioning of the four org directories; `category` enum + `cc`/`cs`
  registry corrections; git-remote-based org-derivation helper; `mux scan` detection/report/
  safe-auto-register subcommand; retiring `wk` and folding `wk-doctor`/`wk-ready` into `mux
  doctor`/`mux ready` (renamed to `ws-doctor`/`ws-ready`); a 4th `cc` bulk-launch group on `mux`;
  `packages/workspace/README.md` documentation of the convention.
- **OUT**: physically moving, renaming, or `mv`-ing any existing project directory or git
  checkout (including resolving the `~/dev/priceless` name collision and the ~7 duplicate-clone
  pairs found on the Mac — `ct`/`civalent`, `mv`/`modern-visa`, `tc`/`tribal-cities`,
  `oo`/`otaku-odyssey`, `lv`/`LasVegasClubPromotions`, `tl`/`tavern-ledger`,
  `card-scope`/`cardscope`) — `mux scan` reports these, Leo resolves them by hand; any change to
  `cc-audit`'s classification (explicitly excluded, stays as-is); any change to the homelab
  machine (already fully migrated, confirmed via SSH this session); rewriting `wsenv`'s existing
  code/cwd -> org resolution logic (unaffected — both `chpwd.zsh` and `mux`'s `pane_exec` keep
  calling it exactly as before); fixing `scripts/mux-remote.sh`'s stale 3-bucket AppleScript
  picker (pre-existing drift, separate concern).

## Testing
| Affected seam | Unit task | E2E task |
|----------------|-----------|----------|
| org-derivation helper (`packages/workspace/lib/org-detect.sh`) | [4.1] | N/A — pure shell function, no user-facing flow |
| `mux scan` (`ws-scan`) detection/report/auto-register | [4.2] | [4.2] (runtime scan against the real `~/dev`, output inspected) |
| `mux doctor` / `mux ready` / `mux --list` (4th cc group) | [4.3] | [4.3] (runtime output inspected against pre-change `wk doctor`/`wk ready` baseline) |
| `run_once_create-org-workspace-dirs.sh.tmpl` provisioning | [4.4] | [4.4] (runtime `chezmoi apply --dry-run` + idempotency re-run) |

## Impact
| Area | Change |
|------|--------|
| `home/projects.toml` | `category` enum gains `cc`; `cc` and `cs` entries corrected |
| `packages/workspace/bin/wk` | deleted |
| `packages/workspace/bin/wk-doctor` | renamed to `ws-doctor`, unchanged body |
| `packages/workspace/bin/wk-ready` | renamed to `ws-ready`, unchanged body |
| `packages/workspace/bin/ws-scan` | new file |
| `packages/workspace/lib/org-detect.sh` | new file |
| `scripts/cmux-workspaces.sh` | gains `ready`/`doctor`/`scan` subcommands + `GROUP_CC` |
| `home/run_once_create-org-workspace-dirs.sh.tmpl` | new file |
| `home/dot_local/bin/symlink_wk*.tmpl` | deleted (3 files) |
| `home/dot_local/bin/symlink_ws-{doctor,ready,scan}.tmpl` | new files |
| `packages/workspace/README.md` | documents the 4-org convention, `mux` as sole CLI surface, known collision |

## Risks
| Risk | Mitigation |
|------|-----------|
| Auto-registering a mis-derived org for an ambiguous/unusual remote | `unknown` org (no rule match) is never auto-registered; only the 4 confirmed heuristics (verified against every live Mac + homelab remote this session) auto-register |
| Provisioning script accidentally treats `~/dev/priceless` or `~/dev/cc` as an emptyable container | `mkdir -p` is inherently non-destructive (no-op on an existing path); requirement text explicitly forbids move/empty semantics; collision is surfaced by `mux scan`, never auto-resolved |
| Registry corruption from a buggy TOML-append | `ws-scan` appends via the same `registry_python`/`tomllib` read-then-append pattern `wsenv`/`ws-ready` already use, never regex/string-splice; task [4.2] requires a runtime dry-run inspection before any real append |
| Duplicate clones get silently merged/deleted | Explicitly out of scope — reported only, never touched |
| Renaming wk-doctor/wk-ready silently changes behavior | Requirement explicitly mandates verbatim body preservation (rename-and-rehome, not rewrite); task [4.3] runtime-compares output against the pre-change baseline |
| Muscle memory breaks (`wk doctor`/`wk ready` no longer exist) | One-time, deliberate: Leo's explicit call this session, not an incidental break |
