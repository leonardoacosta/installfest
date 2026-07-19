/**
 * watch-realtime.test.ts — `ctx-scan-watch` task [4.3], beads:if-o5ot.
 *
 * Real-time responsiveness: edits a fixture project's `CLAUDE.md` while a
 * REAL `ctx-scan watch` subprocess is running against it, and asserts a new
 * `history.jsonl` snapshot appears within 5 seconds — the proposal's own
 * "re-scan on file-change" latency bar. A temp-dir fixture project is used
 * (per this batch's process-hygiene instructions) rather than editing this
 * repo's own real `CLAUDE.md`, so this run never mutates or races the real
 * project's own watch/history state.
 *
 * Process hygiene: `stop()` always runs in `afterEach`, even on assertion
 * failure, so no orphan watcher process survives this test.
 */
import { afterEach, describe, expect, test } from "bun:test";
import { cleanup, file, tmpRoot } from "./helpers/tree";
import { makeFixtureProject, spawnWatch, type SpawnedWatch } from "./fixtures/watch/build";

const REALTIME_BUDGET_MS = 5_000;

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

describe("ctx-scan watch — real-time snapshot latency [4.3]", () => {
  test(
    "editing CLAUDE.md while watch is running produces a new snapshot within 5 seconds",
    async () => {
      const root = tmp("ctx-scan-watch-realtime-");
      const projectPath = makeFixtureProject(
        root,
        "proj-realtime",
        "# proj-realtime\n\nA genuine project CLAUDE.md — real content, not a stub, per this task's real-time edit contract.\n",
      );

      const watch = await spawnWatch(root, { debounceMs: 150 });
      watches.push(watch);

      const editedAt = Date.now();
      file(
        root,
        "proj-realtime/CLAUDE.md",
        "# proj-realtime\n\nEdited while `ctx-scan watch` was running — this line was added for [4.3].\n",
      );

      const snapshots = await watch.waitForSnapshotCount(1, REALTIME_BUDGET_MS);
      const elapsedMs = Date.now() - editedAt;

      // eslint-disable-next-line no-console
      console.log(`[4.3] new snapshot observed ${elapsedMs}ms after the CLAUDE.md edit (budget ${REALTIME_BUDGET_MS}ms)`);

      expect(elapsedMs).toBeLessThan(REALTIME_BUDGET_MS);
      expect(snapshots.length).toBe(1);
      expect(snapshots[0]!.project).toBe(projectPath);
      // The snapshot's own timestamp was captured after the edit was made.
      expect(new Date(snapshots[0]!.timestamp).getTime()).toBeGreaterThanOrEqual(editedAt);
    },
    30_000,
  );
});
