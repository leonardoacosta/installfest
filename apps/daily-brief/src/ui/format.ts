/**
 * format.ts — pure, framework-free formatting/shaping functions for the
 * daily-brief snapshot sections.
 *
 * Deliberately has ZERO ink/react import: `src/ui/sections.tsx` (the ink
 * render) and `src/plainRender.ts` (the `view --plain` static text render)
 * both consume these same functions, so the two renderers can never drift
 * (spec.md "Brief renders as an ink TUI... a non-interactive `view --plain`
 * static render"). Every function here is a pure transform of an
 * already-collected snapshot slice — no I/O, no React, no ink Box/Text.
 *
 * `plainRender.ts` importing only this module (not sections.tsx) means the
 * `--plain` path never pulls in the ink/React runtime at all.
 */

import type { CalendarSourceHealth, MeetingsSection, MxSection } from "../collect";
import type { DocsState, HygieneEntry, SweepFinding } from "../sources/docsState";
import type { OpenItemsRepoResult, OpenItemsScan } from "../sources/openItems";
import type { CalendarEvent, TriageItem } from "../sources/mx";

/** Statuses that a docs-hygiene-daily entry may carry that mean "nothing to
 * flag" — the only vocabulary confirmed live on this machine so far is
 * `"error"` (see docsState.ts's own header comment); this allowlist exists
 * so a future healthy/"ok" status doesn't silently get treated as a flagged
 * finding, without assuming a specific unverified success string is the
 * only one that can occur. */
const HYGIENE_OK_STATUSES = new Set(["ok", "success", "clean", "healthy"]);

/** Strips C0 (`\x00-\x1F`, including tab) and C1 (`\x7F-\x9F`) control
 * characters from an untrusted external string (email subjects, GitHub
 * titles, calendar event titles from mx-gateway) before it reaches either
 * renderer — an unstripped ANSI escape sequence in a title can manipulate
 * the terminal on render (cursor moves, line clears, output spoofing). Pure
 * function, no I/O. */
export function stripControlChars(s: string): string {
  return s.replace(/[\x00-\x1F\x7F-\x9F]/g, "");
}

/** Humanizes an ISO timestamp as "Xm/Xh/Xd ago" relative to `nowMs`
 * (defaults to `Date.now()`). Returns the raw string unchanged if it does
 * not parse as a date. */
export function relativeAgo(iso: string, nowMs: number = Date.now()): string {
  const then = Date.parse(iso);
  if (Number.isNaN(then)) return iso;
  const diffMs = Math.max(0, nowMs - then);
  const diffMin = Math.floor(diffMs / 60_000);
  if (diffMin < 1) return "just now";
  if (diffMin < 60) return `${diffMin}m ago`;
  const diffHr = Math.floor(diffMin / 60);
  if (diffHr < 24) return `${diffHr}h ago`;
  const diffDay = Math.floor(diffHr / 24);
  return `${diffDay}d ago`;
}

function formatEventTime(event: CalendarEvent): string {
  if (event.all_day) return "All day";
  const parsed = new Date(event.start_time);
  if (Number.isNaN(parsed.getTime())) return event.start_time;
  return parsed.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}

/** One line per stale/never-synced calendar source health entry. `null`
 * when a source is healthy (spec.md: banner only appears when a source is
 * NOT_SERVING, has a null last_sync_at, or is >24h stale — collect.ts's
 * `isCalendarSourceStale` already computed the boolean `stale` flag this
 * reads). */
export function describeCalendarSourceHealth(source: CalendarSourceHealth): string | null {
  if (!source.stale) return null;
  if (source.last_sync_at === null) {
    return `${source.id}: never synced`;
  }
  return `${source.id}: stale (last synced ${relativeAgo(source.last_sync_at)})`;
}

export interface MeetingsEventLine {
  time: string;
  title: string;
  location: string | null;
}

export interface MeetingsSummary {
  staleBanners: string[];
  eventLines: MeetingsEventLine[];
}

export function formatMeetingsSummary(meetings: MeetingsSection): MeetingsSummary {
  const staleBanners = meetings.source_health
    .map(describeCalendarSourceHealth)
    .filter((line): line is string => line !== null);
  const eventLines = meetings.events.map((event) => ({
    time: formatEventTime(event),
    title: stripControlChars(event.title),
    location: event.location === null ? null : stripControlChars(event.location),
  }));
  return { staleBanners, eventLines };
}

export interface RadarRow {
  id: string;
  title: string;
  source: string;
  url: string;
  lastActivityAt: string;
}

export interface RadarGroupFormatted {
  label: string;
  rows: RadarRow[];
}

function toRadarRows(items: TriageItem[]): RadarRow[] {
  return items.map((item) => ({
    id: item.core.id,
    title: stripControlChars(item.core.title),
    source: item.core.source,
    url: item.core.url,
    lastActivityAt: item.core.lastActivityAt,
  }));
}

/** Groups the snapshot's already-grouped `mx.triage_mine` (produced by
 * `groupRadarItems()` in collect.ts — NOT re-derived here) into display
 * groups, OPEN before WAITING per spec.md. An "OTHER" group is appended
 * only when non-empty, so an item with an unrecognized disposition is
 * still visible rather than silently dropped. */
export function formatRadarGroups(mx: MxSection): RadarGroupFormatted[] {
  const groups: RadarGroupFormatted[] = [
    { label: "OPEN", rows: toRadarRows(mx.triage_mine.open) },
    { label: "WAITING", rows: toRadarRows(mx.triage_mine.waiting) },
  ];
  if (mx.triage_mine.other.length > 0) {
    groups.push({ label: "OTHER", rows: toRadarRows(mx.triage_mine.other) });
  }
  return groups;
}

export function flattenRadarRows(groups: RadarGroupFormatted[]): RadarRow[] {
  return groups.flatMap((group) => group.rows);
}

export interface OpenItemsRepoLine {
  code: string;
  total_open: number;
  blocked: number;
  human_only: number;
  top_items: OpenItemsRepoResult["top_items"];
}

/** Per-repo open-item count line + already-sorted top items (blocked/
 * human_only buckets first — that ordering comes from `topItems()` in
 * openItems.ts and is reused as-is, never re-sorted here). */
export function formatOpenItemsRepos(openItems: OpenItemsScan): OpenItemsRepoLine[] {
  return openItems.repos.map((repo) => ({
    code: repo.code,
    total_open: repo.summary.total_open,
    blocked: repo.bucket_counts["blocked"] ?? 0,
    human_only: repo.bucket_counts["human_only"] ?? 0,
    top_items: repo.top_items.map((item) => ({ ...item, title: stripControlChars(item.title) })),
  }));
}

export interface DocsSummary {
  staleBanner: string | null;
  hygieneEntries: HygieneEntry[];
  sweepFlagged: SweepFinding[];
}

/** Flagged/error findings only — sweep findings are already pre-filtered to
 * non-"verified" verdicts by `docsState.ts`'s `readSweep()`; hygiene entries
 * are filtered here against a known-ok-status allowlist since the raw
 * `results.jsonl` carries every run's entries unfiltered. */
export function formatDocsSummary(docs: DocsState): DocsSummary {
  const staleParts: string[] = [];
  if (!docs.hygiene.available || docs.hygiene.stale) staleParts.push("hygiene state stale/missing");
  if (!docs.sweep.available || docs.sweep.stale) staleParts.push("sweep state stale/missing");

  const hygieneEntries = docs.hygiene.entries.filter(
    (entry) => !HYGIENE_OK_STATUSES.has(entry.status),
  );

  return {
    staleBanner: staleParts.length > 0 ? `stale: ${staleParts.join(", ")}` : null,
    hygieneEntries,
    sweepFlagged: docs.sweep.flagged ?? [],
  };
}
