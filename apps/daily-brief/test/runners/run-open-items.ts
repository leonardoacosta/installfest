#!/usr/bin/env bun
/**
 * Subprocess runner: imports the REAL `collectOpenItems` export fresh (a new
 * Bun process, so its module-level `DOTFILES`-derived constants pick up
 * whatever env this process was spawned with) and prints its JSON result.
 *
 * Spawned by test/openItems.test.ts and test/collect.test.ts rather than
 * imported in-process, because `bun test` runs all test files in one shared
 * process (verified empirically) and openItems.ts computes its
 * `PROJECTS_TOML_PATH` constant once at module-load time from
 * `process.env.DOTFILES` — a fresh subprocess per scenario is the only way
 * to guarantee each test's DOTFILES override is actually honored, and it
 * matches how this module is really invoked in production (a fresh `bun`
 * process per collect run).
 */
import { collectOpenItems } from "../../src/sources/openItems";

const result = await collectOpenItems();
console.log(JSON.stringify(result));
