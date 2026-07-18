/**
 * App.tsx — composes the four brief sections into one ink layout and (task
 * 3.2) drives the interactive radar list: j/k selection, Enter to open the
 * selected item's URL via the platform opener, `s` to snooze, `t` to set
 * triage status, `q` to quit.
 *
 * Resize-safety is inherent, not special-cased: every Box below uses flex
 * layout with no hardcoded widths, and ink's `render()` already listens for
 * the stdout `resize` event internally (verified in
 * node_modules/ink/build/ink.js) and re-renders — Text lines use
 * `wrap="truncate-end"` (see sections.tsx) so a narrow pane truncates
 * instead of wrapping/clipping badly.
 *
 * mx.ts's `snoozeTriageItem`/`setTriageStatus` NEVER throw (they resolve to
 * a structured `MxActionResult`), so action results are rendered inline via
 * RadarPanel's `actionResults` map — a failed action never crashes this
 * component (spec.md "render the failure inline (never crash)").
 */

import { platform } from "node:os";
import { useState } from "react";
import { Box, Text, useApp, useInput } from "ink";
import type { DailyBriefSnapshot } from "../collect";
import { setTriageStatus, snoozeTriageItem, type MxActionResult } from "../sources/mx";
import { flattenRadarRows, formatRadarGroups } from "./format";
import { DocsPanel, MeetingsPanel, OpenItemsPanel, RadarPanel, type RadarActionResult } from "./sections";

export interface AppProps {
  snapshot: DailyBriefSnapshot;
}

type Mode = "normal" | "confirm-snooze" | "input-status";

/** Shells out to the platform's URL opener. Never throws — `Bun.spawn`
 * throws synchronously when the binary is missing (ENOENT), which this
 * catches and turns into a structured failure result instead of crashing
 * the app (spec.md "render the failure inline (never crash)"). Does not
 * await the opener process's exit — firing the browser/handler and moving
 * on is the correct UX for a keypress. */
function openUrl(url: string): MxActionResult {
  const opener = platform() === "darwin" ? "open" : "xdg-open";
  try {
    Bun.spawn([opener, url], { stdout: "ignore", stderr: "ignore", stdin: "ignore" });
    return { ok: true };
  } catch (err) {
    return { ok: false, error: err instanceof Error ? err.message : String(err) };
  }
}

export default function App({ snapshot }: AppProps) {
  const { exit } = useApp();
  const rows = flattenRadarRows(formatRadarGroups(snapshot.mx));

  const [selectedIndex, setSelectedIndex] = useState(0);
  const [mode, setMode] = useState<Mode>("normal");
  const [statusBuffer, setStatusBuffer] = useState("");
  const [pendingId, setPendingId] = useState<string | null>(null);
  const [actionResults, setActionResults] = useState<Record<string, RadarActionResult>>({});

  const selected = rows[selectedIndex];

  function recordResult(id: string, kind: RadarActionResult["kind"], result: MxActionResult) {
    setActionResults((prev) => ({ ...prev, [id]: { kind, result } }));
    setPendingId(null);
  }

  function runSnooze(id: string) {
    setPendingId(id);
    void snoozeTriageItem(id).then((result) => recordResult(id, "snooze", result));
  }

  function runSetStatus(id: string, status: string) {
    setPendingId(id);
    void setTriageStatus(id, status).then((result) => recordResult(id, "status", result));
  }

  useInput((input, key) => {
    if (mode === "input-status") {
      if (key.return) {
        const trimmed = statusBuffer.trim();
        if (selected && trimmed.length > 0) {
          runSetStatus(selected.id, trimmed);
        }
        setMode("normal");
        setStatusBuffer("");
        return;
      }
      if (key.escape) {
        setMode("normal");
        setStatusBuffer("");
        return;
      }
      if (key.backspace || key.delete) {
        setStatusBuffer((prev) => prev.slice(0, -1));
        return;
      }
      if (input && !key.ctrl && !key.meta) {
        setStatusBuffer((prev) => prev + input);
      }
      return;
    }

    if (mode === "confirm-snooze") {
      if (input === "y" && selected) {
        runSnooze(selected.id);
      }
      setMode("normal");
      return;
    }

    // mode === "normal"
    if (input === "q") {
      exit();
      return;
    }
    if (input === "j" || key.downArrow) {
      setSelectedIndex((idx) => Math.min(idx + 1, Math.max(rows.length - 1, 0)));
      return;
    }
    if (input === "k" || key.upArrow) {
      setSelectedIndex((idx) => Math.max(idx - 1, 0));
      return;
    }
    if (key.return && selected) {
      recordResult(selected.id, "open", openUrl(selected.url));
      return;
    }
    if (input === "s" && selected) {
      setMode("confirm-snooze");
      return;
    }
    if (input === "t" && selected) {
      setStatusBuffer("");
      setMode("input-status");
      return;
    }
  });

  return (
    <Box flexDirection="column">
      <MeetingsPanel meetings={snapshot.meetings} />
      <RadarPanel
        mx={snapshot.mx}
        selectedId={selected?.id}
        actionResults={actionResults}
        pendingId={pendingId}
      />
      <OpenItemsPanel openItems={snapshot.open_items} />
      <DocsPanel docs={snapshot.docs} />
      <Box marginTop={1} flexDirection="column">
        {mode === "confirm-snooze" ? (
          <Text color="yellow">Snooze "{selected?.title}"? (y/n)</Text>
        ) : null}
        {mode === "input-status" ? (
          <Text color="yellow">
            Set status for "{selected?.title}": {statusBuffer}_
          </Text>
        ) : null}
        <Text dimColor>j/k move · enter open · s snooze · t set status · q quit</Text>
      </Box>
    </Box>
  );
}
