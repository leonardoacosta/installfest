---
order: 0722n
---

# Proposal: add-front-door-docs — surface apps/, openspec/specs/, and vendored submodules

## Change ID
`add-front-door-docs`

## Why
The fastest-growing part of the repo — `apps/` (6 entries, 4 first-party) — is invisible in
the root README's directory map, which lists only `home/ platform/ scripts/ ssh-mesh/ docs/`.
The real per-capability doc surface, `openspec/specs/` (12 capability specs: cc-tmux,
cmux-sidebars, ctx-scan, daily-brief, file-viewer, launcher-registry, remote-access,
repo-restructure, ssh-mesh, tmux-config, wavetui, workspace), isn't pointed to from any entry
doc — `rg "openspec/specs" README.md CLAUDE.md` returns nothing. The three most active
first-party apps (`wavetui`, `ctx-scan`, `daily-brief`) have no README at all — only `cc-tmux`
does. And `apps/` silently mixes first-party code with two pinned upstream git submodules
(`kontroll`, `zsa-voyager-keymap`) with no marker distinguishing them, a real risk in an
agent-heavy repo where an agent can't otherwise tell maintained code from vendored code it
should never edit.

This converts `plans/008-front-door-docs.md` into an OpenSpec change. Pure documentation —
zero code risk, no gate dependency.

## What Changes
- README directory-map additions: `apps/` (with a one-line annotation per first-party app plus
  `(vendored submodule)` markers on `kontroll`/`zsa-voyager-keymap`), `packages/`, `shared/`,
  `infra/`, `openspec/`.
- An `openspec/specs/` pointer added to README (as the per-capability doc surface) plus exactly
  one clause appended to CLAUDE.md's existing directory-layout sentence — nothing else in
  CLAUDE.md is touched.
- Three new per-app READMEs: `apps/wavetui/README.md`, `apps/ctx-scan/README.md`,
  `apps/daily-brief/README.md` — each naming what the app is, its build command, its test
  command, its entry point, and a pointer to its `openspec/specs/<name>/spec.md`.
- A vendored-submodules marker (`apps/README.md`, or an apps subsection in the root README)
  naming `kontroll` and `zsa-voyager-keymap` as pinned-upstream submodules — bump, don't edit.
- Optional: a one-line scope note in `docs/executables.md` pointing at the new per-app READMEs
  for `apps/*` binaries (skip if it complicates that file's existing scope statement).

**Explicitly out of scope**: the doc-drift bundle already tracked and closed as bead
`if-7cce.2` (CLAUDE.md's directory-listing dirs, undocumented front-door tools, stale
mx-broker status) — that bead is already resolved (commit `3f9d4b9`) and this change makes
only the one additional openspec/specs clause the plan calls for, nothing further to
CLAUDE.md. Also out of scope: any code, any `.gitmodules`/submodule restructuring (e.g. moving
submodules under `apps/vendor/`), editing the openspec specs themselves (this change points to
them, never edits them), and per-app READMEs for the two submodules (they carry their own
upstream READMEs).

## Context
- touches: `README.md`, `CLAUDE.md`, `apps/wavetui/README.md`, `apps/ctx-scan/README.md`,
  `apps/daily-brief/README.md`, `apps/README.md`
- Independent — zero code risk, no gate dependency, per the source plan's own header
  (`plans/008-front-door-docs.md`, priority 8 of 8, "can run any time").
- Origin: `plans/008-front-door-docs.md`, written against commit `d441448`.

## Testing
N/A — no automated test; verification is the done-criteria greps plus a human accuracy read
(each per-app description cross-checked against its `openspec/specs/<name>/spec.md` and real
entry point), per plans/008's own Test plan section.

## Done Means
- README's directory tree lists `apps/`, `packages/`, `shared/`, `infra/`, and `openspec/`
  (`rg "apps/" README.md` hits inside the tree).
- README and CLAUDE.md both reference `openspec/specs/` as the per-capability doc surface
  (`rg "openspec/specs" README.md CLAUDE.md` hits in both files).
- `apps/wavetui`, `apps/ctx-scan`, and `apps/daily-brief` each have a README naming their
  build command, test command, and entry point.
- A README section (or `apps/README.md`) names `kontroll` and `zsa-voyager-keymap` as vendored,
  pinned-upstream submodules (`rg -i "vendored|submodule" apps/README.md`).
- No code file changed — `git diff --name-only` shows only `.md` files.
