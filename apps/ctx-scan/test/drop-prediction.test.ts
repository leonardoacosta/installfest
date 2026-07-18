/**
 * drop-prediction.test.ts — deterministic least-invoked-first drop ordering
 * (ctx-scan-assembly task [4.2], beads:if-7kg9).
 *
 * Fixes a concrete invocation-frequency input (some counts tied) and a
 * budget that forces exactly three drops, then re-runs `capListingTotal`
 * many times against the SAME input to prove the resulting drop set + order
 * ranks are byte-for-byte identical every run — never re-derived, never
 * order-dependent on object/Set iteration.
 */
import { describe, expect, test } from "bun:test";
import { capListingTotal, type ListingEntryInput } from "../src/truncation";

function fixedEntries(): ListingEntryInput[] {
  // Five 100-char entries; invocation counts include a genuine tie (10, 10)
  // to exercise "ties keep input order" determinism, not just numeric sort.
  return [
    { id: "high-use", text: "a".repeat(100), invocations: 50 },
    { id: "tied-a", text: "b".repeat(100), invocations: 10 },
    { id: "mid-use", text: "c".repeat(100), invocations: 30 },
    { id: "tied-b", text: "d".repeat(100), invocations: 10 },
    { id: "least-use", text: "e".repeat(100), invocations: 5 },
  ];
}

describe("capListingTotal — deterministic least-invoked-first drop ordering [4.2]", () => {
  test("fixed invocation input drops the 3 least-invoked entries, ties in input order, every run", () => {
    const budget = 250; // 5 * 100 = 500 raw; must drop >= 3 entries of 100 chars to fit 250.

    const runs = Array.from({ length: 20 }, () => capListingTotal(fixedEntries(), budget));

    // All 20 independent runs produce byte-identical output.
    const first = runs[0]!;
    for (const run of runs) {
      expect(run).toEqual(first);
    }

    const result = runs[0]!;
    const droppedIds = result.filter((e) => e.dropped).map((e) => e.id);
    const keptIds = result.filter((e) => !e.dropped).map((e) => e.id);

    // Least-invoked-first: 5 drops before either tied-10, both tied-10s drop
    // before 30, and 30/50 (the two highest) are never dropped for a 3-drop budget.
    expect(droppedIds).toEqual(["tied-a", "tied-b", "least-use"]);
    expect(keptIds).toEqual(["high-use", "mid-use"]);

    // Order rank mirrors the raw invocation count for every entry (numeric, never "unknown").
    const orderById = Object.fromEntries(result.map((e) => [e.id, e.order]));
    expect(orderById).toEqual({
      "high-use": 50,
      "tied-a": 10,
      "mid-use": 30,
      "tied-b": 10,
      "least-use": 5,
    });

    // The remaining total actually fits the budget.
    const remainingChars = result.filter((e) => !e.dropped).reduce((sum, e) => sum + e.effective.length, 0);
    expect(remainingChars).toBeLessThanOrEqual(budget);
  });

  test("no invocation data at all -> nothing dropped, every entry order: unknown", () => {
    const entries: ListingEntryInput[] = [
      { id: "a", text: "x".repeat(100) },
      { id: "b", text: "y".repeat(100) },
    ];
    const result = capListingTotal(entries, 50); // budget far below total, but no ranking signal exists.

    expect(result.every((e) => e.dropped === false)).toBe(true);
    expect(result.every((e) => e.order === "unknown")).toBe(true);
  });
});
