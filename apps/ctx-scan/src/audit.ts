/**
 * audit.ts — the §E-R1 rubric-band JSON contract (ctx-scan-budgets task
 * [2.3]). Pure row-computation logic only — `cli.ts` owns building the
 * `Fleet` (via `buildFleet`, same as `scan`) and wiring the `audit` command
 * (task [3.1]); this module never touches argv or stdout, matching the
 * `pipeline.ts`/`cli.ts` wiring-vs-logic split this codebase already
 * establishes.
 *
 * Contract (docs/context-budget-rubric.md Part 3, §E-R1): emit one row per
 * Table A id, exit 0 always (a thrown error surfaces as `{rows:[],
 * error:"..."}`, never a non-zero exit or an uncaught throw), no network
 * access, and (for a single small project) a warm runtime the caller can
 * keep well under the rubric's own 200ms data-producer convention. This
 * module itself does zero I/O beyond the per-row measurement helpers
 * `rubric.ts` already implements (file re-reads keyed off `Node.path`) — no
 * telemetry probing here, unlike `scan`'s hook-size ingestion.
 */
import type { BandVerdict, Fleet } from "./model";
import {
  TABLE_A,
  allFleetNodes,
  annotateFleetBands,
  bandFor,
  descriptionAloneLength,
  nameLength,
  type SourceTag,
  type TableARow,
} from "./rubric";

export interface AuditRow {
  id: string;
  surface: string;
  /** `null` only for a row `docs/context-budget-rubric.md` genuinely cannot compute from today's Fleet/Node model (see `note`). */
  measured: number | null;
  /** The row's Table A "Limit + tag" ceiling. */
  budget: number;
  greenMax: number;
  amberMax: number;
  /** `"UNKNOWN"` only when `computable` is false — an honest "not measured", never a guessed band. */
  band: BandVerdict | "UNKNOWN";
  source: SourceTag;
  computable: boolean;
  /** Populated only when `computable` is false. */
  note?: string;
}

export interface AuditResult {
  rows: AuditRow[];
  error: string | null;
}

const SEVERITY: Record<BandVerdict, number> = { GREEN: 0, AMBER: 1, RED: 2 };

/**
 * The worst (highest-severity, then highest-measured) Node-level verdict for
 * `rowId` across the whole Fleet — the audit row's representative measurement
 * for a node-annotatable rubric row (the worst offender is what determines
 * whether the row is failing, matching the cap-style intent of every
 * node-annotatable row in Table A).
 */
function worstNodeVerdict(fleet: Fleet, rowId: string): { measured: number; band: BandVerdict } | null {
  let best: { measured: number; band: BandVerdict } | null = null;
  for (const node of allFleetNodes(fleet)) {
    for (const b of node.bands) {
      if (b.rule !== rowId) continue;
      if (!best || SEVERITY[b.band] > SEVERITY[best.band] || (SEVERITY[b.band] === SEVERITY[best.band] && b.measured > best.measured)) {
        best = { measured: b.measured, band: b.band };
      }
    }
  }
  return best;
}

/** A1 aggregate: Σ len(name)+len(description)+len(when_to_use) over the global skills-listing + commands-listing Surfaces. */
function aggregateA1(fleet: Fleet): number {
  let total = 0;
  for (const s of fleet.global) {
    if (s.cls !== "skills-listing" && s.cls !== "commands-listing") continue;
    for (const n of s.nodes) total += n.raw_chars + nameLength(n.path);
  }
  return total;
}

/** A7 aggregate: Σ est_tokens over tier===1 (always-loaded) claude-md-chain + rules-import Nodes in the global layer. */
function aggregateA7(fleet: Fleet): number {
  let total = 0;
  for (const s of fleet.global) {
    if (s.cls !== "claude-md-chain" && s.cls !== "rules-import") continue;
    for (const n of s.nodes) {
      if (n.tier === 1) total += n.est_tokens;
    }
  }
  return total;
}

/** A12 aggregate: Σ len(description) alone over the global agents Surface. */
function aggregateA12(fleet: Fleet): number {
  let total = 0;
  for (const s of fleet.global) {
    if (s.cls !== "agents") continue;
    for (const n of s.nodes) total += descriptionAloneLength(n.path) ?? n.raw_chars;
  }
  return total;
}

function buildRow(row: TableARow, fleet: Fleet): AuditRow {
  const base = {
    id: row.id,
    surface: row.surface,
    budget: row.limit,
    greenMax: row.greenMax,
    amberMax: row.amberMax,
    source: row.source,
    computable: row.computable,
  };

  if (!row.computable) {
    return { ...base, measured: null, band: "UNKNOWN", note: row.gapNote };
  }

  if (row.nodeClasses === null) {
    let measured: number;
    switch (row.id) {
      case "A1":
        measured = aggregateA1(fleet);
        break;
      case "A7":
        measured = aggregateA7(fleet);
        break;
      case "A12":
        measured = aggregateA12(fleet);
        break;
      default:
        measured = 0;
    }
    return { ...base, measured, band: bandFor(row, measured) };
  }

  const worst = worstNodeVerdict(fleet, row.id);
  if (!worst) return { ...base, measured: 0, band: "GREEN" };
  return { ...base, measured: worst.measured, band: worst.band };
}

/**
 * Emit the §E-R1 contract for every Table A row against `fleet`. Never
 * throws — any failure surfaces as `{rows: [], error: "<message>"}` so a
 * caller always gets exit-0-safe JSON (task [2.3]'s "exit 0 always"
 * requirement, enforced here at the pure-logic layer so `cli.ts`'s wiring
 * doesn't have to re-implement it).
 */
export function auditFleet(fleet: Fleet): AuditResult {
  try {
    annotateFleetBands(fleet);
    const rows = TABLE_A.map((row) => buildRow(row, fleet));
    return { rows, error: null };
  } catch (err) {
    return { rows: [], error: err instanceof Error ? err.message : String(err) };
  }
}
