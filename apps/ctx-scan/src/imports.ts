/**
 * imports.ts — CLAUDE.md @import chain walker (ctx-scan-assembly task [2.1],
 * beads:if-714m).
 *
 * Resolves `@path/to/file.md`-style import directives up to 4 hops deep
 * (root CLAUDE.md -> import -> import -> import -> import), skipping any
 * `@`-prefixed token that appears inside a fenced code block (``` or ~~~) —
 * those are documentation examples, not real imports. Verified against the
 * real syntax this repo and ~/dev/cc actually use: a standalone `@relative/path.md`
 * line (see ~/dev/cc/CLAUDE.md's `@rules/CORE.md` / `@rules/BEADS.md`).
 */
import { existsSync, readFileSync } from "node:fs";
import { dirname, join } from "node:path";

const MAX_HOPS = 4;

/** One resolved import in the chain. */
export interface ResolvedImport {
  /** Absolute path of the imported file. */
  path: string;
  /** 1-indexed hop depth (the root file's direct imports are depth 1). */
  depth: number;
  /** Absolute path of the file that imported this one. */
  importedFrom: string;
}

/** Strip fenced code blocks (``` or ~~~, any info string) so @-tokens inside them are ignored. */
function stripCodeFences(content: string): string {
  const lines = content.split("\n");
  const out: string[] = [];
  let inFence = false;
  let fenceMarker = "";
  for (const line of lines) {
    const trimmed = line.trimStart();
    const fenceMatch = /^(`{3,}|~{3,})/.exec(trimmed);
    if (fenceMatch) {
      const marker = fenceMatch[1]![0]!.repeat(3); // normalize to the 3-char marker char
      if (!inFence) {
        inFence = true;
        fenceMarker = marker;
        out.push(""); // blank the fence line itself too — no @-tokens expected there
        continue;
      }
      if (inFence && trimmed.startsWith(fenceMarker[0]!.repeat(3))) {
        inFence = false;
        out.push("");
        continue;
      }
    }
    out.push(inFence ? "" : line);
  }
  return out.join("\n");
}

/** Extract `@relative/path` import tokens from one file's (fence-stripped) content. */
function extractImportPaths(content: string): string[] {
  const stripped = stripCodeFences(content);
  const paths: string[] = [];
  // A standalone import line: optional leading whitespace, `@`, a relative
  // path (no spaces), end of line. This matches the real convention
  // (`@rules/CORE.md`) without over-matching prose that merely mentions `@`.
  const lineRe = /^\s*@([^\s@][^\s]*)\s*$/;
  for (const line of stripped.split("\n")) {
    const m = lineRe.exec(line);
    if (m) paths.push(m[1]!);
  }
  return paths;
}

/**
 * Walk the `@import` chain starting from `rootPath`, up to `MAX_HOPS` deep.
 * Returns every resolved import in traversal order. A missing target file is
 * silently skipped (not an error — a dangling @import shouldn't crash the
 * scan), and a cycle is guarded via a visited-path set.
 */
export function resolveImportChain(rootPath: string): ResolvedImport[] {
  const resolved: ResolvedImport[] = [];
  const visited = new Set<string>([rootPath]);

  let frontier: string[] = [rootPath];
  for (let depth = 1; depth <= MAX_HOPS && frontier.length > 0; depth++) {
    const next: string[] = [];
    for (const filePath of frontier) {
      if (!existsSync(filePath)) continue;
      let content: string;
      try {
        content = readFileSync(filePath, "utf8");
      } catch {
        continue;
      }
      const importPaths = extractImportPaths(content);
      const baseDir = dirname(filePath);
      for (const rel of importPaths) {
        const abs = join(baseDir, rel);
        if (visited.has(abs)) continue; // cycle guard
        visited.add(abs);
        resolved.push({ path: abs, depth, importedFrom: filePath });
        next.push(abs);
      }
    }
    frontier = next;
  }

  return resolved;
}
