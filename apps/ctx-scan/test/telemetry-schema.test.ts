/**
 * telemetry-schema.test.ts — schema self-verification against a reachable
 * endpoint, both the valid-schema and the degraded-to-`unavailable` cases
 * (ctx-scan-assembly task [4.5], beads:if-aa5w).
 *
 * Uses the `fake-loki` fixture server so both cases are hermetic and
 * independent of whatever real telemetry infrastructure happens to be
 * running on the host — the "unreachable" half of the Requirement
 * ("Any resolution or schema-assertion failure SHALL degrade... to
 * unavailable... reason recorded") is exercised here as a SCHEMA failure
 * (an endpoint that answers but never yields a schema-complete sample),
 * since this module's own endpoint-resolution order falls through to a real
 * docker-discovered Loki container on a dev box with one running — the
 * schema-assertion layer is the honestly-controllable failure surface.
 * `probeTelemetry` never throws in either case — both resolve to a tagged
 * result, never a thrown exception (the "exit 0 either way" contract the
 * `calibrate` CLI command relies on).
 */
import { afterEach, describe, expect, test } from "bun:test";
import { probeTelemetry, verifyEventSchema } from "../src/telemetry-probe";
import { startFakeLoki, type FakeLokiServer } from "./helpers/fake-loki";

let loki: FakeLokiServer | null = null;

afterEach(() => {
  loki?.stop();
  loki = null;
});

describe("probeTelemetry — reachable + schema-valid case [4.5]", () => {
  test(
    "a schema-complete sample resolves to status: available with provenance",
    async () => {
      loki = startFakeLoki([
        {
          eventType: "api_request",
          line: {
            body: "claude_code.api_request",
            attributes: { "session.id": "sess-1", cache_read_tokens: 100, cache_creation_tokens: 20 },
          },
        },
      ]);

      const result = await probeTelemetry("api_request", { env: { CTX_SCAN_LOKI_URL: loki.url } });

      expect(result.status).toBe("available");
      if (result.status === "available") {
        expect(result.sampled).toBeGreaterThan(0);
        expect(result.provenance.endpoint).toBe(loki.url);
        expect(result.provenance.query).toContain("api_request");
      }
    },
    15_000,
  );
});

describe("probeTelemetry — reachable but schema-incomplete/empty case degrades to unavailable [4.5]", () => {
  test(
    "an endpoint with zero matching events degrades to unavailable, with a recorded reason, no throw",
    async () => {
      // Reachable, labeled, but the query_range window has no events at all
      // for this event type — a legitimate "endpoint up, nothing to trust yet" case.
      loki = startFakeLoki([]);

      let threw = false;
      let result: Awaited<ReturnType<typeof probeTelemetry>> | undefined;
      try {
        result = await probeTelemetry("hook_output_metrics", { env: { CTX_SCAN_LOKI_URL: loki.url } });
      } catch {
        threw = true;
      }

      expect(threw).toBe(false); // never throws — degrades instead
      expect(result!.status).toBe("unavailable");
      if (result!.status === "unavailable") {
        expect(result!.reason.length).toBeGreaterThan(0);
        expect(result!.reason).toContain("hook_output_metrics");
      }
    },
    15_000,
  );

  test(
    "events present but missing required attributes degrade to unavailable with a 'schema drift' reason",
    async () => {
      // hook_output_metrics requires hook_name + stdout_bytes + duration_ms —
      // this sample is missing duration_ms entirely (schema drift).
      loki = startFakeLoki([
        {
          eventType: "hook_output_metrics",
          line: { event: { event_type: "hook_output_metrics", hook_name: "some-hook", stdout_bytes: 123 } },
        },
      ]);

      const resolution = await probeTelemetry("hook_output_metrics", { env: { CTX_SCAN_LOKI_URL: loki.url } });

      expect(resolution.status).toBe("unavailable");
      if (resolution.status === "unavailable") {
        expect(resolution.reason).toContain("schema drift");
        expect(resolution.reason).toContain("duration_ms");
      }
    },
    15_000,
  );

  test(
    "verifyEventSchema itself never throws on an empty endpoint",
    async () => {
      loki = startFakeLoki([]);
      const { resolveLokiEndpoint } = await import("../src/telemetry-probe");
      const resolution = await resolveLokiEndpoint({ CTX_SCAN_LOKI_URL: loki.url });
      expect(resolution.ok).toBe(true);
      if (!resolution.ok) return;

      let threw = false;
      let outcome: Awaited<ReturnType<typeof verifyEventSchema>> | undefined;
      try {
        outcome = await verifyEventSchema(resolution.endpoint, "api_request");
      } catch {
        threw = true;
      }
      expect(threw).toBe(false);
      expect(outcome!.ok).toBe(false);
    },
    15_000,
  );
});
