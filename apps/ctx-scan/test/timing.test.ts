/**
 * timing.test.ts — warm-run timing budget over a small multi-project fixture
 * (ctx-scan-core task [4.5], beads:if-z61j).
 *
 * Informational budget for this proposal's scope (a handful of fixture
 * projects) — informs, does not hard-gate, ctx-scan-assembly's later <5s
 * fleet-wide bar mentioned in the roadmap.
 *
 * UPDATED (ctx-scan-assembly task [3.2]): `buildFleet` now runs the real
 * assembly pipeline, including hook-injection sizing ([2.6]) — when a
 * telemetry endpoint is reachable (verified live on this machine: a running
 * `loki` docker container), that adds one real Loki query to the fleet-wide
 * cost. `pipeline.ts`'s `hookSizeCache` collapses this to ONE real
 * telemetry-endpoint resolution + query per distinct hook-definition set for
 * the whole scan (not one per project) — measured live: a 200-event
 * `hook_output_metrics` query against this machine's real Loki container
 * alone took ~4.6s, and 5 fixture projects sharing the identical
 * global-inherited hook set completed in ~5.2s total (vs. ~11s uncached).
 * The budget below is set above that real, network-bound cost (not the
 * original 2s, which predates hook-telemetry ingestion existing at all) —
 * still tight enough to catch a real regression (e.g. the cache being
 * removed, which would multiply this cost by project count).
 */
import { afterEach, describe, expect, test } from "bun:test";
import { buildFleet } from "../src/cli";
import { cleanup, dir, file, tmpRoot } from "./helpers/tree";

const TIMING_BUDGET_MS = 15_000;

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
  test(
    "completes within budget for a small multi-project fixture tree",
    async () => {
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
      // --probe-hooks stays off (the CLI default) — the point of this budget
      // is file-walk + truncation cost plus at most one real (cached)
      // telemetry round trip, not per-project network-bound hook probing.
      const { fleet } = await buildFleet(root, { allowProbeHooks: false });
      const elapsedMs = performance.now() - start;

      expect(fleet.projects).toHaveLength(5);
      expect(elapsedMs).toBeLessThan(TIMING_BUDGET_MS);
    },
    TIMING_BUDGET_MS + 5_000,
  );
});
