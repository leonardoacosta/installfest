/**
 * docsState.test.ts — `collectDocsState()` fail-open, staleness, and entry
 * shape suite (harden-daily-brief-titles-and-tests task 2.2).
 *
 * Runs the REAL `collectDocsState()` export as a fresh subprocess (see
 * run-open-items.ts's header comment for why) against scratch fixture
 * files built via `DAILY_BRIEF_HYGIENE_RESULTS_PATH` /
 * `DAILY_BRIEF_SWEEP_LAST_RUN_PATH` env overrides — never Leo's real
 * `~/.local/state/docs-hygiene-daily/results.jsonl` or
 * `~/.claude/state/docs-sweep-last-run.json`.
 */
import { afterEach, describe, expect, test } from "bun:test";
import { mkdtemp, rm, utimes, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { runBunScript } from "./helpers/runBun";
import type { DocsState } from "../src/sources/docsState";

let fixtureDir: string | undefined;

afterEach(async () => {
  if (fixtureDir) {
    await rm(fixtureDir, { recursive: true, force: true });
    fixtureDir = undefined;
  }
});

async function runDocsState(env: Record<string, string>): Promise<DocsState> {
  const { stdout, stderr, exitCode } = await runBunScript("run-docs-state.ts", [], env);
  expect(exitCode, `stderr:\n${stderr}`).toBe(0);
  return JSON.parse(stdout) as DocsState;
}

describe("collectDocsState", () => {
  test("missing hygiene + sweep files: both sections fail open (available:false, stale:true), no throw", async () => {
    fixtureDir = await mkdtemp(join(tmpdir(), "daily-brief-docs-state-"));
    const state = await runDocsState({
      DAILY_BRIEF_HYGIENE_RESULTS_PATH: join(fixtureDir, "nonexistent-results.jsonl"),
      DAILY_BRIEF_SWEEP_LAST_RUN_PATH: join(fixtureDir, "nonexistent-last-run.json"),
    });

    expect(state.hygiene.available).toBe(false);
    expect(state.hygiene.stale).toBe(true);
    expect(state.hygiene.entries).toEqual([]);
    expect(state.sweep.available).toBe(false);
    expect(state.sweep.stale).toBe(true);
  });

  test("fresh hygiene results.jsonl parses entries and is not stale", async () => {
    fixtureDir = await mkdtemp(join(tmpdir(), "daily-brief-docs-state-"));
    const hygienePath = join(fixtureDir, "results.jsonl");
    await writeFile(
      hygienePath,
      [
        JSON.stringify({ repo: "cc", status: "error", detail: "broken ref" }),
        JSON.stringify({ repo: "if", status: "ok" }),
      ].join("\n") + "\n",
    );

    const state = await runDocsState({
      DAILY_BRIEF_HYGIENE_RESULTS_PATH: hygienePath,
      DAILY_BRIEF_SWEEP_LAST_RUN_PATH: join(fixtureDir, "nonexistent-last-run.json"),
    });

    expect(state.hygiene.available).toBe(true);
    expect(state.hygiene.stale).toBe(false);
    expect(state.hygiene.entries).toEqual([
      { repo: "cc", status: "error", detail: "broken ref" },
      { repo: "if", status: "ok" },
    ]);
  });

  test("hygiene results.jsonl older than the 48h stale threshold is reported stale even though it parses fine", async () => {
    fixtureDir = await mkdtemp(join(tmpdir(), "daily-brief-docs-state-"));
    const hygienePath = join(fixtureDir, "results.jsonl");
    await writeFile(hygienePath, JSON.stringify({ repo: "cc", status: "ok" }) + "\n");
    const oldTime = new Date(Date.now() - 72 * 60 * 60 * 1000);
    await utimes(hygienePath, oldTime, oldTime);

    const state = await runDocsState({
      DAILY_BRIEF_HYGIENE_RESULTS_PATH: hygienePath,
      DAILY_BRIEF_SWEEP_LAST_RUN_PATH: join(fixtureDir, "nonexistent-last-run.json"),
    });

    expect(state.hygiene.available).toBe(true);
    expect(state.hygiene.stale).toBe(true);
  });

  test("sweep last-run.json filters out verified findings and keeps flagged ones", async () => {
    fixtureDir = await mkdtemp(join(tmpdir(), "daily-brief-docs-state-"));
    const sweepPath = join(fixtureDir, "last-run.json");
    await writeFile(
      sweepPath,
      JSON.stringify({
        generated_at: new Date().toISOString(),
        summary: { verified: 1, flagged: 1 },
        docs: [
          { path: "docs/a.md", verdict: "verified", findings: [] },
          { path: "docs/b.md", verdict: "dangling-ref", findings: ["broken link"] },
        ],
      }),
    );

    const state = await runDocsState({
      DAILY_BRIEF_HYGIENE_RESULTS_PATH: join(fixtureDir, "nonexistent-results.jsonl"),
      DAILY_BRIEF_SWEEP_LAST_RUN_PATH: sweepPath,
    });

    expect(state.sweep.available).toBe(true);
    expect(state.sweep.stale).toBe(false);
    expect(state.sweep.flagged).toEqual([
      { path: "docs/b.md", verdict: "dangling-ref", findings: ["broken link"] },
    ]);
  });

  test("sweep last-run.json with a stale generated_at is reported stale", async () => {
    fixtureDir = await mkdtemp(join(tmpdir(), "daily-brief-docs-state-"));
    const sweepPath = join(fixtureDir, "last-run.json");
    const oldIso = new Date(Date.now() - 72 * 60 * 60 * 1000).toISOString();
    await writeFile(sweepPath, JSON.stringify({ generated_at: oldIso, docs: [] }));

    const state = await runDocsState({
      DAILY_BRIEF_HYGIENE_RESULTS_PATH: join(fixtureDir, "nonexistent-results.jsonl"),
      DAILY_BRIEF_SWEEP_LAST_RUN_PATH: sweepPath,
    });

    expect(state.sweep.available).toBe(true);
    expect(state.sweep.stale).toBe(true);
  });
});
