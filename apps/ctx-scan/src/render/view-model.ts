/**
 * view-model.ts — render-time view model (ctx-scan-render task [1.1],
 * beads:if-taql).
 *
 * Derives the 4-level drill-down structure (fleet -> project -> class ->
 * document) plus the per-document flags every toggle (post-compaction,
 * include-T2, predicted-drops, calibrated-constant marking) needs, from the
 * `Fleet` JSON document `ctx-scan scan` produces. Every function in this
 * module only READS its `Fleet`/`Node`/`Surface` input — nothing here ever
 * assigns into a `Node`/`Surface`/`Project`/`Fleet` field, matching the
 * proposal's "without mutating the source data" requirement. (Node-level
 * rubric `bands` ARE mutated by `rubric.ts`'s `annotateFleetBands` — that
 * happens one layer up, in `render.ts`, before this module ever sees the
 * fleet, exactly mirroring `audit.ts`'s own established call-site pattern.)
 *
 * Toggle STATE (post-compaction / include-T2 / predicted-drops / calibrated-
 * marking) is not baked into multiple precomputed variants here — instead,
 * every document carries the boolean flags a toggle needs
 * (`isPostCompactionSurvivor`, `isPredictedDrop`, `isCalibratedConstant`,
 * plus its own `tier`), and the render layer's inline CSS/JS filters/dims
 * already-rendered markup by reading those flags via `data-*` attributes.
 * This keeps toggling a pure CSS-class flip on `<body>` — no client-side
 * re-render, no combinatorial precomputation.
 */
import { readFileSync } from "node:fs";
import type { Band, BandVerdict, Fleet, Node, NodeClass, NodeOrigin, Project, Surface, Truncation } from "../model";
import { worseBand } from "../rubric";

// ─────────────────────────────────────────────────────────────────────────
// Shared formatting / escaping helpers (used by every render/level-*.ts module)
// ─────────────────────────────────────────────────────────────────────────

/** HTML-escape for both text content and double-quoted attribute values. */
export function escapeHtml(s: string): string {
  return s.replace(/[&<>"']/g, (c) => {
    switch (c) {
      case "&":
        return "&amp;";
      case "<":
        return "&lt;";
      case ">":
        return "&gt;";
      case '"':
        return "&quot;";
      default:
        return "&#39;";
    }
  });
}

/** Alias — same escaping rules suffice for this codebase's attribute usage (always double-quoted). */
export const escapeAttr = escapeHtml;

/**
 * Token figures throughout `docs/context-budget-rubric.md` are always marked
 * `~` (Part 0's own "Token estimate convention": tokens ≈ chars/4, an
 * estimate, never exact) — this formatter honors that convention everywhere
 * a token count is displayed, so the render layer never implies more
 * precision than the underlying data actually has.
 */
export function fmtEstTokens(n: number): string {
  return `~${Math.round(n).toLocaleString()}`;
}

/** Plain thousands-separated integer formatter for exact (non-estimated) counts. */
export function fmtCount(n: number): string {
  return Math.round(n).toLocaleString();
}

// ─────────────────────────────────────────────────────────────────────────
// Class taxonomy
// ─────────────────────────────────────────────────────────────────────────

/** Canonical display order — mirrors `model.ts`'s own `NodeClass` union order, not object-iteration order. */
const CLASS_ORDER: NodeClass[] = [
  "system-prompt",
  "system-tools",
  "claude-md-chain",
  "rules-import",
  "agents",
  "skills-listing",
  "commands-listing",
  "skill-bodies",
  "mcp-tools",
  "hooks-injected",
  "memory",
  "output-style",
  "plugins",
];

export const CLASS_LABELS: Record<NodeClass, string> = {
  "system-prompt": "System Prompt",
  "system-tools": "System Tools",
  "claude-md-chain": "CLAUDE.md Chain",
  "rules-import": "Rules Imports",
  agents: "Agents",
  "skills-listing": "Skills Listing",
  "commands-listing": "Commands Listing",
  "skill-bodies": "Skill Bodies",
  "mcp-tools": "MCP Tools",
  "hooks-injected": "Hooks (Injected)",
  memory: "Memory (MEMORY.md)",
  "output-style": "Output Style",
  plugins: "Plugins",
  // Never populated by `buildClassViews` (no pipeline.ts data producer emits
  // this class into a real Surface) — present only for Record<NodeClass,_>
  // completeness; `refs.ts`'s shelf panel renders its own label independently.
  "reference-file": "Reference Files",
};

/**
 * `NodeClass` values with zero data producers anywhere in
 * `ctx-scan-assembly`'s pipeline today (verified: no call site in
 * `pipeline.ts`/`assembly.ts` ever constructs a Node with either class) —
 * these are Anthropic-side platform internals (the fixed system prompt, the
 * built-in tool schemas) with no measurable source file ctx-scan can read.
 * Render-time still classifies any Node carrying one of these classes
 * (should a future pipeline change start producing them) as a "calibrated
 * constant" rather than "measured", per proposal.md's Level 1 Requirement
 * ("Constants ... SHALL be visually marked as calibrated rather than
 * measured") — this is a forward-compatible passthrough, not fabricated data
 * for classes the current scan never populates.
 */
const CALIBRATED_CONSTANT_CLASSES = new Set<NodeClass>(["system-prompt", "system-tools"]);

/** The always-loaded `@import` chain classes (root CLAUDE.md + imports; rules-import is the `@rules/*` subset). */
const CHAIN_CLASSES = new Set<NodeClass>(["claude-md-chain", "rules-import"]);

/** Listing classes a drop-prediction rank (`Node.order`) can ever apply to (`truncation.ts`'s `capListingTotal`). */
const LISTING_CLASSES = new Set<NodeClass>(["skills-listing", "commands-listing"]);

/**
 * Rubric row A10's per-invoked-skill carry-forward token ceiling
 * (`docs/context-budget-rubric.md` Table A: "first 5,000 tokens per invoked
 * skill ... post-compaction carry-forward window"). Used only to answer the
 * render-time question "does this node's own token cost fit inside the
 * carry-forward window" for the post-compaction toggle — this does NOT
 * re-derive A10's own GREEN/AMBER/RED band (that stays `rubric.ts`'s job).
 */
const POST_COMPACTION_CARRY_FORWARD_TOKENS = 5000;

// ─────────────────────────────────────────────────────────────────────────
// Document (level 3) view
// ─────────────────────────────────────────────────────────────────────────

export type BandVerdictOrNone = BandVerdict | "NONE";

export interface DocumentView {
  path: string;
  /** Short label for UI display — basename of `path` (fragment-id-aware for `#server` / hook-command paths). */
  displayName: string;
  cls: NodeClass;
  tier: number;
  origin: NodeOrigin;
  rawChars: number;
  effectiveChars: number;
  estTokens: number;
  truncations: Truncation[];
  bands: Band[];
  /** Worst (highest-severity) band across `bands`; `"NONE"` when no rubric row applies to this Node at all. */
  worstBand: BandVerdictOrNone;
  order: "unknown" | number | null;
  /** True for `system-prompt`/`system-tools` — see `CALIBRATED_CONSTANT_CLASSES` doc above. */
  isCalibratedConstant: boolean;
  /** True when this Node is part of the always-reloaded T1 chain, or its own token cost fits A10's carry-forward window. */
  isPostCompactionSurvivor: boolean;
  /** True when this listing entry was already dropped by the A1 budget cap at scan time, or carries a numeric drop-risk rank. */
  isPredictedDrop: boolean;
  /** Numeric least-invoked-first rank when known, else `null` (never a fabricated risk score). */
  dropRiskRank: number | null;
}

/** First N chars of a real source file, re-read at render time; `preview: null` when the file is unreadable (moved/deleted since scan, or a synthetic path like a hook command string). */
export interface ContentCacheEntry {
  preview: string | null;
  truncated: boolean;
}

function readContentPreview(path: string, capChars = 8000): ContentCacheEntry {
  let content: string;
  try {
    content = readFileSync(path, "utf8");
  } catch {
    return { preview: null, truncated: false };
  }
  if (content.length <= capChars) return { preview: content, truncated: false };
  return { preview: content.slice(0, capChars), truncated: true };
}

/**
 * Build a path-keyed content-preview cache, reading each UNIQUE source path
 * exactly once. `Node.path` is shared across every project that sees the
 * global layer (the same global CLAUDE.md/skills/agents/etc. Nodes are
 * merged into EVERY project's own class views), so caching by path instead
 * of embedding a fresh `contentPreview` copy per `DocumentView` avoids
 * re-embedding the identical file text once per project in the final JSON
 * blob — verified live against a real `~/dev` scan (28 projects, 168 shared
 * global nodes): without this cache the rendered HTML was ~60MB, almost
 * entirely duplicate global-layer content re-embedded 28 times.
 */
function buildContentCache(fleet: Fleet): Record<string, ContentCacheEntry> {
  const paths = new Set<string>();
  for (const s of fleet.global) {
    for (const n of s.nodes) paths.add(n.path);
  }
  for (const p of fleet.projects) {
    for (const s of p.surfaces) {
      for (const n of s.nodes) paths.add(n.path);
    }
  }
  const cache: Record<string, ContentCacheEntry> = {};
  for (const path of paths) cache[path] = readContentPreview(path);
  return cache;
}

/** Basename of `path`, fragment-id-aware (`assembly.ts`'s MCP fragment paths use `<file>#<serverName>`; hook-command "paths" are raw shell strings). */
function displayNameFor(path: string): string {
  const [withoutFragment, ...fragmentParts] = path.split("#");
  const segments = (withoutFragment ?? path).split("/");
  const base = segments[segments.length - 1] || withoutFragment || path;
  return fragmentParts.length > 0 ? `${base}#${fragmentParts.join("#")}` : base;
}

function worstBandOf(bands: Band[]): BandVerdictOrNone {
  let worst: BandVerdictOrNone = "NONE";
  for (const b of bands) {
    worst = worst === "NONE" ? b.band : worseBand(worst, b.band);
  }
  return worst;
}

function computePredictedDrop(node: Node): { isPredictedDrop: boolean; dropRiskRank: number | null } {
  if (!LISTING_CLASSES.has(node.cls)) return { isPredictedDrop: false, dropRiskRank: null };
  // Already dropped by the A1 listing-total budget cap at scan time — the
  // one honest, directly-observable signal `truncation.ts`'s own module doc
  // notes is, in practice, the only one usually available (no queryable
  // telemetry currently resolves a genuine per-skill invocation count, so
  // `order` stays "unknown" for most real scans — see truncation.ts).
  const alreadyDropped = node.raw_chars > 0 && node.effective_chars === 0;
  const dropRiskRank = typeof node.order === "number" ? node.order : null;
  return { isPredictedDrop: alreadyDropped || dropRiskRank !== null, dropRiskRank };
}

function computePostCompactionSurvivor(node: Node): boolean {
  // The always-loaded chain is fully reloaded every turn, and again after
  // every compaction — this is tier-1 chain content only; nested (tier-2,
  // trigger-paid) CLAUDE.md files share the same `cls` but are NOT part of
  // this reloaded set (assembly.ts's `NESTED_CLAUDE_MD_TIER`).
  if (node.tier === 1 && CHAIN_CLASSES.has(node.cls)) return true;
  // Skill/command listing entries (and, if ever produced, skill bodies)
  // survive compaction whole when their own token cost fits A10's
  // per-invoked-skill carry-forward window.
  if (LISTING_CLASSES.has(node.cls) || node.cls === "skill-bodies") {
    return node.est_tokens <= POST_COMPACTION_CARRY_FORWARD_TOKENS;
  }
  return false;
}

function buildDocumentView(node: Node): DocumentView {
  const { isPredictedDrop, dropRiskRank } = computePredictedDrop(node);
  return {
    path: node.path,
    displayName: displayNameFor(node.path),
    cls: node.cls,
    tier: node.tier,
    origin: node.origin,
    rawChars: node.raw_chars,
    effectiveChars: node.effective_chars,
    estTokens: node.est_tokens,
    truncations: node.truncations,
    bands: node.bands,
    worstBand: worstBandOf(node.bands),
    order: node.order ?? null,
    isCalibratedConstant: CALIBRATED_CONSTANT_CLASSES.has(node.cls),
    isPostCompactionSurvivor: computePostCompactionSurvivor(node),
    isPredictedDrop,
    dropRiskRank,
  };
}

// ─────────────────────────────────────────────────────────────────────────
// Class (level 2) view
// ─────────────────────────────────────────────────────────────────────────

export interface ClassView {
  cls: NodeClass;
  label: string;
  documents: DocumentView[];
  totalTokens: number;
  /** Tier-1 (always-paid) token subtotal — the level-1 stacked-bar solid segment. */
  tier1Tokens: number;
  /** Tier >= 2 (trigger-paid) token subtotal — the level-1 stacked-bar hatched segment (include-T2 toggle). */
  tier2PlusTokens: number;
  worstBand: BandVerdictOrNone;
  hasT2: boolean;
}

function worstBandAcrossDocuments(documents: DocumentView[]): BandVerdictOrNone {
  let worst: BandVerdictOrNone = "NONE";
  for (const d of documents) {
    if (d.worstBand === "NONE") continue;
    worst = worst === "NONE" ? d.worstBand : worseBand(worst, d.worstBand);
  }
  return worst;
}

/** Group a project's (or the global layer's) `Surface[]` into per-class views, in canonical taxonomy order. */
function buildClassViews(surfaces: Surface[]): ClassView[] {
  const byClass = new Map<NodeClass, Node[]>();
  for (const s of surfaces) {
    const list = byClass.get(s.cls);
    if (list) list.push(...s.nodes);
    else byClass.set(s.cls, [...s.nodes]);
  }

  const out: ClassView[] = [];
  for (const [cls, nodes] of byClass.entries()) {
    const documents = nodes.map(buildDocumentView);
    const totalTokens = documents.reduce((sum, d) => sum + d.estTokens, 0);
    const tier1Tokens = documents.filter((d) => d.tier === 1).reduce((sum, d) => sum + d.estTokens, 0);
    const tier2PlusTokens = documents.filter((d) => d.tier >= 2).reduce((sum, d) => sum + d.estTokens, 0);
    out.push({
      cls,
      label: CLASS_LABELS[cls],
      documents,
      totalTokens,
      tier1Tokens,
      tier2PlusTokens,
      worstBand: worstBandAcrossDocuments(documents),
      hasT2: tier2PlusTokens > 0,
    });
  }

  return out.sort((a, b) => CLASS_ORDER.indexOf(a.cls) - CLASS_ORDER.indexOf(b.cls));
}

// ─────────────────────────────────────────────────────────────────────────
// Project (level 1) view
// ─────────────────────────────────────────────────────────────────────────

export interface ProjectView {
  name: string;
  path: string;
  /** Every class visible while working in this project — the shared global layer's classes merged with the project's own. */
  classes: ClassView[];
  /** Tier-1 tokens from the project's OWN surfaces only (the level-0 "project delta" sub-stack). */
  projectDeltaTokens: number;
  /** Tier-1 tokens from the shared global layer (identical across every project — the level-0 "global baseline" sub-stack). */
  globalBaselineTokens: number;
  totalTokens: number;
}

function tier1Tokens(surfaces: Surface[]): number {
  let total = 0;
  for (const s of surfaces) {
    for (const n of s.nodes) {
      if (n.tier === 1) total += n.est_tokens;
    }
  }
  return total;
}

function buildProjectView(project: Project, globalSurfaces: Surface[]): ProjectView {
  const classes = buildClassViews([...globalSurfaces, ...project.surfaces]);
  const globalBaselineTokens = tier1Tokens(globalSurfaces);
  const projectDeltaTokens = tier1Tokens(project.surfaces);
  return {
    name: project.name,
    path: project.path,
    classes,
    projectDeltaTokens,
    globalBaselineTokens,
    totalTokens: globalBaselineTokens + projectDeltaTokens,
  };
}

// ─────────────────────────────────────────────────────────────────────────
// Fleet (level 0) view
// ─────────────────────────────────────────────────────────────────────────

export interface FleetBarView {
  projectName: string;
  projectPath: string;
  globalBaselineTokens: number;
  projectDeltaTokens: number;
  totalTokens: number;
}

export interface FleetView {
  /** Leaderboard-ordered (desc by `totalTokens`). */
  bars: FleetBarView[];
  maxTotalTokens: number;
}

function buildFleetView(projectViews: ProjectView[]): FleetView {
  const bars = projectViews
    .map((p) => ({
      projectName: p.name,
      projectPath: p.path,
      globalBaselineTokens: p.globalBaselineTokens,
      projectDeltaTokens: p.projectDeltaTokens,
      totalTokens: p.totalTokens,
    }))
    .sort((a, b) => b.totalTokens - a.totalTokens);
  const maxTotalTokens = bars.reduce((max, b) => Math.max(max, b.totalTokens), 0);
  return { bars, maxTotalTokens };
}

// ─────────────────────────────────────────────────────────────────────────
// Top-level view model
// ─────────────────────────────────────────────────────────────────────────

export interface RenderViewModel {
  schemaVersion: number;
  root: string;
  generatedAt: string;
  fleet: FleetView;
  /** Leaderboard-ordered, matching `fleet.bars`. */
  projects: ProjectView[];
  /** Path-keyed content-preview cache — see `buildContentCache`'s doc for why this is deduplicated rather than per-`DocumentView`. */
  contentByPath: Record<string, ContentCacheEntry>;
}

/**
 * Derive the full render-time view model from a `Fleet` document. Purely
 * read-only over `fleet` — see this module's header doc for the mutation
 * contract (rubric `bands` are expected to already be annotated by the
 * caller, mirroring `audit.ts`'s own call-site convention).
 */
export function buildViewModel(fleet: Fleet): RenderViewModel {
  const projectViews = fleet.projects
    .map((p) => buildProjectView(p, fleet.global))
    .sort((a, b) => b.totalTokens - a.totalTokens);
  const fleetView = buildFleetView(projectViews);
  return {
    schemaVersion: fleet.schemaVersion,
    root: fleet.root,
    generatedAt: new Date().toISOString(),
    fleet: fleetView,
    projects: projectViews,
    contentByPath: buildContentCache(fleet),
  };
}
