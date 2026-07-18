/**
 * docsState.ts — reads scheduled doc-cleanup state, fail-open.
 *
 * Two independent files:
 *   - `~/.local/state/docs-hygiene-daily/results.jsonl` (the 04:15
 *     docs-hygiene-daily timer output) — one JSON object per line, no
 *     embedded timestamp field on any line (inspected live, confirmed
 *     `{"repo","status","detail"}` only) — staleness is judged off the
 *     file's own mtime.
 *   - `~/.claude/state/docs-sweep-last-run.json` (cc docs-sweep state) —
 *     carries its own `generated_at` ISO field, which is the more reliable
 *     staleness signal here (inspected live) since it is the actual
 *     generation instant rather than a filesystem write time.
 *
 * A missing file resolves to `{ available: false, stale: true }` rather
 * than throwing (spec.md "Docs section reads the scheduled doc-cleanup
 * state").
 */

import { homedir } from "node:os";
import { join } from "node:path";

const RESULTS_JSONL_PATH = join(homedir(), ".local/state/docs-hygiene-daily/results.jsonl");
const SWEEP_LAST_RUN_PATH = join(homedir(), ".claude/state/docs-sweep-last-run.json");
const STALE_THRESHOLD_MS = 48 * 60 * 60 * 1000;

export interface HygieneEntry {
  repo: string;
  status: string;
  detail?: string;
}

export interface HygieneResult {
  available: boolean;
  stale: boolean;
  generated_at: string | null;
  entries: HygieneEntry[];
  error?: string;
}

export interface SweepFinding {
  path: string;
  verdict: string;
  findings: unknown[];
}

export interface SweepResult {
  available: boolean;
  stale: boolean;
  generated_at: string | null;
  summary?: Record<string, number>;
  flagged?: SweepFinding[];
  error?: string;
}

export interface DocsState {
  hygiene: HygieneResult;
  sweep: SweepResult;
}

function isStale(generatedAtMs: number, nowMs: number): boolean {
  return nowMs - generatedAtMs > STALE_THRESHOLD_MS;
}

async function readHygiene(nowMs: number): Promise<HygieneResult> {
  const file = Bun.file(RESULTS_JSONL_PATH);
  if (!(await file.exists())) {
    return { available: false, stale: true, generated_at: null, entries: [] };
  }
  try {
    const text = await file.text();
    const entries: HygieneEntry[] = text
      .split("\n")
      .map((line) => line.trim())
      .filter((line) => line.length > 0)
      .map((line) => JSON.parse(line) as HygieneEntry);
    const generatedAtMs = file.lastModified;
    return {
      available: true,
      stale: isStale(generatedAtMs, nowMs),
      generated_at: new Date(generatedAtMs).toISOString(),
      entries,
    };
  } catch (err) {
    return {
      available: false,
      stale: true,
      generated_at: null,
      entries: [],
      error: err instanceof Error ? err.message : String(err),
    };
  }
}

async function readSweep(nowMs: number): Promise<SweepResult> {
  const file = Bun.file(SWEEP_LAST_RUN_PATH);
  if (!(await file.exists())) {
    return { available: false, stale: true, generated_at: null };
  }
  try {
    const parsed = (await file.json()) as {
      generated_at?: string;
      summary?: Record<string, number>;
      docs?: { path: string; verdict: string; findings: unknown[] }[];
    };
    const generatedAt = parsed.generated_at ?? null;
    const generatedAtMs = generatedAt ? Date.parse(generatedAt) : file.lastModified;
    const flagged = (parsed.docs ?? [])
      .filter((d) => d.verdict !== "verified")
      .map((d) => ({ path: d.path, verdict: d.verdict, findings: d.findings }));
    return {
      available: true,
      stale: isStale(generatedAtMs, nowMs),
      generated_at: generatedAt ?? new Date(file.lastModified).toISOString(),
      summary: parsed.summary,
      flagged,
    };
  } catch (err) {
    return {
      available: false,
      stale: true,
      generated_at: null,
      error: err instanceof Error ? err.message : String(err),
    };
  }
}

export async function collectDocsState(nowMs: number = Date.now()): Promise<DocsState> {
  const [hygiene, sweep] = await Promise.all([readHygiene(nowMs), readSweep(nowMs)]);
  return { hygiene, sweep };
}
