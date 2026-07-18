/**
 * memory-mcp-caps.test.ts — MEMORY.md (200 lines / 25KB) and MCP-description
 * (2KB) caps: capped `effective` value AND retained uncapped `raw` size both
 * correct (ctx-scan-assembly task [4.3], beads:if-qwvt).
 *
 * Unit-level cases nail exact byte/char arithmetic for each binding cap
 * (line-cap only, byte-cap only, both stacked) directly against
 * `truncation.ts`. A pipeline-level case threads an oversized MEMORY.md and
 * an oversized `.mcp.json` description through the real
 * `assembleProjectSurfaces` pipeline to confirm the same numbers survive
 * intact onto the emitted `Node`s.
 */
import { afterEach, describe, expect, test } from "bun:test";
import { join } from "node:path";
import {
  MCP_DESCRIPTION_CAP_BYTES,
  MEMORY_MD_MAX_BYTES,
  MEMORY_MD_MAX_LINES,
  capMcpDescription,
  capMemoryMd,
} from "../src/truncation";
import { assembleProjectSurfaces } from "../src/pipeline";
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

describe("capMemoryMd — line-cap and byte-cap arithmetic [4.3]", () => {
  test("line-cap binds: 300 short lines, well under 25KB", () => {
    // Every line the exact same length (10 chars) for exact arithmetic.
    const uniformLines = Array.from({ length: 300 }, () => "x".repeat(10));
    const content = uniformLines.join("\n");
    expect(content.split("\n")).toHaveLength(300);

    const cap = capMemoryMd(content);

    expect(cap.truncation.raw).toBe(content.length);
    const expectedEffective = uniformLines.slice(0, MEMORY_MD_MAX_LINES).join("\n");
    expect(cap.effective).toBe(expectedEffective);
    expect(cap.truncation.effective).toBe(expectedEffective.length);
    expect(cap.effective.split("\n")).toHaveLength(MEMORY_MD_MAX_LINES);
    expect(Buffer.byteLength(cap.effective, "utf8")).toBeLessThan(MEMORY_MD_MAX_BYTES);
    // Raw (uncapped) size is strictly larger than the capped effective size.
    expect(cap.truncation.raw).toBeGreaterThan(cap.truncation.effective);
  });

  test("byte-cap binds: 50 long lines (under 200 lines), over 25KB", () => {
    const uniformLines = Array.from({ length: 50 }, () => "y".repeat(700));
    const content = uniformLines.join("\n");
    expect(content.split("\n")).toHaveLength(50); // under the 200-line cap
    expect(Buffer.byteLength(content, "utf8")).toBeGreaterThan(MEMORY_MD_MAX_BYTES);

    const cap = capMemoryMd(content);

    expect(cap.truncation.raw).toBe(content.length);
    expect(cap.truncation.effective).toBe(MEMORY_MD_MAX_BYTES);
    expect(cap.effective).toBe(content.slice(0, MEMORY_MD_MAX_BYTES));
    expect(Buffer.byteLength(cap.effective, "utf8")).toBe(MEMORY_MD_MAX_BYTES);
  });

  test("both caps stack: 300 long lines over both thresholds", () => {
    const uniformLines = Array.from({ length: 300 }, () => "z".repeat(200));
    const content = uniformLines.join("\n");
    expect(content.split("\n")).toHaveLength(300); // over 200-line cap
    const afterLineCut = uniformLines.slice(0, MEMORY_MD_MAX_LINES).join("\n");
    expect(Buffer.byteLength(afterLineCut, "utf8")).toBeGreaterThan(MEMORY_MD_MAX_BYTES); // still over byte cap

    const cap = capMemoryMd(content);

    expect(cap.truncation.raw).toBe(content.length);
    expect(cap.truncation.effective).toBe(MEMORY_MD_MAX_BYTES);
    // The 200-line cut is itself a verbatim prefix of the raw content, so the
    // final byte-capped result equals the raw content's own first N chars.
    expect(cap.effective).toBe(content.slice(0, MEMORY_MD_MAX_BYTES));
  });

  test("content within both caps is untouched", () => {
    const content = ["small", "memory", "file"].join("\n");
    const cap = capMemoryMd(content);
    expect(cap.effective).toBe(content);
    expect(cap.truncation.raw).toBe(content.length);
    expect(cap.truncation.effective).toBe(content.length);
  });
});

describe("capMcpDescription — 2KB byte cap [4.3]", () => {
  test("oversized multi-byte description caps at exactly 2048 bytes without splitting a char", () => {
    // 2040 ASCII bytes + 5 four-byte emoji (20 bytes) = 2060 bytes raw.
    // The 2048-byte cut lands exactly 8 bytes into the emoji run (2 whole
    // emoji), so no multi-byte character is split — a deliberately clean
    // boundary for an exact assertion.
    const text = "A".repeat(2040) + "\u{1F680}".repeat(5); // rocket emoji
    const rawBytes = Buffer.byteLength(text, "utf8");
    expect(rawBytes).toBe(2060);

    const cap = capMcpDescription(text);

    expect(cap.truncation.raw).toBe(2060);
    expect(cap.truncation.effective).toBe(MCP_DESCRIPTION_CAP_BYTES);
    expect(cap.effective).toBe("A".repeat(2040) + "\u{1F680}".repeat(2));
    expect(Buffer.byteLength(cap.effective, "utf8")).toBe(MCP_DESCRIPTION_CAP_BYTES);
  });

  test("description under the cap is untouched, raw === effective", () => {
    const text = "a short MCP tool description";
    const cap = capMcpDescription(text);
    expect(cap.effective).toBe(text);
    expect(cap.truncation.raw).toBe(Buffer.byteLength(text, "utf8"));
    expect(cap.truncation.effective).toBe(cap.truncation.raw);
  });
});

describe("pipeline-level: oversized MEMORY.md + MCP description survive intact onto Nodes [4.3]", () => {
  test(
    "assembleProjectSurfaces reports the same raw/effective numbers computed independently",
    async () => {
      const claudeHome = tmp("ctx-scan-caps-claudehome-");
      const projectPath = tmp("ctx-scan-caps-project-");

      const memoryContent = Array.from({ length: 50 }, () => "m".repeat(700)).join("\n");
      const expectedMemoryCap = capMemoryMd(memoryContent);

      const slug = projectPath.replace(/\//g, "-");
      file(claudeHome, join("projects", slug, "memory", "MEMORY.md"), memoryContent);

      const mcpDescription = "A".repeat(2040) + "\u{1F680}".repeat(5);
      const expectedMcpCap = capMcpDescription(mcpDescription);

      dir(projectPath, ".git");
      file(
        projectPath,
        ".mcp.json",
        JSON.stringify({ mcpServers: { fakeServer: { description: mcpDescription } } }),
      );

      const result = await assembleProjectSurfaces(projectPath, claudeHome, {});

      const memorySurface = result.surfaces.find((s) => s.cls === "memory");
      expect(memorySurface).toBeDefined();
      expect(memorySurface!.nodes).toHaveLength(1);
      const memoryNode = memorySurface!.nodes[0]!;
      expect(memoryNode.raw_chars).toBe(expectedMemoryCap.truncation.raw);
      expect(memoryNode.effective_chars).toBe(expectedMemoryCap.truncation.effective);
      expect(memoryNode.effective_chars).toBe(MEMORY_MD_MAX_BYTES);
      expect(memoryNode.truncations).toHaveLength(1);
      expect(memoryNode.truncations[0]!.cap).toBe("memory-md");

      const mcpSurface = result.surfaces.find((s) => s.cls === "mcp-tools");
      expect(mcpSurface).toBeDefined();
      const mcpNode = mcpSurface!.nodes.find((n) => n.path.endsWith("#fakeServer"));
      expect(mcpNode).toBeDefined();
      expect(mcpNode!.raw_chars).toBe(expectedMcpCap.truncation.raw);
      expect(mcpNode!.effective_chars).toBe(expectedMcpCap.truncation.effective);
      expect(mcpNode!.effective_chars).toBe(MCP_DESCRIPTION_CAP_BYTES);
    },
    15_000,
  );
});
