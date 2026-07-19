/**
 * diff-cli.test.ts — `ctx-scan-watch` task [4.4], beads:if-w2z4.
 *
 * Runs the REAL `ctx-scan diff <a> <b>` subprocess (`cli.ts`'s actual argv
 * parsing + `runDiff` wiring, task [3.1]) against the same two fixture
 * snapshots [4.2] seeds (one A4 GREEN -> RED transition, everything else
 * unchanged) — asserts the printed stdout matches the seeded transition
 * exactly, both in human-readable mode and `--json` mode. Mirrors
 * `audit-contract.test.ts`'s own real-subprocess convention.
 */
import { afterEach, describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { cleanup, tmpRoot } from "./helpers/tree";
import { APP_DIR, makeSnapshot, writeSnapshots } from "./fixtures/watch/build";

const roots: string[] = [];
afterEach(() => {
  while (roots.length) cleanup(roots.pop()!);
});
function tmp(prefix: string): string {
  const root = tmpRoot(prefix);
  roots.push(root);
  return root;
}

const TS_A = "2026-02-01T00:00:00.000Z";
const TS_B = "2026-02-01T01:00:00.000Z";

function seedHistory(historyPath: string): void {
  const projectPath = "/fixture/proj-cli-diff";
  writeSnapshots(historyPath, [
    makeSnapshot(projectPath, TS_A, { A1: "GREEN", A2: "AMBER", A3: "GREEN", A4: "GREEN", A5: "RED" }),
    makeSnapshot(projectPath, TS_B, { A1: "GREEN", A2: "AMBER", A3: "GREEN", A4: "RED", A5: "RED" }),
  ]);
}

describe("ctx-scan diff — CLI command [4.4]", () => {
  test("human-readable stdout prints exactly the seeded A4 GREEN -> RED transition, nothing else", () => {
    const root = tmp("ctx-scan-diff-cli-");
    const historyPath = join(root, "history.jsonl");
    seedHistory(historyPath);

    const proc = Bun.spawnSync(["bun", "run", "src/cli.ts", "diff", TS_A, TS_B, "--history", historyPath], {
      cwd: APP_DIR,
      stdout: "pipe",
      stderr: "pipe",
    });

    expect(proc.exitCode).toBe(0);
    const stderr = proc.stderr.toString();
    expect(stderr).toBe("");
    expect(proc.stdout.toString()).toBe("A4: GREEN → RED\n");
  });

  test("--json mode emits the same single transition as structured JSON", () => {
    const root = tmp("ctx-scan-diff-cli-json-");
    const historyPath = join(root, "history.jsonl");
    seedHistory(historyPath);
    const jsonOutPath = join(root, "diff-out.json");

    const proc = Bun.spawnSync(
      ["bun", "run", "src/cli.ts", "diff", TS_A, TS_B, "--history", historyPath, "--json", jsonOutPath],
      { cwd: APP_DIR, stdout: "pipe", stderr: "pipe" },
    );

    expect(proc.exitCode).toBe(0);
    const parsed = JSON.parse(readFileSync(jsonOutPath, "utf8"));
    expect(parsed).toEqual({ transitions: [{ rule: "A4", from: "GREEN", to: "RED" }], error: null });
  });

  test("an unresolvable selector surfaces a stderr error and a non-zero exit code, never a crash", () => {
    const root = tmp("ctx-scan-diff-cli-bad-selector-");
    const historyPath = join(root, "history.jsonl");
    seedHistory(historyPath);

    const proc = Bun.spawnSync(["bun", "run", "src/cli.ts", "diff", "not-a-real-timestamp", TS_B, "--history", historyPath], {
      cwd: APP_DIR,
      stdout: "pipe",
      stderr: "pipe",
    });

    expect(proc.exitCode).toBe(1);
    expect(proc.stderr.toString()).toContain("snapshot not found");
    expect(proc.stdout.toString()).toBe("");
  });
});
