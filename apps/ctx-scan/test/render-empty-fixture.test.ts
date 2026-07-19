/**
 * render-empty-fixture.test.ts — ctx-scan-render task [4.6], beads:if-yv0e.
 *
 * Minimal empty-findings fixture (a tiny project with zero rubric
 * violations); asserts the renderer produces valid output with no error, at
 * every level (0 through 3), plus the fully-empty (zero projects, zero
 * global surfaces) edge case that hits level0's own "No projects discovered"
 * branch.
 */
import { afterEach, describe, expect, test } from "bun:test";
import { cleanup, tmpRoot } from "./helpers/tree";
import { makeFleet, makeNode, plainFile, project, surface } from "./fixtures/render/build";
import { annotateFleetBands } from "../src/rubric";
import { renderFleetHtml } from "../src/render";

const roots: string[] = [];
afterEach(() => {
  while (roots.length) cleanup(roots.pop()!);
});
function tmp(prefix: string): string {
  const root = tmpRoot(prefix);
  roots.push(root);
  return root;
}

describe("ctx-scan render — empty-findings fixture [4.6]", () => {
  test("a tiny project with zero rubric violations renders valid, error-free output at every level", () => {
    const root = tmp("ctx-scan-empty-");
    // A well-within-budget memory node (A9: greenMax=20480 bytes, 50 lines <=160 GREEN) —
    // small enough that no rubric row this project touches ever goes non-GREEN.
    const memoryPath = plainFile(root, "MEMORY.md", 5);
    const memNode = makeNode({ path: memoryPath, cls: "memory", raw_chars: 120, est_tokens: 30 });

    const fleet = makeFleet([], [project("tiny", root, [surface("memory", [memNode])])]);

    expect(() => annotateFleetBands(fleet)).not.toThrow();
    expect(memNode.bands).toEqual([{ rule: "A9", band: "GREEN", measured: 120, limit: 25_600 }]);

    let html = "";
    expect(() => {
      html = renderFleetHtml(fleet, { project: "tiny" });
    }).not.toThrow();

    expect(html).toContain("<!doctype html>");
    expect(html).toContain('data-initial-screen="level1-0"');

    // Level 0: fleet leaderboard with the one project (not the "No projects" branch).
    expect(html).toContain('id="level0"');
    expect(html).not.toContain("No projects discovered");
    expect(html).toContain(">tiny<");

    // Level 1: project screen with the memory class segment, no trim-plan rows.
    expect(html).toContain('id="level1-0" class="screen"');
    expect(html).toContain("Memory (MEMORY.md)");
    expect(html).toContain("No RED/AMBER rubric rows for this project — nothing to trim.");

    // Level 2: the memory class's document bar with exactly one (GREEN/none) segment.
    expect(html).toContain('id="level2-0-0" class="screen" hidden');
    expect(html).not.toContain("No documents in this class.");

    // Level 3: the document detail screen, with the "no violations" message and no truncations.
    expect(html).toContain('id="level3-0-0-0" class="screen" hidden');
    expect(html).toContain("No rubric violations.");
    expect(html).toContain("No truncation applied.");
  });

  test("a fully-empty fleet (zero projects, zero global surfaces) still renders valid output with no error", () => {
    const fleet = makeFleet([], []);
    expect(() => annotateFleetBands(fleet)).not.toThrow();

    let html = "";
    expect(() => {
      html = renderFleetHtml(fleet, { fleet: true });
    }).not.toThrow();

    expect(html).toContain("<!doctype html>");
    expect(html).toContain('id="level0"');
    expect(html).toContain("No projects discovered under the scanned root.");
    // No project sections at all — the projectSections join produces an empty string.
    expect(html).not.toContain('id="level1-0"');
    expect(html).not.toContain('id="level2-0-0"');
    expect(html).not.toContain('id="level3-0-0-0"');
  });

  test("--project pointing at a name absent from an otherwise-nonempty fleet degrades to the fleet view with a stderr warning, no throw", () => {
    const root = tmp("ctx-scan-empty-noproj-");
    const memoryPath = plainFile(root, "MEMORY.md", 3);
    const memNode = makeNode({ path: memoryPath, cls: "memory", raw_chars: 60, est_tokens: 15 });
    const fleet = makeFleet([], [project("tiny", root, [surface("memory", [memNode])])]);
    annotateFleetBands(fleet);

    let html = "";
    expect(() => {
      html = renderFleetHtml(fleet, { project: "does-not-exist" });
    }).not.toThrow();
    expect(html).toContain('data-initial-screen="level0"'); // falls back to the fleet view, not a crash
  });
});
