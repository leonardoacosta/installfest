/**
 * schema.test.ts — Fleet/Project/Surface/Node shape snapshot lock
 * (ctx-scan-core task [4.4], beads:if-linf).
 *
 * Asserts specific field presence + schemaVersion rather than a loose
 * toMatchObject, so a genuine shape drift (a renamed/removed field) fails
 * this test even without a deliberate mutation — the point is that ANY
 * unversioned shape change trips it.
 *
 * UPDATED (ctx-scan-assembly task [3.2]): `buildFleet` now runs the real
 * assembly pipeline, including hook-injection sizing ([2.6]) — when a
 * telemetry endpoint is reachable, that's one real (network-bound) Loki
 * query. An explicit timeout override keeps this from exceeding bun:test's
 * 5000ms default under real, variable telemetry latency (see the same note
 * in `timing.test.ts`).
 */
import { afterEach, describe, expect, test } from "bun:test";
import { buildFleet } from "../src/cli";
import { schemaVersion } from "../src/model";
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

describe("Fleet document shape [4.4]", () => {
  test(
    "matches the committed schema exactly for a fixed fixture",
    async () => {
      const root = tmp("ctx-scan-schema-");
      dir(root, "fixture-project/.git");
      file(root, "fixture-project/CLAUDE.md", "# fixture\n");

      const { fleet } = await buildFleet(root);

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
    },
    15_000,
  );
});
