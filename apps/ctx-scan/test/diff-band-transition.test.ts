/**
 * diff-band-transition.test.ts — `ctx-scan-watch` task [4.2], beads:if-8kfk.
 *
 * Two fixture snapshots with exactly one seeded band transition (A4:
 * GREEN -> RED) among several rows that stay identical between the two —
 * asserts `diffSnapshots` reports that one transition and nothing else, the
 * exact "what regressed this week" contract `diff.ts`'s module doc names as
 * the real deliverable.
 */
import { describe, expect, test } from "bun:test";
import { diffSnapshots } from "../src/diff";
import { makeSnapshot } from "./fixtures/watch/build";

describe("diffSnapshots — band-transition report [4.2]", () => {
  test("reports exactly the one seeded A4 GREEN -> RED transition; every unchanged row is silent", () => {
    const projectPath = "/fixture/proj-a";

    const before = makeSnapshot(projectPath, "2026-01-01T00:00:00.000Z", {
      A1: "GREEN",
      A2: "AMBER",
      A3: "GREEN",
      A4: "GREEN",
      A5: "RED",
    });
    const after = makeSnapshot(projectPath, "2026-01-01T01:00:00.000Z", {
      A1: "GREEN", // unchanged
      A2: "AMBER", // unchanged
      A3: "GREEN", // unchanged
      A4: "RED", // <- the one seeded transition
      A5: "RED", // unchanged
    });

    const transitions = diffSnapshots(before, after);

    expect(transitions).toEqual([{ rule: "A4", from: "GREEN", to: "RED" }]);
  });

  test("no transitions when every row's band is identical between two snapshots", () => {
    const projectPath = "/fixture/proj-b";
    const bands = { A1: "GREEN", A2: "AMBER", A3: "RED" } as const;
    const a = makeSnapshot(projectPath, "2026-01-01T00:00:00.000Z", { ...bands });
    const b = makeSnapshot(projectPath, "2026-01-01T02:00:00.000Z", { ...bands });

    expect(diffSnapshots(a, b)).toEqual([]);
  });

  test("a row present in only one snapshot diffs against UNKNOWN on the missing side", () => {
    const projectPath = "/fixture/proj-c";
    const a = makeSnapshot(projectPath, "2026-01-01T00:00:00.000Z", { A1: "GREEN", A2: "AMBER" });
    const b = makeSnapshot(projectPath, "2026-01-01T00:05:00.000Z", { A1: "GREEN", A3: "RED" });

    const transitions = diffSnapshots(a, b);
    expect(transitions).toEqual([
      { rule: "A2", from: "AMBER", to: "UNKNOWN" },
      { rule: "A3", from: "UNKNOWN", to: "RED" },
    ]);
  });
});
