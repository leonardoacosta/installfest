# Boot-crash triage, identity-based storage, Entra SQL & Key Vault RBAC

> Load this file when diagnosing a Function/App 503 or 500.30 boot crash, an identity-based `AzureWebJobsStorage` host crash-loop, an Entra (UAMI) SQL contained-user error, or a Key Vault RBAC role assignment. Skip when working on Graph, SNOW, or WHS pipelines/APIM/telemetry — those have their own references.

## Function/App boot-crash triage (503 / 500.30 at startup)

A `503 "Function host is not running"` or HTTP `500.30` at startup means the host crashed in `HostBuilder.Build()` during **DI init** — almost always a **config-source access failure**, NOT a network/APIM/routing fault. **An APIM 503 on a wholesale route is usually a downstream symptom of a dead backend host**, not an APIM problem. Recurred across SE, TheBridge, b3OWA, inscipher-proxy (the single highest-frequency lesson in the corpus — 12 sessions).

**Distinguish the failure class by status shape:**

| Symptom | Meaning |
| --- | --- |
| `503` on EVERY route (incl. `/health`) | Worker never started — host crashed at boot |
| `500` WITH a stack trace | Host is up; a runtime exception in the request path |
| `404` on one route | Host up, that route/operation not wired (APIM or app routing) |

**Common boot-crash root causes (all surface as 503 with no App Insights logs — the crash precedes AI init):**

- System/User MI lacks **Key Vault Secrets User** on a vault the app reads at boot (eager `AddAzureKeyVault()` before AI init) → KV `403 AuthorizationFailure`.
- A KV secret (e.g. `AzureExtBlobAccountName`) points at a **legacy storage account** the modern MI has zero roles on → `BlobContainerClient.CreateIfNotExists()` 403 → crash-loop.
- Identity-based `AzureWebJobsStorage` missing `__credential=managedidentity`, or missing the full Blob+Queue+Table host role set (see § Identity-based AzureWebJobsStorage below).
- `enableRbacAssignments` defaulted `false` on the satellite, so the system MI that resolves `@Microsoft.KeyVault(...)` refs has no grant (manifests as `500.30` / missing OTel InstrumentationKey at boot; fix = pass `dev||test`).

**Runtime/framework boot crashes** (same 503/500.30 shape, different root):

| Symptom | Cause | Fix |
| --- | --- | --- |
| `.NET 10` Windows App Service 503s every request despite Bicep accepting `netFrameworkVersion: 'v10.0'` | .NET 10 is **not stable on Windows App Service** — the plan accepts the value but the runtime won't load | Stay on a supported runtime; verify it loads in the Kudu eventlog, don't trust that Bicep accepted it |
| Function app 503 "host not running" after a fresh publish | A **linux-x64 self-contained publish on a Windows func app** | Pin `--runtime win-x64` to match the host OS |
| `ANCM HTTP Error 500.32 — Failed to load dll` | A **64-bit self-contained DLL** in a **32-bit worker** | `use32BitWorkerProcess: false` (bitness must match) |
| Function host 503 on EVERY probe after adding a `/health` | **Duplicate `[Function("name")]`** across two files crashes the host on Windows CI | Rename so each `[Function]` name is unique |
| A func/app `/health`(`/ready`) probe reads **`000`** (pipeline gives up) but the worker IS up and serving other routes | The probe's `--max-time` (e.g. 15s) is **SHORTER than the health-check's own duration** — a slow dependency check runs its full ~60s SqlClient timeout, so the probe times out mid-check. `000` ≠ "no route"; it's the probe quitting early. Confirmed ws-9j4sv 2026-06-20. | **Read the rich body.** ASP.NET `HealthCheckService` returns per-check JSON (`entries{}` with `status`+`durationMs`+`error`). Hit `/api/health` (or `/ready`) from the func's **OWN Kudu** (`/api/command`, in-VNet, `curl -m 90+`) — one `Unhealthy` entry at ~`60000ms` names the hanging dependency outright. **Use the PUBLIC hostname, not `127.0.0.1`** — Kudu is a separate container from the worker, so localhost gives an instant `000` (refused). |
| One `AddDbContextCheck` entry `Unhealthy` at ~`60000ms` while a sibling check (**same SQL server, same UAMI**) is `Healthy` in `<100ms` | The func's UAMI is `CREATE USER`'d into DB-A but **NOT DB-B** — AAD auth reaches the server fine, but the principal has no user mapping in DB-B and `CanConnectAsync` **hangs the full connect timeout (~60s)** rather than fast-failing. The MDR/lookups DB is the classic offender (underseeded with satellite UAMIs). Confirmed ws-9j4sv 2026-06-20. | `CREATE USER [<uami>] WITH SID = <clientId bytes>, TYPE = E` + grant role in DB-B (the `scripts/provision-{es,mdr}-sql-users.ps1` pattern). **Diagnose:** `SELECT name FROM sys.database_principals WHERE type IN ('E','X')` in each DB and diff vs a working env — the missing UAMI is the answer. |

**Inspection path:** no App Insights startup logs exist (crash is pre-AI). Read the Kudu `eventlog.xml` / platform startup logs — a KV/Storage `403` there is the answer. If the worker IS up but a `/health`/`/ready` route hangs (not a pre-AI crash), the **per-check JSON body** is the fastest diagnosis — curl it from the func's own Kudu (PUBLIC hostname, long `--max-time`) and read which dependency is `Unhealthy`. **Do NOT escalate to the network team until the RBAC/secret-target path is ruled out — a 403 is auth, a `~60s` connect hang is a missing DB user OR network** (see § Network-reachability diagnostics in SKILL.md).

## Identity-based AzureWebJobsStorage needs Blob + Queue + Table

Dotnet-isolated function apps with **identity-based** `AzureWebJobsStorage` (`AzureWebJobsStorage__credential=managedidentity`, no key) need the **FULL** storage host role set on the host storage account — **Storage Blob Data Owner/Contributor, Storage Queue Data Contributor, AND Storage Table Data Contributor** — even if the app only uses queues (or no storage) operationally. The Functions host's **singleton-lock check uses Table**, so a missing Table role fails it and the host **crash-loops on cold start** (503). Confirmed FB + SE on `stwhs346wholesaletest` (SE-test had 15/16 routes down until the Table role landed).

- **Cached-token masking:** sibling apps that booted earlier coast on a cached MI token and stay green, so the failure looks app-specific when it's a shared-account grant gap. Differential symptoms across siblings on the same storage account = suspect the role set, not the app.
- **Don't toggle `allowSharedKeyAccess: false` before the MI grants land** — it breaks every app on that account at once.
- **Grant durably via satellite IaC** (`enableRbacAssignments: true` → the Bicep job-function role assignments), NOT a manual PIM grant (lost on next redeploy). The shared SC SPN **can** create these job-function assignments (not escalation roles; see § SPN permission ceiling in SKILL.md).

## Azure SQL + Entra identity (contained users)

Seeding Entra (UAMI) access to canonical Azure SQL has three recurring traps:

- **`CREATE USER ... FROM EXTERNAL PROVIDER` fails `Msg 33134: Principal could not be resolved. Server identity is not configured`** unless the SQL **server's system-assigned MI** holds the Entra **Directory Reader** role (so the server can resolve the principal). Grant Directory Reader to the server MI first. (The canonical scripted fix derives the UAMI clientId → SID via a `CONVERT` chain + `sp_executesql` — see memory `SQL CREATE USER WITH SID`.)
- **`CREATE LOGIN ... FROM EXTERNAL PROVIDER` (in `master`) does NOT grant database access.** You still need a per-database `CREATE USER ... FROM EXTERNAL PROVIDER`; a login without a contained DB user 403s every query.
- **The bootstrap script must enumerate EVERY consuming UAMI** as a contained user in each DB it touches — a missing consumer's queries 403 silently (ES had zero Entra users mapped and every query failed until the missing consumers were added).

Note: the shared satellite SPN has **no Graph permissions** (§ SPN permission ceiling in SKILL.md), so it cannot add itself to the AAD DB-admin group — that grant routes through BBAdmin.

## Key Vault RBAC: who can assign, stale cache, and the manual-GUID collision

Three recurring KV-RBAC gotchas beyond the SPN ceiling (§ SPN permission ceiling in SKILL.md):

- **Only Owner or User Access Administrator can create KV RBAC role assignments.** Contributor and even **Key Vault Administrator cannot** assign roles (KV Admin is a data-plane role, not grant-management). For a non-PIM interactive account that can't get UAA, switch the vault to **access-policy mode** as the workaround.
- **The RBAC cache goes stale after a grant.** A KV `403 "Assignment: (not found)"` that **persists after** the role is confirmed present means the vault's RBAC cache hasn't picked it up — **delete + recreate the assignment**, or restart the app, to flush it (seen on B3AdminTool even with KV Administrator assigned).
- **A manually-created portal assignment collides with Bicep.** A hand-made portal grant has a **different GUID** than Bicep's deterministic `guid(...)`, so flipping `enableRbacAssignments: true` throws `RoleAssignmentExists` and breaks the deploy. Reconcile (delete the manual one) before enabling the Bicep path.
- **Decoding a KV 403:** the `appid` in the error body is your **sign-in client application** (e.g. Visual Studio's client id), NOT the service principal — don't chase the SP when the 403 names a tooling client id.
- **A targeted single-RG satellite `main.bicep` deploy run interactively under BBAdmin FAILS on the `Microsoft.Authorization/roleAssignments/write` modules.** When you ad-hoc a satellite's `main.bicep` against one RG (to seed config / verify a setting) **as BBAdmin**, any `roleAssignments` module in that template throws `AuthorizationFailed` — BBAdmin lacks `roleAssignments/write` and **no PIM-activatable role grants it** (the grant-management roles — Owner / UAA / RBAC-Admin — are exactly the three fenced off everywhere; see § SPN permission ceiling in SKILL.md). Two workarounds: (1) deploy a **throwaway copy of the bicep with the role-assignment modules stripped** (seeds the non-RBAC resources you actually needed), or (2) **let the wholesale pipeline re-apply** — it runs as the **CHS-Owner SPN** (which CAN create job-function role assignments), so queue the env's wholesale deploy (e.g. 452 for stage) and it lands the RBAC the targeted run couldn't. Confirmed 2026-06-30 (TheBridge STAGE seed; ws stage wholesale run 55908 re-applied).
