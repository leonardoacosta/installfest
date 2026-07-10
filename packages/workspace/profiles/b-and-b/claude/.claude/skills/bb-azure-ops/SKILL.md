---
name: bb-azure-ops
description: >-
  B&B (Brown & Brown / bbins) overlay on generic Azure — Azure/Entra/ADO/Graph/ServiceNow via cloudpc.
  Triggers: Brown & Brown, bbins, WHS-346, cloudpc, BBAdmin, BBAdminLAcosta, AzureAD\LeonardoAcosta,
  brown.service-now.com, brownandbrowninc, brownandbrowninc.visualstudio.com, SC-WHS-346, RG-WHS-346-*,
  APIM-WHS-346-*, --as-admin, --as-o365, --as-personal, scripts/ado, scripts/snow-refresh, snow-cookie-refresh,
  SOCKS5, Microsoft Graph, Teams, Outlook, Entra app registrations, SPN permission ceiling, wholesale APIM,
  wholesale App Insights; fleet codes on paths ~/dev/<code> — ws (Wholesale Architecture, ws-topo), fb (Fireball),
  dc (Doc Center, doc-poc), se (Submission Engine, Bridge-Summit), tb (The Bridge), sc (Sales CRM),
  ba (B3, b3admin, b3owa), bo (Office Index PIPS, OfficeIndexToPIPS2.0), es (Email Scheduler), ew (IaC Hub, IaC-Hub.wiki),
  ic (Azure Projects), lu (Lookups, MDR, Master Data Repository), pp (PIPS), plus satellite Bicep names decus + costcenter.
allowed-tools: Read, Glob, Grep, Bash
---

# Brown & Brown Azure Operations

The B&B-specific operational overlay on top of generic Azure knowledge. Three identities, one bastion, one SNOW host, multiple SPNs with shared limits — this skill encodes how to reach Azure, ADO, Graph, and SNOW resources in the Brown & Brown tenant from a Linux workstation.

## When This Skill Applies

Load proactively on ANY B&B context — even a casual mention of a fleet code, a WHS-346 resource, cloudpc, or one of the identity flags. The frontmatter trigger list is deliberately exhaustive because misrouting to generic Azure knowledge (wrong identity, wrong escalation path, wrong naming) is the dominant failure mode here.

Two disambiguations the trigger list compresses:

- **`ba`/`bo` mean different things in two vocabularies.** In the *fleet-code* triggers, `ba` = the B3 pair (b3admin + b3owa) and `bo` = Office Index PIPS. In the `scripts/ado queue <code>` *pipeline* registry they SPLIT: `ba` = b3admin only, `bo` = b3owa. See § ADO access and `references/whs-pipelines.md` § Pipeline ID registry.
- **cloudpc is the conditional-access broker, not just a jump box.** Every Azure/Graph/SNOW HTTPS call routes through its SOCKS5 tunnel because B&B enforces Conditional Access by IP. See § Mental model.

## Common foot-guns (scan this table before any B&B work)

| Symptom | Cause | Fix |
| --- | --- | --- |
| `az` rejects `--as-admin` flag | (Historical — the `az()` shell function was REMOVED 2026-05-31; bare `az --as-admin` now resolves to `~/.local/bin/az` and works. If it recurs, a stale shell still has the old `az()` — `unset -f az` or open a fresh shell.) | Bare `az --as-admin ...` works; or call `/home/nyaptor/.local/bin/az --as-admin ...` directly |
| `az monitor diagnostic-settings list --resource <acct>/blobServices/default` returns EMPTY `value[]` even though a diag setting IS present (portal/REST confirm) | The `az monitor` CLI FALSE-EMPTYs on storage sub-resource scopes — it does not enumerate them correctly. Confirmed 2026-06-19. | Verify via the **management REST API**, never `az monitor`, on any storage sub-resource scope. → deep-dive |
| `az graph query -q "X \| where ..."` fails: `unrecognized arguments: \| where ...` | The `~/.local/bin/az` wrapper does `exec "$REAL_AZ" $ARGS` **unquoted** — word-splits any multi-word/quoted arg (KQL, `--query` JMESPath, spaced `--tags`). Confirmed 2026-05-25. | Bypass the wrapper: set the BBAdmin config dir + SOCKS proxy env vars and call the real pipx binary directly. → deep-dive |
| SNOW URL 404 | Used `brownandbrowninc.service-now.com` | Subdomain is `brown` not `brownandbrowninc` |
| ADO call returns "project does not exist" | Used `brownandbrown` org name | Org is `brownandbrowninc` (note the `inc`) |
| Graph `$filter` returns 400 | Bare space in OData query string | URL-encode spaces as `%20` |
| `dotnet nuget locals --clear` cleared a cache but disk still 98% | Cleared `leo.346-CPC-QJXVZ` profile, not `LeonardoAcosta`'s | Hardcode `C:\Users\LeonardoAcosta\...` paths in cleanup scripts |
| Any B&B command hangs ~30s then fails | SOCKS tunnel down (rare — b-and-b workspace activation auto-starts it) | Re-activate the workspace: `wsenv --activate ws` (fallback: `systemctl --user start cloudpc-tunnel.service`) |
| "cloudpc memory is full" alarm | C: drive at 98%, not RAM | Check `Get-CimInstance Win32_LogicalDisk` first |
| `ssh cloudpc` times out (SYN unanswered) OR resets pre-banner (`kex_exchange_identification: Connection reset`) but SOCKS tunnel + `az`/`curl`-via-:1080 still work and `tailscale ping cloud-pc` pongs | cloudpc `sshd` is **HUNG/dead** (stops accept()ing or serving new conns; the already-forked `-D` tunnel keeps forwarding) — NOT disk/RAM. Two confirmed signatures: SYN-timeout 2026-06-11, pre-banner RST 2026-07-06. | **Not an ADO blocker**: `scripts/ado` is SOCKS-first (2026-07-06) — `queue`/`watch`/`token`/`registry`/`--dry-run` keep working; only the PS-proxy shortcuts (`projects`/`repos`/`pipelines`/`runs`/`prs`/`wi`/`raw`) need SSH. Fix sshd when convenient: RDP in, **elevated** PowerShell, `Restart-Service sshd -Force`. → deep-dive |
| Need to queue a multi-stage ADO pipeline scoped to a SUBSET of stages, but object-typed `templateParameters` (`tasks`/`projects` filters) are silently DROPPED by the queue API | The runs/build queue APIs only honor SCALAR template params; object/array overrides are dropped. | Use the runs API **`stagesToSkip`** field (separate from `templateParameters`) to skip the chain's middle; skipped deps still satisfy a downstream `succeeded()`. → deep-dive |
| Graph `oauth2PermissionGrants` PATCH returns 403 | BBAdmin lacks `DelegatedPermissionGrant.ReadWrite.All` | File SNOW ticket for admin consent path (see § Admin consent below) |
| SNOW `/api/now/v2/table/...` returns 401 with cookies | Modern REST API requires Bearer, rejects cookies | Use legacy `/<table>.do?JSONv2&sysparm_query=...` instead |
| Trying to call Teams chat endpoints with BBAdmin token | BBAdmin token lacks `Chat.Read` | Use `~/.graph-token.json` (O365) instead |
| `legacy ALL-Wholesale-*` sub query returns empty | BBAdmin has no RBAC there | `az --as-o365 account set -s <legacy-sub-id>` first |
| Legacy **PROD** sub `0e4f65bb` (ALL-Wholesale-PROD) returns EMPTY to `--as-o365` even though o365 can list the sub | o365 has NO *active* RBAC on legacy PROD; the PIM-eligible Contributor (`pim` #4) is on **BBAdmin**, NOT o365. Confirmed 2026-06-05. | `pim 4` (activates BBAdmin 8h), then query as **`--as-admin`**; Resource Graph lags the grant — use direct `az resource list`. → deep-dive |
| `az monitor app-insights query -g <rg> --app <name>` returns empty / `Expecting value` | the `-g + --app <name>` form silently fails to resolve the component | Pass the AppId **GUID**, not the name. → deep-dive |
| `az monitor app-insights query` looks "dry" / returns only ~1h of data even with `where timestamp > ago(90d)` in the KQL | The command defaults to a ~1-HOUR API timespan when no `--offset`/`--start-time` is passed, and that timespan CLIPS the in-query `ago()`. | ALWAYS pass `--offset <window>` (or `--start-time`/`--end-time`); same for `log-analytics query` (`--timespan`). → deep-dive |
| Background ADO build-poll loop never breaks on completion, runs to its full iteration cap; every progress line shows an empty status | ADO build/timeline JSON embeds raw control chars; `jq` errors on them and `2>/dev/null` swallows it → gated scalar is `""`. Confirmed 2026-06-04. | Grep the scalar (`grep -o '"status":"[^"]*"' \| head -1 \| cut -d'"' -f4`) or `python json.loads(strict=False)`; never `2>/dev/null`-swallow a loop-gate parser. → deep-dive |
| ADO `preview` (w/ `yamlOverride`) fails `Unexpected parameter 'X'` after a local template edit | `yamlOverride` overrides only the MAIN yaml; referenced TEMPLATES still come from the COMMITTED branch. | Commit+push the template, then preview the pushed branch. → deep-dive |
| ADO QUEUE-time fail: "secure file `X` could not be found / not authorized" despite a runtime `condition:` that should skip the `DownloadSecureFile@1` task | ADO validates the `secureFile` ref at COMPILE/queue time, BEFORE any runtime `condition:` runs. | Gate with a COMPILE-TIME `${{ if parameters.flag }}` block (parameters, NOT variables). → deep-dive |
| A var ADDED to a variable group PROGRAMMATICALLY is visible via API GET but reads `$(VarName)` EMPTY at runtime; UI-set vars expand fine | The pipeline's view of group MEMBERSHIP is cached/stale to API-added vars (not secret-specific). | Add/modify VG vars via the ADO **web UI** (or open+Save the group after an API write). → deep-dive |
| App boots 500/503 after a wholesale **infra** deploy though code didn't change; settings like `WEBSITE_DNS_SERVER` / `AZURE_CLIENT_ID` vanished | Bicep `siteConfig.appSettings` (incremental mode) is a **full replacement, not a merge** — wipes any setting not in the template. | Bake **all** settings into Bicep (single source of truth), or restart the app post-deploy. → deep-dive |
| Function App's `DefaultAzureCredential()` resolves to a MI with **zero roles** (KV/SQL 403) though a user-assigned MI is attached | With both a system-MI and a user-MI present, `DefaultAzureCredential` picks the **system-assigned** one unless told otherwise. | Set the `AZURE_CLIENT_ID` app setting to the **user-assigned MI's client id**. → deep-dive |
| App Service worker dies at boot (ANCM `000`), a runtime-write app hangs, OR IIS **503.0 "Could not download zip"** on every route | `WEBSITE_RUN_FROM_PACKAGE` and `AzureWebApp@1`'s `deploymentMethod` disagree (both directions 503). | `WEBSITE_RUN_FROM_PACKAGE=0` **+** `deploymentMethod: zipDeploy` always travel together. → deep-dive |
| A **secret** variable-group var reads empty in a script step even though the VG is linked and the name matches | ADO does NOT auto-expose **secret** VG vars as `$(macro)`/env (non-secret ones expand). | Map it into an `env:` block on the consuming task (`env: { MY_SECRET: $(MySecretVar) }`); not inherited downstream — inline per task. → deep-dive |
| `azureSubscription: $(SERVICE_CONNECTION)` fails to resolve / the SC isn't found at queue time | The `azureSubscription:` value (and other resource references) binds at **compile time**, so a runtime `$(macro)` doesn't resolve there. | Use the **literal** service-connection name, e.g. `azureSubscription: SC-WHS-346-Wholesale-DEV` — not a variable. |
| A device-code/`az login` re-auth reported success but the failing operation is STILL dead | **Re-auth wrote one config dir; the op reads another** — plain `ssh cloudpc 'az login'` refreshes O365 `~/.azure`, but `--as-admin` reads `~/.azure-bbadmin`. Burned 2026-06-02. | Re-auth the profile the op reads (`AZURE_CONFIG_DIR=$HOME/.azure-bbadmin az --as-admin login`); verify with a REAL ARM op, not `az account show`. → deep-dive |
| Concurrent agents clobber the cloudpc az session | Two `az account clear`/`login` calls race on the shared cloudpc and wipe each other's auth (single-session model) | Serialize cloudpc auth ops; recover with `ssh cloudpc 'az login'`. cloudpc tokens also hit the 60/90-day refresh limit (`AADSTS70043`) — full device-code re-auth. |
| A **local .NET process** (not `az`/`curl`) can't reach a private-network KV/SQL even though the SOCKS env vars are set | The **.NET Azure SDK ignores `HTTPS_PROXY`/`HTTP_PROXY`** env vars, so it never routes through the cloudpc SOCKS tunnel. | Run the .NET process under `proxychains4` (forces SOCKS5 at the syscall layer); the env-var SOCKS pattern only works for `az`/`curl`/Python `requests`. |
| Browser MSAL/OAuth (VS Code Azure auth, Cursor Cosmos) fails `ERR_EMPTY_RESPONSE` / "localhost didn't send data" when proxied | **ProxyBridge never intercepts loopback** (`127.0.0.1`/`::1`), so a proxied browser hijacks the MSAL `localhost` callback. | Use an **unproxied** browser for the OAuth loopback step. → deep-dive |
| Edge from `ssh cloudpc` opens INVISIBLY and/or lands on the Microsoft "Sign in" page on an authed M365/SSC URL, even with `--user-data-dir` set to the o365 profile | SSH runs in **session 0** (no desktop) and **DPAPI is per-user**, so session 0 can't decrypt the o365 tokens → logged-out. | **NO** SSH/session-0 path to an authed page; launch via an **interactive scheduled task**. → deep-dive + § Launching / finding Edge on cloudpc |
| A fresh role/PIM grant still 403s on read or write for ~30 min after it's created | Azure RBAC is **eventually consistent** — the grant takes ~30 min to land and the token's claims stay stale until refreshed. | Wait ~30 min or force a token-claim refresh; PIM-elevate **first** for prod telemetry/Entra reads. → deep-dive |
| A Managed DevOps Pool SKU/size bump silently reverts, or a new pool sits in `notStarted` with zero agents | MDP's **own DSv5 quota** under `Microsoft.DevOpsInfrastructure` (separate from Compute) is exhausted; or an MS backplane hang. | Raise the DevOpsInfrastructure quota via the **portal**; backplane hang = route to a working pool + MS case. → deep-dive |
| A KV private-endpoint write (esp. a bulk secret seed) fails on a single `:443` TCP connect-timeout though routing is correct | Genuine transient — PE NIC blip / TCP retransmit / SNAT-port reuse, NOT a misconfiguration. | Wrap KV `set` in retry-with-backoff; **never gate a deploy or RCA on a single KV-PE timeout** — re-run first. → deep-dive |
| Cloudflare returns `522` / `525` / `526` fronting wholesale APIM | The CF code localizes the layer: **522** = TCP timeout to origin (rules OUT cert), **525** = SSL handshake, **526** = cert invalid (classically APIM `hostnameConfigurations` reset to BuiltIn, dropping `apic.<env>`). | **522** → check origin reachability + CF SSL mode; **526** → restore the custom-domain `hostnameConfigurations` in Bicep. → deep-dive |
| Granting a SPN the Graph **`Application.ReadWrite.OwnedBy`** app permission returns **403 `Authorization_RequestDenied`** despite holding `AppRoleAssignment.ReadWrite.All` + Cloud App Admin | It's a **privileged** Graph permission — consenting it needs **Global Admin / Privileged Role Administrator** (Cloud App Admin is fenced out). | BBAdmin/O365 can *manage* app regs but can't hand a SPN directory-wide control; escalate to GA/PRA or use `ws/apps/appreg-callbacks`. → deep-dive |
| Targeted single-RG sat `main.bicep` deploy under BBAdmin fails `AuthorizationFailed` on a `roleAssignments` module | BBAdmin lacks `roleAssignments/write`; no PIM role grants it | Deploy a throwaway no-roles copy of the bicep, OR let the wholesale pipeline (CHS-Owner SPN) re-apply the RBAC |
| Writing cloudpc's `C:\Windows\System32\drivers\etc\hosts` via ad-hoc PowerShell `Set-Content` truncates it to 0 bytes | `Set-Content` is not atomic on this file under cloudpc's session; a partial/failed write leaves it empty instead of unchanged. Confirmed 2026-07-06 during an ad-hoc PE-pin follow-on check. | NEVER hand-write that file with `Set-Content`/`Add-Content`. Always use the documented atomic pattern (`[IO.File]::WriteAllText(...)`, the managed-block idempotent script) from § PE hosts-pinning below — recover a truncated file via `Copy-Item` from the nearest VSS shadow-copy backup in `C:\Temp\hosts-backups\` if it happens. |
| A pipeline run sits `notStarted` for hours with an EMPTY timeline (no job ever dispatched to the pool), even though the service connection is fine and the pool has capacity | A variable group referenced in the pipeline's top-level `variables:` has no Library **pipeline-permission** grant for that specific def — a first-run authorization gate, distinct from SC authorization or pool capacity. Confirmed 2026-07-05: fireball-test (def 478) stuck 19h on VG `es-canonical-seed-temp-dev`; dispatch was instant the moment the VG was Permitted. | Permit the VG for that def in ADO Library (one click), or flip the VG's security to **"Allow access to all pipelines"** (the config `FortifyVariables` already has) so this can't recur on any current/future def. Default stance per Leo 2026-07-05: flip every project VG to allPipelines-authorized rather than permitting per-def. |
| Need ground truth on whether `api.<env>`/`apic.<env>` DNS actually changed (e.g. suspected v1->v2 APIM cutover), without a cloudpc round-trip | `scripts/dns-flip/manifest.json`'s `records` block is a **point-in-time snapshot** (dated `_live_audit_...`), not live state — and the CF zone-scoped token is already sitting in ws's `.claude/settings.local.json`. Confirmed 2026-07-07. | Query Cloudflare's REST API directly with that token — no cloudpc needed for zone-level DNS reads. → deep-dive |

> Wide rows above end in **→ deep-dive**; the full mechanism, incident narrative, exact repro, and every GUID/command/date for each lives in `references/foot-gun-deep-dives.md` (load it when the one-line essence isn't enough).

## Thinking patterns (frame before acting)

Before any B&B operational task, work these questions in order. They shape decisions in ways the procedures below can't — they keep you out of the wrong identity, the wrong escalation path, and the wrong naming choice.

### 1. Which identity does this need?

Run the decision tree below before reaching for a token. Defaulting to BBAdmin "just in case" hides silent permission gaps — a call that succeeds as BBAdmin but would fail as the satellite SPN looks like a working solution until the pipeline runs it. The identity choice is part of the design, not a debugging step.

### 2. What's the cheapest read path?

For any data lookup, walk this ladder top to bottom and stop at the first path that surfaces what you need:

```
Resource Graph KQL query    (cross-sub, fast, no write needed)
  ↓ if not enough columns
Typed `az` command          (sub-scoped, structured output)
  ↓ if Graph/SNOW data
JSONv2 endpoint             (legacy SNOW, structured)
  ↓ if data lives in UI variable pool
UI HTML scrape              (heavyweight, parse-fragile, last resort)
```

Skipping ahead burns time on parser fragility and rate limits. The ladder also surfaces "I don't actually need to be writing" early — many tasks that look like writes are actually reads-then-decide.

### 3. Is this constraint movable, or is it a permanent design decision?

The SPN ceiling section lists permanent constraints — they look like backlog items but they will not be lifted. Before drafting a SNOW ticket, check whether the wall you hit is in:

- Granting **Owner / User Access Administrator / RBAC Administrator** via the shared satellite SPN — blocked by the CHS Owner ABAC fence (job-function role grants are NOT blocked; see § SPN permission ceiling)
- Graph perms on the shared satellite SPN — permanent
- `DelegatedPermissionGrant.ReadWrite.All` on BBAdmin's Graph token — not grantable
- Application Administrator / Cloud App Admin direct assignment for BBAdmin — gated indefinitely

If yes, redirect to a path that doesn't fight the constraint: BBAdmin manual deploy with PIM, workload identity federation per satellite (see `docs/apim-satellite-rbac-design.md` in ws), or escalation to a colleague who already holds the role (Waheem, Dan O./Dan T., Ravi). Don't burn cycles engineering around what isn't moving.

### 4. What downstream isolation does this naming choice shape?

Resource names — API IDs, backend names, RG names, app reg display names — flow into future RBAC scoping. Two patterns already depend on this:

- `apim-api.bicep` soft fence assumes `${satellite}-*` prefix per registered API
- The proposed workload-identity-federation RBAC role assignments scope on those same prefixes (and ABAC conditions for backends)

A name picked today is the RBAC boundary tomorrow. When in doubt, prefix with the satellite short code (`doc-`, `decus-`, `fireball-`, `b3owa-`, `costcenter-`, `salescrm-`, `b3admin-`, `se-`, `thebridge-`).

### 5. Will this also work for the OTHER identity?

Shared-SPN reality: a call that succeeds locally as BBAdmin might fail when the pipeline runs it as `SC-WHS-346-Wholesale-{ENV}`. Before declaring something works:

- Was the proof-of-life test run as the identity that will actually execute in CI/CD?
- If you tested as BBAdmin, did you check the SPN-ceiling list for any actions you used?
- If you tested as the SPN, did you account for the Graph-perms gap (no manifest reads, no group adds)?

The mismatch surfaces as 403s in the deploy pipeline that nobody can reproduce locally. Verifying the failing-identity path up front prevents that.

### 6. Should this go into the skill or into project memory?

When you find a new gotcha:

- Reusable across projects, generalizes to "any time you're working with B&B Azure" → **this skill** (and add a row to § Common foot-guns if it's scannable)
- Tied to a specific project's state, ticket, or stakeholder decision → **project memory** (`~/.claude/projects/-home-nyaptor-dev-<project>/memory/`)
- Both halves apply → **both**, with the project memory linking back to the relevant skill section

The skill is the canonical operational reference; memories are the project-scoped historical layer. Drifting them apart causes the same gotcha to be relearned in three places.

## Mental model

Brown & Brown's tenant enforces Conditional Access by IP — direct calls from a Linux box fail with `AADSTS50158` or are silently denied. Everything routes through **cloudpc**, a Windows Conditional-Access bastion:

```
Linux workstation
  │
  ├─ SOCKS5 localhost:1080 → cloudpc        (every Azure / Graph / SNOW / ADO HTTPS call — the PRIMARY transport)
  │
  └─ SSH → cloudpc                          (fallback only: az-devops-extension PS proxy, cloudpc files, Edge CDP for SNOW)
```

**You don't start this tunnel by hand.** Starting a b-and-b workspace session — `mux ws` / `ws-claude ws` / `wsenv --activate ws` — auto-ensures it: activation starts `cloudpc-tunnel.service` if `:1080` is down (wired into the generated b-and-b `env.sh`, 2026-05-31). So begin work inside the b-and-b workspace and the tunnel is handled. If a B&B command still hangs ~30s, re-activate the workspace (fallback: `systemctl --user start cloudpc-tunnel.service`). The rare miss is activation in a context without a user session bus.

## Which identity for this call? (decision tree)

```
Does the URL contain graph.microsoft.com?
  │
  ├─ YES → does the call need Chat.Read (Teams chat content)?
  │         ├─ YES → ~/.graph-token.json     (O365 identity, has Chat.Read)
  │         └─ NO  → ~/.graph-bbadmin-token.json   (BBAdmin, has Application.ReadWrite.All etc.)
  │
  └─ NO  → does the resource live in an ALL-Wholesale-* (legacy) sub?
            ├─ YES → /home/nyaptor/.local/bin/az --as-o365 ...
            └─ NO  → /home/nyaptor/.local/bin/az --as-admin ... (default; WHS-346-* and all modern subs)
```

For deep-dive Graph mechanics (token refresh, capability cheat sheet, endpoint catalog, encoding gotchas), read `references/graph-endpoints.md` — see § Loading triggers below.

For deep-dive SNOW mechanics (cookie pattern, CDP, auto-refresh timer, ticket queries), read `references/snow-mechanics.md`.

For the WHS pipeline ID registry, queueing a deploy, APIM API onboarding (the no-wildcard rule), the shared wholesale App Insights trace pattern, and the Postman smoke harness, read `references/whs-pipelines.md` — see § Loading triggers below.

## Three identities, one wrapper

`~/.local/bin/az` is a smart wrapper that picks identity by flag and configures `AZURE_CONFIG_DIR` + proxy env vars accordingly:

| Flag | UPN | Config Dir | Proxy | Use For |
| --- | --- | --- | --- | --- |
| `--as-admin` (default) | `BBAdminLAcosta@bbins.com` | `~/.azure-bbadmin` | SOCKS5 via cloudpc | Azure ARM in WHS-346 subs, PIM, Bicep deploys |
| `--as-o365` | `leonardo.acosta@bridgespecialty.com` | `~/.azure-o365` | SOCKS5 via cloudpc | Microsoft Graph (Teams, Outlook, OneDrive), legacy `ALL-Wholesale-*` subs |
| `--as-personal` | personal Microsoft account | `~/.azure-civalent` | direct | Personal Azure, no proxy |

The wrapper auto-switches to `--as-o365` when the invocation targets `graph.microsoft.com`. BBAdmin is the default for everything else.

### MSAL token lifecycle

Each identity has its own MSAL cache at `AZURE_CONFIG_DIR/msal_token_cache.json`. Access tokens auto-refresh for ~90 days via stored refresh tokens. After that, device-code re-auth is required:

```bash
az --as-admin login       # BBAdmin
az --as-o365 login        # O365
az --as-personal login    # Personal
```

Run `scripts/az-audit` to check per-identity health, tunnel state, and cache age.

## Subscription topology (WHS-346 estate)

| Name | ID | Read identity |
| --- | --- | --- |
| WHS-346-Wholesale-DEV | `21b25913-29c0-40f8-8911-6fe519539060` | `--as-admin` |
| WHS-346-Wholesale-PROD | `b2a995ac-59dc-4cd3-bbe8-77a96b6377e3` | `--as-admin` |
| WHS-346-Wholesale-DMZ-DEV | `5a53500e-8ae0-4110-8047-13156cb8544b` | `--as-admin` |
| WHS-346-ModernDataPlatform-DEV | `f806a961-a554-4b3e-bbf9-247a112214b4` | `--as-admin` |
| WHS-346-ModernDataPlatform-TEST | `04f77af4-cc47-4b81-8b4a-55633b3ed16a` | `--as-admin` |
| WHS-346-ModernDataPlatform-PROD | `0f3176a4-fd40-43c3-9fa0-42a03ad425b7` | `--as-admin` |
| ALL-Wholesale-DEV (legacy) | `979366b2-fe6a-4ee9-a57d-5b280907c375` | `--as-o365` |
| ALL-Wholesale-PROD (legacy) | `0e4f65bb-4da4-49e3-905d-99abb404618d` | `--as-o365` |
| WHS-537-Decus-DEV | `0c3fe2bc-ccf2-4017-90f6-4ebd7df2c9f6` | `--as-admin` (PIM-activate first) |

The legacy `ALL-Wholesale-*` subs use O365 because BBAdmin lacks RBAC there — Leo's direct grant lives on the O365 identity.

**WHS-537-Decus-DEV (added 2026-06-20):** the Decus profit-center sub. BBAdmin now holds **PIM-eligible Contributor** here (DEV only) — it does NOT appear in `az --as-admin account list` until you `pim`-activate, after which `--as-admin` reads/deploys work (~30min RBAC propagation). Contributor lacks `roleAssignments/write` (can't grant scopes on 537) and confers no data-plane secret-get on the Decus KV. No test/stage/prod 537 eligibility. **Deploying** Decus bicep stays team-coordination-held regardless of access — see the `feedback_no_deploy_decus_bicep_without_team` memory.

## SPN permission ceiling (design constraint)

The pipeline service connection `SC-WHS-346-Wholesale-{ENV}` is **shared across all satellites** within an env (doc, decus, fireball, b3owa, costcenter, salescrm, b3admin, submissionengine, thebridge — one SPN per env, not per satellite). It has Contributor + CHS Owner on the sub but:

- **CAN create role assignments — fenced.** CHS Owner (custom role, `Actions:['*']`, NotActions only PolicyInsights attestations) DOES carry `Microsoft.Authorization/roleAssignments/write` + `/delete`. The assignment carries an ABAC v2.0 **constrain-roles** condition: a **deny-list** (`ForAnyOfAllValues:GuidNotEquals`) of exactly three escalation roles — **Owner** (`8e3af657-a8ff-443c-a75c-2fe8c4bcb635`), **User Access Administrator** (`18d7d88d-d35e-4fb5-a5c3-7773c20a72d9`), **Role Based Access Control Administrator** (`f58310d9-a9f6-439a-9e8d-f62e7b41a168`). So the SPN can grant **any job-function role** (Key Vault Secrets User, Storage Blob Data Contributor, Service Bus Data Owner, etc.) but **cannot grant Owner/UAA/RBAC-Admin** (no privilege escalation). Confirmed 2026-06-05 against the live condition on SPN `827e8919-…` at sub `21b25913`. The OLD claim "no roleAssignments/write" was WRONG — and "can't grant job-function roles" is ALSO wrong; the real gate is the 3-role escalation fence. Used live by `reconcile-uami-rbac.yml`, `enableRbacAssignments` Bicep, and the `DevAccessGrants` stage (`.azuredevops/access/` in ws).
- **No** Graph permissions — cannot read Entra, cannot manage app registrations, cannot add to AAD groups. (So an in-pipeline `az role assignment create` MUST use `--assignee-object-id … --assignee-principal-type User`; a bare `--assignee <UPN>` fails — no directory lookup.)
- **No** `Microsoft.Web/sites/publishxml/action` on some RGs.
- **PROD compliance deny-assignments block `listSecrets` even with PIM Owner/Contributor.** A freshly-activated PIM Contributor on `ALL-Wholesale-PROD` APIM KV still returned 403 on `listSecrets` — a **compliance deny-assignment at the resource scope** blocks secret enumeration for non-admins, and neither the SPN nor a PIM-elevated BBAdmin can escalate past it (no `roleAssignments/write` on PROD). Confirmed 2026-06. Route through the platform team for a secret-read role, or use a separate canonical KV without the compliance block — **don't keep retrying, the deny is intentional, not a propagation lag**.

Implications for design:

- Job-function role assignments (KV/Storage/SB data roles, etc.) **can** be deployed by the shared SPN in-pipeline — no BBAdmin/PIM needed. Only **Owner / User Access Administrator / RBAC Administrator** grants require BBAdmin-manual + PIM.
- A plan that needs the SPN to grant *grant-management* rights (UAA/RBAC-Admin) or Ownership must route through BBAdmin, not the SPN.
- Per-satellite RBAC isolation can use ABAC scope conditions on these job-function grants (the fence only blocks escalation roles, not scoping).

BBAdmin Leo himself also lacks `roleAssignments/write` without PIM elevation. PIM bulk activation via `pim --all` works for direct role assignments; group-based PIM assignments (the majority for BBAdmin) require portal activation.

- **Privileged Graph-permission consent is a permanent wall for BBAdmin/O365.** Granting a deploy SPN `Application.ReadWrite.OwnedBy` (or any *privileged* Graph app permission) needs Global Administrator / Privileged Role Administrator — `AppRoleAssignment.ReadWrite.All` + Cloud Application Administrator are NOT enough (403 `Authorization_RequestDenied`, confirmed 2026-06-24). So "let the pipeline SPN manage app registrations via Bicep / the Graph extension (Pattern A)" is **blocked** without an admin escalation. Workaround: manage app-reg redirect URIs out-of-band, authenticated as BBAdmin/O365 (which holds `Application.ReadWrite.All`) — see `ws/apps/appreg-callbacks`.

## Entra External-ID (CIAM) tenants — NOT ws-ownable

The customer-facing login tenants are **Entra External ID (CIAM)** tenants named **InsurePass** — `InsurePass DEV` / `InsurePass TEST` / `InsurePass (prod)`, fronted at `dev.insurepass.com` / `test.insurepass.com` / `login.insurepass.com`. These are a **separate Entra directory**, NOT a resource in any WHS-346 subscription:

- A CIAM tenant has **no ARM resource** — it is not in B&B's MTO, not in any `RG-WHS-346-*`, and **cannot be created, deployed, or modified by Bicep / the pipeline SPN / BBAdmin**. Provisioning (new env tenant, redirect-URI adds, app-reg edits inside it) is owned by the **TheBridge / InsurePass product team**. When a CIAM-tenant change is needed, escalate to that team — do not engineer around it.
- `025ExternalIdentity*` (e.g. `025ExternalIdentityTEST.ciamlogin.com`) is **only the ciamlogin LOGIN SUBDOMAIN** of a CIAM tenant — not the tenant name, not a B&B-managed app. `025` is B&B's **cost-center code for The Bridge (Bridge Summit)**.
- **Probe whether a CIAM tenant exists** via the OIDC discovery endpoint (no auth needed): `curl -s https://login.microsoftonline.com/<domain>.onmicrosoft.com/v2.0/.well-known/openid-configuration` (or `/<tenantId>/...`). A live tenant returns the OIDC document; an absent tenant returns **`AADSTS90002`** ("Tenant ... not found"). Use this before assuming an env tenant exists — e.g. there is **no `InsurePass (STAGE)` / `stage.insurepass.com` tenant**, so TheBridge STAGE runs on the **INTERIM TEST CIAM tenant** (`InsurePass TEST`, tenantId `36fe6b1b…`, app/clientId `39333fde…`, `025ExternalIdentityTEST.ciamlogin.com`) until the product team provisions a real stage tenant; login from stage still needs that team to add the stage redirect URI on test app-reg `39333fde…`. Confirmed 2026-06-30.

## Azure Resource Graph for fast cross-sub queries

Prefer Resource Graph over per-sub `az resource list` for enumeration:

```bash
/home/nyaptor/.local/bin/az --as-admin graph query -q "
  Resources
  | where subscriptionId in ('21b25913-29c0-40f8-8911-6fe519539060', '5a53500e-8ae0-4110-8047-13156cb8544b')
  | where type =~ 'microsoft.network/virtualnetworks'
  | project name, resourceGroup, location, properties.addressSpace.addressPrefixes
" -o json
```

`lastModifiedDateTime` lives on `properties.modifiedDateTime` for most resource types, projectable as `changedDate=tostring(todatetime(properties.modifiedDateTime))`. Not universal — verify per type.

## ADO access — two patterns, pick by use case

Two viable paths from Linux. **Prefer the Linux-native REST pattern** (faster, no cloudpc round-trip) for anything `scripts/ado`'s canned vocabulary doesn't cover. Fall back to the PS proxy only when the work genuinely needs `az devops` extension or cloudpc filesystem access.

### Pattern A — Linux-native REST (preferred for ad-hoc queries)

Confirmed 2026-05-22: the local wrapper holds a BBAdmin federated session strong enough to mint an ADO-audience token directly. No cloudpc round-trip needed for read-only / preview / build-API calls.

```bash
# Mint ADO token via BBAdmin federated session through SOCKS5.
# NOTE: the zsh `az ()` function shim in shared.zsh was REMOVED 2026-05-31, so
# bare `az --as-admin ...` now works. The absolute path is still safest in scripts
# (immune to any stale shell function or workspace PATH shadowing).
TOK=$(/home/nyaptor/.local/bin/az --as-admin account get-access-token \
  --resource 499b84ac-1321-427f-aa17-267ca6975798 \
  --query accessToken -o tsv)

# Read any ADO REST endpoint with that token, via the SOCKS5 tunnel.
curl -s \
  --proxy socks5h://127.0.0.1:1080 \
  -H "Authorization: Bearer $TOK" \
  "https://dev.azure.com/brownandbrowninc/Wholesale%20Architecture/_apis/build/builds/52439?api-version=7.0" \
  | jq

# Pre-push YAML validation via the pipelines preview API. Submits your LOCAL
# yaml as if it were committed; returns finalYaml on success or
# PipelineValidationException with exact line/col on syntax error.
# CAVEAT (Confirmed 2026-06-04): yamlOverride only overrides the MAIN yaml — referenced
# templates are still pulled from the COMMITTED branch. A new param passed to a locally-edited
# template fails preview with "Unexpected parameter 'X'". Commit+push the template, then preview.
YAML=$(cat .azuredevops/build/satellites/wholesale.yml)
BODY=$(jq -nc --arg y "$YAML" \
  '{previewRun:true,yamlOverride:$y,resources:{repositories:{self:{refName:"refs/heads/dev"}}}}')
curl -s --proxy socks5h://127.0.0.1:1080 -H "Authorization: Bearer $TOK" \
  -H "Content-Type: application/json" -X POST \
  "https://dev.azure.com/brownandbrowninc/Wholesale%20Architecture/_apis/pipelines/449/preview?api-version=7.0-preview.1" \
  -d "$BODY"
```

Use this for: `preview`, `build/builds/{id}` + `/timeline` + `/logs`, custom build queries, `pipelines/{id}/runs` with filters, `git/repositories/...`, `core/projects/...`, anything not covered by `scripts/ado` shortcuts.

**Polling a build to completion**: never `jq` the raw `build/builds/{id}` JSON inside a loop gate — it embeds control chars (commit message, `triggerInfo`, log URLs) that make `jq` error; a `2>/dev/null` then yields `""` and the loop runs to its full cap (Confirmed 2026-06-04). Grep the scalar instead: `bstatus=$(printf '%s' "$B" | grep -o '"status":"[^"]*"' | head -1 | cut -d'"' -f4)`, or `python json.loads(strict=False)` for structured fields.

### Pattern B — `scripts/ado` (SOCKS-first since 2026-07-06; SSH PS proxy only where required)

**Transport priority inside the script**: `cmd_token` mints the ADO token via the LOCAL az
wrapper (BBAdmin federated session through SOCKS) FIRST and falls back to SSH only if the
local mint fails; SSH is opened lazily, never eagerly at dispatch. Net effect: `queue`,
`watch`, `token`, `registry`, and `--dry-run` are fully SOCKS-native and survive a cloudpc
sshd outage (proven live 2026-07-06, run 56667 queued+watched while `ssh cloudpc` was dead).
Only the PS-proxy shortcuts still SCP a PowerShell script to cloudpc and run `az devops`
there (the extension lives on cloudpc): `projects`/`repos`/`pipelines`/`runs`/`prs`/`wi`/`raw`
pass-through. `test` now grades the SOCKS path as primary and reports SSH as a
warn-not-fail fallback:

```bash
~/dev/ws/scripts/ado test                 # SOCKS tunnel + local mint + REST (primary); SSH graded as fallback
~/dev/ws/scripts/ado projects             # list all 44 projects
~/dev/ws/scripts/ado repos "Wholesale Architecture"
~/dev/ws/scripts/ado pipelines Fireball
~/dev/ws/scripts/ado runs Fireball        # last 10 pipeline runs
~/dev/ws/scripts/ado prs Fireball         # open PRs
~/dev/ws/scripts/ado wi PIPS              # work items
~/dev/ws/scripts/ado token                # raw AAD token
~/dev/ws/scripts/ado registry            # 14 project codes -> pipeline-id table (alias: codes)
~/dev/ws/scripts/ado queue fb --branch dev          # queue by code (REST + auto env-param)
~/dev/ws/scripts/ado queue fb --branch dev --watch  # + stream the timeline as pino NDJSON
~/dev/ws/scripts/ado queue dp --branch test --dry-run  # resolve only (no queue)
~/dev/ws/scripts/ado watch <runId> --pipe fb --env dev # monitor a running build (pino)
~/dev/ws/scripts/ado <az-devops-args>     # pass-through
```

`queue`/`watch` use the local REST runs API + a pino timeline stream (not the PS proxy); the
queue auto-injects the consolidated yamls' REQUIRED `env` template param (a SCALAR string,
so it sticks — unlike object-typed template params, which the runs API silently drops). See
`references/whs-pipelines.md` § Queueing a pipeline for codes, the corrected manual fallback,
and the NDJSON shape.

> **Two `ba`/`bo` vocabularies differ — don't conflate them.** In the *skill description's fleet-code* trigger keywords, `ba` = the B3 pair (b3admin + b3owa) and `bo` = Office Index PIPS. In the `ado queue <code>` *pipeline* registry they SPLIT: `ba` = b3admin only, `bo` = b3owa (see `references/whs-pipelines.md` § Pipeline ID registry). When queueing a pipeline, use the registry meaning.

**Foot-gun**: `scripts/ado`'s case statement intercepts `projects | repos | pipelines | runs | queue | watch | registry | codes | prs | wi | token | test` as shortcuts. Anything outside that vocabulary falls through to `cmd_raw` which prefixes `az` — so `scripts/ado pipelines runs list` fails because `pipelines` matches the shortcut and `runs` gets reparsed as a project name. When the canned shortcut doesn't fit, switch to Pattern A.

Org name in URLs is **`brownandbrowninc`** (not `brownandbrown`). SSH ControlMaster reuses a single connection across calls.

### When you still need the cloudpc PS-file pattern

Pattern A handles all read-only REST. Two cases still warrant a SCP+PowerShell round-trip:

1. **`az devops` extension subcommands** (`az devops project list`, `az pipelines variable-group ...`) — the extension lives on cloudpc, not always installed on Linux. Use `scripts/ado` or hand-rolled SCP.
2. **Filesystem ops on cloudpc** (reading `.ps1` files under `C:\Users\leo.346-CPC-QJXVZ`, downloading attachments, SNOW Edge profile interactions).

## cloudpc as the broker

cloudpc routes three concerns:

1. **SOCKS5 tunnel** on `localhost:1080` (systemd `cloudpc-tunnel.service`) — every Azure/Graph/SNOW HTTPS call goes through it. **Started automatically by b-and-b workspace activation** (see § Mental model); you don't manage it manually.
2. **SSH** via `~/.ssh/config` alias `cloudpc` — used for ADO proxy, PowerShell on cloudpc, Edge CDP for SNOW cookies
3. **Edge profiles on cloudpc**:
   - `edge-snow-cdp` holds the SAML session for SNOW
   - Default profile holds Leo's daily Microsoft 365 session (the "o365 profile"). **In this profile, only ever select the o365 account (`leonardo.acosta@bridgespecialty.com`) at any AAD account-picker — never BBAdmin or personal.** SSC + other o365-SAML apps federate via `whr=bridgespecialty.com`. (Leo directive, 2026-06-20.)

### Launching / finding Edge on cloudpc

**You cannot drive an authenticated, visible browser from the SSH session.** `ssh cloudpc` runs as the service account `346-cpc-qjxvz\leo` in **session 0** (no interactive desktop); Leo's real desktop is `AzureAD\LeonardoAcosta` in an RDP session. Three consequences (all confirmed 2026-06-20):

- A normal `msedge.exe` started from SSH **exits immediately** — no desktop to attach to. Only `--headless=new --no-sandbox --disable-gpu` runs in session 0 at all (plain headless without those flags also dies).
- Even headless, **DPAPI is per-user/per-session**: from the service account Edge can't decrypt `LeonardoAcosta`'s stored o365 tokens (`Failed to encrypt: Access is denied. (0x5)`), so any M365/SSC page redirects to AAD sign-in. **There is no SSH/session-0 path to an authenticated page** — don't burn time on `--user-data-dir` pointing at his profile; the tokens are unreadable cross-session.
- To open a **visible, signed-in** Edge: ensure `query session` shows an `Active` `LeonardoAcosta` row, then launch via an **interactive scheduled task** in that session — `Register-ScheduledTask -Action (New-ScheduledTaskAction -Execute msedge.exe -Argument '--profile-directory="Default" "<url>"') -Principal (New-ScheduledTaskPrincipal -UserId LeonardoAcosta -LogonType Interactive)`. If nobody is logged on, use an `-AtLogOn` trigger (self-deleting for one-shot) so it opens on his next sign-in. His Default profile then carries valid SSO — no login prompt.

**Finding the active Edge session** (the remote default shell is **cmd.exe**, so wrap multi-statement work in `powershell -NoProfile -Command "..."`; note `\x27` single-quote escaping does NOT work inside a PowerShell `-Filter` string — SCP a `.ps1` or use a here-string for anything with embedded quotes):

```powershell
query session                                              # Active LeonardoAcosta row + session id
Get-Process msedge -IncludeUserName |                      # all Edge procs w/ owner + session
  Select-Object Id,SessionId,UserName,StartTime
Get-NetTCPConnection -State Listen |                       # open CDP / remote-debugging ports
  Where-Object { $_.LocalPort -ge 9000 -and $_.LocalPort -le 9999 } |
  Select-Object LocalPort,OwningProcess
```

A live interactive Edge shows ~15-20 `msedge.exe` children under `AzureAD\LeonardoAcosta` in the RDP session id; a listening `127.0.0.1:9xxx` is an Edge instance with remote-debugging on that you can attach to via CDP.

### Two distinct cloudpc user profiles (major gotcha)

| Profile | Home dir | Used for |
| --- | --- | --- |
| **SSH service account** | `C:\Users\leo.346-CPC-QJXVZ` | Every `ssh cloudpc ...` command and PowerShell script. Has admin rights (DISM, schtasks work). |
| **Interactive RDP account** | `C:\Users\LeonardoAcosta` | What Leo sees when he RDPs in. Holds his Edge profiles (default + edge-snow-cdp), `.nuget`, `source`, `.vscode`, Teams cache, daily browsing state. |

A `dotnet nuget locals all --clear` from the SSH session clears `leo.346-CPC-QJXVZ\.nuget`, **NOT** `LeonardoAcosta\.nuget`. To clean the interactive profile, either hardcode the path in the script (admin override works) or RDP as LeonardoAcosta.

"cloudpc memory is full" almost always means **disk full on C:**, not RAM. Common cause: `LeonardoAcosta\.nuget\packages` (~20 GB) and `LeonardoAcosta\AppData\Local\pnpm` (~4 GB).

## Toolchain bootstrap (when a script/token is missing)

If a smoke-test step below fails because the tool/token isn't there, this is where each one lives and how to (re)establish it. Verify, don't assume.

| Tool | Lives at | Verify / (re)create | If absent |
| --- | --- | --- | --- |
| **SOCKS tunnel** | systemd `--user` unit `cloudpc-tunnel.service` (`ExecStart=ssh -D 1080 -N … cloudpc`) | `ss -tlnp \| grep 1080`; (re)start `systemctl --user start cloudpc-tunnel.service` — but b-and-b workspace activation auto-ensures it (`wsenv --activate ws`) | Re-activate the workspace; if the unit itself is gone it ships from `~/dev/personal/installfest` dotfiles (`~/.config/systemd/user/cloudpc-tunnel.service`) |
| **`scripts/ado`** | `~/dev/ws/scripts/ado` (SOCKS-first: local BBAdmin token mint + ADO REST via :1080, resource `499b84ac-…`; SSH PS proxy only for the extension shortcuts) | `~/dev/ws/scripts/ado test` (grades SOCKS primary; SSH reported warn-only) | Committed in the ws repo — `git -C ~/dev/ws status scripts/ado`; primary path needs a live local `az --as-admin` session + the tunnel; only PS-proxy shortcuts need the SSH alias + cloudpc `az login` |
| **Graph token (BBAdmin)** | `~/.graph-bbadmin-token.json` (O365 sibling: `~/.graph-token.json`) | refreshed by `~/dev/ws/scripts/graph-token-refresh` + `graph-token-refresh.timer` (systemd `--user`); manual: `scripts/graph-token-refresh` (BBAdmin) / `scripts/graph-token-refresh --file ~/.graph-token.json --client <id>` (O365) | File is seeded once from a cloudpc `Connect-MgGraph` mint; if no `refresh_token` in the JSON, re-mint on cloudpc (see `references/graph-endpoints.md`) |
| **`postman-smoke`** | `~/dev/ws/scripts/bin/postman-smoke` (newman over the 221-request wholesale collection) | `postman-smoke <dev\|test\|stage>` — see `postman-smoke --help` | Committed in ws; needs `newman` + the env files (see `references/whs-pipelines.md` § Postman per-env smoke) |
| **`scripts/az-audit`** | `~/dev/ws/scripts/az-audit` (per-identity ARM health, tunnel state, cache age) | `scripts/az-audit` (exit 0 = all green) — see its header comment | Committed in ws; it only reads, nothing to create |
| **`scripts/snow-refresh`** | `~/dev/ws/scripts/snow-refresh` (CDP cookie refresh → `/tmp/snow-cookies.txt`) + `snow-cookie-refresh.timer` | `snow-refresh test` (end-to-end) — see `snow-refresh --help` | First-run needs a one-time interactive SAML sign-in on cloudpc (`snow-refresh start`); full mechanics in `references/snow-mechanics.md` |

## Quick smoke test (verify all toolchains work)

Run before any session that depends on the B&B operational layer:

```bash
# 1. SOCKS tunnel (auto-ensured by `wsenv --activate ws`; this only confirms)
ss -tlnp | grep 1080 || echo "tunnel DOWN — re-activate workspace: wsenv --activate ws"

# 2. Azure (BBAdmin)
/home/nyaptor/.local/bin/az --as-admin account show --query "{name,id}" -o table

# 3. Graph (BBAdmin token)
TOKEN=$(jq -r .access_token ~/.graph-bbadmin-token.json) && \
  curl -s --socks5-hostname localhost:1080 -H "Authorization: Bearer $TOKEN" \
    "https://graph.microsoft.com/v1.0/me?\$select=displayName,userPrincipalName" | jq

# 4. SNOW cookies
test -s /tmp/snow-cookies.txt && echo "snow cookies present" || echo "STALE — wait for timer or run snow-refresh"

# 5. ADO — Linux-native REST (Pattern A; preferred)
TOK=$(/home/nyaptor/.local/bin/az --as-admin account get-access-token \
  --resource 499b84ac-1321-427f-aa17-267ca6975798 --query accessToken -o tsv) && \
curl -s --proxy socks5h://127.0.0.1:1080 -H "Authorization: Bearer $TOK" \
  "https://dev.azure.com/brownandbrowninc/_apis/projects?api-version=7.0&\$top=1" | jq '.value[0].name'

# 6. ADO proxy (Pattern B; only if `az devops` extension needed)
~/dev/ws/scripts/ado test
```

Any red on this checklist before B&B-dependent work blocks the session.

## Loading triggers (sub-files)

### Working with Microsoft Graph (Teams, Outlook, app registrations, directory roles)

**MANDATORY**: Before drafting any Graph API call, read `references/graph-endpoints.md` completely. It covers token refresh, BBAdmin scope cheat sheet, the full endpoint catalog, OData encoding gotchas, role-holder snapshots, and the canonical app-registration-permission-audit pattern. **Do NOT** load `references/snow-mechanics.md` for Graph work.

### Investigating a ServiceNow ticket or troubleshooting SNOW cookies

**MANDATORY**: Read `references/snow-mechanics.md` for the cookie-reuse pattern, CDP+Edge profile detail, systemd timer mechanics, variable pool extraction, and common cookie failure modes. **Do NOT** load `references/graph-endpoints.md` for SNOW work.

### Queueing/finding a WHS pipeline, onboarding an API into APIM, or tracing wholesale telemetry

**MANDATORY**: Read `references/whs-pipelines.md` BEFORE re-discovering pipeline IDs or rebuilding a trace query. It carries the Wholesale Architecture pipeline-id registry (satellite × env), the REST queue pattern + the never-hand-queue-449 rule, the APIM API-onboarding model (pipeline OpenAPI import vs bicep-enumerated vs the decus exception) and the no-wildcard rule, the shared wholesale App Insights AppId map + the `operation_Id` backend-attribution join, and the `postman-smoke` per-env harness. **Do NOT** load `references/graph-endpoints.md` or `references/snow-mechanics.md` for this work.

### Diagnosing a Function/App 503 or 500.30 boot crash, an identity-based storage host crash-loop, an Entra SQL contained-user error, or a Key Vault RBAC assignment

**MANDATORY**: Read `references/boot-crash-and-rbac.md` for the boot-crash status-shape triage (503-vs-500-vs-404), the config-source/MI/KV root-cause list, the runtime/framework boot-crash table, the identity-based `AzureWebJobsStorage` Blob+Queue+Table role requirement, the Entra (UAMI) SQL contained-user traps, and the Key Vault RBAC assign/stale-cache/manual-GUID gotchas. **Do NOT** load `references/graph-endpoints.md` or `references/snow-mechanics.md` for this work.

### Building a fleet/infra diagram or needing a cloud service icon

**MANDATORY**: Read `references/diagram-assets.md` for the `azure-topology-style` sync, the theSVG real-mark slug table (resolve slugs from the registry, never guess), the phosphor fallbacks for Alloy/Tempo/Pyroscope, and the `bb-base.js` orthogonal-wire-routing gotcha. **Do NOT** load `references/graph-endpoints.md` or `references/snow-mechanics.md` for diagram work.

### Diagnosing a WHS-346 VNet / subnet / private-endpoint / DNS / NSG reachability failure (before escalating to the network team)

**MANDATORY**: Read `references/network-diagnostics.md` BEFORE concluding "subnet/endpoint unreachable at the SDN layer" — most "fabric defect" verdicts are a one-rule NSG fix. It carries the silent-NSG-deny signature, the PE effective-route-table non-queryability, the NXDOMAIN-is-DNS-not-RBAC rule, the custom-DNS-disables-built-in resolver trap, `vnetRouteAllEnabled`, the Azure SQL Redirect-port DATA-NSG range, the per-host Hull AD-zone Private DNS fix, the legacy FB App Insights payload-mining technique, the enumerate-each-CIDR outbound-NSG rule, the non-transitive-peering SYN-timeout tell, and the Network-RG write-guard. **Do NOT** load `references/graph-endpoints.md` or `references/snow-mechanics.md` for this work.

### A § Common foot-guns row marked → deep-dive needs its full mechanism / incident narrative / GUID

Read `references/foot-gun-deep-dives.md` when the one-line table essence isn't enough — it carries the full mechanism, exact repro, incident narrative, and every GUID/command/date behind each de-densified row (the `az graph` wrapper bypass, `stagesToSkip`, the stale-VG-membership repro, the dual-503 `RUN_FROM_PACKAGE` directions, the Cloudflare 522/525/526 split, the privileged-Graph-perm 403, etc.). Load only the row you need; **Do NOT** load `references/graph-endpoints.md` or `references/snow-mechanics.md` for this.

### Working only on Azure ARM (Bicep deploys, az calls, RBAC) — no pipeline/APIM/telemetry angle

Stay in this file. The decision tree + identity wrapper section + smoke test cover the path. Only load sub-files if the work expands into Graph, SNOW, or WHS pipelines/APIM/telemetry.

## Related skills

- `bb-fortify` — B&B Fortify SAST/DAST (hosted SSC + ScanCentral): the `fortify-scan-stage` template, `FortifyVariables`, SSC + DAST APIs, CI/CD tokens, `applicationVersion`, scan provisioning. Load for any WHS-346 Fortify/security-scan work (this skill provides the cloudpc tunnel its API calls ride on)
- `leo-writing-voice` — SNOW ticket bodies, Teams replies on Leo's behalf (load explicitly when drafting)
- `azure-topology-style` — diagrams of B&B infrastructure
- `azure-devops-cli` — generic ADO CLI patterns (this skill covers the B&B-specific proxy on top)
- `azure-compute`, `azure-storage`, `azure-messaging`, `azure-observability` — generic Azure patterns; this skill is the B&B overlay

## Update protocol

When a B&B-specific gotcha is discovered (a new SPN limit, a new sub, a working pattern for something that previously didn't work):

1. Update the relevant section of this skill or a reference file
2. If it's a gotcha worth scanning quickly, add a row to § Common foot-guns
3. Cite the date the finding was confirmed inline (e.g. "Confirmed 2026-05-21")
4. If the finding is broader than the operational layer (it changes how a satellite is structured, or it's a policy decision), also reflect it in the relevant `~/dev/ws/CLAUDE.md` section or project memory

This skill is the canonical operational reference. Project-level CLAUDE.md files should reference this skill rather than re-encode its contents.
