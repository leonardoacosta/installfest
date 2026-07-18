/**
 * collect.ts — composes the schemaVersion-1 daily-brief snapshot.
 *
 * Combines mx (briefing + triage + sources), meetings (calendar +
 * per-calendar-source staleness), open_items (fleet beads scan), and docs
 * (doc-hygiene state) into one JSON object, then writes it atomically to
 * `~/.local/state/daily-brief/<YYYY-MM-DD>.json` and `latest.json` (`.tmp`
 * sibling + rename, so an interrupted write never corrupts the prior
 * snapshot — spec.md "snapshot written atomically").
 *
 * The whole entrypoint is wrapped so it NEVER throws uncaught and NEVER
 * exits non-zero: every source already degrades independently, and this
 * top-level try/catch is the last-resort backstop that still writes a
 * best-effort (all-failed) snapshot rather than aborting the run.
 */

import { mkdir, rename, writeFile } from "node:fs/promises";
import { homedir } from "node:os";
import { join } from "node:path";
import { collectDocsState, type DocsState } from "./sources/docsState";
import { collectOpenItems, type OpenItemsScan } from "./sources/openItems";
import {
  getBriefing,
  getSources,
  getTriage,
  groupRadarItems,
  type MxResult,
  type Briefing,
  type SourcesResponse,
  type TriageItem,
} from "./sources/mx";

const STATE_DIR = join(homedir(), ".local/state/daily-brief");
const CALENDAR_SOURCE_IDS = new Set(["gcal", "outlook-calendar"]);
const CALENDAR_STALE_MS = 24 * 60 * 60 * 1000;

export interface MxSection {
  available: boolean;
  briefing: Briefing | null;
  triage_mine: {
    open: TriageItem[];
    waiting: TriageItem[];
    other: TriageItem[];
  };
  sources: SourcesResponse["sources"] | null;
  error?: string;
}

export interface CalendarSourceHealth {
  id: string;
  last_sync_at: string | null;
  item_count: number | null;
  stale: boolean;
}

export interface MeetingsSection {
  events: Briefing["calendar"];
  source_health: CalendarSourceHealth[];
}

export interface DailyBriefSnapshot {
  schemaVersion: 1;
  generated_at: string;
  mx: MxSection;
  meetings: MeetingsSection;
  open_items: OpenItemsScan;
  docs: DocsState;
}

function isCalendarSourceStale(sourceEntry: {
  status: string;
  last_sync_at: string | null;
}): boolean {
  if (sourceEntry.status !== "SERVING") return true;
  if (sourceEntry.last_sync_at === null) return true;
  const lastSyncMs = Date.parse(sourceEntry.last_sync_at);
  return Date.now() - lastSyncMs > CALENDAR_STALE_MS;
}

async function collectMx(): Promise<{ mx: MxSection; meetings: MeetingsSection }> {
  const [briefingResult, triageResult, sourcesResult]: [
    MxResult<Briefing>,
    MxResult<TriageItem[]>,
    MxResult<SourcesResponse>,
  ] = await Promise.all([getBriefing(), getTriage(), getSources()]);

  const briefing = briefingResult.available ? briefingResult.data : null;
  const triage = triageResult.available ? triageResult.data : [];
  const sources = sourcesResult.available ? sourcesResult.data.sources : null;

  const mx: MxSection = {
    available: briefingResult.available || triageResult.available || sourcesResult.available,
    briefing,
    triage_mine: groupRadarItems(triage),
    sources,
    error: [briefingResult, triageResult, sourcesResult]
      .filter((r): r is { available: false; error: string } => !r.available)
      .map((r) => r.error)
      .join("; ") || undefined,
  };

  const source_health: CalendarSourceHealth[] = (sources ?? [])
    .filter((s) => CALENDAR_SOURCE_IDS.has(s.id))
    .map((s) => ({
      id: s.id,
      last_sync_at: s.last_sync_at,
      item_count: s.item_count,
      stale: isCalendarSourceStale(s),
    }));

  const meetings: MeetingsSection = {
    events: briefing?.calendar ?? [],
    source_health,
  };

  return { mx, meetings };
}

async function atomicWriteJson(path: string, data: unknown): Promise<void> {
  const tmpPath = `${path}.tmp`;
  await writeFile(tmpPath, JSON.stringify(data, null, 2), "utf-8");
  await rename(tmpPath, path);
}

function todayDateString(): string {
  return new Date().toISOString().slice(0, 10);
}

async function buildSnapshot(): Promise<DailyBriefSnapshot> {
  const [{ mx, meetings }, open_items, docs] = await Promise.all([
    collectMx(),
    collectOpenItems(),
    collectDocsState(),
  ]);

  return {
    schemaVersion: 1,
    generated_at: new Date().toISOString(),
    mx,
    meetings,
    open_items,
    docs,
  };
}

function allFailedSnapshot(error: unknown): DailyBriefSnapshot {
  const message = error instanceof Error ? error.message : String(error);
  return {
    schemaVersion: 1,
    generated_at: new Date().toISOString(),
    mx: {
      available: false,
      briefing: null,
      triage_mine: { open: [], waiting: [], other: [] },
      sources: null,
      error: message,
    },
    meetings: { events: [], source_health: [] },
    open_items: { repos: [], errors: [{ repo: "collect", error: message }] },
    docs: {
      hygiene: { available: false, stale: true, generated_at: null, entries: [], error: message },
      sweep: { available: false, stale: true, generated_at: null, error: message },
    },
  };
}

/**
 * Runs the full collection + write. Never throws, never resolves to a
 * non-zero-worthy failure — on any uncaught error it still writes a
 * best-effort all-failed snapshot so callers (the CLI entrypoint, the 6am
 * systemd/launchd unit) can always exit 0.
 */
export async function collect(): Promise<DailyBriefSnapshot> {
  let snapshot: DailyBriefSnapshot;
  try {
    snapshot = await buildSnapshot();
  } catch (err) {
    snapshot = allFailedSnapshot(err);
  }

  try {
    await mkdir(STATE_DIR, { recursive: true });
    const datedPath = join(STATE_DIR, `${todayDateString()}.json`);
    const latestPath = join(STATE_DIR, "latest.json");
    await atomicWriteJson(datedPath, snapshot);
    await atomicWriteJson(latestPath, snapshot);
  } catch {
    // Writing the snapshot failed (disk full, permissions, etc.) — still
    // return it in-memory so the CLI can print/inspect it; the collect
    // entrypoint itself never throws past this point.
  }

  return snapshot;
}
