/**
 * widgetOpen.ts — delivers the daily brief as a persistent widget pane.
 *
 * Zellij (spec.md's primary delivery mechanism): 0.44.3, confirmed
 * installed on this machine (`zellij --version`). `zellij list-sessions`
 * (`--help` confirmed live) exposes ONLY a `[Created X ago]` relative
 * creation timestamp per session — no last-activity/last-focus field
 * exists on any zellij CLI surface as of 0.44.3 (checked `--help` on
 * `list-sessions`, `run`, and the top-level command; also inspected two
 * concurrently running sessions live, both only ever showed their creation
 * age). "Most recently CREATED" is therefore the closest available proxy
 * for spec.md's "most-recently-active" session — documented here per this
 * task's explicit instruction to record that choice.
 *
 * The actual injection command — `zellij --session <name> run --floating
 * --pinned true -- <cmd>` — was verified live 2026-07-18 against a real
 * disposable session (hosted in a detached tmux pane so it had a real pty
 * without touching any of Leo's own sessions): it exits 0, prints the
 * created pane id (`terminal_1`), and the command actually ran inside that
 * session's newly-created floating pinned pane.
 *
 * Falls back to tmux (mux-transition period) when no zellij session exists
 * but a tmux server does, per spec.md's tmux-fallback scenario: inserts a
 * new window before the lowest-index window of the most-recently-active
 * ATTACHED tmux session. `^` is tmux's `{start}` token (the lowest-numbered
 * window — confirmed via `man tmux`'s target-window section and a live
 * `tmux new-window -b -d -t "<session>:^"` run, which inserted a window at
 * the target session's index 1, pushing the pre-existing window 1 to
 * index 2). `-d` keeps whichever client is currently attached to that
 * session on its current window (spec.md "without stealing focus").
 *
 * With neither mux present, injection is skipped silently. `openWidget()`
 * NEVER throws and always resolves — every subprocess call is wrapped so a
 * zellij/tmux failure degrades to "did nothing" rather than propagating
 * (spec.md "The injector always exits 0").
 */

import { join } from "node:path";

const INDEX_TSX_PATH = join(import.meta.dir, "index.tsx");

/**
 * The widget pane's command. Uses `process.execPath` (the real Bun binary
 * running this process) + this package's own entrypoint's absolute path,
 * rather than the spec's illustrative `daily-brief view` — there is no
 * global `daily-brief` binary on PATH anywhere in this repo's tooling
 * (package.json's `bin` field is never `npm link`ed or installed; the
 * already-shipped `daily-brief.service`/`.plist` units both invoke
 * `bun run <absolute path to index.tsx> ...` directly, the same pattern
 * this mirrors) — so this is the invocation that actually works regardless
 * of the spawning shell's PATH.
 */
function widgetCommand(): string[] {
  return [process.execPath, "run", INDEX_TSX_PATH, "view"];
}

/** Parses a zellij `[Created X ago]` duration string (e.g. `"17s"`,
 * `"1m 12s"`, `"2h 3m"`, `"3d"`) into seconds. Any unrecognized/unparseable
 * format returns `Infinity` so it sorts last — safer than accidentally
 * treating a malformed string as "just created" and picking the wrong
 * session. */
function parseCreatedAgoSeconds(text: string): number {
  const unitSeconds: Record<string, number> = { d: 86_400, h: 3600, m: 60, s: 1 };
  const matches = [...text.matchAll(/(\d+)\s*(d|h|m|s)/g)];
  if (matches.length === 0) return Infinity;
  let total = 0;
  for (const match of matches) {
    const amount = match[1];
    const unit = match[2];
    if (!amount || !unit) continue;
    total += Number(amount) * (unitSeconds[unit] ?? 0);
  }
  return total;
}

interface ZellijSession {
  name: string;
  createdAgoSeconds: number;
}

function parseZellijSessions(stdout: string): ZellijSession[] {
  const sessions: ZellijSession[] = [];
  for (const rawLine of stdout.split("\n")) {
    const line = rawLine.trim();
    if (!line) continue;
    const match = /^(\S+)\s+\[Created\s+(.+?)\s+ago\]/.exec(line);
    if (!match) continue;
    const name = match[1];
    const durationText = match[2];
    if (!name || !durationText) continue;
    sessions.push({ name, createdAgoSeconds: parseCreatedAgoSeconds(durationText) });
  }
  return sessions;
}

async function mostRecentZellijSession(): Promise<string | null> {
  try {
    const proc = Bun.spawn(["zellij", "list-sessions", "-n"], { stdout: "pipe", stderr: "pipe" });
    const [stdout, exitCode] = await Promise.all([new Response(proc.stdout).text(), proc.exited]);
    // Real zellij exits 1 with "No active zellij sessions found." when
    // none exist (confirmed live) — treat any non-zero exit as "no usable
    // session" rather than trying to disambiguate further.
    if (exitCode !== 0) return null;
    const sessions = parseZellijSessions(stdout);
    if (sessions.length === 0) return null;
    sessions.sort((a, b) => a.createdAgoSeconds - b.createdAgoSeconds);
    return sessions[0]?.name ?? null;
  } catch {
    return null; // zellij binary missing, spawn failure, etc.
  }
}

async function tryZellij(): Promise<boolean> {
  const session = await mostRecentZellijSession();
  if (!session) return false;
  try {
    const proc = Bun.spawn(
      ["zellij", "--session", session, "run", "--floating", "--pinned", "true", "--", ...widgetCommand()],
      { stdout: "ignore", stderr: "ignore", stdin: "ignore" },
    );
    const exitCode = await proc.exited;
    return exitCode === 0;
  } catch {
    return false;
  }
}

interface TmuxSession {
  name: string;
  attached: boolean;
  lastAttached: number;
}

function parseTmuxSessions(stdout: string): TmuxSession[] {
  const sessions: TmuxSession[] = [];
  for (const rawLine of stdout.split("\n")) {
    const line = rawLine.trim();
    if (!line) continue;
    const [name, attachedRaw, lastAttachedRaw] = line.split("\t");
    if (!name) continue;
    sessions.push({
      name,
      attached: attachedRaw === "1",
      lastAttached: lastAttachedRaw ? Number(lastAttachedRaw) || 0 : 0,
    });
  }
  return sessions;
}

async function mostRecentAttachedTmuxSession(): Promise<string | null> {
  try {
    const proc = Bun.spawn(
      ["tmux", "list-sessions", "-F", "#{session_name}\t#{session_attached}\t#{session_last_attached}"],
      { stdout: "pipe", stderr: "pipe" },
    );
    const [stdout, exitCode] = await Promise.all([new Response(proc.stdout).text(), proc.exited]);
    if (exitCode !== 0) return null; // no tmux server running
    const attached = parseTmuxSessions(stdout).filter((session) => session.attached);
    if (attached.length === 0) return null;
    attached.sort((a, b) => b.lastAttached - a.lastAttached);
    return attached[0]?.name ?? null;
  } catch {
    return null; // tmux binary missing, spawn failure, etc.
  }
}

async function tryTmux(): Promise<boolean> {
  const session = await mostRecentAttachedTmuxSession();
  if (!session) return false;
  try {
    const proc = Bun.spawn(
      ["tmux", "new-window", "-b", "-d", "-t", `${session}:^`, "-n", "brief", ...widgetCommand()],
      { stdout: "ignore", stderr: "ignore", stdin: "ignore" },
    );
    const exitCode = await proc.exited;
    return exitCode === 0;
  } catch {
    return false;
  }
}

/**
 * Delivers the brief as a persistent widget pane in the most-recently
 * (created) Zellij session, falling back to a tmux window, or skipping
 * silently if neither mux is running. Always resolves — never throws — so
 * the caller (`index.tsx`'s `collect --open-widget` path) always proceeds
 * to exit 0 regardless of what happened here.
 */
export async function openWidget(): Promise<void> {
  try {
    if (await tryZellij()) return;
    await tryTmux();
  } catch {
    // Belt-and-suspenders: tryZellij/tryTmux already swallow their own
    // failures internally, but this function must NEVER let anything
    // propagate to the caller.
  }
}
