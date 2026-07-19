/**
 * fleet-sparkline.test.ts — `ctx-scan-watch` task [4.5], beads:if-l1ej.
 *
 * A multi-snapshot (3+) fixture history for one project, with varying
 * audit-severity bands across snapshots (all-GREEN -> one-AMBER ->
 * one-RED) — asserts `ctx-scan render --fleet`'s leaderboard renders the
 * per-project drift sparkline (`render/level0-fleet.ts`'s task [3.2])
 * without error in a REAL browser DOM/SVG engine, and that the rendered
 * sparkline reflects the actual snapshot count (same convention as
 * `render-band-colors.test.ts`: a real headless-Chromium DOM, not a
 * string-contains proxy).
 */
import { afterEach, describe, expect, test } from "bun:test";
import { join } from "node:path";
import { cleanup, tmpRoot } from "./helpers/tree";
import { chromiumAvailable, runInBrowser } from "./helpers/browser";
import { makeFleet, makeNode, project, surface } from "./fixtures/render/build";
import { makeSnapshot, writeSnapshots } from "./fixtures/watch/build";
import { buildViewModel } from "../src/render/view-model";
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

describe("fleet leaderboard — drift sparkline [4.5]", () => {
  test("a 3-snapshot fixture history renders a sparkline reflecting the snapshot count, without error", () => {
    if (!chromiumAvailable()) {
      console.warn("[4.5] chromium not available — skipping");
      return;
    }

    const root = tmp("ctx-scan-sparkline-");
    const projectPath = join(root, "proj-a");
    const node = makeNode({ path: `${projectPath}/CLAUDE.md`, cls: "claude-md-chain", raw_chars: 500, est_tokens: 125 });
    const fleet = makeFleet([], [project("proj-a", projectPath, [surface("claude-md-chain", [node])])]);

    // Confirms the fixture project is genuinely the fleet's only bar before
    // trusting index-0 lookups in the browser driver below.
    const vm = buildViewModel(fleet);
    expect(vm.fleet.bars.length).toBe(1);
    expect(vm.fleet.bars[0]!.projectPath).toBe(projectPath);

    const historyPath = join(root, "history.jsonl");
    writeSnapshots(historyPath, [
      makeSnapshot(projectPath, "2026-03-01T00:00:00.000Z", { A1: "GREEN", A2: "GREEN" }),
      makeSnapshot(projectPath, "2026-03-01T01:00:00.000Z", { A1: "AMBER", A2: "GREEN" }),
      makeSnapshot(projectPath, "2026-03-01T02:00:00.000Z", { A1: "RED", A2: "GREEN" }),
    ]);

    const html = renderFleetHtml(fleet, { historyFilePath: historyPath });

    const driver = `
      var row = document.querySelectorAll('.fleet-row')[0];
      if (!row) throw new Error("no .fleet-row rendered");
      var sparklineWrap = row.querySelector('.fleet-row-sparkline');
      var svg = sparklineWrap ? sparklineWrap.querySelector('svg.fleet-sparkline') : null;
      var polyline = svg ? svg.querySelector('polyline') : null;
      var circle = svg ? svg.querySelector('circle') : null;
      window.__RESULT__ = {
        wrapTitle: sparklineWrap ? sparklineWrap.getAttribute('title') : null,
        svgPresent: !!svg,
        ariaLabel: svg ? svg.getAttribute('aria-label') : null,
        pointsAttr: polyline ? polyline.getAttribute('points') : null,
        pointCount: polyline ? polyline.getAttribute('points').trim().split(/\\s+/).length : 0,
        circlePresent: !!circle,
        circleFill: circle ? circle.getAttribute('fill') : null,
      };
    `;

    const result = runInBrowser(html, driver) as {
      wrapTitle: string | null;
      svgPresent: boolean;
      ariaLabel: string | null;
      pointsAttr: string | null;
      pointCount: number;
      circlePresent: boolean;
      circleFill: string | null;
      driverError?: string;
    };

    expect(result.driverError).toBeUndefined();
    expect(result.svgPresent).toBe(true);
    // Reflects the real snapshot count: 3 points (one per snapshot) and the
    // wrap's own title/aria-label both name "3" snapshots.
    expect(result.pointCount).toBe(3);
    expect(result.wrapTitle).toContain("3 snapshot(s) recorded");
    expect(result.ariaLabel).toContain("trend over 3 snapshots");
    // Most recent snapshot (A1: RED) is the worst-severity point plotted —
    // the sparkline's last-point marker renders RED's color.
    expect(result.circlePresent).toBe(true);
    expect(result.circleFill).toBe("#dc2626");
  });

  test("fewer than two snapshots renders no sparkline (documented threshold)", () => {
    if (!chromiumAvailable()) {
      console.warn("[4.5] chromium not available — skipping");
      return;
    }
    const root = tmp("ctx-scan-sparkline-single-");
    const projectPath = join(root, "proj-b");
    const node = makeNode({ path: `${projectPath}/CLAUDE.md`, cls: "claude-md-chain", raw_chars: 500, est_tokens: 125 });
    const fleet = makeFleet([], [project("proj-b", projectPath, [surface("claude-md-chain", [node])])]);

    const historyPath = join(root, "history.jsonl");
    writeSnapshots(historyPath, [makeSnapshot(projectPath, "2026-03-02T00:00:00.000Z", { A1: "GREEN" })]);

    const html = renderFleetHtml(fleet, { historyFilePath: historyPath });
    const driver = `
      var row = document.querySelectorAll('.fleet-row')[0];
      var svg = row ? row.querySelector('svg.fleet-sparkline') : null;
      window.__RESULT__ = { svgPresent: !!svg };
    `;
    const result = runInBrowser(html, driver) as { svgPresent: boolean; driverError?: string };
    expect(result.driverError).toBeUndefined();
    expect(result.svgPresent).toBe(false);
  });
});
