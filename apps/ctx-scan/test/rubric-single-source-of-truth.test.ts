/**
 * rubric-single-source-of-truth.test.ts — mutating one `TABLE_A` constant
 * changes BOTH `ctx-scan scan`'s band-annotated `Node` output (via
 * `computeNodeBands`) and `ctx-scan audit --json`'s row output (via
 * `auditFleet`) identically — proving the single-source-of-truth guarantee
 * `rubric.ts`'s own module header exists to establish (ctx-scan-budgets
 * task [4.6], beads:if-150o).
 *
 * Mutates A2's amber ceiling (normally `LISTING_ENTRY_CAP_CHARS` = 1,536,
 * per the task's own example) directly on the live `TABLE_A` array — the
 * exact object both `computeNodeBands` (task [2.2]) and `audit.ts`'s
 * `buildRow` (task [2.3]) read at call time, since neither module snapshots
 * a copy. The mutation happens and is restored within a single synchronous
 * test body (no `await` in between), so no other test file sharing this
 * process's module cache can observe the intermediate mutated state.
 */
import { describe, expect, test } from "bun:test";
import { auditFleet } from "../src/audit";
import { TABLE_A, computeNodeBands } from "../src/rubric";
import type { Fleet, Node } from "../src/model";

function buildFixtureNode(rawChars: number): Node {
  return {
    path: "/fixture/does-not-need-to-exist.md", // A2's measurer is n.raw_chars — no file re-read
    cls: "skills-listing",
    tier: 1,
    raw_chars: rawChars,
    effective_chars: 0,
    est_tokens: 0,
    origin: "global",
    truncations: [],
    bands: [],
  };
}

function buildFixtureFleet(node: Node): Fleet {
  return {
    schemaVersion: 1,
    root: "/fixture",
    global: [{ cls: "skills-listing", nodes: [node] }],
    projects: [],
  };
}

describe("mutating one TABLE_A constant flows identically to both consumers [4.6]", () => {
  test("A2's amber ceiling: scan's Node.bands and audit's row band move AMBER->RED together, then restore", () => {
    const a2 = TABLE_A.find((r) => r.id === "A2");
    if (!a2) throw new Error("fixture setup error: no TABLE_A row A2");
    const originalAmberMax = a2.amberMax;
    const originalLimit = a2.limit;

    // raw_chars=1400: originally AMBER under the real 1,536-char cap
    // (greenMax=1,229, amberMax=1,536 — 1,230-1,536 is the AMBER band).
    const node = buildFixtureNode(1400);
    const fleet = buildFixtureFleet(node);

    // BEFORE mutation — both consumers agree on AMBER against the real cap.
    const nodeBandBefore = computeNodeBands(node).find((b) => b.rule === "A2");
    expect(nodeBandBefore?.band).toBe("AMBER");

    const auditBefore = auditFleet(fleet);
    const auditRowBefore = auditBefore.rows.find((r) => r.id === "A2");
    expect(auditRowBefore?.band).toBe("AMBER");
    expect(auditRowBefore?.amberMax).toBe(originalAmberMax);

    try {
      // MUTATE the single shared constant — same edit shape as the task's
      // own example ("A2's 1,536-char limit"), lowered so 1,400 now RED.
      a2.amberMax = 1300;
      a2.limit = 1300;

      const nodeBandAfter = computeNodeBands(node).find((b) => b.rule === "A2");
      expect(nodeBandAfter?.band).toBe("RED");
      expect(nodeBandAfter?.limit).toBe(1300);

      const auditAfter = auditFleet(fleet);
      const auditRowAfter = auditAfter.rows.find((r) => r.id === "A2");
      expect(auditRowAfter?.band).toBe("RED");
      expect(auditRowAfter?.amberMax).toBe(1300);
      expect(auditRowAfter?.budget).toBe(1300);

      // Identical outcome from ONE constant edit — proves there is exactly
      // one threshold source, not two independently-maintained ones. Cast to
      // `string` for the comparison only: `Band.band` (`BandVerdict`) and
      // `AuditRow.band` (`BandVerdict | "UNKNOWN"`) are deliberately
      // different-width types (an audit row can be UNKNOWN, a computed Node
      // band never is), so `toBe`'s generic can't unify them directly.
      expect(nodeBandAfter?.band as string).toBe(auditRowAfter?.band as string);
    } finally {
      a2.amberMax = originalAmberMax;
      a2.limit = originalLimit;
    }

    // Restore verified: both consumers report the original AMBER band again
    // — proves the mutation didn't leak into any other test in this process.
    const nodeBandRestored = computeNodeBands(node).find((b) => b.rule === "A2");
    expect(nodeBandRestored?.band).toBe("AMBER");
    const auditRestored = auditFleet(fleet);
    expect(auditRestored.rows.find((r) => r.id === "A2")?.band).toBe("AMBER");
  });
});
