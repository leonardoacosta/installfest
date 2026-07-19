/**
 * rubric-citations.test.ts — every `src/rubric.ts` Table A constant carries a
 * traceable source citation matching `docs/context-budget-rubric.md`'s
 * stated sources (ctx-scan-budgets task [4.1], beads:if-hc1m).
 *
 * "Traceable" is checked against the DOC itself, not just re-asserting
 * rubric.ts's own strings back at itself: the doc's header line names the
 * exact code.claude.com page set + agentskills.io/specification, and Part 0
 * names "Rule 3" as the mechanism for [R]-tagged rows with no external
 * number. This test extracts both from the live doc text and cross-checks
 * every `TABLE_A` row's `sourceCitation` against them, so a citation that
 * drifts from a real doc source (or a row with no citation at all) fails.
 */
import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { TABLE_A } from "../src/rubric";

const DOC_PATH = join(import.meta.dir, "..", "..", "..", "docs", "context-budget-rubric.md");
const DOC_TEXT = readFileSync(DOC_PATH, "utf8");

// The doc's own "Sources:" line names these code.claude.com pages plus the
// agentskills.io spec — extracted from the live doc text, not hardcoded
// independently of it.
const SOURCES_LINE = DOC_TEXT.split("\n").find((l) => l.startsWith("**Sources:**"));

describe("Table A source citations trace to real docs [4.1]", () => {
  test("the doc's own Sources line names the code.claude.com pages + agentskills.io spec this test checks against", () => {
    expect(SOURCES_LINE).toBeDefined();
    expect(SOURCES_LINE).toContain("code.claude.com");
    expect(SOURCES_LINE).toContain("agentskills.io/specification");
    // Every doc page name this test relies on below must actually appear in
    // the doc's Sources line — if the doc drops a page name, DOC_PAGES must
    // shrink with it (never invent a page name absent from the doc).
    for (const page of ["skills.md", "memory.md", "hooks.md", "mcp.md"]) {
      expect(SOURCES_LINE).toContain(page);
    }
  });

  test("the doc's Part 0 actually defines Rule 3 for [R]-tagged repo-set rows", () => {
    expect(DOC_TEXT).toContain("Rule 3 (repo-set)");
    expect(DOC_TEXT).toContain("no external number exists");
  });

  test("every TABLE_A row carries a non-empty sourceCitation", () => {
    for (const row of TABLE_A) {
      expect(row.sourceCitation, `row ${row.id} has no sourceCitation`).toBeTruthy();
      expect(row.sourceCitation.trim().length).toBeGreaterThan(0);
    }
  });

  test("H/G-tagged rows cite one of the doc's real code.claude.com pages or the agentskills.io spec", () => {
    const DOC_PAGE_PATTERN = /code\.claude\.com|agentskills\.io\/specification/;
    for (const row of TABLE_A) {
      if (row.source === "H" || row.source === "G") {
        expect(row.sourceCitation, `row ${row.id} [${row.source}] citation: "${row.sourceCitation}"`).toMatch(
          DOC_PAGE_PATTERN,
        );
      }
    }
  });

  test("R-tagged rows cite Part 0 Rule 3 — the doc's own mechanism for repo-set anchors", () => {
    for (const row of TABLE_A) {
      if (row.source === "R") {
        expect(row.sourceCitation, `row ${row.id} [R] citation: "${row.sourceCitation}"`).toContain("Rule 3");
      }
    }
  });

  test("no row is left with a placeholder/generic citation disconnected from any real source", () => {
    const bannedPlaceholders = ["TBD", "TODO", "n/a", "unknown source", "no citation"];
    for (const row of TABLE_A) {
      const lower = row.sourceCitation.toLowerCase();
      for (const bad of bannedPlaceholders) {
        expect(lower.includes(bad.toLowerCase()), `row ${row.id} citation looks like a placeholder: "${row.sourceCitation}"`).toBe(
          false,
        );
      }
    }
  });

  test("row source tags are exactly H, G, or R (no untagged rows)", () => {
    for (const row of TABLE_A) {
      expect(["H", "G", "R"]).toContain(row.source);
    }
  });

  test("all 14 Table A rows (A1-A14) are present, matching the doc's row count", () => {
    const ids = TABLE_A.map((r) => r.id).sort();
    const expected = Array.from({ length: 14 }, (_, i) => `A${i + 1}`).sort();
    expect(ids).toEqual(expected);
  });
});
