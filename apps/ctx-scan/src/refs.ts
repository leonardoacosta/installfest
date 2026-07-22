/**
 * refs.ts — the references shelf (ctx-scan-refs tasks [1.1], [2.1]-[2.3],
 * [3.1]).
 *
 * Builds a browsable "shelf" of every T3 (on-demand) document `ctx-scan`'s
 * primary token bar deliberately excludes: `references/*.md` files owned by a
 * skill/command/agent, `rules/*.md` files never resolved by the root
 * CLAUDE.md's `@import` chain, and memory topic files under a project's
 * `projects/<slug>/memory/` directory. Each entry is annotated with
 * reachability (a markdown-link citation from its owning body, or `orphan`),
 * size, and `docs/context-budget-rubric.md` Table A's A5 (ToC presence) / A6
 * (nesting depth) bands.
 *
 * Deliberately does NOT touch `pipeline.ts`, `model.ts`'s `Fleet`/`Surface`
 * shape (beyond the additive `reference-file` `NodeClass` — see model.ts's
 * doc), `rubric.ts`, or `audit.ts`: this module reuses `rubric.ts`'s `TABLE_A`
 * row definitions and `bandFor`/`worseBand` functions directly (never
 * re-deriving the A5/A6 threshold numbers), and its shelf-entry Nodes are
 * never inserted into a real `Fleet` — they exist only to reuse
 * `render/level3-document.ts`'s detail-view renderer unchanged (per
 * proposal.md's Impact table). Keeping A5/A6 `computable: false` in
 * `rubric.ts`'s own `TABLE_A` (for the `scan`/`audit` Fleet pipeline, which
 * still has no reference-file ingestion) is the honest state — this shelf is
 * a parallel, self-contained view, not a retrofit of that pipeline.
 */
import { existsSync, readFileSync, readdirSync, statSync, type Dirent } from "node:fs";
import { basename, dirname, join, resolve, sep } from "node:path";
import type { Band, BandVerdict, NodeOrigin } from "./model";
import { resolveImportChain } from "./imports";
import { TABLE_A, bandFor, worseBand } from "./rubric";
import { renderLevel3 } from "./render/level3-document";
import { escapeHtml, fmtCount, type ContentCacheEntry, type DocumentView } from "./render/view-model";

// ─────────────────────────────────────────────────────────────────────────
// [1.1] Shelf-entry view model
// ─────────────────────────────────────────────────────────────────────────

/**
 * One shelf entry — a `references/*.md` file, an un-imported `rules/*.md`
 * file, or a memory topic file. `tocLines`/`nestingLinks` are the raw
 * measurements `tocBand`/`nestingBand` were derived from (via `rubric.ts`'s
 * `bandFor` against the A5/A6 `TABLE_A` rows), kept alongside the bands so
 * the render layer never has to re-read the file to build a violation
 * header.
 */
export interface ShelfEntry {
  path: string;
  /** Owning skill/command/agent name, or the literal bucket `"rules"`/`"memory"` for those two categories. */
  owner: string;
  reachable: boolean;
  /** Present only when `reachable` — e.g. `"routed from SKILL.md line 42"`. */
  citation?: string;
  /** Raw char count of the file on disk. */
  size: number;
  tocBand: BandVerdict;
  nestingBand: BandVerdict;
  /** Line count the A5 band was computed from. */
  tocLines: number;
  /** Nested-reference-link count the A6 band was computed from. */
  nestingLinks: number;
  origin: NodeOrigin;
}

export interface ShelfGroup {
  owner: string;
  entries: ShelfEntry[];
}

const A5_ROW = TABLE_A.find((r) => r.id === "A5")!;
const A6_ROW = TABLE_A.find((r) => r.id === "A6")!;

// ─────────────────────────────────────────────────────────────────────────
// Small shared fs helpers (each ctx-scan module keeps its own copy of these —
// established convention: discovery.ts, assembly.ts, and pipeline.ts each
// already carry their own private `isDirLike`/safe-read helpers rather than
// sharing one, since none of them export it).
// ─────────────────────────────────────────────────────────────────────────

function readFileSafe(path: string): string | null {
  try {
    return readFileSync(path, "utf8");
  } catch {
    return null;
  }
}

function safeReaddir(dir: string): Dirent[] {
  try {
    return readdirSync(dir, { withFileTypes: true });
  } catch {
    return [];
  }
}

function isDirLike(entry: Dirent, parent: string): boolean {
  if (entry.isDirectory()) return true;
  if (entry.isSymbolicLink()) {
    try {
      return statSync(join(parent, entry.name)).isDirectory();
    } catch {
      return false;
    }
  }
  return false;
}

// ─────────────────────────────────────────────────────────────────────────
// [1.1] A5 (ToC presence) / A6 (nesting depth) measurement — reusing
// rubric.ts's TABLE_A row definitions + bandFor, never re-deriving the
// published greenMax/amberMax numbers.
// ─────────────────────────────────────────────────────────────────────────

function countLines(content: string): number {
  return content.length === 0 ? 0 : content.split("\n").length;
}

/**
 * ToC = a "Contents"/"Table of Contents" heading, or a link-list (>=2 bullet
 * markdown links), within the first 40 lines — per
 * `docs/context-budget-rubric.md` A5's own measurement column ("ToC =
 * link-list or 'Contents' heading in first 40 lines"). A single bullet link
 * is treated as a reference, not "a list" — the >=2 threshold is this
 * module's own documented design choice for what counts as a list.
 */
function hasTableOfContents(content: string): boolean {
  const first40 = content.split("\n").slice(0, 40);
  const headingRe = /^#{1,6}\s*(table of contents|contents)\s*$/i;
  if (first40.some((l) => headingRe.test(l.trim()))) return true;
  const bulletLinkRe = /^\s*[-*]\s*\[[^\]]+\]\([^)]+\)/;
  const bulletLinkLines = first40.filter((l) => bulletLinkRe.test(l));
  return bulletLinkLines.length >= 2;
}

/**
 * A5's GREEN/AMBER/RED verdict for one reference file's content: ANY length
 * WITH a ToC is GREEN (the rubric's own ToC-conditional exception —
 * `rubric.ts`'s A5 row documents this as "not a clean single-threshold
 * rule," left for this module to implement); without a ToC, banded via
 * `bandFor` against A5's own stored `greenMax`/`amberMax` (100/300 lines).
 */
function computeTocBand(content: string): { band: BandVerdict; lines: number } {
  const lines = countLines(content);
  if (hasTableOfContents(content)) return { band: "GREEN", lines };
  return { band: bandFor(A5_ROW, lines), lines };
}

interface MdLink {
  text: string;
  target: string;
  line: number;
}

/** Extract `[text](target)` markdown links with their 1-indexed source line. */
function extractMarkdownLinks(content: string): MdLink[] {
  const out: MdLink[] = [];
  const linkRe = /\[([^\]]*)\]\(([^)\s]+)\)/g;
  const lines = content.split("\n");
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i]!;
    linkRe.lastIndex = 0;
    let m: RegExpExecArray | null;
    while ((m = linkRe.exec(line))) {
      out.push({ text: m[1]!, target: m[2]!, line: i + 1 });
    }
  }
  return out;
}

/** Resolve a markdown link's target relative to the file it appears in. `null` for external/anchor-only/mailto links. */
function resolveLinkTarget(citingFilePath: string, rawTarget: string): string | null {
  const target = rawTarget.split("#")[0]!.trim();
  if (!target) return null;
  if (/^[a-z][a-z0-9+.-]*:/i.test(target)) return null; // scheme-prefixed (http:, mailto:, etc.) — never local
  return resolve(dirname(citingFilePath), target);
}

/** True when `path` has a path segment literally named "references". */
function isUnderReferencesDir(path: string): boolean {
  return path.split(sep).some((seg) => seg === "references");
}

/** A6's verdict: count of markdown links from `refPath`'s own content to OTHER files under a `references/` dir. */
function computeNestingBand(refPath: string, content: string): { band: BandVerdict; links: number } {
  let count = 0;
  for (const link of extractMarkdownLinks(content)) {
    const resolved = resolveLinkTarget(refPath, link.target);
    if (resolved && resolved !== refPath && isUnderReferencesDir(resolved)) count++;
  }
  return { band: bandFor(A6_ROW, count), links: count };
}

// ─────────────────────────────────────────────────────────────────────────
// [2.1] Reachability detection
// ─────────────────────────────────────────────────────────────────────────

/**
 * Search `citingCandidates` (owning-document bodies) for a markdown-link
 * citation to `refPath`, returning the first match's `"routed from <file>
 * line <N>"` description. Deliberately markdown-link-only — a reference
 * cited only via prose or a backtick code span is reported `orphan`, a
 * documented detection boundary (proposal.md's Risk section), not a bug.
 */
function findCitation(refPath: string, citingCandidates: string[]): string | null {
  for (const citingPath of citingCandidates) {
    const content = readFileSafe(citingPath);
    if (content === null) continue;
    for (const link of extractMarkdownLinks(content)) {
      const resolved = resolveLinkTarget(citingPath, link.target);
      if (resolved === refPath) {
        return `routed from ${basename(citingPath)} line ${link.line}`;
      }
    }
  }
  return null;
}

// ─────────────────────────────────────────────────────────────────────────
// Discovery: references/*.md grouped by owning skill/command/agent
// ─────────────────────────────────────────────────────────────────────────

interface RawCandidate {
  refPath: string;
  owner: string;
  citingCandidates: string[];
  origin: NodeOrigin;
}

/** Every `.md` file directly inside `dir` (non-recursive), excluding README (matches `pipeline.ts`'s `listingFileExcluded` convention — a README is not an owning body). */
function directMdFiles(dir: string): string[] {
  const out: string[] = [];
  for (const entry of safeReaddir(dir)) {
    if (entry.isFile() && entry.name.endsWith(".md") && entry.name.toLowerCase() !== "readme.md") {
      out.push(join(dir, entry.name));
    }
  }
  return out;
}

/** Every `.md` file under `dir`, recursively. */
function walkMarkdownFilesRecursive(dir: string): string[] {
  const out: string[] = [];
  function walk(d: string): void {
    for (const entry of safeReaddir(d)) {
      const full = join(d, entry.name);
      if (isDirLike(entry, d)) walk(full);
      else if (entry.isFile() && entry.name.endsWith(".md")) out.push(full);
    }
  }
  walk(dir);
  return out;
}

/** Find every directory literally named "references" anywhere under `root` (never descending further once found — no nested references-in-references expected). */
function findAllReferencesDirs(root: string): string[] {
  const out: string[] = [];
  function walk(dir: string): void {
    for (const entry of safeReaddir(dir)) {
      if (!isDirLike(entry, dir)) continue;
      const full = join(dir, entry.name);
      if (entry.name === "references") {
        out.push(full);
        continue;
      }
      walk(full);
    }
  }
  walk(root);
  return out;
}

/**
 * Find every `references/*.md` file under `root` (a `skills`/`commands`/
 * `agents` directory), grouped by owning unit. Handles both shapes observed
 * in this fleet's real skill/command/agent libraries:
 *   - `<root>/<name>/references/*.md` with `<root>/<name>/SKILL.md` (or any
 *     `.md` directly in `<name>/`) as the owning body — the skills-dir shape.
 *   - `<root>/<group>/references/*.md` shared by multiple sibling docs, PLUS
 *     a sibling `<root>/<group>.md` file living one level up (the observed
 *     commands/agents convention, e.g. `commands/apply.md` next to
 *     `commands/apply/references/`) — both are included as citing
 *     candidates.
 *   - a top-level `<root>/references/*.md` with no single owning unit —
 *     owner falls back to `basename(root)`, citing candidates are every
 *     top-level `.md` file directly in `root`.
 */
function findGroupedReferenceFiles(root: string, origin: NodeOrigin): RawCandidate[] {
  if (!existsSync(root)) return [];
  const out: RawCandidate[] = [];
  for (const refsDir of findAllReferencesDirs(root)) {
    const unitDir = dirname(refsDir);
    const isTopLevel = unitDir === root;
    const owner = basename(isTopLevel ? root : unitDir);
    const citing = new Set<string>();
    if (isTopLevel) {
      for (const f of directMdFiles(root)) citing.add(f);
    } else {
      for (const f of directMdFiles(unitDir)) citing.add(f);
      const siblingMd = `${unitDir}.md`;
      if (existsSync(siblingMd)) citing.add(siblingMd);
    }
    for (const refPath of walkMarkdownFilesRecursive(refsDir)) {
      out.push({ refPath, owner, citingCandidates: [...citing], origin });
    }
  }
  return out;
}

// ─────────────────────────────────────────────────────────────────────────
// Discovery: un-imported rules/*.md files
// ─────────────────────────────────────────────────────────────────────────

function findUnimportedRulesFiles(rootClaudeMdPath: string, rulesDir: string, origin: NodeOrigin): RawCandidate[] {
  if (!existsSync(rulesDir)) return [];
  // rootClaudeMdPath is always `<projectRoot>/CLAUDE.md` — its dirname IS the
  // project root, which is what @import resolution must stay confined to.
  const imported = new Set(
    existsSync(rootClaudeMdPath) ? resolveImportChain(rootClaudeMdPath, dirname(rootClaudeMdPath)).map((i) => i.path) : [],
  );
  const rulesFiles = directMdFiles(rulesDir);
  const citing = existsSync(rootClaudeMdPath) ? [rootClaudeMdPath, ...rulesFiles] : [...rulesFiles];
  const out: RawCandidate[] = [];
  for (const refPath of rulesFiles) {
    if (imported.has(refPath)) continue; // resolved by the @import chain — not this shelf's concern
    out.push({ refPath, owner: "rules", citingCandidates: citing.filter((c) => c !== refPath), origin });
  }
  return out;
}

// ─────────────────────────────────────────────────────────────────────────
// Discovery: memory topic files
// ─────────────────────────────────────────────────────────────────────────

function projectMemoryDir(claudeHome: string, projectPath: string): string {
  const slug = projectPath.replace(/\//g, "-");
  return join(claudeHome, "projects", slug, "memory");
}

function findMemoryTopicFiles(memoryDir: string, origin: NodeOrigin): RawCandidate[] {
  if (!existsSync(memoryDir)) return [];
  const memoryMd = join(memoryDir, "MEMORY.md");
  const citing = existsSync(memoryMd) ? [memoryMd] : [];
  const out: RawCandidate[] = [];
  for (const entry of safeReaddir(memoryDir)) {
    if (!entry.isFile() || !entry.name.endsWith(".md") || entry.name === "MEMORY.md") continue;
    out.push({ refPath: join(memoryDir, entry.name), owner: "memory", citingCandidates: citing, origin });
  }
  return out;
}

// ─────────────────────────────────────────────────────────────────────────
// [1.1] Top-level shelf assembly
// ─────────────────────────────────────────────────────────────────────────

function buildEntry(candidate: RawCandidate): ShelfEntry | null {
  const content = readFileSafe(candidate.refPath);
  if (content === null) return null; // unreadable (race/dangling) — skip, never fabricate
  const toc = computeTocBand(content);
  const nesting = computeNestingBand(candidate.refPath, content);
  const citation = findCitation(candidate.refPath, candidate.citingCandidates);
  return {
    path: candidate.refPath,
    owner: candidate.owner,
    reachable: citation !== null,
    citation: citation ?? undefined,
    size: content.length,
    tocBand: toc.band,
    nestingBand: nesting.band,
    tocLines: toc.lines,
    nestingLinks: nesting.links,
    origin: candidate.origin,
  };
}

/**
 * Build the full references shelf for `projectPath`: every `references/*.md`
 * file (skills/commands/agents, both the global `claudeHome` layer and the
 * project's own `.claude/{skills,commands,agents}`), every un-imported
 * `rules/*.md` file (global + project), and every memory topic file for this
 * project — deduplicated by path (the same global file is never listed
 * twice even though it is discovered once per category walk).
 */
export function buildShelf(projectPath: string, claudeHome: string): ShelfEntry[] {
  const projectClaudeDir = join(projectPath, ".claude");
  const candidates: RawCandidate[] = [
    ...findGroupedReferenceFiles(join(claudeHome, "skills"), "global"),
    ...findGroupedReferenceFiles(join(claudeHome, "commands"), "global"),
    ...findGroupedReferenceFiles(join(claudeHome, "agents"), "global"),
    ...findGroupedReferenceFiles(join(projectClaudeDir, "skills"), "project"),
    ...findGroupedReferenceFiles(join(projectClaudeDir, "commands"), "project"),
    ...findGroupedReferenceFiles(join(projectClaudeDir, "agents"), "project"),
    ...findUnimportedRulesFiles(join(claudeHome, "CLAUDE.md"), join(claudeHome, "rules"), "global"),
    ...findUnimportedRulesFiles(join(projectPath, "CLAUDE.md"), join(projectClaudeDir, "rules"), "project"),
    ...findMemoryTopicFiles(projectMemoryDir(claudeHome, projectPath), "project"),
  ];

  const seen = new Set<string>();
  const entries: ShelfEntry[] = [];
  for (const candidate of candidates) {
    if (seen.has(candidate.refPath)) continue;
    seen.add(candidate.refPath);
    const entry = buildEntry(candidate);
    if (entry) entries.push(entry);
  }
  return entries;
}

// ─────────────────────────────────────────────────────────────────────────
// [2.2] Group-by-owner assembly
// ─────────────────────────────────────────────────────────────────────────

export function groupShelfByOwner(entries: ShelfEntry[]): ShelfGroup[] {
  const byOwner = new Map<string, ShelfEntry[]>();
  for (const e of entries) {
    const list = byOwner.get(e.owner);
    if (list) list.push(e);
    else byOwner.set(e.owner, [e]);
  }
  return Array.from(byOwner.entries())
    .map(([owner, list]) => ({ owner, entries: list.sort((a, b) => a.path.localeCompare(b.path)) }))
    .sort((a, b) => a.owner.localeCompare(b.owner));
}

// ─────────────────────────────────────────────────────────────────────────
// [2.3] Per-skill (per-owner) scoping
// ─────────────────────────────────────────────────────────────────────────

/** Scope the shelf to entries owned by exactly `owner` — "what could this skill pull in, and what of it is currently unreachable." */
export function scopeShelfToOwner(entries: ShelfEntry[], owner: string): ShelfEntry[] {
  return entries.filter((e) => e.owner === owner);
}

// ─────────────────────────────────────────────────────────────────────────
// [3.1] Render wiring — a project-scoped shelf panel, grouped by owner, each
// entry click-opening into `render/level3-document.ts`'s detail view (reused
// UNCHANGED: called as-is, then its own self-consistent id/back-link pair is
// relabeled into the shelf's own screen-id namespace — see module doc — so
// it can never collide with a real level-3 document screen for the same
// project index, and so its back-link actually returns to the shelf).
// ─────────────────────────────────────────────────────────────────────────

/** Sentinel `clsIdx` offset for shelf detail sections — far outside any real class-index range, so the (discarded) intermediate id can never collide before being relabeled. */
const SHELF_SENTINEL_CLS_BASE = 900_000;

function buildShelfDocumentView(entry: ShelfEntry): DocumentView {
  const bands: Band[] = [
    { rule: "A5", band: entry.tocBand, measured: entry.tocLines, limit: A5_ROW.limit },
    { rule: "A6", band: entry.nestingBand, measured: entry.nestingLinks, limit: A6_ROW.limit },
  ];
  return {
    path: entry.path,
    displayName: basename(entry.path),
    cls: "reference-file",
    tier: 3, // T3 (on-demand) — see model.ts's NodeClass doc for why this class never appears in a real Fleet Surface.
    origin: entry.origin,
    rawChars: entry.size,
    effectiveChars: entry.size,
    estTokens: Math.ceil(entry.size / 4),
    truncations: [],
    bands,
    worstBand: worseBand(entry.tocBand, entry.nestingBand),
    order: null,
    isCalibratedConstant: false,
    isPostCompactionSurvivor: false,
    isPredictedDrop: false,
    dropRiskRank: null,
  };
}

/** First N chars of a shelf entry's real source file, re-read at render time — same 8,000-char cap + degradation convention as `view-model.ts`'s own `readContentPreview`. */
function readShelfContentPreview(path: string, capChars = 8000): ContentCacheEntry {
  const content = readFileSafe(path);
  if (content === null) return { preview: null, truncated: false };
  if (content.length <= capChars) return { preview: content, truncated: false };
  return { preview: content.slice(0, capChars), truncated: true };
}

function renderShelfEntryDetail(entry: ShelfEntry, projIdx: number, ownerIdx: number, entryIdx: number): string {
  const doc = buildShelfDocumentView(entry);
  const sentinelCls = SHELF_SENTINEL_CLS_BASE + ownerIdx;
  const raw = renderLevel3(doc, projIdx, sentinelCls, entryIdx);
  const rawId = `level3-${projIdx}-${sentinelCls}-${entryIdx}`;
  const rawBack = `level2-${projIdx}-${sentinelCls}`;
  const rawMount = `data-proj="${projIdx}" data-cls="${sentinelCls}" data-doc="${entryIdx}">`;
  const newId = `shelf-doc-${projIdx}-${ownerIdx}-${entryIdx}`;
  const newBack = `shelf-group-${projIdx}-${ownerIdx}`;
  // Shelf documents were never inserted into the view model's
  // `projects[p].classes[c].documents[d]` array, so the mount's default
  // index-based lookup can never resolve them — inject a direct
  // `data-shelf-path` attribute (`SHARED_JS` in `render.ts` checks for this
  // first) rather than editing `level3-document.ts`'s own mount markup.
  const newMount = `data-proj="${projIdx}" data-cls="${sentinelCls}" data-doc="${entryIdx}" data-shelf-path="${escapeHtml(
    entry.path,
  )}">`;
  return raw
    .replace(`id="${rawId}"`, `id="${newId}"`)
    .replace(`data-nav-target="${rawBack}"`, `data-nav-target="${newBack}"`)
    .replace(rawMount, newMount);
}

function bandCss(band: string): string {
  return band === "NONE" ? "band-none" : `band-${band.toLowerCase()}`;
}

function renderShelfGroupSection(group: ShelfGroup, projIdx: number, ownerIdx: number): string {
  const rows = group.entries
    .map((e, i) => {
      const worst = worseBand(e.tocBand, e.nestingBand);
      const status = e.reachable ? (e.citation ?? "reachable") : "orphan";
      return `    <li><a href="#" data-nav-target="shelf-doc-${projIdx}-${ownerIdx}-${i}" class="${bandCss(worst)}">${escapeHtml(
        basename(e.path),
      )}</a> — ${escapeHtml(status)} — ${fmtCount(e.size)} chars — ToC ${e.tocBand} — Nesting ${e.nestingBand}</li>`;
    })
    .join("\n");
  return `<section id="shelf-group-${projIdx}-${ownerIdx}" class="screen" hidden>
  <a href="#" data-nav-target="shelf-${projIdx}" class="back-link">&larr; Back to shelf</a>
  <h3>${escapeHtml(group.owner)}</h3>
  <ul class="doc-list">
${rows || '    <li class="empty">No shelf entries for this owner.</li>'}
  </ul>
</section>`;
}

function renderShelfHome(groups: ShelfGroup[], projIdx: number): string {
  const rows = groups
    .map((g, i) => {
      const orphanCount = g.entries.filter((e) => !e.reachable).length;
      const orphanNote = orphanCount > 0 ? `, ${orphanCount} orphan${orphanCount === 1 ? "" : "s"}` : "";
      return `    <li><a href="#" data-nav-target="shelf-group-${projIdx}-${i}">${escapeHtml(g.owner)}</a> — ${
        g.entries.length
      } file${g.entries.length === 1 ? "" : "s"}${orphanNote}</li>`;
    })
    .join("\n");
  return `<section id="shelf-${projIdx}" class="screen" hidden>
  <a href="#" data-nav-target="level1-${projIdx}" class="back-link">&larr; Back to project</a>
  <h2>References shelf</h2>
  <p class="hint">Every references/ file, un-imported rules file, and memory topic file this project can reach —
  reachability, size, ToC (A5), and nesting-depth (A6) bands, grouped by owner.</p>
  <ul class="class-list">
${rows || '    <li class="empty">No shelf entries discovered for this project.</li>'}
  </ul>
</section>`;
}

/** Small always-visible link into the shelf, attached alongside the level-1 project screen (same pattern `render.ts` already uses for the trim-plan panel). */
function renderShelfLinkHtml(groups: ShelfGroup[], projIdx: number): string {
  const totalEntries = groups.reduce((sum, g) => sum + g.entries.length, 0);
  const totalOrphans = groups.reduce((sum, g) => sum + g.entries.filter((e) => !e.reachable).length, 0);
  const orphanNote = totalOrphans > 0 ? ` (${fmtCount(totalOrphans)} orphaned)` : "";
  return `<div class="trim-plan" id="shelf-link-${projIdx}">
  <h3>References shelf</h3>
  <p class="hint"><a href="#" data-nav-target="shelf-${projIdx}">Browse ${fmtCount(totalEntries)} reference/rules/memory file${
    totalEntries === 1 ? "" : "s"
  }${orphanNote} &rarr;</a></p>
</div>`;
}

export interface ShelfRender {
  /** Small link snippet meant to be attached inline alongside the level-1 project screen. */
  linkHtml: string;
  /** Every `.screen` section the shelf owns (home + per-owner groups + per-entry details). */
  screensHtml: string;
  /** Path-keyed content-preview cache for this project's shelf entries — merge into the view model's own `contentByPath` before serializing (see `render.ts`). */
  contentByPath: Record<string, ContentCacheEntry>;
}

/**
 * Build the full shelf render for one project: discovers the shelf (scoped
 * to `opts.skill` when given, per proposal.md's per-skill-focused-view
 * Requirement), groups by owner, and renders every screen.
 */
export function renderProjectShelf(
  projectPath: string,
  claudeHome: string,
  projIdx: number,
  opts: { skill?: string } = {},
): ShelfRender {
  let entries = buildShelf(projectPath, claudeHome);
  if (opts.skill) entries = scopeShelfToOwner(entries, opts.skill);
  const groups = groupShelfByOwner(entries);

  const home = renderShelfHome(groups, projIdx);
  const groupSections = groups.map((g, i) => renderShelfGroupSection(g, projIdx, i)).join("\n");
  const detailSections = groups
    .flatMap((g, gi) => g.entries.map((e, ei) => renderShelfEntryDetail(e, projIdx, gi, ei)))
    .join("\n");

  const contentByPath: Record<string, ContentCacheEntry> = {};
  for (const entry of entries) contentByPath[entry.path] = readShelfContentPreview(entry.path);

  return {
    linkHtml: renderShelfLinkHtml(groups, projIdx),
    screensHtml: [home, groupSections, detailSections].filter(Boolean).join("\n"),
    contentByPath,
  };
}
