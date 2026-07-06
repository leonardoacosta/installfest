# Visual + diagram assets — fleet icons & wire engine

> Load this file when building a WHS-346 fleet/infra diagram or when you need a real cloud-service icon (Azure/observability marks, theSVG slugs, the `bb-base.js` wire-routing gotcha). Skip when the work has no diagramming/icon angle — those tasks stay in SKILL.md.

## Visual + diagram assets

The `azure-topology-style` skill (load separately when diagramming) ships icon sprites and design tokens for neon-on-black Azure topology diagrams. Sync from skill assets to project public dirs:

```bash
cp ~/.claude/skills/azure-topology-style/assets/azure-icons.svg <project>/public/
cp ~/.claude/skills/azure-topology-style/assets/tokens.css     <project>/<ui>/topology/topology-tokens.css
```

Design language: orange=compute, purple=tools/admin, green=context/data, cyan=flow/network. No rounded connectors. No emojis ever.

### Fleet service icons — use REAL marks from theSVG, not generic glyphs

For any WHS-346 diagram, use the actual product/Azure SVGs (load the `thesvg` skill). Generic
phosphor/Lucide glyphs for a named Azure/observability service read as a placeholder — theSVG has
the real mark. **Resolve slugs from the registry, never guess** (`curl -sL https://cdn.jsdelivr.net/gh/glincker/thesvg@main/src/data/icons.json | jq '.[]|select(.title|test("X";"i"))'`). Azure marks are MIT; fetch `/{slug}/default.svg` (cloud icons have a `default` variant only — no mono/light).

Verified slugs for the resources this fleet diagrams (confirmed present 2026-06-21):

| Fleet resource | theSVG slug | local `docs/assets/` |
| --- | --- | --- |
| App Services | `azure-app-services` | `az-appservice.svg` |
| Function Apps | `azure-function-apps` | — |
| Application Insights | `azure-application-insights` | `az-appinsights.svg` |
| Log Analytics Workspaces | `azure-log-analytics-workspaces` | `az-loganalytics.svg` |
| Azure Monitor (plane) | `azure-monitor` (title "Monitor") | `az-monitor.svg` |
| Data Collection Rules | `azure-data-collection-rules` | `az-dcr.svg` |
| Azure Monitor Workspace (managed Prometheus) | **no distinct AMW icon** — use `prometheus` | `prometheus.svg` |
| Key Vaults | `azure-key-vaults` | — |
| Azure Service Bus | `azure-azure-service-bus` | — |
| API Management | `azure-api-management-services` | `az-apim.svg` |
| Container Apps env | `azure-container-apps-environments` | — |
| Cosmos DB | `azure-azure-cosmos-db` | — |
| MySQL Flexible Server | `azure-azure-database-mysql-server` | — |
| Grafana (OSS, self-hosted) | `grafana` (has `default`+`mono`) | `grafana.svg` |
| Prometheus | `prometheus` | `prometheus.svg` |
| Uptime Kuma | `uptime-kuma` | `uptime-kuma.svg` |
| OpenTelemetry | `opentelemetry` | `opentelemetry.svg` |

No theSVG mark for **Grafana Alloy / Tempo / Pyroscope** — fall back to phosphor (`ph-funnel` /
`ph-path` / `ph-fire`). For `ws` diagrams, download to `docs/assets/` (offline-safe, version-pinned,
matches the existing `az-*.svg`) and reference locally rather than hot-linking the CDN.

> **bb-base.js wire engine gotcha** (for `docs/diagrams/*.html` using `data-wires`): it only routes
> **orthogonal (square)** for edge combos `r→l`, `r→t`, `b→t` — EVERY other combo (e.g. `l→r`,
> `t→b`) falls back to a **diagonal** line. Orient every wire to one of those three: for a
> right-column node reading a left-column backend, declare the wire **backend→node** (`r→l`, reads
> as the data/response flowing to the query layer) instead of node→backend (`l→r` = diagonal).
