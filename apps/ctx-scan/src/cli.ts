#!/usr/bin/env bun
/**
 * cli.ts — `ctx-scan` command entrypoint (commander.js).
 *
 * Wiring layer only: parses argv, delegates to `discovery.ts` for project
 * discovery and `pipeline.ts` for the real Fleet content assembly (the
 * `@import` chain walk, nested-CLAUDE.md mapping, listing truncation, plugin
 * ingestion, and hook-size ingestion — ctx-scan-assembly tasks [2.1]-[2.6]),
 * and to `calibrate.ts` for the `calibrate` subcommand's core logic
 * (ctx-scan-assembly task [2.7]). New pipeline logic lives in `pipeline.ts`,
 * not here — see that module's header doc.
 */

import { writeFileSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";
import { Command } from "commander";

import { discoverProjects, getGlobalLayer } from "./discovery";
import { schemaVersion, type Fleet } from "./model";
import { assembleGlobalSurfaces, assembleProjectSurfaces } from "./pipeline";
import { parseContextOutput, fitRatioFromTelemetry, type ParsedContextOutput } from "./calibrate";
import type { Provenance } from "./telemetry-probe";
import { auditFleet, type AuditResult } from "./audit";
import { writeRenderedFleet } from "./render";

/** Expand a leading `~` / `~/…` to the current user's home directory. */
function expandHome(p: string): string {
  if (p === "~") return homedir();
  if (p.startsWith("~/")) return join(homedir(), p.slice(2));
  return p;
}

/** Write `doc` to `jsonPath` (expanded) if given, else stdout — shared by `scan` and `calibrate`. */
function emitJson(doc: unknown, jsonPath?: string): void {
  const text = JSON.stringify(doc, null, 2);
  if (jsonPath) {
    writeFileSync(expandHome(jsonPath), text + "\n", "utf8");
  } else {
    process.stdout.write(text + "\n");
  }
}

// ─────────────────────────────────────────────────────────────────────────
// `scan`
// ─────────────────────────────────────────────────────────────────────────

interface ScanOptions {
  root: string;
  json?: string;
  probeHooks?: boolean;
}

/**
 * Assemble the Fleet document for `root`: real content on every Node via the
 * full assembly pipeline ([3.2]) — the `@import` chain, nested-CLAUDE.md
 * subtree map, skills/commands/agents listings (with truncation), MEMORY.md,
 * MCP descriptions, plugin surfaces, and hook-injection sizing.
 *
 * `--probe-hooks` is an explicit opt-in (default false): executing a hook's
 * configured shell command as a side effect of an inventory scan is a
 * materially different risk/latency profile than reading files, so it stays
 * off unless the caller asks for it — a hook with no telemetry sample and no
 * probe attempt renders `unknown` (see `pipeline.ts`), never a fabricated
 * zero, and is surfaced via a stderr warning (see `runScan`) rather than
 * silently dropped from the total.
 */
export async function buildFleet(
  root: string,
  opts: { allowProbeHooks?: boolean } = {},
): Promise<{ fleet: Fleet; unknownHooksByProject: Map<string, number> }> {
  const global = getGlobalLayer();
  const projects = discoverProjects(root, { globalPath: global.path });

  const globalSurfaces = await assembleGlobalSurfaces(global.path, { allowProbeHooks: opts.allowProbeHooks });

  // Shared across every project in this scan — most projects under `--root`
  // inherit the identical global default hook set, so this collapses what
  // would otherwise be one redundant telemetry-endpoint resolution per
  // project down to one real resolution per distinct hook set (see
  // `pipeline.ts`'s `AssembleOptions.hookSizeCache` doc).
  const sharedHookCache: NonNullable<Parameters<typeof assembleProjectSurfaces>[2]>["hookSizeCache"] = new Map();

  const unknownHooksByProject = new Map<string, number>();
  const projectDocs = await Promise.all(
    projects.map(async (p) => {
      const result = await assembleProjectSurfaces(p.path, global.path, {
        allowProbeHooks: opts.allowProbeHooks,
        hookSizeCache: sharedHookCache,
      });
      if (result.unknownHooks.length > 0) {
        unknownHooksByProject.set(p.name, result.unknownHooks.length);
        for (const hook of result.unknownHooks) {
          process.stderr.write(
            `[ctx-scan] ${p.name}: hook unmeasured (no telemetry sample, probe disabled/failed) — ` +
              `${hook.event} ${hook.matcher ? `matcher="${hook.matcher}" ` : ""}command="${hook.command}"\n`,
          );
        }
      }
      return { path: p.path, name: p.name, surfaces: result.surfaces };
    }),
  );

  const fleet: Fleet = {
    schemaVersion,
    root,
    global: globalSurfaces,
    projects: projectDocs,
  };
  return { fleet, unknownHooksByProject };
}

async function runScan(opts: ScanOptions): Promise<void> {
  const root = expandHome(opts.root);
  const { fleet } = await buildFleet(root, { allowProbeHooks: opts.probeHooks ?? false });
  emitJson(fleet, opts.json);
}

// ─────────────────────────────────────────────────────────────────────────
// `calibrate`
// ─────────────────────────────────────────────────────────────────────────

interface CalibrateOptions {
  fromTelemetry?: boolean;
  json?: string;
}

interface FittedCalibrateOutput {
  status: "fitted";
  endpoint_provenance: Provenance;
  fitted_ratio: {
    chars_per_token: number;
    total_chars: number;
    total_tokens: number;
    cache_read_tokens: number;
    cache_creation_tokens: number;
    session_id: string;
    project_root: string;
  };
}
interface UnavailableCalibrateOutput {
  status: "unavailable";
  reason: string;
}
interface ParsedCalibrateOutput {
  status: "parsed";
  parsed: ParsedContextOutput;
}

/**
 * `--from-telemetry`: fit the chars<->tokens ratio from live telemetry via
 * `calibrate.ts`'s `fitRatioFromTelemetry` (the C4b sequence). On success,
 * print endpoint provenance + the fitted ratio. Per the proposal's Telemetry-
 * backed calibration Requirement ("exit 0 in both the reachable and
 * unreachable case"), an `unavailable` result prints its degraded-mode reason
 * and still exits 0 — this is a real, expected outcome, never a CLI failure.
 */
async function runCalibrateFromTelemetry(): Promise<FittedCalibrateOutput | UnavailableCalibrateOutput> {
  const result = await fitRatioFromTelemetry();
  if (result.status === "unavailable") {
    return { status: "unavailable", reason: result.reason };
  }
  return {
    status: "fitted",
    endpoint_provenance: result.fit.provenance,
    fitted_ratio: {
      chars_per_token: result.fit.charsPerToken,
      total_chars: result.fit.totalChars,
      total_tokens: result.fit.totalTokens,
      cache_read_tokens: result.fit.cacheReadTokens,
      cache_creation_tokens: result.fit.cacheCreationTokens,
      session_id: result.fit.sessionId,
      project_root: result.fit.projectRoot,
    },
  };
}

/** Read all of stdin to a string. Returns `""` immediately if stdin is a TTY (nothing piped). */
async function readStdin(): Promise<string> {
  if (process.stdin.isTTY) return "";
  const chunks: Buffer[] = [];
  for await (const chunk of process.stdin) {
    chunks.push(chunk as Buffer);
  }
  return Buffer.concat(chunks).toString("utf8");
}

/**
 * Default mode (no `--from-telemetry`): parse a pasted `/context` command's
 * output from stdin via `calibrate.ts`'s `parseContextOutput`. No piped input
 * (a TTY, or a pipe with no recognizable lines) degrades to `unavailable`
 * with a reason — same "exit 0, never fabricate" contract as the telemetry
 * path.
 */
async function runCalibrateStatic(): Promise<ParsedCalibrateOutput | UnavailableCalibrateOutput> {
  const text = await readStdin();
  if (text.trim().length === 0) {
    return {
      status: "unavailable",
      reason:
        "no piped input — pipe a pasted `/context` command's output on stdin (e.g. `pbpaste | ctx-scan calibrate`), or pass --from-telemetry",
    };
  }
  const parsed = parseContextOutput(text);
  if (parsed.totalUsedTokens === null && parsed.categories.length === 0) {
    return {
      status: "unavailable",
      reason: "no recognizable `/context` total or category lines found in the piped input",
    };
  }
  return { status: "parsed", parsed };
}

async function runCalibrate(opts: CalibrateOptions): Promise<void> {
  const output = opts.fromTelemetry ? await runCalibrateFromTelemetry() : await runCalibrateStatic();
  emitJson(output, opts.json);
  // Both branches above degrade to {status:"unavailable", reason} rather than
  // throwing/exiting non-zero — exitCode stays 0 by default in every case.
}

// ─────────────────────────────────────────────────────────────────────────
// `audit`
// ─────────────────────────────────────────────────────────────────────────

interface AuditOptions {
  root: string;
  json?: string;
}

/**
 * Build the Fleet for `--root` (same assembly pipeline `scan` uses, hook
 * probing disabled — the audit contract's "no network access" applies to
 * this command's own row-computation logic, not the underlying scan; a
 * project with configured hooks may still see `ctx-scan-assembly`'s
 * telemetry probe attempt, exactly as `scan` does) and emit `auditFleet`'s
 * §E-R1 rows. Never throws — any failure (bad `--root`, a scan-pipeline
 * exception) degrades to `{rows: [], error: "<message>"}`, matching task
 * [2.3]'s "exit 0 always" requirement.
 */
async function runAudit(opts: AuditOptions): Promise<void> {
  const root = expandHome(opts.root);
  let result: AuditResult;
  try {
    const { fleet } = await buildFleet(root, { allowProbeHooks: false });
    result = auditFleet(fleet);
  } catch (err) {
    result = { rows: [], error: err instanceof Error ? err.message : String(err) };
  }
  emitJson(result, opts.json);
}

// ─────────────────────────────────────────────────────────────────────────
// `render`
// ─────────────────────────────────────────────────────────────────────────

interface RenderCliOptions {
  root: string;
  project?: string;
  fleet?: boolean;
  output: string;
  skill?: string;
}

/**
 * Build the Fleet for `--root` (same assembly pipeline `scan`/`audit` use,
 * hook probing disabled) and write a self-contained drill-down HTML report
 * to `--output` (ctx-scan-render task [3.1]). `--project`/`--fleet` only
 * pick which screen is visible on first paint — the written file always
 * embeds the full fleet (see `render.ts`'s module doc for why). `--skill`
 * scopes every project's references shelf panel (ctx-scan-refs [3.1]) to a
 * single named skill/command/agent owner.
 */
async function runRender(opts: RenderCliOptions): Promise<void> {
  const root = expandHome(opts.root);
  const { fleet } = await buildFleet(root, { allowProbeHooks: false });
  const outPath = expandHome(opts.output);
  writeRenderedFleet(fleet, outPath, { project: opts.project, fleet: opts.fleet, skill: opts.skill });
  process.stdout.write(`[ctx-scan] wrote report to ${outPath}\n`);
}

// ─────────────────────────────────────────────────────────────────────────
// Program
// ─────────────────────────────────────────────────────────────────────────

const program = new Command();

program
  .name("ctx-scan")
  .description("Measure what a Claude Code session loads per project across the fleet.");

program
  .command("scan")
  .description("Discover projects under --root and emit the Fleet document with real per-Node content.")
  .option("--root <path>", "root directory to scan", "~/dev")
  .option("--json <path>", "write JSON to this file (default: stdout)")
  .option("--probe-hooks", "execute hooks with no telemetry sample to measure stdout size (bounded by a timeout)", false)
  .action((opts: ScanOptions) => runScan(opts));

program
  .command("calibrate")
  .description(
    "Fit the chars<->tokens ratio from live telemetry (--from-telemetry), or parse a pasted `/context` output from stdin.",
  )
  .option("--from-telemetry", "fit the ratio from live telemetry instead of parsing piped `/context` output", false)
  .option("--json <path>", "write JSON to this file (default: stdout)")
  .action((opts: CalibrateOptions) => runCalibrate(opts));

program
  .command("audit")
  .description(
    "Emit the docs/context-budget-rubric.md §E-R1 JSON contract: every Table A row's GREEN/AMBER/RED band against the current scan.",
  )
  .option("--root <path>", "root directory to scan", "~/dev")
  .option("--json <path>", "write JSON to this file (default: stdout)")
  .action((opts: AuditOptions) => runAudit(opts));

program
  .command("render")
  .description(
    "Render the fleet as a self-contained drill-down HTML report (fleet -> project -> class -> document, plus a trim-plan panel).",
  )
  .option("--root <path>", "root directory to scan", "~/dev")
  .option("--project <name>", "initial view: drill directly into this project's level-1 view")
  .option("--fleet", "initial view: the fleet leaderboard (default)", false)
  .option("-o, --output <path>", "output HTML file path", "./ctx-scan-report.html")
  .option("--skill <name>", "scope every project's references shelf panel to a single named skill/command/agent owner")
  .action((opts: RenderCliOptions) => runRender(opts));

// Only parse argv when this file is run directly (`bun run src/cli.ts ...` /
// the `ctx-scan` bin entry) — not when `buildFleet` is imported for tests,
// which would otherwise hand commander the test runner's own argv.
if (import.meta.main) {
  // parseAsync (not parse) — both `scan` and `calibrate` actions are async;
  // parse() does not await the action promise, so a rejection surfaces as an
  // unhandled-rejection crash instead of a clean CLI error path.
  await program.parseAsync();
}
