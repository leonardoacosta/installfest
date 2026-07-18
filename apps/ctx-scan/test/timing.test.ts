/**
 * timing.test.ts — warm-run timing budget over a small multi-project fixture
 * (ctx-scan-core task [4.5], beads:if-z61j).
 *
 * Informational budget for this proposal's scope (a handful of fixture
 * projects) — informs, does not hard-gate, ctx-scan-assembly's later <5s
 * fleet-wide bar mentioned in the roadmap.
 */
import { afterEach, describe, expect, test } from "bun:test";
import { buildFleet } from "../src/cli";
import { cleanup, dir, file, tmpRoot } from "./helpers/tree";

const TIMING_BUDGET_MS = 2_000;

const roots: string[] = [];

afterEach(() => {
  while (roots.length) cleanup(roots.pop()!);
});

function tmp(prefix: string): string {
  const root = tmpRoot(prefix);
  roots.push(root);
  return root;
}

describe("buildFleet timing [4.5]", () => {
  test("completes within budget for a small multi-project fixture tree", () => {
    const root = tmp("ctx-scan-timing-");
    for (const name of ["proj-a", "proj-b", "proj-c", "proj-d", "proj-e"]) {
      dir(root, `${name}/.git`);
      file(root, `${name}/CLAUDE.md`, `# ${name}\n`);
      // A moderate amount of unrelated file noise per project, so the walk
      // does real work rather than measuring an empty tree.
      for (let i = 0; i < 20; i++) {
        file(root, `${name}/src/file-${i}.ts`, `export const x${i} = ${i};\n`);
      }
    }

    const start = performance.now();
    const fleet = buildFleet(root);
    const elapsedMs = performance.now() - start;

    expect(fleet.projects).toHaveLength(5);
    expect(elapsedMs).toBeLessThan(TIMING_BUDGET_MS);
  });
});
