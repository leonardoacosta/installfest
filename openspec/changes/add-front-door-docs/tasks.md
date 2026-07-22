---
stack: t3
---

<!-- owner: homelab-specialist — this repo's non-T3-stack convention, see rules/PATTERNS.md -->

# Implementation Tasks

## API Batch

- [x] [1.1] README directory-map additions: add `apps/` (one-line-per-first-party-app
  annotation, plus `(vendored submodule)` markers on `kontroll` and `zsa-voyager-keymap`),
  `packages/`, `shared/`, `infra/`, and `openspec/` to the fenced directory tree in `README.md`
  (currently only lists `home/ platform/ scripts/ ssh-mesh/ docs/`), matching the existing
  `home/` block's `# comment` annotation style. [type:docs]
- [x] [1.2] README + CLAUDE.md openspec/specs pointer: add a short subsection (or a line in an
  existing "where things are documented" spot) to README stating that per-capability behavior
  is specified in `openspec/specs/<capability>/spec.md`. Add exactly ONE clause to CLAUDE.md's
  existing directory-layout sentence (line 9) — e.g. "...`openspec/` (change proposals +
  specs — `openspec/specs/` is the per-capability doc surface)" — and do not expand CLAUDE.md
  further; bead `if-7cce.2` (CLAUDE.md dirs/tools/mx-broker bundle) is already closed
  (commit `3f9d4b9`) and out of scope here. [type:docs]
  - depends on: 1.1
- [x] [1.3] Write three per-app READMEs: `apps/wavetui/README.md` (Go, `bubbletea` TUI —
  what-it-is from `openspec/specs/wavetui/spec.md` + code, `go build`/`go run` against
  `apps/wavetui/cmd/wavetui` as entry point, `go test ./...`, brief mention of the config
  surface `internal/config/config.go` / `.wavetui.toml` / knobs like
  `ctx_scan_poll_seconds`), `apps/ctx-scan/README.md` (Bun/TS, `bin: ctx-scan -> src/cli.ts`,
  `bun test`, pointer to `openspec/specs/ctx-scan/spec.md`), `apps/daily-brief/README.md`
  (Bun/TS + ink, `bin: daily-brief -> src/index.tsx`, `bun test`, `collect`/`view` scripts,
  pointer to `openspec/specs/daily-brief/spec.md`). Match `apps/cc-tmux/README.md`'s shape
  (what it is, how to build/test, entry point); keep each under ~40 lines; document what
  exists, not aspirational behavior. [type:docs]
- [x] [1.4] Vendored-submodules marker: add `apps/README.md` (or a section in the root
  README's apps subsection) naming `kontroll` and `zsa-voyager-keymap` as pinned-upstream git
  submodules (`.gitmodules` already pins them) — state "submodules are pinned upstream — bump,
  don't edit." Do not move or restructure the submodule paths. [type:docs]
  - depends on: 1.1
- [x] [1.5] (optional, low priority) Add one line to `docs/executables.md`'s existing scope
  statement noting that `apps/*` expose their own binaries (`ctx-scan`, `daily-brief`,
  `wavetui`) documented in their per-app READMEs. Skip if it complicates that file's existing
  scope note — note the skip in the implementation report rather than forcing the edit.
  [type:docs]
  - depends on: 1.3

## E2E Batch

- [x] [2.1] Run the done-criteria greps and confirm each passes: `rg "apps/" README.md`,
  `rg "openspec/specs" README.md CLAUDE.md`, `ls apps/wavetui/README.md
  apps/ctx-scan/README.md apps/daily-brief/README.md`, `rg -i "vendored|submodule"
  apps/README.md`. Confirm `git diff --name-only` shows only `.md` files touched. Note: at
  spec-authoring time this task's purpose is to verify the criteria above are well-formed and
  reproducible commands — it does not itself run against implemented code until `/apply`
  executes batch 1. [type:docs]
  - depends on: 1.1, 1.2, 1.3, 1.4
