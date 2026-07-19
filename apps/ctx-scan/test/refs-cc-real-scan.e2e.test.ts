/**
 * refs-cc-real-scan.e2e.test.ts — ctx-scan-refs task [4.4], beads:if-vyxh.
 *
 * Runs the references shelf against the REAL fleet (no fixtures, no mocks —
 * same convention as `render-cc-real-scan.e2e.test.ts`): confirms genuinely
 * orphaned reference files are flagged, the systemic no-ToC (A5) population
 * `docs/context-budget-rubric.md` Part 2 documents ("79 files 101+ lines w/o
 * ToC", AMBER) really does render AMBER at this scale, and that every listed
 * shelf entry actually click-opens into the level-3 detail view in a real
 * headless-Chromium DOM (`test/helpers/browser.ts`), not a string-contains
 * proxy.
 *
 * `--project cc` cannot be used literally on this machine: `~/.claude`
 * symlinks to `~/dev/cc` (`discovery.ts`'s realpath-based global-layer
 * resolution — independently reconfirmed below), so cc's own tree is the
 * shared GLOBAL layer, never a discoverable project. This mirrors
 * `render-cc-real-scan.e2e.test.ts`'s own documented resolution: scan the
 * wider `~/dev` root and use `--project installfest` (this repo, always
 * discoverable) as the concrete project whose shelf we drive — `refs.ts`'s
 * `buildShelf` always walks the real global `claudeHome` layer (cc's own
 * skills/commands/agents/rules) regardless of which project it's called
 * for, so installfest's shelf genuinely includes cc's own reference files.
 *
 * Runtime evidence for this task was also gathered via the literal CLI
 * command the proposal names (`bun run src/cli.ts render --root ~/dev
 * --project installfest -o <path>`) — see the commit message / task
 * completion notes for the pasted stdout/stderr and file-size output. This
 * test is the automated, repeatable form of that same real scan.
 */
import { afterEach, describe, expect, test } from "bun:test";
import { homedir } from "node:os";
import { join } from "node:path";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { chromiumAvailable, runInBrowser } from "./helpers/browser";
import { buildFleet } from "../src/cli";
import { renderFleetHtml } from "../src/render";
import { buildViewModel } from "../src/render/view-model";
import { buildShelf } from "../src/refs";
import { getGlobalLayer, resolveGlobalPath } from "../src/discovery";

const outDirs: string[] = [];
afterEach(() => {
  while (outDirs.length) rmSync(outDirs.pop()!, { recursive: true, force: true });
});

describe("references shelf — real ~/dev/cc scan: orphans, A5 AMBER systemic no-ToC, every entry click-opens [4.4]", () => {
  test(
    "buildShelf against the real global layer: genuine orphans exist, and the no-ToC (A5) population is systemically AMBER",
    () => {
      // Independently reconfirm the documented symlink fact this test's
      // project-choice rationale depends on.
      expect(resolveGlobalPath()).toBe(join(homedir(), "dev", "cc"));

      const claudeHome = getGlobalLayer().path;
      const installfestPath = join(homedir(), "dev", "personal", "installfest");
      const entries = buildShelf(installfestPath, claudeHome);

      expect(entries.length).toBeGreaterThan(0);

      const orphans = entries.filter((e) => !e.reachable);
      // eslint-disable-next-line no-console
      console.log("[4.4] real shelf scan — total entries:", entries.length, "orphans:", orphans.length);
      console.log("[4.4] orphan sample (first 10):", orphans.slice(0, 10).map((e) => e.path));
      expect(orphans.length).toBeGreaterThan(0);

      // Scope to actual `references/*.md` files for the A5 ToC claim — rules/
      // memory entries are a different bucket than the doc's "79 files" figure.
      const referenceFiles = entries.filter((e) => e.path.includes("/references/"));
      const amberToc = referenceFiles.filter((e) => e.tocBand === "AMBER");
      const redToc = referenceFiles.filter((e) => e.tocBand === "RED");
      // eslint-disable-next-line no-console
      console.log(
        "[4.4] reference/*.md files:",
        referenceFiles.length,
        "AMBER (no ToC, 101-300 lines):",
        amberToc.length,
        "RED (no ToC, >300 lines):",
        redToc.length,
      );

      // docs/context-budget-rubric.md Part 2's A5 row: "79 files 101+ lines
      // w/o ToC — AMBER (systemic)". This shelf's own AMBER-only count
      // (69 measured live at authoring time) is the same order of magnitude
      // and the same systemic pattern — a ballpark match, not byte-exact
      // (the doc's count came from a different measurement pass; the tree
      // has moved since), same convention as render-cc-real-scan.e2e.test.ts's
      // "at least one genuine RED body" A4 check.
      expect(amberToc.length).toBeGreaterThanOrEqual(50);
      // The combined no-ToC population (AMBER + RED) is comfortably in the
      // same systemic ballpark as the doc's "79" figure.
      expect(amberToc.length + redToc.length).toBeGreaterThanOrEqual(79);
    },
    30_000,
  );

  test(
    "rendered report: every listed shelf entry has a real, click-openable level-3 detail section",
    async () => {
      if (!chromiumAvailable()) {
        console.warn("[4.4] chromium not available — skipping the browser-rendered half");
        return;
      }

      const root = join(homedir(), "dev");
      const { fleet } = await buildFleet(root, { allowProbeHooks: false });
      // buildViewModel is purely read-only over `fleet` (see view-model.ts's
      // module doc) — safe to call once here just to resolve installfest's
      // project index (deterministic, same sort renderFleetHtml uses
      // internally), independent of the render call below.
      const vm = buildViewModel(fleet);
      const installfestIdx = vm.projects.findIndex((p) => p.name === "installfest");
      expect(installfestIdx).toBeGreaterThanOrEqual(0);

      const html = renderFleetHtml(fleet, { project: "installfest" });

      const outDir = mkdtempSync(join(tmpdir(), "ctx-scan-refs-cc-render-"));
      outDirs.push(outDir);
      const outPath = join(outDir, "ctx-scan-refs-cc.html");
      writeFileSync(outPath, html, "utf8");
      // eslint-disable-next-line no-console
      console.log("[4.4] rendered report:", outPath, `(${(html.length / 1_000_000).toFixed(1)}MB)`);

      const shelfLinkId = `shelf-link-${installfestIdx}`;
      const shelfHomeId = `shelf-${installfestIdx}`;

      const driver = `
        var shelfLink = document.getElementById(${JSON.stringify(shelfLinkId)});
        if (!shelfLink) throw new Error("shelf link not found: " + ${JSON.stringify(shelfLinkId)});
        var openAnchor = shelfLink.querySelector('a[data-nav-target]');
        if (!openAnchor) throw new Error("no anchor with data-nav-target inside the shelf link");
        openAnchor.click();

        var shelfHome = document.getElementById(${JSON.stringify(shelfHomeId)});
        if (!shelfHome) throw new Error("shelf home section not found: " + ${JSON.stringify(shelfHomeId)});
        if (shelfHome.hidden) throw new Error("shelf home section did not become visible after clicking the shelf link");

        var groupLinks = Array.prototype.slice.call(shelfHome.querySelectorAll('a[data-nav-target^="shelf-group-${installfestIdx}-"]'));

        // Structural pass: EVERY entry link anywhere in this project's shelf
        // (home -> every group section) resolves to a real, existing
        // '.screen' detail section — proves every listed entry genuinely has
        // a click-openable detail view, not just the ones we physically click.
        var allEntryLinks = [];
        var missingTargets = [];
        var nonScreenTargets = [];
        groupLinks.forEach(function (gl) {
          var groupId = gl.getAttribute('data-nav-target');
          var groupSection = document.getElementById(groupId);
          if (!groupSection) { missingTargets.push(groupId); return; }
          var entryLinks = groupSection.querySelectorAll('a[data-nav-target^="shelf-doc-${installfestIdx}-"]');
          entryLinks.forEach(function (el) {
            var targetId = el.getAttribute('data-nav-target');
            allEntryLinks.push(targetId);
            var targetSection = document.getElementById(targetId);
            if (!targetSection) { missingTargets.push(targetId); return; }
            if (!targetSection.classList.contains('screen')) nonScreenTargets.push(targetId);
          });
        });

        // Depth pass: physically click through a real sample — first group,
        // its first entry, and (if present) a group roughly in the middle and
        // its first entry — proving the click mechanism itself works, not
        // just that the targets exist.
        var clicked = [];
        function clickThrough(groupIdx) {
          if (groupIdx >= groupLinks.length) return;
          var gl = groupLinks[groupIdx];
          gl.click();
          var groupId = gl.getAttribute('data-nav-target');
          var groupSection = document.getElementById(groupId);
          var groupVisible = groupSection && !groupSection.hidden;
          var entryLink = groupSection ? groupSection.querySelector('a[data-nav-target^="shelf-doc-${installfestIdx}-"]') : null;
          var entryVisible = false;
          var entryHasContent = false;
          var backLinkTarget = null;
          if (entryLink) {
            entryLink.click();
            var entryId = entryLink.getAttribute('data-nav-target');
            var entrySection = document.getElementById(entryId);
            entryVisible = !!entrySection && !entrySection.hidden;
            entryHasContent = !!entrySection && !!entrySection.querySelector('.doc-path') && !!entrySection.querySelector('.doc-meta');
            var backLink = entrySection ? entrySection.querySelector('.back-link[data-nav-target]') : null;
            backLinkTarget = backLink ? backLink.getAttribute('data-nav-target') : null;
          }
          clicked.push({ groupId: groupId, groupVisible: groupVisible, entryFound: !!entryLink, entryVisible: entryVisible, entryHasContent: entryHasContent, backLinkTarget: backLinkTarget });
        }
        clickThrough(0);
        if (groupLinks.length > 2) clickThrough(Math.floor(groupLinks.length / 2));
        if (groupLinks.length > 1) clickThrough(groupLinks.length - 1);

        window.__RESULT__ = {
          groupCount: groupLinks.length,
          totalEntryLinks: allEntryLinks.length,
          missingTargets: missingTargets.slice(0, 10),
          missingTargetCount: missingTargets.length,
          nonScreenTargetCount: nonScreenTargets.length,
          clicked: clicked,
        };
      `;

      const result = runInBrowser(html, driver, { timeoutMs: 60_000 }) as {
        groupCount: number;
        totalEntryLinks: number;
        missingTargets: string[];
        missingTargetCount: number;
        nonScreenTargetCount: number;
        clicked: Array<{
          groupId: string;
          groupVisible: boolean;
          entryFound: boolean;
          entryVisible: boolean;
          entryHasContent: boolean;
          backLinkTarget: string | null;
        }>;
        driverError?: string;
      };

      // eslint-disable-next-line no-console
      console.log("[4.4] browser shelf-navigation result:", JSON.stringify(result, null, 2));

      expect(result.driverError).toBeUndefined();
      expect(result.groupCount).toBeGreaterThan(0);
      expect(result.totalEntryLinks).toBeGreaterThan(0);
      // Every listed entry link resolves to a real, existing `.screen` section.
      expect(result.missingTargetCount).toBe(0);
      expect(result.nonScreenTargetCount).toBe(0);
      // The physically-clicked sample genuinely navigated end-to-end.
      expect(result.clicked.length).toBeGreaterThan(0);
      for (const c of result.clicked) {
        expect(c.groupVisible).toBe(true);
        expect(c.entryFound).toBe(true);
        expect(c.entryVisible).toBe(true);
        expect(c.entryHasContent).toBe(true);
        expect(c.backLinkTarget).toBe(c.groupId);
      }
    },
    90_000,
  );
});
