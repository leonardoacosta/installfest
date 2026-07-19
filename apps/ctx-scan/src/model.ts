/**
 * model.ts — schema-versioned ctx-scan data model.
 *
 * Hierarchy: Fleet → Project → Surface(class) → Node(document). Every later
 * proposal in the ctx-scan chain (assembly, budgets, render, refs, watch)
 * builds on these types, so they stay minimal and decoupled from any
 * scan-command specifics. Do not couple anything here to CLI wiring.
 */

/**
 * Version of the JSON document shape emitted by `ctx-scan scan`. Bump on ANY
 * breaking change to the Fleet/Project/Surface/Node shape — the snapshot test
 * (task [4.4]) fails a shape change that forgets to bump this.
 */
export const schemaVersion = 1;

/**
 * The 14 context-surface classes a Node can belong to. `reference-file` (added
 * by `ctx-scan-refs` task [1.1]) is a shelf-only class: it is never produced
 * by `ctx-scan-assembly`'s pipeline (`pipeline.ts`'s `scan`/`audit` Fleet
 * assembly), so it never appears in `Fleet.global`/`Project.surfaces` and
 * never contributes to the primary token bar — T3 (on-demand) surfaces are
 * deliberately excluded from that bar (proposal.md's Motivation). It exists
 * solely so `refs.ts` can build standalone `Node`-shaped objects (never
 * inserted into a `Fleet`) to reuse `render/level3-document.ts`'s detail-view
 * renderer unchanged for shelf entries.
 */
export type NodeClass =
  | "system-prompt"
  | "system-tools"
  | "claude-md-chain"
  | "rules-import"
  | "agents"
  | "skills-listing"
  | "commands-listing"
  | "skill-bodies"
  | "mcp-tools"
  | "hooks-injected"
  | "memory"
  | "output-style"
  | "plugins"
  | "reference-file";

/** Which layer a Node's bytes originate from (global counted once, not per-project). */
export type NodeOrigin = "global" | "project";

/**
 * A single applied truncation — one per cap a document's size was clipped
 * against (e.g. the 1,536-char listing-entry cap, the 200-line/25KB MEMORY.md
 * cap, the 2KB MCP-description cap). `raw`/`effective` are chars for that one
 * cap's before/after; `cap` names which cap applied (e.g. `"listing-entry"`,
 * `"memory-md"`, `"mcp-description"`) so multiple truncations on one Node
 * (rare, but possible for a listing entry that is also part of a larger doc)
 * stay distinguishable.
 */
export interface Truncation {
  raw: number;
  effective: number;
  cap: string;
}

/** The three verdicts a rubric row can assign a measured value (`ctx-scan-budgets`, Part 0). */
export type BandVerdict = "GREEN" | "AMBER" | "RED";

/**
 * One rubric-row verdict applied to a Node — populated by `ctx-scan-budgets`'s
 * `src/rubric.ts` (`computeNodeBands`). `rule` is the `docs/context-budget-rubric.md`
 * Table A row id the verdict was computed against (e.g. `"A2"`); `measured`/`limit`
 * are the exact values fed into that row's band-derivation rule (Part 0).
 */
export interface Band {
  rule: string;
  band: BandVerdict;
  measured: number;
  limit: number;
}

/** A single measured context document. */
export interface Node {
  /** Absolute path of the source document. */
  path: string;
  /** Context-surface class this document belongs to. */
  cls: NodeClass;
  /** Budget tier. Tier-assignment logic ships in `ctx-scan-assembly`. */
  tier: number;
  /** Raw character count of the source file on disk. */
  raw_chars: number;
  /** Characters actually loaded after truncation. Assembly-computed; 0 here. */
  effective_chars: number;
  /** Estimated token count. Assembly-computed; 0 here. */
  est_tokens: number;
  /** Global (`~/.claude`) vs project-local origin. */
  origin: NodeOrigin;
  /** Truncation records — populated by `ctx-scan-assembly`. */
  truncations: Truncation[];
  /**
   * Rubric-band verdicts — one per applicable `docs/context-budget-rubric.md`
   * Table A row, populated by `ctx-scan-budgets`'s `annotateFleetBands`. `[]`
   * when no Table A row applies to this Node's `cls`, or when the row's
   * source file could not be re-read for a measurement that needs it.
   */
  bands: Band[];
  /**
   * Listing-drop prediction rank for `skills-listing`/`commands-listing`
   * class Nodes only (least-invoked-first, when invocation-frequency
   * telemetry is reachable). `"unknown"` when no invocation data is
   * available — never a guessed number. `undefined` for non-listing
   * classes, where drop-prediction does not apply.
   */
  order?: "unknown" | number;
}

/** A group of Nodes sharing one class within a Project or the global layer. */
export interface Surface {
  cls: NodeClass;
  nodes: Node[];
}

/** A discovered project root and its measured surfaces. */
export interface Project {
  /** Absolute project-root path (outermost git root). */
  path: string;
  /** Display name (typically the project directory basename). */
  name: string;
  surfaces: Surface[];
}

/** The top-level scan document. */
export interface Fleet {
  schemaVersion: number;
  /** Absolute `--root` the scan walked. */
  root: string;
  /**
   * The global `~/.claude` layer, scanned exactly once. Its Nodes carry
   * `origin: "global"`; keeping it off the `projects` list is what prevents
   * double-counting the global layer once per project.
   */
  global: Surface[];
  /** Discovered project roots (never includes the global layer). */
  projects: Project[];
}
