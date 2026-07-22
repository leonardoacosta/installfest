/**
 * discovery.ts — fleet discovery under a --root directory.
 *
 * Walks a directory tree identifying project roots (any dir containing
 * `CLAUDE.md`, `.claude/`, or `.mcp.json`), deduping each to its outermost
 * containing git root, while pruning a hard exclusion list at descent time
 * (never as a post-filter) and guarding symlink cycles via a realpath-based
 * visited set.
 *
 * Also owns global-layer identification ([2.3]): `~/.claude` is resolved via
 * `realpath` (following the `~/dev/cc` symlink target on this machine) and
 * excluded from the discovered-project list even though it matches the
 * discovery predicate — the global layer is counted exactly once, never once
 * per project.
 */

import { existsSync, readdirSync, realpathSync, statSync, type Dirent } from "node:fs";
import { basename, dirname, join, sep } from "node:path";
import { homedir } from "node:os";

/** A discovered project root. */
export interface DiscoveredProject {
  /** Absolute project-root path (outermost containing git root). */
  path: string;
  /** Display name — the project directory basename. */
  name: string;
}

/** The global `~/.claude` layer, resolved once. */
export interface GlobalLayer {
  /** Realpath of `~/.claude` (its symlink target where present). */
  path: string;
  /** Display name — basename of the resolved path. */
  name: string;
  /** Always `"global"` — matches `model.ts`'s `NodeOrigin`. */
  origin: "global";
}

/** Directory names excluded at any depth (exact match). */
const EXCLUDE_EXACT = new Set([
  "node_modules",
  ".git",
  "archived",
  ".worktrees",
  "dist",
  "build",
]);

/** Project-root markers — any one present makes a directory a candidate. */
const MARKERS = ["CLAUDE.md", ".claude", ".mcp.json"];

/** `archive*` / `*-archive` glob patterns plus the exact-name set. */
function nameExcluded(name: string): boolean {
  if (EXCLUDE_EXACT.has(name)) return true;
  if (name.startsWith("archive")) return true; // archive*
  if (name.endsWith("-archive")) return true; // *-archive
  return false;
}

/** Two-segment exclusions: `plugins/cache`, `plugins/marketplaces`. */
function pathExcluded(parentName: string, name: string): boolean {
  return parentName === "plugins" && (name === "cache" || name === "marketplaces");
}

function isProjectRoot(dir: string): boolean {
  return MARKERS.some((m) => existsSync(join(dir, m)));
}

export function safeRealpath(p: string): string | null {
  try {
    return realpathSync(p);
  } catch {
    return null;
  }
}

/**
 * True when `child` is `parent` or lives beneath it. Canonical containment
 * check — any filesystem-walking feature in ctx-scan (discovery's own walk,
 * `imports.ts`'s `@import` chain resolution, and any future walker) must use
 * this, never a second implementation. Callers are responsible for resolving
 * symlinks (`safeRealpath`) before comparing when symlink-escape matters —
 * this function itself does plain string comparison.
 */
export function isWithin(child: string, parent: string): boolean {
  return child === parent || child.startsWith(parent + sep);
}

/** Follows symlinks so a symlinked directory still descends. */
function isDirLike(entry: Dirent, parent: string): boolean {
  if (entry.isDirectory()) return true;
  if (entry.isSymbolicLink()) {
    try {
      return statSync(join(parent, entry.name)).isDirectory();
    } catch {
      return false;
    }
  }
  return false;
}

/**
 * Walk from `startDir` up to (and including) `rootReal`, returning the
 * OUTERMOST ancestor that is a git root, or null if none. Bottom-up walk means
 * the final `.git`-bearing assignment is the topmost one.
 */
function outermostGitRoot(startDir: string, rootReal: string): string | null {
  let cur = safeRealpath(startDir);
  if (!cur) return null;
  let outermost: string | null = null;
  while (isWithin(cur, rootReal)) {
    if (existsSync(join(cur, ".git"))) outermost = cur;
    if (cur === rootReal) break;
    const parent = dirname(cur);
    if (parent === cur) break; // filesystem root
    cur = parent;
  }
  return outermost;
}

/**
 * Resolve the global `~/.claude` layer path via realpath (following the
 * `~/dev/cc` symlink target where present). Falls back to the literal path if
 * the directory does not exist.
 */
export function resolveGlobalPath(homeDir: string = homedir()): string {
  const claudeDir = join(homeDir, ".claude");
  return safeRealpath(claudeDir) ?? claudeDir;
}

/** Identify the global layer per `model.ts`'s `NodeOrigin`. */
export function getGlobalLayer(homeDir: string = homedir()): GlobalLayer {
  const path = resolveGlobalPath(homeDir);
  return { path, name: basename(path), origin: "global" };
}

/**
 * Discover project roots under `root`. Exclusions are applied at descent time
 * (a pruned directory is never entered), symlink cycles are guarded by a
 * realpath visited set, each candidate collapses to its outermost containing
 * git root, results dedupe by realpath, and the global layer is excluded.
 */
export function discoverProjects(
  root: string,
  opts: { globalPath?: string } = {},
): DiscoveredProject[] {
  const rootReal = safeRealpath(root);
  if (!rootReal) return [];
  const globalReal = opts.globalPath ?? resolveGlobalPath();

  const visited = new Set<string>();
  const candidates: string[] = [];

  function walk(dir: string, parentName: string): void {
    const real = safeRealpath(dir);
    if (!real || visited.has(real)) return; // cycle guard
    visited.add(real);

    if (isProjectRoot(dir)) candidates.push(dir);

    let entries: Dirent[];
    try {
      entries = readdirSync(dir, { withFileTypes: true });
    } catch {
      return;
    }
    for (const entry of entries) {
      if (!isDirLike(entry, dir)) continue;
      const name = entry.name;
      if (nameExcluded(name) || pathExcluded(parentName, name)) continue;
      const child = join(dir, name);
      // Containment check (harden-ctx-scan-fs-boundaries): a symlink pointing
      // outside `--root` must never be descended into or discovered as a
      // project. The `visited` cycle guard above is a separate concern (loop
      // termination) — this stops escape, not cycles.
      const childReal = safeRealpath(child);
      if (!childReal || !isWithin(childReal, rootReal)) continue;
      walk(child, name);
    }
  }

  walk(root, basename(rootReal));

  const seen = new Set<string>();
  const out: DiscoveredProject[] = [];
  for (const candidate of candidates) {
    const gitRoot = outermostGitRoot(candidate, rootReal) ?? candidate;
    const real = safeRealpath(gitRoot) ?? gitRoot;
    if (real === globalReal) continue; // global layer never appears as a project
    if (seen.has(real)) continue; // dedupe by realpath
    seen.add(real);
    out.push({ path: gitRoot, name: basename(gitRoot) });
  }
  return out;
}
