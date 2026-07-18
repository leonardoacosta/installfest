/**
 * tree.ts — tiny hermetic-fixture builder for ctx-scan tests.
 *
 * Fixtures are built in the OS tmpdir (never inside the repo), so gitignore
 * rules (`.worktrees`, `.git`, `dist`, `build`) and chezmoi's dotfile
 * management can never eat a committed fixture directory and silently turn a
 * phantom-exclusion assertion into a trivial pass. This mirrors the sibling
 * `apps/daily-brief` idiom of mkdtemp-based throwaway fixtures.
 */
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";

/** Create a fresh throwaway root under the OS tmpdir. */
export function tmpRoot(prefix: string): string {
  return mkdtempSync(join(tmpdir(), prefix));
}

/** Write a file at `root/rel`, creating parent directories as needed. */
export function file(root: string, rel: string, content = ""): void {
  const p = join(root, rel);
  mkdirSync(dirname(p), { recursive: true });
  writeFileSync(p, content);
}

/** Create a directory at `root/rel` (recursive). */
export function dir(root: string, rel: string): void {
  mkdirSync(join(root, rel), { recursive: true });
}

/** Recursively remove a throwaway root. */
export function cleanup(root: string): void {
  rmSync(root, { recursive: true, force: true });
}
