/**
 * node-band-annotation.test.ts — fixture `Node` set with hand-computed
 * expected bands across multiple classes, asserting `computeNodeBands`'s
 * (task [2.2]) annotation matches exactly (ctx-scan-budgets task [4.3],
 * beads:if-byk3).
 *
 * Every measured value is either a literal `Node.raw_chars` field we set
 * directly (A2/A9-bytes/A13/A14 — these measurers read the Node, not the
 * file) or a real fixture file on disk whose exact character/line count we
 * control by construction (A3/A4/A8/A9-lines/A10/A11 — these measurers
 * re-read `node.path`, per `rubric.ts`'s `NODE_MEASURERS`). Expected bands
 * are computed by hand against Table A's published greenMax/amberMax
 * numbers (verified in `rubric-band-boundaries.test.ts`), independent of
 * `computeNodeBands`'s own internals.
 */
import { afterEach, describe, expect, test } from "bun:test";
import { computeNodeBands } from "../src/rubric";
import type { Node, NodeClass } from "../src/model";
import { cleanup, file, tmpRoot } from "./helpers/tree";

const roots: string[] = [];
afterEach(() => {
  while (roots.length) cleanup(roots.pop()!);
});
function tmp(prefix: string): string {
  const root = tmpRoot(prefix);
  roots.push(root);
  return root;
}

function makeNode(overrides: Partial<Node> & Pick<Node, "path" | "cls">): Node {
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

/** Build a SKILL.md-shaped fixture file: N total lines (frontmatter + body), with a description of exact length. */
function skillFile(root: string, rel: string, descriptionLen: number, totalLines: number): string {
  const frontmatterLines = ["---", "name: fixture-skill", `description: "${"a".repeat(descriptionLen)}"`, "---"];
  const bodyLineCount = totalLines - frontmatterLines.length;
  if (bodyLineCount < 0) throw new Error("fixture setup error: totalLines too small for frontmatter");
  const bodyLines = Array.from({ length: bodyLineCount }, (_, i) => `line ${i}`);
  const content = [...frontmatterLines, ...bodyLines].join("\n");
  file(root, rel, content);
  return `${root}/${rel}`;
}

/** Build a plain-text fixture file with exactly N lines (no frontmatter — for claude-md-chain/rules-import/memory nodes). */
function plainFile(root: string, rel: string, totalLines: number): string {
  const lines = Array.from({ length: totalLines }, (_, i) => `line ${i}`);
  file(root, rel, lines.join("\n"));
  return `${root}/${rel}`;
}

/** Build an agent-shaped fixture file with a description of exact length. */
function agentFile(root: string, rel: string, descriptionLen: number): string {
  const content = ["---", "name: fixture-agent", `description: "${"b".repeat(descriptionLen)}"`, "---", "body"].join(
    "\n",
  );
  file(root, rel, content);
  return `${root}/${rel}`;
}

describe("computeNodeBands — hand-computed expected bands across multiple classes [4.3]", () => {
  test("skills-listing node, all-GREEN values", () => {
    const root = tmp("ctx-scan-bands-");
    const path = skillFile(root, "green.md", 700, 350); // desc=700<=819 GREEN, lines=350<=400 GREEN
    const node = makeNode({ path, cls: "skills-listing", raw_chars: 1000 }); // A2: 1000<=1229 GREEN

    const bands = computeNodeBands(node);
    expect(bands).toEqual([
      { rule: "A2", band: "GREEN", measured: 1000, limit: 1536 },
      { rule: "A3", band: "GREEN", measured: 700, limit: 1024 },
      { rule: "A4", band: "GREEN", measured: 350, limit: 500 },
      { rule: "A10", band: "GREEN", measured: 350, limit: 500 }, // A10 mirrors A4 exactly (same measurer, same file)
    ]);
  });

  test("skills-listing node, mixed AMBER/RED values (A2 AMBER, A3/A4/A10 RED)", () => {
    const root = tmp("ctx-scan-bands-");
    const path = skillFile(root, "red.md", 1100, 600); // desc=1100>1024 RED, lines=600>500 RED
    const node = makeNode({ path, cls: "skills-listing", raw_chars: 1400 }); // A2: 1230-1536 AMBER

    const bands = computeNodeBands(node);
    expect(bands).toEqual([
      { rule: "A2", band: "AMBER", measured: 1400, limit: 1536 },
      { rule: "A3", band: "RED", measured: 1100, limit: 1024 },
      { rule: "A4", band: "RED", measured: 600, limit: 500 },
      { rule: "A10", band: "RED", measured: 600, limit: 500 },
    ]);
  });

  test("commands-listing node — A2/A3 apply (shared), but A4/A10 do NOT (skills-listing-only per Table A)", () => {
    const root = tmp("ctx-scan-bands-");
    // Same file shape as the skills-listing RED case above, but classed as
    // commands-listing: A4/A10's `nodeClasses: ["skills-listing"]` means
    // neither row can ever annotate this Node, regardless of its line count.
    const path = skillFile(root, "cmd.md", 1100, 600);
    const node = makeNode({ path, cls: "commands-listing", raw_chars: 1400 });

    const bands = computeNodeBands(node);
    expect(bands).toEqual([
      { rule: "A2", band: "AMBER", measured: 1400, limit: 1536 },
      { rule: "A3", band: "RED", measured: 1100, limit: 1024 },
    ]);
  });

  test("claude-md-chain node, tier=1 — A8 applies, AMBER", () => {
    const root = tmp("ctx-scan-bands-");
    const path = plainFile(root, "CLAUDE.md", 250); // 201-400 AMBER
    const node = makeNode({ path, cls: "claude-md-chain", tier: 1 });

    expect(computeNodeBands(node)).toEqual([{ rule: "A8", band: "AMBER", measured: 250, limit: 200 }]);
  });

  test("claude-md-chain node, tier=2 (nested) — A8's EXTRA_NODE_FILTERS excludes it, bands empty", () => {
    const root = tmp("ctx-scan-bands-");
    const path = plainFile(root, "nested/CLAUDE.md", 250); // same content as the tier=1 case above
    const node = makeNode({ path, cls: "claude-md-chain", tier: 2 });

    expect(computeNodeBands(node)).toEqual([]);
  });

  test("rules-import node, tier=1 — A8 applies, RED", () => {
    const root = tmp("ctx-scan-bands-");
    const path = plainFile(root, "rules/two.md", 450); // >400 RED
    const node = makeNode({ path, cls: "rules-import", tier: 1 });

    expect(computeNodeBands(node)).toEqual([{ rule: "A8", band: "RED", measured: 450, limit: 200 }]);
  });

  test("memory node — A9 bytes AMBER + lines GREEN, worseBand keeps AMBER (measured stays the bytes value)", () => {
    const root = tmp("ctx-scan-bands-");
    const path = plainFile(root, "MEMORY.md", 50); // 50 lines <=160 -> lines dimension GREEN
    const node = makeNode({ path, cls: "memory", raw_chars: 21_000 }); // bytes: 20481-25600 AMBER

    // A9 greenMax=20480 (round(0.8*25600)), amberMax=25600.
    expect(computeNodeBands(node)).toEqual([{ rule: "A9", band: "AMBER", measured: 21_000, limit: 25_600 }]);
  });

  test("memory node — A9 bytes RED overrides a GREEN lines dimension (worseBand picks RED)", () => {
    const root = tmp("ctx-scan-bands-");
    const path = plainFile(root, "MEMORY.md", 10); // lines GREEN
    const node = makeNode({ path, cls: "memory", raw_chars: 30_000 }); // bytes: >25600 RED

    expect(computeNodeBands(node)).toEqual([{ rule: "A9", band: "RED", measured: 30_000, limit: 25_600 }]);
  });

  test("agents node — A11 AMBER", () => {
    const root = tmp("ctx-scan-bands-");
    const path = agentFile(root, "agent.md", 700); // 501-1024 AMBER
    const node = makeNode({ path, cls: "agents" });

    expect(computeNodeBands(node)).toEqual([{ rule: "A11", band: "AMBER", measured: 700, limit: 1024 }]);
  });

  test("mcp-tools node — A13 AMBER (raw_chars is the Node field measurer, no file re-read)", () => {
    const node = makeNode({ path: "/nonexistent/does-not-matter.md", cls: "mcp-tools", raw_chars: 1700 }); // 1601-2048 AMBER

    expect(computeNodeBands(node)).toEqual([{ rule: "A13", band: "AMBER", measured: 1700, limit: 2048 }]);
  });

  test("hooks-injected node — A14 AMBER", () => {
    const node = makeNode({ path: "/nonexistent/does-not-matter.md", cls: "hooks-injected", raw_chars: 9000 }); // 8001-10000 AMBER

    expect(computeNodeBands(node)).toEqual([{ rule: "A14", band: "AMBER", measured: 9000, limit: 10_000 }]);
  });

  test("a class with zero applicable Table A rows (system-prompt) always annotates bands: []", () => {
    const node = makeNode({ path: "/nonexistent/does-not-matter.md", cls: "system-prompt" as NodeClass, raw_chars: 99_999 });

    expect(computeNodeBands(node)).toEqual([]);
  });

  test("an unreadable path never fabricates a measurement — bands stay empty for file-backed rows", () => {
    // agents class requires a real file re-read (descriptionAloneLength); a
    // path that doesn't exist must degrade to skipping the row entirely,
    // never a guessed/zero measurement.
    const node = makeNode({ path: "/definitely/does/not/exist/agent.md", cls: "agents" });

    expect(computeNodeBands(node)).toEqual([]);
  });
});
