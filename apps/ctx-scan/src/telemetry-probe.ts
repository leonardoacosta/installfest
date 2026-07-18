/**
 * telemetry-probe.ts — the C4b sequence (ctx-scan-assembly task [2.5],
 * beads:if-fnwt): endpoint resolution, schema self-verification, and
 * provenance recording for every telemetry-derived number `ctx-scan`
 * consumes (hook-size ingestion in `assembly.ts`, calibration in
 * `calibrate.ts`).
 *
 * Endpoint resolution order (never deviate, never hardcode a resolved value):
 *   1. `CTX_SCAN_LOKI_URL` env var, if set
 *   2. `http://localhost:3100` (Loki's well-known default port)
 *   3. docker-inspect discovery of a running `loki`/`victoria-metrics`
 *      container (by name/image match), reached via its container-network IP
 *   4. Grafana datasource-proxy fallback via `GRAFANA_URL` + `GRAFANA_SA_TOKEN`
 *      (resolves the Loki datasource's `uid` through Grafana's own API — the
 *      uid is NEVER hardcoded)
 *
 * Every candidate is verified reachable (`GET /ready`) before being accepted,
 * and every telemetry-derived number this module hands back carries a
 * `{endpoint, service_version, window, query}` provenance record. Any
 * resolution or schema-assertion failure returns `{ok: false, reason}` —
 * this module NEVER throws and NEVER returns a fabricated zero/number when
 * the real data is unavailable.
 *
 * Verified live (2026-07-18, this machine): `docker ps` surfaces a running
 * `loki` (grafana/loki:3.4.2) container with no published host port; its
 * container-network IP answers `/ready` directly. Real sampled log lines
 * confirm two concrete wire shapes this module's schema specs are built
 * against:
 *   - Anthropic's native OTel `api_request` event lands under Loki label
 *     `service_name="claude-code"` as
 *     `{"body":"claude_code.api_request","attributes":{cache_read_tokens,
 *     cache_creation_tokens,"session.id",...}}`.
 *   - cc's own `hook_output_metrics` event (telemetry-hook-bytes) lands
 *     under `service_name="journal"` (systemd-journal ingestion of the nexus
 *     agent socket-server's pino logs), nested one level under `.event`:
 *     `{"event":{"event_type":"hook_output_metrics","hook_name":...,
 *     "stdout_bytes":...}}`.
 * No live `GRAFANA_SA_TOKEN` was available to exercise the proxy fallback
 * path end-to-end; it is implemented per the C4b sequence and degrades
 * cleanly (documented reason) when the env pair is absent.
 */

/** One resolved candidate Loki-compatible endpoint. */
export interface ResolvedEndpoint {
  mode: "direct" | "proxy";
  /** For `direct`: the Loki base URL. For `proxy`: the Grafana base URL. */
  base: string;
  /** Grafana datasource `uid` — only present in `proxy` mode. */
  proxyDatasourceUid?: string;
  /** Bearer token used for `proxy`-mode requests. */
  grafanaToken?: string;
  /** Which step in the resolution order produced this candidate. */
  source: "env" | "localhost" | "docker" | "grafana-proxy";
}

export type EndpointResolution =
  | { ok: true; endpoint: ResolvedEndpoint }
  | { ok: false; reason: string };

/** Provenance attached to every telemetry-derived number. */
export interface Provenance {
  endpoint: string;
  service_version: string | null;
  window: string;
  query: string;
}

export type ProbeFailure = { ok: false; reason: string };

/** Loki's own well-known default port — this is the literal candidate the
 * proposal's resolution order names as step 2, never a resolved-value stand-in. */
const LOKI_DEFAULT_PORT = 3100;
/** VictoriaMetrics' well-known default HTTP API port. */
const VM_DEFAULT_PORT = 8428;

const READY_TIMEOUT_MS = 2000;
const QUERY_TIMEOUT_MS = 5000;

function errMsg(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

// ─────────────────────────────────────────────────────────────────────────
// Docker-inspect discovery
// ─────────────────────────────────────────────────────────────────────────

interface DockerContainerInfo {
  id: string;
  image: string;
  name: string;
}

/** `docker ps` listing, never throwing (missing/broken docker -> empty list). */
function dockerPs(): DockerContainerInfo[] {
  try {
    const proc = Bun.spawnSync(["docker", "ps", "--format", "{{.ID}}|{{.Image}}|{{.Names}}"]);
    if (proc.exitCode !== 0) return [];
    const out = proc.stdout.toString("utf8");
    return out
      .split("\n")
      .filter((line) => line.trim().length > 0)
      .map((line) => {
        const [id, image, name] = line.split("|");
        return { id: id ?? "", image: image ?? "", name: name ?? "" };
      })
      .filter((c) => c.id.length > 0);
  } catch {
    return [];
  }
}

/** First container-network IP for `containerId`, or null. Never throws. */
function dockerInspectIp(containerId: string): string | null {
  try {
    const proc = Bun.spawnSync(["docker", "inspect", containerId]);
    if (proc.exitCode !== 0) return null;
    const parsed = JSON.parse(proc.stdout.toString("utf8")) as unknown;
    if (!Array.isArray(parsed) || parsed.length === 0) return null;
    const info = parsed[0] as {
      NetworkSettings?: { Networks?: Record<string, { IPAddress?: string }> };
    };
    const networks = info.NetworkSettings?.Networks ?? {};
    for (const key of Object.keys(networks)) {
      const ip = networks[key]?.IPAddress;
      if (ip) return ip;
    }
    return null;
  } catch {
    return null;
  }
}

// ─────────────────────────────────────────────────────────────────────────
// HTTP plumbing (direct vs Grafana datasource-proxy)
// ─────────────────────────────────────────────────────────────────────────

function buildUrl(resolved: ResolvedEndpoint, path: string, params?: Record<string, string>): string {
  const base =
    resolved.mode === "proxy"
      ? `${resolved.base}/api/datasources/proxy/uid/${resolved.proxyDatasourceUid}${path}`
      : `${resolved.base}${path}`;
  if (!params) return base;
  return `${base}?${new URLSearchParams(params).toString()}`;
}

async function lokiFetch(
  resolved: ResolvedEndpoint,
  path: string,
  params?: Record<string, string>,
  timeoutMs = QUERY_TIMEOUT_MS,
): Promise<Response> {
  const url = buildUrl(resolved, path, params);
  const headers: Record<string, string> = {};
  if (resolved.mode === "proxy" && resolved.grafanaToken) {
    headers.Authorization = `Bearer ${resolved.grafanaToken}`;
  }
  return fetch(url, { headers, signal: AbortSignal.timeout(timeoutMs) });
}

async function checkReady(candidate: ResolvedEndpoint): Promise<boolean> {
  try {
    const res = await lokiFetch(candidate, "/ready", undefined, READY_TIMEOUT_MS);
    return res.ok;
  } catch {
    return false;
  }
}

/** Resolve the Loki datasource `uid` through Grafana's own API — never hardcoded. */
async function findGrafanaLokiDatasourceUid(grafanaUrl: string, token: string): Promise<string | null> {
  try {
    const res = await fetch(`${grafanaUrl.replace(/\/$/, "")}/api/datasources`, {
      headers: { Authorization: `Bearer ${token}` },
      signal: AbortSignal.timeout(QUERY_TIMEOUT_MS),
    });
    if (!res.ok) return null;
    const list = (await res.json()) as Array<{ uid?: string; type?: string }>;
    if (!Array.isArray(list)) return null;
    const loki = list.find((d) => d.type === "loki");
    return loki?.uid ?? null;
  } catch {
    return null;
  }
}

// ─────────────────────────────────────────────────────────────────────────
// Endpoint resolution (C4b step 1)
// ─────────────────────────────────────────────────────────────────────────

/**
 * Resolve a reachable Loki-compatible endpoint per the fixed C4b order.
 * Every candidate is verified reachable before being accepted; a candidate
 * that resolves to an address but doesn't answer `/ready` is skipped, not
 * silently trusted.
 */
export async function resolveLokiEndpoint(
  env: Record<string, string | undefined> = process.env,
): Promise<EndpointResolution> {
  const attempts: string[] = [];

  const envUrl = env.CTX_SCAN_LOKI_URL;
  if (envUrl) {
    const candidate: ResolvedEndpoint = { mode: "direct", base: envUrl.replace(/\/$/, ""), source: "env" };
    if (await checkReady(candidate)) return { ok: true, endpoint: candidate };
    attempts.push(`CTX_SCAN_LOKI_URL(${envUrl}): unreachable`);
  }

  {
    const candidate: ResolvedEndpoint = {
      mode: "direct",
      base: `http://localhost:${LOKI_DEFAULT_PORT}`,
      source: "localhost",
    };
    if (await checkReady(candidate)) return { ok: true, endpoint: candidate };
    attempts.push(`localhost:${LOKI_DEFAULT_PORT}: unreachable`);
  }

  const containers = dockerPs();
  const lokiContainer = containers.find((c) => /loki/i.test(c.name) || /loki/i.test(c.image));
  if (lokiContainer) {
    const ip = dockerInspectIp(lokiContainer.id);
    if (ip) {
      const candidate: ResolvedEndpoint = { mode: "direct", base: `http://${ip}:${LOKI_DEFAULT_PORT}`, source: "docker" };
      if (await checkReady(candidate)) return { ok: true, endpoint: candidate };
      attempts.push(`docker loki(${lokiContainer.name}@${ip}): unreachable`);
    } else {
      attempts.push(`docker loki(${lokiContainer.name}): no container-network IP`);
    }
  }
  const vmContainer = containers.find((c) => /victoria-?metrics/i.test(c.name) || /victoria-?metrics/i.test(c.image));
  if (vmContainer) {
    const ip = dockerInspectIp(vmContainer.id);
    if (ip) {
      const candidate: ResolvedEndpoint = { mode: "direct", base: `http://${ip}:${VM_DEFAULT_PORT}`, source: "docker" };
      if (await checkReady(candidate)) return { ok: true, endpoint: candidate };
      attempts.push(`docker victoria-metrics(${vmContainer.name}@${ip}): unreachable`);
    } else {
      attempts.push(`docker victoria-metrics(${vmContainer.name}): no container-network IP`);
    }
  }
  if (!lokiContainer && !vmContainer) attempts.push("docker: no loki/victoria-metrics container found");

  const grafanaUrl = env.GRAFANA_URL;
  const grafanaToken = env.GRAFANA_SA_TOKEN;
  if (grafanaUrl && grafanaToken) {
    const uid = await findGrafanaLokiDatasourceUid(grafanaUrl, grafanaToken);
    if (uid) {
      const candidate: ResolvedEndpoint = {
        mode: "proxy",
        base: grafanaUrl.replace(/\/$/, ""),
        proxyDatasourceUid: uid,
        grafanaToken,
        source: "grafana-proxy",
      };
      if (await checkReady(candidate)) return { ok: true, endpoint: candidate };
      attempts.push(`grafana-proxy(datasource uid ${uid}): unreachable`);
    } else {
      attempts.push("grafana-proxy: no Loki datasource found via Grafana API");
    }
  } else {
    attempts.push("grafana-proxy: GRAFANA_URL/GRAFANA_SA_TOKEN not set");
  }

  return { ok: false, reason: `endpoint resolution failed — ${attempts.join("; ")}` };
}

// ─────────────────────────────────────────────────────────────────────────
// Event schema specs + sampling (C4b step 2: schema self-verification)
// ─────────────────────────────────────────────────────────────────────────

/** One sampled + JSON-parsed log line, with its event-specific attributes extracted. */
export interface QueriedEvent {
  /** Flat attribute record for this event (already unwrapped from OTel/pino nesting). */
  attrs: Record<string, unknown>;
  /** Loki entry timestamp, epoch milliseconds. */
  timestampMs: number;
  /** The service.version resource attribute, when the event shape carries one. */
  serviceVersion: string | null;
}

interface EventSchemaSpec {
  /** LogQL selector used to sample this event type. */
  logqlQuery: string;
  /** Unwrap a parsed JSON log line to this event's flat attribute record, or null if it doesn't match. */
  extractAttributes: (parsed: unknown) => Record<string, unknown> | null;
  /** Attribute keys that MUST be present for the schema to be considered intact. */
  requiredAttrs: string[];
}

/** Known telemetry event types this module can sample + verify. */
export const KNOWN_EVENT_TYPES = ["api_request", "hook_output_metrics"] as const;
export type KnownEventType = (typeof KNOWN_EVENT_TYPES)[number];

function otelServiceVersion(parsed: unknown): string | null {
  const p = parsed as { resources?: Record<string, unknown> } | null;
  const v = p?.resources?.["service.version"];
  return typeof v === "string" ? v : null;
}

const EVENT_SCHEMAS: Record<KnownEventType, EventSchemaSpec> = {
  api_request: {
    logqlQuery: '{service_name="claude-code"} |= "api_request"',
    extractAttributes: (parsed) => {
      const p = parsed as { body?: string; attributes?: Record<string, unknown> } | null;
      if (p && p.body === "claude_code.api_request" && p.attributes) return p.attributes;
      return null;
    },
    requiredAttrs: ["session.id", "cache_read_tokens", "cache_creation_tokens"],
  },
  hook_output_metrics: {
    logqlQuery: '{service_name="journal"} |= "hook_output_metrics"',
    extractAttributes: (parsed) => {
      const p = parsed as { event?: Record<string, unknown>; event_type?: string } | null;
      // cc's own telemetry ships this event journal-wrapped by the nexus
      // socket-server's pino logger, one level under `.event` — but sample
      // generically (fall back to the top level) in case a future emission
      // path ships it unwrapped.
      const inner = (p?.event ?? p) as Record<string, unknown> | undefined;
      if (inner && inner.event_type === "hook_output_metrics") return inner;
      return null;
    },
    requiredAttrs: ["hook_name", "stdout_bytes", "duration_ms"],
  },
};

/** Low-level sampler shared by schema verification and event querying. */
async function sampleEvents(
  endpoint: ResolvedEndpoint,
  eventType: KnownEventType,
  windowMs: number,
  limit: number,
): Promise<{ ok: true; events: QueriedEvent[]; provenance: Provenance } | ProbeFailure> {
  const spec = EVENT_SCHEMAS[eventType];
  const end = Date.now();
  const start = end - windowMs;

  let labels: string[];
  try {
    const res = await lokiFetch(endpoint, "/loki/api/v1/labels");
    if (!res.ok) return { ok: false, reason: `labels query failed: HTTP ${res.status}` };
    const body = (await res.json()) as { status?: string; data?: string[] };
    if (body.status !== "success" || !Array.isArray(body.data)) {
      return { ok: false, reason: "labels query returned an unexpected shape" };
    }
    labels = body.data;
  } catch (err) {
    return { ok: false, reason: `labels query threw: ${errMsg(err)}` };
  }
  if (labels.length === 0) return { ok: false, reason: "endpoint has no ingested streams (empty label set)" };

  let rawEntries: [string, string][];
  try {
    const res = await lokiFetch(endpoint, "/loki/api/v1/query_range", {
      query: spec.logqlQuery,
      start: `${start * 1_000_000}`,
      end: `${end * 1_000_000}`,
      limit: String(limit),
      direction: "backward",
    });
    if (!res.ok) return { ok: false, reason: `sample query failed: HTTP ${res.status}` };
    const body = (await res.json()) as {
      status?: string;
      data?: { result?: Array<{ values: [string, string][] }> };
    };
    if (body.status !== "success" || !body.data?.result) {
      return { ok: false, reason: "sample query returned an unexpected shape" };
    }
    rawEntries = body.data.result.flatMap((s) => s.values);
  } catch (err) {
    return { ok: false, reason: `sample query threw: ${errMsg(err)}` };
  }
  if (rawEntries.length === 0) {
    return { ok: false, reason: `no "${eventType}" events found in the sampled window` };
  }

  const events: QueriedEvent[] = [];
  for (const [tsNanoStr, line] of rawEntries) {
    let parsed: unknown;
    try {
      parsed = JSON.parse(line);
    } catch {
      continue;
    }
    const attrs = spec.extractAttributes(parsed);
    if (!attrs) continue;
    // Loki timestamps are nanosecond-precision strings; ms precision is
    // ample for "which event came first" ordering, so truncate rather than
    // lose precision to a float that can't hold the full nanosecond value.
    const timestampMs = Number(BigInt(tsNanoStr) / 1_000_000n);
    events.push({ attrs, timestampMs, serviceVersion: otelServiceVersion(parsed) });
  }

  return {
    ok: true,
    events,
    provenance: {
      endpoint: endpoint.base,
      service_version: events.find((e) => e.serviceVersion)?.serviceVersion ?? null,
      window: `${new Date(start).toISOString()}..${new Date(end).toISOString()}`,
      query: spec.logqlQuery,
    },
  };
}

/**
 * Query sampled events of `eventType` off `endpoint`, unwrapped to flat
 * attribute records. Returns `{ok:false}` (never throws, never fabricates
 * data) on any resolution/query/parse failure.
 */
export async function queryEvents(
  endpoint: ResolvedEndpoint,
  eventType: KnownEventType,
  opts: { windowMs?: number; limit?: number } = {},
): Promise<{ ok: true; events: QueriedEvent[]; provenance: Provenance } | ProbeFailure> {
  return sampleEvents(endpoint, eventType, opts.windowMs ?? 24 * 60 * 60 * 1000, opts.limit ?? 100);
}

/**
 * Schema self-verification (C4b step 2): confirm `endpoint` actually serves
 * `eventType` events carrying every attribute this module's downstream
 * consumers (hook-size ingestion, calibration) rely on. A schema-drifted or
 * empty result degrades to `{ok:false, reason}` — never a silent wrong number.
 */
export async function verifyEventSchema(
  endpoint: ResolvedEndpoint,
  eventType: KnownEventType,
  opts: { windowMs?: number; sampleLimit?: number } = {},
): Promise<{ ok: true; sampled: number; provenance: Provenance } | ProbeFailure> {
  const spec = EVENT_SCHEMAS[eventType];
  const sampled = await sampleEvents(endpoint, eventType, opts.windowMs ?? 24 * 60 * 60 * 1000, opts.sampleLimit ?? 20);
  if (!sampled.ok) return sampled;

  const withRequiredAttrs = sampled.events.filter((e) => spec.requiredAttrs.every((k) => k in e.attrs));
  if (withRequiredAttrs.length === 0) {
    return {
      ok: false,
      reason: `sampled ${sampled.events.length} "${eventType}" events but none carried all required attributes (${spec.requiredAttrs.join(", ")}) — schema drift`,
    };
  }

  return { ok: true, sampled: withRequiredAttrs.length, provenance: sampled.provenance };
}

/**
 * Top-level probe: resolve an endpoint, then verify `eventType`'s schema
 * against it. This is the single entry point downstream modules ([2.6]'s
 * hook-size ingestion, [2.7]'s calibration) should call before trusting any
 * telemetry-derived number.
 */
export async function probeTelemetry(
  eventType: KnownEventType,
  opts: { windowMs?: number; sampleLimit?: number; env?: Record<string, string | undefined> } = {},
): Promise<
  | { status: "available"; endpoint: ResolvedEndpoint; provenance: Provenance; sampled: number }
  | { status: "unavailable"; reason: string }
> {
  const resolution = await resolveLokiEndpoint(opts.env);
  if (!resolution.ok) return { status: "unavailable", reason: resolution.reason };

  const verified = await verifyEventSchema(resolution.endpoint, eventType, opts);
  if (!verified.ok) return { status: "unavailable", reason: verified.reason };

  return {
    status: "available",
    endpoint: resolution.endpoint,
    provenance: verified.provenance,
    sampled: verified.sampled,
  };
}
