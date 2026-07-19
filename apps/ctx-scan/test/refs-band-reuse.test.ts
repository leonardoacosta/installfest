/**
 * refs-band-reuse.test.ts — ctx-scan-refs task [4.2], beads:if-akzw.
 *
 * `refs.ts`'s own module doc states it reuses `rubric.ts`'s `TABLE_A` row
 * definitions and `bandFor`/`worseBand` functions directly, never
 * re-deriving the A5 (ToC)/A6 (nesting) threshold numbers — the real Fleet
 * pipeline (`pipeline.ts`) never ingests reference-file Nodes at all, so
 * A5/A6 stay `computable: false` there (see `audit-contract.test.ts` /
 * `audit-cc-scorecard.test.ts`, which hard-assert exactly that), and this
 * shelf is the only place these rows ever get applied to a real
 * measurement. Rather than asserting against a populated `Node.bands` array
 * that structurally cannot exist for a reference file, this test proves the
 * REUSE directly: it pulls A5/A6's `greenMax`/`amberMax` straight from
 * `rubric.ts`'s live `TABLE_A` (never a duplicated literal), builds fixture
 * files sized exactly at those published boundaries, and asserts every
 * shelf entry's computed band is byte-identical to calling `rubric.ts`'s
 * own `bandFor` directly against that SAME entry's reported raw measurement
 * (`tocLines`/`nestingLinks`). If `refs.ts` ever re-derived its own
 * threshold numbers instead of importing `rubric.ts`'s, either this
 * boundary-exactness assertion or the direct `bandFor`-equality assertion
 * would fail.
 */
import { afterEach, describe, expect, test } from "bun:test";
import { buildShelf } from "../src/refs";
import { TABLE_A, bandFor } from "../src/rubric";
import { cleanup, file, tmpRoot } from "./helpers/tree";

const roots: string[] = [];
afterEach(() => {
  while (roots.length) cleanup(roots.pop()!);
});
function tmp(prefix: string): string {
  const root = tmpRoot(prefix);
  roots.push(root);
  return root;
}

const A5_ROW = TABLE_A.find((r) => r.id === "A5")!;
const A6_ROW = TABLE_A.find((r) => r.id === "A6")!;

/** N body lines with no ToC heading/link-list (avoids `hasTableOfContents`'s first-40-lines detector) and no markdown links (avoids A6 nesting noise). */
function plainLines(n: number): string {
  return Array.from({ length: n }, (_, i) => `Plain body line ${i} of this fixture reference file.`).join("\n");
}

describe("references shelf — A5/A6 band reuse from rubric.ts, not re-derivation [4.2]", () => {
  test("ToC (A5) band matches rubric.ts's bandFor at the published greenMax/amberMax boundaries", () => {
    const claudeHome = tmp("ctx-scan-refs-a5-");
    file(claudeHome, "skills/toc-fixture/SKILL.md", "# ToC Fixture Skill\n");

    // Exactly at greenMax lines -> GREEN (inclusive boundary, no ToC present).
    file(claudeHome, "skills/toc-fixture/references/at-green-max.md", plainLines(A5_ROW.greenMax));
    // Exactly at amberMax lines -> AMBER (inclusive boundary).
    file(claudeHome, "skills/toc-fixture/references/at-amber-max.md", plainLines(A5_ROW.amberMax));
    // One line past amberMax -> RED.
    file(claudeHome, "skills/toc-fixture/references/past-amber-max.md", plainLines(A5_ROW.amberMax + 1));

    const projectPath = `${claudeHome}-unrelated-project`;
    const entries = buildShelf(projectPath, claudeHome);
    expect(entries).toHaveLength(3);
    const byName = new Map(entries.map((e) => [e.path.split("/").pop()!, e]));

    const atGreen = byName.get("at-green-max.md")!;
    const atAmber = byName.get("at-amber-max.md")!;
    const pastAmber = byName.get("past-amber-max.md")!;

    expect(atGreen.tocLines).toBe(A5_ROW.greenMax);
    expect(atGreen.tocBand).toBe("GREEN");
    expect(atAmber.tocLines).toBe(A5_ROW.amberMax);
    expect(atAmber.tocBand).toBe("AMBER");
    expect(pastAmber.tocLines).toBe(A5_ROW.amberMax + 1);
    expect(pastAmber.tocBand).toBe("RED");

    // The genuine-reuse proof: every entry's band is byte-identical to
    // calling rubric.ts's own bandFor directly on that entry's own reported
    // raw measurement — not a parallel/duplicated threshold implementation.
    for (const e of [atGreen, atAmber, pastAmber]) {
      expect(e.tocBand).toBe(bandFor(A5_ROW, e.tocLines));
    }
  });

  test("nesting-depth (A6) band matches rubric.ts's bandFor — binary GREEN/RED, no AMBER zone", () => {
    const claudeHome = tmp("ctx-scan-refs-a6-");
    file(claudeHome, "skills/nesting-fixture/SKILL.md", "# Nesting Fixture Skill\n");

    // Zero nested-reference links -> GREEN.
    file(claudeHome, "skills/nesting-fixture/references/no-nesting.md", "# No Nesting\n\nPlain content, no links at all.");
    // One markdown link to ANOTHER file under the same references/ dir -> RED
    // (A6's greenMax === amberMax === 0, so any measured >= 1 is RED).
    file(
      claudeHome,
      "skills/nesting-fixture/references/nests-once.md",
      "# Nests Once\n\nLinks to [another reference](sibling.md) under the same references/ dir.",
    );
    file(claudeHome, "skills/nesting-fixture/references/sibling.md", "# Sibling\n\nThe target of the nested link above.");

    const projectPath = `${claudeHome}-unrelated-project`;
    const entries = buildShelf(projectPath, claudeHome);
    const byName = new Map(entries.map((e) => [e.path.split("/").pop()!, e]));

    const noNesting = byName.get("no-nesting.md")!;
    const nestsOnce = byName.get("nests-once.md")!;

    expect(noNesting.nestingLinks).toBe(0);
    expect(noNesting.nestingBand).toBe("GREEN");
    expect(nestsOnce.nestingLinks).toBe(1);
    expect(nestsOnce.nestingBand).toBe("RED");

    for (const e of [noNesting, nestsOnce]) {
      expect(e.nestingBand).toBe(bandFor(A6_ROW, e.nestingLinks));
    }

    // Sanity: A6's published bands really are the binary shape this test
    // exploits (greenMax === amberMax === 0) — if a future rubric.ts change
    // ever gave A6 a real AMBER zone, this assertion (not a silent pass)
    // would be the signal to revisit this test's boundary fixtures.
    expect(A6_ROW.greenMax).toBe(0);
    expect(A6_ROW.amberMax).toBe(0);
  });
});
