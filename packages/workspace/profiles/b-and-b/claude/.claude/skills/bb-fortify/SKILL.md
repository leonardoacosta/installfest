---
name: bb-fortify
description: >-
  Brown & Brown Fortify (OpenText hosted SSC + ScanCentral) — SAST + DAST for the WHS-346 wholesale fleet.
  Triggers: Fortify, SAST, DAST, ScanCentral, WebInspect, SSC, fortifyhosted, fortify-scan-stage.yml, FortifyVariables,
  FortifyScanCentralSAST@7, FortifyScanCentralDAST@7, ssc.bbins.fortifyhosted.com, scdastapi.bbins.fortifyhosted.com,
  scsastctrl.bbins.fortifyhosted.com, scdastapi, scsastctrl, cicdToken, scanCentralCiCdToken, sccCIToken, dastCiCdToken,
  scanCentralDastApiUrl, applicationVersion, "security scan", "vulnerability scan", "app version v1/v2",
  WHS-346-Wholesale-APIM — incl. when only implied (a satellite security-scan stage, a 404/401 from a fortify endpoint).
  Layers on bb-azure-ops (load first for the cloudpc tunnel).
allowed-tools: Read, Glob, Grep, Bash
---

# Brown & Brown Fortify Operations

OpenText-hosted Fortify for the WHS-346 wholesale fleet: **SAST** (ScanCentral SAST → SSC) and
**DAST** (ScanCentral DAST → same SSC, correlated). All API calls route through the **cloudpc
SOCKS tunnel** (`--proxy socks5h://127.0.0.1:1080`) — load `bb-azure-ops` first; if a call hangs
~30s, the tunnel is down (re-activate the workspace).

## When This Skill Applies

Load whenever the work touches Fortify in ANY form — provisioning SSC applications or DAST scan-settings, minting/wiring CI/CD tokens, choosing an `applicationVersion`, or reading scan results — and even when Fortify is only *implied*: a satellite's security-scan stage, or a 404/401 from one of the three hosts below. This skill layers on `bb-azure-ops`; that skill provides the cloudpc SOCKS tunnel and identity wrapper every API call here rides on, so load it first.

## The three hosts (don't confuse them)

| Host | Role | API base | Auth |
| --- | --- | --- | --- |
| `ssc.bbins.fortifyhosted.com` | **SSC** — apps, versions, users, tokens, issues (SAST + DAST results land here) | `/api/v1` | `Authorization: FortifyToken <base64(token)>` |
| `scsastctrl.bbins.fortifyhosted.com/scancentral-ctrl` | **SAST controller** (`scanCentralCtrlUrl`) | — | client token (`scanCtrlToken`) |
| `scdastapi.bbins.fortifyhosted.com` | **ScanCentral DAST API** — scans, scan-settings, triggers | **`/api/v2`** | DAST token from `POST /api/v2/auth` (username/password), passed raw in `Authorization` |

★ The #1 foot-gun: **SSC and DAST have SEPARATE auth.** An SSC `FortifyToken` returns **401**
against the DAST API. The DAST API needs its own unified-login token minted via
`POST /api/v2/auth` with `{username,password}`. See `references/fortify-api.md`.

## The provisioned local CI user (`whs-ci`)

We provisioned a **local SSC user** `whs-ci` (NOT an Entra/SAML identity) for automation — the
single credential that drives both planes:

- **DAST API** — `POST /api/v2/auth {username:"whs-ci",password:…}` mints the DAST token (the
  `DAST_USER`/`DAST_PASS` pair in `references/fortify-api.md` § 2).
- **SSC web UI** — log in as a local user at **`https://ssc.bbins.fortifyhosted.com/login.jsp`**
  (the SAML-bypass path; `/` and `/html/ssc/*` auto-redirect to `…/saml2/authenticate/fortify_ssc`
  → Entra). The login SPA posts form fields **`#j_username` / `#j_password`** + a "Sign in" button.
  Confirmed 2026-06-29: `whs-ci` lands on `/html/ssc/dashboard/issue-stats` with **broad access**
  (Dashboard, Applications, ScanCentral SAST+DAST, Reports, Administration → Local Users / Token
  Management / Policies). Platform shows as **OpenText Application Security CE 25.4**.

Use this account for **headless/automated SSC UI work**: Playwright (or `curl`) via the cloudpc
SOCKS proxy reaches `login.jsp` and authenticates with **no MFA** — unlike the Entra path, which
has no SSH/session-0 browser route (see `bb-azure-ops` § Launching Edge on cloudpc). **Credential
storage:** the password is **operator-vault / ADO `FortifyVariables`-held**, deliberately NOT
inlined here (no plaintext secret in the cc repo); pull it at runtime, pass via env/file, never
argv. See `references/fortify-api.md` § 6.

## Common foot-guns (scan before any Fortify work)

| Symptom | Cause | Fix |
| --- | --- | --- |
| SSC API `401` with the raw token GUID | SSC wants the **base64** of the token, not the raw GUID | `Authorization: FortifyToken $(printf '%s' "$TOKEN" \| base64)` |
| DAST API `401` even with a valid SSC token | SSC tokens don't cross to the DAST API | Mint a DAST token: `POST /api/v2/auth {username,password}` → use `.token` raw in `Authorization` |
| DAST API `404` on `/scans/...` or `/api/v2/scans` | Wrong base path | DAST endpoints live under **`/api/v2`** (e.g. `/api/v2/scans/scan-summary-list`) |
| Pipeline DAST task `404` on `…/scans/start-scan-cicd` | `scanCentralDastApiUrl` set to the host root | It MUST include `/api/v2`: `https://scdastapi.bbins.fortifyhosted.com/api/v2` (the task appends `/scans/start-scan-cicd`) |
| DAST scan never produces findings even though the task "succeeds" | Scanning an unauthenticated SPA (UI behind Entra SSO) — only the login page is reached | UI DAST needs a login macro/creds per scan-setting; API/APIM scans can run unauthenticated or with a sub-key |
| **DAST scan `Interrupted` at exactly ~2 requests, duration 0m, 0 findings** (was the live wholesale state) | The scan-setting was a **Standard** scan of the **APIM gateway ROOT** with **no OpenAPI definition** + **no subscription key** → APIM 404s/401s the root, scanner has nothing to crawl, stops. Confirmed 2026-06-30: 77/77 `WHS-346-Wholesale-APIM`+`Fireball` scans were `Interrupted`/2-req/0-find; masked by the `continueOnError` stage. | **RESOLVED 2026-06-30** — see § Fixing a no-op API DAST scan below. Reconfigured scan-setting **107 in place** (preserves the pipeline's `dastCiCdToken`) to `scanType: API` + the 617-endpoint APIM OpenAPI + the APIM **master sub-key** as a header credential. Verified: scan 804 = API Scan, Running, **25,280 requests** (vs 2), 1 High + 1 Medium. |
| SAST uploads to the wrong/phantom `applicationVersion` | Template default `applicationVersion: v2`, but most SSC apps only have **v1** | Pass each app's REAL version — see § applicationVersion below |
| SAST upload fails `ErrorResponse: Failed to access application version: <App>-<ver>. Unable to find or access specified resource` (build `partiallySucceeded`) | Usually an **`applicationName` MISMATCH** vs the real SSC app name — case or spacing. The controller resolves the app by exact name; the SSC app may be cased/spaced differently than the pipeline passes. Confirmed 2026-06-20: pipeline `B3OWA` failed because the SSC app is **`B3owa`**; once corrected, it resolved. (The same run, SE/SalesCRM/B3Admin/CostCenter — exact-name apps — uploaded fine, ruling out a token-access gap.) | Look up the EXACT SSC app name (`/api/v1/projects?fields=id,name`) and set `applicationName` to match it character-for-character. See the canonical table in § applicationName + applicationVersion. |
| Cloudflare WAF throttles/blocks a DAST scan of `apic.<env>.bridgespecialty.com` | CF in front of APIM trips on scan traffic | Target the **raw `…azure-api.net`** APIM hostname, not the `apic.` CF host |

## Fixing a no-op API DAST scan (the WHS-346-Wholesale-APIM recipe)

How the wholesale APIM DAST was turned from a 2-request no-op into a real 25K-request API scan
(2026-06-30). All calls use the `whs-ci` DAST token (`POST /api/v2/auth`) — which **can write**
(create/PUT scan-settings, upload binary files). Steps:

1. **Get the OpenAPI** — `docs-site/public/openapi/wholesale-apim-openapi.json` (617 paths). **Resolve
   the templated server URL**: the spec's `servers[0].url` is `…wholesale-centralus-{env}.azure-api.net`
   — the `{env}` placeholder makes the derived `AllowedHosts` an **invalid URL** and the WebInspect
   conversion fails (`AllowedHostModel.InvalidUrl`). Rewrite `servers` to the concrete
   `https://apim-whs-346-wholesale-centralus-dev.azure-api.net` before upload. **This was THE bug.**
2. **Get the sub-key** — the APIM **master** subscription (scope = all APIs):
   `POST {mgmtArm}/service/APIM-WHS-346-Wholesale-CentralUS-DEV/subscriptions/master/listSecrets` →
   `.primaryKey`. (Most `/api/*` Fireball ops are `subscriptionRequired:false`, but the master key
   makes the whole gateway authenticate.)
3. **Upload the spec** — two calls: `POST /api/v2/application-version-binary-files/upload-session`
   with body `{applicationVersionId, fileName, fileExtension:".json" (LEADING DOT — bare "json" →
   errorCode 116), fileType:3 (OpenAPIDefinition), fileLength}` → `{id}`; then
   `POST …/upload?applicationVersionId=&sessionId=&offset=0` with the raw bytes → `{id: <binaryFileId>}`.
4. **Reconfigure the scan-setting IN PLACE** (`PUT /api/v2/application-version-scan-settings/107`)
   so it keeps the same `cicdToken` the pipeline fires (`b95eed54…` = `FortifyVariables.dastCiCdToken`):
   `scanType:3`, `scanSettings:{scanMode:2, apiDefinitionType:1 (OpenAPI), apiDefinitionBinaryFileId:<id>,
   policyId:"81a26872-2d4a-48eb-9352-2219b2da5d0f", apiScanConfigurationSettings:{useHeaderAuthenticationSettings:true,
   headerAuthenticationSettings:[{name:"Ocp-Apim-Subscription-Key", scheme:"", parameter:"<master key>"}],
   apiDefinitionVersionType:3}, enableSASTCorrelation:true, startUrls:["https://apim-…-dev.azure-api.net"]}`.
5. **Verify** — poll `GET …/scan-settings/107` until `webInspectSettingsStatusTypeDescription == "Available"`
   (NOT "Failed" — read `…/107/event-logs` for the conversion error if it fails). Then trigger
   `POST /api/v2/scans/start-scan-cicd {cicdToken, name}` and confirm the scan goes `Running` with
   `requestCount >> 2` and `Has API Auth Credentials: true` (scan 804 hit 25,280 requests, 1 High + 1 Med).

Maintenance: the uploaded OpenAPI is a point-in-time snapshot — refresh it (re-upload + PUT) when the
APIM surface changes.

★ **`submitForAudit: true` is REQUIRED or DAST findings never reach SSC.** A completed DAST scan
defaults to `publishStatusType: NotPublished` — its findings stay in ScanCentral DAST and never merge
into the SSC application version, so SAST/DAST correlation and any SSC-reading report show **zero
DAST**. The scan-setting's top-level **`submitForAudit`** flag is the auto-publish-to-SSC control;
create/PUT scan-settings with `submitForAudit: true`. Confirmed 2026-06-30: the per-app settings were
created `false`, so Fireball v2 showed only SCA(SAST) findings; the fix is `PUT …/scan-settings/<id>`
with the same `scanSettings` + `submitForAudit: true` (conversion stays Available, cicdToken
preserved). NOTE: `POST …/scans/<id>/set-publish-status-type {publishStatusType:2}` only flips the
STATUS FLAG — it does NOT upload the FPR; you must re-run a scan under a `submitForAudit:true` setting.
(Apply to all per-app settings 111-118 + the shared 107.)

### Per-app DAST (each satellite scans its own surface)

The shared 107 scan lands all findings under `WHS-346-Wholesale-APIM`. **Per-app** gives each satellite
its own DAST scan on its own SSC app (so DAST correlates with that app's SAST). Built 2026-06-30 — filter
the combined OpenAPI by the satellite's APIM **path prefix(es)**, upload the subset to that satellite's
**SSC app version**, create an API scan-setting there (same body as above). Mapping (path prefix → SSC
app version → DAST scan-setting id):

| Satellite | SSC app ver | APIM path prefixes | setting |
| --- | --- | --- | --- |
| Fireball | 10426 (v2) | api, policy, directbill, inscipher(proxy), clariondoor, blackline, northlight, producer, geovera, rsui, wellsfargo, matchinsurer, indexing, lookup, statements, bridgeepay, epay, velocity, policyimport | 111 |
| SubmissionEngine | 10162 | submissions, createsubmissionapi, submissionengine-{postemail,commonapi,seoip}, postemail, seoip | 112 |
| SalesCRM | 10674 | SalesCRM | 113 |
| B3owa | 10782 | indexingtopips, getofficeresources | 114 |
| B3Admin | 10781 | admin | 115 |
| CostCenter | 10795 | costcenter | 116 |
| TheBridge | 10676 | tbapi, tbfunc | 117 |
| EmailScheduler | 10201 | emailscheduler-schedulemail | 118 |

Read each setting's `cicdToken` via `GET /api/v2/application-version-scan-settings/<id>`. DOC has no APIM
API (no per-app DAST); Decus/PVS are separate. Verified: Fireball scan 806 runs under **Fireball/v2**.

**Pipeline wiring** (`optimize-pipeline-fleet-efficiency` template + 8 call-sites, committed 2026-06-30):
the template now derives `effectiveDastToken` from the `dastCiCdToken` **parameter** (per-app), falling
back to the shared `$(dastCiCdToken)` variable. Each satellite call-site passes
`dastCiCdToken: $(<sat>DastToken)`. **Operator step (ADO UI):** add the 8 `<sat>DastToken` secret vars to
the `FortifyVariables` group (VG 69) with each setting's `cicdToken` — until then pipelines fall back to
the shared scan (no breakage). Same `{env}`-resolution + leading-dot-`fileExtension` gotchas as above.

## Provisioning: per-app DAST tokens + new SSC applications

**DONE 2026-07-04:** both actions below were executed with the `whs-ci` creds (operator-vault /
prior-session history — NOT in any VG, local file, or cloudpc). VG 69 now carries the 8 `<sat>DastToken`
secrets; `PipsToVelocitySync` v1 exists (SSC project 735 / version 10887). This section stays the
runbook for rotating a token or onboarding the next satellite. The `whs-ci` credential is the linchpin —
the `sccCIToken` in VG 69 is read/upload only and cannot write SSC or reach the DAST API.

### Access matrix (verified 2026-07-04)

| Capability | Credential | Status |
| --- | --- | --- |
| Read VG 69 (all 6 vars) | ADO BBAdmin token | WORKS — every var `isSecret:false`; `GET …/variablegroups/69` returns values |
| Write VG 69 (PUT) | ADO BBAdmin token | DONE — full-PUT added the 8 secrets (HTTP 200); use full-PUT, not per-var PATCH (stale-cache) |
| SSC `/api/v1` READ (apps/versions/issues) | base64(`sccCIToken`) | WORKS — 607 apps |
| SSC app/version CREATE | UnifiedLoginToken minted from whs-ci Basic auth, used RAW | DONE — created project 735 / version 10887 |
| DAST `/api/v2` (read scan-settings / mint token) | whs-ci DAST token via `POST /api/v2/auth` | DONE — read cicdTokens for settings 111-118 |

### 1. Per-app DAST tokens → VG 69 (the 8 `partiallySucceeded` pipelines)

The 8 `<sat>DastToken` values ARE the `cicdToken`s of the already-created per-app scan-settings
**111-118** — you READ them, you don't mint new (a re-POST rotates the token). id → SSC app → VG-69
secret var (verified against the 8 call-sites 2026-07-04):

| setting | SSC app | VG-69 secret var |
| --- | --- | --- |
| 111 | Fireball | `fireballDastToken` |
| 112 | SubmissionEngine | `submissionengineDastToken` |
| 113 | SalesCRM | `salescrmDastToken` |
| 114 | B3owa | `b3owaDastToken` |
| 115 | B3Admin | `b3adminDastToken` |
| 116 | CostCenter | `costcenterDastToken` |
| 117 | TheBridge | `thebridgeDastToken` |
| 118 | EmailScheduler | `emailschedulerDastToken` |

Steps (exact commands: `references/fortify-api.md` § 7.1): mint DAST token from whs-ci creds → `GET
/api/v2/application-version-scan-settings/<id>` for 111-118 → read `.cicdToken` → PATCH/PUT VG 69
adding each as `isSecret:true`, then a UI **Save** to defeat the stale-membership cache. Until done,
pipelines fall back to the shared scan and the DAST task posts the literal `$(<sat>DastToken)` macro →
`400 {"errorCode":65}` (cosmetic, `continueOnError` — see ws memory `reference_fortify_dast_errorcode65_nonblocking`).

### 2. New SSC application (PipsToVelocitySync / pvs)

Confirmed absent 2026-07-04 (only PIPS-family + `Velocity Client` exist — distinct products, do NOT
repoint `applicationName` at them). Create via `POST /api/v1/projectVersions` with an **admin
UnifiedLoginToken** (whs-ci — the read-only `sccCIToken` 401/403s on admin scope). Name `PipsToVelocitySync`,
version `v1` (matches the `fortify-scan-stage.yml` default). Full attribute/commit recipe:
`references/fortify-api.md` §§ 3 + 7.2. After commit, the pvs SAST upload resolves instead of
`Failed to access application version: PipsToVelocitySync-v1`.

## Operational automation (scripts in ws `.azuredevops/scripts/`)

Built 2026-06-30 for report delivery, ADO task filing, and exception management:

- **Report-on-scan** — `fortify-report.py` + `_templates/fortify-report-step.yml` (wired into
  `fortify-scan-stage.yml` after the SAST task). Pulls the app version's issues from SSC (hosted SSC
  is internet-reachable from the ADO agent — no tunnel), writes `<app>_SAST_<m>_<d>.csv`, publishes a
  pipeline artifact, and emails via SendGrid when `reportEmailTo` + `SendGridApiKey` are set. (The SSC
  **report-generation API** `/api/v1/reports` 500s on a hand-built body — the templated PDF needs the
  UI's exact trace; the issues-API CSV is the reliable path.)
- **ADO task per issue** — `fortify-ado-tasks.py`. Reads active issues `>= severity` and opens one ADO
  work item each, tagged `fortify:<instanceId>` for **idempotent dedupe via WIQL**. Pipeline:
  `--ado-token $(System.AccessToken)`; `--dry-run` to preview; run under `proxychains4` for a local
  test (urllib can't socks). Verified live (work item #51795 from an SE finding). The native Fortify
  **ADO bug-tracker plugin** (Administration → Bug Tracking Plugins; currently only the Debricked
  parser plugin is installed) is the alternative for two-way state sync.
- **Suppress / exceptions** — the SSC issue-action + audit APIs (`/issues/action`, `/issues/audit`)
  reject hand-built bodies (400 "Object type not recognized" / 404); the reliable path is the **UI
  SUPPRESS** button (select issues on the Audit page → SUPPRESS). Confirmed working — re-suppressed
  Fireball v2's 17 (now 0 active / 44 suppressed). **Persistence caveat:** suppression rides on
  **issue matching** (instance IDs). Fireball's exceptions didn't carry because a re-scan churned the
  instance IDs (24 suppressed instances went `REMOVED`, 17 new came in active). Durable fix = pin the
  ScanCentral/SCA version + build so instance IDs stay stable; for a blanket category exception use a
  filter-set rule (template-level, instance-ID-proof).

**Manifest-enforced notifications + the checkout fix (2026-06-30).** The Fortify stage has TWO
jobs: `FortifyScan` (checks out the **satellite** repo for the SAST scan) and `FortifyDeliver`
(checks out **self/ws** for the report+ADO scripts AND the satellite manifest). The report step
reads `.azuredevops/satellites/<sat>/manifest.yml` → `fortify.reportEmails` and emails **ONLY** that
allow-list — edit the manifest, no pipeline change. (This also fixed a latent bug: the scan job
checks out the satellite, not ws, so the ws scripts weren't on the agent — hence the dedicated
deliver job with `checkout: self`.) `fortify-report.py --sat <key>` parses the list (pyyaml or a
regex fallback) and emits `fortifyReportEmails`; the email step's `condition` gates on it being
non-empty. ADO can't compile-time-read a manifest, so compile-time gates (`adoTaskMode`) stay
call-site params that MIRROR the manifest; runtime values (the email list) are read live.

**Wiring (Fireball, committed 2026-06-30):** the report step emails by linking the
**`es-canonical-seed-temp-dev`** VG (carries `SendGridApiKey`; `bridgespecialty.com` is
SendGrid domain-authenticated, so a `fortify-reports@bridgespecialty.com` sender is valid) +
`reportEmailTo`. The ADO-task step is wired via `fortify-ado-tasks-step.yml` with a `mode`
(`off`|`dry-run`|`create`); Fireball runs `dry-run` so its next deploy logs would-create without
creating. Caveats: (1) authorize the `es-canonical-seed-temp-dev` VG for the consuming pipeline
(first-run prompt); (2) for `create` mode, grant the build service **Contribute to work items**;
(3) `--ssc-token-raw` for a UnifiedLoginToken (a CI `sccCIToken` GUID is base64'd — the default).

## The shared pipeline template (dev-only)

`.azuredevops/build/_templates/fortify-scan-stage.yml` runs both engines, consumed dev-only by
every satellite via `${{ if eq(parameters.env, 'dev') }}` (the stage AND the `FortifyVariables`
group are both env-gated — non-dev compiles them out entirely). Contract:

- **SAST**: `FortifyScanCentralSAST@7` → `scanCentralCtrlUrl`/`scanCtrlToken` + uploads to SSC via
  `sccUrl`/`sccCIToken`, keyed by `applicationName` + `applicationVersion`.
- **DAST**: `FortifyScanCentralDAST@7`, fire-and-forget (`continueOnError`, gated
  `ne(variables['dastCiCdToken'], '')`), reuses `sccUrl`/`sccCIToken`, adds `scanCentralDastApiUrl`
  + `scanCentralCiCdToken`.

`FortifyVariables` (ADO Library, Wholesale Architecture, **VG id 69**) holds:
`scanCentralCtrlUrl`, `scanCtrlToken`, `sccUrl`, `sccCIToken` (SAST/SSC) + `scanCentralDastApiUrl`,
`dastCiCdToken` (DAST). **Per the consolidation, non-dev pipelines are manual-queue-only and `env`
is a required parameter** — never re-add `default: dev` (it caused silent dev deploys; see ws
project memory). Adding a var via the API needs a UI **Save** to resolve at runtime (ADO
stale-membership gotcha) — though a full-PUT has resolved at runtime in practice (verify by run).

## SAST vs DAST model

- **SAST** is per-application (one SSC app per satellite, `applicationName` = satellite PascalCase).
- **DAST** is per-**scan-setting**: each scan-setting carries its own `cicdToken`. The API surface
  is unified — **one `WHS-346-Wholesale-APIM` SSC app + one scan-setting + one shared
  `dastCiCdToken`** covers every satellite's API (they all sit behind the one APIM gateway). Each
  **UI** (Static Web Apps, dashboards) is a separate origin → its own scan-setting + token (these
  are descoped unless revived). DAST findings land in the same SSC as SAST → correlation.

## applicationName + applicationVersion — match the SSC app EXACTLY

The SAST controller resolves the upload target by `applicationName` + `applicationVersion`, matched
against the real SSC app **character-for-character** (case + spacing). The template defaults
`applicationVersion: v2`, but only Fireball has a `v2`; and a couple of satellite folder names don't
match the SSC app name (`B3owa`, `Email Scheduler`). Always verify against the live catalog:

```bash
ADM="<base64 SSC token>"   # admin or CI token
curl -s --max-time 30 --proxy socks5h://127.0.0.1:1080 -H "Authorization: FortifyToken $ADM" \
  -H "Accept: application/json" "https://ssc.bbins.fortifyhosted.com/api/v1/projects?limit=1000&fields=id,name"
# then the app's versions:
#   …/api/v1/projects/<appId>/versions?fields=id,name,active
```

Canonical mapping (audited 2026-06-20 — set these on each satellite's `fortify-scan-stage` call):

| Satellite | `applicationName` (exact SSC name) | SSC id | `applicationVersion` |
| --- | --- | --- | --- |
| Fireball | `Fireball` | 153 | `v2` (also v1, v1.5) |
| SubmissionEngine | `SubmissionEngine` | 159 | `v1` |
| SalesCRM | `SalesCRM` | 581 | `v1` |
| B3OWA | **`B3owa`** (NOT `B3OWA`) | 664 | `v1` |
| B3Admin | `B3Admin` | 663 | `v1` |
| CostCenter | `CostCenter` | 676 | `v1` (note: a separate `Cost Center Lookups` app 661 also exists — don't use it) |
| TheBridge | `TheBridge` | 583 | `v1` (a separate `The Bridge` 605 also exists) |
| DOC | `DOC` | 677 | `v1` |
| EmailScheduler | **`Email Scheduler`** (with a space) | 198 | `v1` |

Verified: with the correct name+version, SE/SalesCRM/B3Admin/CostCenter SAST uploads `succeeded`;
B3OWA failed only until `B3OWA`→`B3owa`. Fireball stays `v2`. Watch for duplicate/variant apps
(`Submission Engine-*`, `Cost Center Lookups`, `The Bridge`, the `PIPS` family) — pick the exact
canonical one above, not a look-alike.

## Reachability (DAST)

The hosted DAST farm scans from OpenText's cloud, so the target must be internet-reachable. Dev
APIM (`apim-whs-346-wholesale-centralus-dev.azure-api.net`) + most app-service backends
(`publicNetworkAccess: Enabled`, no IP lock) ARE reachable — **no in-VNet scan agent needed**.
Target the raw `…azure-api.net` host (bypasses the Cloudflare WAF on `apic.`).

## Deeper reference

For the full API auth flows, endpoint catalog, and step-by-step provisioning (create an SSC app +
version with required attributes, create a CI/CD-enabled DAST scan-setting, mint + read its token,
trigger a scan), read **`references/fortify-api.md`**.
