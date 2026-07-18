#!/usr/bin/env bun
/**
 * Subprocess runner: imports the REAL `collect()` export fresh (see
 * run-open-items.ts's header comment for why this runs as a subprocess
 * rather than an in-process import) and prints the resulting snapshot JSON.
 * `collect()` itself already writes the dated + latest snapshot files to
 * `DAILY_BRIEF_STATE_DIR` (or the real state dir if unset) as a side
 * effect — tests always set `DAILY_BRIEF_STATE_DIR` to a scratch tmp dir.
 */
import { collect } from "../../src/collect";

const snapshot = await collect();
console.log(JSON.stringify(snapshot));
