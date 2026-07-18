/**
 * discovery.test.ts — fleet discovery exclusions + global-layer identification
 * (ctx-scan-core tasks [4.1] beads:if-l383, [4.2] beads:if-kzlk).
 *
 * Uses the `./helpers/tree` hermetic-fixture builder — fixtures live under the
 * OS tmpdir, never inside the repo, so gitignore/chezmoi rules can't eat a
 * committed fixture dir and silently turn a phantom-exclusion assertion into
 * a trivial pass.
 */
import { afterEach, describe, expect, test } from "bun:test";
import { realpathSync } from "node:fs";
import { symlink } from "node:fs/promises";
import { join } from "node:path";
import { discoverProjects, getGlobalLayer } from "../src/discovery";
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

describe("discoverProjects — hard exclusions applied at descent time [4.1]", () => {
  test("real project found; plugin/archive/worktree subtrees excluded as phantoms", () => {
    const root = tmp("ctx-scan-discovery-");

    // Real project: a git root with CLAUDE.md.
    dir(root, "real-project/.git");
    file(root, "real-project/CLAUDE.md", "# real\n");

    // Phantom 1: plugins/marketplaces/<plugin> — its own CLAUDE.md.
    file(root, "plugins/marketplaces/some-plugin/CLAUDE.md", "# plugin\n");

    // Phantom 2: archive/old-project — its own .claude/.
    dir(root, "archive/old-project/.claude");

    // Phantom 3: .worktrees/session-1 — its own .mcp.json.
    file(root, ".worktrees/session-1/.mcp.json", "{}\n");

    const found = discoverProjects(root, { globalPath: "/nonexistent-global-sentinel" });

    expect(found).toHaveLength(1);
    expect(realpathSync(found[0]!.path)).toBe(realpathSync(join(root, "real-project")));
    expect(found[0]!.name).toBe("real-project");
  });

  test("archive*-prefixed and *-archive-suffixed names are both excluded", () => {
    const root = tmp("ctx-scan-discovery-");
    dir(root, "real-project/.git");
    file(root, "real-project/CLAUDE.md", "# real\n");

    dir(root, "archive-2024/proj/.claude");
    dir(root, "old-archive/proj/.claude");

    const found = discoverProjects(root, { globalPath: "/nonexistent-global-sentinel" });
    expect(found.map((p) => p.name)).toEqual(["real-project"]);
  });

  test("symlink cycle terminates without hanging or duplicating", async () => {
    const root = tmp("ctx-scan-discovery-cycle-");
    dir(root, "real-project/.git");
    file(root, "real-project/CLAUDE.md", "# real\n");

    // A symlink inside the tree pointing back at root — creates a cycle.
    await symlink(root, join(root, "real-project", "loop"));

    const found = discoverProjects(root, { globalPath: "/nonexistent-global-sentinel" });
    // Must terminate (test itself would hang/timeout if it didn't) and must
    // not report the cyclic path as a second project.
    expect(found).toHaveLength(1);
  });
});

describe("global-layer identification [4.2]", () => {
  test("global layer appears exactly once and never as a discovered project", async () => {
    const root = tmp("ctx-scan-discovery-global-");

    // The "global" layer: a real target dir with a CLAUDE.md (matches the
    // discovery predicate structurally, same as ~/.claude -> ~/dev/cc).
    dir(root, "global-target/.git");
    file(root, "global-target/CLAUDE.md", "# global\n");

    // The symlink that stands in for ~/.claude.
    const globalSymlink = join(root, "dot-claude-symlink");
    await symlink(join(root, "global-target"), globalSymlink);

    // A separate, genuinely-real project.
    dir(root, "real-project/.git");
    file(root, "real-project/CLAUDE.md", "# real\n");

    const layer = getGlobalLayer(); // confirm shape via the real homedir; the
    // exclusion behavior itself is exercised via opts.globalPath below.
    expect(layer.origin).toBe("global");
    expect(typeof layer.path).toBe("string");

    const globalReal = realpathSync(globalSymlink);
    const found = discoverProjects(root, { globalPath: globalReal });

    expect(found.map((p) => p.name)).toEqual(["real-project"]);
    expect(found.some((p) => realpathSync(p.path) === globalReal)).toBe(false);
  });
});
