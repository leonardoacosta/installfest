/**
 * discovery.test.ts — fleet discovery exclusions + global-layer identification
 * (ctx-scan-core tasks [4.1] beads:if-l383, [4.2] beads:if-kzlk).
 *
 * Builds a hermetic fixture tree per test via mkdtemp (never touches the real
 * ~/dev or ~/.claude), following apps/daily-brief/test's local convention.
 */
import { afterEach, describe, expect, test } from "bun:test";
import { mkdir, mkdtemp, rm, symlink, writeFile } from "node:fs/promises";
import { realpathSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { discoverProjects, getGlobalLayer } from "../src/discovery";

const cleanupDirs: string[] = [];

afterEach(async () => {
  while (cleanupDirs.length) {
    const dir = cleanupDirs.pop()!;
    await rm(dir, { recursive: true, force: true });
  }
});

async function tmp(prefix: string): Promise<string> {
  const dir = await mkdtemp(join(tmpdir(), prefix));
  cleanupDirs.push(dir);
  return dir;
}

describe("discoverProjects — hard exclusions applied at descent time [4.1]", () => {
  test("real project found; plugin/archive/worktree subtrees excluded as phantoms", async () => {
    const root = await tmp("ctx-scan-discovery-");

    // Real project: a git root with CLAUDE.md.
    const real = join(root, "real-project");
    await mkdir(join(real, ".git"), { recursive: true });
    await writeFile(join(real, "CLAUDE.md"), "# real\n");

    // Phantom 1: plugins/marketplaces/<plugin> — its own CLAUDE.md.
    const pluginDir = join(root, "plugins", "marketplaces", "some-plugin");
    await mkdir(pluginDir, { recursive: true });
    await writeFile(join(pluginDir, "CLAUDE.md"), "# plugin\n");

    // Phantom 2: archive/old-project — its own .claude/.
    const archiveDir = join(root, "archive", "old-project");
    await mkdir(join(archiveDir, ".claude"), { recursive: true });

    // Phantom 3: .worktrees/session-1 — its own .mcp.json.
    const worktreeDir = join(root, ".worktrees", "session-1");
    await mkdir(worktreeDir, { recursive: true });
    await writeFile(join(worktreeDir, ".mcp.json"), "{}\n");

    const found = discoverProjects(root, { globalPath: "/nonexistent-global-sentinel" });

    expect(found).toHaveLength(1);
    expect(realpathSync(found[0]!.path)).toBe(realpathSync(real));
    expect(found[0]!.name).toBe("real-project");
  });

  test("archive*-prefixed and *-archive-suffixed names are both excluded", async () => {
    const root = await tmp("ctx-scan-discovery-");
    const real = join(root, "real-project");
    await mkdir(join(real, ".git"), { recursive: true });
    await writeFile(join(real, "CLAUDE.md"), "# real\n");

    const archivePrefixed = join(root, "archive-2024", "proj");
    await mkdir(join(archivePrefixed, ".claude"), { recursive: true });
    const archiveSuffixed = join(root, "old-archive", "proj");
    await mkdir(join(archiveSuffixed, ".claude"), { recursive: true });

    const found = discoverProjects(root, { globalPath: "/nonexistent-global-sentinel" });
    expect(found.map((p) => p.name)).toEqual(["real-project"]);
  });

  test("symlink cycle terminates without hanging or duplicating", async () => {
    const root = await tmp("ctx-scan-discovery-cycle-");
    const real = join(root, "real-project");
    await mkdir(join(real, ".git"), { recursive: true });
    await writeFile(join(real, "CLAUDE.md"), "# real\n");

    // A symlink inside the tree pointing back at root — creates a cycle.
    await symlink(root, join(real, "loop"));

    const found = discoverProjects(root, { globalPath: "/nonexistent-global-sentinel" });
    // Must terminate (test itself would hang/timeout if it didn't) and must
    // not report the cyclic path as a second project.
    expect(found).toHaveLength(1);
  });
});

describe("global-layer identification [4.2]", () => {
  test("global layer appears exactly once and never as a discovered project", async () => {
    const root = await tmp("ctx-scan-discovery-global-");

    // The "global" layer: a real target dir with a CLAUDE.md (matches the
    // discovery predicate structurally, same as ~/.claude -> ~/dev/cc).
    const globalTarget = join(root, "global-target");
    await mkdir(join(globalTarget, ".git"), { recursive: true });
    await writeFile(join(globalTarget, "CLAUDE.md"), "# global\n");

    // The symlink that stands in for ~/.claude.
    const globalSymlink = join(root, "dot-claude-symlink");
    await symlink(globalTarget, globalSymlink);

    // A separate, genuinely-real project.
    const real = join(root, "real-project");
    await mkdir(join(real, ".git"), { recursive: true });
    await writeFile(join(real, "CLAUDE.md"), "# real\n");

    const layer = getGlobalLayer(); // uses the real homedir by default — just
    // confirm shape here; the exclusion behavior is tested via opts.globalPath below.
    expect(layer.origin).toBe("global");
    expect(typeof layer.path).toBe("string");

    const globalReal = realpathSync(globalSymlink);
    const found = discoverProjects(root, { globalPath: globalReal });

    expect(found.map((p) => p.name)).toEqual(["real-project"]);
    expect(found.some((p) => realpathSync(p.path) === globalReal)).toBe(false);
  });
});
