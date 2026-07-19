/**
 * history.ts — append-only snapshot history (`ctx-scan-watch` tasks [1.1]/[2.2]).
 *
 * A snapshot is one timestamped `ctx-scan scan` + `ctx-scan audit` output for
 * a single project, appended as one JSON line to `~/.ctx-scan/history.jsonl`
 * by `watch.ts` on every debounced re-scan. Append-only by design (proposal's
 * own Risk table: unbounded growth is a known, accepted tradeoff — rotation
 * is an explicit future option, not built here). This module owns the record
 * shape + the read/append primitives only; `watch.ts` decides WHEN to append,
 * `diff.ts` decides how two snapshots are compared, and `render/level0-fleet.ts`
 * reads snapshots to build the fleet-leaderboard sparkline — none of those
 * concerns live here, matching this codebase's established
 * logic-module/wiring-module split (see `audit.ts`'s own header doc).
 */
import { appendFileSync, existsSync, mkdirSync, readFileSync } from "node:fs";
import { homedir } from "node:os";
import { dirname, join } from "node:path";
import type { AuditResult } from "./audit";
import type { Fleet } from "./model";

/**
 * One timestamped re-scan snapshot for a single project — one JSON line in
 * `~/.ctx-scan/history.jsonl`. `scanOutput`/`auditOutput` are scoped to just
 * that project (plus the shared global layer `ctx-scan scan`/`audit` always
 * include) — `watch.ts` builds them by pointing `buildFleet` at the changed
 * project's own root, not the full fleet `--root`, so a snapshot never grows
 * with unrelated sibling projects.
 */
export interface HistorySnapshot {
  /** ISO-8601 timestamp of when this snapshot was captured. */
  timestamp: string;
  /** Absolute project-root path this snapshot was captured for (the canonical per-project key). */
  project: string;
  /** The full `ctx-scan scan` Fleet document, scoped to this project + the global layer. */
  scanOutput: Fleet;
  /** The full `ctx-scan audit` §E-R1 rows for this project's scan. */
  auditOutput: AuditResult;
}

/** Default location: `~/.ctx-scan/history.jsonl`. Callers may override via `{filePath}` (tests, `--history`). */
export function defaultHistoryFilePath(homeDir: string = homedir()): string {
  return join(homeDir, ".ctx-scan", "history.jsonl");
}

/** Append one snapshot as a single JSON line — creates the parent directory (and the file, on first write) as needed. */
export function appendSnapshot(snapshot: HistorySnapshot, opts: { filePath?: string } = {}): void {
  const filePath = opts.filePath ?? defaultHistoryFilePath();
  mkdirSync(dirname(filePath), { recursive: true });
  appendFileSync(filePath, `${JSON.stringify(snapshot)}\n`, "utf8");
}

/**
 * Read every snapshot from `filePath` (default: the real history file), in
 * append (chronological, oldest-first) order. Returns `[]` when the file
 * does not exist yet — a fresh install with no watch history is a normal
 * state, never an error. A malformed line (partial write, hand-edited
 * fixture typo) is skipped rather than aborting the whole read, matching
 * this codebase's established graceful-degradation convention (dangling
 * imports, unreadable dirs, and malformed settings JSON all skip rather than
 * throw — see `pipeline.ts`/`settings-resolver.ts`).
 */
export function readSnapshots(opts: { filePath?: string } = {}): HistorySnapshot[] {
  const filePath = opts.filePath ?? defaultHistoryFilePath();
  if (!existsSync(filePath)) return [];
  let content: string;
  try {
    content = readFileSync(filePath, "utf8");
  } catch {
    return [];
  }
  const out: HistorySnapshot[] = [];
  for (const line of content.split("\n")) {
    const trimmed = line.trim();
    if (!trimmed) continue;
    try {
      out.push(JSON.parse(trimmed) as HistorySnapshot);
    } catch {
      continue; // partial/corrupt line — skip, never abort the whole read.
    }
  }
  return out;
}

/** Snapshots for one project (matched by exact `project` path), in chronological order. */
export function snapshotsForProject(project: string, opts: { filePath?: string } = {}): HistorySnapshot[] {
  return readSnapshots(opts).filter((s) => s.project === project);
}
