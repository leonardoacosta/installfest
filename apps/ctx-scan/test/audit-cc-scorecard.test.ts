/**
 * audit-cc-scorecard.test.ts — real `ctx-scan audit` run against `~/dev/cc`
 * reproduces `docs/context-budget-rubric.md` Part 2's scorecard bands
 * (ctx-scan-budgets task [4.5], beads:if-ujtc).
 *
 * This is a genuinely real, non-fixture run — no mocks, no tmp trees — of
 * the actual `ctx-scan audit` subprocess (`cli.ts`'s real argv parsing +
 * `runAudit` wiring) against this machine's real `~/dev/cc` tree. Verified
 * live before writing these assertions (`bun run src/cli.ts audit --root
 * ~/dev/cc`, 2026-07-18, exit 0, ~0.57s real time on this machine — NOT the
 * ~5.4s the impl-phase disclosed-gaps note describes; timing varies with
 * whether a telemetry probe endpoint is reachable, see `pipeline.ts`'s
 * hook-size caching, and is not asserted here):
 *
 *   A1  RED   measured=37644 (budget 8000)   — doc: RED, ~5.8x (46,200 chars,
 *       an external tool's Σ name+description+when_to_use; ctx-scan's own
 *       Requirement caps description+when_to_use only, ~30% lower — same
 *       explained gap `cc-audit-scan.test.ts` [4.8] already documents. Band
 *       matches; the exact multiplier does not, and is not asserted here.)
 *   A3  RED   measured=1191 (budget 1024)    — doc: RED (1 over, t3-code-patterns @ 1,190) — near-exact
 *   A4  RED   measured=759  (budget 500)     — doc: RED (6 over, max 2,527) — band matches, worst-node figure differs (doc's separate external tool vs ctx-scan's own worst-Node-in-Fleet convention)
 *   A7  RED   measured=15848 (budget 5000)   — doc: RED, ~1.6x the 10,000 [R] ceiling (~16,126 tok) — tight match (~2%), same tolerance `cc-audit-scan.test.ts` already establishes for this exact row
 *   A9  GREEN measured=0                     — doc: GREEN
 *   A11 AMBER measured=590  (budget 1024)    — doc: AMBER (plan.md/ux-journey-auditor, max 977) — near-exact
 *   A12 AMBER measured=8210 (budget 12000)   — doc: AMBER (9,569 chars) — same order of magnitude, both AMBER
 *   A13 GREEN measured=0                     — doc: GREEN
 *
 * A5 is asserted UNKNOWN/not-computable, NOT AMBER as the doc's scorecard
 * states — this is the disclosed, out-of-scope impl gap (no `references/*.md`
 * ingestion path exists in the Fleet/Node model), a legitimate documented
 * mismatch per this task's own instructions, not a test failure to force-pass.
 */
import { describe, expect, test } from "bun:test";
import { homedir } from "node:os";
import { join } from "node:path";
import type { AuditResult } from "../src/audit";

const APP_DIR = join(import.meta.dir, "..");
const CC_ROOT = join(homedir(), "dev", "cc");

describe("ctx-scan audit vs. the 2026-07-18 rubric scorecard, real ~/dev/cc [4.5]", () => {
  test(
    "reproduces the scorecard's bands for every row the impl can actually compute",
    () => {
      const proc = Bun.spawnSync(["bun", "run", "src/cli.ts", "audit", "--root", CC_ROOT], {
        cwd: APP_DIR,
        stdout: "pipe",
        stderr: "pipe",
      });

      expect(proc.exitCode).toBe(0);
      const result: AuditResult = JSON.parse(proc.stdout.toString("utf8"));
      expect(result.error).toBeNull();

      // eslint-disable-next-line no-console
      console.log("[4.5] real `ctx-scan audit --root ~/dev/cc` rows:", JSON.stringify(result.rows, null, 2));

      const byId = new Map(result.rows.map((r) => [r.id, r]));

      // A1: RED, well over budget — real multiplier differs from the doc's
      // external-tool measurement (see header), so only the band + a wide
      // "clearly over budget" bound are asserted.
      const a1 = byId.get("A1")!;
      expect(a1.band).toBe("RED");
      expect(a1.measured!).toBeGreaterThan(a1.budget * 2);

      const a3 = byId.get("A3")!;
      expect(a3.band).toBe("RED");
      expect(a3.measured!).toBeGreaterThan(a3.budget);

      const a4 = byId.get("A4")!;
      expect(a4.band).toBe("RED");
      expect(a4.measured!).toBeGreaterThan(a4.budget);

      const a7 = byId.get("A7")!;
      expect(a7.band).toBe("RED");
      // Doc: ~1.6x the 10,000-token RED ceiling (16,126 tok). Tight ballpark.
      expect(a7.measured!).toBeGreaterThan(10_000);
      expect(a7.measured!).toBeLessThan(20_000);

      const a9 = byId.get("A9")!;
      expect(a9.band).toBe("GREEN");

      const a13 = byId.get("A13")!;
      expect(a13.band).toBe("GREEN");

      const a11 = byId.get("A11")!;
      expect(a11.band).toBe("AMBER");

      const a12 = byId.get("A12")!;
      expect(a12.band).toBe("AMBER");

      // A5: the disclosed, out-of-scope gap. The scorecard says AMBER
      // (systemic ToC gap, 79 files); today's impl cannot compute this row
      // at all (no references/*.md NodeClass) and MUST say so honestly
      // rather than fabricating a band — this is the documented mismatch,
      // not a bug.
      const a5 = byId.get("A5")!;
      expect(a5.computable).toBe(false);
      expect(a5.band).toBe("UNKNOWN");
      expect(a5.measured).toBeNull();
      expect(a5.note).toBeTruthy();
    },
    30_000,
  );
});
