/**
 * render-drilldown.test.ts — ctx-scan-render task [4.2], beads:if-j112.
 *
 * Fixture data exercising all 4 levels; asserts each level's DOM structure
 * matches the expected shape AND that clicking through levels 0->1->2->3
 * (and back) actually works — driven in a real headless-Chromium DOM/CSS
 * engine (see `test/helpers/browser.ts`'s module doc for why this over a new
 * `playwright` dependency), not a string-contains proxy.
 */
import { afterEach, describe, expect, test } from "bun:test";
import { cleanup, file, tmpRoot } from "./helpers/tree";
import { chromiumAvailable, runInBrowser } from "./helpers/browser";
import { makeFleet, makeNode, project, surface } from "./fixtures/render/build";
import { annotateFleetBands } from "../src/rubric";
import { renderFleetHtml } from "../src/render";
import type { Fleet } from "../src/model";

const roots: string[] = [];
afterEach(() => {
  while (roots.length) cleanup(roots.pop()!);
});
function tmp(prefix: string): string {
  const root = tmpRoot(prefix);
  roots.push(root);
  return root;
}

/**
 * Two projects ("alpha" gets more T1 tokens so it leaderboards first),
 * "alpha" carrying two classes (skills-listing with 2 documents,
 * claude-md-chain with 1) so level 1's stacked bar has >1 segment and level
 * 2 has >1 document to click between.
 */
function buildFixture(root: string): Fleet {
  const claudeMdPath = `${root}/CLAUDE.md`;
  file(root, "CLAUDE.md", "# Alpha project rules\nSome real chain content the level-3 view should show.");

  const skillAPath = `${root}/skill-a.md`;
  file(
    root,
    "skill-a.md",
    ["---", "name: skill-a", 'description: "First fixture skill, plain GREEN content"', "---", "body a"].join("\n"),
  );
  const skillBPath = `${root}/skill-b.md`;
  file(
    root,
    "skill-b.md",
    ["---", "name: skill-b", 'description: "Second fixture skill, plain GREEN content"', "---", "body b"].join("\n"),
  );

  const chainNode = makeNode({ path: claudeMdPath, cls: "claude-md-chain", tier: 1, raw_chars: 80, est_tokens: 20 });
  const skillANode = makeNode({ path: skillAPath, cls: "skills-listing", raw_chars: 900, est_tokens: 220 });
  const skillBNode = makeNode({ path: skillBPath, cls: "skills-listing", raw_chars: 700, est_tokens: 180 });

  const alpha = project("alpha", root, [
    surface("claude-md-chain", [chainNode]),
    surface("skills-listing", [skillANode, skillBNode]),
  ]);

  const betaMemoryPath = `${root}/beta-MEMORY.md`;
  file(root, "beta-MEMORY.md", "beta memory line 1\nbeta memory line 2");
  const betaNode = makeNode({ path: betaMemoryPath, cls: "memory", raw_chars: 40, est_tokens: 10 });
  const beta = project("beta", `${root}/beta`, [surface("memory", [betaNode])]);

  const fleet = makeFleet([], [alpha, beta]);
  annotateFleetBands(fleet);
  return fleet;
}

describe("ctx-scan render — 4-level drill-down DOM structure + navigation [4.2]", () => {
  test("static structure: every level's screen + nav targets exist with the expected shape", () => {
    const root = tmp("ctx-scan-drilldown-");
    const fleet = buildFixture(root);
    const html = renderFleetHtml(fleet, { fleet: true });

    // Level 0: one fleet-row per project, each with a data-nav-target into level1.
    expect(html).toContain('id="level0"');
    expect(html).toContain("alpha");
    expect(html).toContain("beta");
    const fleetBarTargets = [...html.matchAll(/class="fleet-bar" data-nav-target="(level1-\d+)"/g)].map((m) => m[1]);
    expect(fleetBarTargets.length).toBe(2);

    // Level 1: both projects get a `.screen` section; alpha's has 2 class segments
    // (claude-md-chain + skills-listing) each with a data-nav-target into level2.
    for (const target of fleetBarTargets) {
      expect(html).toContain(`id="${target}" class="screen" hidden`);
    }
    const alphaProjIdx = fleetBarTargets.findIndex((t) => {
      const re = new RegExp(`id="${t}"[\\s\\S]{0,400}?<h2>alpha</h2>`);
      return re.test(html);
    });
    expect(alphaProjIdx).toBeGreaterThanOrEqual(0);
    const alphaLevel1Id = fleetBarTargets[alphaProjIdx]!;
    const alphaProjNum = alphaLevel1Id.replace("level1-", "");
    const level2TargetsForAlpha = [
      ...html.matchAll(new RegExp(`data-nav-target="(level2-${alphaProjNum}-\\d+)"`, "g")),
    ].map((m) => m[1]);
    // 2 classes -> 2 distinct level2 ids reachable from alpha's level1 screen (class-seg-group + legend both cite them, so dedupe).
    expect(new Set(level2TargetsForAlpha).size).toBe(2);

    // Level 2: each class's screen exists and contains doc-bar-seg entries with data-nav-target into level3.
    for (const l2 of new Set(level2TargetsForAlpha)) {
      expect(html).toContain(`id="${l2}" class="screen" hidden`);
    }
    const skillsListingL2 = [...level2TargetsForAlpha].find((t) => {
      const re = new RegExp(`id="${t}"[\\s\\S]{0,200}?<h2>Skills Listing</h2>`);
      return re.test(html);
    });
    expect(skillsListingL2).toBeDefined();
    // 3 documents total under alpha (1 claude-md-chain + 2 skills-listing) -> 3 distinct level3 screens.
    const allLevel3ForAlpha = new Set(
      [...html.matchAll(new RegExp(`(level3-${alphaProjNum}-\\d+-\\d+)`, "g"))].map((m) => m[1]),
    );
    expect(allLevel3ForAlpha.size).toBe(3);

    // Level 3: each level3 screen actually exists as a `.screen` section.
    for (const l3 of allLevel3ForAlpha) {
      expect(html).toContain(`id="${l3}" class="screen" hidden`);
    }
  });

  test(
    "interactive: clicking through level0 -> level1 -> level2 -> level3 -> back chain actually navigates (real browser)",
    () => {
      if (!chromiumAvailable()) {
        console.warn("[4.2] chromium not available — skipping the interactive half");
        return;
      }
      const root = tmp("ctx-scan-drilldown-nav-");
      const fleet = buildFixture(root);
      const html = renderFleetHtml(fleet, { fleet: true });

      const driver = `
        function screenState() {
          var out = {};
          document.querySelectorAll('.screen').forEach(function (s) { out[s.id] = s.hidden; });
          return out;
        }
        var steps = [];
        steps.push({ step: 'initial', screens: screenState() });

        // Level 0 -> level 1: click the first fleet bar.
        var fleetBar = document.querySelector('.fleet-bar[data-nav-target]');
        var level1Id = fleetBar.getAttribute('data-nav-target');
        fleetBar.click();
        steps.push({ step: 'after-click-level1', target: level1Id, screens: screenState() });

        // Level 1 -> level 2: click the first class segment group within that project's screen.
        var level1Section = document.getElementById(level1Id);
        var classSeg = level1Section.querySelector('.class-seg-group[data-nav-target]');
        var level2Id = classSeg.getAttribute('data-nav-target');
        classSeg.click();
        steps.push({ step: 'after-click-level2', target: level2Id, screens: screenState() });

        // Level 2 -> level 3: click the first document segment.
        var level2Section = document.getElementById(level2Id);
        var docSeg = level2Section.querySelector('.doc-bar-seg[data-nav-target]');
        var level3Id = docSeg.getAttribute('data-nav-target');
        docSeg.click();
        steps.push({ step: 'after-click-level3', target: level3Id, screens: screenState() });

        // Level 3 heading + doc-path text, to confirm the RIGHT document rendered.
        var level3Section = document.getElementById(level3Id);
        var heading = level3Section.querySelector('h3').textContent;
        var docPath = level3Section.querySelector('.doc-path code').textContent;

        // Back-chain: level3 -> level2 -> level1 -> level0.
        level3Section.querySelector('.back-link').click();
        var afterBackToL2 = screenState();
        document.getElementById(level2Id).querySelector('.back-link').click();
        var afterBackToL1 = screenState();
        document.getElementById(level1Id).querySelector('.back-link').click();
        var afterBackToL0 = screenState();

        window.__RESULT__ = {
          steps: steps,
          level3Heading: heading,
          level3DocPath: docPath,
          afterBackToL2: afterBackToL2,
          afterBackToL1: afterBackToL1,
          afterBackToL0: afterBackToL0,
        };
      `;

      const result = runInBrowser(html, driver) as {
        steps: Array<{ step: string; target?: string; screens: Record<string, boolean> }>;
        level3Heading: string;
        level3DocPath: string;
        afterBackToL2: Record<string, boolean>;
        afterBackToL1: Record<string, boolean>;
        afterBackToL0: Record<string, boolean>;
        driverError?: string;
      };

      expect(result.driverError).toBeUndefined();

      const [initial, afterL1, afterL2, afterL3] = result.steps;
      // Initial: level0 visible, everything else hidden.
      expect(initial!.screens["level0"]).toBe(false);
      expect(Object.entries(initial!.screens).filter(([id, hidden]) => id !== "level0" && !hidden)).toEqual([]);

      // After clicking into level1: only that level1 screen is visible.
      expect(afterL1!.screens["level0"]).toBe(true);
      expect(afterL1!.screens[afterL1!.target!]).toBe(false);
      expect(Object.entries(afterL1!.screens).filter(([, hidden]) => !hidden).length).toBe(1);

      // After clicking into level2: only that level2 screen is visible.
      expect(afterL2!.screens[afterL1!.target!]).toBe(true);
      expect(afterL2!.screens[afterL2!.target!]).toBe(false);
      expect(Object.entries(afterL2!.screens).filter(([, hidden]) => !hidden).length).toBe(1);

      // After clicking into level3: only that level3 screen is visible.
      expect(afterL3!.screens[afterL2!.target!]).toBe(true);
      expect(afterL3!.screens[afterL3!.target!]).toBe(false);
      expect(Object.entries(afterL3!.screens).filter(([, hidden]) => !hidden).length).toBe(1);

      // The level3 screen rendered the actual document (name + real path), not a placeholder.
      expect(result.level3Heading.length).toBeGreaterThan(0);
      expect(result.level3DocPath).toContain(root);

      // Back-chain actually rewinds one level at a time.
      expect(result.afterBackToL2[afterL2!.target!]).toBe(false);
      expect(result.afterBackToL1[afterL1!.target!]).toBe(false);
      expect(result.afterBackToL0["level0"]).toBe(false);
    },
    30_000,
  );
});
