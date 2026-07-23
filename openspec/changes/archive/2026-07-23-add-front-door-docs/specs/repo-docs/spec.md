# repo-docs Specification

## Purpose
Front-door discoverability of this repo's structure: the `apps/` layer (first-party tools plus
vendored submodules), the `openspec/specs/` capability-spec surface, and which entries under
`apps/` are pinned upstream code versus maintained first-party code. This capability governs
the entry-level documentation surface (`README.md`, `CLAUDE.md`, per-app READMEs) rather than
any runtime behavior.

## ADDED Requirements

### Requirement: The root README surfaces the apps/ layer and the openspec capability-spec surface
The root `README.md` directory map SHALL list `apps/`, `packages/`, `shared/`, `infra/`, and `openspec/` alongside the existing `home/`, `platform/`, `scripts/`, `ssh-mesh/`, and `docs/` entries, following the same fenced-tree, one-line-per-entry annotation style already used for the `home/` block, and SHALL also state that per-capability behavior is specified under `openspec/specs/<capability>/spec.md` (as a dedicated subsection or a line in an existing "where things are documented" spot); `CLAUDE.md` SHALL carry the equivalent `openspec/specs/`-as-doc-surface pointer as a single added clause on its existing directory-layout sentence, with no other CLAUDE.md content added or restructured by this requirement.

#### Scenario: apps/ appears in the README directory map
- Given: `README.md`'s fenced directory tree
- When: a reader scans the tree for the `apps/` directory
- Then: an `apps/` entry is present with a one-line annotation naming the first-party apps and
  marking `kontroll` and `zsa-voyager-keymap` as vendored submodules

#### Scenario: openspec/specs/ is discoverable from both entry docs
- Given: a reader who wants to find a capability's behavioral spec
- When: they run `rg "openspec/specs" README.md CLAUDE.md`
- Then: both files contain a hit pointing at `openspec/specs/` as the per-capability doc surface

### Requirement: Each active first-party app has a README naming its build/test command and entry point
Every active first-party app under `apps/` that is not a vendored submodule — currently `cc-tmux` (existing), `wavetui`, `ctx-scan`, and `daily-brief` — SHALL have an `apps/<name>/README.md` stating, at minimum, a one-paragraph description of what the app is (drawn from its actual code and its `openspec/specs/<name>/spec.md`, never invented), its build command, its test command, its entry point, and a pointer to its own `openspec/specs/<name>/spec.md` where one exists; each README SHALL stay short (target: under ~40 lines) and SHALL NOT document aspirational behavior the code does not implement.

#### Scenario: wavetui's README names its Go build/test commands
- Given: `apps/wavetui/README.md`
- When: a reader looks for how to build and test it
- Then: the README names `go build`/`go run` against `apps/wavetui/cmd/wavetui` as the entry
  point and `go test ./...` as the test command, and points at
  `openspec/specs/wavetui/spec.md`

#### Scenario: ctx-scan and daily-brief READMEs name Bun commands
- Given: `apps/ctx-scan/README.md` and `apps/daily-brief/README.md`
- When: a reader looks for how to build and test each
- Then: each README names its `bin` entry point (`src/cli.ts` / `src/index.tsx`) and `bun test`
  as the test command, and points at its own `openspec/specs/<name>/spec.md`

### Requirement: Vendored submodules are marked distinctly from first-party code
The repo SHALL carry one clear, discoverable marker distinguishing vendored git submodules under `apps/` (`kontroll`, `zsa-voyager-keymap`) from first-party apps — either a dedicated `apps/README.md` or an equivalent apps subsection in the root README — stating that these entries are pinned upstream submodules that should be bumped, not edited; this requirement covers documentation only and does not restructure `.gitmodules` or move any submodule path.

#### Scenario: a reader checks whether an apps/ entry is safe to edit
- Given: `apps/README.md` (or the README's apps subsection)
- When: a reader (human or agent) looks up `kontroll` or `zsa-voyager-keymap`
- Then: the doc names both as vendored, pinned-upstream submodules and states they should be
  bumped rather than edited in place
