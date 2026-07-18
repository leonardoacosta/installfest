#!/usr/bin/env bun
/**
 * cli.ts — `ctx-scan` command entrypoint (commander.js).
 *
 * Consumer layer: wires the `scan` subcommand onto the discovery + settings
 * building blocks. This proposal (`ctx-scan-core`) proves discovery + document
 * shape wire up end-to-end — per-Node `effective_chars`/`truncations`/`bands`
 * and the `global`/`surfaces` content assembly land in `ctx-scan-assembly`, so
 * `global` and each `Project.surfaces` are emitted empty here by design.
 */

import { writeFileSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";
import { Command } from "commander";

import { discoverProjects, getGlobalLayer } from "./discovery";
import { schemaVersion, type Fleet } from "./model";

/** Expand a leading `~` / `~/…` to the current user's home directory. */
function expandHome(p: string): string {
  if (p === "~") return homedir();
  if (p.startsWith("~/")) return join(homedir(), p.slice(2));
  return p;
}

interface ScanOptions {
  root: string;
  json?: string;
}

/** Assemble the Fleet document for `root`. Content-empty per this proposal's scope. */
export function buildFleet(root: string): Fleet {
  const global = getGlobalLayer();
  const projects = discoverProjects(root, { globalPath: global.path });
  return {
    schemaVersion,
    root,
    global: [],
    projects: projects.map((p) => ({ path: p.path, name: p.name, surfaces: [] })),
  };
}

function runScan(opts: ScanOptions): void {
  const root = expandHome(opts.root);
  const fleet = buildFleet(root);
  const doc = JSON.stringify(fleet, null, 2);
  if (opts.json) {
    writeFileSync(expandHome(opts.json), doc + "\n", "utf8");
  } else {
    process.stdout.write(doc + "\n");
  }
}

const program = new Command();

program
  .name("ctx-scan")
  .description("Measure what a Claude Code session loads per project across the fleet.");

program
  .command("scan")
  .description("Discover projects under --root and emit the Fleet document.")
  .option("--root <path>", "root directory to scan", "~/dev")
  .option("--json <path>", "write JSON to this file (default: stdout)")
  .action((opts: ScanOptions) => runScan(opts));

program.parse();
