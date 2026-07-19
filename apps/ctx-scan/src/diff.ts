/**
 * diff.ts — snapshot band-transition comparator (`ctx-scan-watch` task
 * [2.3]). Compares every rubric row's `band` between two `HistorySnapshot`s
 * (`history.ts`) and reports exactly which rows transitioned — the "what
 * regressed this week" answer the proposal names as the real deliverable.
 * Pure comparison logic only; `cli.ts` owns the `ctx-scan diff <a> <b>`
 * wiring (argv parsing, stdout formatting), matching this codebase's
 * established wiring-vs-logic split.
 */
import type { BandVerdict } from "./model";
import type { HistorySnapshot } from "./history";
import { readSnapshots } from "./history";

/** A single rubric row whose `band` differs between two snapshots. `"UNKNOWN"` on one side means the row was absent from that snapshot's audit output entirely. */
export interface BandTransition {
  rule: string;
  from: BandVerdict | "UNKNOWN";
  to: BandVerdict | "UNKNOWN";
}

export interface DiffResult {
  transitions: BandTransition[];
  /** Non-null only when snapshot resolution itself failed (e.g. an unknown selector) — never thrown, matching `audit.ts`'s exit-0-always contract. */
  error: string | null;
}

/**
 * Resolve `selector` against `snapshots` (in the array's own order —
 * `readSnapshots` returns chronological/append order):
 *   1. An exact `timestamp` match wins first (the precise, unambiguous case).
 *   2. Else, a base-10 integer selects an array index — negative indexes
 *      count from the end, Python-slice-style (`-1` is "most recent").
 *   3. Else `null` (selector matches nothing).
 */
export function resolveSnapshot(snapshots: HistorySnapshot[], selector: string): HistorySnapshot | null {
  const byTimestamp = snapshots.find((s) => s.timestamp === selector);
  if (byTimestamp) return byTimestamp;
  if (/^-?\d+$/.test(selector)) {
    const idx = Number.parseInt(selector, 10);
    const resolved = idx < 0 ? snapshots.length + idx : idx;
    return snapshots[resolved] ?? null;
  }
  return null;
}

/**
 * Compare every rubric row's `band` between two snapshots' `auditOutput.rows`
 * (keyed by Table A row `id`). A row present in only one snapshot's audit
 * output diffs against `"UNKNOWN"` on the missing side — this can only
 * happen if the rubric's own row set changed between the two scan runs
 * (e.g. a Table A row added/removed between ctx-scan versions), never as a
 * normal band fluctuation. Returns `[]` when nothing changed, sorted by rule
 * id for deterministic output.
 */
export function diffSnapshots(a: HistorySnapshot, b: HistorySnapshot): BandTransition[] {
  const aBands = new Map(a.auditOutput.rows.map((r) => [r.id, r.band] as const));
  const bBands = new Map(b.auditOutput.rows.map((r) => [r.id, r.band] as const));
  const ids = new Set<string>([...aBands.keys(), ...bBands.keys()]);
  const transitions: BandTransition[] = [];
  for (const id of Array.from(ids).sort()) {
    const from = aBands.get(id) ?? "UNKNOWN";
    const to = bBands.get(id) ?? "UNKNOWN";
    if (from !== to) transitions.push({ rule: id, from, to });
  }
  return transitions;
}

/**
 * Load two named snapshots from `filePath` (default: the real history file)
 * and diff them. Never throws — a resolution or read failure surfaces as
 * `{transitions: [], error: "..."}`, matching `audit.ts`'s own
 * exit-0-always contract so `cli.ts`'s `diff` command never crashes on a
 * bad selector or a missing/corrupt history file.
 */
export function diffByName(a: string, b: string, opts: { filePath?: string } = {}): DiffResult {
  try {
    const snapshots = readSnapshots(opts);
    const snapA = resolveSnapshot(snapshots, a);
    const snapB = resolveSnapshot(snapshots, b);
    if (!snapA) return { transitions: [], error: `snapshot not found: "${a}"` };
    if (!snapB) return { transitions: [], error: `snapshot not found: "${b}"` };
    return { transitions: diffSnapshots(snapA, snapB), error: null };
  } catch (err) {
    return { transitions: [], error: err instanceof Error ? err.message : String(err) };
  }
}

/** `A4: GREEN → RED` — the CLI's human-readable per-transition line. */
export function formatTransition(t: BandTransition): string {
  return `${t.rule}: ${t.from} → ${t.to}`;
}
