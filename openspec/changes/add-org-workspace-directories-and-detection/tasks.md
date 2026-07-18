<!-- beads:epic:if-kiy -->
<!-- beads:feature:if-79ze -->

# Implementation Tasks

## DB Batch

- [ ] [1.1] [P-1] Correct `home/projects.toml`: extend the `category` field comment to document the 4th value `"cc"`; reclassify the `cc` entry (`category = "personal"` -> `"cc"`, `path = ".claude"` -> `"dev/cc"`); reclassify the `cs` entry (`category = "personal"` -> `"priceless"`, path/remote already agree) [owner:devops-engineer] [type:config] [beads:if-lhcj]
- [ ] [1.2] [P-2] Create `home/run_once_create-org-workspace-dirs.sh.tmpl`: idempotent `mkdir -p ~/dev/{brown,priceless,cc,personal}`, never touches existing contents, follows the existing `run_once_install-packages.sh.tmpl` header/logging conventions [owner:devops-engineer] [type:config] [beads:if-q7rg]

## API Batch

- [ ] [2.1] [P-1] Create `packages/workspace/lib/org-detect.sh`: shared helper deriving org from a repo's `git remote get-url origin`, precedence (registry code `cc` -> `cc` hardcoded; `brownandbrowninc` in origin -> `b-and-b`; `github.com[:/]Priceless-Development/` -> `priceless`; `github.com[:/]leonardoacosta/` -> `personal`; else `unknown`); reuses `scripts/lib/registry.sh`'s `registry_python` pattern for any TOML I/O [owner:devops-engineer] [type:config] [beads:if-yas7]
- [ ] [2.2] [P-1] Create `packages/workspace/bin/ws-scan` (dispatched as `mux scan`): walks `~/dev` excluding `archive/` paths, stops descent at each repo's `.git` root, classifies via [2.1], dedups new registrations by origin URL (not path/code), auto-appends genuinely-new entries to `home/projects.toml` via read-then-append `tomllib`/append (never regex/string-splice), reports (stdout only) category mismatches, missing paths, duplicate-origin clones, and the known `~/dev/priceless` collision by name; excludes `cc-audit` entirely from derivation [owner:devops-engineer] [type:config] [beads:if-q5bh]
- [ ] [2.3] [P-1] Delete `packages/workspace/bin/wk` and `home/dot_local/bin/symlink_wk.tmpl`. Rename `packages/workspace/bin/wk-doctor` -> `packages/workspace/bin/ws-doctor` and `packages/workspace/bin/wk-ready` -> `packages/workspace/bin/ws-ready`, bodies unchanged (rename-and-rehome only, per proposal.md's explicit verbatim-preservation requirement); delete their `symlink_wk-doctor.tmpl`/`symlink_wk-ready.tmpl` templates, add `home/dot_local/bin/symlink_ws-doctor.tmpl`/`symlink_ws-ready.tmpl` (matching the existing `symlink_ws-claude.tmpl` pattern) [owner:devops-engineer] [type:config] [beads:if-4qo8]

## UI Batch

- [ ] [3.1] [P-1] Extend `scripts/cmux-workspaces.sh` (`mux`): add `mux doctor [code]` (exec `ws-doctor "$@"`), `mux ready [org]` (exec `ws-ready "$@"`), `mux scan` (exec `ws-scan`) as new leading-arg cases in the existing arg-parsing loop, checked before the fallback `targets+=("$1")`; add a fourth `GROUP_CC` bucket to `load_projects`'s python (mirroring `GROUP_BB`/`GROUP_PRICELESS`/`GROUP_PERSONAL`) selected via `mux cc`; extend `--list`/`--help` text to show the `cc` group [owner:devops-engineer] [type:config] [beads:if-7tqw]
- [ ] [3.2] [P-3] Update `packages/workspace/README.md`: document the 4-org (`brown`/`priceless`/`cc`/`personal`) directory convention, the git-remote derivation rule, `mux scan`'s report-vs-auto-register split, `mux doctor`/`mux ready`/`mux scan`/`mux cc` as the retired `wk`'s replacements, and the known `~/dev/priceless` collision caveat [owner:docs-engineer] [type:docs] [beads:if-srvh]

## E2E Batch

- [ ] [4.1] Runtime-verify `org-detect.sh` against the confirmed live remote table from this session (a `brownandbrowninc` AzDO URL, a `Priceless-Development` GitHub URL, a plain `leonardoacosta` GitHub URL, the `cc` code special-case, and an unrecognized host) — paste actual stdout for each of the 5 cases [owner:general-purpose] [type:testing] [beads:if-geh7]
- [ ] [4.2] Runtime dry-run `mux scan` against the real `~/dev` on this Mac; inspect and paste the actual appended `projects.toml` entries plus every reported warning (mismatches, missing paths, duplicate clones, the priceless collision) before treating this task as done [owner:general-purpose] [type:testing] [beads:if-3ufo]
- [ ] [4.3] Runtime-verify `mux doctor`, `mux ready <org>`, and `mux --list`/`mux --help` (showing the new `cc` group) — paste actual stdout for each, and confirm `mux doctor`/`mux ready` output matches the pre-change `wk doctor`/`wk ready` baseline captured before task [2.3] deletes `wk` [owner:general-purpose] [type:testing] [beads:if-18eo]
- [ ] [4.4] Runtime-verify `chezmoi apply --dry-run` shows the provisioning script would create only missing org dirs, then verify a second run is a clean no-op; paste actual output for both runs [owner:general-purpose] [type:testing] [beads:if-ui37]
- [ ] [4.5] [user] DECISION: resolve the `~/dev/priceless` name collision (the existing `Priceless-Development/priceless` checkout currently occupies the path meant for the org container) — rename/relocate it by hand once [4.2]'s scan report confirms the collision — searched: this proposal's design.md § "the `~/dev/priceless` collision", rules/CORE.md Breaking Changes Policy; no documented pattern covers automatically relocating a live git checkout, this is inherently a human call. [type:config] [beads:if-anhp]
