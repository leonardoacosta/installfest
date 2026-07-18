#!/usr/bin/env bun
/**
 * daily-brief CLI entry.
 *
 * Subcommands:
 *   collect   — run the full collector (sources/mx.ts + openItems.ts +
 *               docsState.ts via collect.ts), write the dated + latest
 *               snapshot, print it. `--open-widget` then delivers it into
 *               the most-recently-active Zellij (or tmux fallback) session
 *               via widgetOpen.ts.
 *   view      — render the ink TUI over `~/.local/state/daily-brief/
 *               latest.json`. `--plain` instead prints plainRender.ts's
 *               static ANSI-free render and never mounts ink.
 */

import { homedir } from "node:os";
import { join } from "node:path";
import { render } from "ink";
import { collect, type DailyBriefSnapshot } from "./collect";
import { renderPlainSnapshot } from "./plainRender";
import App from "./ui/App";
import { openWidget } from "./widgetOpen";

const LATEST_SNAPSHOT_PATH = join(homedir(), ".local/state/daily-brief/latest.json");

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

async function runCollect(shouldOpenWidget: boolean): Promise<number> {
  const snapshot = await collect();
  console.log(JSON.stringify(snapshot, null, 2));
  if (shouldOpenWidget) {
    await openWidget();
  }
  return 0;
}

async function runView(plain: boolean): Promise<number> {
  const file = Bun.file(LATEST_SNAPSHOT_PATH);
  if (!(await file.exists())) {
    console.error(`No snapshot found at ${LATEST_SNAPSHOT_PATH}. Run "daily-brief collect" first.`);
    return 1;
  }

  const snapshot = (await file.json()) as DailyBriefSnapshot;

  if (plain) {
    console.log(renderPlainSnapshot(snapshot));
    return 0;
  }

  const instance = render(<App snapshot={snapshot} />);
  await instance.waitUntilExit();
  return 0;
}

async function main(): Promise<number> {
  const { subcommand, openWidget: shouldOpenWidget, plain } = parseArgs(process.argv.slice(2));

  switch (subcommand) {
    case "collect":
      return runCollect(shouldOpenWidget);
    case "view":
      return runView(plain);
    default:
      console.error(
        `Usage: daily-brief <collect|view> [--open-widget] [--plain]\nGot: ${subcommand ?? "(no subcommand)"}`,
      );
      return 1;
  }
}

const exitCode = await main();
process.exit(exitCode);
