/**
 * plainRender.ts — static ANSI-free text render for `daily-brief view --plain`.
 *
 * Reuses the exact same pure formatting functions from `./ui/format` that
 * `./ui/sections.tsx` consumes for the interactive ink render, so plain-text
 * and ink output can never drift (spec.md "Brief renders as an ink TUI...
 * a non-interactive `view --plain` static render"). This module
 * intentionally imports NOTHING from `ink`/`react` — only the pure
 * data-shaping layer — so the `--plain` path never mounts (or even loads)
 * the ink/React runtime.
 */

import type { DailyBriefSnapshot } from "./collect";
import {
  formatDocsSummary,
  formatMeetingsSummary,
  formatOpenItemsRepos,
  formatRadarGroups,
  relativeAgo,
} from "./ui/format";

const TOP_ITEMS_DISPLAY_LIMIT = 5;

function renderMeetings(snapshot: DailyBriefSnapshot): string[] {
  const { staleBanners, eventLines } = formatMeetingsSummary(snapshot.meetings);
  const lines = ["MEETINGS"];
  for (const banner of staleBanners) lines.push(`  ! ${banner}`);
  if (eventLines.length === 0 && staleBanners.length === 0) {
    lines.push("  No meetings today.");
  }
  for (const event of eventLines) {
    lines.push(
      `  ${event.time.padEnd(9)} ${event.title}${event.location ? ` (${event.location})` : ""}`,
    );
  }
  return lines;
}

function renderRadar(snapshot: DailyBriefSnapshot): string[] {
  const lines = [`RADAR${snapshot.mx.available ? "" : " (mx unavailable)"}`];
  if (!snapshot.mx.available && snapshot.mx.error) lines.push(`  ! ${snapshot.mx.error}`);

  const groups = formatRadarGroups(snapshot.mx);
  const total = groups.reduce((sum, group) => sum + group.rows.length, 0);
  if (total === 0 && snapshot.mx.available) lines.push("  Nothing in your court.");

  for (const group of groups) {
    if (group.rows.length === 0) continue;
    lines.push(`  ${group.label}`);
    for (const row of group.rows) {
      lines.push(`    - ${row.title} [${row.source}] (${relativeAgo(row.lastActivityAt)})`);
    }
  }
  return lines;
}

function renderOpenItems(snapshot: DailyBriefSnapshot): string[] {
  const repos = formatOpenItemsRepos(snapshot.open_items);
  const lines = ["OPEN ITEMS"];
  if (repos.length === 0) lines.push("  No repos with open beads.");

  for (const repo of repos) {
    lines.push(
      `  ${repo.code}: ${repo.total_open} open (${repo.blocked} blocked, ${repo.human_only} human-only)`,
    );
    for (const item of repo.top_items.slice(0, TOP_ITEMS_DISPLAY_LIMIT)) {
      lines.push(`    - [${item.bucket}] ${item.title}`);
    }
  }
  for (const err of snapshot.open_items.errors) {
    lines.push(`  ! ${err.repo}: ${err.error}`);
  }
  return lines;
}

function renderDocs(snapshot: DailyBriefSnapshot): string[] {
  const { staleBanner, hygieneEntries, sweepFlagged } = formatDocsSummary(snapshot.docs);
  const lines = ["DOCS"];
  if (staleBanner) lines.push(`  ! ${staleBanner}`);
  if (hygieneEntries.length === 0 && sweepFlagged.length === 0) {
    lines.push("  No flagged docs findings.");
  }
  for (const entry of hygieneEntries) {
    lines.push(`    - ${entry.repo}: ${entry.status}${entry.detail ? ` — ${entry.detail}` : ""}`);
  }
  for (const finding of sweepFlagged) {
    lines.push(`    - ${finding.path}: ${finding.verdict}`);
  }
  return lines;
}

export function renderPlainSnapshot(snapshot: DailyBriefSnapshot): string {
  return [
    ...renderMeetings(snapshot),
    "",
    ...renderRadar(snapshot),
    "",
    ...renderOpenItems(snapshot),
    "",
    ...renderDocs(snapshot),
  ].join("\n");
}
