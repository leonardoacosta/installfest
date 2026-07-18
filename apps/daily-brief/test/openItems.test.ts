/**
 * openItems.test.ts — registry-loop fixture suite (add-daily-brief-tui
 * task 4.2, beads:if-2wo6).
 *
 * Exercises the REAL `collectOpenItems()` export against a throwaway
 * `projects.toml` fixture (never Leo's real `home/projects.toml`) built by
 * test/helpers/projectsFixture.ts, run as a fresh subprocess with
 * `DOTFILES` pointed at the fixture root (see run-open-items.ts's header
 * comment for why a subprocess). The fixture's four entries exercise all
 * three scenarios spec.md's "Open-items section aggregates every registry
 * repo with beads" requirement describes:
 *   - an `archive/`-path entry -> excluded
 *   - a beads-less repo -> silently skipped (no error)
 *   - a repo with `.beads/` but no `.git` -> real `open-items` binary
 *     reports "not a git repository", which surfaces as a per-repo error
 *   - a normal repo (git + `.beads/issues.jsonl`) -> included
 */
import { afterAll, beforeAll, describe, expect, test } from "bun:test";
import { buildProjectsFixture, type ProjectsFixture } from "./helpers/projectsFixture";
import { runBunScript } from "./helpers/runBun";
import type { OpenItemsScan } from "../src/sources/openItems";

describe("collectOpenItems against a fixture registry", () => {
  let fixture: ProjectsFixture;
  let scan: OpenItemsScan;

  beforeAll(async () => {
    fixture = await buildProjectsFixture();
    const { stdout, stderr, exitCode } = await runBunScript("run-open-items.ts", [], {
      DOTFILES: fixture.dotfilesDir,
    });
    expect(exitCode, `run-open-items.ts exited ${exitCode}, stderr:\n${stderr}`).toBe(0);
    scan = JSON.parse(stdout) as OpenItemsScan;
  });

  afterAll(async () => {
    await fixture.cleanup();
  });

  test("archive-path entry is excluded entirely", () => {
    expect(scan.repos.some((r) => r.code === "archived")).toBe(false);
    expect(scan.errors.some((e) => e.repo === "archived")).toBe(false);
  });

  test("beads-less repo is silently skipped (not an error)", () => {
    expect(scan.repos.some((r) => r.code === "nobeads")).toBe(false);
    expect(scan.errors.some((e) => e.repo === "nobeads")).toBe(false);
  });

  test("repo with .beads/ but no .git records a per-repo error, not a thrown exception", () => {
    const err = scan.errors.find((e) => e.repo === "broken");
    expect(err).toBeDefined();
    expect(err?.error).toContain("beads unavailable");
  });

  test("normal repo (git + .beads/issues.jsonl) is included in the aggregation", () => {
    const repo = scan.repos.find((r) => r.code === "normal");
    expect(repo).toBeDefined();
    expect(repo?.summary.total_open).toBe(1);
    expect(repo?.top_items.some((i) => i.id === "fx-normal")).toBe(true);
  });

  test("exactly one repo included, one errored, two silently excluded", () => {
    expect(scan.repos.length).toBe(1);
    expect(scan.errors.length).toBe(1);
  });
});
