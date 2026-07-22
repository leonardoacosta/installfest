/**
 * calibrate.ts — `ctx-scan calibrate` core logic (ctx-scan-assembly task
 * [2.7], beads:if-lmxg): parse a pasted `/context` output, and (default when
 * telemetry is reachable) fit the chars->tokens ratio from telemetry.
 *
 * This module implements the calibration LOGIC only. Registering the actual
 * `ctx-scan calibrate` commander subcommand and wiring its
 * `[--from-telemetry] [--json]` output shape is task [3.1] (UI batch) —
 * `cli.ts` is not in this proposal's touched-files list (see proposal.md
 * Context: touches assembly.ts/imports.ts/truncation.ts/calibrate.ts/
 * telemetry-probe.ts only) and is intentionally left to that task.
 *
 * ## `/context` parser: grounded in real captures, not guessed
 * `code.claude.com` does not publish `/context`'s exact text layout, so this
 * parser was built and verified against REAL `/context` invocations recorded
 * in this machine's own session transcripts (under `~/.claude/projects`,
 * `local_command` entries whose content starts with "Context Usage"),
 * spanning multiple models (`claude-sonnet-5`, `claude-opus-4-8[1m]`) and
 * context-window sizes (200k-class vs 1M). Confirmed stable across every
 * sample: after stripping ANSI color codes, each category line reduces to
 * `<label>: <value>(k|m)? tokens? (<pct>%)` (the "tokens" word is present on
 * every category EXCEPT "Free space", confirmed literally absent there in
 * every sample), and the total line reduces to
 * `<used>(k|m)?/<capacity>(k|m)? tokens (<pct>%)`. The parser matches on
 * these two shapes generically (any label text) rather than hardcoding the
 * specific category names observed, so it tolerates a category set that
 * shifts between Claude Code versions.
 *
 * ## `--from-telemetry` ratio fit: honestly scoped
 * The proposal's C4b sequence says to "pull the first `api_request` per
 * session's `cache_read_tokens`/`cache_creation_tokens`... and fit the
 * chars->tokens ratio against it." A live schema check against this
 * machine's real Loki (2026-07-18) confirmed the native `claude_code.
 * api_request` event carries no project/cwd attribute by default (matches
 * the documented gap: "Out-of-box there is no cwd/project attribute" per
 * `docs/explainers/nx-session-context-api-migration.md`) — so there is no
 * way to correlate an ARBITRARY historical session's tokens with a
 * particular project's char count without also depending on an opt-in
 * `OTEL_RESOURCE_ATTRIBUTES=project=<name>` convention this repo does not
 * yet require. Multi-session ratio fitting across projects is therefore
 * NOT attempted here (that would require fabricating a session->project
 * correlation this module cannot honestly make). What IS honestly available:
 * the CURRENT session's own id (`CLAUDE_CODE_SESSION_ID`/`CLAUDE_SESSION_ID`,
 * confirmed live env vars in a running Claude Code session) matched against
 * the CURRENT project's own CLAUDE.md `@import` chain char count (via [2.1]'s
 * `resolveImportChain`) — a single legitimate calibration point per
 * invocation, exactly as `[2.5]`'s probe module is designed to source it.
 * Fitting a ratio across many sessions/projects (should a project attribute
 * become standard) is future work for whichever module wires the
 * accumulated calibration history, not a gap in this task's scope.
 */
import { existsSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { resolveImportChain } from "./imports";
import { probeTelemetry, queryEvents, type Provenance } from "./telemetry-probe";

// ─────────────────────────────────────────────────────────────────────────
// Pasted `/context` output parsing
// ─────────────────────────────────────────────────────────────────────────

export interface ContextCategory {
  label: string;
  tokens: number;
  pct: number;
}

export interface ParsedContextOutput {
  totalUsedTokens: number | null;
  totalCapacityTokens: number | null;
  totalPct: number | null;
  categories: ContextCategory[];
}

const ANSI_RE = /\x1b\[[0-9;]*m/g;
const TOTAL_RE = /([\d,.]+)\s*([kKmM])?\s*\/\s*([\d,.]+)\s*([kKmM])?\s*tokens\s*\(([\d.]+)%\)/;
const CATEGORY_RE = /([A-Za-z][A-Za-z ]*[A-Za-z]):\s*([\d,.]+)\s*([kKmM])?\s*(?:tokens\s*)?\(([\d.]+)%\)/;

function tokenValue(numStr: string, unit?: string): number {
  const n = parseFloat(numStr.replace(/,/g, ""));
  if (!unit) return n;
  const u = unit.toLowerCase();
  if (u === "k") return n * 1_000;
  if (u === "m") return n * 1_000_000;
  return n;
}

/**
 * Parse a pasted `/context` command output (with or without its ANSI color
 * codes — both forms are tolerated) into the total usage line plus the
 * per-category breakdown. Lines that don't match either shape are ignored;
 * an input with zero recognizable lines returns an all-null/empty result
 * rather than throwing — the caller decides whether that counts as a
 * parse failure.
 */
export function parseContextOutput(text: string): ParsedContextOutput {
  const stripped = text.replace(ANSI_RE, "");
  const lines = stripped.split("\n");

  let totalUsedTokens: number | null = null;
  let totalCapacityTokens: number | null = null;
  let totalPct: number | null = null;
  const categories: ContextCategory[] = [];

  for (const line of lines) {
    const totalMatch = TOTAL_RE.exec(line);
    if (totalMatch) {
      totalUsedTokens = tokenValue(totalMatch[1]!, totalMatch[2]);
      totalCapacityTokens = tokenValue(totalMatch[3]!, totalMatch[4]);
      totalPct = parseFloat(totalMatch[5]!);
      continue;
    }
    const catMatch = CATEGORY_RE.exec(line);
    if (catMatch) {
      categories.push({
        label: catMatch[1]!.trim(),
        tokens: tokenValue(catMatch[2]!, catMatch[3]),
        pct: parseFloat(catMatch[4]!),
      });
    }
  }

  return { totalUsedTokens, totalCapacityTokens, totalPct, categories };
}

// ─────────────────────────────────────────────────────────────────────────
// `--from-telemetry` chars<->tokens ratio fit
// ─────────────────────────────────────────────────────────────────────────

export interface CalibrationFit {
  sessionId: string;
  projectRoot: string;
  totalChars: number;
  cacheReadTokens: number;
  cacheCreationTokens: number;
  totalTokens: number;
  /** How many characters, on average, one real token cost in this sample. */
  charsPerToken: number;
  provenance: Provenance;
}

export type CalibrationResult =
  | { status: "fitted"; fit: CalibrationFit }
  | { status: "unavailable"; reason: string };

function errMsg(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

/** Sum the char count of `projectRoot`'s root CLAUDE.md plus its full `@import` chain (task [2.1]). */
function measureClaudeMdChainChars(projectRoot: string): { ok: true; chars: number } | { ok: false; reason: string } {
  const rootClaudeMd = join(projectRoot, "CLAUDE.md");
  if (!existsSync(rootClaudeMd)) {
    return { ok: false, reason: `no CLAUDE.md at project root "${projectRoot}" to measure` };
  }
  let chars: number;
  try {
    chars = readFileSync(rootClaudeMd, "utf8").length;
  } catch (err) {
    return { ok: false, reason: `failed to read root CLAUDE.md: ${errMsg(err)}` };
  }
  for (const imp of resolveImportChain(rootClaudeMd, projectRoot)) {
    try {
      chars += readFileSync(imp.path, "utf8").length;
    } catch {
      // Dangling import — imports.ts's own convention is to skip, not abort.
    }
  }
  return { ok: true, chars };
}

/**
 * Fit a chars->tokens ratio for the CURRENT session against the CURRENT
 * project's CLAUDE.md `@import`-chain char count, per the C4b sequence:
 * resolve+verify telemetry via [2.5]'s probe, pull this session's first
 * (earliest-timestamp) `api_request` event, and divide the measured chars by
 * that event's `cache_read_tokens + cache_creation_tokens`. Degrades to
 * `{status:"unavailable", reason}` on any missing precondition (no session
 * id, no CLAUDE.md, telemetry unreachable, no matching event) — never
 * throws, never fabricates a ratio.
 */
export async function fitRatioFromTelemetry(
  opts: {
    projectRoot?: string;
    sessionId?: string;
    env?: Record<string, string | undefined>;
    windowMs?: number;
  } = {},
): Promise<CalibrationResult> {
  const env = opts.env ?? process.env;
  const projectRoot = opts.projectRoot ?? process.cwd();
  const sessionId = opts.sessionId ?? env.CLAUDE_CODE_SESSION_ID ?? env.CLAUDE_SESSION_ID;
  if (!sessionId) {
    return {
      status: "unavailable",
      reason:
        "no session id available (CLAUDE_CODE_SESSION_ID/CLAUDE_SESSION_ID unset and no explicit sessionId override) to correlate telemetry with the local char scan",
    };
  }

  const measured = measureClaudeMdChainChars(projectRoot);
  if (!measured.ok) return { status: "unavailable", reason: measured.reason };
  if (measured.chars === 0) {
    return { status: "unavailable", reason: "measured CLAUDE.md chain is 0 chars — nothing to fit a ratio against" };
  }

  const probe = await probeTelemetry("api_request", { env, windowMs: opts.windowMs });
  if (probe.status === "unavailable") {
    return { status: "unavailable", reason: probe.reason };
  }

  // "First" is the earliest event among the queried window/limit, not a
  // guaranteed absolute session-start event — an extremely long/high-volume
  // session can have more `api_request` events than `limit` covers, in which
  // case this is the earliest one WE SAMPLED, not necessarily the session's
  // literal first request. Verified live (2026-07-18, this exact session):
  // correct and internally consistent, but callers wanting the true first
  // request of a very long session should pass a larger `limit` via a future
  // option, or narrow `windowMs` to the session's actual start time.
  const queried = await queryEvents(probe.endpoint, "api_request", {
    windowMs: opts.windowMs ?? 24 * 60 * 60 * 1000,
    limit: 500,
  });
  if (!queried.ok) return { status: "unavailable", reason: queried.reason };

  const sessionEvents = queried.events.filter((e) => e.attrs["session.id"] === sessionId);
  if (sessionEvents.length === 0) {
    return {
      status: "unavailable",
      reason: `no api_request events found for session "${sessionId}" in the sampled window`,
    };
  }

  const first = sessionEvents.reduce((earliest, e) => (e.timestampMs < earliest.timestampMs ? e : earliest));
  const cacheReadTokens = first.attrs.cache_read_tokens;
  const cacheCreationTokens = first.attrs.cache_creation_tokens;
  if (typeof cacheReadTokens !== "number" || typeof cacheCreationTokens !== "number") {
    return {
      status: "unavailable",
      reason: `session "${sessionId}"'s first api_request event is missing numeric cache_read_tokens/cache_creation_tokens`,
    };
  }

  const totalTokens = cacheReadTokens + cacheCreationTokens;
  if (totalTokens === 0) {
    return { status: "unavailable", reason: "first api_request event reports 0 total cache tokens — cannot fit a ratio" };
  }

  return {
    status: "fitted",
    fit: {
      sessionId,
      projectRoot,
      totalChars: measured.chars,
      cacheReadTokens,
      cacheCreationTokens,
      totalTokens,
      charsPerToken: measured.chars / totalTokens,
      provenance: queried.provenance,
    },
  };
}
