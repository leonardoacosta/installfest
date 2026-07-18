/**
 * settings-resolver.ts — 5-layer settings-precedence resolution.
 *
 * For a given project, reads every settings source on disk and resolves, for
 * each top-level key, which layer won in precedence order:
 *
 *   managed → CLI → .claude/settings.local.json → .claude/settings.json →
 *   ~/.claude/settings.json
 *
 * `managed` and `CLI` have no on-disk source in this proposal — they are
 * included as always-empty tiers so the ordering is structurally correct for a
 * future proposal to fill in. Project `.mcp.json` and root `mcp.json` are read
 * as supplementary project-scoped layers positioned just below the project
 * settings files; their keyspace (`mcpServers`) does not collide with the
 * settings keyspace, so placement never disturbs the stated 5-tier spine.
 *
 * A malformed/unparseable JSON file NEVER throws — it reports a per-file parse
 * error in the result and is skipped for resolution, so one bad file does not
 * abort the whole scan.
 */

import { existsSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { homedir } from "node:os";

/** Layer names, highest precedence first. */
export type SettingsLayerName =
  | "managed"
  | "CLI"
  | ".claude/settings.local.json"
  | ".claude/settings.json"
  | ".mcp.json"
  | "mcp.json"
  | "~/.claude/settings.json";

/** The per-key resolution outcome. */
export interface ResolvedSetting {
  key: string;
  value: unknown;
  /** Which layer supplied the winning value. */
  layer: SettingsLayerName;
}

/** Per-file read outcome — records presence and any parse failure. */
export interface LayerReadResult {
  layer: SettingsLayerName;
  /** Absolute path read, or null for the synthetic managed/CLI tiers. */
  path: string | null;
  /** Whether the file existed on disk (false for synthetic tiers). */
  present: boolean;
  /** JSON parse error message, or null when clean. */
  parseError: string | null;
}

/** The full resolution result for one project. */
export interface SettingsResolution {
  projectPath: string;
  /** Every layer's read outcome, in precedence order (highest first). */
  layers: LayerReadResult[];
  /** Winning value + layer per top-level key. */
  resolved: Record<string, ResolvedSetting>;
}

/** Overrides for testing — otherwise derived from `projectPath` + `homedir()`. */
export interface ResolveSettingsOptions {
  /** Override the user layer path (default `~/.claude/settings.json`). */
  userSettingsPath?: string;
}

interface LayerSource {
  layer: SettingsLayerName;
  path: string | null;
}

interface ParsedLayer {
  read: LayerReadResult;
  /** Parsed top-level object, or null when absent/malformed. */
  data: Record<string, unknown> | null;
}

/** Read + parse one layer, never throwing. */
function readLayer(source: LayerSource): ParsedLayer {
  const { layer, path } = source;
  if (path === null) {
    return { read: { layer, path: null, present: false, parseError: null }, data: null };
  }
  if (!existsSync(path)) {
    return { read: { layer, path, present: false, parseError: null }, data: null };
  }
  let raw: string;
  try {
    raw = readFileSync(path, "utf8");
  } catch (err) {
    const parseError = err instanceof Error ? err.message : String(err);
    return { read: { layer, path, present: true, parseError }, data: null };
  }
  try {
    const parsed = JSON.parse(raw) as unknown;
    const data =
      parsed !== null && typeof parsed === "object" && !Array.isArray(parsed)
        ? (parsed as Record<string, unknown>)
        : null;
    return { read: { layer, path, present: true, parseError: null }, data };
  } catch (err) {
    const parseError = err instanceof Error ? err.message : String(err);
    return { read: { layer, path, present: true, parseError }, data: null };
  }
}

/**
 * Resolve settings precedence for `projectPath`. The returned `resolved` map
 * records, per top-level key, the value and layer that won. Never throws on a
 * malformed file — see the module doc.
 */
export function resolveSettings(
  projectPath: string,
  opts: ResolveSettingsOptions = {},
): SettingsResolution {
  const userSettingsPath =
    opts.userSettingsPath ?? join(homedir(), ".claude", "settings.json");

  // Highest precedence first.
  const sources: LayerSource[] = [
    { layer: "managed", path: null },
    { layer: "CLI", path: null },
    { layer: ".claude/settings.local.json", path: join(projectPath, ".claude", "settings.local.json") },
    { layer: ".claude/settings.json", path: join(projectPath, ".claude", "settings.json") },
    { layer: ".mcp.json", path: join(projectPath, ".mcp.json") },
    { layer: "mcp.json", path: join(projectPath, "mcp.json") },
    { layer: "~/.claude/settings.json", path: userSettingsPath },
  ];

  const parsed = sources.map(readLayer);

  const resolved: Record<string, ResolvedSetting> = {};
  // Walk highest → lowest; first layer to define a key wins.
  for (const { read, data } of parsed) {
    if (!data) continue;
    for (const key of Object.keys(data)) {
      if (key in resolved) continue;
      resolved[key] = { key, value: data[key], layer: read.layer };
    }
  }

  return {
    projectPath,
    layers: parsed.map((p) => p.read),
    resolved,
  };
}
