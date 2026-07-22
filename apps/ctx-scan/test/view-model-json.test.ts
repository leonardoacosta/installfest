/**
 * view-model-json.test.ts — `ctx-scan view-model --json`'s emitted envelope
 * shape (wavetui-context-pane task [2.1], ADDED "view-model subcommand emits
 * the band-annotated view model as JSON" Requirement).
 *
 * Mirrors audit-contract.test.ts's own pattern: the success case spawns the
 * REAL `ctx-scan view-model` command as a subprocess (`cli.ts`'s actual
 * argv-parsing + `runViewModel` wiring) against a fixture tree, then
 * schema-checks the parsed stdout. The fixture places the project markers
 * directly AT `--root` (not a subdirectory) — this is wavetui's own real
 * usage shape (ctxscan.go passes its own repo root as `--root`, per
 * proposal.md's "Scan root decision"), so this test exercises the exact
 * invocation pattern the Go source depends on, not just a generic
 * `ctx-scan --root <parent>` fleet-discovery case audit-contract.test.ts
 * already covers.
 */
import { afterEach, describe, expect, test } from "bun:test";
import { join } from "node:path";
import { cleanup, dir, file, tmpRoot } from "./helpers/tree";

const roots: string[] = [];
afterEach(() => {
  while (roots.length) cleanup(roots.pop()!);
});
function tmp(prefix: string): string {
  const root = tmpRoot(prefix);
  roots.push(root);
  return root;
}

const APP_DIR = join(import.meta.dir, "..");

interface ViewModelEnvelope {
  schemaVersion: number;
  viewModel: {
    schemaVersion: number;
    root: string;
    generatedAt: string;
    fleet: { bars: unknown[]; maxTotalTokens: number };
    projects: Array<{
      name: string;
      path: string;
      classes: Array<{
        cls: string;
        label: string;
        documents: Array<{
          path: string;
          displayName: string;
          cls: string;
          tier: number;
          origin: string;
          rawChars: number;
          effectiveChars: number;
          estTokens: number;
          truncations: unknown[];
          bands: Array<{ rule: string; band: string; measured: number; limit: number }>;
          worstBand: string;
        }>;
        totalTokens: number;
        worstBand: string;
      }>;
      totalTokens: number;
    }>;
    contentByPath: Record<string, unknown>;
  };
}

test(
  "view-model output is band-annotated and schema-versioned (offending entry carries a non-GREEN band)",
  () => {
    // Project markers placed AT root (wavetui's own real invocation shape),
    // not in a discovered subdirectory.
    const root = tmp("ctx-scan-view-model-");
    dir(root, ".git");
    dir(root, ".claude");
    // A8 ("Per-file CLAUDE.md / rules import", tier===1) measures LINE
    // count, not char count — its amberMax is 400 lines, so a CLAUDE.md well
    // past that forces a RED verdict on its own Node, matching the ADDED
    // Requirement's scenario: "a fixture project tree with at least one
    // document exceeding a Table A cap."
    const lines = Array.from({ length: 500 }, (_, i) => `line ${i} of the fixture CLAUDE.md`);
    file(root, "CLAUDE.md", lines.join("\n"));

    const proc = Bun.spawnSync(["bun", "run", "src/cli.ts", "view-model", "--root", root], {
      cwd: APP_DIR,
      stdout: "pipe",
      stderr: "pipe",
    });

    expect(proc.exitCode).toBe(0);
    const stdout = proc.stdout.toString("utf8");
    let parsed: ViewModelEnvelope;
    try {
      parsed = JSON.parse(stdout);
    } catch (err) {
      throw new Error(`stdout was not valid JSON: ${err}\nstdout was:\n${stdout}`);
    }

    // Envelope-level schemaVersion — the field wavetui's Go decoder checks.
    expect(parsed.schemaVersion).toBe(1);
    expect(parsed.viewModel.schemaVersion).toBe(1);
    expect(Array.isArray(parsed.viewModel.projects)).toBe(true);
    expect(parsed.viewModel.projects.length).toBeGreaterThanOrEqual(1);

    const project = parsed.viewModel.projects.find((p) => p.path === root);
    expect(project).toBeDefined();

    const chainClass = project!.classes.find((c) => c.cls === "claude-md-chain");
    expect(chainClass).toBeDefined();
    expect(chainClass!.documents.length).toBeGreaterThanOrEqual(1);

    // Scoped to origin==="project": buildClassViews merges the shared global
    // layer's own CLAUDE.md chain into every project's classes too (both
    // share the same "claude-md-chain" cls and basename), so a plain
    // basename match would nondeterministically pick up whichever machine's
    // real ~/.claude chain happens to run first in the merged list instead
    // of this test's own fixture document.
    const doc = chainClass!.documents.find((d) => d.origin === "project" && d.path === join(root, "CLAUDE.md"));
    expect(doc).toBeDefined();
    // Bands are present WITHOUT a separate `audit` invocation — the whole
    // point of this subcommand vs. the raw-`scan` empty-bands shape.
    expect(doc!.bands.length).toBeGreaterThan(0);
    expect(doc!.worstBand).not.toBe("NONE");
    expect(["AMBER", "RED"]).toContain(doc!.worstBand);
  },
  20_000,
);

test("a clean project with no cap violations still emits GREEN/NONE bands, never a fabricated RED", () => {
  const root = tmp("ctx-scan-view-model-clean-");
  dir(root, ".git");
  dir(root, ".claude");
  file(root, "CLAUDE.md", "# tiny fixture\n");

  const proc = Bun.spawnSync(["bun", "run", "src/cli.ts", "view-model", "--root", root], {
    cwd: APP_DIR,
    stdout: "pipe",
    stderr: "pipe",
  });

  expect(proc.exitCode).toBe(0);
  const parsed: ViewModelEnvelope = JSON.parse(proc.stdout.toString("utf8"));
  expect(parsed.schemaVersion).toBe(1);

  const project = parsed.viewModel.projects.find((p) => p.path === root);
  expect(project).toBeDefined();
  // Scoped to origin==="project" — the shared global layer merged into
  // every project's classes reflects whatever real ~/.claude chain the
  // machine running this test happens to have, which this test makes no
  // claim about; only the fixture project's OWN documents must be clean.
  for (const cls of project!.classes) {
    for (const doc of cls.documents) {
      if (doc.origin !== "project") continue;
      expect(["GREEN", "NONE"]).toContain(doc.worstBand);
    }
  }
});
