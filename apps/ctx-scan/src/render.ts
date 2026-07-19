/**
 * render.ts — `ctx-scan render` entrypoint (ctx-scan-render task [3.1],
 * beads:if-9h3k): assembles level0-fleet.ts, level1-project.ts,
 * level2-class.ts, level3-document.ts, and trim-plan.ts into ONE
 * self-contained HTML file — inline CSS/JS, inline (base64-encoded) JSON
 * data, no CDN dependency, no external `<script src=`/`<link href=`
 * reference, no `fetch(`/`XMLHttpRequest` call anywhere (the "airplane
 * test", proposal.md's Self-containment Requirement).
 *
 * `cli.ts` owns argv parsing and the `ctx-scan render` command registration
 * (matching the existing `scan`/`calibrate`/`audit` wiring split) — this
 * module owns HTML assembly only, exposed as `renderFleetHtml`/
 * `writeRenderedFleet` for both that wiring and direct test use.
 *
 * Design notes (real ambiguity resolved with a documented choice, per the
 * proposal's own "some ambiguity is expected" framing):
 *   - The rendered file always embeds the FULL fleet (every project, every
 *     class, every document, every project's own trim plan) regardless of
 *     `--project`/`--fleet` — there is no server to re-render a second view
 *     from, and the airplane test requires the file to be fully
 *     self-sufficient. The CLI flag only picks which `.screen` is visible
 *     on first paint (`data-initial-screen` on `<html>`); every other level
 *     is one click away via the always-present drill-down navigation.
 *   - The view model's `contentByPath` cache (arbitrary scanned source text,
 *     deduplicated by path — see view-model.ts's `buildContentCache`) is
 *     never embedded as literal characters in the file — an arbitrary
 *     document could legitimately contain `fetch(`/`<script src=` as its
 *     own prose or code example, which would false-positive task [4.1]'s
 *     self-containment grep even though no real external reference exists.
 *     The whole view model is base64-encoded once and decoded by the inline
 *     script via `atob` + `TextDecoder` (no network, still fully offline)
 *     before being written into the DOM via `.textContent =` (inherently
 *     HTML-injection-safe, and provably free of the grepped substrings —
 *     base64's alphabet cannot spell `fetch(` or `<script`).
 */
import { writeFileSync } from "node:fs";
import type { Fleet } from "./model";
import { annotateFleetBands } from "./rubric";
import { getGlobalLayer } from "./discovery";
import { renderLevel0 } from "./render/level0-fleet";
import { renderLevel1 } from "./render/level1-project";
import { renderLevel2 } from "./render/level2-class";
import { renderLevel3 } from "./render/level3-document";
import { computeTrimPlan, renderTrimPlanHtml } from "./render/trim-plan";
import { buildViewModel, escapeHtml } from "./render/view-model";
import { renderProjectShelf } from "./refs";

export interface RenderOptions {
  /** Initial view: drill directly into this project's level-1 screen. Falls back to the fleet view (with a stderr warning) if no project matches. */
  project?: string;
  /** Initial view: the fleet leaderboard. Default when neither option is given. */
  fleet?: boolean;
  /** Scope every project's references shelf panel (ctx-scan-refs) to a single named skill/command/agent owner. */
  skill?: string;
  /** Override the history.jsonl path the level-0 fleet sparkline (`ctx-scan-watch` task [3.2]) reads — default: `~/.ctx-scan/history.jsonl` via `history.ts`'s own default. */
  historyFilePath?: string;
}

const SHARED_CSS = `
:root {
  --band-green: #16a34a;
  --band-amber: #d97706;
  --band-red: #dc2626;
  --band-none: #94a3b8;
  --bg: #f8fafc;
  --panel-bg: #ffffff;
  --text: #0f172a;
  --muted: #64748b;
  --border: #e2e8f0;
}
* { box-sizing: border-box; }
body {
  margin: 0;
  padding: 24px;
  background: var(--bg);
  color: var(--text);
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
  line-height: 1.5;
}
h1, h2, h3, h4 { margin-top: 0; }
code { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 0.85em; }
.hint { color: var(--muted); font-size: 0.9em; }
.empty { color: var(--muted); font-style: italic; }
.back-link { display: inline-block; margin-bottom: 12px; color: #2563eb; text-decoration: none; }
.back-link:hover { text-decoration: underline; }
.doc-path { color: var(--muted); font-size: 0.85em; word-break: break-all; }

.fleet-legend { font-size: 0.85em; color: var(--muted); margin-bottom: 12px; }
.legend-swatch { display: inline-block; width: 12px; height: 12px; margin: 0 4px 0 12px; vertical-align: middle; border-radius: 2px; }
.legend-swatch:first-child { margin-left: 0; }

.fleet-leaderboard { display: flex; flex-direction: column; gap: 10px; }
.fleet-row { display: grid; grid-template-columns: 160px 1fr 104px 100px; align-items: center; gap: 12px; }
.fleet-row-label { font-weight: 600; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.fleet-row-total { text-align: right; color: var(--muted); font-size: 0.9em; }
.fleet-bar { display: flex; height: 28px; border-radius: 4px; overflow: hidden; background: var(--border); cursor: pointer; }
.fleet-seg { height: 100%; min-width: 1px; }
.fleet-seg-global { background: #94a3b8; }
.fleet-seg-delta { background: #2563eb; }
.fleet-row-sparkline { display: flex; align-items: center; justify-content: center; color: var(--muted); font-size: 0.75em; }
.fleet-row-sparkline:empty::after { content: "no history"; font-style: italic; }
.fleet-sparkline { display: block; }

.toggle-bar { display: flex; flex-wrap: wrap; gap: 16px; margin: 12px 0; padding: 10px 12px; background: var(--panel-bg); border: 1px solid var(--border); border-radius: 6px; font-size: 0.9em; }
.toggle-bar label { display: flex; align-items: center; gap: 6px; cursor: pointer; }

.class-bar, .doc-bar { display: flex; height: 32px; border-radius: 4px; overflow: hidden; background: var(--border); margin: 12px 0; }
.class-seg-group { display: flex; cursor: pointer; }
.class-seg, .doc-bar-seg { height: 100%; min-width: 2px; border: 2px solid transparent; cursor: pointer; }
.class-seg-hatched { background-image: repeating-linear-gradient(45deg, rgba(0,0,0,.18) 0 5px, transparent 5px 10px); }

.band-green { border-color: var(--band-green) !important; }
.band-amber { border-color: var(--band-amber) !important; }
.band-red { border-color: var(--band-red) !important; }
.band-none { border-color: var(--band-none) !important; }
.class-seg.band-green, .doc-bar-seg.band-green { background: #bbf7d0; }
.class-seg.band-amber, .doc-bar-seg.band-amber { background: #fde68a; }
.class-seg.band-red, .doc-bar-seg.band-red { background: #fecaca; }
.class-seg.band-none, .doc-bar-seg.band-none { background: #e2e8f0; }

.class-list, .doc-list { list-style: none; padding: 0; margin: 12px 0; }
.class-list li, .doc-list li { padding: 4px 0; border-bottom: 1px solid var(--border); }
.class-list a, .doc-list a { text-decoration: none; color: var(--text); border-left: 4px solid var(--band-none); padding-left: 8px; }
.class-list a.band-green, .doc-list a.band-green { border-left-color: var(--band-green); }
.class-list a.band-amber, .doc-list a.band-amber { border-left-color: var(--band-amber); }
.class-list a.band-red, .doc-list a.band-red { border-left-color: var(--band-red); color: #991b1b; font-weight: 600; }
.t2-marker { color: var(--muted); font-size: 0.85em; }

.doc-meta { display: flex; gap: 8px; margin: 8px 0; flex-wrap: wrap; }
.badge { display: inline-block; padding: 2px 8px; border-radius: 10px; background: var(--border); font-size: 0.8em; }
.badge-calibrated { background: #ddd6fe; }
.badge-measured { background: #dbeafe; }
.badge-survivor { background: #d1fae5; }
.badge-drop { background: #fee2e2; }

.violation-header { padding: 8px 12px; border-radius: 4px; border-left: 4px solid var(--band-none); background: var(--panel-bg); font-family: ui-monospace, monospace; font-size: 0.9em; }
.violation-header.band-red { border-left-color: var(--band-red); background: #fef2f2; }
.violation-header.band-amber { border-left-color: var(--band-amber); background: #fffbeb; }
.violation-header.band-green { border-left-color: var(--band-green); }

.size-table, .trim-table { border-collapse: collapse; width: 100%; margin: 8px 0; }
.size-table th, .size-table td, .trim-table th, .trim-table td { text-align: left; padding: 4px 8px; border-bottom: 1px solid var(--border); font-size: 0.9em; }
.trim-table tr.band-red td { background: #fef2f2; }
.trim-table tr.band-amber td { background: #fffbeb; }

.truncation-list { padding-left: 20px; }
.doc-content { background: #0f172a; color: #e2e8f0; padding: 12px; border-radius: 6px; overflow-x: auto; white-space: pre-wrap; word-break: break-word; font-size: 0.8em; max-height: 480px; overflow-y: auto; }

.trim-plan { margin-top: 20px; padding: 12px 16px; background: var(--panel-bg); border: 1px solid var(--border); border-radius: 6px; }
.trim-summary { font-weight: 600; }

body.t-include-t2 .doc-bar-seg[data-tier-ge2="1"],
body.t-include-t2 .class-seg[data-tier-ge2="1"],
body.t-include-t2 .doc-list li[data-tier-ge2="1"],
body.t-include-t2 .t2-marker { display: revert; }
.doc-bar-seg[data-tier-ge2="1"], .class-seg-hatched[data-tier-ge2="1"] { display: none; }
.doc-list li[data-tier-ge2="1"] { display: none; }
.t2-marker { display: none; }

body.t-post-compaction .doc-bar-seg[data-survivor="0"],
body.t-post-compaction .doc-list li[data-survivor="0"] { display: none !important; }

body.t-predicted-drops [data-predicted-drop="1"] { opacity: 0.4; outline: 2px dashed #b45309; outline-offset: -2px; }

[data-calibrated="1"] { }
body.t-calibrated-marking [data-calibrated="1"] { background-image: repeating-linear-gradient(-45deg, rgba(124,58,237,.35) 0 4px, transparent 4px 8px); }
`;

const SHARED_JS = `
(function () {
  "use strict";

  function decodeViewModel() {
    var el = document.getElementById("ctx-scan-data");
    if (!el) return null;
    try {
      var raw = el.textContent.trim();
      var binary = atob(raw);
      var bytes = new Uint8Array(binary.length);
      for (var i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
      var json = new TextDecoder("utf-8").decode(bytes);
      return JSON.parse(json);
    } catch (e) {
      return null;
    }
  }

  var DATA = decodeViewModel();

  function showScreen(id) {
    var screens = document.querySelectorAll(".screen");
    for (var i = 0; i < screens.length; i++) {
      screens[i].hidden = screens[i].id !== id;
    }
    window.scrollTo(0, 0);
  }

  document.addEventListener("click", function (e) {
    var el = e.target && e.target.closest ? e.target.closest("[data-nav-target]") : null;
    if (!el) return;
    e.preventDefault();
    showScreen(el.getAttribute("data-nav-target"));
  });

  document.addEventListener("keydown", function (e) {
    if (e.key !== "Enter" && e.key !== " ") return;
    var el = e.target;
    if (el && el.hasAttribute && el.hasAttribute("data-nav-target")) {
      e.preventDefault();
      showScreen(el.getAttribute("data-nav-target"));
    }
  });

  var toggles = document.querySelectorAll("[data-toggle]");
  for (var t = 0; t < toggles.length; t++) {
    var cb = toggles[t];
    if (cb.checked) document.body.classList.add("t-" + cb.getAttribute("data-toggle"));
    cb.addEventListener("change", function (ev) {
      var key = ev.target.getAttribute("data-toggle");
      var checked = ev.target.checked;
      document.body.classList.toggle("t-" + key, checked);
      var mirrors = document.querySelectorAll('[data-toggle="' + key + '"]');
      for (var m = 0; m < mirrors.length; m++) mirrors[m].checked = checked;
    });
  }

  if (DATA) {
    var mounts = document.querySelectorAll(".doc-content-mount");
    for (var mIdx = 0; mIdx < mounts.length; mIdx++) {
      (function (mount) {
        // ctx-scan-refs shelf-entry detail sections mount their content by
        // path directly (data-shelf-path) — they were never inserted into
        // DATA.projects[p].classes[c].documents[d] (see refs.ts's module
        // doc), so that index lookup can never resolve them.
        var shelfPath = mount.getAttribute("data-shelf-path");
        var entry = null;
        if (shelfPath) {
          entry = DATA.contentByPath ? DATA.contentByPath[shelfPath] : null;
        } else {
          var p = Number(mount.getAttribute("data-proj"));
          var c = Number(mount.getAttribute("data-cls"));
          var d = Number(mount.getAttribute("data-doc"));
          var proj = DATA.projects && DATA.projects[p];
          var cls = proj && proj.classes && proj.classes[c];
          var doc = cls && cls.documents && cls.documents[d];
          entry = doc && DATA.contentByPath ? DATA.contentByPath[doc.path] : null;
        }
        mount.innerHTML = "";
        if (!entry || entry.preview === null || entry.preview === undefined) {
          var pEmpty = document.createElement("p");
          pEmpty.className = "empty";
          pEmpty.textContent = "Content unavailable (source file not readable from this machine at render time).";
          mount.appendChild(pEmpty);
          return;
        }
        var pre = document.createElement("pre");
        pre.className = "doc-content";
        pre.textContent = entry.preview;
        mount.appendChild(pre);
        if (entry.truncated) {
          var hint = document.createElement("p");
          hint.className = "hint";
          hint.textContent = "Preview truncated for display.";
          mount.appendChild(hint);
        }
      })(mounts[mIdx]);
    }
  }

  var initial = document.documentElement.getAttribute("data-initial-screen") || "level0";
  showScreen(initial);
})();
`;

/**
 * Build the complete self-contained HTML report for `fleetDoc`. Annotates
 * rubric `bands` onto every Node first (mirroring `audit.ts`'s own
 * established call-site pattern — `ctx-scan scan`'s raw output has empty
 * `bands: []` on every Node; only a rubric-consuming command computes them),
 * then derives the view model and stitches every level + the trim panel
 * together.
 */
export function renderFleetHtml(fleetDoc: Fleet, opts: RenderOptions = {}): string {
  annotateFleetBands(fleetDoc);
  const vm = buildViewModel(fleetDoc);

  let initialScreenId = "level0";
  if (opts.project) {
    const idx = vm.projects.findIndex((p) => p.name === opts.project);
    if (idx === -1) {
      process.stderr.write(
        `[ctx-scan render] no project named "${opts.project}" found in the scanned fleet — falling back to the fleet view.\n`,
      );
    } else {
      initialScreenId = `level1-${idx}`;
    }
  }

  const level0Html = renderLevel0(vm.fleet, { historyFilePath: opts.historyFilePath });

  // Resolved once per render — cheap (a single realpath call); ctx-scan-refs's
  // shelf panel needs the real global `~/.claude` layer path to discover
  // skill/command/agent-owned references/rules/memory files (see refs.ts's
  // module doc — this is a self-contained, parallel discovery pass, never
  // threaded through `fleetDoc` itself).
  const claudeHome = getGlobalLayer().path;

  // Computed once per project, ahead of `dataB64` below, so each shelf
  // entry's own content-preview cache can be merged into `vm.contentByPath`
  // before the view model is serialized (the references shelf [3.1]'s
  // detail-view "Content" section reads that SAME cache by path — see
  // `SHARED_JS`'s `data-shelf-path` mount lookup). A discovery failure
  // (unreadable dir, permission error) degrades to an empty shelf for that
  // project rather than aborting the whole render.
  const shelfByProject = vm.projects.map((project, projIdx) => {
    try {
      return renderProjectShelf(project.path, claudeHome, projIdx, { skill: opts.skill });
    } catch (err) {
      process.stderr.write(
        `[ctx-scan render] references shelf computation failed for project "${project.name}": ${
          err instanceof Error ? err.message : String(err)
        }\n`,
      );
      return { linkHtml: "", screensHtml: "", contentByPath: {} };
    }
  });
  for (const shelf of shelfByProject) {
    for (const [path, entry] of Object.entries(shelf.contentByPath)) {
      if (!(path in vm.contentByPath)) vm.contentByPath[path] = entry;
    }
  }

  const projectSections = vm.projects
    .map((project, projIdx) => {
      const trimPlan = computeTrimPlan(project, fleetDoc);
      const level1Html = renderLevel1(project, projIdx);
      const trimHtml = renderTrimPlanHtml(trimPlan, projIdx);
      const level2Sections = project.classes.map((cls, clsIdx) => renderLevel2(cls, projIdx, clsIdx)).join("\n");
      const level3Sections = project.classes
        .flatMap((cls, clsIdx) => cls.documents.map((doc, docIdx) => renderLevel3(doc, projIdx, clsIdx, docIdx)))
        .join("\n");

      const shelf = shelfByProject[projIdx]!;

      // Attach the trim-plan panel + shelf link right after the stacked bar,
      // inside the same level-1 `.screen` element — both are panels
      // alongside the project view, not their own drill-down level.
      const level1WithPanels = level1Html.replace(/<\/section>\s*$/, `${trimHtml}\n${shelf.linkHtml}\n</section>`);
      return `${level1WithPanels}\n${level2Sections}\n${level3Sections}\n${shelf.screensHtml}`;
    })
    .join("\n");

  const dataB64 = Buffer.from(JSON.stringify(vm), "utf8").toString("base64");

  return `<!doctype html>
<html lang="en" data-initial-screen="${initialScreenId}">
<head>
<meta charset="utf-8" />
<meta name="viewport" content="width=device-width, initial-scale=1" />
<title>ctx-scan report — ${escapeHtml(fleetDoc.root)}</title>
<style>${SHARED_CSS}</style>
</head>
<body>
<div id="ctx-scan-app">
${level0Html}
${projectSections}
</div>
<script type="application/octet-stream" id="ctx-scan-data">${dataB64}</script>
<script>${SHARED_JS}</script>
</body>
</html>
`;
}

/** Render `fleetDoc` and write it to `outPath` (overwriting any existing file). */
export function writeRenderedFleet(fleetDoc: Fleet, outPath: string, opts: RenderOptions = {}): void {
  const html = renderFleetHtml(fleetDoc, opts);
  writeFileSync(outPath, html, "utf8");
}
