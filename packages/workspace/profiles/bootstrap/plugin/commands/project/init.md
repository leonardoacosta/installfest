---
model: sonnet
name: project:init
description:
  Bootstrap project into spec-driven workflow with stack detection, template scaffolding
  (.gitignore, .vscode, CLAUDE.md, settings.json), beads/openspec init, framework cleanup, and
  config validation. Use when setting up a new project or re-running to fill config gaps.
Keywords: bootstrap, init, scaffold, setup, gitignore, vscode, beads, openspec, framework cleanup.
argument-hint: "[--audit]"
allowed-tools: Bash, Read, Edit, Write, AskUserQuestion, Glob, Grep
---

# Project Init

Bootstrap any project into the full spec-driven dev workflow. Detects the stack, gathers project
identity interactively, applies the appropriate CLAUDE.md and settings.json templates, scaffolds
`.gitignore` and `.vscode/settings.json`, initializes beads and openspec, sets up shared symlinks
for T3 projects, removes non-standard frameworks, and validates the result. Pass `--audit` to also
generate audit loop infrastructure.

**Idempotency**: Every step checks existing state before writing. Re-running is safe -- existing
correct setup is preserved, only gaps are filled.

## Arguments

Parse `$ARGUMENTS` for flags:

```bash
AUDIT=false
DOCS=false
for arg in $ARGUMENTS; do
  case "$arg" in
    --audit) AUDIT=true ;;
    --docs) DOCS=true ;;
    *) echo "Unknown argument: $arg" >&2 ;;
  esac
done
```

- No args: full interactive bootstrap (Steps 1-12, skip Step 11 audit phase, skip Step 8.5 docs skeleton)
- `--audit`: also run Step 11 to create audit loop infrastructure
- `--docs`: also run Step 8.5 to scaffold a `docs/` skeleton (advisor-plans/023 canon)
- `--dry-run`: Show what would be created without writing files

---

## Delta Preview Protocol

**MANDATORY -- READ FIRST**: Before executing any step, read
[`references/delta-preview.md`](../workflow/references/delta-preview.md) completely. It defines the
rendering protocol (box format, 5 action types, merge strategies, confirmation flow) that governs
ALL file operations in this command.

**Do NOT** proceed to Step 1 without loading the protocol. Every write, merge, and delete must
render a preview box and get user confirmation before applying.

---

## NEVER

- **NEVER** write a file without rendering the delta preview box first -- even for "obvious" changes
- **NEVER** remove or reorder existing entries during MERGE -- only append missing ones
- **NEVER** delete framework artifacts without the DELETE preview + user confirmation
- **NEVER** overwrite `.claude/settings.json` -- always MERGE (hooks and permissions accumulate)
- **NEVER** run in CI/automated pipelines -- requires interactive AskUserQuestion prompts
- **NEVER** assume stack detection is correct when ambiguous -- confirm with user
- **NEVER** create `.gitignore` entries that conflict with tracked files (check `git ls-files`
  first)
- **NEVER** flag `.agents/` for cleanup without verifying it contains skill-like content (could be a
  legitimate project directory unrelated to AI frameworks)

---

## Step 1: Stack Detection

Scan the current directory for stack indicator files:

```bash
source ~/.claude/scripts/lib/stack-detect.sh
STACK=$(detect_stack)
```

Stack-to-template prefix mapping:

| Stack detected | Template prefix | Display name                           |
| -------------- | --------------- | -------------------------------------- |
| `t3-docker`    | `t3-docker`     | T3 Turbo + Docker (homelab deployment) |
| `t3-turbo`     | `t3-turbo`      | T3 Turbo (Next.js + tRPC + Drizzle)    |
| `go-cli`       | `go-cli`        | Go CLI                                 |
| `dotnet-next`  | `dotnet-next`   | .NET + Next.js                         |
| `bash-infra`   | `bash-infra`    | Bash / Infrastructure                  |
| `terraform`    | `terraform`     | Terraform                              |

If no indicators match, or if multiple could match (ambiguous), use AskUserQuestion:

```
What stack is this project?
1. T3 Turbo + Docker (Next.js + tRPC + Drizzle, Docker/homelab deploy)
2. T3 Turbo (Next.js + tRPC + Drizzle ORM, Vercel deploy)
3. Go CLI (single binary, possibly Bubbletea)
4. .NET + Next.js (C# backend + Next.js frontend)
5. Bash / Infrastructure (Docker, systemd, shell scripts)
6. Terraform (Infrastructure as Code)
```

Set `TEMPLATE_PREFIX` from the result.

---

## Step 1b: Compose Stack Manifest (`.claude/project.toml`)

> Mandated by `compose-stack-toolchain` design.md Decisions 1 + 8. This step writes the
> project's stack composition manifest — the source of truth that `/apply`, `/apply:all`,
> `/audit:services`, and `/audit:journeys` read at dispatch time. Skip only if the project
> deliberately opts to stay on the legacy alias path (rare; document the rationale in the
> project's CLAUDE.md).

### 1b.1 Auto-detect candidate skill set

Probe the working tree for marker files and emit a candidate skill list. The mapping is
mechanical — no heuristics, no guessing. Multiple markers compose into multiple skills.

| Marker file (project root)                       | Candidate skill |
| ------------------------------------------------ | --------------- |
| `turbo.json` AND `pnpm-workspace.yaml`           | `t3`            |
| `bun.lock` OR `bunfig.toml`                      | `bun`           |
| `drizzle.config.{ts,js,mjs}` OR `src/db/schema.ts` | `drizzle`     |
| `vercel.json` OR `.vercel/project.json`          | `vercel`        |
| `Dockerfile` OR `docker-compose.yml`             | `docker`        |
| `*.csproj` OR `*.sln`                            | `dotnet`        |
| `*.tf` OR `*.tfvars`                             | `terraform`     |
| `Package.swift` OR `*.xcodeproj` OR `project.yml` | `swift`        |
| `effect.config.*` OR effect deps in package.json | `effect`        |
| `openspec/` AND `commands/` AND `.claude/`       | `meta`          |
| `next.config.{js,mjs,ts}` (no t3)                | `nextjs`        |
| `go.mod`                                         | `go`            |

```bash
CANDIDATE_SKILLS=()
[ -f turbo.json ] && [ -f pnpm-workspace.yaml ] && CANDIDATE_SKILLS+=(t3)
[ -f bun.lock ] && CANDIDATE_SKILLS+=(bun)
{ [ -f drizzle.config.ts ] || [ -f drizzle.config.js ]; } && CANDIDATE_SKILLS+=(drizzle)
[ -f vercel.json ] && CANDIDATE_SKILLS+=(vercel)
[ -f Dockerfile ] && CANDIDATE_SKILLS+=(docker)
compgen -G "*.csproj" >/dev/null 2>&1 && CANDIDATE_SKILLS+=(dotnet)
compgen -G "*.tf" >/dev/null 2>&1 && CANDIDATE_SKILLS+=(terraform)
{ [ -f Package.swift ] || [ -f project.yml ]; } && CANDIDATE_SKILLS+=(swift)
# ... extend as needed ...
echo "Detected candidate skills: ${CANDIDATE_SKILLS[*]:-<none>}"
```

### 1b.2 Confirm with the user via AskUserQuestion

Show the auto-detected candidate set and ask the user to confirm, edit, or replace. Surface the
category each candidate skill would claim (from its `category:` frontmatter) so the user can see
the implied phase model before approving.

```
Detected stack composition for `<project>`:

  skills = ["t3", "drizzle", "vercel"]

  → Phases:  DB, API, UI, E2E, DOC
  → DB:      drizzle (db-engineer, gate=pnpm db:generate-check)
  → API:     t3      (api-engineer, gate=pnpm tsc --noEmit)
  → UI:      t3      (ui-engineer)
  → Deploy:  vercel  (deploy-engineer, gate=vercel inspect)

Accept this composition? [yes / edit / skip-toml]
```

`edit` re-prompts for a comma-separated skill list; `skip-toml` aborts this step and leaves the
project on the legacy alias path (record the choice in the project's CLAUDE.md).

### 1b.3 Validate BEFORE writing (Decision 8)

Source the composition library and call `validate_project_toml` on the proposed content
written to a temp file. Only if validation passes do we render the delta preview and write the
final `.claude/project.toml`.

```bash
source ~/.claude/scripts/lib/compose-stack.sh

TMP_TOML=$(mktemp -t project-toml.XXXXXX)
cat >"$TMP_TOML" <<EOF
[project]
name = "$PROJECT_NAME"
code = "$PROJECT_CODE"
schema = 1

[stack]
skills = [$(printf '"%s",' "${CONFIRMED_SKILLS[@]}" | sed 's/,$//')]
EOF

if ! validate_project_toml "$TMP_TOML"; then
  echo "ERROR: proposed project.toml failed composition validation." >&2
  echo "Remediation: resolve the conflict printed above (usually an ambiguous" >&2
  echo "  same-level category claim — add [stack.overrides] to disambiguate)" >&2
  echo "  then re-run /project:init." >&2
  rm -f "$TMP_TOML"
  exit 1
fi

# Validation passed → render delta preview per the Delta Preview Protocol
# and write to $PROJECT_ROOT/.claude/project.toml.
mkdir -p "$PROJECT_ROOT/.claude"
mv "$TMP_TOML" "$PROJECT_ROOT/.claude/project.toml"
```

The validator is the same library used by `/apply` and `/apply:all` at dispatch time — so a
manifest that lands via this step is guaranteed to dispatch without revalidation failure.

---

## Step 2: Interactive Configuration

Use AskUserQuestion for each variable. Auto-detect where possible and present the detected value as
the default -- the user can confirm or override.

> See [`references/init-reference.md`](references/init-reference.md) for the full variable table,
> auto-detection sources, and formatting rules.

Set `TEMPLATE_PREFIX` from the result.

---

## Step 3: Apply CLAUDE.md Template

```bash
TEMPLATE_PATH="$HOME/.claude/templates/workflow/${TEMPLATE_PREFIX}.CLAUDE.md.tmpl"
OUTPUT_PATH=".claude/CLAUDE.md"
```

1. Read the template from `~/.claude/templates/workflow/${TEMPLATE_PREFIX}.CLAUDE.md.tmpl`
2. Replace all `{{VARIABLE}}` placeholders with answers from Step 2

> See [`references/init-reference.md`](references/init-reference.md) for the full
> placeholder-to-value mapping table.

3. **Delta Preview**:
   - If `.claude/CLAUDE.md` does not exist -> render `NEW FILE` box -> prompt `[Y]es / [s]kip`
   - If it exists -> render `OVERWRITE` box showing unified diff -> prompt `[Y]es / [e]dit / [s]kip`
   - Apply only after user confirms

---

## Step 4: Apply settings.json Template

```bash
TEMPLATE_PATH="$HOME/.claude/templates/workflow/${TEMPLATE_PREFIX}.settings.json.tmpl"
OUTPUT_PATH=".claude/settings.json"
```

1. Read the template from `~/.claude/templates/workflow/${TEMPLATE_PREFIX}.settings.json.tmpl`
2. Replace `{{PROJECT_CODE}}` with `$PROJECT_CODE`
3. **Delta Preview**:
   - If `.claude/settings.json` does not exist -> render `NEW FILE` box -> prompt `[Y]es / [s]kip`
   - If it exists -> compute MERGE (add missing Stop hooks into existing `hooks.Stop[0].hooks`
     array, deduplicate `permissions.allow` entries) -> render `MERGE` box showing only additions ->
     prompt `[Y]es / [e]dit / [s]kip`
   - Apply only after user confirms

---

## Step 5: Apply .gitignore Template

```bash
TEMPLATE_PATH="$HOME/.claude/templates/workflow/${TEMPLATE_PREFIX}.gitignore.tmpl"
OUTPUT_PATH=".gitignore"
```

1. Read the template from `~/.claude/templates/workflow/${TEMPLATE_PREFIX}.gitignore.tmpl`
2. Replace any `{{VARIABLE}}` placeholders (e.g. `{{BINARY_NAME}}` for Go projects)
3. **Delta Preview**:
   - If `.gitignore` does not exist -> render `NEW FILE` box -> prompt `[Y]es / [s]kip`
   - If it exists -> compute MERGE: a. Parse existing `.gitignore` into a set of active patterns
     (ignore comments, blank lines) b. Parse template into sections (delimited by
     `# --- Section ---` headers) c. For each template section: check if ALL patterns in that
     section already exist in the file
     - If all exist -> add to "Already covered" summary
     - If some/none exist -> add missing patterns to "Additions" with the section header d. Render
       `MERGE` box -> prompt `[Y]es / [e]dit / [s]kip`
   - When applying MERGE: append missing sections to the end of the existing file, preserving
     existing content verbatim. Never reorder or remove existing entries.
   - Apply only after user confirms

> See [`references/init-reference.md`](references/init-reference.md) for the template inventory
> table (stack-to-template mapping and notable patterns).

---

## Step 6: Apply .vscode/settings.json Template (repo-specific only)

```bash
TEMPLATE_PATH="$HOME/.claude/templates/workflow/${TEMPLATE_PREFIX}.vscode.json.tmpl"
OUTPUT_PATH=".vscode/settings.json"
```

**IMPORTANT — globals live elsewhere.** Editor ergonomics (`formatOnSave`, prettier defaults,
ESLint, `typescript.tsdk`, git.*, common excludes, language formatters) are managed centrally
in **`~/dev/if/shared/vscode-user-settings.json`** and deployed to canonical OS locations via
**chezmoi symlink-stubs** under `~/dev/if/home/`. Run `chezmoi apply` to install/refresh
globals — `/project:init` does NOT install globals itself.

The per-project `.vscode/settings.json` should contain ONLY repo-specific overrides:

- Stack-specific `files.exclude` entries (e.g. `vendor`, `.terraform`, `bin/obj`)
- `eslint.workingDirectories` (monorepo package paths)
- `tailwindCSS.experimental.configFile` (per-app config map)
- Stack-specific language formatters via `[language]` selectors (never top-level
  `editor.defaultFormatter` — that clobbers the global Prettier default)
- `extensions.recommendations`

If you find globals (`editor.formatOnSave`, `prettier.requireConfig`, `git.confirmSync`,
`eslint.run`, `eslint.format.enable`, `typescript.tsdk`, top-level `editor.defaultFormatter`,
universal excludes like `node_modules`/`.next`/`dist`/`coverage`) in the existing repo file,
**flag them in the MERGE preview as REMOVABLE** — they are now redundant with the global
config and should be deleted to avoid drift.

1. Read the template from `~/.claude/templates/workflow/${TEMPLATE_PREFIX}.vscode.json.tmpl`
2. **Delta Preview**:
   - If `.vscode/settings.json` does not exist -> `mkdir -p .vscode` -> render `NEW FILE` box ->
     prompt `[Y]es / [s]kip`
   - If it exists -> compute MERGE: a. Parse existing JSON b. For each key in the template: check if
     it already exists in the file
     - `files.exclude`: merge individual exclude entries (add missing keys, preserve existing)
     - Top-level keys (editor, typescript, etc.): add if missing, skip if present c. Detect any
       globalized keys present in the existing file and list them under a "REDUNDANT (now in
       ~/dev/if/shared/vscode-user-settings.json — deployed via chezmoi)" section of the
       MERGE preview d. Render `MERGE` box showing additions + redundant keys -> prompt
       `[Y]es / [e]dit / [s]kip`
   - When applying MERGE: add missing keys to the existing JSON structure. Removal of redundant
     globalized keys requires explicit user opt-in via `[e]dit`; default is preserve.
   - Apply only after user confirms
3. **Advisory check**: If `~/dev/if/shared/vscode-user-settings.json` is missing on this
   machine, warn the user that the chezmoi-managed dotfiles repo (`~/dev/if`) is not present —
   the per-project settings here assume it is. Suggest:
   ```bash
   git clone <if-repo> ~/dev/if && chezmoi init --source=~/dev/if && chezmoi apply
   ```
   Do NOT block the rest of the steps.

> See [`references/init-reference.md`](references/init-reference.md) for the template inventory
> table (stack-to-template mapping and notable settings).

---

## Step 7: Initialize Beads

```bash
if [ ! -d .beads ]; then
  bd init
else
  echo ".beads/ already exists -- skipping bd init"
fi
```

**Delta Preview**:

- If `.beads/` does not exist -> render `NEW FILE` box listing the files `bd init` will create
  (`beads.toml`, `issues.jsonl`, `interactions.jsonl`, `.gitattributes`) -> prompt `[Y]es / [s]kip`
- If `.beads/` exists -> render `NO CHANGE` box -> auto-continue

If `bd init` fails (command not found), warn the user: "bd CLI not found. Install beads first, then
run `bd init` manually."

---

## Step 8: Initialize OpenSpec

```bash
mkdir -p openspec/changes openspec/specs
```

**openspec/AGENTS.md** -- if missing, check for the global template:

```bash
if [ ! -f openspec/AGENTS.md ]; then
  if [ -f "$HOME/.claude/openspec/AGENTS.md" ]; then
    cp "$HOME/.claude/openspec/AGENTS.md" openspec/AGENTS.md
  else
    # Create minimal AGENTS.md skeleton
    Write openspec/AGENTS.md with a minimal skeleton explaining:
    - That this project uses OpenSpec for spec-driven development
    - How to create a change proposal (openspec/changes/<spec-name>/proposal.md)
    - How to run /feature and /apply
  fi
fi
```

**openspec/project.md** -- if missing, create a skeleton:

```
# $PROJECT_NAME -- Project Reference

## Domain Model

$DOMAIN_TERMS (as table)

## Key User Journeys

$KEY_ROUTES (as table)

## Data Scope

$DATA_SCOPE

## Architecture Notes

> Fill in architecture notes here after /project:discover runs.
```

**Delta Preview**:

- If `openspec/` does not exist -> render `NEW FILE` box listing all files that will be created
  (AGENTS.md, project.md, changes/, specs/) -> prompt `[Y]es / [s]kip`
- If `openspec/` exists but some files are missing -> render `MERGE` box listing only new files ->
  prompt `[Y]es / [s]kip`
- If everything exists -> render `NO CHANGE` box -> auto-continue

---

## Step 8.5: Docs Skeleton (`--docs` flag only)

**advisor-plans/023 (2026-07-04):** scaffolds the canon documented in the `documentation-writer`
skill's `references/operational-docs-canon.md` — the four directory roles (`reference/`,
`notes/`, `guides/`, `diagrams/`) plus a README stub pointing back at the canon. Does NOT
retrofit an existing project's `docs/` tree — skip entirely if `docs/` already exists (this step
is for greenfield scaffolding only; see that reference for auditing an existing tree).

```bash
if [ "$DOCS" = "true" ] && [ ! -d docs ]; then
  mkdir -p docs/reference docs/notes/archive docs/guides docs/diagrams
  cat > docs/README.md <<'EOF'
# docs/

Four directories, four roles — see the `documentation-writer` skill's
`references/operational-docs-canon.md` for the full canon (directory roles, frontmatter
contract, dated self-correction banners, machine-artifact convention, vendor-junk ban).

| Directory | Role |
| --- | --- |
| `reference/` | How-it-IS state per subsystem (some pages are machine-written) |
| `notes/` | Dated investigation journal (superseded notes move to `notes/archive/`) |
| `guides/` | Runbooks — step-by-step operational procedures |
| `diagrams/` | Visual explainers referenced from the other three |

Every page opens with frontmatter: `title`, `type`, `domain`, `tags`, `status`, `updated`.
EOF
elif [ "$DOCS" = "true" ]; then
  echo "docs/ already exists — Step 8.5 does not retrofit an existing tree, skipping."
fi
```

**Delta Preview**:

- `--docs` and `docs/` absent -> render `NEW FILE` box listing the four dirs + `docs/README.md`
  -> prompt `[Y]es / [s]kip`
- `--docs` and `docs/` already exists -> print the skip line above, no prompt
- No `--docs` flag -> Step 8.5 does not run at all

---

## Step 9: Shared Symlinks (T3 only)

Only for `STACK="t3-turbo"` or `STACK="t3-docker"`. Skip entirely for other stacks.

Create symlinks from project `.claude/` to global shared directories (agents, skills, helpers,
rules).

> See [`references/init-reference.md`](references/init-reference.md) for the symlink creation script
> and delta preview format.

---

## Step 9.5: Seed Test User (Web UI projects only)

Only for stacks with a web UI (`t3-turbo`, `t3-docker`, `dotnet-next`). Skip entirely for `go-cli`,
`bash-infra`, and `terraform`.

**Detection:**

```bash
HAS_WEB_UI=false
[ -d "apps/nextjs" ] || [ -d "apps/web" ] || [ -d "apps/app" ] && HAS_WEB_UI=true
```

If `HAS_WEB_UI=false`, skip this step and auto-continue.

**Purpose:** Ensures a platform-owner test user exists on the deployed dev environment before smoke
verification runs (Phase 9 of `apply:all`). The test credentials are used by browser agents to
verify UI changes after deploys.

**Default credentials:**

| Field    | Default                             |
| -------- | ----------------------------------- |
| Email    | `platform-owner@test.<project>.com` |
| Password | `Test123!`                          |
| Role     | `platformOwner`                     |

**Delta Preview:** Render a `NEW STEP` box showing the credentials that will be seeded:

```
┌─ SEED TEST USER ──────────────────────────────────────────┐
│  Email:    platform-owner@test.<project>.com              │
│  Password: Test123!                                       │
│  Role:     platformOwner                                  │
│  Target:   $OO_BASE_URL (or equivalent dev environment)   │
└───────────────────────────────────────────────────────────┘
```

Prompt: `[Y]es — seed test user / [s]kip — I'll configure credentials manually`

If confirmed, run:

```bash
/audit:seed-user platform-owner@test.${PROJECT_CODE}.com Test123! platformOwner
```

If `audit:seed-user` reports the user already exists (idempotent), render `NO CHANGE` and
auto-continue.

If the command fails (e.g., dev environment not yet deployed), warn the user:

```
Warning: Could not seed test user — dev environment may not be deployed yet.
Run /audit:seed-user manually after deploying: /audit:seed-user platform-owner@test.<project>.com Test123! platformOwner
```

Do NOT block the remaining steps on this failure.

---

## Step 9.6: DESIGN.md — Design Source of Truth (Web UI projects only)

Only for stacks with a web UI (`t3-turbo`, `t3-docker`, `dotnet-next`). Skip entirely for
`go-cli`, `bash-infra`, and `terraform` (reuse the `HAS_WEB_UI` detection from Step 9.5).

**Purpose:** Place a `DESIGN.md` at the project root so the CLAUDE.md "Design Source of Truth"
section (Step 3), the `frontend-design` skill, and the `ui-engineer`/`ux-specialist` agents have a
normative design contract to read before any frontend work.

**Resolution order:**

```bash
ROOT_DESIGN="DESIGN.md"
PLAN_DESIGN=$(ls docs/plan/*/brand/DESIGN.md 2>/dev/null | head -1)

if [ -f "$ROOT_DESIGN" ]; then
  : # existing root DESIGN.md — preserve (NO CHANGE)
elif [ -n "$PLAN_DESIGN" ]; then
  : # copy plan-pipeline DESIGN.md to root (NEW FILE)
else
  : # scaffold a minimal valid stub (NEW FILE)
fi
```

1. **Existing root `DESIGN.md`** → render a `NO CHANGE` box and auto-continue. NEVER overwrite.
2. **Plan-pipeline `DESIGN.md` exists** (`docs/plan/<name>/brand/DESIGN.md`) and no root file →
   render a `NEW FILE` delta preview, then on confirm `cp "$PLAN_DESIGN" DESIGN.md`.
3. **Neither exists** → render a `NEW FILE` preview for a minimal valid stub:

```md
---
version: alpha
name: {{PROJECT_NAME}}
colors:
  primary: "#2563EB"
  on-primary: "#FFFFFF"
  neutral: "#FAFAFA"
typography:
  body-md:
    fontFamily: Inter
    fontSize: 16px
    fontWeight: 400
    lineHeight: 1.6
---

## Overview

Placeholder design system for {{PROJECT_NAME}}. Run `/plan:design` to generate a complete,
lint-validated design system (full palette, typography scale, components, and rationale), or
edit this file by hand. This file is the source of truth for all frontend work — see the
"Design Source of Truth" section in `.claude/CLAUDE.md`.

## Do's and Don'ts

- Do replace this stub before building production UI
- Do keep derived token files (`tokens.css`, Tailwind theme) generated from this file
```

**Delta Preview:** render the appropriate box (`NEW FILE` for copy/stub, `NO CHANGE` for
existing) per the Delta Preview Protocol. Prompt `[Y]es / [s]kip`.

**Validation (optional, graceful-degrade):** after writing, lint the result; surface errors,
do not block:

```bash
npx --yes @google/design.md lint DESIGN.md 2>/dev/null || \
  echo "design.md CLI unavailable — skipping lint (non-blocking)"
```

> See [`references/init-reference.md`](references/init-reference.md) for the stub rationale and
> the per-stack web-UI detection note.

---

## Step 9.7: Fallow Configuration (JS/TS projects only)

Only for stacks where `package.json` exists at root and the project has JS/TS source
(`t3-turbo`, `t3-docker`, or any stack with `apps/*`/`packages/*` workspaces).

**Detection:**

```bash
HAS_JS=false
[ -f "package.json" ] && HAS_JS=true
```

If `HAS_JS=false`, skip this step.

**Idempotency check:** If any of `.fallowrc.json`, `.fallowrc.jsonc`, `fallow.toml`, or
`.fallow.toml` exists at the project root, render NO CHANGE and auto-continue.

**Scope + workspace discovery:**

```bash
# Try root package name (works when root is scoped: @oo/root → scope=oo)
SCOPE=$(jq -r '.name' package.json | sed -nE 's|^@([^/]+)/.*|\1|p')

# Fallback: most T3 roots are unscoped (e.g. "otaku-odyssey"). Infer from first @scope/* workspace.
if [ -z "$SCOPE" ]; then
  SCOPE=$(find packages apps -maxdepth 2 -name package.json -not -path '*/node_modules/*' \
    -exec jq -r '.name' {} + 2>/dev/null \
    | grep -oE '^@[^/]+' | head -1 | sed 's/^@//')
fi
[ -z "$SCOPE" ] && SCOPE=$(basename "$PWD")

WORKSPACE_PACKAGES=$(find packages apps -maxdepth 2 -name package.json \
  -not -path '*/node_modules/*' -exec jq -r '.name' {} + 2>/dev/null \
  | grep "^@${SCOPE}/" | sort -u)
```

**Delta Preview:** Render `NEW FILE .fallowrc.json` showing the resolved config with `$SCOPE`
substituted into `publicPackages` and discovered workspace package names listed. Use the
template + rationale in [`references/fallow-config.md`](references/fallow-config.md).

Also render an `EDIT .gitignore` box appending `.fallow/` (the cache directory) if not already
present.

Prompt: `[Y]es — write .fallowrc.json + .gitignore entry / [s]kip — I'll configure manually`

On confirm:

1. Write `.fallowrc.json` to project root
2. Append `.fallow/` to `.gitignore` (MERGE — never reorder)
3. Optional sub-prompt: `Install fallow as a dev dependency? [Y/n]` — defaults YES for
   `t3-turbo`, NO for other stacks. If yes, run `pnpm add -Dw fallow@latest`.

**Validation:**

```bash
pnpm exec fallow config --format json --quiet 2>/dev/null || npx fallow config --format json --quiet 2>/dev/null
```

Non-zero exit = JSON parse error or fallow not installed. Roll back the write and surface the
error.

> See [`references/fallow-config.md`](references/fallow-config.md) for the full template
> rationale, per-stack additions, boundaries preset selection, `usedClassMembers` patterns,
> pnpm catalog handling, and regression-baseline migration for legacy repos.

---

## Step 9.9: Worktree-Isolation Configs

Emit per-project configuration enabling concurrent `/apply` sessions to run inside isolated git
worktrees at `<repo>/.worktrees/<session-id>/`:

```bash
~/.claude/scripts/bin/worktree-config-writer apply "$PROJECT_ROOT"
```

This writes to `.gitignore`, `.rgignore`, `.vscode/settings.json`, `tsconfig.json`,
`pnpm-workspace.yaml`, and `next.config.js` (where they exist), tagging each modification with
`# managed: worktree-isolation`. Idempotent — safe to re-run.

For pnpm projects, the writer additionally runs `preflight-pnpm` before setting
`enableGlobalVirtualStore: true` — on preflight failure, the flag is skipped and the project
falls back to per-worktree `node_modules` install (higher disk cost, isolation still works).

**Delta Preview**: render a `MERGE` box per touched file with the worktree-isolation entries.
Prompt `[Y]es / [s]kip` once for the whole payload.

---

## Step 10: Framework & Plugin Cleanup

Remove non-standard Claude plugins, AI frameworks, and their configuration artifacts.

> See [`references/init-reference.md`](references/init-reference.md) for detection and removal
> steps. Load [`references/framework-artifacts.md`](../workflow/references/framework-artifacts.md)
> for the full artifact catalog.

---

## Step 11: Audit Infrastructure (--audit flag only)

Only run this step if `AUDIT=true`. Discovers domain names from the codebase, generates
`domains.json`, creates audit command files and per-domain audit skeletons.

> See [`references/init-reference.md`](references/init-reference.md) for domain discovery scripts
> (per stack), template placeholders, and audit skeleton format.

---

## Step 12: Validate

**MANDATORY -- LOAD**: Read [`references/validation.md`](../workflow/references/validation.md) for
the full scoring rubric (8 checks, 100 points) and output format.

Run all 8 config health checks and compute the score.

> See [`references/init-reference.md`](references/init-reference.md) for the full scoring rubric,
> output format, and remediation guidance.

---

## Output

> See [`references/init-reference.md`](references/init-reference.md) for the output summary format.

Mark each line with the actual outcome (applied, skipped by user, already existed, failed).

```
Next: /next to verify | /project:discover to gather context | /plan:scope to start planning |
/feature to start building
```

---

## Error Handling

> See [`references/init-reference.md`](references/init-reference.md) for the full error handling
> table.

All steps are independent -- a failure in any step does not block subsequent steps. Report all
failures in the output summary.
