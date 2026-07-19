/**
 * level0-fleet.ts — fleet leaderboard (ctx-scan-render task [2.1],
 * beads:if-l8o0): one leaderboard bar per project, showing always-loaded
 * (T1) tokens with the global-baseline sub-stack rendered visually distinct
 * from the project-specific delta.
 */
import { escapeAttr, escapeHtml, fmtEstTokens, type FleetView } from "./view-model";

/** Render the level-0 fleet leaderboard section (always visible/un-hidden by default; `render.ts` may still override the page's initial screen). */
export function renderLevel0(fleet: FleetView): string {
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
      return `  <div class="fleet-row">
    <div class="fleet-row-label">${escapeHtml(bar.projectName)}</div>
    <div class="fleet-bar" data-nav-target="level1-${i}" role="button" tabindex="0"
      title="${escapeAttr(bar.projectName)} — ${fmtEstTokens(bar.totalTokens)} tok (T1)">
      <div class="fleet-seg fleet-seg-global" style="width:${globalPct.toFixed(3)}%"
        title="Global baseline: ${fmtEstTokens(bar.globalBaselineTokens)} tok"></div>
      <div class="fleet-seg fleet-seg-delta" style="width:${deltaPct.toFixed(3)}%"
        title="Project delta: ${fmtEstTokens(bar.projectDeltaTokens)} tok"></div>
    </div>
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
