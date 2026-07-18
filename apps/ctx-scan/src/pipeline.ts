/**
 * pipeline.ts — full assembly-pipeline orchestration (ctx-scan-assembly task
 * [3.2], beads:if-kygo): composes `imports.ts` ([2.1]), `assembly.ts`'s
 * nested-CLAUDE.md ([2.2]) + plugin ([2.4]) + hook-size ([2.6]) ingestion,
 * `truncation.ts` ([2.3]), and `settings-resolver.ts` into real
 * `Surface[]`/`Node[]` content for a project or the global layer — replacing
 * `ctx-scan-core`'s bare discovery pass (empty `surfaces: []`).
 *
 * `cli.ts`'s own header comment defers "the `global`/`surfaces` content
 * assembly" to this proposal; this module is where that new pipeline logic
 * lives so `cli.ts` stays a thin wiring layer (parse argv, call this module,
 * emit JSON).
 *
 * Two content classes are deliberately NOT wired here even though their
 * building blocks exist:
 *   - `hooks-injected`: `assembly.ts`'s `hookSizeNodes` hardcodes
 *     `origin: "project"` — hooks fire within a session opened at a
 *     particular project, so they are attributed per-project only, never to
 *     the global layer (no double count).
 *   - `plugins`: `assembly.ts`'s `pluginSurfaceNodes` hardcodes
 *     `origin: "global"` on every Node it builds (both plugin-description and
 *     plugin-MCP entries) — it was not designed to be called with a
 *     project-filtered plugin subset (its only exported entry point,
 *     `listInstalledPlugins`, has no such filter), so plugin surfaces are
 *     assembled exactly once, at the global layer, matching that hardcoded
 *     origin rather than duplicating its private logic per project.
 */
import { existsSync, readFileSync, readdirSync, statSync, type Dirent } from "node:fs";
import { join, sep } from "node:path";
import type { Node, NodeClass, NodeOrigin, Surface } from "./model";
import { resolveImportChain } from "./imports";
import {
  extractHookDefinitions,
  hookSizeNodes,
  ingestHookSizes,
  nestedClaudeMdNodes,
  pluginSurfaceNodes,
  unknownHookCommands,
  type HookSizeResult,
} from "./assembly";
import {
  capListingTotal,
  capMcpDescription,
  capMemoryMd,
  parseFrontmatter,
  type ListingEntryInput,
} from "./truncation";
import { resolveSettings } from "./settings-resolver";

// ─────────────────────────────────────────────────────────────────────────
// Small shared node-building helpers
// ─────────────────────────────────────────────────────────────────────────

function estTokens(chars: number): number {
  return Math.ceil(chars / 4);
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

function makeUnTruncatedNode(path: string, cls: NodeClass, tier: number, rawChars: number, origin: NodeOrigin): Node {
  return {
    path,
    cls,
    tier,
    raw_chars: rawChars,
    effective_chars: rawChars,
    est_tokens: estTokens(rawChars),
    origin,
    truncations: [],
    bands: [],
  };
}

function groupIntoSurfaces(nodes: Node[]): Surface[] {
  const byClass = new Map<NodeClass, Node[]>();
  for (const node of nodes) {
    const list = byClass.get(node.cls);
    if (list) list.push(node);
    else byClass.set(node.cls, [node]);
  }
  return Array.from(byClass.entries()).map(([cls, clsNodes]) => ({ cls, nodes: clsNodes }));
}

// ─────────────────────────────────────────────────────────────────────────
// [2.1] CLAUDE.md @import chain -> claude-md-chain / rules-import nodes
// ─────────────────────────────────────────────────────────────────────────

/**
 * An imported document is classified `rules-import` when its resolved path
 * has a path segment literally named `rules` (case-insensitive) — the
 * observed real convention this repo and `~/dev/cc` both use (`@rules/CORE.md`,
 * `@rules/BEADS.md`, per `imports.ts`'s own module doc). Any other resolved
 * import is treated as a `claude-md-chain` continuation.
 */
function isRulesImportPath(path: string): boolean {
  return path.split(sep).some((seg) => seg.toLowerCase() === "rules");
}

/** Build the root CLAUDE.md node plus every resolved `@import` in its chain. */
function claudeMdChainNodes(rootClaudeMdPath: string, origin: NodeOrigin): Node[] {
  if (!existsSync(rootClaudeMdPath)) return [];
  let rootContent: string;
  try {
    rootContent = readFileSync(rootClaudeMdPath, "utf8");
  } catch {
    return [];
  }

  const out: Node[] = [makeUnTruncatedNode(rootClaudeMdPath, "claude-md-chain", 1, rootContent.length, origin)];

  for (const imp of resolveImportChain(rootClaudeMdPath)) {
    let content: string;
    try {
      content = readFileSync(imp.path, "utf8");
    } catch {
      continue; // dangling import — imports.ts's own convention: skip, never fabricate.
    }
    const cls: NodeClass = isRulesImportPath(imp.path) ? "rules-import" : "claude-md-chain";
    out.push(makeUnTruncatedNode(imp.path, cls, 1, content.length, origin));
  }
  return out;
}

// ─────────────────────────────────────────────────────────────────────────
// [2.3] Frontmatter listing discovery (skills / commands / agents)
// ─────────────────────────────────────────────────────────────────────────

/** Directory-name exclusions applied when walking for command/agent .md files. */
function listingDirExcluded(name: string): boolean {
  if (name === "references" || name === "node_modules" || name === ".git") return true;
  if (name.startsWith("archive")) return true;
  if (name.endsWith("-archive")) return true;
  return false;
}

/** File-level exclusions: READMEs and rubric/contract docs are not real listing entries. */
function listingFileExcluded(fullPath: string): boolean {
  const lower = fullPath.toLowerCase();
  if (lower.includes(`${sep}readme`) || lower.endsWith("readme.md")) return true;
  if (lower.endsWith("contract.md")) return true;
  return false;
}

/** Recursively find `.md` files under `dir`, applying the exclusions above. */
function walkMarkdownFiles(dir: string): string[] {
  const out: string[] = [];
  function walk(d: string): void {
    let entries: Dirent[];
    try {
      entries = readdirSync(d, { withFileTypes: true });
    } catch {
      return;
    }
    for (const entry of entries) {
      const full = join(d, entry.name);
      if (isDirLike(entry, d)) {
        if (listingDirExcluded(entry.name)) continue;
        walk(full);
      } else if (entry.isFile() && entry.name.endsWith(".md")) {
        if (!listingFileExcluded(full)) out.push(full);
      }
    }
  }
  walk(dir);
  return out;
}

/** Find `<skillsDir>/<name>/SKILL.md` entries (one level deep — the real skills-dir shape). */
function findSkillFiles(skillsDir: string): string[] {
  if (!existsSync(skillsDir)) return [];
  let entries: Dirent[];
  try {
    entries = readdirSync(skillsDir, { withFileTypes: true });
  } catch {
    return [];
  }
  const out: string[] = [];
  for (const entry of entries) {
    if (!isDirLike(entry, skillsDir)) continue;
    if (listingDirExcluded(entry.name)) continue;
    const skillMd = join(skillsDir, entry.name, "SKILL.md");
    if (existsSync(skillMd)) out.push(skillMd);
  }
  return out;
}

/**
 * Parse frontmatter (`description` + `when_to_use`, per the proposal's
 * Requirement) for each file, cap via [2.3]'s per-entry + listing-total rules
 * (`capListingTotal`, least-invoked-first drop ordering when `invocationCounts`
 * resolves an entry, `order: "unknown"` otherwise), and build `Node`s — the
 * exact same reuse pattern `assembly.ts`'s `pluginSurfaceNodes` already
 * establishes for plugin descriptions.
 */
function buildListingNodes(
  files: string[],
  cls: NodeClass,
  origin: NodeOrigin,
  invocationCounts: Record<string, number>,
): Node[] {
  const entries: (ListingEntryInput & { path: string; rawChars: number })[] = [];
  for (const filePath of files) {
    let content: string;
    try {
      content = readFileSync(filePath, "utf8");
    } catch {
      continue;
    }
    // `parseFrontmatter` (gray-matter/js-yaml) throws on non-strict YAML —
    // verified live against this machine's own real skill/agent library
    // (e.g. an unescaped `:` in a flow-scalar-like description breaks the
    // YAML mapping parse). A malformed frontmatter block is not this scan's
    // problem to fix; skip the file and keep going, matching every other
    // graceful-degradation convention in this codebase (dangling imports,
    // unreadable dirs, malformed settings JSON all skip rather than abort).
    let fm: ReturnType<typeof parseFrontmatter>;
    try {
      fm = parseFrontmatter(content);
    } catch {
      continue;
    }
    const text = [fm.description, fm.when_to_use].filter((s): s is string => Boolean(s)).join("\n");
    if (!text) continue;
    const id = fm.name ?? filePath;
    entries.push({ id, text, invocations: invocationCounts[id], path: filePath, rawChars: text.length });
  }

  const capped = capListingTotal(entries);
  const out: Node[] = [];
  for (let i = 0; i < capped.length; i++) {
    const entry = capped[i]!;
    const source = entries[i]!;
    out.push({
      path: source.path,
      cls,
      tier: 1,
      raw_chars: source.rawChars,
      effective_chars: entry.dropped ? 0 : entry.effective.length,
      est_tokens: estTokens(entry.dropped ? 0 : entry.effective.length),
      origin,
      truncations: [entry.truncation],
      bands: [],
      order: entry.order,
    });
  }
  return out;
}

// ─────────────────────────────────────────────────────────────────────────
// [2.3] MEMORY.md cap
// ─────────────────────────────────────────────────────────────────────────

/**
 * Project auto-memory lives at `<claudeHome>/projects/<slug>/memory/MEMORY.md`,
 * where `<slug>` is the project's absolute path with every `/` replaced by
 * `-` — verified live against this machine's own
 * `~/.claude/projects/-home-nyaptor-dev-personal-installfest/memory/MEMORY.md`.
 */
function projectMemoryPath(claudeHome: string, projectPath: string): string {
  const slug = projectPath.replace(/\//g, "-");
  return join(claudeHome, "projects", slug, "memory", "MEMORY.md");
}

function memoryNode(memoryPath: string, origin: NodeOrigin): Node | null {
  if (!existsSync(memoryPath)) return null;
  let content: string;
  try {
    content = readFileSync(memoryPath, "utf8");
  } catch {
    return null;
  }
  const cap = capMemoryMd(content);
  return {
    path: memoryPath,
    cls: "memory",
    tier: 1,
    raw_chars: cap.truncation.raw,
    effective_chars: cap.truncation.effective,
    est_tokens: estTokens(cap.truncation.effective),
    origin,
    truncations: [cap.truncation],
    bands: [],
  };
}

// ─────────────────────────────────────────────────────────────────────────
// [2.3] MCP tool/server description cap
// ─────────────────────────────────────────────────────────────────────────

interface McpServerConfig {
  note?: string;
  description?: string;
}

/** Read one `.mcp.json`-shaped file's per-server `note`/`description` fields into capped Nodes. */
function mcpDescriptionNodesFromFile(mcpJsonPath: string, origin: NodeOrigin): Node[] {
  if (!existsSync(mcpJsonPath)) return [];
  let parsed: { mcpServers?: Record<string, McpServerConfig> };
  try {
    parsed = JSON.parse(readFileSync(mcpJsonPath, "utf8")) as typeof parsed;
  } catch {
    return [];
  }
  const out: Node[] = [];
  for (const [serverName, cfg] of Object.entries(parsed.mcpServers ?? {})) {
    const description = cfg.note ?? cfg.description;
    if (typeof description !== "string" || description.length === 0) continue;
    const cap = capMcpDescription(description);
    out.push({
      path: `${mcpJsonPath}#${serverName}`,
      cls: "mcp-tools",
      tier: 1,
      raw_chars: cap.truncation.raw,
      effective_chars: cap.truncation.effective,
      est_tokens: estTokens(cap.truncation.effective),
      origin,
      truncations: [cap.truncation],
      bands: [],
    });
  }
  return out;
}

// ─────────────────────────────────────────────────────────────────────────
// Top-level assembly
// ─────────────────────────────────────────────────────────────────────────

export interface AssembleOptions {
  /** Allow `--probe-hooks` execution fallback when telemetry has no sample. Default false. */
  allowProbeHooks?: boolean;
  /** Per-listing-entry invocation counts, keyed by frontmatter `name` (or file path fallback). */
  invocationCounts?: Record<string, number>;
  env?: Record<string, string | undefined>;
  /**
   * Shared across every `assembleProjectSurfaces` call in one fleet scan,
   * keyed by the resolved hook-definition set (stable-stringified). Most
   * projects under a `--root` inherit the IDENTICAL global default hook set
   * (settings-resolver's precedence falls through to `~/.claude/settings.json`
   * when a project defines none of its own), so without this cache a
   * multi-project scan would re-run `ingestHookSizes`'s telemetry-endpoint
   * resolution (real docker/network round trips) once per project for
   * what is, in practice, the exact same query — verified live: an
   * uncached 5-fixture-project scan took ~11s, all but one of those calls
   * doing genuinely redundant work.
   */
  hookSizeCache?: Map<string, Promise<HookSizeResult[]>>;
}

/** Stable cache key for a resolved hook-definition set (order-preserving, JSON-based). */
function hookDefsCacheKey(hookDefs: ReturnType<typeof extractHookDefinitions>): string {
  return JSON.stringify(hookDefs);
}

export interface ProjectAssemblyResult {
  surfaces: Surface[];
  /** Hooks with neither a telemetry sample nor a probe measurement — never silently dropped from the total. */
  unknownHooks: HookSizeResult[];
}

/**
 * Assemble one project's real Surface[] content: root CLAUDE.md + `@import`
 * chain ([2.1]), nested CLAUDE.md subtree map ([2.2]), skills/commands/agents
 * listings ([2.3]), MEMORY.md ([2.3]), the project's own MCP descriptions
 * ([2.3]), and hook-injection sizing ([2.6]) via the project's *resolved*
 * hooks config (settings-resolver's precedence — so a project with no
 * project-local hooks correctly inherits the operative global default set,
 * exactly as a real session opened there would).
 */
export async function assembleProjectSurfaces(
  projectPath: string,
  claudeHome: string,
  opts: AssembleOptions = {},
): Promise<ProjectAssemblyResult> {
  const invocationCounts = opts.invocationCounts ?? {};
  const nodes: Node[] = [];

  nodes.push(...claudeMdChainNodes(join(projectPath, "CLAUDE.md"), "project"));
  nodes.push(...nestedClaudeMdNodes(projectPath));
  nodes.push(
    ...buildListingNodes(findSkillFiles(join(projectPath, ".claude", "skills")), "skills-listing", "project", invocationCounts),
  );
  nodes.push(
    ...buildListingNodes(
      walkMarkdownFiles(join(projectPath, ".claude", "commands")),
      "commands-listing",
      "project",
      invocationCounts,
    ),
  );
  nodes.push(
    ...buildListingNodes(walkMarkdownFiles(join(projectPath, ".claude", "agents")), "agents", "project", invocationCounts),
  );

  const mem = memoryNode(projectMemoryPath(claudeHome, projectPath), "project");
  if (mem) nodes.push(mem);

  nodes.push(...mcpDescriptionNodesFromFile(join(projectPath, ".mcp.json"), "project"));
  nodes.push(...mcpDescriptionNodesFromFile(join(projectPath, "mcp.json"), "project"));

  const settings = resolveSettings(projectPath);
  const hookDefs = extractHookDefinitions(settings.resolved.hooks?.value);
  // Skip the (network/docker-spawning) telemetry probe entirely when there is
  // nothing to measure — ingestHookSizes probes unconditionally otherwise.
  let hookResults: HookSizeResult[] = [];
  if (hookDefs.length > 0) {
    const cache = opts.hookSizeCache;
    if (cache) {
      const key = hookDefsCacheKey(hookDefs);
      let pending = cache.get(key);
      if (!pending) {
        pending = ingestHookSizes(hookDefs, { allowProbe: opts.allowProbeHooks ?? false, env: opts.env });
        cache.set(key, pending);
      }
      hookResults = await pending;
    } else {
      hookResults = await ingestHookSizes(hookDefs, { allowProbe: opts.allowProbeHooks ?? false, env: opts.env });
    }
  }
  nodes.push(...hookSizeNodes(hookResults));

  return { surfaces: groupIntoSurfaces(nodes), unknownHooks: unknownHookCommands(hookResults) };
}

/**
 * Assemble the global `~/.claude` layer's real Surface[] content: root
 * CLAUDE.md + `@import` chain, nested CLAUDE.md subtree map, global
 * skills/commands/agents listings, the global `mcp.json`'s tool descriptions,
 * and plugin-surface ingestion ([2.4] — the only place plugins are wired, see
 * this module's header doc for why). No hook-injection sizing here: hooks are
 * a per-project concern (see header doc).
 */
export async function assembleGlobalSurfaces(claudeHome: string, opts: AssembleOptions = {}): Promise<Surface[]> {
  const invocationCounts = opts.invocationCounts ?? {};
  const nodes: Node[] = [];

  nodes.push(...claudeMdChainNodes(join(claudeHome, "CLAUDE.md"), "global"));
  // `nestedClaudeMdNodes` (assembly.ts [2.2]) hardcodes `origin: "project"` on
  // every Node it builds — correct when called per-project, wrong here (these
  // are global-tree files, e.g. `~/.claude/plugins/**/CLAUDE.md`). Override at
  // this call site rather than special-casing the shared function.
  nodes.push(...nestedClaudeMdNodes(claudeHome).map((n) => ({ ...n, origin: "global" as const })));
  nodes.push(...buildListingNodes(findSkillFiles(join(claudeHome, "skills")), "skills-listing", "global", invocationCounts));
  nodes.push(
    ...buildListingNodes(walkMarkdownFiles(join(claudeHome, "commands")), "commands-listing", "global", invocationCounts),
  );
  nodes.push(...buildListingNodes(walkMarkdownFiles(join(claudeHome, "agents")), "agents", "global", invocationCounts));
  nodes.push(...mcpDescriptionNodesFromFile(join(claudeHome, "mcp.json"), "global"));
  nodes.push(...pluginSurfaceNodes(claudeHome, invocationCounts));

  return groupIntoSurfaces(nodes);
}
