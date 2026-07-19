/**
 * trim-plan.ts — greedy remediation plan over RED/AMBER rows for one focused
 * project (ctx-scan-render task [2.5], beads:if-x8yr).
 *
 * Read-only by construction: every function here only READS `ProjectView`/
 * `Fleet` data and returns plain data or an HTML string — there is no
 * file-write code path anywhere in this module (proposal.md's Trim-plan
 * panel Requirement + Scope OUT: "any code that applies a trim suggestion
 * automatically" is explicitly excluded from this proposal).
 *
 * Candidates come from two sources:
 *   - per-document rubric violations already computed onto each
 *     `DocumentView` (view-model.ts's `bands`, sourced from
 *     `ctx-scan-budgets`'s `computeNodeBands`) — one candidate per
 *     (document, violated row).
 *   - the three FLEET-WIDE aggregate rows (A1/A7/A12) that have no single
 *     Node to attribute a change to (`rubric.ts`'s `nodeClasses: null`
 *     rows) — reused directly from `audit.ts`'s `auditFleet`, which already
 *     implements that aggregation once (extend-before-create: no
 *     reimplementation of aggregateA1/A7/A12 here). These rows are
 *     inherently global (computed from `fleet.global` only), so they are
 *     identical for every project — a project's trim plan still surfaces
 *     them because a session opened in that project pays for the shared
 *     global layer too.
 *
 * Every candidate's `tokensRecovered` estimate is converted to TOKENS
 * (Part 0's own "tokens ≈ chars/4" convention) regardless of the row's own
 * unit, so the whole plan can be ranked and summed on one consistent axis.
 */
import type { Fleet } from "../model";
import { auditFleet } from "../audit";
import { TABLE_A, type TableARow } from "../rubric";
import { SHORT_SURFACE_LABEL } from "./level3-document";
import { escapeHtml, fmtEstTokens, type DocumentView, type ProjectView } from "./view-model";

const TABLE_A_BY_ID = new Map(TABLE_A.map((row) => [row.id, row]));
const CHARS_PER_TOKEN = 4; // Part 0's own "tokens ≈ chars/4" convention.
const AGGREGATE_ROW_IDS = ["A1", "A7", "A12"] as const;

export type TrimCandidateKind = "node" | "aggregate";

export interface TrimCandidate {
  id: string;
  kind: TrimCandidateKind;
  rule: string;
  surfaceLabel: string;
  band: "AMBER" | "RED";
  measured: number;
  greenTarget: number;
  overage: number;
  tokensRecovered: number;
  description: string;
  path?: string;
}

export interface TrimPlanStep extends TrimCandidate {
  runningTotalTokens: number;
  reachesTarget: boolean;
}

export interface TrimPlan {
  projectName: string;
  steps: TrimPlanStep[];
  /** Sum of every candidate's own `tokensRecovered` — the target the running total climbs toward. */
  totalOverageTokens: number;
  finalTotalTokens: number;
  /** `finalTotalTokens >= totalOverageTokens` — vacuously true when there are zero candidates. */
  reachesGreen: boolean;
}

/** Convert a row-unit overage into an estimated token count. Bounded and never fabricated for units with no clean conversion. */
function estimateTokensRecovered(row: TableARow, measured: number, doc?: DocumentView): number {
  const overage = measured - row.greenMax;
  if (overage <= 0) return 0;
  switch (row.unit) {
    case "tokens":
      return Math.round(overage);
    case "chars":
    case "bytes":
      return Math.ceil(overage / CHARS_PER_TOKEN);
    case "lines": {
      // No direct chars-per-line conversion is tracked anywhere in this
      // codebase — approximate by the same fraction of the node's own
      // est_tokens that the line-overage represents of its total measured
      // lines. Bounded to the node's own token cost; 0 with no `doc` (the
      // aggregate-row call sites never use the "lines" unit today).
      if (!doc || doc.estTokens <= 0 || measured <= 0) return 0;
      const fraction = overage / measured;
      return Math.min(doc.estTokens, Math.ceil(fraction * doc.estTokens));
    }
    case "count":
      return 0;
  }
}

function surfaceLabelFor(rule: string, row: TableARow | undefined): string {
  return SHORT_SURFACE_LABEL[rule] ?? row?.surface ?? rule;
}

/** Gather one candidate per (document, non-GREEN band) across every class in `project`. */
function nodeCandidates(project: ProjectView): TrimCandidate[] {
  const out: TrimCandidate[] = [];
  for (const cls of project.classes) {
    for (const doc of cls.documents) {
      for (const band of doc.bands) {
        if (band.band === "GREEN") continue;
        const row = TABLE_A_BY_ID.get(band.rule);
        if (!row) continue;
        const tokensRecovered = estimateTokensRecovered(row, band.measured, doc);
        if (tokensRecovered <= 0) continue;
        out.push({
          id: `node:${doc.path}#${band.rule}`,
          kind: "node",
          rule: band.rule,
          surfaceLabel: surfaceLabelFor(band.rule, row),
          band: band.band,
          measured: band.measured,
          greenTarget: row.greenMax,
          overage: band.measured - row.greenMax,
          tokensRecovered,
          description: `${doc.displayName} (${row.surface})`,
          path: doc.path,
        });
      }
    }
  }
  return out;
}

/** Gather the fleet-wide aggregate-row (A1/A7/A12) candidates, reusing `audit.ts`'s `auditFleet` (see module doc). */
function aggregateCandidates(fleet: Fleet): TrimCandidate[] {
  const audit = auditFleet(fleet);
  const out: TrimCandidate[] = [];
  for (const rowId of AGGREGATE_ROW_IDS) {
    const auditRow = audit.rows.find((r) => r.id === rowId);
    if (!auditRow || auditRow.measured === null) continue;
    if (auditRow.band !== "AMBER" && auditRow.band !== "RED") continue;
    const row = TABLE_A_BY_ID.get(rowId);
    if (!row) continue;
    const tokensRecovered = estimateTokensRecovered(row, auditRow.measured);
    if (tokensRecovered <= 0) continue;
    out.push({
      id: `aggregate:${rowId}`,
      kind: "aggregate",
      rule: rowId,
      surfaceLabel: surfaceLabelFor(rowId, row),
      band: auditRow.band,
      measured: auditRow.measured,
      greenTarget: row.greenMax,
      overage: auditRow.measured - row.greenMax,
      tokensRecovered,
      description: row.surface,
    });
  }
  return out;
}

/** Deterministic tie-break: highest recovery first, then RED before AMBER, then rule id, then path. */
function compareCandidates(a: TrimCandidate, b: TrimCandidate): number {
  if (a.tokensRecovered !== b.tokensRecovered) return b.tokensRecovered - a.tokensRecovered;
  const severity = { RED: 1, AMBER: 0 } as const;
  if (severity[a.band] !== severity[b.band]) return severity[b.band] - severity[a.band];
  if (a.rule !== b.rule) return a.rule.localeCompare(b.rule);
  return (a.path ?? "").localeCompare(b.path ?? "");
}

/**
 * Compute the greedy remediation plan for one project: every RED/AMBER
 * candidate (node-level plus the shared global aggregate rows), ranked by
 * tokens-recovered-per-change (highest first), with a running total.
 */
export function computeTrimPlan(project: ProjectView, fleet: Fleet): TrimPlan {
  const candidates = [...nodeCandidates(project), ...aggregateCandidates(fleet)].sort(compareCandidates);
  const totalOverageTokens = candidates.reduce((sum, c) => sum + c.tokensRecovered, 0);

  let running = 0;
  const steps: TrimPlanStep[] = candidates.map((c) => {
    running += c.tokensRecovered;
    return { ...c, runningTotalTokens: running, reachesTarget: running >= totalOverageTokens };
  });

  const finalTotalTokens = steps.length > 0 ? steps[steps.length - 1]!.runningTotalTokens : 0;
  return {
    projectName: project.name,
    steps,
    totalOverageTokens,
    finalTotalTokens,
    reachesGreen: finalTotalTokens >= totalOverageTokens,
  };
}

/** Render the trim-plan panel for one project (attached alongside its level-1 stacked bar, not its own drill level). */
export function renderTrimPlanHtml(plan: TrimPlan, projIdx: number): string {
  if (plan.steps.length === 0) {
    return `<div class="trim-plan" id="trim-plan-${projIdx}">
  <h3>Trim plan</h3>
  <p class="empty">No RED/AMBER rubric rows for this project — nothing to trim.</p>
</div>`;
  }

  const rows = plan.steps
    .map(
      (step) => `      <tr class="band-${step.band.toLowerCase()}">
        <td>${escapeHtml(step.rule)} ${escapeHtml(step.surfaceLabel)}</td>
        <td>${escapeHtml(step.description)}</td>
        <td>${step.band}</td>
        <td>${step.measured.toLocaleString()} / ${step.greenTarget.toLocaleString()}</td>
        <td>${fmtEstTokens(step.tokensRecovered)}</td>
        <td>${fmtEstTokens(step.runningTotalTokens)}${step.reachesTarget ? " &#10003;" : ""}</td>
      </tr>`,
    )
    .join("\n");

  return `<div class="trim-plan" id="trim-plan-${projIdx}">
  <h3>Trim plan</h3>
  <p class="hint">Greedy remediation order, ranked by tokens recovered per change. Read-only — this panel only proposes; nothing here edits a source file.</p>
  <table class="trim-table">
    <thead>
      <tr><th>Row</th><th>Where</th><th>Band</th><th>Measured / GREEN</th><th>Tokens recovered</th><th>Running total</th></tr>
    </thead>
    <tbody>
${rows}
    </tbody>
  </table>
  <p class="trim-summary">Running total: ${fmtEstTokens(plan.finalTotalTokens)} / target ${fmtEstTokens(
    plan.totalOverageTokens,
  )} tok — ${plan.reachesGreen ? "reaches GREEN" : "does not yet reach GREEN"}.</p>
</div>`;
}
