/**
 * sections.tsx — pure ink components for the four brief sections (MEETINGS
 * / RADAR / OPEN ITEMS / DOCS), each a function of a slice of the
 * collect.ts snapshot shape (spec.md "Brief renders as an ink TUI...").
 *
 * All display shaping (staleness banners, grouping, relative timestamps) is
 * delegated to `./format` — the same pure functions `plainRender.ts`
 * consumes for `view --plain`, so the two renders never drift.
 *
 * RadarPanel additionally accepts optional interactive-state props
 * (`selectedId`, `actionResults`, `pendingId`) so the static (task 3.1) and
 * interactive (task 3.2, driven by App.tsx's `useInput`) renders share one
 * component — omitting them renders the plain grouped list with no
 * selection cursor or inline action feedback.
 */

import { Box, Text } from "ink";
import type { MeetingsSection, MxSection } from "../collect";
import type { DocsState } from "../sources/docsState";
import type { MxActionResult } from "../sources/mx";
import type { OpenItemsScan } from "../sources/openItems";
import {
  flattenRadarRows,
  formatDocsSummary,
  formatMeetingsSummary,
  formatOpenItemsRepos,
  formatRadarGroups,
  relativeAgo,
} from "./format";

const TOP_ITEMS_DISPLAY_LIMIT = 5;

export interface RadarActionResult {
  kind: "snooze" | "status" | "open";
  result: MxActionResult;
}

function actionLabel(kind: RadarActionResult["kind"], ok: boolean): string {
  const verb = kind === "snooze" ? "snooze" : kind === "status" ? "status" : "open";
  return ok ? `${verb} ok` : `${verb} failed`;
}

function groupColor(label: string): string | undefined {
  if (label === "OPEN") return "green";
  if (label === "WAITING") return "magenta";
  if (label === "OTHER") return "gray";
  return undefined;
}

export function MeetingsPanel({ meetings }: { meetings: MeetingsSection }) {
  const { staleBanners, eventLines } = formatMeetingsSummary(meetings);
  return (
    <Box flexDirection="column" marginBottom={1}>
      <Text bold color="cyan">
        MEETINGS
      </Text>
      {staleBanners.map((banner) => (
        <Text key={banner} color="yellow">
          ! {banner}
        </Text>
      ))}
      {eventLines.length === 0 && staleBanners.length === 0 ? (
        <Text dimColor>No meetings today.</Text>
      ) : null}
      {eventLines.map((event, idx) => (
        <Text key={`${idx}-${event.time}-${event.title}`} wrap="truncate-end">
          {event.time.padEnd(9)} {event.title}
          {event.location ? ` (${event.location})` : ""}
        </Text>
      ))}
    </Box>
  );
}

export function RadarPanel({
  mx,
  selectedId,
  actionResults,
  pendingId,
}: {
  mx: MxSection;
  selectedId?: string;
  actionResults?: Record<string, RadarActionResult>;
  pendingId?: string | null;
}) {
  const groups = formatRadarGroups(mx);
  const totalRows = flattenRadarRows(groups).length;

  return (
    <Box flexDirection="column" marginBottom={1}>
      <Text bold color="cyan">
        RADAR{mx.available ? "" : " (mx unavailable)"}
      </Text>
      {!mx.available && mx.error ? <Text color="red">! {mx.error}</Text> : null}
      {totalRows === 0 && mx.available ? <Text dimColor>Nothing in your court.</Text> : null}
      {groups.map((group) =>
        group.rows.length === 0 ? null : (
          <Box flexDirection="column" key={group.label}>
            <Text color={groupColor(group.label)}>{group.label}</Text>
            {group.rows.map((row) => {
              const isSelected = row.id === selectedId;
              const action = actionResults?.[row.id];
              const isPending = pendingId === row.id;
              return (
                <Box key={row.id} flexDirection="row">
                  <Text color={isSelected ? "cyan" : undefined}>{isSelected ? "> " : "  "}</Text>
                  <Text wrap="truncate-end">
                    {row.title} [{row.source}] · {relativeAgo(row.lastActivityAt)}
                  </Text>
                  {isPending ? <Text color="yellow"> …</Text> : null}
                  {action ? (
                    <Text color={action.result.ok ? "green" : "red"}>
                      {" "}
                      {actionLabel(action.kind, action.result.ok)}
                      {action.result.ok
                        ? ""
                        : `: ${action.result.error ?? String(action.result.status ?? "unknown")}`}
                    </Text>
                  ) : null}
                </Box>
              );
            })}
          </Box>
        ),
      )}
    </Box>
  );
}

export function OpenItemsPanel({ openItems }: { openItems: OpenItemsScan }) {
  const repos = formatOpenItemsRepos(openItems);
  return (
    <Box flexDirection="column" marginBottom={1}>
      <Text bold color="cyan">
        OPEN ITEMS
      </Text>
      {repos.length === 0 ? <Text dimColor>No repos with open beads.</Text> : null}
      {repos.map((repo) => (
        <Box flexDirection="column" key={repo.code}>
          <Text>
            {repo.code}: {repo.total_open} open ({repo.blocked} blocked, {repo.human_only} human-only)
          </Text>
          {repo.top_items.slice(0, TOP_ITEMS_DISPLAY_LIMIT).map((item) => (
            <Text key={item.id} dimColor wrap="truncate-end">
              {"  "}· [{item.bucket}] {item.title}
            </Text>
          ))}
        </Box>
      ))}
      {openItems.errors.map((err) => (
        <Text key={err.repo} color="red">
          ! {err.repo}: {err.error}
        </Text>
      ))}
    </Box>
  );
}

export function DocsPanel({ docs }: { docs: DocsState }) {
  const { staleBanner, hygieneEntries, sweepFlagged } = formatDocsSummary(docs);
  return (
    <Box flexDirection="column">
      <Text bold color="cyan">
        DOCS
      </Text>
      {staleBanner ? <Text color="yellow">! {staleBanner}</Text> : null}
      {hygieneEntries.length === 0 && sweepFlagged.length === 0 ? (
        <Text dimColor>No flagged docs findings.</Text>
      ) : null}
      {hygieneEntries.map((entry, idx) => (
        <Text key={`${idx}-${entry.repo}`} wrap="truncate-end">
          {"  "}· {entry.repo}: {entry.status}
          {entry.detail ? ` — ${entry.detail}` : ""}
        </Text>
      ))}
      {sweepFlagged.map((finding, idx) => (
        <Text key={`${idx}-${finding.path}`} wrap="truncate-end">
          {"  "}· {finding.path}: {finding.verdict}
        </Text>
      ))}
    </Box>
  );
}
