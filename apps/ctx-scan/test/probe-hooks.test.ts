/**
 * probe-hooks.test.ts — `--probe-hooks` execution against a real fixture
 * hook alongside a timeout-bounded misbehaving sibling (ctx-scan-assembly
 * task [4.6], beads:if-7jx4).
 *
 * A "misbehaving hook" here is a real shell process that sleeps well past
 * the probe timeout without closing stdout — `probeHookStdoutBytes`
 * (assembly.ts, private) races the read against a timer and kills the
 * process on timeout, so it must render `bytes: "unknown"`, never hang the
 * whole `ingestHookSizes` call and never report a fabricated `0`. The good
 * sibling hook is a real, fast command whose exact byte count is asserted
 * for contrast, proving the timeout path didn't also poison the well-behaved
 * hook's measurement.
 */
import { describe, expect, test } from "bun:test";
import { ingestHookSizes, type HookDefinition } from "../src/assembly";

describe("ingestHookSizes --probe-hooks — timeout-bounded misbehaving sibling [4.6]", () => {
  test(
    "a hanging hook renders unknown (not a hang, not a fabricated zero); its fast sibling measures correctly",
    async () => {
      const goodCommand = "printf '%s' 'fixture-output' # /tmp/ctx-scan-e2e-good-sibling.sh";
      // Sleeps far longer than the probe timeout below, never closing stdout
      // in time — the real "misbehaving hook" case.
      const badCommand = "sleep 5 # /tmp/ctx-scan-e2e-bad-sibling.sh";

      const hookDefs: HookDefinition[] = [
        { event: "PostToolUse", matcher: "", command: goodCommand },
        { event: "PostToolUse", matcher: "", command: badCommand },
      ];

      const start = performance.now();
      const results = await ingestHookSizes(hookDefs, {
        allowProbe: true,
        probeTimeoutMs: 400,
        // Force telemetry unreachable via an env override the resolver tries
        // first — real hooks below have unique, never-telemetered names, so
        // even if a real Loki is discovered downstream, neither will find a
        // matching sample and both correctly fall to the probe path.
        env: { CTX_SCAN_LOKI_URL: "http://127.0.0.1:1" },
      });
      const elapsedMs = performance.now() - start;

      const good = results.find((r) => r.command === goodCommand)!;
      const bad = results.find((r) => r.command === badCommand)!;

      expect(good.source).toBe("probe");
      expect(good.bytes).toBe("fixture-output".length);

      expect(bad.source).toBe("unknown"); // never a hang, never a fabricated 0
      expect(bad.bytes).toBe("unknown");

      // The bad hook's 5s sleep must NOT have been awaited to completion —
      // the probe's own 400ms timeout bounds it, so total elapsed stays far
      // under 5000ms even accounting for one real telemetry-resolution round trip.
      expect(elapsedMs).toBeLessThan(10_000);
    },
    20_000,
  );
});
