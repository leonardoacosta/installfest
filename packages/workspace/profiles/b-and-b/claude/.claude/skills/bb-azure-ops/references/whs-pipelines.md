# WHS Pipelines, APIM Onboarding & Wholesale Telemetry

The Wholesale (WHS-346) operational quick-reference: the ADO pipeline ID registry,
how to queue a deploy, how an API is onboarded into the wholesale APIM (and why no
API should be a wildcard), the shared App Insights trace pattern, and the Postman
smoke harness. This is the "stop re-discovering it" layer — IDs and commands you'd
otherwise rebuild from scratch each session.

All pipelines live in the ADO project **`Wholesale Architecture`** under org
**`brownandbrowninc`**. Identity for every call here is BBAdmin (`--as-admin`) — the
modern WHS-346 subs. See the parent skill for the identity wrapper + SOCKS tunnel.

## Contents
- [Pipeline ID registry](#pipeline-id-registry) — code → satellite × env, verified 2026-06-30
- [Queueing a pipeline](#queueing-a-pipeline) — `ado queue <code>`, corrected REST fallback, watch, the 449 rule
- [APIM API onboarding & the no-wildcard rule](#apim-api-onboarding--the-no-wildcard-rule)
- [Wholesale App Insights trace](#wholesale-app-insights-trace) — AppId map + the operation_Id join
- [Postman per-env smoke](#postman-per-env-smoke)

---

## Pipeline ID registry

Verified live 2026-06-08; prod satellites + emailscheduler test/stage/prod registered
2026-06-17 (defs 566-574, 576-577 — `defaultBranch: refs/heads/dev` until prod cutover
flips them to main alongside the D6 ref-pin); + 2026-06-30: registered qr 638-640, dp
test/stage 641-642, ptv prod 643 (skip-first-run, no deploy); ptv dev/test/stage 593-595.
Blank = no pipeline exists for that env. The live source of truth for code→id is now
`~/dev/ws/scripts/lib/ado-pipelines.json` — this markdown table is a convenience cache;
`ado registry` (alias `ado codes`) prints it. If a row looks wrong, re-run the discovery
one-liner in the next section.

The leading `code` is the 2-3-char `ado queue <code>` project code (see [Queueing a
pipeline](#queueing-a-pipeline)).

| code | Satellite / stack | dev | test | stage | prod |
| :-- | --- | :-: | :-: | :-: | :-: |
| ws | wholesale (foundation) | 449 | 451 | 452 | 418 |
| fb | fireball | 450 | 478 | 486 | 566 |
| cc | costcenter | 447 | 448 | 469 | 570 |
| sc | salescrm | 472 | 482 | 488 | 568 |
| se | submissionengine | 471 | 481 | 487 | 567 |
| tb | thebridge | 475 | 485 | 491 | 569 |
| bo | b3owa | 473 | 483 | 489 | 571 |
| ba | b3admin | 474 | 484 | 490 | 572 |
| ip | inscipher-proxy | 501 | 503 | 504 | 505 |
| es | emailscheduler | 517 | 554 | 574 | 573 |
| ptv | pipstovelocitysync | 593 | 594 | 595 | 643 |
| dp | dataplatform (env-param, DISABLED) | 613 | 641 | 642 |  |
| mo | monitoring-hub (`prod` BOOL param, NOT env) | 494 | 494 | 494 |  |
| qr | quoterepo (INERT — see note) | 638 | 639 | 640 |  |
| — | apim-custom-domain (env-param) | 576 | 576 |  |  |

> **`dp` (dataplatform)** is env-param + still **DISABLED** (dev=613 ADO `queueStatus=disabled`)
> — held pending mmorales/Hull sign-off + the 411 SC grant. There is no prod def.
> **`mo` (monitoring-hub)** is a single def **494** at the folder root; it uses a `prod`
> BOOL param (NOT an `env` param), and the `ado` CLI aliases test/stage to the one dev hub.
> **`qr` (quoterepo)** defs are registered but **INERT**: `quoterepo.yml` pins the app repo
> `Quote Repo/QuoteRepo.Subscriber` which does NOT exist (real repo is `Quote Repo/QuoteRepo`),
> and it needs a cross-project read grant before it can run.

> **Code-set disambiguation:** these `ado queue` codes are a DISTINCT set from the fleet-name
> trigger vocabulary in `bb-azure-ops` SKILL.md (which uses `ba`=B3 broadly and `bo`=Office
> Index PIPS). The queue codes SPLIT B3: **`ba`=b3admin, `bo`=b3owa**. When queueing, use these
> codes — not the SKILL.md fleet names.

> `apim-custom-domain` is now ONE env-parameterised def = **576** (`\fleet-sync`,
> `build/apim-custom-domain.yml`, env selected by the `environment` param). The per-env
> defs **527/540 + their `build/{dev,test}/apim-custom-domain.yml` sources were DELETED
> 2026-06-17** (along with a stray root-folder dup def 575). `parity-validator` test
> variant = **577** (`\test`); 546 (`\wholesale`) still runs the dev variant. The 8 prod
> satellites (566-573) + emailscheduler stage (574)
> are registered but `trigger: none` + `pr: none` (prod DEPLOY is HELD); they render
> "never built" on the status board until first run.

**Ops / standalone** (no env folder — run on demand, not per-env):

| Pipeline | id | purpose |
| --- | :-: | --- |
| canonical-seed | 541 | seeds the canonical KV / App Config (one of the two seed paths — see memory `541 vs 449 seed-path divergence`) |
| parity-validator | 546 | legacy-vs-canonical contract parity suite |
| postman-publish | 544 | publishes the postman env + collection artifacts |
| b3owa-legacy-kv-rbac-grant | 542 | one-off KV RBAC grant for b3owa |
| ops-decommission-fb-nonfb-orphans-test | 545 | orphan-resource decommission (test) |

**`\wholesale` folder** (cross-cutting):

| Pipeline | id | note |
| --- | :-: | --- |
| docs-wiki-sync | 514 | **manual-queue only** despite docs claiming auto-sync (see memory `docs-wiki-sync`) |
| fleet-sync | 519 | fleet config sync |
| migration-status-check | 515 | migration-console status refresh |
| monitoring | 494 | monitoring stack |

> prod pipelines now exist for the FULL fleet (registered 2026-06-17): wholesale 418,
> inscipher-proxy 505, fireball 566, submissionengine 567, salescrm 568, thebridge 569,
> costcenter 570, b3owa 571, b3admin 572, emailscheduler 573. All are `trigger: none`
> (registered, never-run) — prod stand-up is still gated behind the canonical cutover, so
> the definitions exist for badge coverage + readiness but no prod deploy has run.

---

## Queueing a pipeline

Satellite pipelines are `trigger: none` — **manual queue is the normal, expected path**
(not a workaround). The consolidated YAML lives in the **ws repo**
(`.azuredevops/build/satellites/<sat>.yml` — the old per-env `build/<env>/<sat>.yml`
folders are EMPTY post-consolidation) and pins the *app* repo as a resource at a specific
ref. The consolidated yamls declare `env` as a **REQUIRED** template parameter with **NO
default** — a queue that omits `templateParameters.env` fails `A value for the 'env'
parameter must be provided` (verified via previewRun on def 478, 2026-06-30). The ADO
build-def folder (`\dev \test \stage \prod`) is cosmetic — it does NOT bake env.

### Primary path: `ado queue <code> --branch <env>`

`~/dev/ws/scripts/ado queue` resolves `<code>` + `<env>` → def id via
`scripts/lib/ado-pipelines.json` (NEVER hardcode ids), then queues via the REST runs API,
injecting `templateParameters.env=<env>` for env-param projects. It is the correct path —
it can't drop the required `env` param the way a hand-rolled `az pipelines run` would
(`az pipelines run --parameters` CANNOT set template parameters).

```bash
~/dev/ws/scripts/ado registry                          # code|satellite|dev|test|stage|prod table
~/dev/ws/scripts/ado queue fb --branch dev             # queue fireball dev (REST + auto env-param)
~/dev/ws/scripts/ado queue fb --branch dev --watch     # + stream the timeline as pino NDJSON
~/dev/ws/scripts/ado queue dp --branch test --dry-run  # resolve only — prints {code,satellite,env,
                                                       #   pipelineId,ref,yaml,templateParameters}, no queue
~/dev/ws/scripts/ado watch <runId> --pipe fb --env dev # monitor a running build (pino events)
```

Flags: `--branch <dev|test|stage>` selects the **env** (prod refused — gated); `--ref <gitref>`
overrides the ws-repo git ref the def builds from (default `refs/heads/dev`) — a DIFFERENT
axis from `--branch`/env; trailing `k=v` pairs append further template parameters. A raw
numeric pipeline-id still passes through (legacy). **`mo`** (monitoring, env-param:false)
takes no env param. See § Watching a run for the `watch`/`--watch` NDJSON shape.

### Fallback: manual REST queue (corrected — MUST include templateParameters.env)

When not using `ado queue` (e.g. a one-off curl). The OLD snippet here omitted
`templateParameters`, which is WRONG for every env-param def. `env` is a SCALAR string, so
it sticks (unlike object-typed template params, which the queue API silently drops):

```bash
# 1. mint an ADO token (BBAdmin federated session through SOCKS)
TOK=$(~/dev/ws/scripts/ado token)

# 2. queue the run. templateParameters.env is REQUIRED (consolidated yamls have no default).
#    self.refName = the WS-REPO branch carrying the pipeline YAML (almost always
#    refs/heads/dev). The app repo's ref is pinned inside the yml.
curl -s --proxy socks5h://127.0.0.1:1080 -H "Authorization: Bearer $TOK" \
  -H "Content-Type: application/json" -X POST \
  "https://dev.azure.com/brownandbrowninc/Wholesale%20Architecture/_apis/pipelines/<ID>/runs?api-version=7.0" \
  -d '{"templateParameters":{"env":"<dev|test|stage>"},"resources":{"repositories":{"self":{"refName":"refs/heads/dev"}}}}'
# -> returns {id: <runId>, state: inProgress, _links.web.href: <build URL>}
# (mo/monitoring def 494 takes a `prod` BOOL param instead of env — omit env there.)
```

**Validate a def/params before a real queue — `previewRun` (NON-MUTATING):** same URL with
`?api-version=7.0-preview.1`, body `{"previewRun":true,"templateParameters":{"env":"test"},
"resources":{...}}` → returns `finalYaml` on success, or the missing-param error. Use it to
confirm the env param + ref before spending a real queue.

### Registering a new pipeline def (how the 6 new defs were registered)

```bash
~/dev/ws/scripts/ado raw pipelines create \
  --name <satellite> --organization https://dev.azure.com/brownandbrowninc \
  --project "Wholesale Architecture" --repository "Wholesale Architecture" --repository-type tfsgit \
  --branch dev --yml-path .azuredevops/build/satellites/<sat>.yml \
  --folder-path "\<dev|test|stage|prod>" --skip-first-run
```

**ALWAYS pass `--skip-first-run`** — without it `az pipelines create` AUTO-QUEUES a deploy on
creation. The `--folder-path` encodes env cosmetically only; env is still passed at queue time.

**Find a pipeline id** (when the registry above is stale or for a new pipeline):

```bash
curl -s --proxy socks5h://127.0.0.1:1080 -H "Authorization: Bearer $TOK" \
  "https://dev.azure.com/brownandbrowninc/Wholesale%20Architecture/_apis/pipelines?api-version=7.0&\$top=300" \
  | python3 -c "import json,sys;[print(p['id'],p.get('folder'),p['name']) for p in json.load(sys.stdin)['value']]"
```

**Which app-repo ref does a pipeline build?** The pin is in the yml, not the queue call:

```bash
grep -nE "repository:|name:|ref:" ~/dev/ws/.azuredevops/build/satellites/b3admin.yml | head
#   ref: refs/heads/feat/health-check-endpoints   <- builds THIS app branch
```

To deploy a different app branch, change that `ref:` in the yml (commit to ws `dev`),
or override it in the queue body under `resources.repositories.<repoAlias>.refName`.
Queuing alone does **not** change which app branch is built — only the pinned ref does.

### Adding an org agent pool to a project + authorizing it — pure REST, no UI (Confirmed 2026-07-02)

A pipeline in project B referencing a Managed DevOps Pool registered only in project A fails at
QUEUE time with `Could not find a pool with name <pool>. The pool does not exist or has not been
authorized for use` — the run reports `completed/failed` INSTANTLY with the error under
`validationResults` (not the timeline; a queue-time validation kill has no timeline). Both the
project-add AND the pipeline authorization are one REST call — the documented "authorize in the
ADO UI" click is NOT required:

```bash
# Creates a project-scoped agent QUEUE bound to the org pool (id from project A's queue, e.g.
# MDOP-WHS-537-EastUS2-DEV = org pool 73) AND authorizes it for all the project's pipelines.
curl -s --proxy socks5h://127.0.0.1:1080 -H "Authorization: Bearer $TOK" \
  -H "Content-Type: application/json" -X POST \
  "https://dev.azure.com/brownandbrowninc/<project>/_apis/distributedtask/queues?api-version=7.1-preview.1&authorizePipelines=true" \
  -d '{"name":"MDOP-WHS-537-EastUS2-DEV","projectId":null,"pool":{"id":73}}'
# -> {"id":<new queue id>, "pool":{"id":73}, ...} ; requeue the pipeline immediately after.
```

`authorizePipelines=true` covers the QUEUE half. **Second half (same incident): a run can still
park at `Checkpoint.Authorization` = `inProgress`** (timeline shows ONLY Checkpoint records; no
pool job request is ever created) when ANY other resource the run touches — typically a service
connection — is authorized for a *different* def in the project but not this one (peer def 586
had both SCs, def 616 did not). The fix is the same pattern, per resource:

```bash
# Inspect, then grant the def on each endpoint (repeat per SC the yml references):
curl -s --proxy socks5h://127.0.0.1:1080 -H "Authorization: Bearer $TOK" \
  "https://dev.azure.com/brownandbrowninc/<project>/_apis/pipelines/pipelinePermissions/endpoint/<endpointId>?api-version=7.1-preview.1"
curl -s --proxy socks5h://127.0.0.1:1080 -H "Authorization: Bearer $TOK" \
  -H "Content-Type: application/json" -X PATCH \
  "https://dev.azure.com/brownandbrowninc/<project>/_apis/pipelines/pipelinePermissions/endpoint/<endpointId>?api-version=7.1-preview.1" \
  -d '{"pipelines":[{"id":<defId>,"authorized":true}]}'
# Endpoint ids: GET <project>/_apis/serviceendpoint/endpoints?api-version=7.1-preview.4
# The STUCK run releases in-place (~45s) once the last grant lands — no requeue needed.
```

Live proof: bsi.Aggregates def 616 on pool 73 — queue POST fixed the pool-not-found validation
kill; the requeued run then sat 40+ min at `Checkpoint.Authorization` until the
`SC-WHS-346-Wholesale-DEV` endpoint grant landed, after which the checkpoint completed
in-place. Resource-authz triage order: `validationResults` (instant fail) → timeline
Checkpoint records (silent park) → pool `jobrequests` (absent = never reached the pool).

### Watching a run (pino NDJSON)

`~/dev/ws/scripts/ado watch <runId> [--pipe <code>] [--env <env>] [--poll <sec=8>] [--pretty]`
polls the build timeline over `socks5h://127.0.0.1:1080` (token via the script's own
`cmd_token`) and streams it through `scripts/lib/ado-monitor.mjs` (real pino + pino-pretty;
auto-runs `npm install --prefix ~/dev/ws/scripts` once if pino is missing). The NDJSON emits:
stage/job transitions; `result=failed` → level 50 (error); `succeededWithIssues`/`canceled`
→ level 40 (warn); each timeline `issues[]` entry by type; a final summary; and the process
exits 1 on a failed run. Use `--pretty` for humans; the raw NDJSON is what a Claude Code
`Monitor` filters on (`"level":50` / `"level":40`). `ado queue ... --watch` chains straight
into this on the run it just queued.

### The 449 rule (and why satellites are different)

- **Never hand-queue `449` (wholesale dev) on top of an in-flight run.** A second
  deployment to the same ARM deployment name returns `DeploymentActive` and locks for
  ~5 min. Confirm no run is in flight first. (The wholesale auto-trigger story is
  contested between `ws/CLAUDE.md` § Pipeline Auto-Trigger and project memory — treat
  the live `runs` list as truth, not either doc.)
- **Satellites (`trigger: none`) are safe to hand-queue** — that's the only way they
  run. A ws `dev` push does not auto-trigger them.
- Foundation deploys are monolithic on dev (~40 min, satellites inline) but split on
  test/stage (~12-15 min) — see memory `Pipeline deploy-speed audit`.

### DeploymentActive isn't just the 449 self-collision — it's a shared-NAME collision

The `DeploymentActive` lock fires whenever two ARM deployments share the same **deployment name** in the same scope within ~5 min, not only on re-queuing 449. Two traps beyond the 449 rule (confirmed across 13 sessions):

- **Pipeline-green is not ARM-settled.** Build 51756 (success) and 51757 (failed, 2s apart) both targeted the DEV RG; 51757 hit the lock left by 51756. Before gating the next module/stage as "ready," poll the ARM state, not the pipeline status:
  ```bash
  az --as-admin deployment group show -g <rg> -n main --query provisioningState -o tsv  # want: Succeeded
  ```
- **Shared deployment names collide across satellites.** All 7 satellites used `name: 'deploy-database'` for their cross-scope module into the shared wholesale RG — when stages run in parallel, ARM locks the name and the second fails (often surfacing as a generic `exit code 1`). Use a deterministic, per-satellite deployment name (`name: '<sat>-deploy-database'`).

A failed/in-progress deployment holds the lock for ~5 min after it finishes; there is no force-release — wait it out.

### Cross-pipeline ordering has no YAML guarantee

A satellite pipeline's `resources.pipelines` completion trigger + `dependsOn: []` does **NOT** enforce run order against the wholesale pipeline. A satellite CodeDeploy can fire *before* wholesale's `SatelliteInfra` stage has provisioned that satellite's app shell, so the deploy targets a resource that doesn't exist yet. To guarantee ordering: (a) manually run the wholesale `SatelliteInfra` stage first and confirm `provisioningState: Succeeded`, or (b) decompose into explicit sequential stages within one pipeline. **Completion triggers also arm from the *default* branch (`main`)**, not `dev` — a trigger defined only in `dev` YAML never fires until merged to `main` (ws/CLAUDE.md § Pipeline Trigger Behavior).

---

## Stage/test build failures: diff against dev/test FIRST

Stage (and test) pipelines deploy from `main` and carry the same structure as dev/test, but they **lag 1-2 weeks** on fixes — the dev/test fix lands first and is propagated upward later. So when a stage or test build fails, the fix is almost always **"propagate the dev/test pattern,"** not a novel bug.

**Triage move: diff the failing stage YAML against the passing dev/test YAML before anything else.** Recurring instances (all were a never-propagated dev/test fix):

- canonical-seed service-connection names (stage VG used the wrong SC name)
- STG VNet-integration NSG missing the Key Vault outbound rule dev/test have
- `UseDotNet` version pin (salescrm stage stuck on 9.x while dev/test moved to 10.x)
- build-UI step running on the VNet pool with no npm feed (b3admin)
- stale code/secrets in the stage env that dev/test already moved past

Exception: MDR-dependent satellites (cc, tb) have **no real stage env** (no-op stubs) — a "stage failure" there is usually the stub, not a propagation gap (memory `MDR-dependent sats have no stage env`).

---

## APIM API onboarding & the no-wildcard rule

Canonical wholesale APIM lives in `.azuredevops/bicep/wholesale/routing/apim/`
(`index.bicep` wires every API). There are three legitimate ways an API gets there,
and one anti-pattern.

**Principle (Leo):** *no API should be a catch-all wildcard.* The wildcard
(`apis/catchall-api.bicep`, one op per method forwarding `/{*path}`) is a **stopgap
durable shell**, never the end state — it 404s on real routes and gives the dev portal
nothing to show.

| Shape | Used by | When |
| --- | --- | --- |
| **Pipeline OpenAPI import** (preferred) | costcenter, salescrm, fireball funcs, b3owa | backend exposes a live `/openapi/v1.json` or `/api/swagger.json`. The **satellite pipeline owns the API** — NO bicep module. |
| **Bicep-enumerated operations** | fireball-eventpublisher / -bridgeepay / -quarantined | legacy-PROD-faithful ports; ops hand-declared as `service/apis/operations`. |
| **Bicep swagger-definitions import** | **decus** (the only intended bicep exception) | enumerated `...swagger.definitions+json` + a `validate-jwt` policy in `decus/modules/api.bicep`. |
| ~~Wildcard catch-all~~ (anti-pattern) | b3admin (blocked), some SE facades | stopgap only — retire it. |

### The pipeline-import mechanism

`.azuredevops/build/templates/apim-import-steps.yml` — one task, N APIs, parallel
prefetch + parallel `az apim api import`. Caller passes:

```yaml
- template: ../templates/apim-import-steps.yml
  parameters:
    serviceConnection: "$(SERVICE_CONNECTION)"
    apis:
      - apiId: costcenter
        displayName: Cost Center
        description: Cost center and master-data lookups.
        tag: lookups-reference           # capability tag (resource declared in tags.bicep)
        path: costcenter
        format: OpenApiJson              # OpenApiJson for /openapi/v1.json or 3.x; OpenApi for 2.0
        swaggerUrl: https://FUNC-WHS-346-CostCenter-CentralUS-$(ENV).azurewebsites.net/api/swagger.json
        subscriptionRequired: false      # SE model: open through APIM, gate is backend posture (ws-peti4)
        product: costcenter              # dev-portal catalog membership (resource in products.bicep)
```

The template has a **ref-resolvability gate** (build 54178 incident): a 200-with-valid-JSON
swagger whose `$ref`s dangle during cold-start is treated as "still warming" and retried,
so the import never sees an incomplete doc.

### Retiring a wildcard (the costcenter model)

Worked example: b3owa, ws-ab7ok, 2026-06-08.

1. Confirm the backend serves a healthy spec: `curl https://apic.<env>.bridgespecialty.com/<api>/openapi/v1.json` → 200 with `paths`.
2. Replace the satellite pipeline's `APIMRouteSync` stage (often a stale "RBAC pending"
   placeholder — the shared SPN **can** import; costcenter proves it) with a real
   `apim-import-steps.yml` call.
3. Delete the `<api>Api` catch-all module from `wholesale/routing/apim/index.bicep`.
   **ARM incremental mode** (no `deploymentMode: complete` anywhere) means removing the
   module does NOT delete the existing API — the next pipeline import re-owns it as
   enumerated ops. Wildcard → enumerated, no outage window.

**Gotcha that blocks import:** a backend whose `/openapi/v1.json` 500s can't be imported.
b3admin hit `System.InvalidOperationException: ...JSON property name ... collides` — a
System.Text.Json duplicate-property collision under `JsonSerializerDefaults.Web`
(`PropertyNameCaseInsensitive = true`) crashing the OpenAPI doc generator. Fix is in the
app repo (ws-8bzid). General `/openapi` doc-gen crash family: see memory
`APIM swagger endpoint patterns`.

**Gotcha that blocks import #2 — APIM rejects OpenAPI 3.1; `MapOpenApi` defaults to 3.1.**
Confirmed 2026-06-08 (ws-8bzid), b3admin + b3owa. The wholesale APIM `api import` only
accepts OpenAPI **3.0** — a 3.1 doc fails with `(ValidationError) Parsing error(s): The
input OpenAPI file is not valid for the OpenAPI specification 3.1.1`. But .NET 9/10's
`Microsoft.AspNetCore.OpenApi` (`MapOpenApi` / `/openapi/v1.json`) **defaults to 3.1.1**.
The existing fleet (costcenter/salescrm/fireball) dodged this only because it imports
Swashbuckle's `/api/swagger.json`, which is 3.0. Any satellite moving to the native
`/openapi/v1.json` import hits the wall. **Fix — one line in the app's `AddOpenApi`:**

```csharp
builder.Services.AddOpenApi(options =>
    options.OpenApiVersion = Microsoft.OpenApi.OpenApiSpecVersion.OpenApi3_0);
```

Then `/openapi/v1.json` emits `openapi: 3.0.4` and imports cleanly. Verify on the backend
directly (`curl https://appsvc-...azurewebsites.net/openapi/v1.json | jq .openapi`), not
through the gateway — once enumerated, APIM 404s `/openapi/v1.json` (it isn't an operation).

**The masking trap that hides both gotchas:** the bulk-import task in
`apim-import-steps.yml` runs with `continueOnError: true`, so a *failed* import surfaces as
a yellow `succeededWithIssues` stage, NOT a red build. Run color lies. The real signal is
the import task log's `##[section]Summary: total=N ok=? fail=?` line and the per-API
`OK`/`FAIL` rows — pull them before declaring an import done:

```bash
# from the build timeline, find the "Bulk import" Task record's log.url, then:
curl -s --proxy socks5h://127.0.0.1:1080 -H "Authorization: Bearer $TOK" "$LOGURL" \
  | grep -iE "Summary:|FAIL |OK |ValidationError"
```

Proof of a real enumerated import (not the 7-method wildcard): the APIM operation count
equals the doc's path count (b3admin=108, b3owa=25), via
`GET .../service/<apim>/apis/<apiId>/operations?api-version=2024-05-01`.

### APIM subscription-key sync (the final step, not a preflight)

Sync APIM subscription keys **last**, AFTER the API import completes — order is `Preflight → InfraBootstrap → CodeDeploy → APIMRouteSync → (final) MigrateApimSubKeys`. If keys sync before the API exists, legacy consumers await keys for an API that isn't there yet.

- **Legacy subscription keys carry over verbatim** — copy them into the canonical KV via a cross-subscription SPN grant (**API Management Service Contributor** on the legacy APIM, e.g. the wholesale SC SPN granted on the 3 legacy fireball APIMs in sub `979366b2`). No regeneration, no consumer churn (Leo: canonical name = legacy name).
- **Legacy products + keys are NOT CLI-discoverable** — manually audit each legacy env and cross-reference the legacy KV inventory. Watch naming variations (`FireballDevUser` vs `FireballTestUser`). Legacy **PROD** `listSecrets` is authz-blocked even with Owner+Contributor (sub `0e4f65bb`).
- **Pipeline-ownership seams cause silent desync**: if pipeline A seeds the subkey into KV but pipeline B refreshes the postman-env, dev can work by accident while test goes stale (403). Confirmed: inscipher-proxy (501) seeds the KV subkey but doesn't refresh postman-env; fireball (450) refreshes postman-env but doesn't own the inscipherproxy subscription.

---

## APIM status-code triage (gateway vs backend, before pulling App Insights)

The APIM status code + latency + body shape localizes a failure to the gateway or the backend in one curl, before you reach for the AI trace:

| Signal | Verdict |
| --- | --- |
| **401 in ~4ms**, terse `{statusCode, message}` body, NO `Request-Context` header | **Gateway subscription-key gate** rejected it — `subscriptionRequired=true` runs *before* operation lookup, so the backend was never hit. |
| **401/403 in seconds**, body is backend JSON, response carries a `Request-Context` appId header | **Backend auth** rejected it (the request reached the backend). |
| **404** | Operation/route not found — APIM has no operation wired. Cross-check the AI trace `exReason = OperationNotFound`. |
| **403 "Web App Unavailable"** | Backend **IP-restriction** is blocking APIM's gateway IP (commonly a stale gateway IP after a migration). |

- **IP-locked gateways silently 403 all non-Cloudflare traffic.** The raw APIM gateway has a global filter allowlisting exactly the ~15 official Cloudflare IPv4 ranges, so a direct hit from an MS-hosted agent 403s. Route through the CF-fronted hostname: `https://apic.<env>.bridgespecialty.com/<api>/...`.
- **Function `authLevel=Function` needs APIM to inject `x-functions-key`.** A function-layer API returning a non-APIM-shaped 401 means APIM is missing the `set-header x-functions-key` policy — 15 Fireball APIs all 401'd because only `directbill` had the injection policy.

## APIM v1 → v2 is a delete-and-recreate (not an in-place flip)

Flipping a Developer (v1) APIM to StandardV2 (v2) **cannot be done in-place** — different resource API versions, different subnet delegation (`Microsoft.Web`), different deployment semantics. A Bicep update against the live Developer instance fails. **Delete and recreate.** Per-env SKU: non-prod **Standard**, prod **Premium / StandardV2**; StandardV2 **cannot downgrade** back to Developer.

Routing/config differences that bite (dev stand-up 2026-06-03):

- **Canonical routes at the API level via `serviceUrl`** (legacy used per-op `<set-backend-service>`). A `serviceUrl` **missing the `/api` suffix** 404s every operation.
- **`apic` must be declared in Bicep** — if the AVM module resets `hostnameConfigurations` to BuiltIn-only it drops `apic.<env>`, the Cloudflare **526** root cause (§ AVM module defaults that bite).
- **IP-lockdown is dead post-v2** — route ingress through Cloudflare instead.
- **Subscriptions are NOT in Bicep** — keys carry over verbatim as plain-string values into canonical KV. v1 product sub-keys go silently unused after a re-import and are safe to decommission.

## APIM provisioning prerequisites & Bicep gotchas

Before provisioning APIM with VNet integration, validate the subnet and watch these Bicep traps:

- **Validate the subnet first.** APIM-with-VNet needs `Microsoft.Sql` + `Microsoft.Storage` service endpoints on the subnet, NSG rules aligned across envs, and an actual CIDR. APIM can ARM-deploy `Succeeded` yet have a **null `addressPrefix`** — a shell with no network presence. A green deploy is not proof; check `properties.virtualNetworkConfiguration.subnetResourceId` resolves and the subnet has a CIDR.
- **APIM private-endpoint `groupId` is `Gateway`** (App Service is `sites` — don't copy the App Service PE module verbatim).
- **APIM global-scope policy cannot use `<base/>`** — `Element <base/> is not allowed in global context`; `<base/>` is only valid at API/operation scope.
- **An APIM backend `resourceId` must be an absolute HTTP URL**, not an ARM resource path.
- **Logic App Consumption tier has no VNet integration**, so it cannot reach a PE-only Service Bus — use Logic App **Standard** when the SB is private-endpoint-only.

## Health endpoints + APIM case-sensitivity / backend-service policy

Fleet convention: health endpoints are **lowercase `/health`** (or `/api/health`) with the route literal matching `.WithName("health")` (lowercase op id). Onboarding them through APIM:

- **APIM routes are case-sensitive.** A 404 *through APIM* (when the backend serves the route directly) is usually a **path-case mismatch** (`/Health` op vs `GET /health` literal), not a down backend. Fireball had `.WithName("Health")` against a `GET /health` literal — fixed by lowercasing all three apps.
- **Every operation needs a backend.** A manually-added `/health` op with **no `<set-backend-service>` policy** (and no API-level `serviceUrl`) **500s "Backend unhealthy"** or 404s. Five Fireball `/health` ops 500'd until the `<set-backend-service backend-id=...>` policy was added.
- **Functions `serviceUrl` ends in `/api`.** The op urlTemplate varies per satellite (`/health` vs `/api/health`) — curl the exact path per satellite before declaring it down.
- **Probe in fallback order** `/health` → `/api/health` → `/healthz` → `/ping`, and distinguish **404 (route missing)** from **500/503 (host crashed — SKILL.md § Function/App boot-crash triage)**. A `200` alone is not proof — assert the body/path, not just the status.

## AVM module defaults that bite (SKU / identity / admin immutability)

AVM (Azure Verified Modules, `br/chsmodules` / `br/public:avm`) carry **enterprise defaults** that break against pre-existing WHS-346 resources. Local module forks infer correct values; AVM does not:

| Resource | AVM default that bites | Fix |
| --- | --- | --- |
| **SQL DB in an elastic pool** | defaults to standalone `GP_Gen5_2` → `ElasticPoolSkuCombinationInvalid` | pass explicit `ElasticPool` SKU name + `capacity: 0` |
| **Cosmos account** | `defaultIdentity: SystemAssignedIdentity` is **rejected at create-time** | create with `FirstPartyIdentity` (via `defaultIdentityOverride`), flip to SMI on a *later* deploy; a Failed create leaves an orphan — delete + re-queue |
| **SQL Server** | AAD-admin + CloudSA admin are **immutable** | to change either, recreate the server |
| **APIM (v2)** | resets `hostnameConfigurations` to BuiltIn-only every deploy, dropping `apic.<env>` → the Cloudflare **526** | manage custom domains **in Bicep**, never runtime PATCH |

General rule: when an AVM deploy fails on a resource that already exists with non-default settings, suspect a default-vs-existing mismatch before debugging the module. Verify the AVM contract via Context7/MS-Learn first — never hypothesize (memory `Don't hypothesize on library/module contracts`).

## SQL / Cosmos cross-sub migration mechanics

WHS-346 DB migrations move data via geo-replication / container-copy, not pipelines (the decision was a manual operator runbook). Operational gotchas:

- **A linked SQL geo-secondary (`createMode: Secondary`) is read-only** and rejects `CREATE USER` / `ALTER ROLE` until cutover breaks the link and promotes it to primary. Plan all RBAC/user seeding for **after** the link break.
- **ARM geo-replication needs the full source database Resource ID** (not the name) in `sourceDatabaseId`. Keep the enterprise name (no `-migrated` suffix) — the secondary becomes primary post-failover.
- `az sql db replica create` can return success with **no visible Primary link** after ~25s — verify in the portal, don't trust the CLI exit code.
- **Breaking the link** uses `Microsoft.Sql/servers/databases/replicationLinks/delete` — the SPN needs that specific action (job-function-grantable; not blocked by the escalation fence).
- **Backup→restore cross-sub** auto-translates SQL RBAC by **SID match** if the **same SPN** restores — no manual AAD re-seed. Backup-import is safe **only into empty targets** (else it clobbers).
- **Cosmos has no cross-account geo-rep** — use `az cosmosdb copy --mode Online` (change-feed). Cosmos periodic→continuous backup is a **one-way, unreversible** re-platform — gate it.

App Config CLI/REST auth gotchas (recur during the same migrations):

- `AzureAppConfiguration.Connect(connectionString)` wants the **full connection string** `Endpoint=...;Id=...;Secret=...`, NOT the bare `AzureAppConfigEndpoint` URI — wiring the URI yields a Parse error at startup.
- `az appconfig kv list --auth-mode login` **403s even for Owner** — use `--auth-mode key` for reads.
- `az appconfig kv set`/`set-keyvault` **crash at parser-build on Python 3.14** ("badly formed help string"); reads work — write via the data-plane REST PUT + HMAC instead.

## Agent-pool egress matrix — split a job that touches both PEs and public endpoints

The two ADO agent pools have **inverse** reachability, so a job touching both PE-only and public resources MUST be split across pools:

| Pool | Reaches PE-only resources | Reaches public endpoints |
| --- | :-: | :-: |
| **VNet-injected** `WHS-ManagedPool-CentralUS-<ENV>-01` | yes (with `inject-pe-hosts.yml` IP-pinning; custom DNS lacks privatelink forwarders) | **no** — the WHS NVA blocks egress to public endpoints (`global-appconfig-all.azconfig.io`, `registry.npmjs.org`) |
| **MS-hosted** | **no** — fails `ForbiddenByConnection` *before* RBAC fires | yes (open internet) |

- An MS-hosted agent hitting a PE-only KV fails `ForbiddenByConnection` **before** any RBAC check — that's a routing wall, not a missing role. Move the step to the VNet pool; don't add RBAC.
- `npm ECONNRESET` to `registry.npmjs.org` from the VNet pool is a silent SYN-drop; **NAT alone does not fix it** (build 52879 reproduced 52858's failure despite the NAT bicep landing). Keep npm/public steps on MS-hosted until verified.
- **MS-hosted free-tier has a hard 60-min wall** regardless of `timeoutInMinutes` — a job set to 75 was killed at 60. Long work runs on the WHS-Pool.

**Practical split:** App Config / public-endpoint / npm steps → MS-hosted; PE-only KV / SQL / Service Bus steps → VNet pool.

## False-green stages (a stage marks GREEN while doing nothing)

A green stage is NOT proof the work happened — verify the effect, not the exit code. Known false-green modes:

- **Swallowed `--query` error** — JMESPath has **no `to_lower()`**, so `az functionapp/webapp list --query "...to_lower()..."` errors; a trailing `|| echo ''` swallows it, leaving the var empty, so the downstream `for` loop never iterates. Prefer **bash-side filtering** over JMESPath that can silently error.
- **Presence-gate skip** — wholesale `449` can skip `WholesaleDeploy` (and dependents) when APIM is *detected present*, unless `forceBootstrap: true`. Idempotent incremental Bicep is its own self-heal — no probe needed.
- **Preflight-before-deploy** — a health/preflight audit that runs BEFORE the code-deploy stage always "passes" on stale state. Order matters.
- **Zero-secret Seed** — a Seed stage can copy **zero** secrets and still go green: DNS failures get masked as `WARN: skipped` lines. **Always verify a Seed by source-vs-dest secret COUNT.**

## WHS pipeline tooling foot-guns (Windows agent / shell silent-skips)

A class where a tool silently skips work and the pipeline goes green-but-wrong, or fails Linux/Windows-only:

- **A bare `dist` `.gitignore` rule silently ignores vendored `lib/*/dist/` files** → never commit → prod 404s. Use `git add -f` or scope the ignore (`/dist/`).
- **UTF-16 LE files are treated as binary by git** → `git grep` skips them silently. Re-encode to UTF-8 to grep/diff.
- **Windows cloudpc runs PowerShell 5.1** (stricter parser than PS7) — a script that parses under PS7 can fail on cloudpc. Test via `powershell -File script.ps1`, not `pwsh`.
- **`npm i -g pnpm@latest` pulls pnpm 11** (needs `node:sqlite`, Node ≥ 22.13) and breaks Node-20 pipelines. **Pin `pnpm@10`**.
- **`sqlcmd -P` caps passwords at 128 chars** — a ~1500-char AAD token fails. Write the token to a **UTF-16 LE** file and pass `-P /path/to/tokenfile` (file form has no cap).
- **A bash `for` loop exits with the last iteration's command status** — a trailing falsey `[ $i -lt N ]` makes the loop exit `1`, failing the step though the work succeeded. End the body with `:` or `exit 0`.
- **Windows CI is case-insensitive** — a TypeScript import with a case mismatch builds on a Windows agent but fails on Linux. Match import casing exactly.

---

## Wholesale App Insights trace

A single **shared** Application Insights per env captures both APIM gateway telemetry and
most backend telemetry — so you can attribute a gateway failure to its backend without
per-satellite AI access.

| Env | Component | AppId (for `--app`) |
| --- | --- | --- |
| dev | AI-WHS-346-Wholesale-CentralUS-DEV | `f4e1a837-c4e0-48f3-9e79-e33e3924d9e6` |
| test | AI-WHS-346-Wholesale-CentralUS-TEST | `b179c272-d7d9-49e2-93f1-618ba6438a10` |
| stage | AI-WHS-346-Wholesale-CentralUS-STAGE | `d7d8f2f1-dc0f-4ccf-ad88-9b617ff695b6` |

RG `rg-whs-346-wholesale-centralus-<env>`, sub `21b25913` (WHS-346-Wholesale-DEV hosts
all three). Re-discover with a Resource Graph query on `microsoft.insights/components`
where name startswith `AI-WHS-346-Wholesale`.

**The backend-attribution pattern.** APIM emits the inbound `request` AND its outbound
`dependency` under the **same `operation_Id`**, so one self-join tells you whether a
gateway 5xx is a real backend fault or an APIM misroute:

```kql
let win = 30m;
let deps = dependencies | where timestamp > ago(win) | where cloud_RoleName startswith "APIM"
  | project opId = operation_Id, depHost = tostring(parse_url(target).Host), depCode = resultCode;
let excs = exceptions | where timestamp > ago(win) | where cloud_RoleName startswith "APIM"
  | project opId = operation_Id, exReason = strcat(type, ": ", substring(outerMessage, 0, 140));
requests | where timestamp > ago(win) | where cloud_RoleName startswith "APIM" | where toint(resultCode) >= 400
| project opId = operation_Id, apimPath = name, code = resultCode, t = timestamp
| join kind=leftouter deps on opId
| join kind=leftouter excs on opId
| summarize arg_max(t, code, depHost, depCode, exReason) by apimPath
```

- The APIM `request.name` is literally `METHOD /path` with env-substituted values — an
  **exact** match key against a Postman/route path. No fuzzy matching.
- A 404 with `exReason = OperationNotFound` = APIM has no operation wired (request never
  reached a backend). A 5xx with `depHost = func-...` + `depCode = 500` = genuine backend fault.
- **`requests` carry NO bodies** — notes can describe host/code/policy, never payload.

**Two foot-guns (both will silently give you wrong/empty results):**
1. **Always pass `--offset <window>`** to `az monitor app-insights query` — without it the
   API clips to ~1h and the in-query `ago()` is meaningless. (Same for log-analytics: use `--timespan`.)
2. **Bypass the `az` wrapper for KQL** — it word-splits the quoted `--analytics-query`. Either
   call the real binary with `AZURE_CONFIG_DIR=$HOME/.azure-bbadmin` + SOCKS proxy env set, or
   (cleaner) drive it from Python `subprocess` with an argv list (no shell, no split).

```bash
export AZURE_CONFIG_DIR=$HOME/.azure-bbadmin HTTPS_PROXY=socks5h://127.0.0.1:1080 \
  HTTP_PROXY=socks5h://127.0.0.1:1080 NO_PROXY=localhost,127.0.0.1,::1
"$HOME/.local/share/pipx/venvs/azure-cli/bin/az" monitor app-insights query \
  --app f4e1a837-c4e0-48f3-9e79-e33e3924d9e6 --offset 30m --analytics-query "<KQL>" -o json
```

Live consumer: `~/dev/ws/scripts/lib/postman-trace.py` (turns this query into per-endpoint notes).

---

## Postman per-env smoke

`~/dev/ws/scripts/bin/postman-smoke <dev|test|stage>` runs the 221-request wholesale
aggregate collection through `apic.<env>.bridgespecialty.com` and produces:

- a status histogram + 5xx/no-response failures table (exit `0` clean / `2` failures / `1` setup error);
- `docs/postman/endpoint-status-<env>.md` — per-env report, columns `API | Path | Status | Note`,
  where Note resolves **known-exceptions JSON → inline App Insights trace → built-in default**;
- the cross-env matrix `docs/postman/endpoint-status-matrix.{json,md}`;
- an auto-commit of the refreshed status docs.

Flags: `--no-trace` (skip the AI scan), `--no-commit`, `--trace-window <W>` (default 30m;
bump to `2h` to dodge telemetry ingest lag right after a run), `--folder <sat>`, `--strict`
(4xx fail too), `--no-matrix`. Accepted exceptions live in
`docs/postman/postman-known-exceptions.json` (precedence `api → env → endpoint`) so known
failures don't re-trigger a trace query.

**The big 401 wall is backend Entra Bearer, NOT a sub-key/`apiKey` defect (corrected 2026-06-09).**
The old note ("env files lack `apiKey`, collection sends literal `{{apiKey}}`") is STALE — the live
collection has **zero** `{{apiKey}}` refs: collection-level auth is `{{key_master}}` and every folder
overrides with its own `{{key_*}}` (or `noauth`). The ~99×401 on `test` is the **`api` folder** —
~99 `/api/Admin/*` ops that route through the **canonical** APIM (`apic.<env>` → `APIM-WHS-346-
Wholesale-CentralUS-<ENV>`, API `fireball-api` path `api`) to the **canonical** backend
`APPSVC-WHS-346-FBApi-CentralUS-<ENV>` — NOT the legacy `fireball-<env>-api` (verify the route with
`az apim api show ... --query serviceUrl`; do not infer the backend from ARRAffinity/ASP.NET response
headers — those are present on canonical App Service responses too). The backend demands an Entra JWT.
Proven live: identical `WWW-Authenticate: Bearer` 401 **with key, with master key, and with no key**
(APIM passes through; the backend rejects). The audience app reg is `local-dashboard-dev`
(`822e3250`, tenant `f1289cc5`) — confirmed for canonical dev/test/stage (the canonical KV secret is
`AzureAd-ClientId` / `AzureAd-TenantId`, **single dash**, NOT `AzureAd--ClientId`).
`Fireball.Api` validates a **v2** authority (`login.microsoftonline.com/{tid}/v2.0`, `aud=ClientId`
bare). **Full auth chain to a 200 (verified 2026-06-10):** (1) reg `requestedAccessTokenVersion=2`
(else v1 token → `sts.windows.net` issuer + `api://` aud both mismatch); (2) the Admin controllers
carry `[Authorize, FireballAuthorize(groupName:"Fireball-Admin")]` → the token's `groups` claim must
contain the Fireball-Admin GUID (`GroupID:Fireball-Admin`), so it needs a **delegated** token from a
member, not a client-credentials/app-only token; (3) reg `groupMembershipClaims` must emit the group
(assign Fireball-Admin to the enterprise app under ApplicationGroup, or set SecurityGroup) + the
Azure CLI client pre-authorized for `access_as_user`. A subscription key never opens this surface;
classify the `api` folder as backend-auth-blocked (delegated-admin only — not headless-smoke-able),
not a routing regression.

### Sub-key gate vs open routes (interpreting a 401)

Not every APIM 401 is a failure. Distinguish:

- **A required-subscription route returns 401 WITHOUT a key — that's the auth gate WORKING.** The body says `subscription key` ("Access denied due to missing subscription key"). Open/internal routes return 200 with no key. Postman collections must document **which routes need a key** per env/route — don't assume all routes are keyed (or all open).
- **Exclude app-level 401s.** Fireball's `subscription key is not mapped to vendor` is a **backend** rejection, not an APIM gate — different layer.
- **You cannot exempt `/health` from a key with an operation-level `<choose>`.** API-level `subscriptionRequired` is enforced **before** operation policies run. To open one op, toggle **API-level `subscriptionRequired: false`** and re-implement the check as a conditional policy.
- Use the standard, **case-sensitive `Ocp-Apim-Subscription-Key`** header — a custom header name bypasses APIM's subscription check entirely.
