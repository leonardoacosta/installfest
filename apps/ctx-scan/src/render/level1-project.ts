/**
 * level1-project.ts — per-project stacked bar with toggles (ctx-scan-render
 * task [2.2], beads:if-qoaa): one color-coded segment per class (the
 * 13-class taxonomy), plus the four toggle controls (post-compaction,
 * include-T2, predicted-drops, calibrated-constant marking).
 *
 * The toggles themselves are pure CSS/JS on already-rendered markup (see
 * render.ts's `SHARED_CSS`/`SHARED_JS`) — this module only emits the
 * checkbox controls plus the `data-*` attributes each toggle's CSS rule
 * selects on (`data-tier-ge2` for include-T2 splits into a T1 solid segment
 * and a T2 hatched sub-segment per class).
 */
import { CLASS_LABELS } from "./view-model";
import { escapeAttr, escapeHtml, fmtEstTokens, type ProjectView } from "./view-model";

function bandCss(band: string): string {
  return band === "NONE" ? "band-none" : `band-${band.toLowerCase()}`;
}

/** Render one project's level-1 stacked bar + toggle controls (`hidden` by default; `render.ts` composes it as a sibling `.screen`). */
export function renderLevel1(project: ProjectView, projIdx: number): string {
  const denom = project.classes.reduce((sum, c) => sum + c.totalTokens, 0) || 1;

  const segs = project.classes
    .map((cls, clsIdx) => {
      const t1Pct = (cls.tier1Tokens / denom) * 100;
      const t2Pct = (cls.tier2PlusTokens / denom) * 100;
      const band = bandCss(cls.worstBand);
      const t2Seg =
        cls.tier2PlusTokens > 0
          ? `<div class="class-seg class-seg-hatched ${band}" style="width:${t2Pct.toFixed(3)}%" data-tier-ge2="1"></div>`
          : "";
      return `    <div class="class-seg-group" data-nav-target="level2-${projIdx}-${clsIdx}" role="button" tabindex="0"
      title="${escapeAttr(CLASS_LABELS[cls.cls])} — ${fmtEstTokens(cls.totalTokens)} tok — ${cls.worstBand}">
      <div class="class-seg ${band}" style="width:${t1Pct.toFixed(3)}%" data-tier-ge2="0"></div>
${t2Seg ? `      ${t2Seg}` : ""}
    </div>`;
    })
    .join("\n");

  const legend = project.classes
    .map((cls, clsIdx) => {
      const band = bandCss(cls.worstBand);
      return `    <li><a href="#" data-nav-target="level2-${projIdx}-${clsIdx}" class="${band}">${escapeHtml(
        CLASS_LABELS[cls.cls],
      )} — ${fmtEstTokens(cls.totalTokens)} tok${cls.hasT2 ? ' <span class="t2-marker" data-tier-ge2="1">(T2)</span>' : ""}</a></li>`;
    })
    .join("\n");

  return `<section id="level1-${projIdx}" class="screen" hidden>
  <a href="#" data-nav-target="level0" class="back-link">&larr; Back to fleet</a>
  <h2>${escapeHtml(project.name)}</h2>
  <p class="doc-path"><code>${escapeHtml(project.path)}</code></p>
  <p class="hint">${fmtEstTokens(project.globalBaselineTokens)} tok global baseline + ${fmtEstTokens(
    project.projectDeltaTokens,
  )} tok project delta = ${fmtEstTokens(project.totalTokens)} tok total (tier-1 only).</p>
  <div class="toggle-bar">
    <label><input type="checkbox" data-toggle="post-compaction" /> Post-compaction view</label>
    <label><input type="checkbox" data-toggle="include-t2" /> Include T2 (trigger-paid)</label>
    <label><input type="checkbox" data-toggle="predicted-drops" /> Highlight predicted drops</label>
    <label><input type="checkbox" data-toggle="calibrated-marking" checked /> Highlight calibrated constants</label>
  </div>
  <div class="class-bar">
${segs}
  </div>
  <ul class="class-list">
${legend}
  </ul>
</section>`;
}
