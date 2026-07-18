#!/usr/bin/env bun
/**
 * Subprocess runner: invokes the REAL `snoozeTriageItem`/`setTriageStatus`
 * exports fresh (see run-open-items.ts's header comment for why — mx.ts's
 * `MX_BASE_URL`/`GATEWAY_ENV_PATH` constants are computed once at
 * module-load time from env vars). Usage:
 *
 *   bun run run-mx-action.ts <snooze|status> <id>
 */
import { setTriageStatus, snoozeTriageItem } from "../../src/sources/mx";

const [action, id] = process.argv.slice(2);

let result: unknown;
if (action === "snooze") {
  result = await snoozeTriageItem(id ?? "fixture-item");
} else if (action === "status") {
  result = await setTriageStatus(id ?? "fixture-item", "resolved");
} else {
  console.error(`Usage: run-mx-action.ts <snooze|status> <id>. Got: ${action ?? "(none)"}`);
  process.exit(1);
}

console.log(JSON.stringify(result));
