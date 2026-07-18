/**
 * projectsFixture.ts — builds a throwaway `projects.toml` registry + repo
 * tree for openItems.test.ts and collect.test.ts, under a fresh `mkdtemp`
 * directory (never Leo's real `home/projects.toml`).
 *
 * Four repo entries, matching spec.md's "Open-items section aggregates
 * every registry repo with beads" scenarios:
 *   - "archived"  — resolves under an `archive/` path segment. Deliberately
 *                   ALSO given `.beads/` + a real git repo, so exclusion can
 *                   only be attributed to the archive-path check, not to a
 *                   missing prerequisite.
 *   - "nobeads"   — exists, is a real git repo, but has no `.beads/` dir.
 *   - "broken"    — has `.beads/` but is NOT a git repository (no `.git`),
 *                   which makes the real `open-items` binary's own
 *                   `git rev-parse --show-toplevel` step fail and print
 *                   `{"error": "not a git repository", ...}` with no
 *                   `beads` key — the real, unmodified script's own genuine
 *                   failure mode, which openItems.ts's `scanRepo()` turns
 *                   into a per-repo error (`"open-items reported beads
 *                   unavailable"`).
 *   - "normal"    — has `.beads/issues.jsonl` with one valid fixture line
 *                   and a real git repo — the happy path.
 *
 * The temp root lives under `os.tmpdir()` (verified NOT inside any git
 * tree on this machine), so `broken`'s `git rev-parse` genuinely fails
 * rather than accidentally discovering an ancestor `.git`.
 */

import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

const FIXTURE_BEADS_LINE = (id: string) =>
  `${JSON.stringify({
    id,
    status: "open",
    priority: 2,
    issue_type: "task",
    title: `fixture task ${id}`,
    labels: [],
    dependencies: [],
  })}\n`;

async function gitInit(dir: string): Promise<void> {
  const proc = Bun.spawn(["git", "init", "-q"], { cwd: dir, stdout: "ignore", stderr: "ignore" });
  const exitCode = await proc.exited;
  if (exitCode !== 0) throw new Error(`git init failed in ${dir}`);
}

export interface ProjectsFixture {
  /** Root dir standing in for `$DOTFILES` — contains `home/projects.toml`. */
  dotfilesDir: string;
  /** Root dir standing in for `local_base_linux` — contains the repo dirs. */
  basesDir: string;
  cleanup: () => Promise<void>;
}

export async function buildProjectsFixture(): Promise<ProjectsFixture> {
  const root = await mkdtemp(join(tmpdir(), "daily-brief-fixture-"));
  const dotfilesDir = join(root, "dotfiles");
  const basesDir = join(root, "bases");
  const homeDir = join(basesDir); // resolveProjectRoot joins base + project.path directly

  await mkdir(join(dotfilesDir, "home"), { recursive: true });

  // archived/old-project — has .beads + git, but path contains "archive"
  const archivedDir = join(homeDir, "archive", "old-project");
  await mkdir(join(archivedDir, ".beads"), { recursive: true });
  await writeFile(join(archivedDir, ".beads", "issues.jsonl"), FIXTURE_BEADS_LINE("fx-archived"));
  await gitInit(archivedDir);

  // nobeads-repo — real git repo, no .beads/
  const nobeadsDir = join(homeDir, "nobeads-repo");
  await mkdir(nobeadsDir, { recursive: true });
  await gitInit(nobeadsDir);

  // broken-repo — .beads/ present, NOT a git repo
  const brokenDir = join(homeDir, "broken-repo");
  await mkdir(join(brokenDir, ".beads"), { recursive: true });

  // normal-repo — .beads/issues.jsonl + real git repo (happy path)
  const normalDir = join(homeDir, "normal-repo");
  await mkdir(join(normalDir, ".beads"), { recursive: true });
  await writeFile(join(normalDir, ".beads", "issues.jsonl"), FIXTURE_BEADS_LINE("fx-normal"));
  await gitInit(normalDir);

  const tomlContent = `[defaults]
local_base_linux = "${basesDir}"
local_base_mac = "${basesDir}"

[[projects]]
code = "archived"
path = "archive/old-project"

[[projects]]
code = "nobeads"
path = "nobeads-repo"

[[projects]]
code = "broken"
path = "broken-repo"

[[projects]]
code = "normal"
path = "normal-repo"
`;
  await writeFile(join(dotfilesDir, "home", "projects.toml"), tomlContent);

  return {
    dotfilesDir,
    basesDir,
    cleanup: async () => {
      await rm(root, { recursive: true, force: true });
    },
  };
}
