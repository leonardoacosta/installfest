/**
 * collect.test.ts — collector fail-open + atomic-write + schema suite
 * (add-daily-brief-tui task 4.1, beads:if-mbm6).
 *
 * Runs the REAL `collect()` export as a fresh subprocess (see
 * run-open-items.ts's header comment for why) against:
 *   - a definitely-refused MX_GATEWAY_URL (mx-gateway down)
 *   - the openItems fixture registry (one repo errors, others populate)
 *   - nonexistent docs-state paths (missing docs state files)
 *   - a scratch DAILY_BRIEF_STATE_DIR (never Leo's real
 *     ~/.local/state/daily-brief/), so the atomic-write behavior can be
 *     inspected without touching real state.
 */
import { afterAll, beforeAll, describe, expect, test } from "bun:test";
import { existsSync } from "node:fs";
import { mkdtemp, readdir, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { buildProjectsFixture, type ProjectsFixture } from "./helpers/projectsFixture";
import { runBunScript } from "./helpers/runBun";
import type { DailyBriefSnapshot } from "../src/collect";

/** Starts a Bun HTTP server then immediately stops it, handing back a port
 * nothing is listening on — a deterministic "mx-gateway down" simulation
 * (connections to it reliably get ECONNREFUSED). */
function unusedPort(): number {
  const server = Bun.serve({ port: 0, fetch: () => new Response("ok") });
  const port = server.port;
  server.stop(true);
  return port;
}

describe("collect() fail-open composition + atomic writes + schema", () => {
  let fixture: ProjectsFixture;
  let stateDir: string;
  let hygienePath: string;
  let sweepPath: string;
  let deadMxUrl: string;

  beforeAll(async () => {
    fixture = await buildProjectsFixture();
    stateDir = await mkdtemp(join(tmpdir(), "daily-brief-state-"));
    hygienePath = join(stateDir, "nonexistent-hygiene-results.jsonl");
    sweepPath = join(stateDir, "nonexistent-sweep-last-run.json");
    deadMxUrl = `http://127.0.0.1:${unusedPort()}`;
  });

  afterAll(async () => {
    await fixture.cleanup();
    await rm(stateDir, { recursive: true, force: true });
  });

  async function runCollect(): Promise<{ snapshot: DailyBriefSnapshot; exitCode: number; stderr: string }> {
    const { stdout, stderr, exitCode } = await runBunScript("run-collect.ts", [], {
      DOTFILES: fixture.dotfilesDir,
      MX_GATEWAY_URL: deadMxUrl,
      DAILY_BRIEF_STATE_DIR: stateDir,
      DAILY_BRIEF_HYGIENE_RESULTS_PATH: hygienePath,
      DAILY_BRIEF_SWEEP_LAST_RUN_PATH: sweepPath,
    });
    return { snapshot: JSON.parse(stdout) as DailyBriefSnapshot, exitCode, stderr };
  }

  test("mx-gateway down: snapshot still written, mx.available is false, other sections populate", async () => {
    const { snapshot, exitCode, stderr } = await runCollect();
    expect(exitCode, `stderr:\n${stderr}`).toBe(0);
    expect(snapshot.mx.available).toBe(false);
    expect(snapshot.mx.error).toBeTruthy();
    // open_items and docs still populate independently of mx being down.
    expect(snapshot.open_items.repos.length).toBe(1);
    expect(snapshot.docs).toBeTruthy();
  });

  test("one registry repo errors, others still populate", async () => {
    const { snapshot } = await runCollect();
    expect(snapshot.open_items.repos.some((r) => r.code === "normal")).toBe(true);
    expect(snapshot.open_items.errors.some((e) => e.repo === "broken")).toBe(true);
    // The archive-path and beads-less entries are excluded/skipped, not errors.
    expect(snapshot.open_items.errors.length).toBe(1);
  });

  test("missing docs state files: docs section reports available:false / stale, not a throw", async () => {
    const { snapshot, exitCode } = await runCollect();
    expect(exitCode).toBe(0);
    expect(snapshot.docs.hygiene.available).toBe(false);
    expect(snapshot.docs.hygiene.stale).toBe(true);
    expect(snapshot.docs.sweep.available).toBe(false);
    expect(snapshot.docs.sweep.stale).toBe(true);
  });

  test("snapshot schema: schemaVersion and all top-level keys present", async () => {
    const { snapshot } = await runCollect();
    expect(snapshot.schemaVersion).toBe(1);
    expect(typeof snapshot.generated_at).toBe("string");
    expect(snapshot.mx).toBeTruthy();
    expect(snapshot.meetings).toBeTruthy();
    expect(snapshot.open_items).toBeTruthy();
    expect(snapshot.docs).toBeTruthy();
  });

  test("no .tmp file survives after a normal run", async () => {
    await runCollect();
    expect(existsSync(join(stateDir, "latest.json.tmp"))).toBe(false);
    const files = await readdir(stateDir);
    expect(files.some((f) => f.endsWith(".tmp"))).toBe(false);
  });

  test("a stale leftover .tmp from a previous crash does not corrupt the next read of latest.json", async () => {
    // Simulate a crash mid-write: a prior valid latest.json, plus a stray
    // .tmp left over from an interrupted write that never got renamed.
    const latestPath = join(stateDir, "latest.json");
    const staleTmpPath = join(stateDir, "latest.json.tmp");
    await writeFile(latestPath, JSON.stringify({ schemaVersion: 1, marker: "previous-valid-snapshot" }));
    await writeFile(staleTmpPath, "{ not valid json, truncated mid-writ");

    // Reading latest.json right now (before collect() runs again) must be
    // unaffected by the stray .tmp sitting beside it.
    const preRunContent = JSON.parse(await readFile(latestPath, "utf-8"));
    expect(preRunContent.marker).toBe("previous-valid-snapshot");

    // A subsequent complete collect() run must produce a fully valid,
    // schema-correct latest.json (not corrupted by the stale .tmp), and
    // must leave no .tmp file behind afterward.
    const { snapshot, exitCode } = await runCollect();
    expect(exitCode).toBe(0);
    expect(snapshot.schemaVersion).toBe(1);
    const postRunContent = JSON.parse(await readFile(latestPath, "utf-8"));
    expect(postRunContent.schemaVersion).toBe(1);
    expect(existsSync(staleTmpPath)).toBe(false);
  });
});
