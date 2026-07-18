/**
 * mxActions.test.ts — 401-then-retry radar action suite (add-daily-brief-tui
 * task 4.3, beads:if-77uy).
 *
 * A `Bun.serve` stub stands in for mx-gateway and records every request it
 * receives (method, path, Authorization header). Runs the REAL
 * `snoozeTriageItem`/`setTriageStatus` exports as a fresh subprocess (see
 * run-open-items.ts's header comment for why) with `MX_GATEWAY_URL`
 * pointed at the stub and `MX_GATEWAY_ENV_PATH` pointed at a fixture
 * `gateway.env` (never Leo's real `~/.mx/gateway.env`).
 */
import { afterEach, describe, expect, test } from "bun:test";
import { mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import type { Server } from "bun";
import { runBunScript } from "./helpers/runBun";
import type { MxActionResult } from "../src/sources/mx";

const FIXTURE_TOKEN = "fixture-token-abc123";

interface RecordedRequest {
  method: string;
  path: string;
  authorization: string | null;
}

let server: Server | undefined;
let fixtureDir: string | undefined;

afterEach(async () => {
  server?.stop(true);
  server = undefined;
  if (fixtureDir) {
    await rm(fixtureDir, { recursive: true, force: true });
    fixtureDir = undefined;
  }
});

async function writeFixtureGatewayEnv(token: string | null): Promise<string> {
  fixtureDir = await mkdtemp(join(tmpdir(), "daily-brief-gateway-env-"));
  const path = join(fixtureDir, "gateway.env");
  await writeFile(path, token === null ? "# no token here\n" : `MX_GATEWAY_TOKEN=${token}\n`);
  return path;
}

test("unauthenticated first, then retries once with the bearer token on 401, succeeds", async () => {
  const requests: RecordedRequest[] = [];
  server = Bun.serve({
    port: 0,
    fetch(req) {
      const url = new URL(req.url);
      requests.push({
        method: req.method,
        path: url.pathname,
        authorization: req.headers.get("authorization"),
      });
      const auth = req.headers.get("authorization");
      if (auth === `Bearer ${FIXTURE_TOKEN}`) {
        return new Response("{}", { status: 200 });
      }
      return new Response(JSON.stringify({ error: "unauthorized" }), { status: 401 });
    },
  });

  const gatewayEnvPath = await writeFixtureGatewayEnv(FIXTURE_TOKEN);
  const { stdout, stderr, exitCode } = await runBunScript("run-mx-action.ts", ["snooze", "item-1"], {
    MX_GATEWAY_URL: `http://127.0.0.1:${server.port}`,
    MX_GATEWAY_ENV_PATH: gatewayEnvPath,
  });
  expect(exitCode, `stderr:\n${stderr}`).toBe(0);
  const result = JSON.parse(stdout) as MxActionResult;

  expect(requests.length).toBe(2);
  expect(requests[0]?.authorization).toBeNull();
  expect(requests[0]?.path).toBe("/triage/item-1/snooze");
  expect(requests[1]?.authorization).toBe(`Bearer ${FIXTURE_TOKEN}`);

  expect(result.ok).toBe(true);
  expect(result.status).toBe(200);
});

test("setTriageStatus follows the same unauthenticated-first, retry-once contract", async () => {
  const requests: RecordedRequest[] = [];
  server = Bun.serve({
    port: 0,
    fetch(req) {
      requests.push({
        method: req.method,
        path: new URL(req.url).pathname,
        authorization: req.headers.get("authorization"),
      });
      if (req.headers.get("authorization") === `Bearer ${FIXTURE_TOKEN}`) {
        return new Response("{}", { status: 200 });
      }
      return new Response(JSON.stringify({ error: "unauthorized" }), { status: 401 });
    },
  });

  const gatewayEnvPath = await writeFixtureGatewayEnv(FIXTURE_TOKEN);
  const { stdout, exitCode } = await runBunScript("run-mx-action.ts", ["status", "item-2"], {
    MX_GATEWAY_URL: `http://127.0.0.1:${server.port}`,
    MX_GATEWAY_ENV_PATH: gatewayEnvPath,
  });
  expect(exitCode).toBe(0);
  const result = JSON.parse(stdout) as MxActionResult;

  expect(requests.length).toBe(2);
  expect(requests[0]?.authorization).toBeNull();
  expect(result.ok).toBe(true);
});

test("double failure (401 then still failing) returns a structured error, never throws", async () => {
  const requests: RecordedRequest[] = [];
  server = Bun.serve({
    port: 0,
    fetch(req) {
      requests.push({
        method: req.method,
        path: new URL(req.url).pathname,
        authorization: req.headers.get("authorization"),
      });
      // Always 401, even with the correct bearer token — simulates a token
      // that mx-gateway itself rejects (expired/invalid).
      return new Response(JSON.stringify({ error: "unauthorized" }), { status: 401 });
    },
  });

  const gatewayEnvPath = await writeFixtureGatewayEnv(FIXTURE_TOKEN);
  const { stdout, stderr, exitCode } = await runBunScript("run-mx-action.ts", ["snooze", "item-3"], {
    MX_GATEWAY_URL: `http://127.0.0.1:${server.port}`,
    MX_GATEWAY_ENV_PATH: gatewayEnvPath,
  });
  // The action client itself never throws/exits non-zero — a bad HTTP
  // outcome is a structured result, not a process failure.
  expect(exitCode, `stderr:\n${stderr}`).toBe(0);
  const result = JSON.parse(stdout) as MxActionResult;

  expect(requests.length).toBe(2);
  expect(result.ok).toBe(false);
  expect(result.status).toBe(401);
  expect(result.error).toBeTruthy();
});

test("no token available (gateway.env has no MX_GATEWAY_TOKEN key): 401 with no retry attempt over the wire, structured error", async () => {
  const requests: RecordedRequest[] = [];
  server = Bun.serve({
    port: 0,
    fetch(req) {
      requests.push({
        method: req.method,
        path: new URL(req.url).pathname,
        authorization: req.headers.get("authorization"),
      });
      return new Response(JSON.stringify({ error: "unauthorized" }), { status: 401 });
    },
  });

  const gatewayEnvPath = await writeFixtureGatewayEnv(null);
  const { stdout, exitCode } = await runBunScript("run-mx-action.ts", ["snooze", "item-4"], {
    MX_GATEWAY_URL: `http://127.0.0.1:${server.port}`,
    MX_GATEWAY_ENV_PATH: gatewayEnvPath,
  });
  expect(exitCode).toBe(0);
  const result = JSON.parse(stdout) as MxActionResult;

  // No token found -> the client never issues a second HTTP request at all.
  expect(requests.length).toBe(1);
  expect(result.ok).toBe(false);
  expect(result.status).toBe(401);
  expect(result.error).toContain("no MX_GATEWAY_TOKEN");
});
