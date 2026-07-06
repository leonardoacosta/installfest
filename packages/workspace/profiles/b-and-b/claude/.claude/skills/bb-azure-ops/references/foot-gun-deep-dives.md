# Foot-gun deep-dives — full mechanism, incident narrative & exact repro

> Load this file when a row in SKILL.md § Common foot-guns is marked **→ deep-dive** and the one-line table essence isn't enough — this carries the full mechanism, the incident narrative, the exact repro, and every GUID/command/date behind that row. Skip when the table cell already answers your question, or when the work is Graph/SNOW/WHS-pipeline/boot-crash/diagram (those have their own references).

One subsection per de-densified table row (heading = the symptom). Rows that are already one-liners in the table have no deep-dive.

### `az monitor diagnostic-settings list` FALSE-EMPTY on a storage sub-resource scope

The `az monitor` CLI returns a FALSE-EMPTY on the `blobServices/default` (and likely other storage sub-resource) scope — it does NOT enumerate settings on storage sub-resource scopes correctly. Confirmed 2026-06-19: nearly drove a wrong "the diag deploy is broken, re-queue the 40-min pipeline" conclusion when the setting (`stwhs346sedev-blob-diagnostics`) was actually live the whole time. Verify diag settings via the **management REST API**, never the `az monitor` CLI, on any storage sub-resource scope: `curl ... "https://management.azure.com<accountId>/blobServices/default/providers/Microsoft.Insights/diagnosticSettings?api-version=2021-05-01-preview"`. The account-scope `az` list is also unreliable for blob ops — REST is the only source of truth here.

### `az graph query` (and any multi-word arg) fails `unrecognized arguments`

The `~/.local/bin/az` wrapper does `exec "$REAL_AZ" $ARGS` **unquoted** — word-splits any multi-word/quoted arg (KQL queries, `--query` JMESPath, space-containing `--tags`). Confirmed 2026-05-25. Bypass the wrapper: `export AZURE_CONFIG_DIR=$HOME/.azure-bbadmin HTTPS_PROXY=socks5h://127.0.0.1:1080 HTTP_PROXY=socks5h://127.0.0.1:1080 NO_PROXY=localhost,127.0.0.1,::1` and call the real binary `$HOME/.local/share/pipx/venvs/azure-cli/bin/az` directly.

### cloudpc `sshd` HUNG/dead (fresh SSH fails, tunnel still forwards)

**OpenSSH `sshd` service on cloudpc stops serving NEW connections** while the established `-D` tunnel (already-forked session) keeps forwarding. TWO confirmed failure signatures — both share "tunnel alive, fresh SSH dead": (a) **SYN timeout** (2026-06-11): TCP SYN to `:22` never answered; `tailscale ssh cloud-pc` connects-then-"timed out during banner exchange". (b) **Pre-banner RST** (2026-07-06): TCP handshake ACCEPTED ("Connection established" in `ssh -vvv`) then reset before the version banner — `kex_exchange_identification: read: Connection reset by peer` — while RDP `:3389` stays open (proves the VM is awake, Tailscale fine). In both variants C: disk + RAM are FINE — NOT disk/RAM starvation (don't chase `Win32_LogicalDisk` first here). **Triage a suspected outage without SSH**: `ping`/`tailscale ping` (host up), bash `/dev/tcp/100.83.148.5/22` + `/3389` (which ports accept), `systemctl --user status cloudpc-tunnel.service` + a SOCKS curl (tunnel still forwarding). **Impact is now LIMITED**: `scripts/ado` is SOCKS-first (2026-07-06) — token mint, queue, watch, registry all work with sshd dead; only PS-proxy shortcuts + cloudpc filesystem ops are blocked. Fix: RDP in (or use an existing interactive session), open **PowerShell as Administrator** (a non-elevated `Restart-Service sshd` fails with `Cannot open sshd service on computer '.'`), run `Restart-Service sshd -Force` (fallback if wedged: `Stop-Service sshd -Force; Get-Process sshd | Stop-Process -Force; Start-Service sshd`). Briefly drops the SOCKS tunnel; `cloudpc-tunnel.service` auto-reconnects. No reboot needed. **Permanent fix**: `~/dev/ws/scripts/cloudpc-sshd-watchdog.ps1 -Install` (elevated, once) registers BOTH layers — `sc.exe failure` crash recovery AND a 5-min SYSTEM scheduled task that probes the 127.0.0.1:22 banner and `Restart-Service sshd -Force`es on hang (service recovery alone can't catch the hung-but-Running state). Install over SSH once sshd is back (`scp` + `ssh cloudpc 'powershell ... -Install'` — the SSH service account has admin rights) or paste into the RDP session used for the manual restart. Health log: `C:\Tools\sshd-watchdog.log`.

### Queueing a SUBSET of pipeline stages — `stagesToSkip`, not object `templateParameters`

The queue API silently DROPS object-typed `templateParameters` (only scalars like `whatIf` stick — confirmed across legacy `/build/builds` AND `/pipelines/{id}/runs`); a pipeline's `tasks`/`projects` filter params are `type: object`, so you cannot scope a run through them. Use the runs API **`stagesToSkip`** field (the API form of the UI "Stages to run") — a SEPARATE mechanism from `templateParameters`: `POST .../pipelines/{id}/runs?api-version=7.1-preview.1` body `{ "resources":{"repositories":{"self":{"refName":"refs/heads/dev"}}}, "templateParameters":{"whatIf":false}, "stagesToSkip":["StageA","StageB",...] }`. A `stagesToSkip` skip resolves to `Skipped`, and a downstream stage's default `succeeded()` condition treats a skipped dependency as **satisfied** (does NOT cascade-skip) — so you can skip the whole middle of a linear chain and still run the tail stage. Confirmed 2026-06-12 (fleet-sync 519: skipped PeDns/Rbac/SbTopology/StorageTopology/KvSync, ApimSync still ran). Probe with `whatIf:true` first (non-mutating) to confirm the tail stage isn't cascade-skipped, then re-queue `whatIf:false`.

### Legacy PROD sub `0e4f65bb` empty to `--as-o365` — `pim 4` then `--as-admin`

o365 has NO *active* RBAC on legacy PROD; the PIM-eligible "Contributor → ALL-Wholesale-PROD" (`pim` role #4) is on **BBAdmin** (principal `f183a4a9`), NOT o365 (`3add8b4a`). Confirmed 2026-06-05. `pim 4` (activates BBAdmin, 8h), then query as **`--as-admin`** (not `--as-o365`). Resource Graph lags the PIM grant — use direct `az resource list` / `az monitor app-insights query` (live RBAC).

### `az monitor app-insights query -g <rg> --app <name>` returns empty / `Expecting value`

The `-g + --app <name>` form silently fails to resolve the component. Pass the AppId **GUID** instead: `--app $(az resource show -g <rg> --resource-type Microsoft.Insights/components -n <name> --query properties.AppId -o tsv)`. Reminder: AI `requests` has NO bodies — only apps that explicitly log the body leak payloads into `traces`, e.g. `Request requestBody:[...]`.

### `az monitor app-insights query` "dry" without `--offset`

**The command defaults to a ~1-HOUR API timespan when no `--offset`/`--start-time` is passed, and that timespan CLIPS the in-query `ago()` filter.** A KQL `ago(90d)` does NOTHING without the matching `--offset`. Burned a whole session concluding "the legacy AIs went dry / traces purge hourly" — both FALSE; retention was 120d and `traces`=33.6M rows. Confirmed 2026-06-06. ALWAYS pass `--offset <window>` (e.g. `--offset 90d`) — or `--start-time`/`--end-time` — on every historical AI query. Same applies to `az monitor log-analytics query` (use `--timespan`/ISO8601). A "dry" result without `--offset` is meaningless.

### ADO build-poll loop never breaks (raw-JSON `jq` in a loop gate)

ADO build/timeline JSON embeds raw control chars (commit message, `triggerInfo`, log URLs); `jq` errors on them and `2>/dev/null` swallows the error → the gated scalar is `""` so `completed` never matches. Confirmed 2026-06-04. Don't `jq` raw ADO JSON in a loop gate. Grep the scalar: `bstatus=$(printf '%s' "$B" | grep -o '"status":"[^"]*"' | head -1 | cut -d'"' -f4)`, or parse structured fields with `python json.loads(strict=False)`. Never `2>/dev/null`-swallow a parser a loop's exit depends on.

### ADO `preview` fails `Unexpected parameter 'X'` after a local template edit

`yamlOverride` (POST `.../pipelines/ID/preview`) only overrides the MAIN yaml; referenced TEMPLATES are still pulled from the COMMITTED branch, so a new param passed to a locally-edited template isn't visible — preview fails `Unexpected parameter 'X'`. Confirmed 2026-06-04. Commit+push the template first, then preview the pushed branch. Local `yamlOverride` alone cannot validate a not-yet-committed template change.

### ADO secure-file `condition:` doesn't skip compile-time validation

ADO validates the `secureFile` reference at COMPILE/queue time, BEFORE any runtime `condition:` runs — so a `condition:`-gated `DownloadSecureFile@1` task still fails QUEUE-time validation ("secure file `X` could not be found / not authorized") when the file is absent. Confirmed 2026-06-07 (build 54205, bsg-email-scheduler). Gate the task with a COMPILE-TIME `${{ if parameters.flag }}` block (parameters, NOT variables) so it's excluded from the compiled pipeline entirely. Verify with the `preview` API: `finalYaml` should omit the secure file. Same compile-time-validation applies to service connections + environments.

### Variable-group var added via API reads EMPTY at runtime (stale membership)

A variable ADDED to an existing variable group PROGRAMMATICALLY (REST `variablegroups` PUT, or `az pipelines variable-group variable create/update`) is visible via API GET but the pipeline reads `$(VarName)` as EMPTY at runtime — a script consuming it silently skips. Pre-existing operator-UI-set vars in the SAME group expand fine. NOT secret-specific: verified 2026-06-07 across 5 seed runs + 4 set-methods incl. a NON-secret var whose value WAS visible via `variable list` (CFWebhookSecrets on VG 84 `se-appconfig-temp-dev`, pipeline 541). Name byte-matched, VG authorized for the pipeline, YAML wiring committed — every config layer correct, yet runtime resolution returned empty. The pipeline's view of the variable group's MEMBERSHIP appears cached/stale to API-added variables — the API write lands in the group (GET sees it) but isn't registered for `$(macro)` resolution until the group is re-saved through the web UI. (Earlier note blamed secret-masking; the non-secret repro disproved that.) Fix: **Add/modify variable-group variables via the ADO web UI** (Pipelines → Library → group → Add → name+value → Save), OR after an API write, open the group in the UI and hit Save to commit membership. Don't burn pipeline runs re-trying programmatic sets — they GET-back fine but won't expand.

### App settings vanish after a wholesale infra deploy (Bicep appSettings = full replacement)

App boots 500/503 after a wholesale **infra** deploy even though code didn't change; settings like `WEBSITE_DNS_SERVER` / `AZURE_CLIENT_ID` / `ASPNETCORE_DETAILEDERRORS` vanished. Bicep `siteConfig.appSettings` in incremental mode is a **full replacement, not a merge** — every infra deploy wipes any app setting not declared in the template, nuking runtime-set vars and triggering a cold-start cascade. Confirmed across 7 sessions. Fix: bake **all** required settings into Bicep (single source of truth), or restart the app after each infra deploy. Don't set settings at runtime and expect them to survive the next deploy.

### `DefaultAzureCredential()` binds the system-MI (zero roles) over the user-MI

A Function App's `DefaultAzureCredential()` resolves to a managed identity with **zero roles** (KV/SQL 403) even though a user-assigned MI is attached. With both a system-MI and a user-MI present, `DefaultAzureCredential` picks the **system-assigned** MI unless told otherwise; the module sets `AzureWebJobsStorage__clientId` but NOT the generic `AZURE_CLIENT_ID`. Fix: set the `AZURE_CLIENT_ID` app setting to the **user-assigned MI's client id** so `DefaultAzureCredential` binds to the identity that actually holds the grants.

### `WEBSITE_RUN_FROM_PACKAGE` + `deploymentMethod` must agree (both 503 directions)

The boot symptom is an ANCM health probe reading `000` (no-TCP) or a runtime-write/init-step app hanging. `WEBSITE_RUN_FROM_PACKAGE=1` makes `wwwroot` a read-only ZipFS — any runtime write blocks and ANCM kills the worker. Worse, `AzureWebApp@1` defaults to `deploymentMethod: runFromPackage` and **silently resets `RUN_FROM_PACKAGE` back to `1` every deploy**, overriding a Bicep `=0`. Confirmed 2026 (FBMatchInsurer). Set `WEBSITE_RUN_FROM_PACKAGE=0` for apps with init/runtime-write side-effects, AND add `deploymentMethod: zipDeploy` to the `AzureWebApp@1` task so the deploy stops re-forcing `=1`. Bare IIS 503 + empty `wwwroot` + zero ARM deploys = `RUN_FROM_PACKAGE=1` set but no code ever published. **The inverse pairing also 503s**: pipeline does `zipDeploy` (mutable wwwroot, leaves `SitePackages` empty) while the app setting is still `=1` → worker tries to mount a package zip that was never created → IIS **503.0 "Could not download zip"** on EVERY route, Kudu `~1` site stays 200, and NO app boot exception (worker never starts). Diagnostic: read `LogFiles/DetailedErrors/ErrorPage*.htm` (says "Could not download zip") + `vfs/data/SitePackages/packagename.txt` (404 = no package). Fix = `=0` + `zipDeploy` (must always travel together). Confirmed 2026-06-10 FBApi-test.

### Secret VG var reads empty in a script step — map into `env:`

ADO does NOT auto-expose **secret** VG variables as `$(macro)`/env (non-secret ones expand, secret ones don't). Confirmed (pipeline 541 seed `VGSRC_x=$(x)` failed loud on the first secret var). Explicitly map it into an `env:` block on the consuming task: `env: { MY_SECRET: $(MySecretVar) }`. Note: an `env:` var set in one task does NOT inherit downstream — inline the mapping in each task. (For the temp-seed VGs, repopulate as `--secret false`.)

### Re-auth "succeeded" but the op stays dead (wrong config dir)

**Re-auth wrote one config dir; the op reads another.** A plain `ssh cloudpc 'az login'` refreshes the **O365 default** `~/.azure`, but `az --as-admin ...` reads `~/.azure-bbadmin` — so the BBAdmin op stays broken. Burned 2026-06-02. Re-auth the profile the op reads: `AZURE_CONFIG_DIR=$HOME/.azure-bbadmin az --as-admin login` (management scope). **Verify with a REAL ARM op, not `az account show`** — `account show` reads cached metadata and lies after token expiry; `az-audit` mtime also lies.

### Browser MSAL/OAuth fails `ERR_EMPTY_RESPONSE` on the loopback callback when proxied

Browser MSAL/OAuth (VS Code Azure auth, Cursor Cosmos) fails `ERR_EMPTY_RESPONSE` / "localhost didn't send data" when the browser is proxied. **ProxyBridge never intercepts loopback** (`127.0.0.1`/`::1`), so a proxied browser hijacks the MSAL `localhost` callback before the loopback listener sees it. Use an **unproxied** browser for the OAuth loopback step (Safari worked where proxied Edge failed); "didn't send data" is a Remote-SSH/loopback boundary, NOT a tunnel fault.

### Edge from `ssh cloudpc` opens invisibly / logged-out (session-0 + DPAPI)

SSH runs as the service account `346-cpc-qjxvz\leo` in **session 0** (no desktop): a normal `msedge.exe` exits instantly there, and only `--headless=new --no-sandbox --disable-gpu` runs at all. Worse, **DPAPI is per-user** — Edge can't decrypt `LeonardoAcosta`'s o365 SSO/refresh tokens from session 0 (`Failed to encrypt: Access is denied. (0x5)` / `Failed to decrypt token for service AccountId-<o365-objid>` — `3add8b4a…` is the o365 identity), so the profile loads logged-OUT and bounces to AAD login. Confirmed 2026-06-20. There is **NO** SSH/session-0 path to an authenticated page. Launch in the interactive `LeonardoAcosta` session: confirm `query session` shows an `Active` `LeonardoAcosta` row, then fire an **interactive scheduled task** (`Register-ScheduledTask` with `-Principal (New-ScheduledTaskPrincipal -UserId LeonardoAcosta -LogonType Interactive)`, or `schtasks /ru LeonardoAcosta /it`). If nobody is logged on, an `-AtLogOn` self-deleting task auto-opens it on his next sign-in. See § Launching / finding Edge on cloudpc in SKILL.md.

### A fresh role/PIM grant still 403s for ~30 min (RBAC eventual consistency)

A fresh role/PIM grant still 403s on read or write for ~30 min after it's created because Azure RBAC is **eventually consistent** — a new grant (or PIM activation) can take ~30 min to land, and the bearer token's claims are stale until refreshed. Wait ~30 min, OR force a token-claim refresh (RDP into cloudpc re-auths the session claims). For prod telemetry/Entra reads, PIM-elevate **first** (`pim <n>` / `pim --all`) — even an O365-admin-assigned account often needs PIM active.

### Managed DevOps Pool bump reverts / pool stuck `notStarted`

MDP has its **own DSv5 quota** under the `Microsoft.DevOpsInfrastructure` provider (separate from Compute quota) — exhausting it silently reverts the bump. A pool stuck `notStarted` with **0 Activity-Log events** is an MS-side backplane failure that **survives delete+recreate**. Raise the DevOpsInfrastructure quota via the **portal** (Quota API rejects it). For the backplane hang there's no self-fix — route deploys to a working pool's subnet and open an MS support case. (Image PATCH rejects mixing `wellKnownImageName` + resourceId objects; gallery must attach to the DevCenter **with a managed identity**.)

### KV private-endpoint write fails on a single `:443` TCP connect-timeout

A KV private-endpoint write (esp. a bulk secret seed) fails on a single `:443` TCP connect-timeout even though routing is correct (PE in VNetLocal, NVA-bypassed). Genuine transient — PE NIC blip / TCP retransmit / SNAT-port reuse, NOT a misconfiguration; the flake predates the routing changes. Wrap KV `set` in retry-with-backoff. **Never gate a deploy or a root-cause conclusion on a single KV-PE timeout** — re-run first (212/213 succeed, 1 timeout; next run seeds clean).

### Cloudflare `522` / `525` / `526` fronting wholesale APIM

The CF code localizes the layer: **522 = TCP timeout to origin** (network/routing/firewall — rules OUT cert), **525 = SSL handshake failed**, **526 = SSL cert invalid** (classically APIM `hostnameConfigurations` reset to BuiltIn dropping `apic.<env>`). For **522** check origin reachability + CF SSL mode (Full vs Flexible) vs what the origin supports — NOT the cert. For **526** restore the custom-domain `hostnameConfigurations` in Bicep (§ in `references/whs-pipelines.md`). CF is the sole intended ingress (AFD decommissioned).

### SPN `Application.ReadWrite.OwnedBy` 403 (privileged Graph permission)

Granting a service principal the Graph `Application.ReadWrite.OwnedBy` app permission via `POST /servicePrincipals/{sp}/appRoleAssignments` returns 403 even though the calling identity holds `AppRoleAssignment.ReadWrite.All` + Cloud Application Administrator. `Application.ReadWrite.OwnedBy` is on Microsoft's **privileged Graph permission** set — admin-consenting a *privileged* permission requires **Global Administrator or Privileged Role Administrator**; Cloud App Admin (`wids b79fbf4d…`) + `AppRoleAssignment.ReadWrite.All` are deliberately fenced out of the privileged set (anti-escalation). Confirmed 2026-06-24. BBAdmin/O365 CAN *manage* app regs (redirectUris/owners via `Application.ReadWrite.All`) but CANNOT hand a deploy SPN directory-wide app control. Escalate to Global Admin / PRA (SNOW), OR run an operator-authenticated out-of-band lane (the `ws/apps/appreg-callbacks` Ink tool — read-merge-PATCH redirect URIs as BBAdmin, NOT the pipeline SPN), OR sidestep with a stable custom domain so the SWA callback never changes.
