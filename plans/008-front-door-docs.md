# Plan 008 — Front-door docs: surface the apps/ layer, openspec specs, and vendored submodules

**Written against commit:** `d441448` — if excerpts no longer match, STOP and report drift.
**Findings:** docs #1 (README omits `apps/`, MED), #2 (openspec/specs undiscoverable, HIGH), #3 (no per-app READMEs for the 3 active apps, HIGH), #4 (vendored submodules unmarked, MED). Plus direction item E.
**Priority:** 8 of 8. Pure documentation — zero code risk, no gate dependency. Can run any time.

## Why this matters

The fastest-growing part of the repo — `apps/` (6 entries, 4 first-party) — is invisible in
the front-door README's directory map, and the real per-capability doc surface
(`openspec/specs/`, 12 specs) isn't pointed to from any entry doc. The three most active
first-party apps (wavetui, ctx-scan, daily-brief) have no README. And `apps/` mixes
first-party code with pinned upstream git submodules (`kontroll`, `zsa-voyager-keymap`) with
no marker, so an agent can't tell maintained code from vendored — a real risk in an
agent-heavy repo.

**Do NOT re-report the known doc-drift bundle tracked as bead `if-7cce.2`** (CLAUDE.md dirs,
undocumented front-door tools, stale mx-broker doc status). This plan covers only the NEW
drift above. If your edits would overlap if-7cce.2's CLAUDE.md scope, coordinate: make the
minimal addition and note the overlap.

## Current state (verified excerpts)

`README.md:85-115` — directory map lists only `home/ platform/ scripts/ ssh-mesh/ docs/`;
no `apps/`, `packages/`, `shared/`, `infra/`, or `openspec/`.

`.gitmodules` — two submodules unmarked in `apps/`:

```
[submodule "apps/zsa-voyager-keymap"]
	url = https://github.com/leonardoacosta/oryx-with-custom-qmk.git
[submodule "apps/kontroll"]
	url = https://github.com/zsa/kontroll.git
```

`openspec/specs/` holds 12 capability specs (cc-tmux, ctx-scan, daily-brief, wavetui,
workspace, ssh-mesh, remote-access, tmux-config, launcher-registry, file-viewer,
cmux-sidebars, repo-restructure); `rg "openspec/specs" README.md CLAUDE.md AGENTS.md` → nothing.

`apps/wavetui/`, `apps/ctx-scan/`, `apps/daily-brief/` have no `*.md` (verified: only
cc-tmux has a first-party README).

## Conventions to match

- README uses a fenced ASCII tree with `# comment` annotations (see the `home/` block at
  :86-105) — extend that exact style, don't restructure it.
- CLAUDE.md line 9 already lists the top-level layout in prose — the `apps/` mention there is
  in-scope for if-7cce.2, so touch it minimally (one clause) if at all.
- Per-app READMEs should be short and match cc-tmux's README shape (what it is, how to
  build/test, entry point) — read `apps/cc-tmux/README.md` as the template. Do not write
  aspirational docs; document what exists.

## Steps

1. **README: add `apps/` (and the other missing dirs) to the directory map.** In the fenced
   tree at README.md:85-115, add entries after `scripts/` (or wherever alphabetical/logical):
   `apps/` with a one-line-per-app annotation naming each first-party app and marking the two
   submodules as `(vendored submodule)`; plus one-line entries for `packages/`, `shared/`,
   `infra/`, `openspec/`. Keep annotations to one line each.
   Verify: `rg "apps/" README.md` → present; the tree still renders (balanced `├──`/`└──`).

2. **README + CLAUDE.md: point to `openspec/specs/` as the capability doc surface.** Add a
   short subsection (or a line in an existing "where things are documented" spot) to README
   stating that per-capability behavior is specified in `openspec/specs/<capability>/spec.md`,
   listing the capability names or just pointing at the directory. Add one clause to
   CLAUDE.md:9's existing openspec mention: "...specs — `openspec/specs/` is the per-capability
   doc surface." Verify: `rg "openspec/specs" README.md CLAUDE.md` → hits in both.

3. **Write per-app READMEs** for `apps/wavetui/README.md`, `apps/ctx-scan/README.md`,
   `apps/daily-brief/README.md`. Each: one-paragraph what-it-is (draw from the openspec spec
   and the code, not invention), build command, test command (`go test ./...` /
   `bun test`), entry point (`cmd/wavetui`, the `bin` field), and a pointer to its
   `openspec/specs/<name>/spec.md`. For wavetui, mention the config surface
   (`internal/config/config.go`, `.wavetui.toml`, `ctx_scan_poll_seconds` etc.) briefly.
   Keep each under ~40 lines. Verify: three new files exist and name the correct build/test commands.

4. **Mark the vendored submodules (finding #4 / direction E).** Two low-effort options —
   pick the doc-only one unless the maintainer has asked to restructure:
   - (chosen) Add a short "Vendored vs first-party" note in `apps/` — a top-level
     `apps/README.md` (or a section in the main README's apps subsection) listing which
     entries are pinned upstream submodules (`kontroll`, `zsa-voyager-keymap`) and which are
     first-party. State: "submodules are pinned upstream — bump, don't edit."
   - (do NOT do without maintainer sign-off) physically moving submodules to `apps/vendor/`
     — that rewrites `.gitmodules` paths and is a structural change, out of scope for a docs plan.
   Verify: `apps/README.md` (or the README apps section) names both submodules as vendored.

5. **Optional: docs/executables.md scope note (finding #5).** `docs/executables.md:2-3`
   scopes itself to `scripts/`, `platform/`, `home/dot_local/bin/` and excludes `apps/`
   binaries by design. Add one line noting that `apps/*` expose their own binaries
   (`ctx-scan`, `daily-brief`, `wavetui`) documented in their per-app READMEs. Low priority —
   do it if quick, skip if it complicates the file.

## Boundaries

- **In scope:** `README.md`, `CLAUDE.md` (minimal, one clause — respect if-7cce.2 overlap), new files `apps/wavetui/README.md`, `apps/ctx-scan/README.md`, `apps/daily-brief/README.md`, new `apps/README.md` (or an apps section), optionally `docs/executables.md` (one line).
- **Out of scope:** any code, any `.gitmodules`/submodule restructuring, the if-7cce.2 bundle content (undocumented front-door tools like copen/mac-open/view/vnc-mac are already tracked there), the openspec specs themselves (you point to them, don't edit them), per-app READMEs for the two submodules (they carry upstream READMEs — leave them).

## Done criteria (machine-checkable)

- `rg "apps/" README.md` → present in the directory tree.
- `rg "openspec/specs" README.md CLAUDE.md` → hits in both files.
- `ls apps/wavetui/README.md apps/ctx-scan/README.md apps/daily-brief/README.md` → all three exist.
- `rg -i "vendored|submodule" apps/README.md` (or the README apps section) → names kontroll + zsa-voyager-keymap.
- Each new per-app README names the correct test command for that app (`go test` vs `bun test`).
- No code file changed: `git diff --name-only` shows only `.md` files.

## Test plan

Docs — no automated test. Verification is the done-criteria greps plus a human read for
accuracy (the per-app descriptions must match reality; cross-check each against its
`openspec/specs/<name>/spec.md` and its actual entry point). Record the greps in your report.

## Maintenance note

New apps under `apps/` need: a directory-tree line in README, a per-app README, and (if
capability-specced) an openspec spec + a pointer. Consider stating that expectation in
`apps/README.md` so it's self-perpetuating. This plan is the doc half of direction item E;
the structural half (an `apps/vendor/` split) is a separate maintainer decision.

## Escape hatch

- If editing CLAUDE.md's directory prose would substantially overlap the if-7cce.2 bead's scope: make ONLY the openspec-specs clause addition, leave the rest to that bead, and note the boundary in your report.
