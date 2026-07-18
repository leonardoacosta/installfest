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

/** The 13 context-surface classes a Node can belong to. */
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
  | "plugins";

/** Which layer a Node's bytes originate from (global counted once, not per-project). */
export type NodeOrigin = "global" | "project";

/**
 * A truncation record. Placeholder: the real shape (offset / limit / reason)
 * is populated by `ctx-scan-assembly`. Kept opaque so the schema is stable
 * without over-designing a field no proposal fills yet.
 */
export type Truncation = unknown;

/**
 * A rubric-band record. Placeholder: the real shape (band id / ceiling /
 * verdict) is populated by `ctx-scan-budgets`. Kept opaque for now.
 */
export type Band = unknown;

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
  /** Rubric-band records — populated by `ctx-scan-budgets`. */
  bands: Band[];
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
