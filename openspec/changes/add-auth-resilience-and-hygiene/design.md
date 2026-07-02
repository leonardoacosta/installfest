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
