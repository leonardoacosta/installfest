/**
 * level0-fleet.ts — fleet leaderboard (ctx-scan-render task [2.1],
 * beads:if-l8o0): one leaderboard bar per project, showing always-loaded
 * (T1) tokens with the global-baseline sub-stack rendered visually distinct
 * from the project-specific delta.
 *
 * Extended by `ctx-scan-watch` task [3.2] with a per-project drift
 * sparkline: an inline SVG polyline of the project's audit-severity trend
 * across its recent `~/.ctx-scan/history.jsonl` snapshots (`history.ts`),
 * rendered only when at least two snapshots exist for that project (a
 * single point has no trend to show). Reading history at render time
 * mirrors `view-model.ts`'s own `buildContentCache`/`readContentPreview`
 * convention (a fresh file re-read alongside the Fleet document, not baked
 * into it) and keeps the self-containment invariant: the SVG is built as a
 * plain string here and inlined directly into the page — no external JS
 * charting library, no CDN.
 */
import { snapshotsForProject, type HistorySnapshot } from "../history";
import { escapeAttr, escapeHtml, fmtEstTokens, type FleetView } from "./view-model";

/** How many of a project's most recent snapshots the sparkline plots — bounds both SVG width and render cost on a long-lived history file. */
const SPARKLINE_MAX_POINTS = 20;

/** GREEN=0 / AMBER=1 / RED=2 — `"UNKNOWN"` (a row the rubric couldn't compute) contributes 0, matching `audit.ts`'s own "never fabricate a verdict" convention rather than penalizing an uncomputable row. */
function bandSeverity(band: string): number {
  if (band === "RED") return 2;
  if (band === "AMBER") return 1;
  return 0;
}

/** Sum of every audit row's severity for one snapshot — a single scalar "how bad is this project right now" trend point. */
function snapshotSeverityScore(snapshot: HistorySnapshot): number {
  let total = 0;
  for (const row of snapshot.auditOutput.rows) total += bandSeverity(row.band);
  return total;
}

/**
 * Inline SVG polyline sparkline of the audit-severity trend across
 * `snapshots` (oldest -> newest, left -> right). No external libs — the
 * self-containment invariant from `ctx-scan-render` (`render.ts`'s "airplane
 * test") applies here exactly as it does to every other render/*.ts module.
 */
function renderSparkline(snapshots: HistorySnapshot[]): string {
  const recent = snapshots.slice(-SPARKLINE_MAX_POINTS);
  const scores = recent.map(snapshotSeverityScore);
  const width = 96;
  const height = 22;
  const pad = 2;
  const max = Math.max(...scores, 1);
  const min = Math.min(...scores, 0);
  const range = max - min || 1;
  const stepX = scores.length > 1 ? (width - pad * 2) / (scores.length - 1) : 0;
  const yFor = (score: number): number => height - pad - ((score - min) / range) * (height - pad * 2);

  const points = scores.map((score, i) => `${(pad + i * stepX).toFixed(1)},${yFor(score).toFixed(1)}`).join(" ");
  const lastScore = scores[scores.length - 1]!;
  const lastX = pad + (scores.length - 1) * stepX;
  const lastColor = lastScore >= 2 ? "#dc2626" : lastScore >= 1 ? "#d97706" : "#16a34a";

  return `<svg class="fleet-sparkline" width="${width}" height="${height}" viewBox="0 0 ${width} ${height}" role="img"
      aria-label="Audit severity trend over ${scores.length} snapshots (higher = more/worse AMBER+RED rows)">
      <polyline points="${points}" fill="none" stroke="#64748b" stroke-width="1.5" />
      <circle cx="${lastX.toFixed(1)}" cy="${yFor(lastScore).toFixed(1)}" r="2.5" fill="${lastColor}" />
    </svg>`;
}

export interface RenderLevel0Options {
  /** Override the history.jsonl path (tests / `render --history`) — default: `~/.ctx-scan/history.jsonl` via `history.ts`'s own default. */
  historyFilePath?: string;
}

/** Render the level-0 fleet leaderboard section (always visible/un-hidden by default; `render.ts` may still override the page's initial screen). */
export function renderLevel0(fleet: FleetView, opts: RenderLevel0Options = {}): string {
  if (fleet.bars.length === 0) {
    return `<section id="level0" class="screen">
  <h1>ctx-scan fleet report</h1>
  <p class="empty">No projects discovered under the scanned root.</p>
</section>`;
  }

  const denom = fleet.maxTotalTokens || 1;
  const rows = fleet.bars
    .map((bar, i) => {
      const globalPct = (bar.globalBaselineTokens / denom) * 100;
      const deltaPct = (bar.projectDeltaTokens / denom) * 100;
      const history = snapshotsForProject(bar.projectPath, { filePath: opts.historyFilePath });
      const sparklineHtml = history.length >= 2 ? renderSparkline(history) : "";
      return `  <div class="fleet-row">
    <div class="fleet-row-label">${escapeHtml(bar.projectName)}</div>
    <div class="fleet-bar" data-nav-target="level1-${i}" role="button" tabindex="0"
      title="${escapeAttr(bar.projectName)} — ${fmtEstTokens(bar.totalTokens)} tok (T1)">
      <div class="fleet-seg fleet-seg-global" style="width:${globalPct.toFixed(3)}%"
        title="Global baseline: ${fmtEstTokens(bar.globalBaselineTokens)} tok"></div>
      <div class="fleet-seg fleet-seg-delta" style="width:${deltaPct.toFixed(3)}%"
        title="Project delta: ${fmtEstTokens(bar.projectDeltaTokens)} tok"></div>
    </div>
    <div class="fleet-row-sparkline" title="Audit severity trend (${history.length} snapshot(s) recorded)">${sparklineHtml}</div>
    <div class="fleet-row-total">${fmtEstTokens(bar.totalTokens)} tok</div>
  </div>`;
    })
    .join("\n");

  return `<section id="level0" class="screen">
  <h1>ctx-scan fleet report</h1>
  <p class="hint">Always-loaded (T1) tokens per project. Click a project to drill into its per-class breakdown.</p>
  <div class="fleet-legend"><span class="legend-swatch fleet-seg-global"></span> global baseline
    <span class="legend-swatch fleet-seg-delta"></span> project delta</div>
  <div class="fleet-leaderboard">
${rows}
  </div>
</section>`;
}
