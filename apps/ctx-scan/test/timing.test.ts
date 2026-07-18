/**
 * timing.test.ts — warm-run timing budget over a small multi-project fixture
 * (ctx-scan-core task [4.5], beads:if-z61j).
 *
 * Informational budget for this proposal's scope (a handful of fixture
 * projects) — informs, does not hard-gate, ctx-scan-assembly's later <5s
 * fleet-wide bar mentioned in the roadmap.
 */
import { afterEach, describe, expect, test } from "bun:test";
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { buildFleet } from "../src/cli";

const TIMING_BUDGET_MS = 2_000;

const cleanupDirs: string[] = [];

afterEach(async () => {
  while (cleanupDirs.length) {
    const dir = cleanupDirs.pop()!;
    await rm(dir, { recursive: true, force: true });
  }
});

async function tmp(prefix: string): Promise<string> {
  const dir = await mkdtemp(join(tmpdir(), prefix));
  cleanupDirs.push(dir);
  return dir;
}

describe("buildFleet timing [4.5]", () => {
  test("completes within budget for a small multi-project fixture tree", async () => {
    const root = await tmp("ctx-scan-timing-");
    for (const name of ["proj-a", "proj-b", "proj-c", "proj-d", "proj-e"]) {
      const proj = join(root, name);
      await mkdir(join(proj, ".git"), { recursive: true });
      await writeFile(join(proj, "CLAUDE.md"), `# ${name}\n`);
      // A moderate amount of unrelated file noise per project, so the walk
      // does real work rather than measuring an empty tree.
      await mkdir(join(proj, "src"), { recursive: true });
      for (let i = 0; i < 20; i++) {
        await writeFile(join(proj, "src", `file-${i}.ts`), `export const x${i} = ${i};\n`);
      }
    }

    const start = performance.now();
    const fleet = buildFleet(root);
    const elapsedMs = performance.now() - start;

    expect(fleet.projects).toHaveLength(5);
    expect(elapsedMs).toBeLessThan(TIMING_BUDGET_MS);
  });
});
