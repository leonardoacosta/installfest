/**
 * refs-skill-scoping.test.ts — ctx-scan-refs task [4.3], beads:if-rm5i.
 *
 * Multi-skill fixture (three owners: two skills + a commands-dir owner);
 * asserts `renderProjectShelf`'s `--skill <name>` scoping (the same
 * `opts.skill` path `cli.ts`'s `render --skill <name>` flag drives, see
 * `cli.ts`'s `runRender` -> `writeRenderedFleet` -> `renderFleetHtml` ->
 * `renderProjectShelf` wiring) excludes every OTHER owner's references —
 * from the raw entry list, the rendered screen HTML, AND the content-preview
 * cache (so a scoped render never even reads/embeds an out-of-scope file's
 * bytes).
 */
import { afterEach, describe, expect, test } from "bun:test";
import { buildShelf, renderProjectShelf, scopeShelfToOwner } from "../src/refs";
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

function buildMultiSkillFixture(claudeHome: string): void {
  file(claudeHome, "skills/skill-alpha/SKILL.md", "# Skill Alpha\n\n[Alpha ref](references/alpha-ref.md)\n");
  file(claudeHome, "skills/skill-alpha/references/alpha-ref.md", "# Alpha Reference\n\nOwned by skill-alpha only.");

  file(claudeHome, "skills/skill-beta/SKILL.md", "# Skill Beta\n\n[Beta ref](references/beta-ref.md)\n");
  file(claudeHome, "skills/skill-beta/references/beta-ref.md", "# Beta Reference\n\nOwned by skill-beta only.");

  // A third, differently-shaped owner (commands/ dir) to prove scoping
  // isn't accidentally skills-only.
  file(claudeHome, "commands/deploy.md", "# /deploy\n\n[Deploy notes](deploy/references/notes.md)\n");
  file(claudeHome, "commands/deploy/references/notes.md", "# Deploy Notes\n\nOwned by the deploy command only.");
}

describe("references shelf — per-skill scoping excludes every other owner [4.3]", () => {
  test("unscoped shelf sees all three owners; scoping to one excludes the other two entirely", () => {
    const claudeHome = tmp("ctx-scan-refs-scoping-");
    buildMultiSkillFixture(claudeHome);
    const projectPath = `${claudeHome}-unrelated-project`;

    const allEntries = buildShelf(projectPath, claudeHome);
    const owners = new Set(allEntries.map((e) => e.owner));
    expect(owners).toEqual(new Set(["skill-alpha", "skill-beta", "deploy"]));
    expect(allEntries).toHaveLength(3);

    const scopedToAlpha = scopeShelfToOwner(allEntries, "skill-alpha");
    expect(scopedToAlpha).toHaveLength(1);
    expect(scopedToAlpha[0]!.path.endsWith("alpha-ref.md")).toBe(true);
    // Neither other owner's entries leak through.
    expect(scopedToAlpha.some((e) => e.owner === "skill-beta")).toBe(false);
    expect(scopedToAlpha.some((e) => e.owner === "deploy")).toBe(false);
  });

  test("renderProjectShelf({ skill }) — scoped screen HTML and content cache exclude other owners' files", () => {
    const claudeHome = tmp("ctx-scan-refs-scoping-render-");
    buildMultiSkillFixture(claudeHome);
    const projectPath = `${claudeHome}-unrelated-project-render`;

    const unscoped = renderProjectShelf(projectPath, claudeHome, 0);
    expect(unscoped.screensHtml).toContain("skill-alpha");
    expect(unscoped.screensHtml).toContain("skill-beta");
    expect(unscoped.screensHtml).toContain("deploy");
    expect(Object.keys(unscoped.contentByPath)).toHaveLength(3);

    const scoped = renderProjectShelf(projectPath, claudeHome, 0, { skill: "skill-alpha" });

    // The scoped shelf HOME section only lists the one scoped owner group.
    expect(scoped.screensHtml).toContain("skill-alpha");
    expect(scoped.screensHtml).not.toContain("skill-beta");
    expect(scoped.screensHtml).not.toContain(">deploy<");
    expect(scoped.screensHtml).not.toContain("beta-ref.md");
    expect(scoped.screensHtml).not.toContain("notes.md");
    expect(scoped.screensHtml).toContain("alpha-ref.md");

    // The content-preview cache built for the scoped render never reads
    // (or embeds) the excluded owners' files at all.
    const scopedContentPaths = Object.keys(scoped.contentByPath);
    expect(scopedContentPaths).toHaveLength(1);
    expect(scopedContentPaths[0]!.endsWith("alpha-ref.md")).toBe(true);
  });
});
