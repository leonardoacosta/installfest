/**
 * level3-document.ts — document detail view (ctx-scan-render task [2.4],
 * beads:if-fwhb): rendered content, violation header text assembly, and
 * tier/origin/truncation/raw-vs-effective display for one `DocumentView`.
 *
 * Document content is deliberately NOT inlined as literal escaped text here —
 * an arbitrary scanned source file's content could legitimately contain
 * substrings like `fetch(` or `<script src=` as its own prose/code (e.g. a
 * skill doc describing what NOT to do), which would false-positive task
 * [4.1]'s self-containment grep even though no real external reference
 * exists. `render.ts` base64-encodes the whole view model (including the
 * deduplicated, path-keyed `contentByPath` cache — see view-model.ts's
 * `buildContentCache`) exactly once; this module emits an empty mount point
 * (`.doc-content-mount`) that the shared client script populates via
 * `.textContent =` (inherently HTML-injection safe) after decoding — see
 * render.ts's `SHARED_JS`.
 */
import type { Band } from "../model";
import { TABLE_A } from "../rubric";
import { escapeHtml, fmtCount, fmtEstTokens, type DocumentView } from "./view-model";

const TABLE_A_BY_ID = new Map(TABLE_A.map((row) => [row.id, row]));

/**
 * Short display label per Table A row id, matching the proposal's own
 * example wording ("A2 listing entry", "A4 body") rather than Table A's
 * full `surface` column text ("Per-skill listing entry", "SKILL.md body").
 */
export const SHORT_SURFACE_LABEL: Record<string, string> = {
  A1: "listing total",
  A2: "listing entry",
  A3: "description",
  A4: "body",
  A5: "reference ToC",
  A6: "nesting depth",
  A7: "chain",
  A8: "chain file",
  A9: "memory",
  A10: "carry-forward",
  A11: "agent description",
  A12: "roster total",
  A13: "mcp description",
  A14: "hook output",
};

/**
 * Assemble the violation header text for one document's `bands`, e.g.
 * `"A2 listing entry 1,610/1,536 [H]; A4 body 599/500"` — one clause per
 * non-GREEN band, joined with "; ". Returns `""` when every band is GREEN
 * (or there are no applicable rows at all).
 */
export function assembleViolationHeader(bands: Band[]): string {
  const violations = bands.filter((b) => b.band !== "GREEN");
  if (violations.length === 0) return "";
  return violations
    .map((b) => {
      const row = TABLE_A_BY_ID.get(b.rule);
      const label = SHORT_SURFACE_LABEL[b.rule] ?? row?.surface.toLowerCase() ?? b.rule;
      const tag = row ? ` [${row.source}]` : "";
      return `${b.rule} ${label} ${fmtCount(b.measured)}/${fmtCount(b.limit)}${tag}`;
    })
    .join("; ");
}

function truncationsHtml(doc: DocumentView): string {
  if (doc.truncations.length === 0) return `<p class="empty">No truncation applied.</p>`;
  const items = doc.truncations
    .map((t) => `<li><code>${escapeHtml(t.cap)}</code>: ${fmtCount(t.raw)} &rarr; ${fmtCount(t.effective)} chars</li>`)
    .join("");
  return `<ul class="truncation-list">${items}</ul>`;
}

/** Render one document's level-3 detail section (`hidden` by default; `render.ts` composes it as a sibling `.screen`). */
export function renderLevel3(doc: DocumentView, projIdx: number, clsIdx: number, docIdx: number): string {
  const header = assembleViolationHeader(doc.bands);
  const bandCss = doc.worstBand === "NONE" ? "band-none" : `band-${doc.worstBand.toLowerCase()}`;

  return `<section id="level3-${projIdx}-${clsIdx}-${docIdx}" class="screen" hidden>
  <a href="#" data-nav-target="level2-${projIdx}-${clsIdx}" class="back-link">&larr; Back to class</a>
  <h3>${escapeHtml(doc.displayName)}</h3>
  <p class="doc-path"><code>${escapeHtml(doc.path)}</code></p>
  <div class="doc-meta">
    <span class="badge">tier ${doc.tier}</span>
    <span class="badge">${escapeHtml(doc.origin)}</span>
    ${
      doc.isCalibratedConstant
        ? `<span class="badge badge-calibrated">calibrated constant</span>`
        : `<span class="badge badge-measured">measured</span>`
    }
    ${doc.isPostCompactionSurvivor ? `<span class="badge badge-survivor">post-compaction survivor</span>` : ""}
    ${doc.isPredictedDrop ? `<span class="badge badge-drop">predicted drop</span>` : ""}
  </div>
  <p class="violation-header ${bandCss}">${header ? escapeHtml(header) : "No rubric violations."}</p>
  <table class="size-table">
    <tbody>
      <tr><th>Raw</th><td>${fmtCount(doc.rawChars)} chars</td></tr>
      <tr><th>Effective</th><td>${fmtCount(doc.effectiveChars)} chars</td></tr>
      <tr><th>Est. tokens</th><td>${fmtEstTokens(doc.estTokens)}</td></tr>
    </tbody>
  </table>
  <h4>Truncations</h4>
  ${truncationsHtml(doc)}
  <h4>Content</h4>
  <div class="doc-content-mount" data-proj="${projIdx}" data-cls="${clsIdx}" data-doc="${docIdx}">
    <p class="empty">Loading content&hellip;</p>
  </div>
</section>`;
}
