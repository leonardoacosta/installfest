#!/usr/bin/env bun
/**
 * daily-brief CLI entry.
 *
 * Subcommands:
 *   collect   — run the full collector (sources/mx.ts + openItems.ts +
 *               docsState.ts via collect.ts), write the dated + latest
 *               snapshot, print it.
 *   view      — render the ink TUI over the latest snapshot. NOT YET
 *               IMPLEMENTED — UI lands in a later batch (tasks 3.x). This
 *               stub exists so the CLI shape (and --open-widget/--plain
 *               flags) is stable for that batch to fill in.
 *
 * Flags `--open-widget` and `--plain` are parsed but are no-ops until the
 * UI/widget batches land (tasks 3.3/3.1 respectively).
 */

import { collect } from "./collect";

interface ParsedArgs {
  subcommand: string | undefined;
  openWidget: boolean;
  plain: boolean;
}

function parseArgs(argv: string[]): ParsedArgs {
  const [subcommand, ...rest] = argv;
  return {
    subcommand,
    openWidget: rest.includes("--open-widget"),
    plain: rest.includes("--plain"),
  };
}

async function runCollect(): Promise<number> {
  const snapshot = await collect();
  console.log(JSON.stringify(snapshot, null, 2));
  return 0;
}

function runView(): number {
  // src/ui/* (ink App/sections/plainRender) is a later batch (tasks 3.x).
  console.log("daily-brief view: not yet implemented (UI lands in a later batch)");
  return 0;
}

async function main(): Promise<number> {
  const { subcommand } = parseArgs(process.argv.slice(2));

  switch (subcommand) {
    case "collect":
      return runCollect();
    case "view":
      return runView();
    default:
      console.error(
        `Usage: daily-brief <collect|view> [--open-widget] [--plain]\nGot: ${subcommand ?? "(no subcommand)"}`,
      );
      return 1;
  }
}

const exitCode = await main();
process.exit(exitCode);
