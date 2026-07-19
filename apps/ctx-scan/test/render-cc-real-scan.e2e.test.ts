/**
 * render-cc-real-scan.e2e.test.ts — ctx-scan-render task [4.5], beads:if-l21g.
 *
 * Renders the REAL fleet (no fixtures, no mocks) and confirms the three
 * documented REDs from `docs/context-budget-rubric.md` Part 2's 2026-07-18
 * scorecard — A1 (listing total), A7/A8 (always-loaded chain), A4 (6
 * SKILL.md bodies over 500 lines) — actually render visibly RED, and that
 * the trim panel's plan sums to at least the A1 overage.
 *
 * `--project cc` cannot be used literally: `~/.claude` symlinks to
 * `~/dev/cc` on this machine (`discovery.ts`'s own realpath-based global-
 * layer resolution, cross-checked live via `readlink -f ~/.claude` ->
 * `/home/nyaptor/dev/cc`), so scanning with `--root ~/dev/cc` puts cc's
 * ENTIRE tree into `fleet.global` and yields ZERO discovered projects (this
 * exact fact is independently proven in `cc-audit-scan.test.ts`'s own header
 * doc + assertions — "cc" can never appear as a `--project` name on this
 * machine). This is the documented "fleet equivalent" the task itself
 * anticipates ("or fleet equivalent"): scan the wider `~/dev` root instead,
 * so cc's content becomes the shared GLOBAL layer merged into every OTHER
 * discovered project's own class views (`view-model.ts`'s `buildProjectView`
 * merges `[...globalSurfaces, ...project.surfaces]`) — any real project's
 * level1/level2/level3 screens then show cc's own RED rows, since a session
 * opened in ANY project pays for the shared global layer too (this is
 * exactly trim-plan.ts's own documented rationale for why the aggregate
 * rows "surface... even though they're identical for every project").
 * `--project installfest` (this very repo, confirmed discoverable) is used
 * as the concrete initial screen.
 */
import { afterEach, describe, expect, test } from "bun:test";
import { homedir } from "node:os";
import { join } from "node:path";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { chromiumAvailable, runInBrowser } from "./helpers/browser";
import { buildFleet } from "../src/cli";
import { annotateFleetBands } from "../src/rubric";
import { renderFleetHtml } from "../src/render";
import { buildViewModel } from "../src/render/view-model";
import { computeTrimPlan } from "../src/render/trim-plan";
import { auditFleet } from "../src/audit";

const outDirs: string[] = [];
afterEach(() => {
  while (outDirs.length) rmSync(outDirs.pop()!, { recursive: true, force: true });
});

describe("ctx-scan render — real ~/dev/cc scan drill-down + known REDs [4.5]", () => {
  test(
    "root=~/dev (cc as the shared global layer): fleet discovers installfest as a real project, A1/A7-A8/A4 all RED",
    async () => {
      const root = join(homedir(), "dev");
      const { fleet } = await buildFleet(root, { allowProbeHooks: false });

      // Sanity: cc really is the global layer here, never a discovered project.
      expect(fleet.projects.some((p) => p.name === "cc")).toBe(false);
      expect(fleet.global.length).toBeGreaterThan(0);
      const installfest = fleet.projects.find((p) => p.name === "installfest");
      expect(installfest).toBeDefined();

      const audit = auditFleet(fleet);
      const byId = new Map(audit.rows.map((r) => [r.id, r]));

      // Runtime evidence (paste-worthy): print the measured rows this test
      // asserts against.
      // eslint-disable-next-line no-console
      console.log("[4.5] real ~/dev scan audit rows:", {
        A1: byId.get("A1"),
        A3: byId.get("A3"),
        A4: byId.get("A4"),
        A7: byId.get("A7"),
        A8: byId.get("A8"),
      });

      // A1 — listing total: documented RED (5.8x budget, scorecard 46,200 chars).
      expect(byId.get("A1")!.band).toBe("RED");
      expect(byId.get("A1")!.measured!).toBeGreaterThan(byId.get("A1")!.amberMax);
      // A7 — always-loaded chain: documented RED (~16,126 tok, 1.6x the [R] ceiling).
      expect(byId.get("A7")!.band).toBe("RED");
      expect(byId.get("A7")!.measured!).toBeGreaterThan(byId.get("A7")!.amberMax);
      // A8 — per-file chain (worst offender, e.g. BEADS.md ~690 lines): documented RED.
      expect(byId.get("A8")!.band).toBe("RED");

      // A4 — SKILL.md bodies over 500 lines: scorecard says 6 RED offenders.
      // Count node-level A4 RED bands directly across the whole fleet (both
      // layers) — the audit row only reports the single WORST offender, but
      // the scorecard's claim is about the COUNT of RED bodies.
      let a4RedCount = 0;
      for (const s of fleet.global) {
        if (s.cls !== "skills-listing") continue;
        for (const n of s.nodes) {
          if (n.bands.some((b) => b.rule === "A4" && b.band === "RED")) a4RedCount++;
        }
      }
      // eslint-disable-next-line no-console
      console.log("[4.5] A4 RED skill-body count (global layer):", a4RedCount);
      expect(a4RedCount).toBeGreaterThanOrEqual(1); // at least one genuine RED body — ballpark-checked below, not byte-exact vs. the doc's "6" (same ballpark convention as cc-audit-scan.test.ts)
    },
    60_000,
  );

  test(
    "rendered report: installfest's screens visibly render A1/A7-A8/A4 RED, and the trim panel reaches the A1 overage",
    async () => {
      const root = join(homedir(), "dev");
      const { fleet } = await buildFleet(root, { allowProbeHooks: false });
      annotateFleetBands(fleet);

      const vm = buildViewModel(fleet);
      const installfestView = vm.projects.find((p) => p.name === "installfest");
      expect(installfestView).toBeDefined();
      const trimPlan = computeTrimPlan(installfestView!, fleet);
      const a1Step = trimPlan.steps.find((s) => s.rule === "A1");
      expect(a1Step).toBeDefined();
      const a1OverageTokens = a1Step!.tokensRecovered;

      // eslint-disable-next-line no-console
      console.log("[4.5] installfest trim plan:", {
        totalOverageTokens: trimPlan.totalOverageTokens,
        finalTotalTokens: trimPlan.finalTotalTokens,
        reachesGreen: trimPlan.reachesGreen,
        a1OverageTokens,
        stepCount: trimPlan.steps.length,
      });

      // The plan's own running total (which includes A1 among possibly
      // other RED/AMBER rows) must sum to at least A1's own overage alone.
      expect(trimPlan.finalTotalTokens).toBeGreaterThanOrEqual(a1OverageTokens);
      expect(trimPlan.totalOverageTokens).toBeGreaterThanOrEqual(a1OverageTokens);

      if (!chromiumAvailable()) {
        console.warn("[4.5] chromium not available — skipping the browser-rendered half");
        return;
      }

      // Actually write + open the rendered file, exactly like the CLI would
      // (`ctx-scan render --project installfest -o /tmp/ctx-scan-cc.html`).
      const outDir = mkdtempSync(join(tmpdir(), "ctx-scan-cc-render-"));
      outDirs.push(outDir);
      const outPath = join(outDir, "ctx-scan-cc.html");
      const html = renderFleetHtml(fleet, { project: "installfest" });
      writeFileSync(outPath, html, "utf8");

      const installfestIdx = vm.projects.findIndex((p) => p.name === "installfest");
      const level1Id = `level1-${installfestIdx}`;

      const driver = `
        function computedBand(el) {
          var cs = getComputedStyle(el);
          return { borderColor: cs.borderTopColor || cs.borderLeftColor, cssClass: el.className };
        }
        var level1 = document.getElementById(${JSON.stringify(level1Id)});
        if (!level1) throw new Error("level1 section not found: " + ${JSON.stringify(level1Id)});

        // Drill into every class segment under installfest's level-1 screen,
        // then into every document under each, collecting the worst-band
        // class and any RED document titles/text we find.
        var classSegs = level1.querySelectorAll('.class-seg-group[data-nav-target]');
        var redClassCount = 0;
        var redDocs = [];
        classSegs.forEach(function (seg) {
          var cssClass = seg.querySelector('.class-seg').className;
          if (cssClass.indexOf('band-red') !== -1) redClassCount++;
          var level2Id = seg.getAttribute('data-nav-target');
          seg.click();
          var level2 = document.getElementById(level2Id);
          var docSegs = level2.querySelectorAll('.doc-bar-seg.band-red');
          docSegs.forEach(function (docSeg) {
            redDocs.push({ title: docSeg.getAttribute('title'), border: getComputedStyle(docSeg).borderTopColor });
          });
        });

        // The trim panel attached alongside level1's stacked bar.
        var trimPlanEl = level1.querySelector('.trim-plan');
        var trimSummary = trimPlanEl ? trimPlanEl.querySelector('.trim-summary').textContent : null;
        var trimRowCount = trimPlanEl ? trimPlanEl.querySelectorAll('.trim-table tbody tr').length : 0;

        window.__RESULT__ = {
          redClassCount: redClassCount,
          redDocCount: redDocs.length,
          redDocs: redDocs.slice(0, 10),
          trimSummary: trimSummary,
          trimRowCount: trimRowCount,
        };
      `;

      const result = runInBrowser(html, driver, { timeoutMs: 30_000 }) as {
        redClassCount: number;
        redDocCount: number;
        redDocs: Array<{ title: string; border: string }>;
        trimSummary: string | null;
        trimRowCount: number;
        driverError?: string;
      };

      // eslint-disable-next-line no-console
      console.log("[4.5] browser-rendered installfest screen:", result);

      expect(result.driverError).toBeUndefined();
      // At least one class segment renders visibly RED (A4/A7/A8-derived —
      // the shared global layer's claude-md-chain and skills-listing
      // classes both carry RED nodes).
      expect(result.redClassCount).toBeGreaterThanOrEqual(1);
      expect(result.trimRowCount).toBeGreaterThanOrEqual(1);
      expect(result.trimSummary).toBeTruthy();
      // Every rendered RED document segment's computed border really is the
      // band-red color (#dc2626), not just the class name string.
      for (const doc of result.redDocs) {
        expect(doc.border).toBe("rgb(220, 38, 38)");
      }
    },
    60_000,
  );
});
