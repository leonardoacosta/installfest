#!/usr/bin/env bun
/**
 * Subprocess runner: imports the REAL `collectDocsState` export fresh (see
 * run-open-items.ts's header comment for why this runs as a subprocess
 * rather than an in-process import — docsState.ts computes its
 * `RESULTS_JSONL_PATH`/`SWEEP_LAST_RUN_PATH` constants once at module-load
 * time from process.env) and prints its JSON result.
 */
import { collectDocsState } from "../../src/sources/docsState";

const result = await collectDocsState();
console.log(JSON.stringify(result));
