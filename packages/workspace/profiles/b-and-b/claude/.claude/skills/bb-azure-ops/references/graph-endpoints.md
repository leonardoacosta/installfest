# Microsoft Graph endpoints — B&B patterns

> Load this file when working with the Microsoft Graph API in a B&B context (Teams chat reads, Outlook search, Entra app registrations, directory role queries). Skip when working on SNOW or ADO — those have their own patterns.

## Token files and refresh

| Token file | Identity | Best for |
| --- | --- | --- |
| `~/.graph-token.json` | O365 (`leonardo.acosta@bridgespecialty.com`) | Teams chats, Outlook search, OneDrive (has `Chat.Read`) |
| `~/.graph-bbadmin-token.json` | BBAdmin (`BBAdminLAcosta@bbins.com`) | App registrations, directory operations (has `Application.ReadWrite.All`) |

Refresh via cloudpc PowerShell — the refresh script lives on the SSH service account profile, but writes are scoped to its own home directory:

```bash
# Refresh on cloudpc
ssh cloudpc 'powershell -NoProfile -File C:\Users\leo.346-CPC-QJXVZ\graph-token-refresh.ps1 -Which O365'
ssh cloudpc 'powershell -NoProfile -File C:\Users\leo.346-CPC-QJXVZ\graph-token-refresh.ps1 -Which BBAdmin'

# scp the refreshed files back to the Linux side
scp cloudpc:'C:/Users/leo.346-CPC-QJXVZ/.graph-token.json'     ~/.graph-token.json
scp cloudpc:'C:/Users/leo.346-CPC-QJXVZ/.graph-pim-token.json' ~/.graph-bbadmin-token.json
```

The refresh script handles MSAL token exchange. Refresh tokens last ~90 days per identity; after that you need device-code re-auth via `az --as-<ident> login` and a fresh PowerShell refresh.

## Canonical call pattern

```bash
TOKEN=$(jq -r '.access_token' ~/.graph-token.json)
curl -s --socks5-hostname localhost:1080 \
  -H "Authorization: Bearer $TOKEN" \
  "https://graph.microsoft.com/v1.0/<endpoint>"
```

Always use `--socks5-hostname` (DNS resolves through the proxy) — not `--socks5` (which resolves locally and may fail CAE).

## BBAdmin token capability cheat sheet (confirmed 2026-05-21)

Confirmed scopes include: `Application.ReadWrite.All`, `Directory.ReadWrite.All`, `AppRoleAssignment.ReadWrite.All`, `Group.ReadWrite.All`, `User.ReadWrite.All`, `Mail.Read`, `Mail.Send`, and ~60 more.

NOT in scopes: `DelegatedPermissionGrant.ReadWrite.All`, `Chat.Read`.

This means with BBAdmin:

- App registration manifest PATCH: **works** (`Application.ReadWrite.All`)
- Admin consent grant (`oauth2PermissionGrants` POST/PATCH): **FAILS 403** — needs `DelegatedPermissionGrant.ReadWrite.All` or active directory role like Cloud App Admin
- Teams chat read: **FAILS** — switch to the O365 token for `Chat.Read`

When choosing: O365 token for user-context reads (Teams, Outlook, calendars), BBAdmin token for admin/directory operations (app regs, role assignments, group membership).

## Common endpoints

| Goal | Endpoint |
| --- | --- |
| List recent Teams chats | `/v1.0/me/chats?$top=50&$expand=members` (NO `$orderby` — see gotcha below) |
| Messages in a chat | `/v1.0/me/chats/{chatId}/messages?$top=50&$filter=lastModifiedDateTime%20gt%20<ISO>` |
| List joined teams / channels | `/v1.0/me/joinedTeams` then `/v1.0/teams/{tid}/channels` |

## Searching Teams = enumerate + scan (NOT the Search API) — confirmed 2026-05-24

There is **no usable server-side Teams message search** with our tokens. The canonical method (see cloudpc `graph-teams.ps1`) is:

1. `GET /me/chats?$expand=members&$top=50` to find the target chat by topic/members (chat messages work with `Chat.Read`, which the O365 token HAS).
2. `GET /me/chats/{chatId}/messages?$top=N` and grep/scan client-side.
3. For channels: `GET /teams/{tid}/channels/{cid}/messages` — needs `ChannelMessage.Read.All` (admin consent, NOT granted) → 403. Chat messages do NOT.

**Do NOT use `POST /search/query` with `entityTypes:["chatMessage"]`** — it requires `Chat.Read` **AND** `ChannelMessage.Read.All` together; the O365 token has only the former, so it returns 403 Forbidden. This is a dead end, not a token-refresh problem.

### Two stale-reference gotchas (corrected 2026-05-24)

- `$orderby=lastUpdatedDateTime desc` on `/me/chats` now returns 400 `QueryOptions to order by 'lastUpdatedDateTime' is not supported`. Drop `$orderby`; sort client-side.
- `lastMessagePreview.createdDateTime` comes back `null` in the `$expand=members` listing — you cannot prioritize chats by recency from the list payload. Pull messages per candidate chat instead.
| Search Outlook by subject | `/v1.0/me/messages?$search="subject:<query>"` |
| Outlook by conversation ID | `/v1.0/me/messages?$filter=conversationId%20eq%20'<id>'` |
| List app registrations by prefix | `/v1.0/applications?$filter=startswith(displayName,'<prefix>')&$select=id,appId,displayName` |
| Get one app registration | `/v1.0/applications/{objectId}` |
| Service principal for an app | `/v1.0/servicePrincipals(appId='<appId>')` |
| oauth2PermissionGrants for a SP | `/v1.0/oauth2PermissionGrants?$filter=clientId%20eq%20'<spOid>'` |
| Resolve Graph scope IDs to names | `/v1.0/servicePrincipals(appId='00000003-0000-0000-c000-000000000000')?$select=id,oauth2PermissionScopes` |
| List directory role holders | `/v1.0/roleManagement/directory/roleAssignmentScheduleInstances?$filter=roleDefinitionId%20eq%20'<roleId>'&$expand=principal` |
| List PIM-eligible role holders | `/v1.0/roleManagement/directory/roleEligibilityScheduleInstances?$filter=roleDefinitionId%20eq%20'<roleId>'&$expand=principal` |
| User's transitive group membership | `/v1.0/users/{oid}/transitiveMemberOf?$select=id,displayName,isAssignableToRole` |

## URL encoding gotcha

**Bare spaces in `$filter` cause 400.** Always URL-encode as `%20` when constructing query strings. Examples:

```bash
# WRONG — bare spaces, returns 400
curl ... "https://graph.microsoft.com/v1.0/oauth2PermissionGrants?\$filter=clientId eq '$SP'"

# RIGHT — encoded
FILTER=$(python3 -c "import urllib.parse; print(urllib.parse.quote(\"clientId eq '$SP'\"))")
curl ... "https://graph.microsoft.com/v1.0/oauth2PermissionGrants?\$filter=$FILTER"

# Also right — manual %20 substitution
curl ... "https://graph.microsoft.com/v1.0/.../?\$filter=clientId%20eq%20'$SP'"
```

Inside Python/Node, prefer `urllib.parse.quote` or equivalent over manual string mashing — it handles all OData special chars (`,`, `:`, `'`).

## Well-known directory role definition IDs

For admin-consent escalation queries:

| Role | roleDefinitionId |
| --- | --- |
| Global Administrator | `62e90394-69f5-4237-9190-012177145e10` |
| Application Administrator | `9b895d92-2cd3-44c7-9d02-a6ac2d5ea5c3` |
| Cloud Application Administrator | `158c047a-c907-4556-b7ef-446551a6b5f7` |
| Privileged Role Administrator | `e8611ab8-c189-46e8-94e1-60213ab1f814` |

## Admin consent escalation (snapshot 2026-05-21)

When BBAdmin's 403 blocks admin-consent operations:

| Role | Active holders (immediate ask) | Eligible (PIM-activatable) |
| --- | --- | --- |
| Global Administrator | Waheem Rahman, brivers, simeon (svc) | Jack Turner, Ravi Pinnamaneni, John Evans, +11 |
| Cloud Application Admin | Dan Osborne, Dan Thurston, Apple Service | Tim Settar |
| Application Admin | service accounts only | Nate Benson, Ian Castro, Lucas Lindaman, +4 |
| Privileged Role Admin | svc accounts + groups | (none direct) |

Pre-built admin-consent URL pattern (sign in as one of the above, click Accept):

```
https://login.microsoftonline.com/bbins.com/adminconsent?client_id=<appId>
```

This bypasses portal navigation — shareable directly via Teams to a consent-capable colleague. The shared URL renders the same consent prompt the portal would.

## Pagination

Graph returns `@odata.nextLink` for paged results. Loop until absent:

```bash
URL="https://graph.microsoft.com/v1.0/me/chats?\$top=50"
while [ -n "$URL" ]; do
  RESPONSE=$(curl -s --socks5-hostname localhost:1080 -H "Authorization: Bearer $TOKEN" "$URL")
  echo "$RESPONSE" | jq '.value[]' > /tmp/chats-page.json
  URL=$(echo "$RESPONSE" | jq -r '."@odata.nextLink" // empty')
done
```

For Teams chat messages, expect ~5-15 pages over a 2-week window.

## Pattern for app registration permission audit

Common task: confirm an app reg has the right Graph scopes granted. Three queries needed:

1. **Manifest declares** — `GET /applications/{objectId}` → `requiredResourceAccess[?resourceAppId=='00000003-...'].resourceAccess[]`
2. **Service principal exists** — `GET /servicePrincipals(appId='<appId>')` → confirms the app has been instantiated as an enterprise app
3. **Admin consent granted** — `GET /oauth2PermissionGrants?$filter=clientId eq '<spOid>'` filter on `consentType='AllPrincipals'` → returns space-separated `scope` string

Compare (1) to (3). If a scope is declared but not in granted, that's the consent gap — Therese's Mail.Read story on `346-OfficeIndextoPIPs-DEV` was exactly this pattern.

### Per-satellite app-reg / CIAM config lives in the manifest `appReg:` slice (registry retired)

The central `app-reg-registry.json` was **DELETED** (commits 92d9bc8e + d5bc21dc, 2026-06-30). App-registration + CIAM config (clientId, tenantId, authority, redirect URIs, the `AzureAd-*` values seeded into KV + app settings) now lives **per-satellite, per-env** in each satellite's manifest: `.azuredevops/satellites/<sat>/manifest.yml` under an **`appReg:`** key (keyed by env). Five Bicep consumers were repointed to `loadYamlContent(<manifest>).appReg[env]` (sats fb / se / ba / bo / tb). When editing an app-reg or CIAM value, edit the **satellite manifest `appReg:` slice** — there is no longer a single central registry to grep. The Bicep deploy reads the manifest, seeds the `AzureAd-*` KV secrets, and resolves them into the app's settings (verified for TheBridge STAGE against the interim TEST CIAM tenant — KV secrets + TBApi app settings both resolved). Confirmed 2026-06-30.
