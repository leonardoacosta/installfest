/**
 * fake-loki.ts — hermetic HTTP fixture server standing in for a Loki
 * endpoint, for ctx-scan-assembly tasks [4.4]/[4.5]. A REAL `Bun.serve`
 * instance (not a mocked `fetch`), so `telemetry-probe.ts`'s actual HTTP
 * plumbing (`lokiFetch`/`checkReady`/`sampleEvents`) runs unmodified against
 * it — the only thing under test control is which log lines the fixture
 * hands back for `/loki/api/v1/query_range`.
 *
 * Serves the three endpoints `telemetry-probe.ts` calls:
 *   - `GET /ready`                    -> 200 (reachability check)
 *   - `GET /loki/api/v1/labels`       -> `{status:"success", data:[...]}`
 *   - `GET /loki/api/v1/query_range`  -> `{status:"success", data:{result:[{values}]}}`,
 *     filtered by whichever `eventType` substring appears in the `query` param
 *     (mirrors `EVENT_SCHEMAS`'s two distinct LogQL selectors: `"api_request"`
 *     vs `"hook_output_metrics"` — a substring match cleanly disambiguates them).
 */
import type { KnownEventType } from "../../src/telemetry-probe";

export interface FakeLokiEvent {
  eventType: KnownEventType;
  /** Raw JSON value for this log line — shaped per `EVENT_SCHEMAS[eventType].extractAttributes`. */
  line: unknown;
}

export interface FakeLokiServer {
  url: string;
  stop: () => void;
}

/** Start a fake Loki server pre-loaded with `events`. `labels` defaults to a non-empty set. */
export function startFakeLoki(events: FakeLokiEvent[], labels: string[] = ["service_name"]): FakeLokiServer {
  let tsCounter = 0n;
  const server = Bun.serve({
    port: 0,
    fetch(req) {
      const url = new URL(req.url);
      if (url.pathname === "/ready") {
        return new Response("ready");
      }
      if (url.pathname === "/loki/api/v1/labels") {
        return Response.json({ status: "success", data: labels });
      }
      if (url.pathname === "/loki/api/v1/query_range") {
        const query = url.searchParams.get("query") ?? "";
        const matching = events.filter((e) => query.includes(e.eventType));
        const values: [string, string][] = matching.map((e) => {
          tsCounter += 1n;
          const tsNs = (BigInt(Date.now()) * 1_000_000n + tsCounter).toString();
          return [tsNs, JSON.stringify(e.line)];
        });
        return Response.json({
          status: "success",
          data: { result: values.length > 0 ? [{ values }] : [] },
        });
      }
      return new Response("not found", { status: 404 });
    },
  });
  return { url: `http://127.0.0.1:${server.port}`, stop: () => server.stop(true) };
}
