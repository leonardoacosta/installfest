/**
 * watch.ts — chokidar-based re-scan feeder (`ctx-scan-watch` task [2.1]).
 *
 * Watches every project root discovered under `--root`, debounces per
 * project, and on a settled file-change re-scans ONLY the changed project
 * (never the whole fleet) by calling the existing `buildFleet`/`auditFleet`
 * functions directly (in-process, same Bun runtime) rather than re-exec'ing
 * `ctx-scan scan`/`ctx-scan audit` as a subprocess — a subprocess round trip
 * per file-change would pay full Bun startup + module-resolution cost on
 * every debounced edit, which is exactly the "re-scan on file-change" hot
 * path this proposal exists to make cheap.
 *
 * `buildFleet`/`auditFleet` are injected as `WatchOptions.buildFleetFn`/
 * `auditFleetFn` rather than imported directly from `cli.ts` — `cli.ts`
 * already imports `startWatch` from this module to wire the `watch` command,
 * and `buildFleet` is defined there, so a direct import here would create a
 * module cycle. Dependency injection sidesteps that entirely, keeps this
 * module's core logic decoupled from CLI wiring (this codebase's established
 * split — see `audit.ts`'s header doc), and makes `startWatch` trivially
 * testable with a fake `buildFleetFn`/`auditFleetFn` (no real filesystem scan
 * needed to test the debounce/dispatch logic in isolation).
 *
 * Calling `buildFleet(projectPath)` (the changed project's own root, not the
 * original `--root`) re-discovers exactly that one project: `buildFleet`'s
 * `discoverProjects` walk treats its `root` argument as the scan boundary,
 * and a project root already carries its own `.git`/`CLAUDE.md`/`.claude`
 * markers (that is how it was discovered as a project in the first place),
 * so pointing `discoverProjects` at it directly yields a one-project Fleet
 * (plus the global layer, exactly as any other scan). No new "single-project
 * scan" code path was needed — this reuses `buildFleet` unchanged.
 *
 * Process lifecycle: this module owns zero `process.on(...)` signal
 * handling — `cli.ts`'s `runWatch` (the wiring layer) installs SIGINT/SIGTERM
 * handlers that call `WatchHandle.close()` for a clean chokidar teardown.
 * Keeping that here would make `startWatch` unsafe to call from a test
 * process (registering real process signal handlers mid-test-run). See
 * `cli.ts`'s `runWatch` doc for the full shutdown contract.
 */
import { sep } from "node:path";
import { watch as chokidarWatch, type FSWatcher } from "chokidar";
import type { AuditResult } from "./audit";
import { discoverProjects, getGlobalLayer } from "./discovery";
import { appendSnapshot, type HistorySnapshot } from "./history";
import type { Fleet } from "./model";

/** Matches `cli.ts`'s `buildFleet` signature — injected, never imported directly (see module doc). */
export type BuildFleetFn = (
  root: string,
  opts?: { allowProbeHooks?: boolean },
) => Promise<{ fleet: Fleet; unknownHooksByProject: Map<string, number> }>;

/** Matches `audit.ts`'s `auditFleet` signature — injected alongside `buildFleetFn`. */
export type AuditFleetFn = (fleet: Fleet) => AuditResult;

export interface WatchOptions {
  /** Root directory to discover project roots under (same semantics as `scan --root`). */
  root: string;
  buildFleetFn: BuildFleetFn;
  auditFleetFn: AuditFleetFn;
  /** Per-project debounce window before a settled file-change triggers a re-scan. Default 400ms. */
  debounceMs?: number;
  /** Override the history.jsonl path — tests / `--history` point this at a fixture instead of `~/.ctx-scan/history.jsonl`. */
  historyFilePath?: string;
  /** Forwarded to `buildFleetFn` — see `cli.ts`'s `scan --probe-hooks` doc for the risk/latency tradeoff this opts into. */
  allowProbeHooks?: boolean;
  /** Fired after a snapshot is successfully appended — the CLI logs from this; tests assert on it directly instead of polling the history file. */
  onSnapshot?: (snapshot: HistorySnapshot) => void;
  /** Fired when a debounced re-scan throws — a re-scan failure must never crash the watcher process. */
  onError?: (err: unknown, projectPath: string) => void;
}

export interface WatchHandle {
  /** Absolute project roots discovered at watch-start (fixed for the lifetime of this handle — matches `scan`'s one-shot discovery model; a project added after `watch` starts requires a restart). */
  projectPaths: string[];
  /** Clears pending debounce timers and closes the chokidar watcher (releases its fs handles). Safe to call more than once. */
  close: () => Promise<void>;
}

const DEFAULT_DEBOUNCE_MS = 400;

/** Directory segments never worth watching — mirrors `discovery.ts`'s own exclusion set (kept independent rather than imported: discovery's set is a scan-time concern over directory *names*, this is a watch-time concern over full changed *paths*, and the two lists drifting independently is an acceptable, explicit choice over threading a shared constant through an unrelated module boundary). */
function isIgnoredSegment(name: string): boolean {
  if (name === "node_modules" || name === ".git" || name === "dist" || name === "build" || name === ".worktrees") {
    return true;
  }
  if (name.startsWith("archive") || name.endsWith("-archive")) return true;
  return false;
}

function isIgnoredPath(path: string): boolean {
  return path.split(sep).some(isIgnoredSegment);
}

/** True when `changedPath` is `projectPath` itself or lives beneath it. */
function isWithinProject(changedPath: string, projectPath: string): boolean {
  return changedPath === projectPath || changedPath.startsWith(projectPath + sep);
}

/**
 * Start watching every project discovered under `opts.root`. Debounces
 * per-project (a burst of edits within the debounce window collapses to one
 * re-scan), and on a settled change calls `buildFleetFn`/`auditFleetFn`
 * scoped to just the changed project, then appends the resulting snapshot
 * via `history.ts`'s `appendSnapshot`.
 */
export function startWatch(opts: WatchOptions): WatchHandle {
  const debounceMs = opts.debounceMs ?? DEFAULT_DEBOUNCE_MS;
  const global = getGlobalLayer();
  const discovered = discoverProjects(opts.root, { globalPath: global.path });
  // Longest-path-first so a nested project root (rare, but possible under a
  // broad `--root`) matches before its containing parent when resolving a
  // changed file to its owning project.
  const projectPaths = discovered.map((p) => p.path).sort((a, b) => b.length - a.length);

  const timers = new Map<string, ReturnType<typeof setTimeout>>();

  function projectForPath(changedPath: string): string | null {
    for (const p of projectPaths) {
      if (isWithinProject(changedPath, p)) return p;
    }
    return null;
  }

  async function rescan(projectPath: string): Promise<void> {
    try {
      const { fleet } = await opts.buildFleetFn(projectPath, { allowProbeHooks: opts.allowProbeHooks ?? false });
      const auditOutput = opts.auditFleetFn(fleet);
      const snapshot: HistorySnapshot = {
        timestamp: new Date().toISOString(),
        project: projectPath,
        scanOutput: fleet,
        auditOutput,
      };
      appendSnapshot(snapshot, { filePath: opts.historyFilePath });
      opts.onSnapshot?.(snapshot);
    } catch (err) {
      opts.onError?.(err, projectPath);
    }
  }

  function scheduleRescan(projectPath: string): void {
    const existing = timers.get(projectPath);
    if (existing) clearTimeout(existing);
    const timer = setTimeout(() => {
      timers.delete(projectPath);
      void rescan(projectPath);
    }, debounceMs);
    timers.set(projectPath, timer);
  }

  // `ignoreInitial: true` — a fresh watch start must never re-scan every
  // discovered project just because chokidar's initial directory walk emits
  // synthetic `add` events for every already-existing file; only REAL
  // changes after watch-start should trigger a re-scan.
  const watcher: FSWatcher = chokidarWatch(projectPaths, {
    ignoreInitial: true,
    ignored: (path: string) => isIgnoredPath(path),
    persistent: true,
  });

  watcher.on("all", (_event, changedPath) => {
    if (!changedPath) return;
    const projectPath = projectForPath(changedPath);
    if (projectPath) scheduleRescan(projectPath);
  });

  return {
    projectPaths,
    async close() {
      for (const timer of timers.values()) clearTimeout(timer);
      timers.clear();
      await watcher.close();
    },
  };
}
