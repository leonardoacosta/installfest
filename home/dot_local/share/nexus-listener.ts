#!/usr/bin/env bun
/**
 * nexus-listener — SSE consumer for nexus-agent.
 *
 * Replaces the `nexus-notifier.sh listen` mode, which has a well-known
 * stall on macOS-bundled bash 3.2 (process-substitution + child FD
 * inheritance race that wedges the read loop after the first dispatched
 * event). This implementation uses native fetch() streaming, so there's
 * no pipe-handling involved at all.
 *
 * Scope: ONLY the listener (SSE → banner + TTS dispatch). The legacy
 * `nexus-notifier.sh drain` worker still runs and consumes the FIFO for
 * serialized `say` playback — we just write to its FIFO instead of
 * spawning say directly, preserving the playback ordering invariant.
 *
 * Channel routing matches the bash listener exactly:
 *   tts            → banner + audio  (logged as "tts+banner: …")
 *   desktop|banner → banner only     (logged as "banner: …")
 *   slack | empty  → silent no-op
 *   *              → "unknown channel: …" log line
 *
 * Cross-platform notes:
 *   - macOS  → terminal-notifier (preferred) or osascript fallback for banner;
 *              ~/Library/Application Support/nexus/tts-queue.fifo for audio.
 *   - Linux  → notify-send for banner; FIFO path resolved from XDG_RUNTIME_DIR
 *              or falls back to direct `say`/`espeak` if no FIFO present.
 */

import { spawn } from "bun";
import { appendFile, writeFile } from "node:fs/promises";
import { existsSync, mkdirSync, statSync } from "node:fs";
import { homedir, platform } from "node:os";
import { join, dirname } from "node:path";

const HOME = homedir();
const IS_MAC = platform() === "darwin";

const LOG = join(HOME, ".local/state/nexus-listener.log");
const FIFO = IS_MAC
  ? join(HOME, "Library/Application Support/nexus/tts-queue.fifo")
  : join(HOME, ".local/state/nexus-tts-queue.fifo");

// Matches nx-send.sh's default (scripts/lib/nx-send.sh) -- correct when this
// listener runs on the same box as nexus-agent. Cross-machine deployments
// (the macOS plist) set NEXUS_URL explicitly to the AdGuard-authoritative
// leonardoacosta.dev name, so this fallback only matters for local/manual
// invocation.
const NEXUS_URL = process.env.NEXUS_URL ?? "http://localhost:7400";
const RECONNECT_MS = 5_000;
const STREAM_MAX_MS = 30 * 60 * 1_000; // mirror curl --max-time 1800
const DEDUP_WINDOW_MS = 30_000;

let TTS_ENABLED = parseBool(process.env.TTS_ENABLED, true);
let BANNER_ENABLED = parseBool(process.env.BANNER_ENABLED, true);

mkdirSync(dirname(LOG), { recursive: true });

// ── Logging ──────────────────────────────────────────────────────────
async function log(msg: string): Promise<void> {
  const line = `[${new Date().toString()}] ${msg}\n`;
  try {
    await appendFile(LOG, line);
  } catch {
    /* swallow */
  }
}

function parseBool(v: string | undefined, dflt: boolean): boolean {
  if (v === undefined) return dflt;
  const s = v.toLowerCase();
  return !(s === "0" || s === "false" || s === "off" || s === "");
}

// ── Dedup (in-memory, 30s window — matches bash listener) ────────────
let lastDedupId = "";
let lastDedupTs = 0;
function shouldSkipDup(id: string): boolean {
  if (!id) return false;
  const now = Date.now();
  if (id === lastDedupId && now - lastDedupTs < DEDUP_WINDOW_MS) return true;
  lastDedupId = id;
  lastDedupTs = now;
  return false;
}

// ── Banner dispatch ──────────────────────────────────────────────────
// Optionally takes a `cancelPid` — when the user CLICKS the banner,
// terminal-notifier runs `kill -TERM <cancelPid>` to stop the in-flight
// TTS. macOS does not surface dismiss/swipe events to userland, so only
// click-to-cancel is supported. Banner default timeout (~5s) is a
// reasonable proxy for "user moved on".
function dispatchBanner(
  title: string,
  body: string,
  cancelPid?: number,
): void {
  if (!BANNER_ENABLED) return;
  if (IS_MAC) {
    const tn = "/opt/homebrew/bin/terminal-notifier";
    if (existsSync(tn)) {
      const cmd = [tn, "-title", title, "-message", body];
      if (cancelPid && cancelPid > 0) {
        cmd.push("-execute", `/bin/kill -TERM ${cancelPid}`);
      }
      spawn({ cmd, stdout: "ignore", stderr: "ignore" });
      return;
    }
    // Fallback: osascript display notification has no -execute equivalent,
    // so banners through this path can't cancel TTS.
    const esc = (s: string) =>
      s.replace(/\\/g, "\\\\").replace(/"/g, '\\"');
    spawn({
      cmd: [
        "/usr/bin/osascript",
        "-e",
        `display notification "${esc(body)}" with title "${esc(title)}"`,
      ],
      stdout: "ignore",
      stderr: "ignore",
    });
    return;
  }
  // Linux: notify-send doesn't support callbacks portably across DEs.
  // Cancellation via banner click isn't supported here.
  spawn({
    cmd: ["notify-send", "--app-name=nexus", title, body],
    stdout: "ignore",
    stderr: "ignore",
  });
}

// ── Audio dispatch (in-process queue, single voice, cancellable) ─────
// Queue model: one `say`/`espeak` process running at a time. New event
// bodies append to the queue. `currentPid` exposes the active utterance's
// pid so the banner dispatch can pass it as a cancel target — a
// banner-click runs `kill -TERM <pid>`, immediately advancing the queue
// to the next body.
//
// We deliberately do NOT use the legacy FIFO + nexus-notifier.sh drain.
// The drain bash worker can wedge in ways that block FIFO writers, which
// would freeze the event loop on the first dispatched event.
const audioQueue: string[] = [];
let audioBusy = false;
let currentPid = 0;

function dispatchAudio(body: string): void {
  if (!TTS_ENABLED) return;
  audioQueue.push(body);
  drainAudioQueue();
}

// Synchronously start TTS for the next queued body and return its pid,
// so the banner dispatched alongside has a valid cancel target. If the
// queue is empty or already speaking, returns 0 (no pid to cancel).
function startNextAudioAndGetPid(): number {
  if (audioBusy || audioQueue.length === 0 || !TTS_ENABLED) return 0;
  audioBusy = true;
  const body = audioQueue.shift()!;
  const cmd = IS_MAC
    ? ["/usr/bin/say", body]
    : existsSync("/usr/bin/espeak")
      ? ["/usr/bin/espeak", body]
      : null;
  if (!cmd) {
    audioBusy = false;
    return 0;
  }
  const proc = spawn({ cmd, stdout: "ignore", stderr: "ignore" });
  currentPid = proc.pid;
  // 60s cap mirrors the legacy drain — a wedged audio device can't
  // stall the queue past one minute per utterance.
  const cap = setTimeout(() => proc.kill(), 60_000);
  // Once this utterance ends (naturally, via banner-click kill, or cap),
  // mark not-busy and recurse for the next queued body.
  proc.exited.finally(() => {
    clearTimeout(cap);
    currentPid = 0;
    audioBusy = false;
    drainAudioQueue();
  });
  return proc.pid;
}

async function drainAudioQueue(): Promise<void> {
  // If something's already speaking, the .exited callback will pump
  // the next body. Only start when idle.
  if (!audioBusy) startNextAudioAndGetPid();
}

// ── Event router ─────────────────────────────────────────────────────
interface Payload {
  id?: string;
  title?: string;
  body?: string;
  message?: string;
  channel?: string;
  project?: string;
  audioBase64?: string;
}

async function processEvent(p: Payload): Promise<void> {
  const id = p.id ?? "";
  const title = p.title ?? p.project ?? "Claude Code";
  const body = p.body ?? p.message ?? "";
  const channel = (p.channel ?? "").trim();

  if (!body) return;

  if (shouldSkipDup(id)) {
    await log(`dedup skipped id=${id}`);
    return;
  }

  switch (channel) {
    case "desktop":
    case "banner":
      dispatchBanner(title, body);
      await log(`banner: [${title}] ${body}`);
      break;
    case "tts": {
      // Order matters: enqueue audio first so we can synchronously start
      // it and retrieve a pid to pass to the banner. If the queue was
      // already busy with a prior utterance, cancelPid is 0 (no-op
      // -execute) — the banner still fires, but click won't cancel.
      // That's the right behaviour: this banner advertises THIS event,
      // not whatever's currently speaking from a previous one.
      audioQueue.push(body);
      const cancelPid = startNextAudioAndGetPid();
      dispatchBanner(title, body, cancelPid);
      await log(`tts+banner: [${title}] ${body}${cancelPid ? ` (cancel pid=${cancelPid})` : ""}`);
      break;
    }
    case "slack":
    case "":
      // intentionally silent — matches bash listener
      break;
    default:
      // Comma-separated combos (e.g. "desktop,tts") — match bash semantics
      if (channel.includes("tts") || channel.includes("desktop")) {
        if (channel.includes("tts")) {
          audioQueue.push(body);
          const cancelPid = startNextAudioAndGetPid();
          dispatchBanner(title, body, cancelPid);
        } else {
          dispatchBanner(title, body);
        }
        await log(`both: [${title}] ${body}`);
      } else {
        await log(`unknown channel: ${channel}`);
      }
  }
}

// ── Settings bootstrap ───────────────────────────────────────────────
async function bootstrapSettings(): Promise<void> {
  try {
    const r = await fetch(`${NEXUS_URL}/notifications/settings`, {
      signal: AbortSignal.timeout(5_000),
    });
    if (!r.ok) return;
    const s = await r.json();
    if (typeof s.ttsEnabled === "boolean") TTS_ENABLED = s.ttsEnabled;
    if (typeof s.bannerEnabled === "boolean") BANNER_ENABLED = s.bannerEnabled;
    await log(
      `settings bootstrapped tts=${TTS_ENABLED} banner=${BANNER_ENABLED}`,
    );
  } catch (e) {
    await log(`settings bootstrap failed (${e}); using env defaults`);
  }
}

// ── SSE stream loop ──────────────────────────────────────────────────
async function streamOnce(): Promise<void> {
  const ctl = new AbortController();
  const cap = setTimeout(() => ctl.abort("max-time"), STREAM_MAX_MS);
  try {
    const res = await fetch(`${NEXUS_URL}/events/stream`, {
      headers: { Accept: "text/event-stream" },
      signal: ctl.signal,
    });
    if (!res.ok || !res.body) {
      await log(`stream: HTTP ${res.status}`);
      return;
    }
    const reader = res.body.getReader();
    const decoder = new TextDecoder();
    let buffer = "";
    let eventName = "";

    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      buffer += decoder.decode(value, { stream: true });
      // SSE frames are separated by \n\n, but each line is split on \n.
      // We split greedily on \n and keep the trailing partial in buffer.
      const lines = buffer.split("\n");
      buffer = lines.pop() ?? "";

      for (const raw of lines) {
        const line = raw.replace(/\r$/, ""); // tolerate CRLF
        if (line.startsWith("event: ")) {
          eventName = line.slice(7).trim();
        } else if (line.startsWith("data: ")) {
          const data = line.slice(6);
          if (eventName === "NotificationFired") {
            try {
              const parsed = JSON.parse(data) as { payload?: Payload };
              if (parsed.payload) await processEvent(parsed.payload);
            } catch (e) {
              await log(`parse error: ${e}`);
            }
          } else if (eventName === "SettingsChanged") {
            try {
              const parsed = JSON.parse(data) as {
                payload?: { ttsEnabled?: boolean; bannerEnabled?: boolean };
              };
              const p = parsed.payload ?? {};
              if (typeof p.ttsEnabled === "boolean") TTS_ENABLED = p.ttsEnabled;
              if (typeof p.bannerEnabled === "boolean")
                BANNER_ENABLED = p.bannerEnabled;
              await log(
                `SettingsChanged applied tts=${TTS_ENABLED} banner=${BANNER_ENABLED}`,
              );
            } catch {
              /* swallow */
            }
          }
          eventName = "";
        } else if (line === "") {
          // blank line ends a frame; reset event name for the next frame
          eventName = "";
        }
        // ": keepalive" comments are silently consumed — same as bash
      }
    }
  } finally {
    clearTimeout(cap);
  }
}

async function streamLoop(): Promise<never> {
  await log(
    `nexus-listener (listen) starting — url=${NEXUS_URL} fifo=${FIFO} ` +
      `tts_enabled=${TTS_ENABLED} banner_enabled=${BANNER_ENABLED}`,
  );
  for (;;) {
    try {
      await streamOnce();
      await log("stream disconnected, reconnecting in 5s");
    } catch (e) {
      await log(`stream error: ${e}; reconnecting in 5s`);
    }
    await new Promise((r) => setTimeout(r, RECONNECT_MS));
  }
}

// ── Main ─────────────────────────────────────────────────────────────
process.on("SIGTERM", () => {
  log("SIGTERM received, exiting").finally(() => process.exit(0));
});
process.on("SIGINT", () => {
  log("SIGINT received, exiting").finally(() => process.exit(0));
});

await bootstrapSettings();
await streamLoop();
