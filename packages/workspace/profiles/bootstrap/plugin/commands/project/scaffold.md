---
model: opus
name: project:scaffold
description: Scaffold a new T3 Turbo project with full workflow integration (PRD discovery, beads, openspec)
effort: high
argument-hint: "[project-name]"
allowed-tools: Read, Bash, Write, Edit, AskUserQuestion
---

# /scaffold Command

Scaffold a production-ready T3 Turbo project with complete Claude workflow integration.

## Usage

```bash
/scaffold <project-name> [--prd=<path>] [--path=<path>] [--no-install]
```

**Arguments:**

- `project-name` (required): Lowercase alphanumeric with hyphens (e.g., `my-app`)
- `--prd=<path>`: Path to existing PRD markdown file (skips interactive discovery)
- `--path=<path>`: Custom install path (default: `~/personal/$NAME`)
- `--no-install`: Skip `pnpm install` step

---

## Phase 1: Validation

### Step 1.1: Parse Arguments

Extract from user input:

- `$NAME` = project name (first argument)
- `$PRD_PATH` = --prd value (optional)
- `$PROJECT_PATH` = --path value OR `~/personal/$NAME`
- `$NO_INSTALL` = --no-install flag present

### Step 1.2: Validate Inputs

```bash
# Validate project name (lowercase alphanumeric + hyphens)
if ! echo "$NAME" | grep -qE '^[a-z][a-z0-9-]*$'; then
  echo "Error: Project name must be lowercase alphanumeric with hyphens"
  exit 1
fi

# Check target path doesn't exist
if [ -d "$PROJECT_PATH" ]; then
  echo "Error: Directory already exists: $PROJECT_PATH"
  echo "Use a different name or remove the existing directory"
  exit 1
fi
```

**Gate 1:** Valid project name and available target path

---

## Phase 2: PRD Discovery

### If `--prd=<path>` provided

Read the PRD file and parse into structured sections:

```bash
# Read PRD file
PRD_CONTENT=$(cat "$PRD_PATH")
```

Parse these sections from the markdown:

- `## Purpose` or `## Problem`
- `## Users` or `## Target Users`
- `## Features` or `## MVP Features`
- `## Data Model` or `## Entities`
- `## User Journeys` or `## Flows`
- `## Integrations` (optional)
- `## Design` (optional)
- `## Constraints` (optional)

### If no PRD provided -> Interactive Discovery

Ask these questions sequentially using AskUserQuestion:

**Question Set 1: Purpose & Problem**

```claude-code
Questions:
1. "What problem does this app solve?"
   Options: [Free text response expected]

2. "Who are the target users?"
   Options:
   - "Consumers (B2C)"
   - "Businesses (B2B)"
   - "Internal team"
   - "Developers (API/Platform)"
```

**Question Set 2: Core Features**

```claude-code
Question: "Which features are needed for MVP?"
multiSelect: true
Options:
- "User authentication (signup, login, profile)"
- "Payments (Stripe checkout, subscriptions)"
- "Real-time updates (WebSockets, live data)"
- "File uploads (images, documents)"
- "Admin dashboard"
- "API for third-party integrations"
- "Email notifications"
- "Search functionality"
```

**Question Set 3: Data Model**

```claude-code
Question 1: "What are the main data entities?"
Options: [Free text - e.g., "Users, Products, Orders, Reviews"]

Question 2: "What's the data scoping model?"
Options:
- "Single tenant (one instance)"
- "Multi-tenant (organizations/teams)"
- "User-scoped (personal data per user)"
```

**Question Set 4: User Journeys**

```claude-code
Question 1: "Describe the primary user journey (happy path)"
Options: [Free text - e.g., "User signs up -> browses products -> adds to cart -> checks out"]

Question 2: "Any secondary journeys?"
multiSelect: true
Options:
- "Admin management flow"
- "Guest/anonymous flow"
- "Onboarding flow"
- "Settings/preferences flow"
```

**Question Set 5: Integrations**

```claude-code
Question: "Which external services will you integrate?"
multiSelect: true
Options:
- "Stripe (payments)"
- "OAuth providers (Google, GitHub, etc.)"
- "Resend (transactional email)"
- "Cloudflare R2/S3 (file storage)"
- "Mapbox/Google Maps"
- "PostHog/Analytics"
- "Sentry (error tracking)"
```

**Question Set 6: Design Preferences**

```claude-code
Question: "Any specific design requirements?"
multiSelect: true
Options:
- "Dark mode support"
- "Mobile-first responsive"
- "WCAG AA accessibility"
- "WCAG AAA accessibility"
```

**Question Set 7: Constraints (Optional)**

```claude-code
Question: "Any technical constraints?"
multiSelect: true
Options:
- "PostgreSQL required" (Neon is the canonical provider for new T3 Turbo projects)
- "Must deploy to Vercel"
- "HIPAA compliance needed"
- "GDPR compliance needed"
- "Offline support required"
```

**Output:** Store responses as structured PRD data for template generation.

**Gate 2:** PRD data collected (either from file or interactive)

---

## Phase 3: Create T3 Project

### Step 3.1: Run create-turbo

```bash
cd ~/personal
npx create-turbo@latest $NAME -e https://github.com/t3-oss/create-t3-turbo
```

This creates the full T3 Turbo monorepo structure:

- `apps/nextjs/` - Next.js web app
- `apps/expo/` - React Native mobile app
- `packages/api/` - tRPC routers
- `packages/auth/` - Better-Auth
- `packages/db/` - Drizzle schemas
- `packages/ui/` - Shared components
- `packages/validators/` - Zod schemas
- `tooling/` - ESLint, Prettier, TypeScript configs

### Step 3.2: Verify Structure

```bash
ls "$PROJECT_PATH/apps/" "$PROJECT_PATH/packages/" "$PROJECT_PATH/tooling/"
```

**Gate 3:** Project directory exists with apps/, packages/, tooling/

---

## Phase 4: Init Claude Config

```bash
bd init-claude "$PROJECT_PATH"
```

This creates:

- `.claude/` directory with symlinks to `~/.claude/shared/`
- `.claude/settings.json` - Hook configuration

**Gate 4:** `.claude/` directory configured

---

## Phase 5: Init Beads

```bash
cd "$PROJECT_PATH" && bd init
```

Creates tracked files:

- `.beads/config.yaml` - Issue prefix, daemon settings
- `.beads/README.md` - Quick-start docs
- `.beads/.gitignore` - Excludes runtime files
- `.beads/metadata.json` - Database metadata
- `.beads/issues.jsonl` - Issue source of truth (empty)
- `.beads/interactions.jsonl` - Interaction history (empty)

**Gate 5:** `.beads/` initialized

---

## Phase 6: Create OpenSpec Structure

### Step 6.1: Create Directories

```bash
cd "$PROJECT_PATH"
mkdir -p openspec/flows/{admin,applications,public,utility}
mkdir -p openspec/specs
mkdir -p openspec/changes/archive
```

### Step 6.2: Create project.md (PRD-Populated)

> See [`references/scaffold-reference.md`](references/scaffold-reference.md) for the project.md template.

### Step 6.3: Create AGENTS.md

> See [`references/scaffold-reference.md`](references/scaffold-reference.md) for the AGENTS.md template.

### Step 6.4: Create flows/README.md

Write `openspec/flows/README.md` using PRD journeys.

**Gate 6:** OpenSpec structure created with PRD-populated content

---

## Phase 7: Create Roadmap

> See [`references/scaffold-reference.md`](references/scaffold-reference.md) for the roadmap.md template.

**Gate 7:** Roadmap created with PRD features

---

## Phase 8: Update Project Registry

Edit `~/.claude/CLAUDE.md` to add new project to registry table.

Insert after the last project entry:

```markdown
| `$NAME` | `~/personal/$NAME` | T3 Turbo | pnpm | — | ✅ |
```

**Gate 8:** Project added to global registry

---

## Phase 9: Install Dependencies (Background)

If `--no-install` flag NOT provided:

**Copy environment template first:**

```bash
cd "$PROJECT_PATH"
if [ -f ".env.example" ]; then
  cp .env.example .env
  echo "Created .env from .env.example - edit with your values"
fi
```

**Launch background install:**

```bash
# Background install - continues while you work
Bash({
  command: "cd \"$PROJECT_PATH\" && pnpm install",
  run_in_background: true,
  description: "Install project dependencies"
})
```

**Completion notification:**
When install completes, announce via TTS:
```bash
. ~/.claude/scripts/lib/nx-send.sh
nx_send '{"event":"notification","message":"Dependencies installed for $NAME. Ready for pnpm dev.","message_type":"brief","channels":["tts"]}'
```

**If blocking needed:**
Use `TaskOutput({ task_id: installTaskId, block: true })` to wait for completion before proceeding.

**Gate 9:** Install launched (background) OR skipped (--no-install), .env created

---

## Phase 10: Git Commit

> Note: Git commit proceeds while install runs in background. Install failures don't block commit.

```bash
cd "$PROJECT_PATH"
git add .claude .beads openspec roadmap.md
git commit -m "$(cat <<'EOF'
chore: add claude workflow integration

- Initialize beads issue tracking
- Create openspec structure with project context
- Add roadmap with MVP features
- Configure Claude Code hooks
EOF
)"
```

**Gate 10:** Workflow files committed

---

## Phase 11: Completion

> See [`references/scaffold-reference.md`](references/scaffold-reference.md) for the completion summary template and TTS notification.

---

## Error Handling

| Phase | Error | Recovery |
|-------|-------|----------|
| 1 | Invalid name | Show format requirements, exit |
| 1 | Path exists | Suggest new name, exit |
| 3 | create-turbo fails | Show error, cleanup partial, exit |
| 4 | bd init-claude fails | Warn, continue (manual setup possible) |
| 5 | bd init fails | Warn, continue |
| 9 | pnpm install fails | Warn, continue |
| 10 | git commit fails | Warn about uncommitted changes |

---

## PRD File Format

> See [`references/scaffold-reference.md`](references/scaffold-reference.md) for the PRD file format specification.
