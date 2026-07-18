---
order: 0718c
---

# Proposal: ctx-scan Loading-Rule Assembly Engine

## Change ID
`ctx-scan-assembly`

## Summary
Implement the truncation-aware loading-rule engine that turns `ctx-scan-core`'s bare discovery
output into real `effective_chars`/`est_tokens` numbers: the CLAUDE.md `@import` chain walker,
nested-CLAUDE.md subtree mapping, skill/command/agent frontmatter parsing with listing
truncation, MEMORY.md and MCP-description caps, plugin-surface ingestion, hook-size ingestion
from `telemetry-hook-bytes`, and a telemetry-backed (or static-fallback) calibration mode.

## Context
- Extends: `apps/ctx-scan/src/model.ts`, `apps/ctx-scan/src/discovery.ts` (both from
  `ctx-scan-core`)
- Related: `telemetry-hook-bytes` (`~/dev/cc`, external — this proposal's hook-size ingestion
  consumes its `hook_output_metrics` telemetry event, falling back to `--probe-hooks` execution
  when that data is absent)
- depends on: `ctx-scan-core`
- touches: `apps/ctx-scan/src/assembly.ts`, `apps/ctx-scan/src/imports.ts`,
  `apps/ctx-scan/src/truncation.ts`, `apps/ctx-scan/src/calibrate.ts`,
  `apps/ctx-scan/src/telemetry-probe.ts`, `apps/ctx-scan/test/fixtures/**`

## Motivation
`ctx-scan-core` establishes the data model and discovers projects, but every `Node` it emits is
still just a raw file reference — no truncation math, no `@import` resolution, no invocation
ordering. Without this proposal, the tool would fall directly into named mistake #1 (bytes-as-bloat)
and #6 (raw-size truncation blindness): a fat `references/` directory or an untruncated 3KB MCP
description would report as real load-bearing cost when the platform never actually pays that
much. This proposal is also where the telemetry probe operator-ratified in the roadmap's C4b
lands — it assumes the scan always runs on the host running the Grafana docker container, so no
tunnel/auth plumbing is needed, only endpoint resolution and schema self-verification.

## Requirements

### Requirement: CLAUDE.md @import chain walker
The system SHALL resolve `@import` directives in CLAUDE.md up to 4 hops deep, skipping any
`@`-prefixed token inside a code fence, and attribute each resolved document to the
`claude-md-chain` or `rules-import` class per its origin.

### Requirement: Nested CLAUDE.md subtree mapping
The system SHALL identify nested (non-root) CLAUDE.md files and scope them as T2 trigger-paid,
mapped to the subtree they govern rather than the project root.

### Requirement: Frontmatter-driven listing assembly with truncation
The system SHALL parse skill, command, and agent frontmatter (`name`, `description`,
`when_to_use`, including multiline YAML), apply the 1,536-char per-entry truncation, apply the
listing's overall budget-fraction cap with least-invoked-first drop ordering when invocation data
is available, and mark entries `order: unknown` when it is not.

### Requirement: MEMORY.md and MCP description caps
The system SHALL cap MEMORY.md contribution at 200 lines / 25KB and each MCP tool/server
description at 2KB, retaining the untruncated `raw` value alongside the capped `effective` value
on every affected `Node`.

### Requirement: Plugin-surface ingestion
The system SHALL ingest plugin-provided description/tool surfaces into the data model using the
same truncation rules as native skills/commands/MCP entries.

### Requirement: Hook-injection sizing via telemetry or explicit probe
The system SHALL source `hooks-injected` class sizes from `telemetry-hook-bytes`'s
`hook_output_metrics` events when reachable, fall back to `--probe-hooks` (execute with timeout,
measure stdout) when telemetry is unavailable, and render any hook with neither source as an
`unknown` node — never a zero.

### Requirement: Telemetry-backed calibration
`ctx-scan calibrate` SHALL support parsing a pasted `/context` output and, when reachable
(default), a `--from-telemetry` mode implementing the C4b sequence: endpoint resolution
(`CTX_SCAN_LOKI_URL` env → `http://localhost:3100` → docker-inspect discovery → Grafana
datasource-proxy fallback), schema self-verification against sampled recent events, and fitting
the chars→tokens ratio against the first `api_request` per session's `cache_read_tokens`. Any
resolution or schema-assertion failure SHALL degrade the affected feature to `unavailable` with
the reason recorded, and SHALL exit 0 in both the reachable and unreachable case.

## Scope
- **IN**: `@import` resolution, nested-CLAUDE.md mapping, frontmatter + listing truncation,
  MEMORY.md/MCP caps, plugin ingestion, hook-size ingestion (telemetry + probe fallback), the
  telemetry calibration module (endpoint resolution, schema self-verification, ratio fitting).
- **OUT**: rubric band assignment (`ctx-scan-budgets`), the HTML visual (`ctx-scan-render`), the
  references explorer (`ctx-scan-refs`), watch mode (`ctx-scan-watch`).

## Testing
| Affected seam | Unit task | E2E task |
|----------------|-----------|----------|
| `@import` chain walker | `[4.1]` fixture project with known imports/nesting | N/A — pure parsing logic |
| Listing truncation + drop prediction | `[4.2]` deterministic drop-prediction fixture | N/A |
| MEMORY.md/MCP caps | `[4.3]` oversized fixture files | N/A |
| Hook-size ingestion (telemetry + probe fallback) | `[4.4]` mocked telemetry endpoint fixture | `[4.6]` `--probe-hooks` against a real fixture hook |
| Telemetry calibration module | `[4.5]` schema-assertion fixture (reachable + unreachable) | `[4.7]` live run against real containers when available, skip-with-reason otherwise |
| End-to-end totals reproduction | `[4.8]` cc-audit scan reproduces the 2026-07-18 rubric scorecard numbers within tolerance | N/A |

## Impact
| Area | Change |
|------|--------|
| `apps/ctx-scan/src/` | New modules: `assembly.ts`, `imports.ts`, `truncation.ts`, `calibrate.ts`, `telemetry-probe.ts` |
| `apps/ctx-scan/src/model.ts` | `Node.effective_chars`/`truncations`/`bands`(partial: raw/effective split only, band assignment is `ctx-scan-budgets`) now populated for real |

## Risks
| Risk | Mitigation |
|------|-----------|
| Telemetry schema drifts across CC versions, silently returning wrong numbers | Schema self-verification step (C4b step 2) asserts required attributes per event type before trusting any query result; missing attribute degrades to `unavailable`, never a silent wrong number |
| Hardcoding the 2026-07-18 probe's Grafana endpoint/UIDs | Endpoint resolution order (env → localhost → docker inspect → Grafana proxy) with recorded provenance; `[4.9]` greps for zero hardcoded endpoint/UID literals |
| `--probe-hooks` execution hangs on a misbehaving hook | Probe execution is timeout-bounded; a timeout renders that hook `unknown`, not a hang |
