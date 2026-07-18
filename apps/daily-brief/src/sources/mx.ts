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
 *
 * Radar action client (snoozeTriageItem/setTriageStatus, added 2026-07-18):
 * POSTs unauthenticated first (localhost-trust model, cross-repo dep
 * if-ammk), and on HTTP 401 retries once with a bearer token read from
 * `~/.mx/gateway.env`. Confirmed live against the running gateway the same
 * day: an unauthenticated POST to a triage action endpoint returns 401
 * `{"error":"unauthorized"}`; the same call with the real
 * `MX_GATEWAY_TOKEN` from `~/.mx/gateway.env` returns 503
 * `{"error":"triage ledger unavailable"}` for a bogus item id — i.e. auth
 * passes and the gateway gets past the 401 check before failing on the
 * lookup. These functions NEVER throw — they feed inline UI rendering in a
 * later batch, so a thrown exception would crash the TUI.
 */

import { homedir } from "node:os";
import { join } from "node:path";

const MX_BASE_URL = process.env.MX_GATEWAY_URL ?? "http://127.0.0.1:8799";
const TIMEOUT_MS = 3000;
// Overridable so tests can supply a fixture gateway.env instead of reading
// Leo's real token file — same env-override idiom as MX_GATEWAY_URL above
// (add-daily-brief-tui task 4.3).
const GATEWAY_ENV_PATH = process.env.MX_GATEWAY_ENV_PATH ?? join(homedir(), ".mx/gateway.env");

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

/**
 * Structured result for a radar action POST. NEVER thrown — always
 * resolved, so a caller rendering inline UI can branch on `.ok` without a
 * try/catch (spec.md "Radar section surfaces MINE triage items with
 * act-on-item keys": "render the failure inline (never crash) if both
 * attempts fail").
 */
export interface MxActionResult {
  ok: boolean;
  status?: number;
  error?: string;
}

/**
 * Reads `MX_GATEWAY_TOKEN` out of `~/.mx/gateway.env` (a flat `KEY=value`
 * shell-env-style file, confirmed live 2026-07-18 — a single line,
 * `MX_GATEWAY_TOKEN=<64-char hex>`). Simple line parser rather than a
 * dotenv dependency: the file has no quoting/interpolation to handle, and
 * Bun's own `.env` auto-load only covers cwd-relative `.env` files, not an
 * arbitrary path — stdlib string parsing is the right rung here (Reader
 * Gate preference order). Returns `null` if the file is absent, unreadable,
 * or carries no `MX_GATEWAY_TOKEN` key — callers treat that as "no token
 * available", not an error.
 */
async function readGatewayToken(): Promise<string | null> {
  const file = Bun.file(GATEWAY_ENV_PATH);
  try {
    if (!(await file.exists())) return null;
    const text = await file.text();
    for (const rawLine of text.split("\n")) {
      const line = rawLine.trim();
      if (!line || line.startsWith("#")) continue;
      const eqIdx = line.indexOf("=");
      if (eqIdx === -1) continue;
      const key = line.slice(0, eqIdx).trim();
      if (key === "MX_GATEWAY_TOKEN") {
        return line.slice(eqIdx + 1).trim();
      }
    }
    return null;
  } catch {
    return null;
  }
}

async function postOnce(path: string, token: string | undefined, body: unknown): Promise<
  | { kind: "response"; res: Response }
  | { kind: "error"; error: string }
> {
  try {
    const res = await fetch(`${MX_BASE_URL}${path}`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        ...(token ? { Authorization: `Bearer ${token}` } : {}),
      },
      body: JSON.stringify(body ?? {}),
      signal: AbortSignal.timeout(TIMEOUT_MS),
    });
    return { kind: "response", res };
  } catch (err) {
    return { kind: "error", error: err instanceof Error ? err.message : String(err) };
  }
}

/**
 * POSTs to a triage action endpoint unauthenticated first; on HTTP 401,
 * retries exactly once with a bearer token from `~/.mx/gateway.env` (if
 * present). Never throws — every branch resolves to an `MxActionResult`.
 */
async function mxActionPost(path: string, body?: unknown): Promise<MxActionResult> {
  const first = await postOnce(path, undefined, body);
  if (first.kind === "error") {
    return { ok: false, error: first.error };
  }
  if (first.res.status !== 401) {
    if (first.res.ok) return { ok: true, status: first.res.status };
    return {
      ok: false,
      status: first.res.status,
      error: `HTTP ${first.res.status} ${first.res.statusText}`,
    };
  }

  // 401 — retry once with a bearer token, if one is available.
  const token = await readGatewayToken();
  if (!token) {
    return { ok: false, status: 401, error: "unauthorized; no MX_GATEWAY_TOKEN in ~/.mx/gateway.env" };
  }
  const retry = await postOnce(path, token, body);
  if (retry.kind === "error") {
    return { ok: false, error: retry.error };
  }
  if (retry.res.ok) return { ok: true, status: retry.res.status };
  return {
    ok: false,
    status: retry.res.status,
    error: `HTTP ${retry.res.status} ${retry.res.statusText}`,
  };
}

export async function snoozeTriageItem(id: string): Promise<MxActionResult> {
  return mxActionPost(`/triage/${encodeURIComponent(id)}/snooze`);
}

export async function setTriageStatus(id: string, status: string): Promise<MxActionResult> {
  return mxActionPost(`/triage/${encodeURIComponent(id)}/status`, { status });
}

// action client (snooze/status/401-retry) lands in task 2.1 — this file is
// read-only (GET + pure grouping) for the DB batch.
