/**
 * browser.ts — headless-Chromium test driver for ctx-scan-render's E2E tasks
 * ([4.1], [4.2], [4.3], [4.5]).
 *
 * Reuse search before adding anything (rules/CORE.md Reader Gate): this
 * monorepo has no `playwright`/`jsdom`/`happy-dom` dependency anywhere
 * (`grep -r playwright --include package.json`, `find -iname jsdom` — both
 * empty across the workspace). Rather than adding a new dependency for one
 * proposal's E2E batch, this drives the system's already-installed
 * `chromium` binary (`/usr/bin/chromium`, confirmed present) directly via its
 * `--headless=new --dump-dom` CLI mode: it executes the page's REAL inline
 * script (including `atob`/`TextDecoder` view-model decode and the click
 * delegation logic) in a REAL DOM/CSS engine, so assertions here are genuine
 * runtime verification — not a string-contains proxy.
 *
 * How it works: a driver script (plain JS, runs synchronously after the
 * page's own `<script>` tags execute, since ours is appended immediately
 * before `</body>` and render.ts's SHARED_JS is a synchronous IIFE with no
 * DOMContentLoaded/load gating) performs assertions/clicks/reads against the
 * live DOM, then serializes its findings into `document.title` as a
 * percent-encoded JSON string — percent-encoding sidesteps HTML-entity
 * re-escaping ambiguity when `--dump-dom` serializes the mutated document
 * back out. The dumped DOM's `<title>` is regex-extracted and decoded here.
 */
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

const CHROMIUM_BIN = process.env.CTX_SCAN_TEST_CHROMIUM ?? "chromium";

/**
 * Render `html` (a complete document, e.g. `renderFleetHtml`'s output) in
 * headless Chromium with `driverJs` appended as a final synchronous script,
 * and return whatever JSON-serializable value `driverJs` assigns to the
 * in-page global `__RESULT__`.
 *
 * `networkDisabled` (task [4.1]'s airplane test) remaps every hostname to
 * the reserved/unroutable `240.0.0.1` range via Chromium's own
 * `--host-resolver-rules`, so any external reference the page tried to make
 * would fail fast rather than silently succeeding because this sandbox
 * happens to have egress.
 */
export function runInBrowser(
  html: string,
  driverJs: string,
  opts: { networkDisabled?: boolean; timeoutMs?: number } = {},
): unknown {
  const dir = mkdtempSync(join(tmpdir(), "ctx-scan-browser-"));
  const htmlPath = join(dir, "page.html");
  const driver = `
<script>
(function () {
  try {
    ${driverJs}
  } catch (e) {
    window.__RESULT__ = { driverError: String(e && e.stack || e) };
  }
  document.title = encodeURIComponent(JSON.stringify(window.__RESULT__ === undefined ? null : window.__RESULT__));
})();
</script>
`;
  const lastBodyClose = html.lastIndexOf("</body>");
  const withDriver =
    lastBodyClose === -1 ? html + driver : html.slice(0, lastBodyClose) + driver + html.slice(lastBodyClose);
  writeFileSync(htmlPath, withDriver, "utf8");

  try {
    const args = [
      "--headless=new",
      "--disable-gpu",
      "--no-sandbox",
      "--disable-dev-shm-usage",
      "--virtual-time-budget=4000",
      "--dump-dom",
    ];
    if (opts.networkDisabled) {
      args.push("--host-resolver-rules=MAP * 240.0.0.1", "--proxy-server=240.0.0.1:1");
    }
    args.push(`file://${htmlPath}`);

    const result = Bun.spawnSync([CHROMIUM_BIN, ...args], {
      timeout: opts.timeoutMs ?? 20_000,
    });
    if (!result.success) {
      const stderr = result.stderr?.toString() ?? "";
      throw new Error(`chromium exited ${result.exitCode}: ${stderr.slice(0, 2000)}`);
    }
    const dumped = result.stdout.toString();
    const match = dumped.match(/<title>([^<]*)<\/title>/);
    if (!match) {
      throw new Error("no <title> found in dumped DOM — driver script never ran or crashed before setting it");
    }
    const decoded = decodeURIComponent(match[1]!);
    return JSON.parse(decoded);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
}

/** True when `CHROMIUM_BIN` resolves on PATH — lets a test skip gracefully in an environment with no browser, rather than fail opaquely. */
export function chromiumAvailable(): boolean {
  const result = Bun.spawnSync([CHROMIUM_BIN, "--version"], { timeout: 5_000 });
  return result.success;
}
