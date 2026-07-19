/**
 * rubric.ts — the shared context-budget rubric module (ctx-scan-budgets tasks
 * [1.1], [1.2]-adjacent, [2.1], [2.2]).
 *
 * Single source of truth for `docs/context-budget-rubric.md`'s Table A
 * (rows A1-A14) and Part 0's three band-derivation rules, consumed by both
 * `ctx-scan`'s own `Node.bands` annotation (this module's `annotateFleetBands`)
 * and `ctx-scan audit --json` (`audit.ts`) — one constants block, two
 * consumers, never a second parallel threshold set (the `ctx-scan` roadmap's
 * "Second rubric" mistake this proposal exists to avoid).
 *
 * DESIGN NOTE on greenMax/amberMax vs. Part 0's formulas: Part 0 states three
 * clean formulas (Rule 1 GREEN <= 0.8*L, Rule 2 GREEN <= V, Rule 3 same shape
 * as Rule 1). Reproducing Table A's *published* GREEN/AMBER numbers via those
 * formulas mechanically matches for most rows (A1-A4, A9), but NOT all of
 * them: A7 (5,000/10,000 tok) matches Rule 2's V/2V shape despite its `[R]`
 * tag, and A11 (500/1,024), A12 (8,000/12,000), and A13 (1,600/2,048) use
 * hand-picked round numbers that don't reconstruct exactly from any single
 * `0.8*L` or `V/2V` application of the row's own stated limit. Task [1.1]
 * requires `greenMax`/`amberMax` to be the doc's VERBATIM published numbers
 * — so every row below stores those literal numbers directly, never a
 * formula-recomputed value. `deriveBandRule1`/`deriveBandRule2`/`deriveBandRule3`
 * (task [2.1]) are still implemented as independently correct, standalone
 * pure functions matching Part 0's prose exactly (each row's `bandRule`
 * field names which one conceptually governs it, per Part 0's own rule-3
 * prose: "the rubric sets [R] values using the nearest analogous [H]/[G]
 * anchor, stated per row") — they are the derivation logic Part 0 documents,
 * not a re-derivation path the stored numbers must round-trip through.
 * Live banding (`bandFor`) always classifies a measured value against the
 * row's own stored `greenMax`/`amberMax`, so every row's live behavior always
 * matches Table A exactly, formula-clean or not.
 */
import { readFileSync } from "node:fs";
import type { Band, BandVerdict, Fleet, Node, NodeClass, Surface } from "./model";
import { parseFrontmatter } from "./truncation";
import {
  LISTING_ENTRY_CAP_CHARS,
  LISTING_TOTAL_BUDGET_CHARS,
  MCP_DESCRIPTION_CAP_BYTES,
  MEMORY_MD_MAX_BYTES,
  MEMORY_MD_MAX_LINES,
} from "./truncation";

export type { Band, BandVerdict };

// ─────────────────────────────────────────────────────────────────────────
// [1.1] Table A rows A1-A14
// ─────────────────────────────────────────────────────────────────────────

/** H = hard documented limit, G = documented guidance, R = repo-set (no external number; flagged for ratification). */
export type SourceTag = "H" | "G" | "R";

/** Which Part 0 rule conceptually governs a row's published bands (see module DESIGN NOTE above). */
export type BandRule = "rule1" | "rule2" | "rule3" | null;

/** One `docs/context-budget-rubric.md` Table A row, verbatim. */
export interface TableARow {
  /** Table A row id, "A1".."A14". */
  id: string;
  /** Table A "Surface" column. */
  surface: string;
  /** GREEN upper bound (inclusive), verbatim from Table A. */
  greenMax: number;
  /** AMBER upper bound (inclusive) — RED starts above this, verbatim from Table A. */
  amberMax: number;
  /** Table A "Limit + tag" column's numeric ceiling. */
  limit: number;
  source: SourceTag;
  /** Source document + constant name this row's numbers were sourced from (verbatim, per task [1.1]). */
  sourceCitation: string;
  unit: "chars" | "lines" | "tokens" | "bytes" | "count";
  bandRule: BandRule;
  /**
   * Which `NodeClass`es this row can annotate on individual Nodes
   * (`annotateFleetBands`, task [2.2]). `null` means the row is fleet-wide
   * aggregate-only (computed by `audit.ts`, never attached to one Node's
   * `bands`) — A1, A7, A12.
   */
  nodeClasses: NodeClass[] | null;
  /** False when today's Fleet/Node model cannot compute this row at all (see `gapNote`). */
  computable: boolean;
  /** Populated only when `computable` is false — why, and what would be needed. */
  gapNote?: string;
}

export const TABLE_A: TableARow[] = [
  {
    id: "A1",
    surface: "Skill+command listing total",
    greenMax: 6400,
    amberMax: LISTING_TOTAL_BUDGET_CHARS,
    limit: LISTING_TOTAL_BUDGET_CHARS,
    source: "H",
    sourceCitation:
      "code.claude.com skills.md — skillListingBudgetFraction default 0.01 of a 200K ctx window (8,000 chars ~= 1%), fetched 2026-07-18",
    unit: "chars",
    bandRule: "rule1",
    nodeClasses: null, // aggregate: sum over skills-listing + commands-listing Nodes
    computable: true,
  },
  {
    id: "A2",
    surface: "Per-skill listing entry",
    greenMax: 1229,
    amberMax: LISTING_ENTRY_CAP_CHARS,
    limit: LISTING_ENTRY_CAP_CHARS,
    source: "H",
    sourceCitation: "code.claude.com skills.md — skillListingMaxDescChars, fetched 2026-07-18",
    unit: "chars",
    bandRule: "rule1",
    nodeClasses: ["skills-listing", "commands-listing"],
    computable: true,
  },
  {
    id: "A3",
    surface: "Per-skill description alone",
    greenMax: 819,
    amberMax: 1024,
    limit: 1024,
    source: "H",
    sourceCitation: "agentskills.io/specification — description length MUST <= 1024 chars, fetched 2026-07-18",
    unit: "chars",
    bandRule: "rule1",
    nodeClasses: ["skills-listing", "commands-listing"],
    computable: true,
  },
  {
    id: "A4",
    surface: "SKILL.md body",
    greenMax: 400,
    amberMax: 500,
    limit: 500,
    source: "H",
    sourceCitation:
      "agentskills.io/specification (SHOULD <=500 lines) + code.claude.com skills.md (<5,000-token guidance) — treated as hard (G->H-treated) because of A10's silent post-compaction truncation cliff, fetched 2026-07-18",
    unit: "lines",
    bandRule: "rule1",
    nodeClasses: ["skills-listing"],
    computable: true,
  },
  {
    id: "A5",
    surface: "Reference file ToC",
    greenMax: 100,
    amberMax: 300,
    limit: 300,
    source: "G",
    sourceCitation:
      "code.claude.com skills.md best-practices (100 lines) + agentskills.io/specification skill-creator guidance (300 lines) — bands bridge the two sources, fetched 2026-07-18",
    unit: "lines",
    bandRule: null, // ToC-conditional exception, not a clean single-threshold rule
    nodeClasses: null,
    computable: false,
    gapNote:
      "No NodeClass exists for reference/*.md files anywhere in ctx-scan-assembly's Node model (model.ts's 13 NodeClass values cover skills/commands/agents/CLAUDE.md/memory/mcp/hooks/plugins listings, never a standalone 'references' surface) — this row cannot be computed without a new ingestion path, out of this proposal's scope (touches: rubric.ts, audit.ts, test/fixtures only).",
  },
  {
    id: "A6",
    surface: "Reference nesting depth",
    greenMax: 0,
    amberMax: 0,
    limit: 0,
    source: "H",
    sourceCitation:
      "agentskills.io/specification — one-level-deep SHOULD, treated as hard ([H]-treated: unreachable chained content), fetched 2026-07-18",
    unit: "count",
    bandRule: null, // binary (0 = GREEN, >=1 = RED, no AMBER zone)
    nodeClasses: null,
    computable: false,
    gapNote: "Same gap as A5 — no reference-file ingestion exists in the current Node model to walk for nested links.",
  },
  {
    id: "A7",
    surface: "Always-loaded CLAUDE.md chain",
    greenMax: 5000,
    amberMax: 10000,
    limit: 5000, // Rule 2 anchor value V (2V = 10,000 = the RED threshold)
    source: "R",
    sourceCitation:
      "docs/context-budget-rubric.md Part 0 Rule 3 — anchored to memory.md's per-file guidance (A8, 200-line target) x a 3-file chain; doc's own Limit+tag column states no single numeral, so this module infers V=5,000 from the published GREEN ceiling for Rule-2-shaped consistency (see module DESIGN NOTE)",
    unit: "tokens",
    bandRule: "rule2",
    nodeClasses: null, // aggregate: sum est_tokens over tier===1 claude-md-chain + rules-import Nodes
    computable: true,
  },
  {
    id: "A8",
    surface: "Per-file CLAUDE.md / rules import",
    greenMax: 200,
    amberMax: 400,
    limit: 200,
    source: "G",
    sourceCitation: "code.claude.com memory.md — 200-line-per-file guidance (Part 0 Rule 2), fetched 2026-07-18",
    unit: "lines",
    bandRule: "rule2",
    nodeClasses: ["claude-md-chain", "rules-import"], // tier===1 only — see EXTRA_NODE_FILTERS
    computable: true,
  },
  {
    id: "A9",
    surface: "MEMORY.md auto-load",
    greenMax: Math.round(0.8 * MEMORY_MD_MAX_BYTES), // 20,480 bytes = 20KB
    amberMax: MEMORY_MD_MAX_BYTES, // 25,600 bytes = 25KB
    limit: MEMORY_MD_MAX_BYTES,
    source: "H",
    sourceCitation:
      "code.claude.com memory.md — 200 lines / 25KB per-project MEMORY.md cap, whichever binds first, fetched 2026-07-18",
    unit: "bytes",
    bandRule: "rule1",
    nodeClasses: ["memory"],
    computable: true,
  },
  {
    id: "A10",
    surface: "Compaction carry-forward safety",
    greenMax: 400,
    amberMax: 500,
    limit: 500,
    source: "H",
    sourceCitation:
      "code.claude.com skills.md/hooks.md — first 5,000 tokens per invoked skill, 25,000 combined, post-compaction carry-forward window; derived: mirrors A4 compliance 1:1 per docs/context-budget-rubric.md's own wording, fetched 2026-07-18",
    unit: "lines",
    bandRule: "rule1",
    nodeClasses: ["skills-listing"], // same node set as A4 — this row IS A4's mirror
    computable: true,
  },
  {
    id: "A11",
    surface: "Agent description",
    greenMax: 500,
    amberMax: 1024,
    limit: 1024,
    source: "R",
    sourceCitation:
      "docs/context-budget-rubric.md Part 0 Rule 3 — no documented cap exists for agent descriptions (verified); anchor borrowed from A3's 1,024-char spec ceiling, fetched 2026-07-18",
    unit: "chars",
    bandRule: "rule3",
    nodeClasses: ["agents"],
    computable: true,
  },
  {
    id: "A12",
    surface: "Agent roster total",
    greenMax: 8000,
    amberMax: 12000,
    limit: 12000,
    source: "R",
    sourceCitation:
      "docs/context-budget-rubric.md Part 0 Rule 3 — parity anchor with A1 (the roster is the 'listing' of agents); no platform enforcement at all, fetched 2026-07-18",
    unit: "chars",
    bandRule: "rule3",
    nodeClasses: null, // aggregate: sum per-agent description-alone chars over the global agents Surface
    computable: true,
  },
  {
    id: "A13",
    surface: "MCP tool description / server instructions",
    greenMax: 1600,
    amberMax: MCP_DESCRIPTION_CAP_BYTES,
    limit: MCP_DESCRIPTION_CAP_BYTES,
    source: "H",
    sourceCitation: "code.claude.com mcp.md — 2KB per tool description / server instructions, fetched 2026-07-18",
    unit: "bytes",
    bandRule: "rule1",
    nodeClasses: ["mcp-tools"],
    computable: true,
  },
  {
    id: "A14",
    surface: "Hook stdout / additionalContext / systemMessage",
    greenMax: 8000,
    amberMax: 10000,
    limit: 10000,
    source: "H",
    sourceCitation: "code.claude.com hooks.md — 10,000-char hook output cap, fetched 2026-07-18",
    unit: "chars",
    bandRule: "rule1",
    nodeClasses: ["hooks-injected"],
    computable: true,
  },
];

// ─────────────────────────────────────────────────────────────────────────
// [2.1] Part 0 band-derivation rules — pure functions
// ─────────────────────────────────────────────────────────────────────────

function thresholdBand(measured: number, greenMax: number, amberMax: number): BandVerdict {
  if (measured <= greenMax) return "GREEN";
  if (measured <= amberMax) return "AMBER";
  return "RED";
}

/** Rule 1 (hard limits): GREEN <= 0.8*L, AMBER 0.8*L-L, RED > L. */
export function deriveBandRule1(measured: number, limit: number): BandVerdict {
  return thresholdBand(measured, Math.round(0.8 * limit), limit);
}

/** Rule 2 (guidance values): GREEN <= V, AMBER V-2*V, RED > 2*V. */
export function deriveBandRule2(measured: number, guidance: number): BandVerdict {
  return thresholdBand(measured, guidance, 2 * guidance);
}

/** Rule 3 (repo-set): same shape as Rule 1, tagged source "R" at the call site. */
export function deriveBandRule3(measured: number, limit: number): BandVerdict {
  return deriveBandRule1(measured, limit);
}

/**
 * Classify `measured` against `row`'s own stored (doc-verbatim) `greenMax`/
 * `amberMax` — the actual live banding path both `annotateFleetBands` and
 * `audit.ts` use, always faithful to Table A regardless of whether the row's
 * numbers happen to reconstruct cleanly from `deriveBandRule1/2/3` (see
 * module DESIGN NOTE).
 */
export function bandFor(row: TableARow, measured: number): BandVerdict {
  return thresholdBand(measured, row.greenMax, row.amberMax);
}

const SEVERITY: Record<BandVerdict, number> = { GREEN: 0, AMBER: 1, RED: 2 };

/** The worse (higher-severity) of two bands. */
export function worseBand(a: BandVerdict, b: BandVerdict): BandVerdict {
  return SEVERITY[b] > SEVERITY[a] ? b : a;
}

// ─────────────────────────────────────────────────────────────────────────
// File re-read helpers — a Node's own fields don't carry every dimension
// Table A needs (line counts, description-alone length); `node.path` points
// at the real source file ctx-scan-assembly already read once, so re-reading
// it here for a second, differently-shaped measurement is consistent with
// every other graceful-degradation convention in this codebase (skip on
// failure, never fabricate).
// ─────────────────────────────────────────────────────────────────────────

function readFileSafe(path: string): string | null {
  try {
    return readFileSync(path, "utf8");
  } catch {
    return null;
  }
}

/** Total line count of the file at `path`, or `null` if unreadable. */
export function countLines(path: string): number | null {
  const content = readFileSafe(path);
  if (content === null) return null;
  if (content.length === 0) return 0;
  return content.split("\n").length;
}

/** `len(description)` alone (never combined with `when_to_use`) for the frontmatter file at `path`, or `null`. */
export function descriptionAloneLength(path: string): number | null {
  const content = readFileSafe(path);
  if (content === null) return null;
  try {
    const fm = parseFrontmatter(content);
    return fm.description?.length ?? null;
  } catch {
    return null;
  }
}

/** `len(name)` for the frontmatter file at `path` — 0 (never fabricated non-zero) when unreadable/absent. */
export function nameLength(path: string): number {
  const content = readFileSafe(path);
  if (content === null) return 0;
  try {
    const fm = parseFrontmatter(content);
    return fm.name?.length ?? 0;
  } catch {
    return 0;
  }
}

// ─────────────────────────────────────────────────────────────────────────
// [2.2] Node band annotation
// ─────────────────────────────────────────────────────────────────────────

/** Per-Node measurement for each node-annotatable row, keyed by row id. `null` = unreadable source, never fabricated. */
const NODE_MEASURERS: Partial<Record<string, (node: Node) => number | null>> = {
  A2: (n) => n.raw_chars,
  A3: (n) => descriptionAloneLength(n.path),
  A4: (n) => countLines(n.path),
  A8: (n) => countLines(n.path),
  A9: (n) => n.raw_chars, // bytes dimension (primary — see A9_LINES below for the secondary dimension)
  A10: (n) => countLines(n.path),
  A11: (n) => descriptionAloneLength(n.path),
  A13: (n) => n.raw_chars,
  A14: (n) => n.raw_chars,
};

/** Extra per-row applicability constraints beyond a plain `nodeClasses` match. */
const EXTRA_NODE_FILTERS: Partial<Record<string, (node: Node) => boolean>> = {
  // A8 is the always-loaded chain's per-file check — nested (tier 2) CLAUDE.md
  // files are trigger-paid, not part of the chain A8 measures.
  A8: (n) => n.tier === 1,
};

/** A9's secondary (line-count) dimension — GREEN requires BOTH bytes and lines in range; RED if EITHER exceeds. */
const A9_LINES_GREEN_MAX = Math.round(0.8 * MEMORY_MD_MAX_LINES);
const A9_LINES_AMBER_MAX = MEMORY_MD_MAX_LINES;

/** Compute every applicable Table A row's `Band` verdict for one Node. */
export function computeNodeBands(node: Node): Band[] {
  const out: Band[] = [];
  for (const row of TABLE_A) {
    if (!row.computable || !row.nodeClasses || !row.nodeClasses.includes(node.cls)) continue;
    const extraFilter = EXTRA_NODE_FILTERS[row.id];
    if (extraFilter && !extraFilter(node)) continue;
    const measurer = NODE_MEASURERS[row.id];
    if (!measurer) continue;
    const measured = measurer(node);
    if (measured === null) continue; // source file unreadable — never fabricate a measurement

    let band = bandFor(row, measured);
    if (row.id === "A9") {
      const lines = countLines(node.path);
      if (lines !== null) {
        band = worseBand(band, thresholdBand(lines, A9_LINES_GREEN_MAX, A9_LINES_AMBER_MAX));
      }
    }
    out.push({ rule: row.id, band, measured, limit: row.limit });
  }
  return out;
}

/** Annotate every Node in `surface.nodes` with its computed `bands` (mutates in place). */
function annotateSurfaceBands(surface: Surface): void {
  for (const node of surface.nodes) {
    node.bands = computeNodeBands(node);
  }
}

/** Annotate every Node across the whole Fleet (global layer + every project) with its computed `bands`. */
export function annotateFleetBands(fleet: Fleet): void {
  for (const surface of fleet.global) annotateSurfaceBands(surface);
  for (const project of fleet.projects) {
    for (const surface of project.surfaces) annotateSurfaceBands(surface);
  }
}

/** Flatten every Node across the whole Fleet (global layer + every project). */
export function allFleetNodes(fleet: Fleet): Node[] {
  const out: Node[] = [];
  for (const s of fleet.global) out.push(...s.nodes);
  for (const p of fleet.projects) {
    for (const s of p.surfaces) out.push(...s.nodes);
  }
  return out;
}
