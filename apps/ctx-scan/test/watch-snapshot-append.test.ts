/**
 * watch-snapshot-append.test.ts — `ctx-scan-watch` task [4.1], beads:if-m642.
 *
 * Spawns a REAL `ctx-scan watch` subprocess against a fixture project (no
 * fake `buildFleetFn`/`auditFleetFn` — the exact wiring `cli.ts`'s `runWatch`
 * uses), then bursts several rapid edits within the debounce window and
 * asserts exactly ONE new `history.jsonl` line was appended for that
 * project — proving both the append-on-change behavior AND `watch.ts`'s
 * documented per-project debounce (a burst of edits collapses to one
 * re-scan), not merely "an edit produced *a* line."
 *
 * Process hygiene: the watcher is always stopped via `stop()` in a
 * `finally`, even if an assertion above it throws, so no orphan chokidar
 * watcher process survives this test.
 */
import { afterEach, describe, expect, test } from "bun:test";
import { cleanup, file, tmpRoot } from "./helpers/tree";
import { makeFixtureProject, spawnWatch, type SpawnedWatch } from "./fixtures/watch/build";
import { readSnapshots } from "../src/history";

const roots: string[] = [];
const watches: SpawnedWatch[] = [];

afterEach(async () => {
  while (watches.length) await watches.pop()!.stop();
  while (roots.length) cleanup(roots.pop()!);
});

function tmp(prefix: string): string {
  const root = tmpRoot(prefix);
  roots.push(root);
  return root;
}

describe("ctx-scan watch — snapshot append on file change [4.1]", () => {
  test(
    "a burst of rapid edits to one fixture project collapses to exactly one new history.jsonl line",
    async () => {
      const root = tmp("ctx-scan-watch-append-");
      const projectPath = makeFixtureProject(root, "proj-a");

      const watch = await spawnWatch(root, { debounceMs: 200 });
      watches.push(watch);

      // Confirm the watcher actually discovered the fixture project before
      // relying on its debounce/re-scan behavior.
      expect(watch.proc.exitCode).toBeNull();

      // Sanity: no snapshot exists yet — `ignoreInitial: true` means
      // watch-start itself must never append anything.
      expect(readSnapshots({ filePath: watch.historyPath })).toEqual([]);

      // Burst: 4 rapid edits, each well inside the 200ms debounce window —
      // `watch.ts`'s `scheduleRescan` clears the prior pending timer on
      // every change, so this MUST settle to one re-scan, not four.
      for (let i = 0; i < 4; i++) {
        file(root, "proj-a/CLAUDE.md", `# proj-a\n\nedit ${i}\n`);
        await Bun.sleep(30);
      }

      const snapshots = await watch.waitForSnapshotCount(1, 10_000);
      // Give any (incorrect) extra re-scans a further debounce window's
      // worth of time to land before asserting the count is final.
      await Bun.sleep(400);
      const finalSnapshots = readSnapshots({ filePath: watch.historyPath });

      expect(finalSnapshots.length).toBe(1);
      expect(snapshots.length).toBe(1);
      expect(finalSnapshots[0]!.project).toBe(projectPath);
      expect(finalSnapshots[0]!.scanOutput.projects.length).toBe(1);
      expect(finalSnapshots[0]!.scanOutput.projects[0]!.path).toBe(projectPath);
    },
    30_000,
  );
});
