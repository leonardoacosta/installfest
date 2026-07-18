---
stack: t3
---
<!-- beads:epic:if-wfel -->
<!-- beads:feature:if-vxit -->

# Implementation Tasks

## DB Batch

- [x] [1.1] Extend `src/model.ts`: add the truncation-detail shape (`raw`, `effective`, applied [beads:if-malb]
  cap name) reused across every truncated `Node`, and the `order: "unknown" | number` field for
  listing-drop prediction.

## API Batch

- [x] [2.1] Implement `src/imports.ts`: `@import` chain walker, ≤4 hops, code-fence-aware [beads:if-714m]
  (an `@`-prefixed token inside a fenced code block is never resolved as an import).
  - depends on: 1.1
- [x] [2.2] Implement nested-CLAUDE.md subtree mapping in `src/assembly.ts`: identify non-root [beads:if-rlmt]
  CLAUDE.md files, scope each to its governed subtree, classify as T2 trigger-paid.
  - depends on: 1.1
- [x] [2.3] Implement `src/truncation.ts`: frontmatter parsing (gray-matter, multiline YAML) for [beads:if-5lpf]
  skills/commands/agents, 1,536-char per-entry cap, listing budget-fraction cap with
  least-invoked-first drop ordering (sourced from telemetry `tool_name="Skill"` counts when
  reachable, else `order: unknown`), MEMORY.md 200-line/25KB cap, MCP description 2KB cap.
  - depends on: 1.1
- [x] [2.4] Implement plugin-surface ingestion in `src/assembly.ts`, reusing `[2.3]`'s truncation [beads:if-101u]
  rules for plugin-provided descriptions/tool surfaces.
  - depends on: 2.3
- [x] [2.5] Implement `src/telemetry-probe.ts`: the C4b sequence — endpoint resolution [beads:if-fnwt]
  (`CTX_SCAN_LOKI_URL` env → `http://localhost:3100` → docker-inspect discovery of the
  `loki`/`victoria-metrics` containers → Grafana datasource-proxy fallback via `GRAFANA_URL` +
  `GRAFANA_SA_TOKEN`), schema self-verification (labels + sampled recent events, asserting the
  required per-event-type attributes), and provenance recording
  (`{endpoint, service_version, window, query}`) on every telemetry-derived number. Any
  resolution or assertion failure degrades the affected feature to `unavailable` with the reason
  recorded, exit 0 either way.
  - depends on: 1.1
- [x] [2.6] Implement hook-size ingestion: consume `hook_output_metrics` events via `[2.5]`'s [beads:if-5ema]
  probe module when reachable; fall back to `--probe-hooks` (execute the hook with a timeout,
  measure stdout) when telemetry is unreachable; render `unknown` (never zero) when neither
  source is available.
  - depends on: 2.5
- [x] [2.7] Implement `src/calibrate.ts` and the `ctx-scan calibrate` command: parse a pasted [beads:if-lmxg]
  `/context` output, or (default when reachable) `--from-telemetry` — pull the first
  `api_request` per session's `cache_read_tokens`/`cache_creation_tokens` via `[2.5]`'s probe and
  fit the chars→tokens ratio against it.
  - depends on: 2.5

## UI Batch

- [x] [3.1] Wire `ctx-scan calibrate [--from-telemetry] [--json]` output: endpoint provenance, [beads:if-6ajw]
  verified schema summary, fitted ratio, or the static-only degraded-mode reason.
  - depends on: 2.7
- [x] [3.2] Wire the `scan` command to run the full assembly pipeline (imports, nesting, [beads:if-kygo]
  truncation, plugin ingestion, hook sizing) instead of `ctx-scan-core`'s bare discovery pass,
  populating real `effective_chars`/`est_tokens` on every `Node`.
  - depends on: 2.1, 2.2, 2.3, 2.4, 2.6

## E2E Batch

- [ ] [4.1] Fixture project with known `@import` chain and nested CLAUDE.md files; assert [beads:if-e4wx]
  hand-computed effective totals match exactly.
  - depends on: 2.1, 2.2
- [ ] [4.2] Deterministic drop-prediction fixture (fixed invocation-frequency input); assert the [beads:if-7kg9]
  drop-prediction list is identical across repeated runs.
  - depends on: 2.3
- [ ] [4.3] Oversized MEMORY.md and MCP-description fixtures; assert the capped `effective` value [beads:if-qwvt]
  and the retained uncapped `raw` value are both correct.
  - depends on: 2.3
- [ ] [4.4] Mocked telemetry endpoint fixture (fake Loki responses); assert hook-size ingestion [beads:if-h3x3]
  correctly prefers telemetry data when present.
  - depends on: 2.6
- [ ] [4.5] Schema-assertion fixture covering both a reachable-with-valid-schema case and an [beads:if-aa5w]
  unreachable case; assert `unavailable` degradation with a recorded reason in the latter, exit 0
  in both.
  - depends on: 2.5
- [ ] [4.6] `--probe-hooks` run against a real fixture hook with a timeout-bounded misbehaving [beads:if-7jx4]
  sibling hook; assert the timed-out hook renders `unknown`, not a hang or a false zero.
  - depends on: 2.6
- [ ] [4.7] Live run of `ctx-scan calibrate --from-telemetry --json` when local Grafana/Loki [beads:if-cdv8]
  containers are available; paste the actual endpoint-provenance + fitted-ratio output as
  runtime evidence. When containers are stopped, paste the static-only degraded-mode output
  instead, confirming exit 0 either way.
  - depends on: 3.1
- [ ] [4.8] Full scan of `~/dev/cc` (cc-audit); assert the reproduced totals (46,200-char [beads:if-9ynu]
  listing incl. commands; ~16.1K-token chain) match the 2026-07-18 rubric scorecard within the
  documented estimate tolerance.
  - depends on: 3.2
- [ ] [4.9] Grep the shipped source for hardcoded Grafana/Loki hostnames or datasource UIDs; [beads:if-w4wv]
  assert zero matches — every endpoint must come through `[2.5]`'s resolution order.
  - depends on: 2.5
- [ ] [4.10] `tsc --noEmit` and `bun test` both green. [beads:if-1wsa]
  - depends on: 3.2
