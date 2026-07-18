/**
 * assembly.ts — nested-CLAUDE.md subtree mapping (task [2.2], beads:if-rlmt),
 * plugin-surface ingestion (task [2.4], beads:if-101u), and hook-size
 * ingestion (task [2.6], beads:if-5ema).
 *
 * This module produces the building-block ingestion functions the full scan
 * pipeline wiring (`ctx-scan-assembly` task [3.2], UI batch — out of this
 * proposal's API-batch scope) composes together; it does not itself wire
 * `cli.ts`'s `scan` command.
 */
import { existsSync, readFileSync, readdirSync, statSync, type Dirent } from "node:fs";
import { dirname, join } from "node:path";
import { homedir } from "node:os";
import type { Node, Truncation } from "./model";
import {
  capListingTotal,
  capMcpDescription,
  type ListingEntryInput,
} from "./truncation";
import { probeTelemetry, queryEvents, type Provenance } from "./telemetry-probe";

// ─────────────────────────────────────────────────────────────────────────
// [2.2] Nested CLAUDE.md subtree mapping
// ─────────────────────────────────────────────────────────────────────────

/**
 * Tier assigned to nested (non-root) CLAUDE.md files: they are NOT part of
 * the always-loaded chain (`imports.ts`'s tier-1 root `@import` walk) — the
 * platform only pays for them when Claude enters the subtree they govern.
 * "T2 trigger-paid" is the proposal's own literal wording (proposal.md
 * Requirement: Nested CLAUDE.md subtree mapping).
 */
export const NESTED_CLAUDE_MD_TIER = 2;

/** Same directory-name exclusions `discovery.ts` prunes at descent time (kept
 * in sync by convention, not by import, since discovery.ts doesn't export them). */
const EXCLUDE_EXACT = new Set(["node_modules", ".git", "archived", ".worktrees", "dist", "build"]);
function nameExcluded(name: string): boolean {
  if (EXCLUDE_EXACT.has(name)) return true;
  if (name.startsWith("archive")) return true;
  if (name.endsWith("-archive")) return true;
  return false;
}

function isDirLike(entry: Dirent, parent: string): boolean {
  if (entry.isDirectory()) return true;
  if (entry.isSymbolicLink()) {
    try {
      return statSync(join(parent, entry.name)).isDirectory();
    } catch {
      return false;
    }
  }
  return false;
}

/** One non-root CLAUDE.md file, scoped to the subtree it governs. */
export interface NestedClaudeMd {
  /** Absolute path of the nested CLAUDE.md file. */
  path: string;
  /**
   * Absolute path of the subtree this file governs. A nested CLAUDE.md
   * governs its own containing directory and everything beneath it, so this
   * is always `dirname(path)` — kept as an explicit field for readability
   * rather than making every caller re-derive it.
   */
  subtreeRoot: string;
  /** Raw char count of the file on disk. */
  rawChars: number;
}

/**
 * Find every non-root CLAUDE.md file under `projectRoot`, each scoped to its
 * own governed subtree. The project ROOT `CLAUDE.md` (`projectRoot/CLAUDE.md`)
 * is excluded — that one belongs to the always-loaded `@import` chain
 * (`imports.ts`'s tier-1 walk), not this trigger-paid set. A missing/unreadable
 * file is silently skipped, matching `imports.ts`'s dangling-reference
 * tolerance — a scan should degrade gracefully, never crash on one bad file.
 */
export function findNestedClaudeMdFiles(projectRoot: string): NestedClaudeMd[] {
  const rootClaudeMd = join(projectRoot, "CLAUDE.md");
  const out: NestedClaudeMd[] = [];

  function walk(dir: string): void {
    let entries: Dirent[];
    try {
      entries = readdirSync(dir, { withFileTypes: true });
    } catch {
      return;
    }
    for (const entry of entries) {
      const full = join(dir, entry.name);
      if (isDirLike(entry, dir)) {
        if (nameExcluded(entry.name)) continue;
        walk(full);
      } else if (entry.isFile() && entry.name === "CLAUDE.md" && full !== rootClaudeMd) {
        try {
          const rawChars = readFileSync(full, "utf8").length;
          out.push({ path: full, subtreeRoot: dir, rawChars });
        } catch {
          continue;
        }
      }
    }
  }
  walk(projectRoot);
  return out;
}

/**
 * Build `model.Node` records for the nested-CLAUDE.md set. No truncation cap
 * applies to nested CLAUDE.md content itself (the rubric's per-file line
 * guidance, A8, is a threshold check for `ctx-scan-budgets`, not a cap this
 * layer enforces), so `effective_chars` always equals `raw_chars` here.
 */
export function nestedClaudeMdNodes(projectRoot: string): Node[] {
  return findNestedClaudeMdFiles(projectRoot).map((f) => ({
    path: f.path,
    cls: "claude-md-chain",
    tier: NESTED_CLAUDE_MD_TIER,
    raw_chars: f.rawChars,
    effective_chars: f.rawChars,
    est_tokens: Math.ceil(f.rawChars / 4),
    origin: "project",
    truncations: [],
    bands: [],
  }));
}

// ─────────────────────────────────────────────────────────────────────────
// [2.4] Plugin-surface ingestion
// ─────────────────────────────────────────────────────────────────────────

interface InstalledPluginEntry {
  scope: "user" | "project";
  installPath: string;
  projectPath?: string;
}

/** One installed plugin, resolved from `~/.claude/plugins/installed_plugins.json`. */
export interface InstalledPlugin {
  /** `<name>@<marketplace>` — the key `installed_plugins.json` uses. */
  id: string;
  name: string;
  marketplace: string;
  installPath: string;
  scope: "user" | "project";
  projectPath?: string;
}

/**
 * List installed plugins from `<claudeHome>/plugins/installed_plugins.json`.
 * A missing/unparseable manifest returns `[]` rather than throwing — the same
 * graceful-degradation convention `settings-resolver.ts` establishes for
 * malformed JSON.
 */
export function listInstalledPlugins(claudeHome: string = join(homedir(), ".claude")): InstalledPlugin[] {
  const manifestPath = join(claudeHome, "plugins", "installed_plugins.json");
  if (!existsSync(manifestPath)) return [];
  let parsed: { plugins?: Record<string, InstalledPluginEntry[]> };
  try {
    parsed = JSON.parse(readFileSync(manifestPath, "utf8")) as typeof parsed;
  } catch {
    return [];
  }
  const out: InstalledPlugin[] = [];
  for (const [id, entries] of Object.entries(parsed.plugins ?? {})) {
    const atIdx = id.lastIndexOf("@");
    const name = atIdx === -1 ? id : id.slice(0, atIdx);
    const marketplace = atIdx === -1 ? "" : id.slice(atIdx + 1);
    for (const entry of entries) {
      out.push({
        id,
        name,
        marketplace,
        installPath: entry.installPath,
        scope: entry.scope,
        projectPath: entry.projectPath,
      });
    }
  }
  return out;
}

interface PluginManifest {
  description?: string;
}

/** Read `<installPath>/.claude-plugin/plugin.json`'s `description`, or null. */
function readPluginDescription(installPath: string): string | null {
  const manifestPath = join(installPath, ".claude-plugin", "plugin.json");
  if (!existsSync(manifestPath)) return null;
  try {
    const manifest = JSON.parse(readFileSync(manifestPath, "utf8")) as PluginManifest;
    return typeof manifest.description === "string" ? manifest.description : null;
  } catch {
    return null;
  }
}

interface McpServerConfig {
  note?: string;
  description?: string;
}

interface PluginMcpEntry {
  serverName: string;
  description: string;
}

/** Read `<installPath>/.mcp.json`'s per-server `note`/`description` fields. */
function readPluginMcpDescriptions(installPath: string): PluginMcpEntry[] {
  const mcpPath = join(installPath, ".mcp.json");
  if (!existsSync(mcpPath)) return [];
  try {
    const parsed = JSON.parse(readFileSync(mcpPath, "utf8")) as {
      mcpServers?: Record<string, McpServerConfig>;
    };
    const out: PluginMcpEntry[] = [];
    for (const [serverName, cfg] of Object.entries(parsed.mcpServers ?? {})) {
      const description = cfg.note ?? cfg.description;
      if (typeof description === "string" && description.length > 0) {
        out.push({ serverName, description });
      }
    }
    return out;
  } catch {
    return [];
  }
}

/**
 * Ingest plugin-provided description + MCP tool-surface Nodes, reusing
 * `truncation.ts`'s [2.3] cap logic exactly as native skill/command/MCP
 * entries do:
 *   - each plugin's own manifest `description` -> `cls: "plugins"`, capped
 *     via the same listing-entry (1,536 char) + listing-total budget-fraction
 *     rules as native skills/commands (`capListingTotal`), least-invoked-first
 *     when `invocationCounts` is supplied, `order: "unknown"` otherwise.
 *   - each plugin's own `.mcp.json` server description -> `cls: "mcp-tools"`
 *     (this content occupies the exact same context-surface as a native MCP
 *     tool/server description), capped via the 2KB MCP-description cap
 *     (`capMcpDescription`) — no shared total budget, matching rubric row A13
 *     (per-item cap only, no listing-total row for MCP).
 *
 * A plugin's OWN shipped skills/commands (e.g. a plugin bundling a
 * `skills/<name>/SKILL.md`) are out of scope here — those are ingested by
 * whichever mechanism scans skills/commands generally (task [3.2]'s pipeline
 * wiring), not duplicated by this plugin-manifest-level ingestion.
 */
export function pluginSurfaceNodes(
  claudeHome: string = join(homedir(), ".claude"),
  invocationCounts: Record<string, number> = {},
): Node[] {
  const plugins = listInstalledPlugins(claudeHome);
  const out: Node[] = [];

  const descriptionEntries: (ListingEntryInput & { path: string; rawChars: number })[] = [];
  for (const plugin of plugins) {
    const description = readPluginDescription(plugin.installPath);
    if (description !== null) {
      descriptionEntries.push({
        id: plugin.id,
        text: description,
        invocations: invocationCounts[plugin.id],
        path: join(plugin.installPath, ".claude-plugin", "plugin.json"),
        rawChars: description.length,
      });
    }
  }

  const capped = capListingTotal(descriptionEntries);
  for (let i = 0; i < capped.length; i++) {
    const entry = capped[i]!;
    const source = descriptionEntries[i]!;
    out.push({
      path: source.path,
      cls: "plugins",
      tier: 1,
      raw_chars: source.rawChars,
      effective_chars: entry.dropped ? 0 : entry.effective.length,
      est_tokens: Math.ceil((entry.dropped ? 0 : entry.effective.length) / 4),
      origin: "global",
      truncations: [entry.truncation],
      bands: [],
      order: entry.order,
    });
  }

  for (const plugin of plugins) {
    for (const mcpEntry of readPluginMcpDescriptions(plugin.installPath)) {
      const cap = capMcpDescription(mcpEntry.description);
      out.push({
        path: join(plugin.installPath, ".mcp.json"),
        cls: "mcp-tools",
        tier: 1,
        raw_chars: cap.truncation.raw,
        effective_chars: cap.truncation.effective,
        est_tokens: Math.ceil(cap.truncation.effective / 4),
        origin: "global",
        truncations: [cap.truncation],
        bands: [],
      });
    }
  }

  return out;
}

// ─────────────────────────────────────────────────────────────────────────
// [2.6] Hook-size ingestion
// ─────────────────────────────────────────────────────────────────────────

/** One hook command entry from a resolved `settings.json` `hooks{}` block. */
export interface HookDefinition {
  /** Hook lifecycle event, e.g. "PostToolUse". */
  event: string;
  /** Matcher string (may be empty for context-blind events). */
  matcher: string;
  /** The shell command configured for this hook entry. */
  command: string;
}

interface RawHookEntry {
  type?: string;
  command?: string;
  timeout?: number;
}
interface RawMatcherGroup {
  matcher?: string;
  hooks?: RawHookEntry[];
}

/** Extract flat `HookDefinition`s from a settings.json `hooks` block. */
export function extractHookDefinitions(hooksConfig: unknown): HookDefinition[] {
  const out: HookDefinition[] = [];
  if (!hooksConfig || typeof hooksConfig !== "object") return out;
  for (const [event, groups] of Object.entries(hooksConfig as Record<string, unknown>)) {
    if (!Array.isArray(groups)) continue;
    for (const group of groups as RawMatcherGroup[]) {
      const matcher = typeof group.matcher === "string" ? group.matcher : "";
      for (const hookEntry of group.hooks ?? []) {
        if (hookEntry.type === "command" && typeof hookEntry.command === "string") {
          out.push({ event, matcher, command: hookEntry.command });
        }
      }
    }
  }
  return out;
}

/** Strip a trailing `.sh` extension for loose basename matching against telemetry `hook_name`. */
function stripShExt(name: string): string {
  return name.endsWith(".sh") ? name.slice(0, -3) : name;
}

/**
 * Best-effort hook identity: basename of the last path-like token in the
 * command string, after stripping wrapping quotes. Real hook commands are
 * routed through `hook-wrap.sh sh -c '<real-hook-path>'` (telemetry-hook-bytes),
 * so the last whitespace-separated token is typically single-quoted — verified
 * live against this machine's own `settings.json` (e.g.
 * `hook-wrap.sh sh -c '~/.claude/scripts/hooks/telemetry.sh'`), where the naive
 * last-token split leaves a trailing `'` that would otherwise break the
 * basename match against telemetry's unquoted `hook_name` attribute.
 */
function commandHookName(command: string): string {
  const tokens = command.trim().split(/\s+/);
  const last = (tokens[tokens.length - 1] ?? command).replace(/^['"]+|['"]+$/g, "");
  const parts = last.split("/");
  return parts[parts.length - 1] ?? last;
}

export interface HookSizeResult {
  event: string;
  matcher: string;
  command: string;
  /** Where the byte figure came from — "unknown" is a real render value, never a fabricated zero. */
  source: "telemetry" | "probe" | "unknown";
  bytes: number | "unknown";
  provenance?: Provenance;
}

/**
 * Execute `command` with a hard timeout and measure its captured stdout byte
 * length. Returns null on any spawn failure OR on timeout (a misbehaving
 * hook that never closes stdout renders `unknown`, never hangs the scan —
 * task [4.6]'s exact requirement).
 */
async function probeHookStdoutBytes(command: string, timeoutMs: number): Promise<number | null> {
  let proc: Bun.Subprocess<"ignore", "pipe", "ignore">;
  try {
    proc = Bun.spawn(["bash", "-c", command], {
      stdin: "ignore",
      stdout: "pipe",
      stderr: "ignore",
    });
  } catch {
    return null;
  }

  const readStdout = (async (): Promise<number> => {
    const buf = await Bun.readableStreamToArrayBuffer(proc.stdout);
    return buf.byteLength;
  })();
  const timeout = new Promise<null>((resolve) => {
    setTimeout(() => resolve(null), timeoutMs);
  });

  try {
    const result = await Promise.race([readStdout, timeout]);
    if (result === null) {
      try {
        proc.kill();
      } catch {
        /* already exited */
      }
      return null;
    }
    return result;
  } catch {
    try {
      proc.kill();
    } catch {
      /* already exited */
    }
    return null;
  }
}

/**
 * Ingest hook-injected sizes for `hookDefs`: prefer `hook_output_metrics`
 * telemetry (via [2.5]'s probe) when reachable and schema-verified, matched
 * to each hook by a best-effort basename comparison against the telemetry
 * `hook_name` attribute; fall back to `--probe-hooks` execution (timeout-
 * bounded) when telemetry is unreachable or a given hook has no matching
 * telemetry sample; render `bytes: "unknown"` (never `0`) when neither
 * source produced a real measurement.
 */
export async function ingestHookSizes(
  hookDefs: HookDefinition[],
  opts: { allowProbe?: boolean; probeTimeoutMs?: number; env?: Record<string, string | undefined> } = {},
): Promise<HookSizeResult[]> {
  const allowProbe = opts.allowProbe ?? true;
  const probeTimeoutMs = opts.probeTimeoutMs ?? 5000;

  const telemetry = await probeTelemetry("hook_output_metrics", { env: opts.env });

  let telemetryByHookName = new Map<string, { bytes: number; provenance: Provenance }>();
  if (telemetry.status === "available") {
    const queried = await queryEvents(telemetry.endpoint, "hook_output_metrics", { limit: 200 });
    if (queried.ok) {
      // Most-recent-first isn't guaranteed by Loki's `direction=backward` per
      // stream ordering across multiple streams, so explicitly keep the
      // highest stdout_bytes-bearing sample per hook_name (a hook's output
      // size can legitimately vary run to run; the goal here is "do we have
      // ANY real measurement", not a specific statistic like p50 — refining
      // to a distribution is `ctx-scan-budgets` territory).
      for (const evt of queried.events) {
        const hookName = evt.attrs.hook_name;
        const stdoutBytes = evt.attrs.stdout_bytes;
        if (typeof hookName === "string" && typeof stdoutBytes === "number") {
          const key = stripShExt(hookName).toLowerCase();
          if (!telemetryByHookName.has(key)) {
            telemetryByHookName.set(key, { bytes: stdoutBytes, provenance: queried.provenance });
          }
        }
      }
    }
  }

  const results: HookSizeResult[] = [];
  for (const hookDef of hookDefs) {
    const key = stripShExt(commandHookName(hookDef.command)).toLowerCase();
    const fromTelemetry = telemetryByHookName.get(key);
    if (fromTelemetry) {
      results.push({
        event: hookDef.event,
        matcher: hookDef.matcher,
        command: hookDef.command,
        source: "telemetry",
        bytes: fromTelemetry.bytes,
        provenance: fromTelemetry.provenance,
      });
      continue;
    }

    if (allowProbe) {
      const probed = await probeHookStdoutBytes(hookDef.command, probeTimeoutMs);
      if (probed !== null) {
        results.push({
          event: hookDef.event,
          matcher: hookDef.matcher,
          command: hookDef.command,
          source: "probe",
          bytes: probed,
        });
        continue;
      }
    }

    results.push({
      event: hookDef.event,
      matcher: hookDef.matcher,
      command: hookDef.command,
      source: "unknown",
      bytes: "unknown",
    });
  }

  return results;
}

/**
 * Build `model.Node` records from `ingestHookSizes` output for hooks that
 * carry a REAL measurement (telemetry or probe) — `hooks-injected` class,
 * tier 1 (paid whenever the hook actually fires).
 *
 * `model.Node`'s `raw_chars`/`effective_chars`/`est_tokens` fields are typed
 * as plain `number` (task [1.1]'s already-shipped shape has no `"unknown"`
 * union on them, unlike `order`) — there is no honest numeric way to encode
 * "unknown" on a Node without lying with a fabricated `0`, which is exactly
 * what task [2.6] forbids ("render `unknown` — never zero"). So a hook with
 * `bytes: "unknown"` is deliberately NOT materialized as a Node here; use
 * `unknownHookCommands` (below) to get the list of hooks with no
 * measurement at all, which the eventual pipeline wiring ([3.2]) must
 * surface some other way (e.g. a distinct "unmeasured" bucket) rather than
 * silently omitting them from the total.
 */
export function hookSizeNodes(results: HookSizeResult[]): Node[] {
  const out: Node[] = [];
  for (const r of results) {
    if (r.bytes === "unknown") continue;
    const truncations: Truncation[] = [];
    out.push({
      path: r.command,
      cls: "hooks-injected",
      tier: 1,
      raw_chars: r.bytes,
      effective_chars: r.bytes,
      est_tokens: Math.ceil(r.bytes / 4),
      origin: "project",
      truncations,
      bands: [],
    });
  }
  return out;
}

/** Commands for which `ingestHookSizes` could source no real measurement (telemetry or probe). */
export function unknownHookCommands(results: HookSizeResult[]): HookSizeResult[] {
  return results.filter((r) => r.bytes === "unknown");
}
