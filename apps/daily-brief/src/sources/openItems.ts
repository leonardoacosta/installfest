/**
 * openItems.ts — fleet-wide open-beads aggregation.
 *
 * Parses `home/projects.toml` (`$DOTFILES`-relative), resolves each
 * project's path against the home directory, and for every entry whose
 * resolved directory (a) exists, (b) contains a `.beads/` subdirectory, and
 * (c) does not resolve under an `**\/archive/**` path, runs
 * `~/.claude/scripts/bin/open-items --json` from that directory. A single
 * repo's scan failing (non-zero exit or unparseable JSON) never aborts the
 * fleet loop — its error is recorded and the loop continues (spec.md
 * "Open-items section aggregates every registry repo with beads").
 *
 * TOML is parsed via Bun's built-in `Bun.TOML.parse` (no smol-toml or other
 * dependency needed — native platform feature over an installed dep, per
 * the Reader Gate preference order).
 */

import { existsSync } from "node:fs";
import { homedir, platform } from "node:os";
import { join } from "node:path";

const DOTFILES = process.env.DOTFILES ?? join(homedir(), "dev/personal/installfest");
const PROJECTS_TOML_PATH = join(DOTFILES, "home/projects.toml");
const OPEN_ITEMS_BIN = join(homedir(), ".claude/scripts/bin/open-items");
const ARCHIVE_PATH_RE = /(^|\/)archive(\/|$)/;
const TOP_ITEMS_LIMIT = 15;

interface ProjectEntry {
  code: string;
  name?: string;
  category?: string;
  icon?: string;
  path: string;
  tiers?: string[];
}

interface ProjectsTomlShape {
  defaults?: Record<string, string>;
  projects?: ProjectEntry[];
}

interface OpenItemRecord {
  id: string;
  priority: number;
  type: string;
  status: string;
  title: string;
  bucket: string;
  bucket_reason: string;
  ambiguous: boolean;
  cross_repo: string | null;
}

interface OpenItemsCliOutput {
  project: string;
  root: string;
  head?: string;
  beads?: {
    available: boolean;
    total_open: number;
    by_status: Record<string, number>;
    active_epics: number;
    active_proposal_linked: number;
    bucket_counts: Record<string, number>;
    items: OpenItemRecord[];
  };
  [key: string]: unknown;
}

export interface OpenItemsRepoResult {
  code: string;
  root: string;
  summary: {
    total_open: number;
    by_status: Record<string, number>;
    active_epics: number;
    active_proposal_linked: number;
  };
  bucket_counts: Record<string, number>;
  top_items: OpenItemRecord[];
}

export interface OpenItemsError {
  repo: string;
  error: string;
}

export interface OpenItemsScan {
  repos: OpenItemsRepoResult[];
  errors: OpenItemsError[];
}

function expandHome(value: string): string {
  return value === "$HOME" ? homedir() : value;
}

interface ProjectsToml {
  defaults: Record<string, string>;
  projects: ProjectEntry[];
}

async function loadProjectsToml(): Promise<ProjectsToml> {
  const text = await Bun.file(PROJECTS_TOML_PATH).text();
  const parsed = Bun.TOML.parse(text) as ProjectsTomlShape;
  return { defaults: parsed.defaults ?? {}, projects: parsed.projects ?? [] };
}

/** `local_base_mac`/`local_base_linux` per platform (both currently `"$HOME"`). */
function resolveLocalBase(defaults: Record<string, string>): string {
  const key = platform() === "darwin" ? "local_base_mac" : "local_base_linux";
  return expandHome(defaults[key] ?? "$HOME");
}

function resolveProjectRoot(project: ProjectEntry, base: string): string {
  return join(base, project.path);
}

/** Ordering: blocked bucket first, then human_only, then everything else. */
function topItems(items: OpenItemRecord[]): OpenItemRecord[] {
  const blocked = items.filter((i) => i.bucket === "blocked");
  const humanOnly = items.filter((i) => i.bucket === "human_only");
  const rest = items.filter((i) => i.bucket !== "blocked" && i.bucket !== "human_only");
  return [...blocked, ...humanOnly, ...rest].slice(0, TOP_ITEMS_LIMIT);
}

async function scanRepo(project: ProjectEntry, root: string): Promise<OpenItemsRepoResult> {
  const proc = Bun.spawn([OPEN_ITEMS_BIN, "--json"], {
    cwd: root,
    stdout: "pipe",
    stderr: "pipe",
  });
  const [stdout, exitCode] = await Promise.all([new Response(proc.stdout).text(), proc.exited]);
  if (exitCode !== 0) {
    throw new Error(`open-items exited ${exitCode}`);
  }
  const parsed = JSON.parse(stdout) as OpenItemsCliOutput;
  const beads = parsed.beads;
  if (!beads || !beads.available) {
    throw new Error("open-items reported beads unavailable");
  }
  return {
    code: project.code,
    root,
    summary: {
      total_open: beads.total_open,
      by_status: beads.by_status,
      active_epics: beads.active_epics,
      active_proposal_linked: beads.active_proposal_linked,
    },
    bucket_counts: beads.bucket_counts,
    top_items: topItems(beads.items ?? []),
  };
}

export async function collectOpenItems(): Promise<OpenItemsScan> {
  const repos: OpenItemsRepoResult[] = [];
  const errors: OpenItemsError[] = [];

  let projects: ProjectEntry[];
  let base: string;
  try {
    const toml = await loadProjectsToml();
    projects = toml.projects;
    base = resolveLocalBase(toml.defaults);
  } catch (err) {
    return {
      repos: [],
      errors: [
        {
          repo: "projects.toml",
          error: err instanceof Error ? err.message : String(err),
        },
      ],
    };
  }

  for (const project of projects) {
    const root = resolveProjectRoot(project, base);
    if (ARCHIVE_PATH_RE.test(root)) continue;
    if (!existsSync(root)) continue;
    if (!existsSync(join(root, ".beads"))) continue;

    try {
      repos.push(await scanRepo(project, root));
    } catch (err) {
      errors.push({
        repo: project.code,
        error: err instanceof Error ? err.message : String(err),
      });
    }
  }

  return { repos, errors };
}
