/**
 * imports-chain.test.ts — @import chain + nested-CLAUDE.md fixture
 * (ctx-scan-assembly task [4.1], beads:if-e4wx).
 *
 * Builds a fixture project with a KNOWN two-hop `@import` chain (root ->
 * docs/one.md -> docs/rules/two.md, the latter classified `rules-import`
 * since its resolved path carries a `rules` path segment per
 * `pipeline.ts`'s `isRulesImportPath`), a fenced-code-block `@`-token that
 * must NOT resolve, and one nested (non-root) CLAUDE.md governing a subtree.
 * Every expected char total is computed from the SAME literal fixture
 * strings the test writes to disk (independent of the pipeline's own
 * internal arithmetic), so a real regression in the assembly math — not
 * just a mutation of these fixtures — would fail this test.
 */
import { afterEach, describe, expect, test } from "bun:test";
import { symlinkSync } from "node:fs";
import { join } from "node:path";
import { buildFleet } from "../src/cli";
import { resolveImportChain } from "../src/imports";
import { cleanup, dir, file, tmpRoot } from "./helpers/tree";

const roots: string[] = [];

afterEach(() => {
  while (roots.length) cleanup(roots.pop()!);
});

function tmp(prefix: string): string {
  const root = tmpRoot(prefix);
  roots.push(root);
  return root;
}

describe("@import chain + nested CLAUDE.md — hand-computed totals [4.1]", () => {
  test(
    "root chain, rules-import hop, fenced-code skip, and nested subtree all measure exactly",
    async () => {
      const root = tmp("ctx-scan-imports-");

      const rootContent = ["# Root CLAUDE.md", "", "@docs/one.md", "", "```text", "@fake/skip.md", "```", ""].join(
        "\n",
      );
      const oneContent = ["# One", "", "@rules/two.md", ""].join("\n");
      const twoContent = ["# Two — a rules-scoped import", "some rule text here"].join("\n");
      const nestedContent = ["# Nested CLAUDE.md", "governs packages/app only"].join("\n");

      dir(root, "proj/.git");
      file(root, "proj/CLAUDE.md", rootContent);
      file(root, "proj/docs/one.md", oneContent);
      file(root, "proj/docs/rules/two.md", twoContent);
      file(root, "proj/packages/app/CLAUDE.md", nestedContent);

      const { fleet } = await buildFleet(root);

      expect(fleet.projects).toHaveLength(1);
      const project = fleet.projects[0]!;

      const claudeMdSurface = project.surfaces.find((s) => s.cls === "claude-md-chain");
      expect(claudeMdSurface).toBeDefined();

      // Tier-1 nodes are the always-loaded chain (root + resolved non-rules imports).
      const chainNodes = claudeMdSurface!.nodes.filter((n) => n.tier === 1);
      expect(chainNodes).toHaveLength(2);
      const chainPaths = chainNodes.map((n) => n.path).sort();
      expect(chainPaths).toEqual([`${root}/proj/CLAUDE.md`, `${root}/proj/docs/one.md`].sort());

      const expectedChainChars = rootContent.length + oneContent.length;
      const actualChainChars = chainNodes.reduce((sum, n) => sum + n.effective_chars, 0);
      expect(actualChainChars).toBe(expectedChainChars);
      // claude-md-chain nodes are never truncated — raw and effective match exactly.
      for (const n of chainNodes) {
        expect(n.raw_chars).toBe(n.effective_chars);
        expect(n.truncations).toEqual([]);
        expect(n.est_tokens).toBe(Math.ceil(n.effective_chars / 4));
      }

      // The fenced `@fake/skip.md` token must never resolve into a node.
      expect(chainPaths.some((p) => p.includes("fake/skip"))).toBe(false);

      // Tier-2 nodes under the same `claude-md-chain` class are the nested subtree map.
      const nestedNodes = claudeMdSurface!.nodes.filter((n) => n.tier === 2);
      expect(nestedNodes).toHaveLength(1);
      expect(nestedNodes[0]!.path).toBe(`${root}/proj/packages/app/CLAUDE.md`);
      expect(nestedNodes[0]!.effective_chars).toBe(nestedContent.length);
      expect(nestedNodes[0]!.raw_chars).toBe(nestedContent.length);
      expect(nestedNodes[0]!.origin).toBe("project");

      // The two-hop-deep import (docs/rules/two.md) is classified rules-import,
      // not claude-md-chain, since its resolved path carries a `rules` segment.
      const rulesSurface = project.surfaces.find((s) => s.cls === "rules-import");
      expect(rulesSurface).toBeDefined();
      expect(rulesSurface!.nodes).toHaveLength(1);
      const rulesNode = rulesSurface!.nodes[0]!;
      expect(rulesNode.path).toBe(`${root}/proj/docs/rules/two.md`);
      expect(rulesNode.effective_chars).toBe(twoContent.length);
      expect(rulesNode.raw_chars).toBe(twoContent.length);
      expect(rulesNode.tier).toBe(1);

      // Grand total across the whole chain (root + hop1 + hop2), independent
      // of which class each hop landed in.
      const grandTotal = actualChainChars + rulesNode.effective_chars;
      expect(grandTotal).toBe(rootContent.length + oneContent.length + twoContent.length);
    },
    15_000,
  );
});

describe("resolveImportChain — containment (harden-ctx-scan-fs-boundaries [1.3])", () => {
  test("an @import escaping the project root is skipped; an in-project @import still resolves", () => {
    const root = tmp("ctx-scan-imports-traversal-");
    const projectRoot = join(root, "proj");

    file(
      root,
      "proj/CLAUDE.md",
      ["# Root", "", "@docs/legit.md", "", "@../outside.md", ""].join("\n"),
    );
    file(root, "proj/docs/legit.md", "# Legit — inside the project root\n");
    file(root, "outside.md", "# Outside the project root — must never resolve\n");

    const resolved = resolveImportChain(join(projectRoot, "CLAUDE.md"), projectRoot);

    expect(resolved.map((r) => r.path)).toEqual([join(projectRoot, "docs/legit.md")]);
    expect(resolved.some((r) => r.path.includes("outside.md"))).toBe(false);
  });

  test("an @import whose lexical path is in-project but whose target is a symlink escaping the root is skipped (gate-failure regression, wave 2 post-wave review)", () => {
    const root = tmp("ctx-scan-imports-symlink-escape-");
    const projectRoot = join(root, "proj");

    file(root, "outside-secret.md", "# Secret content living outside the project root\n");
    file(
      root,
      "proj/CLAUDE.md",
      ["# Root", "", "@docs/legit.md", "", "@docs/leaked.md", ""].join("\n"),
    );
    file(root, "proj/docs/legit.md", "# Legit — inside the project root\n");
    dir(root, "proj/docs");
    // The import LINE names an in-project-looking path ("docs/leaked.md"),
    // but the file at that path is a symlink whose TARGET is outside
    // projectRoot — a lexical-only containment check (comparing the joined
    // path string, never resolving the symlink) would incorrectly treat
    // this as in-project and let the next hop's readFileSync follow the
    // symlink and leak the outside file's content.
    symlinkSync(join(root, "outside-secret.md"), join(projectRoot, "docs/leaked.md"));

    const resolved = resolveImportChain(join(projectRoot, "CLAUDE.md"), projectRoot);

    expect(resolved.map((r) => r.path)).toEqual([join(projectRoot, "docs/legit.md")]);
    expect(resolved.some((r) => r.path.includes("leaked"))).toBe(false);
  });
});
