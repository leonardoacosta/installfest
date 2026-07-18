/**
 * settings-resolver.test.ts — 5-layer precedence + malformed-file resilience
 * (ctx-scan-core task [4.3], beads:if-7gyx).
 */
import { afterEach, describe, expect, test } from "bun:test";
import { join } from "node:path";
import { resolveSettings } from "../src/settings-resolver";
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

describe("resolveSettings — precedence [4.3]", () => {
  test("local settings win over project settings win over user settings", () => {
    const projectPath = tmp("ctx-scan-settings-");
    const userDir = tmp("ctx-scan-user-");

    dir(projectPath, ".claude");
    file(
      projectPath,
      ".claude/settings.local.json",
      JSON.stringify({ sharedKey: "from-local", localOnlyKey: "local" }),
    );
    file(
      projectPath,
      ".claude/settings.json",
      JSON.stringify({ sharedKey: "from-project", projectOnlyKey: "project" }),
    );
    file(userDir, "settings.json", JSON.stringify({ sharedKey: "from-user", userOnlyKey: "user" }));
    const userSettingsPath = join(userDir, "settings.json");

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

  test("malformed JSON reports parseError and never throws; other layers still resolve", () => {
    const projectPath = tmp("ctx-scan-settings-malformed-");
    const userDir = tmp("ctx-scan-user-malformed-");

    dir(projectPath, ".claude");
    // Malformed: local settings is broken JSON.
    file(projectPath, ".claude/settings.local.json", "{ not valid json");
    file(projectPath, ".claude/settings.json", JSON.stringify({ ok: "from-project" }));
    file(userDir, "settings.json", JSON.stringify({ ok: "from-user" }));
    const userSettingsPath = join(userDir, "settings.json");

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

  test("missing files are absent, not errors", () => {
    const projectPath = tmp("ctx-scan-settings-missing-");
    const result = resolveSettings(projectPath, { userSettingsPath: join(projectPath, "nonexistent.json") });
    for (const layer of result.layers) {
      if (layer.path === null) continue; // synthetic managed/CLI tiers
      expect(layer.present).toBe(false);
      expect(layer.parseError).toBeNull();
    }
    expect(Object.keys(result.resolved)).toHaveLength(0);
  });
});
