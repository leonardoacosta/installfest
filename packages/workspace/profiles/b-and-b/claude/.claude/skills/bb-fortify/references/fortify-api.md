# Fortify API + provisioning reference

> All calls route through the cloudpc SOCKS tunnel: `--proxy socks5h://127.0.0.1:1080`.
> Load `bb-azure-ops` for the tunnel + the `az --as-admin` wrapper used to mint ADO tokens.

## Table of contents
1. SSC REST API (`/api/v1`) — auth, apps, versions, tokens, scope
2. ScanCentral DAST API (`/api/v2`) — auth, scans, scan-settings, CI/CD tokens
3. Provisioning an SSC application + version
4. Provisioning a DAST scan-setting (mint a CI/CD token)
5. Triggering a scan + the pipeline contract
6. Credentials inventory

---

## 1. SSC REST API (`https://ssc.bbins.fortifyhosted.com/api/v1`)

**Auth:** `Authorization: FortifyToken <base64(token)>`. The raw token GUID 401s — base64 it.
A **CI token** (`sccCIToken`, a GUID) authenticates and can read apps/issues + upload, but is
**forbidden** from token admin (`/api/v1/tokens` → 403) and user admin (`/api/v1/localUsers` →
401). A **UnifiedLoginToken** from an admin account has broader scope (reads users; `/tokens` may
still be super-admin-only).

```bash
ADM=$(printf '%s' "<token-guid>" | base64)   # or use an already-base64 token verbatim
H=(-H "Authorization: FortifyToken $ADM" -H "Accept: application/json")
B="--max-time 30 --proxy socks5h://127.0.0.1:1080"

# applications (projects)
curl -s $B "${H[@]}" "https://ssc.bbins.fortifyhosted.com/api/v1/projects?limit=1000&fields=id,name"
# an app's versions  (use this to pick applicationVersion — see SKILL.md)
curl -s $B "${H[@]}" "https://ssc.bbins.fortifyhosted.com/api/v1/projects/<appId>/versions?fields=id,name,active"
# issue templates (need a valid id to create an app)
curl -s $B "${H[@]}" "https://ssc.bbins.fortifyhosted.com/api/v1/issueTemplates?fields=id,name"
```

Caution: SSC JSON for builds/timelines can carry control chars — prefer `python json.loads(strict=False)`
or grep scalars over `jq` in loop gates.

## 2. ScanCentral DAST API (`https://scdastapi.bbins.fortifyhosted.com/api/v2`)

**Auth is SEPARATE from SSC.** Mint a DAST token, then pass it RAW (no `FortifyToken`/`Bearer`
prefix — it's an `apiKey` in the `Authorization` header):

```bash
# 1. mint the DAST token (username/password -> {token:...}); keep creds out of argv via a file
printf '{"username":"%s","password":"%s"}' "$DAST_USER" "$DAST_PASS" > /tmp/da.json
DT=$(curl -s $B -X POST -H "Content-Type: application/json" --data @/tmp/da.json \
  "https://scdastapi.bbins.fortifyhosted.com/api/v2/auth" | python3 -c "import sys,json;print(json.load(sys.stdin)['token'])")
shred -u /tmp/da.json
# 2. use it
DH=(-H "Authorization: $DT" -H "Accept: application/json")

# list scans (note the path — /api/v2/scans 404s; the collection is scan-summary-list)
curl -s $B "${DH[@]}" "https://scdastapi.bbins.fortifyhosted.com/api/v2/scans/scan-summary-list?pageSize=20"
# scan-settings for an app-version (carry the cicdToken)
curl -s $B "${DH[@]}" "https://scdastapi.bbins.fortifyhosted.com/api/v2/application-versions/<avId>/scan-settings"
# scan policies (e.g. "API" policy for an APIM scan)
curl -s $B "${DH[@]}" "https://scdastapi.bbins.fortifyhosted.com/api/v2/policies?pageSize=40"
```

The DAST `applicationVersionId` IS the SSC projectVersion id (they share ids). Swagger:
`/swagger/v2/swagger.json`.

## 3. Provisioning an SSC application + version

Multi-step: create version (uncommitted) → set required attributes → commit. (Admin token.)

```bash
# create app+version (uncommitted; reversible)
curl -s $B "${H[@]}" -H "Content-Type: application/json" -X POST \
  --data '{"name":"dev","active":true,"committed":false,
    "project":{"name":"<AppName>","issueTemplateId":"Prioritized-HighRisk-Project-Template"},
    "issueTemplateId":"Prioritized-HighRisk-Project-Template"}' \
  "https://ssc.bbins.fortifyhosted.com/api/v1/projectVersions"   # -> data.id (versionId), data.project.id
```

Required attributes (use the INTEGER `attributeDefinitionId`, not the guid):

| id | attribute | sensible value |
| --- | --- | --- |
| 7 | Accessibility | `externalpublicnetwork` |
| 5 | DevPhase | `Active` |
| 6 | DevStrategy | `Internal` |
| 27 | Division | `00d6b5f3-d8bb-4361-86ca-525636cf51ed` (Wholesale) |
| 16 | URL (TEXT) | the app's URL |

```bash
# set attributes (SINGLE -> values[{guid}]; TEXT -> value)
curl -s $B "${H[@]}" -H "Content-Type: application/json" -X PUT --data '[
 {"attributeDefinitionId":7,"values":[{"guid":"externalpublicnetwork"}],"value":null},
 {"attributeDefinitionId":5,"values":[{"guid":"Active"}],"value":null},
 {"attributeDefinitionId":6,"values":[{"guid":"Internal"}],"value":null},
 {"attributeDefinitionId":27,"values":[{"guid":"00d6b5f3-d8bb-4361-86ca-525636cf51ed"}],"value":null},
 {"attributeDefinitionId":16,"values":[],"value":"https://<host>"}]' \
  "https://ssc.bbins.fortifyhosted.com/api/v1/projectVersions/<versionId>/attributes"
# commit
curl -s $B "${H[@]}" -H "Content-Type: application/json" -X PUT --data '{"committed":true}' \
  "https://ssc.bbins.fortifyhosted.com/api/v1/projectVersions/<versionId>"
```

## 4. Provisioning a DAST scan-setting (mints the CI/CD token)

`POST /api/v2/application-version-scan-settings` with a `BasicScanSettingsDTO`. The response is a
`SaveApplicationVersionScanSettingsResponseDTO` carrying the new `cicdToken`. Minimal working body
(validated 2026-06-20 — the "API" policy `81a26872-2d4a-48eb-9352-2219b2da5d0f`):

```bash
curl -s $B "${DH[@]}" -H "Content-Type: application/json" -X POST --data '{
  "applicationVersionId": <avId>, "name": "<scan name>", "scanType": 1, "submitForAudit": false,
  "scanSettings": {
    "scanMode": 2, "startUrls": ["https://<target>"],
    "restrictToFolder": false, "policyId": "81a26872-2d4a-48eb-9352-2219b2da5d0f",
    "userAgentType": 1, "spaOptionType": 1,
    "hasSiteAuthentication": false, "hasNetworkAuthentication": false,
    "useProxyServer": false, "enableTrafficMonitor": false,
    "enableSASTCorrelation": true, "useScannerScaling": false }}' \
  "https://scdastapi.bbins.fortifyhosted.com/api/v2/application-version-scan-settings"
# -> {"id":..,"cicdToken":"<GUID>",...}
```

Gotchas: `allowedHosts` wants a non-trivial object shape — OMIT it (nullable) for a baseline scan.
`scanMode`: 1=CrawlOnly 2=CrawlAndAudit 3=AuditOnly. `scanType`: 1=Standard 2=WorkflowDriven 3=API.
A meaningful API scan should later add the APIM OpenAPI definition (`apiDefinition*`) + a sub-key;
a UI scan needs `hasSiteAuthentication: true` + a login-macro `loginMacroBinaryFileId`. Existing
scan-settings store config as an uploaded **WebInspect XML** (`webInspectSettings`), not inline
`scanSettings` — clone via `sourceScanSettingsId` if you need that exact config.

## 5. Triggering a scan + the pipeline contract

```bash
# direct trigger (what the pipeline task does under the hood)
curl -s $B "${DH[@]}" -H "Content-Type: application/json" -X POST \
  --data '{"cicdToken":"<GUID>","name":"<run name>"}' \
  "https://scdastapi.bbins.fortifyhosted.com/api/v2/scans/start-scan-cicd"   # -> 201 {"id":<scanId>}
```

`FortifyScanCentralDAST@7` (task v7.4.0) builds `<scanCentralDastApiUrl>/scans/start-scan-cicd` —
so `scanCentralDastApiUrl` MUST be `…fortifyhosted.com/api/v2`. It authenticates via `sscCiToken`
and the `scanCentralCiCdToken`; the scan is fire-and-forget (no wait), results land in SSC.

## 6. Credentials inventory (where they live)

| Secret | Where | Notes |
| --- | --- | --- |
| SAST controller url/token | `FortifyVariables` (`scanCentralCtrlUrl`, `scanCtrlToken`) | non-secret in the VG |
| SSC url + CI token | `FortifyVariables` (`sccUrl`, `sccCIToken`) | shared SAST+DAST SSC auth |
| DAST API url | `FortifyVariables` (`scanCentralDastApiUrl`) | MUST end `/api/v2` |
| DAST CI/CD token | `FortifyVariables` (`dastCiCdToken`) | per scan-setting; unified APIM = one shared token |
| DAST API user/pass | (operator-held `whs-ci` account) | mints the `/api/v2/auth` token; NOT in the VG |
| SSC admin UnifiedLoginToken | (operator-held) | for app/user/scan-setting provisioning |

FB's pipeline-level SAST vars live on build def 328 (`2-dev-build`); the canonical `FortifyVariables`
group is VG 69 in the Wholesale Architecture ADO project.

---

## 7. Provisioning runbook — per-app DAST tokens + a new SSC app

Two provisioning actions gate the fleet. **Both need the `whs-ci` credential** (username+password),
which mints the tokens that can WRITE to SSC / reach the DAST API. The creds are NOT in VG 69, any
Wholesale VG, local files, or on cloudpc — they live in the operator vault / prior-session history.
The `sccCIToken` in VG 69 is **read/upload only** (401 on `/api/v1/localUsers`, 403 on
`/api/v1/tokens`), so it can neither create an SSC app nor reach the DAST API (401 raw + base64).

**Both actions were executed 2026-07-04** with the whs-ci creds — VG 69 now carries the 8
`<sat>DastToken` secrets, and `PipsToVelocitySync` v1 exists (SSC project 735 / version 10887). The
runbook below stays valid for re-reading a rotated token or onboarding the next satellite.

**SSC admin auth (learned 2026-07-04):** whs-ci Basic auth is accepted ONLY at `POST /api/v1/tokens`
(mints a UnifiedLoginToken, HTTP 201) — Basic auth 401s on ordinary GETs. Use the minted
UnifiedLoginToken **RAW** in `Authorization: FortifyToken <token>` (do NOT base64 it — base64 is only
for the GUID-style CI token). The DAST `/api/v2/auth` accepts the whs-ci user/pass directly. The SSC
default issue template resolves to `PCI-SSF-1.2-Basic-Project-Template` (fetch dynamically, don't hardcode).

### 7.1 Read the 8 per-app DAST cicdTokens → PATCH into VG 69

The 8 `<sat>DastToken` values ARE the `cicdToken`s of the already-created per-app scan-settings
**111-118** — READ the existing ones (don't mint new; a re-POST would rotate the token and orphan the
scan-setting). id → SSC app → VG-69 secret var name (verified against the 8 pipeline call-sites,
2026-07-04):

| setting | SSC app | VG-69 secret var (`$(...)` at call-site) |
| --- | --- | --- |
| 111 | Fireball | `fireballDastToken` |
| 112 | SubmissionEngine | `submissionengineDastToken` |
| 113 | SalesCRM | `salescrmDastToken` |
| 114 | B3owa | `b3owaDastToken` |
| 115 | B3Admin | `b3adminDastToken` |
| 116 | CostCenter | `costcenterDastToken` |
| 117 | TheBridge | `thebridgeDastToken` |
| 118 | EmailScheduler | `emailschedulerDastToken` |

```bash
# a. mint the DAST token (whs-ci creds; keep out of argv)
printf '{"username":"%s","password":"%s"}' "$WHS_CI_USER" "$WHS_CI_PASS" > /tmp/da.json
DT=$(curl -s --max-time 40 --proxy socks5h://127.0.0.1:1080 -X POST \
  -H "Content-Type: application/json" --data @/tmp/da.json \
  "https://scdastapi.bbins.fortifyhosted.com/api/v2/auth" \
  | python3 -c "import sys,json;print(json.load(sys.stdin)['token'])")
shred -u /tmp/da.json

# b. read each per-app cicdToken (raw DT in Authorization, no prefix)
for id in 111 112 113 114 115 116 117 118; do
  printf '%s -> ' "$id"
  curl -s --max-time 40 --proxy socks5h://127.0.0.1:1080 \
    -H "Authorization: $DT" -H "Accept: application/json" \
    "https://scdastapi.bbins.fortifyhosted.com/api/v2/application-version-scan-settings/$id" \
    | python3 -c "import sys,json;print(json.load(sys.stdin).get('cicdToken'))"
done
```

Then PATCH VG 69 (BBAdmin ADO token, resource `499b84ac-…`). Fetch the current group, add the 8 vars
as `{"value":"<cicdToken>","isSecret":true}`, PUT the whole object back
(`PUT …/distributedtask/variablegroups/69?api-version=7.1-preview.2`). **Foot-gun:** an API-added VG
var reads `$(macro)` EMPTY at runtime until a UI **Save** resolves the stale membership cache
(`bb-azure-ops` § Common foot-guns) — so after the PUT, open VG 69 in the ADO web UI and Save, or add
the 8 vars via the UI directly. A full PUT has resolved at runtime in practice — verify by a real run.

### 7.2 Create the PipsToVelocitySync (pvs) SSC app

Confirmed absent 2026-07-04 (607 SSC apps; only PIPS-family + `Velocity Client` — distinct products,
do NOT repoint `applicationName` at them). Create with an **admin UnifiedLoginToken** (whs-ci) via the
§ 3 recipe: `POST /api/v1/projectVersions` with `project.name:"PipsToVelocitySync"`, version `v1`
(matches the `fortify-scan-stage.yml` default) → set required attributes (§ 3 table) → commit. The URL
attribute (id 16) can be the pvs APIM/app host or a placeholder — pvs is an SB subscriber, so the SAST
scan is the point, not DAST. After commit, the next pvs deploy's SAST upload resolves instead of
`Failed to access application version: PipsToVelocitySync-v1`.
