/**
 * mx.ts — fail-open read client for mx-gateway (http://127.0.0.1:8799).
 *
 * Every exported fetch function NEVER throws: on network error, timeout, or
 * a non-2xx response it resolves to `{ available: false, error }` so a
 * collector composing multiple sources can degrade this one independently
 * (see spec.md "Daily snapshot collection composes all brief sources
 * fail-open").
 *
 * Real response shapes below were confirmed live against the running
 * gateway on 2026-07-18 — see collect.ts / index.tsx run output.
 */

const MX_BASE_URL = process.env.MX_GATEWAY_URL ?? "http://127.0.0.1:8799";
const TIMEOUT_MS = 3000;

export interface MxAvailable<T> {
  available: true;
  data: T;
}

export interface MxUnavailable {
  available: false;
  error: string;
}

export type MxResult<T> = MxAvailable<T> | MxUnavailable;

export interface CalendarEvent {
  title: string;
  start_time: string;
  all_day: boolean;
  location: string | null;
}

export interface BriefingQueueItem {
  [key: string]: unknown;
}

export interface MedEntry {
  group: string;
  med_name: string;
  dose: string;
  status: string;
}

export interface Briefing {
  generated_at: string;
  queue: BriefingQueueItem[];
  calendar: CalendarEvent[];
  meds: MedEntry[];
}

export interface TriageAuthor {
  kind: string;
  value: string;
  display: string;
  source: string;
}

export interface TriageCore {
  id: string;
  source: string;
  kind: string;
  threadKey: string;
  title: string;
  url: string;
  author: TriageAuthor;
  ballInCourt: string;
  createdAt: string;
  lastActivityAt: string;
  stillPresentUpstream: boolean;
  lastSeenAt: string;
}

export interface TriageComms {
  priority: string;
  upstreamState: string;
  suggestedDisposition: string;
  dispositionEvidence: string;
}

export interface TriageItem {
  core: TriageCore;
  payload: {
    comms?: TriageComms;
  };
  /**
   * spec.md's Radar requirement describes grouping by `verdict.disposition`,
   * but the live /triage payload (confirmed 2026-07-18) carries no `verdict`
   * field at all — only `payload.comms.suggestedDisposition`. Kept optional
   * here (rather than silently dropped) so a future gateway version adding
   * `verdict` is picked up automatically by groupRadarItems below without a
   * type change.
   */
  verdict?: {
    disposition?: string;
  };
}

export interface SourceEntry {
  id: string;
  display_name: string;
  produces_kind: string | null;
  in_aggregate: boolean;
  status: string;
  reason: string;
  last_sync_at: string | null;
  item_count: number | null;
  mine_count: number;
  can_search: boolean;
  can_stream: boolean;
}

export interface SourcesResponse {
  sources: SourceEntry[];
}

async function mxFetch<T>(path: string): Promise<MxResult<T>> {
  try {
    const res = await fetch(`${MX_BASE_URL}${path}`, {
      signal: AbortSignal.timeout(TIMEOUT_MS),
    });
    if (!res.ok) {
      return { available: false, error: `HTTP ${res.status} ${res.statusText}` };
    }
    const data = (await res.json()) as T;
    return { available: true, data };
  } catch (err) {
    return {
      available: false,
      error: err instanceof Error ? err.message : String(err),
    };
  }
}

export async function getBriefing(): Promise<MxResult<Briefing>> {
  return mxFetch<Briefing>("/briefing");
}

export async function getTriage(): Promise<MxResult<TriageItem[]>> {
  return mxFetch<TriageItem[]>("/triage");
}

export async function getSources(): Promise<MxResult<SourcesResponse>> {
  return mxFetch<SourcesResponse>("/sources");
}

export interface RadarGroups {
  open: TriageItem[];
  waiting: TriageItem[];
  other: TriageItem[];
}

/**
 * Pure function: filters triage items to `core.ballInCourt === "MINE"` and
 * groups by disposition, OPEN before WAITING (spec.md "Radar section
 * surfaces MINE triage items"). Takes an already-fetched array — no I/O.
 */
export function groupRadarItems(triageItems: TriageItem[]): RadarGroups {
  const groups: RadarGroups = { open: [], waiting: [], other: [] };
  for (const item of triageItems) {
    if (item.core.ballInCourt !== "MINE") continue;
    const disposition = item.verdict?.disposition ?? item.payload?.comms?.suggestedDisposition;
    if (disposition === "OPEN") groups.open.push(item);
    else if (disposition === "WAITING") groups.waiting.push(item);
    else groups.other.push(item);
  }
  return groups;
}

// action client (snooze/status/401-retry) lands in task 2.1 — this file is
// read-only (GET + pure grouping) for the DB batch.
