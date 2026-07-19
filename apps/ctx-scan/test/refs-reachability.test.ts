/**
 * refs-reachability.test.ts — ctx-scan-refs task [4.1], beads:if-8mly.
 *
 * Fixture with one `references/` file cited via a markdown link from its
 * owning SKILL.md, one genuinely orphaned reference file, and a third
 * reference cited only via PROSE (no markdown link) — establishing the
 * detection boundary `refs.ts`'s `findCitation` documents explicitly:
 * markdown-link citations count as reachable, prose-only mentions do not
 * (proposal.md's Risk section).
 *
 * `buildShelf` is exercised directly against an isolated fixture tree passed
 * as BOTH `claudeHome` (so `findGroupedReferenceFiles(join(claudeHome,
 * "skills"), ...)` discovers exactly this fixture's one skill, never the real
 * `~/.claude`) and an unrelated non-existent `projectPath` (so the
 * project-local `.claude/skills` + project memory-dir walks find nothing —
 * every discovered entry in this test comes from the one fixture skill).
 */
import { afterEach, describe, expect, test } from "bun:test";
import { buildShelf } from "../src/refs";
import { cleanup, file, tmpRoot } from "./helpers/tree";

const roots: string[] = [];
afterEach(() => {
  while (roots.length) cleanup(roots.pop()!);
});
function tmp(prefix: string): string {
  const root = tmpRoot(prefix);
  roots.push(root);
  return root;
}

describe("references shelf — reachability detection (markdown-link only) [4.1]", () => {
  test("linked reference reports its citing line; orphan reports orphan; prose-only citation does NOT count as reachable", () => {
    const claudeHome = tmp("ctx-scan-refs-reachability-");

    // SKILL.md: line 1 = heading, line 2 = blank, line 3 = markdown-link
    // citation to linked.md, line 4 = blank, line 5 = prose-only mention of
    // prose-only.md with NO markdown-link syntax around it.
    const skillMdLines = [
      "# Fixture Skill",
      "",
      "See [the linked reference](references/linked.md) for details.",
      "",
      "Also see prose-only.md for background — deliberately not a markdown link.",
    ];
    file(claudeHome, "skills/fixture-skill/SKILL.md", skillMdLines.join("\n"));
    file(claudeHome, "skills/fixture-skill/references/linked.md", "# Linked\n\nThis one is cited via markdown link.");
    file(claudeHome, "skills/fixture-skill/references/orphaned.md", "# Orphaned\n\nNo owning document ever cites this file.");
    file(
      claudeHome,
      "skills/fixture-skill/references/prose-only.md",
      "# Prose Only\n\nOnly mentioned in prose by its owning SKILL.md, never markdown-linked.",
    );

    // A project path that doesn't exist on disk at all — buildShelf's
    // project-local discovery (.claude/skills, .claude/rules, the memory
    // dir) all degrade to [] via existsSync guards, so every entry below
    // comes from the claudeHome fixture skill above.
    const projectPath = `${claudeHome}-unrelated-project`;

    const entries = buildShelf(projectPath, claudeHome);
    expect(entries).toHaveLength(3);

    const byName = new Map(entries.map((e) => [e.path.split("/").pop()!, e]));
    const linked = byName.get("linked.md");
    const orphaned = byName.get("orphaned.md");
    const proseOnly = byName.get("prose-only.md");
    expect(linked).toBeDefined();
    expect(orphaned).toBeDefined();
    expect(proseOnly).toBeDefined();

    // Linked: reachable, with the exact citing file + 1-indexed line number
    // the markdown link appears on in the fixture SKILL.md above (line 3).
    expect(linked!.reachable).toBe(true);
    expect(linked!.citation).toBe("routed from SKILL.md line 3");

    // Orphaned: genuinely never referenced anywhere — reports `orphan` (no citation).
    expect(orphaned!.reachable).toBe(false);
    expect(orphaned!.citation).toBeUndefined();

    // Prose-only: mentioned by name in SKILL.md's prose, but NEVER via
    // markdown-link syntax — per the documented detection boundary, this
    // must NOT count as reachable. This is the assertion that actually
    // proves the boundary (not just that orphan detection works at all).
    expect(proseOnly!.reachable).toBe(false);
    expect(proseOnly!.citation).toBeUndefined();

    // All three share the same owner (the fixture skill directory name).
    for (const e of entries) expect(e.owner).toBe("fixture-skill");
  });
});
