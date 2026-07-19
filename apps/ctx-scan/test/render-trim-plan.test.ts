/**
 * render-trim-plan.test.ts — ctx-scan-render task [4.4], beads:if-mpmv.
 *
 * Fixture RED set with a known overage; asserts the trim plan's running
 * total reaches or exceeds the overage. Pure unit-style test (no browser
 * needed — `computeTrimPlan` is plain data-in/data-out, per trim-plan.ts's
 * own module doc: read-only, no file-write code path).
 *
 * Both fixture nodes use a `chars`/`bytes`-unit Table A row so
 * `tokensRecovered` is a clean, independently-hand-computable
 * `ceil(overage / 4)` (Part 0's own "tokens ~= chars/4" convention,
 * trim-plan.ts's `CHARS_PER_TOKEN`) — avoiding the "lines"-unit row's
 * fraction-of-est_tokens approximation, which would make the expected value
 * depend on trim-plan.ts's own internals rather than a value this test
 * derives independently.
 */
import { describe, expect, test } from "bun:test";
import { makeFleet, makeNode, project, surface } from "./fixtures/render/build";
import { annotateFleetBands } from "../src/rubric";
import { buildViewModel } from "../src/render/view-model";
import { computeTrimPlan } from "../src/render/trim-plan";

describe("ctx-scan render — trim-plan arithmetic [4.4]", () => {
  test("greedy plan over a known RED set: running total reaches (equals) the hand-computed overage", () => {
    // A14 (hooks-injected): greenMax=8000, amberMax=10000, unit=chars, measurer=raw_chars.
    // measured=12000 -> RED (>10000); overage = 12000-8000 = 4000 chars -> ceil(4000/4) = 1000 tokens recovered.
    const hooksNode = makeNode({ path: "/fixture/hooks-a.md", cls: "hooks-injected", raw_chars: 12_000, est_tokens: 3000 });
    // A13 (mcp-tools): greenMax=1600, amberMax=2048, unit=bytes, measurer=raw_chars.
    // measured=2400 -> RED (>2048); overage = 2400-1600 = 800 bytes -> ceil(800/4) = 200 tokens recovered.
    const mcpNode = makeNode({ path: "/fixture/mcp-a.md", cls: "mcp-tools", raw_chars: 2400, est_tokens: 600 });

    const fleet = makeFleet(
      [], // empty global layer — no A1/A7/A12 aggregate candidates leak into this plan
      [
        project("proj", "/fixture/proj", [
          surface("hooks-injected", [hooksNode]),
          surface("mcp-tools", [mcpNode]),
        ]),
      ],
    );
    annotateFleetBands(fleet);

    // Confirm the rubric assigned exactly the RED bands this test relies on,
    // before ever touching computeTrimPlan — isolates a rubric regression
    // from a trim-plan regression.
    expect(hooksNode.bands).toEqual([{ rule: "A14", band: "RED", measured: 12_000, limit: 10_000 }]);
    expect(mcpNode.bands).toEqual([{ rule: "A13", band: "RED", measured: 2400, limit: 2048 }]);

    const vm = buildViewModel(fleet);
    const projectView = vm.projects.find((p) => p.name === "proj");
    expect(projectView).toBeDefined();

    const plan = computeTrimPlan(projectView!, fleet);

    const KNOWN_OVERAGE_TOKENS = 1000 + 200; // hand-computed from the two RED rows above

    expect(plan.steps.length).toBe(2);
    // Greedy order: highest tokens-recovered-per-change first (A14's 1000 before A13's 200).
    expect(plan.steps[0]!.rule).toBe("A14");
    expect(plan.steps[0]!.tokensRecovered).toBe(1000);
    expect(plan.steps[1]!.rule).toBe("A13");
    expect(plan.steps[1]!.tokensRecovered).toBe(200);

    expect(plan.totalOverageTokens).toBe(KNOWN_OVERAGE_TOKENS);
    // The core acceptance criterion: running total reaches OR EXCEEDS the known overage.
    expect(plan.finalTotalTokens).toBeGreaterThanOrEqual(KNOWN_OVERAGE_TOKENS);
    expect(plan.steps[0]!.runningTotalTokens).toBe(1000);
    expect(plan.steps[1]!.runningTotalTokens).toBe(1200);
    expect(plan.reachesGreen).toBe(true);
  });

  test("a project with zero RED/AMBER candidates yields an empty, vacuously-green plan", () => {
    const greenNode = makeNode({ path: "/fixture/mcp-green.md", cls: "mcp-tools", raw_chars: 500, est_tokens: 125 });
    const fleet = makeFleet([], [project("proj", "/fixture/proj", [surface("mcp-tools", [greenNode])])]);
    annotateFleetBands(fleet);
    expect(greenNode.bands).toEqual([{ rule: "A13", band: "GREEN", measured: 500, limit: 2048 }]);

    const vm = buildViewModel(fleet);
    const projectView = vm.projects.find((p) => p.name === "proj")!;
    const plan = computeTrimPlan(projectView, fleet);

    expect(plan.steps).toEqual([]);
    expect(plan.totalOverageTokens).toBe(0);
    expect(plan.finalTotalTokens).toBe(0);
    expect(plan.reachesGreen).toBe(true); // vacuously true, per trim-plan.ts's own documented contract
  });

  test("global-layer aggregate RED (A1) surfaces as a trim candidate shared across projects", () => {
    // A1 is aggregate-only (nodeClasses: null) — computed by audit.ts's
    // aggregateA1 over the GLOBAL layer's skills-listing/commands-listing
    // Surfaces (raw_chars + nameLength(path), summed). greenMax=6400,
    // amberMax=8000 (LISTING_TOTAL_BUDGET_CHARS).
    //
    // Seven nodes at raw_chars=1200 each (individually GREEN under A2's own
    // 1,229 greenMax, and A3/A4/A10 degrade to "skip" on an unreadable path
    // — no per-node candidate) whose SUM (8,400) crosses amberMax into RED —
    // isolating the genuinely aggregate-only case the module doc describes
    // ("no single Node to attribute a change to"), unlike a single huge node
    // which would also trip its own per-node A2 band.
    const globalSkills = Array.from({ length: 7 }, (_, i) =>
      makeNode({
        path: `/fixture/does-not-exist-global-skill-${i}.md`, // nameLength()/descriptionAloneLength() degrade to 0/null on unreadable path — never fabricated
        cls: "skills-listing",
        raw_chars: 1200,
        est_tokens: 300,
      }),
    );
    const fleet = makeFleet(
      [surface("skills-listing", globalSkills)],
      [project("proj", "/fixture/proj", [])], // project has no surfaces of its own — the aggregate is purely global
    );
    annotateFleetBands(fleet);

    // Confirm no per-node candidate leaks in from these nodes (A2 GREEN, A3/A4/A10 skipped).
    for (const n of globalSkills) expect(n.bands).toEqual([{ rule: "A2", band: "GREEN", measured: 1200, limit: 1536 }]);

    const vm = buildViewModel(fleet);
    const projectView = vm.projects.find((p) => p.name === "proj")!;
    const plan = computeTrimPlan(projectView, fleet);

    expect(plan.steps.length).toBe(1);
    expect(plan.steps[0]!.rule).toBe("A1");
    expect(plan.steps[0]!.band).toBe("RED");
    expect(plan.steps[0]!.measured).toBe(8400); // aggregateA1: 7 * raw_chars(1200) + nameLength(0, unreadable path)
    // overage = 8400 - 6400 = 2000 chars -> ceil(2000/4) = 500 tokens.
    expect(plan.steps[0]!.tokensRecovered).toBe(500);
    expect(plan.totalOverageTokens).toBe(500);
    expect(plan.finalTotalTokens).toBeGreaterThanOrEqual(plan.totalOverageTokens);
    expect(plan.reachesGreen).toBe(true);
  });
});
