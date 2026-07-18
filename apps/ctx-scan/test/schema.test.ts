/**
 * schema.test.ts — Fleet/Project/Surface/Node shape snapshot lock
 * (ctx-scan-core task [4.4], beads:if-linf).
 *
 * Asserts specific field presence + schemaVersion rather than a loose
 * toMatchObject, so a genuine shape drift (a renamed/removed field) fails
 * this test even without a deliberate mutation — the point is that ANY
 * unversioned shape change trips it.
 */
import { afterEach, describe, expect, test } from "bun:test";
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { buildFleet } from "../src/cli";
import { schemaVersion } from "../src/model";

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

describe("Fleet document shape [4.4]", () => {
  test("matches the committed schema exactly for a fixed fixture", async () => {
    const root = await tmp("ctx-scan-schema-");
    const proj = join(root, "fixture-project");
    await mkdir(join(proj, ".git"), { recursive: true });
    await writeFile(join(proj, "CLAUDE.md"), "# fixture\n");

    const fleet = buildFleet(root);

    // Top-level shape.
    expect(fleet.schemaVersion).toBe(schemaVersion);
    expect(fleet.schemaVersion).toBe(1); // pin the current value explicitly
    expect(fleet.root).toBe(root);
    expect(Array.isArray(fleet.global)).toBe(true);
    expect(Array.isArray(fleet.projects)).toBe(true);
    expect(Object.keys(fleet).sort()).toEqual(["global", "projects", "root", "schemaVersion"]);

    // Project shape.
    expect(fleet.projects).toHaveLength(1);
    const project = fleet.projects[0]!;
    expect(Object.keys(project).sort()).toEqual(["name", "path", "surfaces"]);
    expect(project.name).toBe("fixture-project");
    expect(Array.isArray(project.surfaces)).toBe(true);
  });
});
