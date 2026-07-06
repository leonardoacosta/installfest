# ServiceNow mechanics — cookie reuse + auto-refresh

> Load this file when working with ServiceNow tickets (RITM/REQ lookups, variable pool extraction, ticket comments) or troubleshooting the SNOW auto-refresh timer. Skip when working on Graph/ADO/Azure — those have their own patterns.

## Why cookies instead of OAuth

The Brown & Brown tenant restricts every public Microsoft client from the SNOW SAML enterprise app. Both the Graph CLI client (`14d82eec-...`) and the Az CLI client (`04b07795-...`) get `AADSTS650057` when requesting a SNOW-audience token. Device-code flow fails the same way.

This is **not a permission Leo can request**. Until an admin registers a new app reg with SNOW in `requiredResourceAccess`, the OAuth path is dead. Cookie reuse is the workaround that ships.

## How session cookies work

cloudpc has a dedicated Edge profile at `C:\Users\LeonardoAcosta\AppData\Local\edge-snow-cdp` that holds a long-lived SAML session for `brown.service-now.com`. The `snow-refresh` script:

1. SSH's to cloudpc as the SSH service account (`leo.346-CPC-QJXVZ`)
2. Spawns Edge via `schtasks /RU AzureAD\LeonardoAcosta /IT` — runs in LeonardoAcosta's interactive session because that's where the SAML cookies live
3. Opens Chrome DevTools Protocol on port 9222
4. Drives CDP to navigate to `https://brown.service-now.com`, completing SAML if needed
5. Extracts cookies via `Network.getAllCookies` — pulls session cookies (`JSESSIONID`, `BIGipServerpool_brown`, `glide_session_store`, etc.) from RAM
6. Writes the cookie string to `/tmp/snow-cookies.txt` on the Linux side

Why CDP and not the SQLite cookie store: Chromium only flushes cookies with `Expires`/`Max-Age` to disk. Session cookies (which SNOW uses) live exclusively in RAM. Plus modern Edge encrypts persisted cookies with `v20` app-bound encryption that requires the Edge process's identity to decrypt — admin context can't unwrap them.

## Refresh + use

```bash
~/dev/ws/scripts/snow-refresh                # refresh /tmp/snow-cookies.txt
~/dev/ws/scripts/snow-refresh test           # full E2E verification
~/dev/ws/scripts/snow-refresh start          # spawn Edge CDP without extracting (first-run setup)
~/dev/ws/scripts/snow-refresh stop           # kill the CDP Edge instance
~/dev/ws/scripts/snow-refresh print          # extract and print to stdout (skip file write)

# Use the cookies:
COOKIE=$(cat /tmp/snow-cookies.txt)
curl -s --socks5-hostname localhost:1080 -H "Cookie: $COOKIE" \
  "https://brown.service-now.com/sc_req_item.do?JSONv2&sysparm_action=getRecords&sysparm_query=sys_id=<sys_id>"
```

## Auto-refresh (systemd user timer)

A timer keeps `/tmp/snow-cookies.txt` fresh without manual intervention:

```
~/.config/systemd/user/snow-cookie-refresh.service    (oneshot, calls snow-refresh)
~/.config/systemd/user/snow-cookie-refresh.timer      (every 6h)
```

Schedule: `OnBootSec=2min` to catch post-boot, `OnUnitActiveSec=6h` for steady state, `Persistent=true` so missed ticks fire on next boot.

Inspect:

```bash
systemctl --user list-timers snow-cookie-refresh.timer        # next fire + last result
systemctl --user status snow-cookie-refresh.service           # last invocation summary
journalctl --user -u snow-cookie-refresh.service -n 20        # last 20 log lines
systemctl --user start snow-cookie-refresh.service            # manual fire
```

If the timer turns red, the `edge-snow-cdp` profile's Entra session expired. Fix: RDP to cloudpc, sign back in to `brown.service-now.com` once in the Edge window. Refresh tokens last ~90 days as long as the timer is running.

## Endpoint reality

| Endpoint | Auth | Status |
| --- | --- | --- |
| `/api/now/v2/table/...` (modern REST) | Bearer only | REJECTS cookies — "User is not authenticated" |
| `/<table>.do?JSONv2&sysparm_query=...` | Session cookie | Works (read-only) |
| `/<table>.do?sys_id=...` (UI HTML) | Session cookie | Works — scrape `<textarea>` for variable pool content |
| `/sc_item_option_mtom.do?...` (variable bindings) | Session cookie + admin perms | Sometimes 403 — recent cookies CAN read it |
| `/api/sn_sc/v1/servicecatalog/...` | Bearer only | REJECTS cookies |

Permission ceiling: read tickets + their variable pools (when cookies are fresh). No write access. Comments + state changes go via the UI in Edge.

## Variable pool extraction pattern

The Business Justification field on Cloud Access Requests doesn't surface in JSONv2 — it lives in `sc_item_option` records linked via `sc_item_option_mtom`. To extract:

```bash
COOKIE=$(cat /tmp/snow-cookies.txt)

# Step 1: get all option-link records for this RITM
curl -s --socks5-hostname localhost:1080 -H "Cookie: $COOKIE" \
  "https://brown.service-now.com/sc_item_option_mtom.do?JSONv2&sysparm_action=getRecords&sysparm_query=request_item=<ritm_sys_id>" \
  | jq -r '.records[].sc_item_option' > /tmp/option-ids.txt

# Step 2: fetch each option's value
for OID in $(cat /tmp/option-ids.txt); do
  curl -s --socks5-hostname localhost:1080 -H "Cookie: $COOKIE" \
    "https://brown.service-now.com/sc_item_option.do?JSONv2&sysparm_action=getRecords&sysparm_query=sys_id=$OID" \
    | jq -r '.records[] | "VALUE: \(.value)"'
done
```

The Business Justification text typically appears as one of the longer non-empty values. Other entries surface: subscription IDs, resource group names, requested role names, target UPNs.

## Common ticket queries

| Goal | Query |
| --- | --- |
| Lookup by ticket number | `?JSONv2&sysparm_action=getRecords&sysparm_query=number=RITM0297404` |
| Lookup by sys_id | `?JSONv2&sysparm_action=getRecords&sysparm_query=sys_id=<sys_id>` |
| List recent RITMs | `?JSONv2&sysparm_action=getRecords&sysparm_query=requested_for=<your_user_sys_id>^opened_atRELATIVE@dayofweek@ago@30` |
| RITMs linked to a REQ | `sc_req_item.do?JSONv2&sysparm_action=getRecords&sysparm_query=request=<req_sys_id>` |

Use `sysparm_display_value=true` to get human-readable strings instead of sys_ids in references (e.g. user UPNs instead of opaque IDs).

## Writing tickets on Leo's behalf

Do NOT use this skill's voice for the body. Load the `leo-writing-voice` skill explicitly and use its SNOW register section:

- No greetings, no apologies, no backstory, no closings
- No cross-system refs (don't cite ADO tickets in the body)
- No pre-solved URLs (don't tell the operator how to do their job)
- Bullet lists for resources and scopes
- Legible name before ID
- The body is a work order, not a narrative

The ticket UI form fields (Priority, Needed By, Cloud Provider) follow the form's own structure — only the Business Justification needs the SNOW register treatment.

## Known cookie failure modes

| Symptom | Cause | Fix |
| --- | --- | --- |
| `snow-refresh` reports `STALE_COOKIES` | Edge profile's Entra session expired (>90d or password rotation) | RDP to cloudpc, sign back in via `edge-snow-cdp` Edge window |
| Cookie file 0 bytes after refresh | CDP couldn't connect to port 9222 — Edge didn't start | `snow-refresh stop` then `snow-refresh start`; check `query session` shows LeonardoAcosta has an interactive session |
| 401 from `sc_req_item.do` with fresh cookies | LeonardoAcosta hasn't signed in to SNOW from this profile yet | One-time RDP + manual navigation to brown.service-now.com |
| `Insufficient rights to query records` on mtom | Cookie session lacks admin perms for that variable | Re-refresh — recent sessions occasionally have broader scope than older ones |
| Cookie file present but all SNOW calls 302 to login | SAML cookies expired but Edge process still warm | Just re-run `snow-refresh`; the timer will heal it within 6h anyway |
