/**
 * truncation.ts — frontmatter parsing + platform truncation caps
 * (ctx-scan-assembly task [2.3], beads:if-5lpf).
 *
 * Every numeric constant here is sourced from `docs/context-budget-rubric.md`
 * Table A (measured 2026-07-18, code.claude.com docs) — never invented:
 *   A2  per-listing-entry cap   : 1,536 chars  (skillListingMaxDescChars)
 *   A1  listing-total budget    : 8,000 chars  (skillListingBudgetFraction 0.01 of 200K ctx)
 *   A9  MEMORY.md cap           : 200 lines OR 25KB, whichever binds first
 *   A13 MCP description cap    : 2KB (2,048 bytes) per tool/server description
 */
import matter from "gray-matter";
import type { Truncation } from "./model";

export const LISTING_ENTRY_CAP_CHARS = 1536; // A2
export const LISTING_TOTAL_BUDGET_CHARS = 8000; // A1
export const MEMORY_MD_MAX_LINES = 200; // A9
export const MEMORY_MD_MAX_BYTES = 25 * 1024; // A9 (25KB)
export const MCP_DESCRIPTION_CAP_BYTES = 2048; // A13

/** Result of applying one cap: the capped value plus the Truncation record. */
export interface CapResult<T> {
  effective: T;
  truncation: Truncation;
}

/** Frontmatter fields ctx-scan cares about (skills/commands/agents share this shape). */
export interface ParsedFrontmatter {
  name?: string;
  description?: string;
  when_to_use?: string;
  /** Every other frontmatter key, untyped (multiline YAML included via gray-matter). */
  data: Record<string, unknown>;
  /** The document body, after the frontmatter block. */
  body: string;
}

/** Parse a skill/command/agent .md file's frontmatter (multiline YAML included). */
export function parseFrontmatter(content: string): ParsedFrontmatter {
  const { data, content: body } = matter(content);
  return {
    name: typeof data.name === "string" ? data.name : undefined,
    description: typeof data.description === "string" ? data.description : undefined,
    when_to_use: typeof data.when_to_use === "string" ? data.when_to_use : undefined,
    data,
    body,
  };
}

/** Apply the A2 per-listing-entry cap to one skill/command description string. */
export function capListingEntry(text: string): CapResult<string> {
  const raw = text.length;
  if (raw <= LISTING_ENTRY_CAP_CHARS) {
    return { effective: text, truncation: { raw, effective: raw, cap: "listing-entry" } };
  }
  const effective = text.slice(0, LISTING_ENTRY_CAP_CHARS);
  return {
    effective,
    truncation: { raw, effective: LISTING_ENTRY_CAP_CHARS, cap: "listing-entry" },
  };
}

/**
 * One listing entry going into the shared budget-fraction cap.
 *
 * `invocations` is deliberately an externally-supplied number rather than
 * something this module fetches itself — this module has no I/O, and per
 * the ctx-scan-assembly proposal ("sourced from telemetry `tool_name="Skill"`
 * counts when reachable, else order: unknown") the sourcing is telemetry's
 * job, not truncation's.
 *
 * VERIFIED FINDING (task [2.5]/[2.3] cross-check, live against this
 * machine's real Loki, 2026-07-18): Claude Code's native
 * `claude_code.tool_result` OTel event DOES carry `tool_name="Skill"` for
 * every Skill-tool invocation, but it carries NO skill-name/identifier
 * attribute — Anthropic's own privacy-by-default tool-argument redaction
 * (`OTEL_LOG_TOOL_DETAILS` off, confirmed in
 * `docs/handoffs/hl-20260602T235235.md`) means the specific skill invoked is
 * never logged, only that "a Skill-class tool ran". cc's own
 * `command_start` telemetry event (`~/.claude/telemetry/agents.jsonl`) was
 * also checked live and only fires for slash-COMMAND invocations (`apply`,
 * `feature`, `improve:*`, ...), never for individual Skill-tool loads. So
 * **no currently-queryable telemetry source can resolve a genuine
 * per-skill/per-command invocation count** — in practice, until Claude Code
 * or cc's own instrumentation adds a skill-identifying attribute, every
 * real listing scan will legitimately fall through to `order: "unknown"`
 * for every entry. That is the CORRECT degraded behavior this module
 * already implements (see below) — not a bug to route around by inventing
 * a fabricated mapping.
 */
export interface ListingEntryInput {
  id: string;
  text: string;
  /** Invocation count, when telemetry is reachable AND per-entry-resolvable. Absent -> order "unknown". */
  invocations?: number;
}

export interface ListingEntryOutput {
  id: string;
  effective: string;
  truncation: Truncation;
  /** Least-invoked-first drop rank ("unknown" when no invocation data was given). */
  order: "unknown" | number;
  /** True when this entry was dropped entirely by the total-budget cap. */
  dropped: boolean;
}

/**
 * Apply the A1 listing-total budget cap across entries (each already run
 * through `capListingEntry` for A2). When the combined effective length
 * exceeds `budgetChars`, entries are dropped least-invoked-first (lowest
 * `invocations` first) until the total fits. Entries with no invocation data
 * are treated as ties (dropped in input order among themselves) and marked
 * `order: "unknown"`.
 */
export function capListingTotal(
  entries: ListingEntryInput[],
  budgetChars: number = LISTING_TOTAL_BUDGET_CHARS,
): ListingEntryOutput[] {
  const perEntry = entries.map((e) => ({ id: e.id, invocations: e.invocations, ...capListingEntry(e.text) }));

  const hasInvocationData = entries.some((e) => e.invocations !== undefined);
  // Rank order: known invocation counts ascending (least-invoked drops first),
  // unknowns treated as "drop last among knowns" is wrong per spec — unknowns
  // simply can't be ranked, so they keep their input order and are never
  // preferentially dropped ahead of a known-low entry. We only assign a
  // numeric `order` when hasInvocationData is true for THIS entry.
  const withOrder: ListingEntryOutput[] = perEntry.map((e) => ({
    id: e.id,
    effective: e.effective,
    truncation: e.truncation,
    order: e.invocations === undefined ? "unknown" : e.invocations,
    dropped: false,
  }));

  if (!hasInvocationData) {
    // No ranking signal at all — nothing can be dropped in a principled
    // order, so report the full set undropped (over-budget is surfaced by
    // the rubric band check downstream, not silently dropped here).
    return withOrder;
  }

  let total = withOrder.reduce((sum, e) => sum + e.effective.length, 0);
  if (total <= budgetChars) return withOrder;

  // Drop least-invoked-first among entries WITH invocation data until the
  // total fits or no more droppable entries remain.
  const droppableIdx = withOrder
    .map((e, i) => ({ i, order: e.order }))
    .filter((e) => typeof e.order === "number")
    .sort((a, b) => (a.order as number) - (b.order as number));

  for (const { i } of droppableIdx) {
    if (total <= budgetChars) break;
    withOrder[i]!.dropped = true;
    total -= withOrder[i]!.effective.length;
  }

  return withOrder;
}

/** Apply the A9 MEMORY.md cap: 200 lines OR 25KB, whichever binds first. */
export function capMemoryMd(content: string): CapResult<string> {
  const raw = content.length;
  const lines = content.split("\n");

  let effective = content;
  if (lines.length > MEMORY_MD_MAX_LINES) {
    effective = lines.slice(0, MEMORY_MD_MAX_LINES).join("\n");
  }
  if (Buffer.byteLength(effective, "utf8") > MEMORY_MD_MAX_BYTES) {
    // Byte-cap wins if it binds tighter than the line-cap already applied.
    const buf = Buffer.from(effective, "utf8").subarray(0, MEMORY_MD_MAX_BYTES);
    effective = buf.toString("utf8");
  }

  return {
    effective,
    truncation: { raw, effective: effective.length, cap: "memory-md" },
  };
}

/** Apply the A13 MCP tool/server description cap: 2KB. */
export function capMcpDescription(text: string): CapResult<string> {
  const raw = Buffer.byteLength(text, "utf8");
  if (raw <= MCP_DESCRIPTION_CAP_BYTES) {
    return { effective: text, truncation: { raw, effective: raw, cap: "mcp-description" } };
  }
  const buf = Buffer.from(text, "utf8").subarray(0, MCP_DESCRIPTION_CAP_BYTES);
  const effective = buf.toString("utf8");
  return {
    effective,
    truncation: { raw, effective: Buffer.byteLength(effective, "utf8"), cap: "mcp-description" },
  };
}
