/**
 * settings-resolver.test.ts — 5-layer precedence + malformed-file resilience
 * (ctx-scan-core task [4.3], beads:if-7gyx).
 */
import { afterEach, describe, expect, test } from "bun:test";
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { resolveSettings } from "../src/settings-resolver";

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

describe("resolveSettings — precedence [4.3]", () => {
  test("local settings win over project settings win over user settings", async () => {
    const projectPath = await tmp("ctx-scan-settings-");
    const userDir = await tmp("ctx-scan-user-");

    await mkdir(join(projectPath, ".claude"), { recursive: true });
    await writeFile(
      join(projectPath, ".claude", "settings.local.json"),
      JSON.stringify({ sharedKey: "from-local", localOnlyKey: "local" }),
    );
    await writeFile(
      join(projectPath, ".claude", "settings.json"),
      JSON.stringify({ sharedKey: "from-project", projectOnlyKey: "project" }),
    );
    const userSettingsPath = join(userDir, "settings.json");
    await writeFile(userSettingsPath, JSON.stringify({ sharedKey: "from-user", userOnlyKey: "user" }));

    const result = resolveSettings(projectPath, { userSettingsPath });

    // A key set at all three real layers: local wins.
    expect(result.resolved.sharedKey?.value).toBe("from-local");
    expect(result.resolved.sharedKey?.layer).toBe(".claude/settings.local.json");

    // Keys unique to each layer still resolve, from their own layer.
    expect(result.resolved.localOnlyKey?.value).toBe("local");
    expect(result.resolved.projectOnlyKey?.value).toBe("project");
    expect(result.resolved.userOnlyKey?.value).toBe("user");

    // Layer read-outcomes are reported for every tier in precedence order.
    expect(result.layers.map((l) => l.layer)).toEqual([
      "managed",
      "CLI",
      ".claude/settings.local.json",
      ".claude/settings.json",
      ".mcp.json",
      "mcp.json",
      "~/.claude/settings.json",
    ]);
  });

  test("malformed JSON reports parseError and never throws; other layers still resolve", async () => {
    const projectPath = await tmp("ctx-scan-settings-malformed-");
    const userDir = await tmp("ctx-scan-user-malformed-");

    await mkdir(join(projectPath, ".claude"), { recursive: true });
    // Malformed: local settings is broken JSON.
    await writeFile(join(projectPath, ".claude", "settings.local.json"), "{ not valid json");
    await writeFile(join(projectPath, ".claude", "settings.json"), JSON.stringify({ ok: "from-project" }));
    const userSettingsPath = join(userDir, "settings.json");
    await writeFile(userSettingsPath, JSON.stringify({ ok: "from-user" }));

    expect(() => resolveSettings(projectPath, { userSettingsPath })).not.toThrow();

    const result = resolveSettings(projectPath, { userSettingsPath });
    const localRead = result.layers.find((l) => l.layer === ".claude/settings.local.json");
    expect(localRead?.present).toBe(true);
    expect(localRead?.parseError).not.toBeNull();

    // The malformed layer contributed nothing; the next real layer down (project
    // settings) supplies the value instead.
    expect(result.resolved.ok?.value).toBe("from-project");
    expect(result.resolved.ok?.layer).toBe(".claude/settings.json");
  });

  test("missing files are absent, not errors", async () => {
    const projectPath = await tmp("ctx-scan-settings-missing-");
    const result = resolveSettings(projectPath, { userSettingsPath: join(projectPath, "nonexistent.json") });
    for (const layer of result.layers) {
      if (layer.path === null) continue; // synthetic managed/CLI tiers
      expect(layer.present).toBe(false);
      expect(layer.parseError).toBeNull();
    }
    expect(Object.keys(result.resolved)).toHaveLength(0);
  });
});
