/**
 * render-band-colors.test.ts — ctx-scan-render task [4.3], beads:if-kj1x.
 *
 * Fixture with known RED/AMBER/GREEN nodes; asserts each renders with the
 * correct band color — verified via a REAL browser's computed style
 * (`getComputedStyle(...).borderColor`/`.backgroundColor`), not merely the
 * presence of a `band-red`/`band-amber`/`band-green` CSS class string, so
 * this genuinely proves the CSS in `render.ts`'s `SHARED_CSS` actually paints
 * the color it claims to (see `test/helpers/browser.ts` for why a real
 * headless-Chromium DOM/CSS engine is used instead of a string check).
 */
import { afterEach, describe, expect, test } from "bun:test";
import { cleanup, tmpRoot } from "./helpers/tree";
import { chromiumAvailable, runInBrowser } from "./helpers/browser";
import { makeFleet, makeNode, project, surface } from "./fixtures/render/build";
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

// Same CSS custom-property values as render.ts's SHARED_CSS `:root` block —
// asserted against directly (not re-derived) so this test fails loudly if
// either side ever drifts.
const BAND_RGB = {
  GREEN: "rgb(22, 163, 74)", // #16a34a
  AMBER: "rgb(217, 119, 6)", // #d97706
  RED: "rgb(220, 38, 38)", // #dc2626
};

describe("ctx-scan render — band coloring [4.3]", () => {
  test("known RED/AMBER/GREEN mcp-tools nodes render with the correct computed border color", () => {
    if (!chromiumAvailable()) {
      console.warn("[4.3] chromium not available — skipping");
      return;
    }
    const root = tmp("ctx-scan-bandcolor-");

    // mcp-tools' A13 measurer reads node.raw_chars directly (no file re-read
    // needed) — greenMax=1600, amberMax=2048 (rubric.ts TABLE_A). Chosen
    // values land cleanly in each band with margin.
    const greenNode = makeNode({ path: `${root}/green.md`, cls: "mcp-tools", raw_chars: 1000, est_tokens: 250 }); // <=1600 GREEN
    const amberNode = makeNode({ path: `${root}/amber.md`, cls: "mcp-tools", raw_chars: 1800, est_tokens: 450 }); // 1601-2048 AMBER
    const redNode = makeNode({ path: `${root}/red.md`, cls: "mcp-tools", raw_chars: 3000, est_tokens: 750 }); // >2048 RED

    const fleet = makeFleet(
      [],
      [project("proj", root, [surface("mcp-tools", [greenNode, amberNode, redNode])])],
    );
    annotateFleetBands(fleet);

    // Sanity: confirm the rubric actually assigned the bands we're relying on
    // before ever touching the browser — isolates a CSS-mapping bug from a
    // rubric-computation bug should this ever fail.
    expect(greenNode.bands).toEqual([{ rule: "A13", band: "GREEN", measured: 1000, limit: 2048 }]);
    expect(amberNode.bands).toEqual([{ rule: "A13", band: "AMBER", measured: 1800, limit: 2048 }]);
    expect(redNode.bands).toEqual([{ rule: "A13", band: "RED", measured: 3000, limit: 2048 }]);

    const html = renderFleetHtml(fleet, { project: "proj" });

    const driver = `
      // Drill into the mcp-tools class (level2) the same way a user would:
      // click the level1 class segment, then read each doc-bar-seg's own
      // computed border-color (band coloring lives on the border, per
      // render.ts SHARED_CSS's .band-* rules).
      var level1 = document.getElementById('level1-0');
      var classSeg = level1.querySelector('.class-seg-group[data-nav-target]');
      classSeg.click();
      var level2Id = classSeg.getAttribute('data-nav-target');
      var level2 = document.getElementById(level2Id);
      var segs = level2.querySelectorAll('.doc-bar-seg');
      var colors = [];
      segs.forEach(function (seg) {
        var cs = getComputedStyle(seg);
        colors.push({ title: seg.getAttribute('title'), borderColor: cs.borderTopColor, cssClass: seg.className });
      });
      // Also check the level-2 list entries (left-border band coloring).
      var listItems = level2.querySelectorAll('.doc-list a');
      var listColors = [];
      listItems.forEach(function (a) {
        var cs = getComputedStyle(a);
        listColors.push({ text: a.textContent, borderLeftColor: cs.borderLeftColor });
      });
      window.__RESULT__ = { level2Id: level2Id, segColors: colors, listColors: listColors };
    `;

    const result = runInBrowser(html, driver) as {
      level2Id: string;
      segColors: Array<{ title: string; borderColor: string; cssClass: string }>;
      listColors: Array<{ text: string; borderLeftColor: string }>;
      driverError?: string;
    };

    expect(result.driverError).toBeUndefined();
    expect(result.segColors.length).toBe(3);

    const byTitlePrefix = (title: string) => result.segColors.find((c) => c.title.startsWith(title));
    const green = byTitlePrefix("green.md");
    const amber = byTitlePrefix("amber.md");
    const red = byTitlePrefix("red.md");
    expect(green).toBeDefined();
    expect(amber).toBeDefined();
    expect(red).toBeDefined();

    expect(green!.borderColor).toBe(BAND_RGB.GREEN);
    expect(amber!.borderColor).toBe(BAND_RGB.AMBER);
    expect(red!.borderColor).toBe(BAND_RGB.RED);
    expect(green!.cssClass).toContain("band-green");
    expect(amber!.cssClass).toContain("band-amber");
    expect(red!.cssClass).toContain("band-red");

    // Level-2 list entries carry the same band via their left border.
    const greenLi = result.listColors.find((c) => c.text.startsWith("green.md"));
    const amberLi = result.listColors.find((c) => c.text.startsWith("amber.md"));
    const redLi = result.listColors.find((c) => c.text.startsWith("red.md"));
    expect(greenLi!.borderLeftColor).toBe(BAND_RGB.GREEN);
    expect(amberLi!.borderLeftColor).toBe(BAND_RGB.AMBER);
    expect(redLi!.borderLeftColor).toBe(BAND_RGB.RED);
  }, 30_000);

  test("a node with no applicable rubric row renders band-none (no color claim)", () => {
    if (!chromiumAvailable()) {
      console.warn("[4.3] chromium not available — skipping");
      return;
    }
    const root = tmp("ctx-scan-bandnone-");
    // system-prompt has zero applicable Table A nodeClasses rows (see
    // node-band-annotation.test.ts's own equivalent case) -> bands: [].
    const node = makeNode({ path: `${root}/x.md`, cls: "system-prompt", raw_chars: 500, est_tokens: 125 });
    const fleet = makeFleet([], [project("proj", root, [surface("system-prompt", [node])])]);
    annotateFleetBands(fleet);
    expect(node.bands).toEqual([]);

    const html = renderFleetHtml(fleet, { project: "proj" });
    const driver = `
      var level1 = document.getElementById('level1-0');
      var classSeg = level1.querySelector('.class-seg-group[data-nav-target]');
      classSeg.click();
      var level2 = document.getElementById(classSeg.getAttribute('data-nav-target'));
      var seg = level2.querySelector('.doc-bar-seg');
      window.__RESULT__ = { borderColor: getComputedStyle(seg).borderTopColor, cssClass: seg.className };
    `;
    const result = runInBrowser(html, driver) as { borderColor: string; cssClass: string; driverError?: string };
    expect(result.driverError).toBeUndefined();
    expect(result.cssClass).toContain("band-none");
    expect(result.borderColor).toBe("rgb(148, 163, 184)"); // --band-none: #94a3b8
  }, 30_000);
});
