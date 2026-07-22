/**
 * plainRender.test.ts — `renderPlainSnapshot()` output-shape suite plus the
 * required security-regression case for the control-char-stripping finding
 * (harden-daily-brief-titles-and-tests task 2.1): a title fixture
 * containing an ANSI escape sequence (`\x1b[`) must render with the escape
 * removed.
 */
import { describe, expect, test } from "bun:test";
import { renderPlainSnapshot } from "../src/plainRender";
import type { DailyBriefSnapshot } from "../src/collect";
import type { TriageItem } from "../src/sources/mx";

function triageItem(titleOverride: string): TriageItem {
  return {
    core: {
      id: "item-1",
      source: "github",
      kind: "issue",
      threadKey: "thread-1",
      title: titleOverride,
      url: "https://example.com/item-1",
      author: { kind: "user", value: "octocat", display: "octocat", source: "github" },
      ballInCourt: "MINE",
      createdAt: "2026-07-20T00:00:00.000Z",
      lastActivityAt: "2026-07-20T00:00:00.000Z",
      stillPresentUpstream: true,
      lastSeenAt: "2026-07-20T00:00:00.000Z",
    },
    payload: {},
  };
}

function baseSnapshot(overrides: Partial<DailyBriefSnapshot> = {}): DailyBriefSnapshot {
  return {
    schemaVersion: 1,
    generated_at: "2026-07-22T12:00:00.000Z",
    mx: {
      available: true,
      briefing: null,
      triage_mine: { open: [], waiting: [], other: [] },
      sources: null,
    },
    meetings: { events: [], source_health: [] },
    open_items: { repos: [], errors: [] },
    docs: {
      hygiene: { available: false, stale: true, generated_at: null, entries: [] },
      sweep: { available: false, stale: true, generated_at: null },
    },
    ...overrides,
  };
}

describe("renderPlainSnapshot", () => {
  test("renders all four section headers", () => {
    const output = renderPlainSnapshot(baseSnapshot());
    expect(output).toContain("MEETINGS");
    expect(output).toContain("RADAR");
    expect(output).toContain("OPEN ITEMS");
    expect(output).toContain("DOCS");
  });

  test("empty sections render their empty-state lines", () => {
    const output = renderPlainSnapshot(baseSnapshot());
    expect(output).toContain("No meetings today.");
    expect(output).toContain("Nothing in your court.");
    expect(output).toContain("No repos with open beads.");
    expect(output).toContain("No flagged docs findings.");
  });

  test("a radar item title containing an ANSI escape sequence renders with the escape stripped", () => {
    const snapshot = baseSnapshot({
      mx: {
        available: true,
        briefing: null,
        triage_mine: {
          open: [triageItem("urgent\x1b[2Jclear the screen")],
          waiting: [],
          other: [],
        },
        sources: null,
      },
    });

    const output = renderPlainSnapshot(snapshot);
    expect(output).not.toContain("\x1b[");
    expect(output).toContain("urgent[2Jclear the screen");
  });
});
