/**
 * build.ts — shared fixture builders for the `ctx-scan-watch` E2E batch
 * ([4.1]-[4.5]).
 *
 * Reuses `test/fixtures/render/build.ts`'s `makeFleet`/`project`/`surface`/
 * `makeNode` (extend-before-create: a `HistorySnapshot.scanOutput` is a plain
 * `Fleet` document, so it needs no bespoke builder) and `test/helpers/tree.ts`'s
 * `tmpRoot`/`dir`/`file` for on-disk fixture projects. Adds the two things
 * those don't cover: a minimal-but-valid `AuditRow`/`HistorySnapshot` builder
 * (`history.ts`'s append-only record shape) and a real subprocess driver for
 * `ctx-scan watch` (`spawnWatch`) — the watcher is a long-running foreground
 * process by design (`cli.ts`'s `runWatch` doc), so exercising it for real
 * means spawning it, waiting for genuine readiness/output, and always
 * terminating it via SIGTERM, never leaving an orphan process behind.
 */
import { join } from "node:path";
import type { Subprocess } from "bun";
import type { AuditResult, AuditRow } from "../../../src/audit";
import type { BandVerdict } from "../../../src/model";
import type { HistorySnapshot } from "../../../src/history";
import { appendSnapshot, readSnapshots } from "../../../src/history";
import { makeFleet, project, surface, makeNode } from "../render/build";
import { dir, file } from "../../helpers/tree";

/** Absolute path to this app's `cli.ts` entrypoint — resolved from this file's own location, independent of test-runner cwd. */
export const CLI_PATH = join(import.meta.dir, "..", "..", "..", "src", "cli.ts");
export const APP_DIR = join(import.meta.dir, "..", "..", "..");

/**
 * A minimal-but-schema-complete `AuditRow` for one rubric row's band, with
 * every other field defaulted to values that satisfy `AuditRow`'s shape
 * without asserting anything about them — `diff.ts`/`level0-fleet.ts` only
 * ever read `.id`/`.band` off these, but the fixture still needs to be a
 * genuine `AuditRow`, not a partial stand-in.
 */
export function makeAuditRow(id: string, band: BandVerdict, overrides: Partial<AuditRow> = {}): AuditRow {
  return {
    id,
    surface: "fixture",
    measured: 100,
    budget: 200,
    greenMax: 100,
    amberMax: 200,
    band,
    source: "H",
    computable: true,
    ...overrides,
  };
}

/**
 * A `HistorySnapshot` for `project`/`timestamp` whose `auditOutput.rows` are
 * built directly from `bands` (rule id -> band verdict) — the only part of a
 * snapshot `diff.ts`/the fleet sparkline actually read. `scanOutput` is a
 * trivial one-project `Fleet` (via `render/build.ts`'s `makeFleet`) purely so
 * the snapshot is a genuine, schema-complete `HistorySnapshot`, matching
 * `history.ts`'s own field contract.
 */
export function makeSnapshot(
  projectPath: string,
  timestamp: string,
  bands: Record<string, BandVerdict>,
): HistorySnapshot {
  const rows: AuditRow[] = Object.entries(bands).map(([id, band]) => makeAuditRow(id, band));
  const node = makeNode({ path: `${projectPath}/CLAUDE.md`, cls: "claude-md-chain", raw_chars: 100, est_tokens: 25 });
  const scanOutput = makeFleet([], [project("fixture-project", projectPath, [surface("claude-md-chain", [node])])]);
  return {
    timestamp,
    project: projectPath,
    scanOutput,
    auditOutput: { rows, error: null } satisfies AuditResult,
  };
}

/** Write `snapshots` to `filePath` in order, one `appendSnapshot` call each — builds a real, on-disk `history.jsonl`. */
export function writeSnapshots(filePath: string, snapshots: HistorySnapshot[]): void {
  for (const s of snapshots) appendSnapshot(s, { filePath });
}

/** Build a minimal, real, discoverable fixture project on disk: `<root>/<name>/CLAUDE.md` (a genuine project-root marker per `discovery.ts`'s `MARKERS`). */
export function makeFixtureProject(root: string, name: string, claudeMdContent = `# ${name}\n\nA fixture project for ctx-scan-watch E2E tests.\n`): string {
  dir(root, `${name}/.claude`);
  file(root, `${name}/CLAUDE.md`, claudeMdContent);
  return join(root, name);
}

/**
 * Read from `stream` (a still-open `ReadableStream<Uint8Array>` on a
 * long-running subprocess) until `pattern` matches the accumulated text, or
 * `timeoutMs` elapses. Keeps exactly one `reader.read()` call outstanding at
 * a time (reused across polling ticks) — issuing a second concurrent
 * `read()` on the same reader before the first resolves is invalid and would
 * make this flaky under real chokidar/Bun startup jitter.
 */
async function waitForStreamPattern(
  stream: ReadableStream<Uint8Array>,
  pattern: RegExp,
  timeoutMs: number,
): Promise<string> {
  const reader = stream.getReader();
  const decoder = new TextDecoder();
  let buf = "";
  const deadline = Date.now() + timeoutMs;
  let pending: ReturnType<typeof reader.read> | null = null;
  try {
    for (;;) {
      const remaining = deadline - Date.now();
      if (remaining <= 0) {
        throw new Error(`timed out after ${timeoutMs}ms waiting for pattern ${pattern}; stdout so far: ${JSON.stringify(buf)}`);
      }
      if (!pending) pending = reader.read();
      const tick = new Promise<"tick">((resolve) => setTimeout(() => resolve("tick"), Math.min(remaining, 100)));
      const result = await Promise.race([pending, tick]);
      if (result === "tick") continue; // `pending` stays outstanding — re-check the deadline, don't double-read
      pending = null;
      const { value, done } = result;
      if (value) buf += decoder.decode(value, { stream: true });
      if (pattern.test(buf)) return buf;
      if (done) {
        throw new Error(`stdout closed before pattern ${pattern} matched; stdout so far: ${JSON.stringify(buf)}`);
      }
    }
  } finally {
    reader.releaseLock();
  }
}

export interface SpawnedWatch {
  proc: Subprocess<"ignore", "pipe", "pipe">;
  historyPath: string;
  root: string;
  /** Poll `historyPath` until it holds at least `expected` snapshots, or throw after `timeoutMs`. */
  waitForSnapshotCount(expected: number, timeoutMs: number): Promise<HistorySnapshot[]>;
  /** SIGTERM the watcher and await its own graceful `handle.close()` exit (see `cli.ts`'s `runWatch` shutdown contract). Safe to call more than once. */
  stop(): Promise<void>;
}

/**
 * Spawn a REAL `ctx-scan watch` subprocess (`bun <cli.ts> watch --root ...`)
 * against `root`, wait for its own startup log line (genuine readiness, not
 * a guessed sleep), and return a handle for driving/tearing it down. Always
 * pair with `stop()` in a `finally`/`afterEach` — an unterminated watcher is
 * a long-running chokidar process that outlives the test run.
 */
export async function spawnWatch(root: string, opts: { debounceMs?: number } = {}): Promise<SpawnedWatch> {
  const historyPath = join(root, "history.jsonl");
  const debounceMs = opts.debounceMs ?? 150;

  const proc = Bun.spawn(
    ["bun", CLI_PATH, "watch", "--root", root, "--history", historyPath, "--debounce-ms", String(debounceMs)],
    { cwd: APP_DIR, stdin: "ignore", stdout: "pipe", stderr: "pipe" },
  );

  await waitForStreamPattern(proc.stdout, /\[ctx-scan watch] watching .* discovered\)/, 15_000);
  // chokidar's own fs.watch/inotify registration completes just after the
  // handle is constructed and the readiness line is printed (`watch.ts`'s
  // `startWatch` returns synchronously, then `cli.ts` logs immediately) — a
  // small buffer here absorbs that last sliver of real OS-level setup so the
  // very first fixture edit isn't racing watcher registration.
  await Bun.sleep(300);

  let stopped = false;
  return {
    proc,
    historyPath,
    root,
    async waitForSnapshotCount(expected, timeoutMs) {
      const deadline = Date.now() + timeoutMs;
      for (;;) {
        const snaps = readSnapshots({ filePath: historyPath });
        if (snaps.length >= expected) return snaps;
        if (Date.now() >= deadline) {
          throw new Error(`timed out after ${timeoutMs}ms waiting for >=${expected} snapshot(s); have ${snaps.length}`);
        }
        await Bun.sleep(50);
      }
    },
    async stop() {
      if (stopped) return;
      stopped = true;
      if (proc.exitCode === null) proc.kill("SIGTERM");
      const exited = await Promise.race([
        proc.exited.then(() => "exited" as const),
        Bun.sleep(5_000).then(() => "timeout" as const),
      ]);
      if (exited === "timeout" && proc.exitCode === null) {
        proc.kill("SIGKILL");
        await proc.exited;
      }
    },
  };
}
