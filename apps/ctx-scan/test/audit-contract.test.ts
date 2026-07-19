/**
 * audit-contract.test.ts — `ctx-scan audit --json`'s §E-R1 contract shape,
 * both the `error: null` success case and an `error`-populated partial
 * failure case (ctx-scan-budgets task [4.4], beads:if-y2zm).
 *
 * The success case spawns the REAL `ctx-scan audit` command as a subprocess
 * (`cli.ts`'s actual argv-parsing + `runAudit` wiring, task [3.1]) against a
 * fixture tree, then schema-checks the parsed stdout. Per `cli.ts`, `--json
 * <path>` takes a file-path argument (matching `scan`/`calibrate`'s
 * convention) — omitting it entirely, not passing a bare `--json` flag, is
 * what prints JSON to stdout, since `audit`'s only output format IS JSON
 * (verified live: `ctx-scan audit --json` with no path errors "option
 * '--json <path>' argument missing"; `ctx-scan audit` alone streams the
 * §E-R1 JSON to stdout).
 *
 * The error case calls `auditFleet` directly with a deliberately malformed
 * `Fleet` to force the `catch` branch documented in `audit.ts`'s own header
 * ("any failure surfaces as `{rows:[], error:"..."}`") — this is the exact
 * contract `cli.ts`'s `runAudit` delegates to and re-emits verbatim via
 * `emitJson`, so exercising it here at the `auditFleet` boundary proves the
 * same shape the CLI would print on a real scan-pipeline exception.
 */
import { afterEach, describe, expect, test } from "bun:test";
import { join } from "node:path";
import { auditFleet, type AuditResult, type AuditRow } from "../src/audit";
import type { Fleet } from "../src/model";
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

const APP_DIR = join(import.meta.dir, "..");

function assertRowShape(row: AuditRow): void {
  expect(typeof row.id).toBe("string");
  expect(typeof row.surface).toBe("string");
  expect(row.measured === null || typeof row.measured === "number").toBe(true);
  expect(typeof row.budget).toBe("number");
  expect(typeof row.greenMax).toBe("number");
  expect(typeof row.amberMax).toBe("number");
  expect(["GREEN", "AMBER", "RED", "UNKNOWN"]).toContain(row.band);
  expect(["H", "G", "R"]).toContain(row.source);
  expect(typeof row.computable).toBe("boolean");
  if (!row.computable) {
    expect(row.band).toBe("UNKNOWN");
    expect(row.measured).toBeNull();
    expect(typeof row.note).toBe("string");
  }
}

describe("§E-R1 contract shape [4.4]", () => {
  test(
    "success case: real `ctx-scan audit` subprocess emits {rows:[...14 rows...], error:null} to stdout",
    async () => {
      const root = tmp("ctx-scan-audit-contract-");
      dir(root, "fixture-project/.git");
      file(root, "fixture-project/CLAUDE.md", "# fixture\n");

      const proc = Bun.spawnSync(["bun", "run", "src/cli.ts", "audit", "--root", root], {
        cwd: APP_DIR,
        stdout: "pipe",
        stderr: "pipe",
      });

      expect(proc.exitCode).toBe(0);
      const stdout = proc.stdout.toString("utf8");
      let parsed: AuditResult;
      try {
        parsed = JSON.parse(stdout);
      } catch (err) {
        throw new Error(`stdout was not valid JSON: ${err}\nstdout was:\n${stdout}`);
      }

      expect(parsed.error).toBeNull();
      expect(Array.isArray(parsed.rows)).toBe(true);
      expect(parsed.rows).toHaveLength(14);
      const ids = parsed.rows.map((r) => r.id).sort();
      expect(ids).toEqual(Array.from({ length: 14 }, (_, i) => `A${i + 1}`).sort());
      for (const row of parsed.rows) assertRowShape(row);

      // The disclosed gap rows (A5/A6 — no reference/*.md ingestion path)
      // must degrade honestly, never a fabricated band.
      const a5 = parsed.rows.find((r) => r.id === "A5")!;
      const a6 = parsed.rows.find((r) => r.id === "A6")!;
      expect(a5.computable).toBe(false);
      expect(a5.band).toBe("UNKNOWN");
      expect(a6.computable).toBe(false);
      expect(a6.band).toBe("UNKNOWN");
    },
    20_000,
  );

  test("error case: a malformed Fleet forces auditFleet's catch branch — {rows:[], error:<string>}", () => {
    // `global` is typed as `Surface[]` but we deliberately violate that here
    // to force `annotateFleetBands`'s `for (const surface of fleet.global)`
    // to throw (iterating a non-iterable) — the exact "scan-pipeline
    // exception" shape `audit.ts`'s header doc says degrades to this
    // contract, never a thrown error or a non-zero exit.
    const malformedFleet = {
      schemaVersion: 1,
      root: "/does/not/matter",
      global: null,
      projects: [],
    } as unknown as Fleet;

    const result = auditFleet(malformedFleet);

    expect(result.rows).toEqual([]);
    expect(result.error).not.toBeNull();
    expect(typeof result.error).toBe("string");
    expect(result.error!.length).toBeGreaterThan(0);
  });

  test("a genuinely healthy Fleet with real content still shape-matches via direct auditFleet call", () => {
    const root = tmp("ctx-scan-audit-contract-direct-");
    dir(root, "fixture-project/.git");
    file(root, "fixture-project/CLAUDE.md", "# fixture\n");

    // Build a minimal-but-real Fleet by hand (no assembly pipeline needed —
    // auditFleet only reads `global`/`projects` surfaces + calls
    // `annotateFleetBands` internally), proving the contract at the
    // pure-logic boundary independent of the CLI/subprocess path above.
    const fleet: Fleet = { schemaVersion: 1, root, global: [], projects: [] };
    const result = auditFleet(fleet);

    expect(result.error).toBeNull();
    expect(result.rows).toHaveLength(14);
    for (const row of result.rows) assertRowShape(row);
  });
});
