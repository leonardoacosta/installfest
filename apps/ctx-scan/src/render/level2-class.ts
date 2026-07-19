/**
 * level2-class.ts — per-class proportional document bar (ctx-scan-render
 * task [2.3], beads:if-qw32): each document rendered as a segment
 * proportional to its own `est_tokens` within the class, bordered in its
 * `ctx-scan-budgets` band color (GREEN/AMBER/RED) — sourced from
 * `DocumentView.worstBand`, itself derived from `Node.bands`.
 */
import { escapeAttr, escapeHtml, fmtEstTokens, type ClassView } from "./view-model";

function bandCss(band: string): string {
  return band === "NONE" ? "band-none" : `band-${band.toLowerCase()}`;
}

/** Render one class's level-2 proportional document bar (`hidden` by default; `render.ts` composes it as a sibling `.screen`). */
export function renderLevel2(cls: ClassView, projIdx: number, clsIdx: number): string {
  const denom = cls.totalTokens || 1;

  const segs = cls.documents
    .map((doc, docIdx) => {
      const pct = (doc.estTokens / denom) * 100;
      return `    <div class="doc-bar-seg ${bandCss(doc.worstBand)}" style="width:${pct.toFixed(3)}%"
      data-nav-target="level3-${projIdx}-${clsIdx}-${docIdx}" role="button" tabindex="0"
      data-tier-ge2="${doc.tier >= 2 ? 1 : 0}"
      data-survivor="${doc.isPostCompactionSurvivor ? 1 : 0}"
      data-predicted-drop="${doc.isPredictedDrop ? 1 : 0}"
      data-calibrated="${doc.isCalibratedConstant ? 1 : 0}"
      title="${escapeAttr(doc.displayName)} — ${fmtEstTokens(doc.estTokens)} tok — ${doc.worstBand}"></div>`;
    })
    .join("\n");

  const list = cls.documents
    .map((doc, docIdx) => {
      const bandSuffix = doc.worstBand !== "NONE" && doc.worstBand !== "GREEN" ? ` — ${doc.worstBand}` : "";
      return `    <li data-tier-ge2="${doc.tier >= 2 ? 1 : 0}" data-survivor="${doc.isPostCompactionSurvivor ? 1 : 0}"
      data-predicted-drop="${doc.isPredictedDrop ? 1 : 0}" data-calibrated="${doc.isCalibratedConstant ? 1 : 0}">
      <a href="#" data-nav-target="level3-${projIdx}-${clsIdx}-${docIdx}" class="${bandCss(doc.worstBand)}">${escapeHtml(
        doc.displayName,
      )} — ${fmtEstTokens(doc.estTokens)} tok${bandSuffix}</a>
    </li>`;
    })
    .join("\n");

  return `<section id="level2-${projIdx}-${clsIdx}" class="screen" hidden>
  <a href="#" data-nav-target="level1-${projIdx}" class="back-link">&larr; Back to project</a>
  <h2>${escapeHtml(cls.label)}</h2>
  <div class="doc-bar">
${segs || '    <p class="empty">No documents in this class.</p>'}
  </div>
  <ul class="doc-list">
${list || '    <li class="empty">No documents in this class.</li>'}
  </ul>
</section>`;
}
