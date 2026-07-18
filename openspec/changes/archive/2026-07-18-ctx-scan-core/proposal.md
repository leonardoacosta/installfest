---
order: 0718a
---

# Proposal: ctx-scan Core — Discovery, Config Resolution, Data Model

## Change ID
`ctx-scan-core`

## Summary
Scaffold `apps/ctx-scan`, a TypeScript + commander.js CLI, with fleet discovery across `~/dev`,
a 5-layer settings-precedence resolver, global-vs-project layer attribution, and the
schema-versioned `Fleet → Project → Surface → Node` data model every later `ctx-scan` proposal
(loading-rule engine, rubric budgets, the drill-down visual, the references explorer, watch mode)
builds on.

## Context
- Extends: nothing — new app tree
- Related: `telemetry-hook-bytes` (external proposal, `~/dev/cc` repo — ships the hook byte-size
  telemetry `ctx-scan-assembly`, a later proposal in this chain, will consume)
- touches: `apps/ctx-scan/package.json`, `apps/ctx-scan/tsconfig.json`, `apps/ctx-scan/src/cli.ts`,
  `apps/ctx-scan/src/discovery.ts`, `apps/ctx-scan/src/settings-resolver.ts`,
  `apps/ctx-scan/src/model.ts`, `apps/ctx-scan/test/fixtures/**`

> No `- depends on:` — first proposal in this repo's `ctx-scan` chain. It has a real but
> cross-repo predecessor, `telemetry-hook-bytes` (`~/dev/cc`), which cannot be expressed as an
> in-repo dependency line since `/triage` only resolves slugs within this repo's own
> `openspec/changes/`. `ctx-scan-core` itself does not consume that proposal's output —
> `ctx-scan-assembly` (two proposals downstream) is the first consumer.

## Motivation
`~/dev` currently has no tool that measures what a Claude Code session actually loads per
project versus what raw file sizes suggest. Companion doc `context-budget-rubric.md` defines the
budget bands; `ctx-scan` is the scanner that measures against them. This first proposal exists to
get the foundational plumbing right before any loading-rule logic is written, because three of
the roadmap's named failure modes are foundational, not incidental: phantom projects from scanning
vendored/archived/worktree directories (mistake #2), double-counting the global `~/.claude` layer
once per project (mistake #3), and treating raw file bytes as the measurement axis before any
tiering exists to say which bytes actually matter (mistake #1 — guarded here by giving every
`Node` a `tier` field from day one, even though the tier-assignment *logic* ships in
`ctx-scan-assembly`).

## Requirements

### Requirement: Commander.js CLI skeleton
The system SHALL provide a `ctx-scan` binary built on commander.js with a `scan` subcommand
accepting `--root <path>` (default `~/dev`) and `--json <path>` (default: stdout).

### Requirement: Fleet discovery with exclusions
`ctx-scan scan` SHALL discover project roots under `--root` as any directory containing
`CLAUDE.md`, `.claude/`, or `.mcp.json`, deduped to the outermost git root, while excluding
`node_modules`, `.git`, `archive*`/`*-archive`/`archived`, `plugins/cache`, `plugins/marketplaces`,
`.worktrees`, `dist`, and `build` at any depth, with symlink cycles guarded via a realpath set.

### Requirement: Settings precedence resolver
The system SHALL resolve, for every effective setting in `.claude/settings.json`,
`.claude/settings.local.json`, `.mcp.json` (and root `mcp.json` where present), plus the user
layer (`~/.claude/settings.json`), the winning layer in precedence order `managed → CLI →
.claude/settings.local.json → .claude/settings.json → ~/.claude/settings.json`, and record which
layer won for every key.

### Requirement: Global-layer identification
`~/.claude` (or the real path its `~/dev` symlink target resolves to, via `realpath`) SHALL be
scanned exactly once as the GLOBAL layer and SHALL NOT appear as a discovered project in the
fleet listing.

### Requirement: Schema-versioned Fleet/Project/Surface/Node data model
The system SHALL define TypeScript types for `Fleet → Project → Surface(class) → Node(document)`,
where every `Node` carries `{path, cls, tier, raw_chars, effective_chars, est_tokens, origin:
"global"|"project", truncations: [], bands: []}`, `cls` is one of the 13 classes (`system-prompt`,
`system-tools`, `claude-md-chain`, `rules-import`, `agents`, `skills-listing`,
`commands-listing`, `skill-bodies`, `mcp-tools`, `hooks-injected`, `memory`, `output-style`,
`plugins`), and the overall JSON document carries a `schemaVersion` integer field.

## Scope
- **IN**: CLI skeleton, discovery + exclusions, settings precedence resolution (structural —
  recording which layer won, not yet the truncation-aware assembly logic), global-layer
  identification, the versioned data model and its fixture-backed snapshot test.
- **OUT**: the CLAUDE.md `@import` chain walker, truncation math, MCP/memory caps, and
  invocation-ordering (`ctx-scan-assembly`); rubric bands (`ctx-scan-budgets`); the HTML
  renderer (`ctx-scan-render`); the references explorer (`ctx-scan-refs`); watch mode
  (`ctx-scan-watch`).

## Testing
| Affected seam | Unit task | E2E task |
|----------------|-----------|----------|
| Fleet discovery + exclusions | `[4.1]` fixture tree with vendored/archive/worktree dirs | `[4.1]` asserts zero phantom projects |
| Global-layer dedupe | `[4.2]` symlinked `~/.claude` fixture | `[4.2]` asserts single global entry |
| Settings precedence resolver | `[4.3]` layered fixture settings files | N/A — pure resolution logic, no user-facing flow |
| Data model schema | `[4.4]` snapshot fixture locks the schema shape | N/A — schema stability is the unit assertion itself |

## Impact
| Area | Change |
|------|--------|
| `apps/ctx-scan/` | New app — CLI skeleton, discovery, settings resolver, data model |
| `apps/` | Gains a fifth sibling app alongside `cc-tmux`, `daily-brief`, `kontroll`, `zsa-voyager-keymap` |

## Risks
| Risk | Mitigation |
|------|-----------|
| Discovery walks a huge `~/dev` tree slowly | Exclusion list applied at directory-descent time (skip, not post-filter); `[4.5]` asserts a warm-run time budget |
| Realpath-based dedup misses an edge-case symlink layout | `[4.2]`'s fixture specifically exercises the `~/.claude` → `~/dev/cc` symlink shape already live on this machine |
| Settings resolver silently drops a layer on a malformed JSON file | Resolver reports a per-file parse error in its output rather than throwing, so one bad settings file doesn't abort the whole scan |
