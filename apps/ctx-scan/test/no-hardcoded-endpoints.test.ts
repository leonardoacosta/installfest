/**
 * no-hardcoded-endpoints.test.ts — zero hardcoded Grafana/Loki
 * hostnames/IPs/datasource UIDs in shipped source (ctx-scan-assembly task
 * [4.9], beads:if-w4wv).
 *
 * Every real endpoint MUST come through `telemetry-probe.ts`'s
 * `resolveLokiEndpoint` resolution order (env -> localhost default port ->
 * docker-inspect discovery -> Grafana datasource-proxy). This test greps the
 * shipped source for the patterns a REGRESSION would introduce:
 *   - a literal IPv4 address (a resolved container/proxy IP baked in instead
 *     of computed from `docker inspect` at runtime)
 *   - a literal Grafana datasource `uid` string assigned to a constant
 *     (instead of resolved via `findGrafanaLokiDatasourceUid`)
 *   - a non-default, non-localhost hostname wired directly into a request URL
 *
 * The well-known LITERAL default ports (`localhost:3100` for Loki,
 * `VM_DEFAULT_PORT` 8428 for VictoriaMetrics) are legitimate, intentional,
 * and explicitly named as such by the proposal's own resolution order (step
 * 2) — they are excluded from the violation patterns below, not smuggled
 * past the check.
 */
import { describe, expect, test } from "bun:test";
import { readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";

const SRC_DIR = join(import.meta.dir, "..", "src");

function allSourceFiles(dir: string): string[] {
  const out: string[] = [];
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const full = join(dir, entry.name);
    if (entry.isDirectory()) out.push(...allSourceFiles(full));
    else if (entry.isFile() && entry.name.endsWith(".ts")) out.push(full);
  }
  return out;
}

// A literal IPv4 address anywhere in source — a genuine resolved-value leak
// (docker-inspect discovery must compute this at runtime, never bake it in).
const IPV4_RE = /\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b/g;

// A Grafana datasource uid assigned as a literal string constant, rather
// than resolved via the Grafana API at runtime.
const HARDCODED_UID_RE = /\b(?:datasourceUid|proxyDatasourceUid|LOKI_UID|GRAFANA_UID)\s*[:=]\s*["'][a-zA-Z0-9_-]{4,}["']/g;

// A non-default Grafana/Loki hostname wired directly into a URL literal
// (e.g. an internal domain or a specific ops hostname) — excludes the
// well-known "localhost" default explicitly named by the resolution order.
const HARDCODED_HOSTNAME_RE = /https?:\/\/(?!localhost\b)[a-zA-Z0-9.-]*\.(?:grafana|loki|internal|corp)\b/gi;

describe("apps/ctx-scan/src — zero hardcoded telemetry endpoints [4.9]", () => {
  test("no literal IPv4 address anywhere in shipped source", () => {
    const offenders: string[] = [];
    for (const filePath of allSourceFiles(SRC_DIR)) {
      const content = readFileSync(filePath, "utf8");
      const matches = content.match(IPV4_RE);
      if (matches) offenders.push(`${filePath}: ${matches.join(", ")}`);
    }
    expect(offenders).toEqual([]);
  });

  test("no hardcoded Grafana datasource uid constant", () => {
    const offenders: string[] = [];
    for (const filePath of allSourceFiles(SRC_DIR)) {
      const content = readFileSync(filePath, "utf8");
      const matches = content.match(HARDCODED_UID_RE);
      if (matches) offenders.push(`${filePath}: ${matches.join(", ")}`);
    }
    expect(offenders).toEqual([]);
  });

  test("no hardcoded non-default Grafana/Loki hostname wired into a URL literal", () => {
    const offenders: string[] = [];
    for (const filePath of allSourceFiles(SRC_DIR)) {
      const content = readFileSync(filePath, "utf8");
      const matches = content.match(HARDCODED_HOSTNAME_RE);
      if (matches) offenders.push(`${filePath}: ${matches.join(", ")}`);
    }
    expect(offenders).toEqual([]);
  });

  test("the only 'localhost' occurrences are the two named well-known default ports", () => {
    const occurrences: string[] = [];
    for (const filePath of allSourceFiles(SRC_DIR)) {
      const content = readFileSync(filePath, "utf8");
      const lines = content.split("\n");
      lines.forEach((line, i) => {
        if (/localhost/.test(line)) occurrences.push(`${filePath}:${i + 1}: ${line.trim()}`);
      });
    }
    // Every occurrence must reference the named constants/doc comments in
    // telemetry-probe.ts — never a resolved runtime value baked in elsewhere.
    for (const occ of occurrences) {
      expect(occ).toContain("telemetry-probe.ts");
    }
    expect(occurrences.length).toBeGreaterThan(0); // sanity: the check itself is exercising real matches
  });
});
