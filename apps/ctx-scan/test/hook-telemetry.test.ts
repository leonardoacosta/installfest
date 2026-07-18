/**
 * hook-telemetry.test.ts — hook-size ingestion prefers telemetry over probe
 * (ctx-scan-assembly task [4.4], beads:if-h3x3).
 *
 * Uses the `fake-loki` fixture server (a real `Bun.serve` instance, not a
 * mocked `fetch`) to hand back a `hook_output_metrics` sample for a fixture
 * hook whose command is ALSO directly executable — if `ingestHookSizes` ever
 * regressed to preferring (or falling back to) the probe path despite a real
 * telemetry match, the observed byte count would be the probed "hello-world"
 * (11 bytes), not the fixture's distinct telemetry value (4096) — making the
 * two paths trivially distinguishable.
 */
import { afterEach, describe, expect, test } from "bun:test";
import { ingestHookSizes, type HookDefinition } from "../src/assembly";
import { startFakeLoki, type FakeLokiServer } from "./helpers/fake-loki";

let loki: FakeLokiServer | null = null;

afterEach(() => {
  loki?.stop();
  loki = null;
});

describe("ingestHookSizes — telemetry preferred over probe when both are available [4.4]", () => {
  test(
    "a hook with a real telemetry sample reports the telemetry byte count, not the probed one",
    async () => {
      // Real, directly-executable command whose actual stdout ("hello-world",
      // 11 bytes) is deliberately DIFFERENT from the fixture's telemetry byte
      // count (4096) — proves which source actually won. `commandHookName`
      // (assembly.ts) derives the hook identity from the basename of the
      // LAST whitespace-separated token in the command, so the fake
      // telemetry sample's `hook_name` must match that exact basename
      // (minus `.sh`), not just the trailing "myhook" substring.
      const command = "printf '%s' 'hello-world' # /tmp/ctx-scan-e2e-myhook.sh";

      loki = startFakeLoki([
        {
          eventType: "hook_output_metrics",
          line: {
            event: {
              event_type: "hook_output_metrics",
              hook_name: "ctx-scan-e2e-myhook",
              stdout_bytes: 4096,
              duration_ms: 7,
            },
          },
        },
      ]);

      const hookDefs: HookDefinition[] = [{ event: "PostToolUse", matcher: "", command }];

      const results = await ingestHookSizes(hookDefs, {
        allowProbe: true, // probe IS allowed — telemetry must still win
        probeTimeoutMs: 2000,
        env: { CTX_SCAN_LOKI_URL: loki.url },
      });

      expect(results).toHaveLength(1);
      const result = results[0]!;
      expect(result.source).toBe("telemetry");
      expect(result.bytes).toBe(4096);
      expect(result.bytes).not.toBe(11); // the real probed byte count, proving probe was NOT used
      expect(result.provenance).toBeDefined();
      expect(result.provenance!.endpoint).toBe(loki!.url);
    },
    15_000,
  );

  test(
    "a hook with NO matching telemetry sample falls back to the real probe",
    async () => {
      const command = "printf '%s' 'hello-world' # /tmp/ctx-scan-e2e-unmatched-hook.sh";

      // Telemetry is reachable and schema-valid, but carries no sample for
      // THIS hook's name — must fall through to probing, not "telemetry: 0".
      loki = startFakeLoki([
        {
          eventType: "hook_output_metrics",
          line: {
            event: { event_type: "hook_output_metrics", hook_name: "some-other-hook", stdout_bytes: 999, duration_ms: 3 },
          },
        },
      ]);

      const hookDefs: HookDefinition[] = [{ event: "PostToolUse", matcher: "", command }];
      const results = await ingestHookSizes(hookDefs, {
        allowProbe: true,
        probeTimeoutMs: 2000,
        env: { CTX_SCAN_LOKI_URL: loki.url },
      });

      expect(results).toHaveLength(1);
      expect(results[0]!.source).toBe("probe");
      expect(results[0]!.bytes).toBe(11); // "hello-world".length
    },
    15_000,
  );
});
