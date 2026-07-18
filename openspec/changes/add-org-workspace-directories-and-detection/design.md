# Design: Org Workspace Directories + Filesystem Project Detection

## Ground truth gathered this session

SSH'd into the homelab machine and diffed its `~/dev` layout against the Mac's:

| | Homelab | Mac |
|---|---|---|
| `~/dev/brown` | real dir, itself a git repo (`.git`/`.beads`/`CLAUDE.md` at its root), holds `b3admin`, `b3owa`, `dc`, `ds`, `fireball`, `ic`, `lu`, `ws`, `salescrm`, `submissionengine`, etc. as subdirs (each its own nested git checkout) | does not exist (`b&b` near-empty, `brownandbrown.its.*` flat siblings instead) |
| `~/dev/priceless` | plain container dir (no `.git` of its own), holds `otaku-odyssey`, `civalent`, `tl`, `tribal-cities`, `lv`, `modern-visa`, `styles-silas`, `card-scope`, `priceless-config`, etc. | **itself a git repo** — `github.com/Priceless-Development/priceless.git` — collides with the intended container name |
| `~/dev/personal` | plain container dir, holds `ap`, `homelab`, `installfest`, `la`, `mesh`, `nv`, `nexus`, etc. | plain container dir already (partial: only `installfest`/`nexus` symlinked in; most personal repos still flat) |
| `~/dev/cc` | the live Claude Code config repo | same — the live Claude Code config repo, `~/.claude` symlinks to it |
| `~/dev/archive` | `cl`, `co`, `ct`, `cw`, `cx`, `xx`, `zune` | scattered `dev/archive/*` registry paths, not all present on disk |

Confirmed origin remotes for every top-level Mac `~/dev/*` git repo (full table run this
session): `brownandbrowninc` (AzDO, two host forms) for every B&B satellite, `Priceless-
Development` (GitHub) for every priceless project, `leonardoacosta` (GitHub) for personal repos
plus `cc`/`cc-audit` (both point at `central-claude`). This is what grounds the org-derivation
heuristic in the proposal — it isn't guessed, it's the rule that already explains 100% of the
observed remotes.

## Key decision: report vs. auto-register

Posed to Leo as an explicit binary choice (`AskUserQuestion`): should `wk scan` auto-register
newly-found repos into `projects.toml`, or only report drift for manual review? Leo chose
**auto-register**. This document records the asymmetry that choice implies and why it's safe:

- **New, unregistered repos** (no existing entry's remote matches): auto-appended. Low risk —
  appending a new `[[projects]]` block cannot corrupt or overwrite anything that already exists,
  and the dedup-by-remote check (not by path or code) means a repo already known under any code
  never gets a second entry.
- **Already-registered entries whose live category disagrees with derived category**: reported
  only, never rewritten in place. This is a different risk class — overwriting a field a human
  already set (however wrong it might look) without an explicit review step is the kind of
  silent-correction the Breaking Changes Policy (rules/CORE.md) exists to prevent. The one
  exception in this proposal is the `cs` entry, corrected explicitly and by name in the spec
  text itself (not by the generic scan logic) because the evidence is unambiguous: its own
  `path` (`dev/priceless/card-scope`) and its git remote both already say `priceless` — only the
  `category` field itself was wrong. That's a spec-author-verified one-off correction, not a
  precedent for the scan to silently "fix" future mismatches the same way.
- **Path-missing entries and duplicate clones**: reported only. Both require a human decision
  (is the project gone, renamed, or just not on this machine yet? which duplicate is canonical?)
  that the tooling has no safe default for.

## Key decision: `cc` org is hardcoded, not remote-derived

`cc`'s origin (`github.com/leonardoacosta/central-claude.git`) is structurally identical to any
other `leonardoacosta`-owned personal repo — there is no URL signal that distinguishes "this is
the cc org" from "this is a personal project I happen to own." Rather than invent a fragile
naming-pattern rule (e.g. "starts with `cc`"), the derivation helper special-cases by registry
`code == "cc"` — an explicit, auditable one-line exception instead of a heuristic that could
misfire on some future personal repo that happens to start with "cc". `cc-audit` shares the same
remote (also `central-claude`) but is explicitly excluded from derivation entirely per Leo — it
is a distinct, out-of-scope tool for auditing cc, not a second cc-org member.

## Key decision: the `~/dev/priceless` collision is surfaced, never auto-resolved

Moving a live git checkout (`~/dev/priceless`, the actual `Priceless-Development/priceless`
project) out of the way so the directory can serve as the org container is exactly the kind of
hard-to-reverse filesystem operation the operating manual's Iron Laws gate on user confirmation.
`mux scan` reports it by name every run until Leo resolves it by hand (e.g. renaming the
project's checkout, or deciding the org container should live at a different path) — the
provisioning script's `mkdir -p` is a no-op against an existing directory either way, so nothing
breaks by leaving the collision unresolved; `wsenv` continues working exactly as it does today
(it already tolerates a project living wherever its registry `path` says).

## Key decision: `wk` is retired mid-flight, folded into `mux`

This session started as "add a workspace display to `wk`'s help output" and ended somewhere
different: while scoping the org-directory work, it became clear `wk` (identity/status/tracker-
ready) and `mux` (bulk-launch workspaces) had grown into two commands both reading
`projects.toml` and both organizing by org — `wk`'s bare-invocation display (this session's
first change, commit `5870fee`) was the moment that overlap became visible, since it started
duplicating the exact "list every org" framing `mux --list` already had. `mux` is also the
command with the deeper existing integration: its `pane_exec` already calls `wsenv` per-pane for
identity activation, so it was never truly `wk`-independent to begin with.

Leo's call (`AskUserQuestion`, this session): retire `wk` as a distinct dispatcher, fold its two
real jobs into `mux` (`mux doctor` for provenance, `mux ready` for tracker-query), and let the
physical `~/dev/<org>` directory you're `cd`'d into — not a CLI command — be the "what workspace
am I in" signal. `chpwd.zsh`'s existing auto-activation already makes cwd the source of truth for
shell identity; this just stops maintaining a second, redundant command surface on top of it.

**What survives the rename**: `wk-doctor`'s and `wk-ready`'s actual logic (GLOBAL/ORG/REPO/
CONTRACT provenance layers; tracker-adapter dispatch) is unchanged — only the filename (drops
the `wk-` PATH-discovery prefix, since there's no more `wk` umbrella to discover it) and the
entry point (`mux doctor`/`mux ready` instead of a bare `wk doctor`/`wk ready`) change. This is
an explicit rename-and-rehome, verified by comparing output against the pre-change baseline
(task [4.3]), not a silent behavior rewrite.

**Why not just delete `wk-doctor`/`wk-ready` outright**: their underlying value (provenance
inspection, tracker-ready query) is real and unrelated to the umbrella-dispatcher question — only
`wk` itself (the thin `wk-<name>` PATH-scanning shim) was the redundant layer.

## Key decision: org-root launch reuses the existing single-code path, no new argument type

Mid-session, Leo flagged that `mux <orgid>` should behave like any other single-workspace launch
— never fan out into a suite. The naive fix would add a parallel "org launch mode" (a new arg
shape, a new dispatch branch, its own pane-layout logic). But `home/projects.toml` already proves
a simpler mechanism works: `brown` (`code = "brown"`, `path = "dev/brown"`) is registered exactly
like any other project, and `mux brown` already opens one workspace at `~/dev/brown` today — no
special-casing needed, because `mux <code>` has never had bulk-launch semantics; only the
`b`/`c`/`p` *group letters* did. `cc` will work the same way once its `path` is corrected to
`dev/cc` (Requirement above). So the actual gap is narrow: `priceless` and `personal` are
categories, not registered codes — there is no `[[projects]]` row pointing at their org roots.
Adding two self-referential rows (mirroring `brown`) closes that gap using the mechanism that
already exists, rather than inventing a second one (Reader Gate: reuse before reinvention). The
group letters and their bulk-launch machinery are then dead weight and get deleted outright,
per Leo's explicit choice to drop suite-mode entirely rather than keep it behind a flag.

**Non-git org roots need a pane-layout fallback.** `brown` and `cc` are themselves git repos at
their root (confirmed via SSH this session — `brown` even carries its own `.beads`, matching the
`central-planning` hydration-hub pattern), so a `lazygit` pane there works today. `priceless` and
`personal`, per the homelab's own layout, are plain container directories with no `.git` at their
root — `lazygit` would error immediately if launched there. `populate_workspace` needs a `[[ -d
"$full_path/.git" ]]` check (or equivalent) to skip that pane rather than open a broken one; the
claude and nvim panes are unaffected either way (both work fine in a non-git directory).

## Non-goals restated

No repo `mv`, no dedup of duplicate clones, no change to `cc-audit`, no homelab-side change
(already correct), no change to `wsenv`'s resolution algorithm itself, no bulk/suite launch mode
in any form (not even behind a flag — dropped entirely per Leo's explicit choice) — only the
registry data and a new detection/display layer around it.
