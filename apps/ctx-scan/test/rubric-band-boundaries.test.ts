/**
 * rubric-band-boundaries.test.ts — GREEN/AMBER and AMBER/RED transition
 * points for a Rule-1, Rule-2, and Rule-3 row (ctx-scan-budgets task [4.2],
 * beads:if-ug8h).
 *
 * Uses `bandFor` — the live banding path both `annotateFleetBands` (task
 * [2.2]) and `audit.ts` (task [2.3]) actually call, classifying a measured
 * value against a row's own stored (doc-verbatim) `greenMax`/`amberMax` —
 * NOT a fresh `deriveBandRule1/2/3(measured, limit)` reconstruction, since
 * `rubric.ts`'s own DESIGN NOTE documents that some rows (A11, A12, A13)
 * don't reconstruct cleanly from a single 0.8*L/V*2 formula application of
 * their stored `limit`. Where a row's numbers DO reconstruct cleanly (A3
 * for Rule 1, A7 for Rule 2 — both called out in that DESIGN NOTE), this
 * test also cross-checks `deriveBandRule1`/`deriveBandRule2` directly
 * against the same boundary values, so both the live path and the
 * documented-derivation functions are proven correct where they're
 * expected to agree.
 */
import { describe, expect, test } from "bun:test";
import { TABLE_A, bandFor, deriveBandRule1, deriveBandRule2 } from "../src/rubric";

function row(id: string) {
  const r = TABLE_A.find((x) => x.id === id);
  if (!r) throw new Error(`fixture setup error: no TABLE_A row ${id}`);
  return r;
}

describe("Band boundary transitions [4.2]", () => {
  describe("Rule-1 row (A3 — per-skill description alone, greenMax=819, amberMax=1024)", () => {
    const a3 = row("A3");

    test("fixture sanity: A3 is tagged bandRule rule1", () => {
      expect(a3.bandRule).toBe("rule1");
    });

    test("greenMax exactly (819) is GREEN", () => {
      expect(bandFor(a3, 819)).toBe("GREEN");
      expect(deriveBandRule1(819, a3.limit)).toBe("GREEN");
    });
    test("greenMax + 1 (820) crosses into AMBER", () => {
      expect(bandFor(a3, 820)).toBe("AMBER");
      expect(deriveBandRule1(820, a3.limit)).toBe("AMBER");
    });
    test("amberMax exactly (1024) is still AMBER", () => {
      expect(bandFor(a3, 1024)).toBe("AMBER");
      expect(deriveBandRule1(1024, a3.limit)).toBe("AMBER");
    });
    test("amberMax + 1 (1025) crosses into RED", () => {
      expect(bandFor(a3, 1025)).toBe("RED");
      expect(deriveBandRule1(1025, a3.limit)).toBe("RED");
    });
  });

  describe("Rule-2 row (A7 — always-loaded CLAUDE.md chain, greenMax=5000, amberMax=10000)", () => {
    const a7 = row("A7");

    test("fixture sanity: A7 is tagged bandRule rule2", () => {
      expect(a7.bandRule).toBe("rule2");
    });

    test("greenMax exactly (5000) is GREEN", () => {
      expect(bandFor(a7, 5000)).toBe("GREEN");
      expect(deriveBandRule2(5000, a7.limit)).toBe("GREEN"); // limit=5000 is Rule 2's guidance value V
    });
    test("greenMax + 1 (5001) crosses into AMBER", () => {
      expect(bandFor(a7, 5001)).toBe("AMBER");
      expect(deriveBandRule2(5001, a7.limit)).toBe("AMBER");
    });
    test("amberMax exactly (10000) is still AMBER", () => {
      expect(bandFor(a7, 10000)).toBe("AMBER");
      expect(deriveBandRule2(10000, a7.limit)).toBe("AMBER");
    });
    test("amberMax + 1 (10001) crosses into RED", () => {
      expect(bandFor(a7, 10001)).toBe("RED");
      expect(deriveBandRule2(10001, a7.limit)).toBe("RED");
    });
  });

  describe("Rule-3 row (A11 — agent description, greenMax=500, amberMax=1024)", () => {
    const a11 = row("A11");

    test("fixture sanity: A11 is tagged bandRule rule3", () => {
      expect(a11.bandRule).toBe("rule3");
    });

    // A11 is one of the DESIGN NOTE's called-out non-reconstructing rows —
    // its published greenMax (500) is a hand-picked round number, not
    // 0.8*1024 (819). The live `bandFor` path (used by production code) is
    // what's under test here, deliberately NOT `deriveBandRule3`.
    test("greenMax exactly (500) is GREEN", () => {
      expect(bandFor(a11, 500)).toBe("GREEN");
    });
    test("greenMax + 1 (501) crosses into AMBER", () => {
      expect(bandFor(a11, 501)).toBe("AMBER");
    });
    test("amberMax exactly (1024) is still AMBER", () => {
      expect(bandFor(a11, 1024)).toBe("AMBER");
    });
    test("amberMax + 1 (1025) crosses into RED", () => {
      expect(bandFor(a11, 1025)).toBe("RED");
    });
  });
});
