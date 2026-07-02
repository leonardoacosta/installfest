# Design: CA policy forensics + parameter decisions

## Forensics (2026-07-02, read-only, bbins.com tenant f1289cc5-8456-4f28-8eab-700d1300fc5d)

Direct CA policy read is 403 for both identities (needs Security/Global Reader). The
enforced value comes from Azure's own AADSTS70043 error text, embedded in every genuine
firing: `maximum allowed lifetime for this request is 5184000` — **exactly 60 days**.
Every (issued-date, lifetime) pair across all 270 ws session files shows 5184000s, with
first failure 60-63 days after issue.

Key facts driving the design:

1. **60-day absolute refresh-token lifetime (CA sign-in frequency), both identities.**
   No short (12h/24h/7d) window exists. The audit's "~2,000 AADSTS errors / 20 per
   session" was retry/echo amplification of **~3 true expiry events in 3.5 months**.
2. **Independent clocks per identity.** bbadmin and o365 expire on different days
   (issue dates differ); each needs its own timer keyed off its own token issue date.
3. **Background refresh does NOT reset the window — confirmed empirically.** A token
   silently refreshed for 60 days, then the RT was rejected regardless of activity.
   Only interactive `az login --use-device-code` starts a new 60-day clock. No
   keep-alive scheme can extend it; do not build one.
4. **cloudpc/CAE egress is orthogonal.** The whitelisted IP satisfies the
   location/compliant-network control (the AADSTS50158 class, already solved by the
   SOCKS tunnel); it does not lengthen the 60-day lifetime.
5. **The real pain is failure handling, not cadence.** Expiry is detected only when an
   az call fails mid-task, and every subsequent call re-emits 70043 until manual
   re-auth — a retry storm. Error classes 50173/50076 were mx-side; 700082 (90-day
   inactivity) has zero live hits.

## Decisions

- **D1 — Nudge parameters (Req-1):** window 60d, lead margin 5d (fire from day 55),
  timer `OnCalendar=daily` + `Persistent=true`. Per-identity state keyed off token
  issue date read from MSAL cache metadata (timestamps only). The earlier 12h fallback
  and 15m timer cadence in draft tasks were pre-forensics guesses — superseded.
- **D2 — Fail-fast on 70043 (new Req-5):** the az wrapper detects AADSTS70043 in
  stderr, emits ONE loud notify with the exact re-login command, and touches a
  per-identity state marker; while the marker exists, subsequent az calls for that
  identity short-circuit with a one-line "re-auth required" error instead of hitting
  the network. Marker cleared on successful `az login` (wrapper detects `login` in
  argv + exit 0). This converts a retry storm into one actionable failure.
- **D3 — Issue-date source:** prefer parsing the issued-on date from the newest 70043
  text when present in state, else MSAL cache file metadata. Never read token values.

## TOTP feasibility probe (2026-07-02, read-only)

Goal: if the tenant permits a software-OATH (TOTP) method for these identities, a homelab
`oathtool` step could make `az-reauth` fully hands-free (no MFA tap) — emulating the
algorithm, not the app. Number-matched push has no script-derivable code; TOTP does.

**API path is blocked (inconclusive, not negative).** All Graph reads return 403
accessDenied — same wall as the CA policy read. The `az` CLI first-party token lacks
`UserAuthenticationMethod.Read` (for `/me/authentication/methods` and
`/softwareOathMethods`) and `Policy.Read.All` (for
`/policies/authenticationMethodsPolicy` SoftwareOath state); neither identity has
directory-reader consent for the az SP. v1.0 and beta both 403; both identities 403.

**Resolution is a manual browser check (30s, Leo-only):** at
`https://mysignins.microsoft.com/security-info` (signed in as each identity, via
Edge/ProxyBridge through cloudpc), click "Add sign-in method" and check whether
"Authenticator app or hardware token — code" (software OATH / TOTP) is offered.
- If offered -> enroll it, capture the base32 seed once, add a `oathtool --totp -b`
  step to `az-reauth` -> hands-free re-auth (new follow-on task).
- If not offered (expected — B&B likely disables software-OATH to force phishing-
  resistant number-matched push) -> Req-6 one-tap stands as the final answer; close the
  probe.

Expectation: not offered. This probe exists to convert that expectation into a
confirmed fact with one click, not to build on an assumption.

**RESULT (2026-07-02): software-OATH / hardware-token code IS allowed** (confirmed by
Leo at security-info). TOTP is enrollable → a script-derivable code is available. This
unlocks a hands-free path, but forks into two designs with different security cost — az
device-code login also requires username+password entry before MFA, so "fully tapless"
means automating the whole sign-in, not just the code.

- **D5-A (recommended) — TOTP replaces the phone, human stays at the browser.**
  `az-reauth` runs `oathtool --totp -b <seed>` and clipboards the code to the Mac next
  to the device code; Leo pastes it into the already-open Edge login (password via SSO).
  No password storage, no headless browser. Removes the real friction (phone reach +
  number matching) and keeps both factors from co-locating. Within the Req-6 boundary.
- **D5-B — fully tapless, headless browser + stored password.** Playwright on homelab
  drives device code → username → stored password → oathtool TOTP, zero interaction.
  Cost: BB password + TOTP seed both on homelab → two factors collapse to one on that
  host; automates the complete sign-in the CA policy keeps periodic-and-human; brittle
  vs login-page changes. Defensible on Leo's own org-permitted account, but maximal
  surface for a bi-monthly event.
- Seed hygiene (both): enroll software OATH once, store base32 seed in 1Password
  (`op`/`opsh`), never plaintext in the repo. az-reauth reads it via `op` at runtime.

**DECISION: D5-A chosen.** Independent 4-lens judge panel (2026-07-02) scored A over B
unanimously — security 8/2, ergonomics 8/6, maintenance 9/2, policy 8/2. Both options are
fully buildable (oathtool, op, node, playwright, chromium all already installed), so this
is a correctness call, not feasibility. B rejected: it co-locates password + seed on the
always-on homelab (collapses both MFA factors on host compromise → self-renewing ADO +
Azure RM + Graph/M365 takeover), hard-depends on Microsoft's churning AAD login DOM
(~50% chance of breakage per run at 12 runs/yr), reads to security as automation-as-user
with a stored corporate password, and saves only ~2 min/yr over A.

**B revives ONLY if** the re-auth window can open when no human is ever present (truly
unattended server) OR it becomes an IT-sanctioned service account under a PAM program.
Neither holds for Leo's interactive identity today.
