/**
 * build.ts — shared fixture builders for the ctx-scan-render E2E batch
 * ([4.2], [4.3], [4.4], [4.6]).
 *
 * Mirrors `node-band-annotation.test.ts`'s own `makeNode`/file-builder
 * conventions (extend-before-create: reusing that shape rather than
 * inventing a second one) but exported for reuse across the render test
 * suite, and adds a `makeFleet` convenience for assembling a full
 * `Fleet` document (global + projects) the way `render.ts`'s
 * `renderFleetHtml`/`buildViewModel` expect to consume one.
 */
import { file } from "../../helpers/tree";
import type { Fleet, Node, NodeClass, Project, Surface } from "../../../src/model";

/** Same default shape as `node-band-annotation.test.ts`'s `makeNode` — every field overridable. */
export function makeNode(overrides: Partial<Node> & Pick<Node, "path" | "cls">): Node {
  return {
    tier: 1,
    raw_chars: 0,
    effective_chars: 0,
    est_tokens: 0,
    origin: "project",
    truncations: [],
    bands: [],
    ...overrides,
  };
}

/** Build a SKILL.md-shaped fixture file: frontmatter + N body lines, real content on disk. */
export function skillFile(root: string, rel: string, opts: { description?: string; bodyLines?: number } = {}): string {
  const description = opts.description ?? "a fixture skill for ctx-scan-render tests";
  const bodyLines = opts.bodyLines ?? 5;
  const frontmatter = ["---", "name: fixture-skill", `description: "${description}"`, "---"];
  const body = Array.from({ length: bodyLines }, (_, i) => `Body line ${i} of the fixture skill.`);
  file(root, rel, [...frontmatter, ...body].join("\n"));
  return `${root}/${rel}`;
}

/** Build a plain CLAUDE.md/MEMORY.md-shaped fixture file with exactly N lines. */
export function plainFile(root: string, rel: string, totalLines: number): string {
  const lines = Array.from({ length: totalLines }, (_, i) => `line ${i} of the fixture file`);
  file(root, rel, lines.join("\n"));
  return `${root}/${rel}`;
}

/** One project's worth of `{ cls, nodes }` surfaces. */
export function surface(cls: NodeClass, nodes: Node[]): Surface {
  return { cls, nodes };
}

export function project(name: string, path: string, surfaces: Surface[]): Project {
  return { path, name, surfaces };
}

/** Assemble a full `Fleet` document from a global-layer surface set and a project list. */
export function makeFleet(global: Surface[], projects: Project[]): Fleet {
  return { schemaVersion: 1, root: "/fixture-root", global, projects };
}
