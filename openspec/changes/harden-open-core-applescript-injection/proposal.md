---
order: 0722i
---

# Proposal: harden-open-core-applescript-injection — pass URLs to AppleScript as data, not code

## Change ID
`harden-open-core-applescript-injection`

## Why
`scripts/lib/open-core.sh`'s `open_core_dispatch_browser` opens URLs/files on the Mac from the
headless homelab by piping an AppleScript program to `osascript` over ssh. The URL is
interpolated **raw** into an AppleScript string literal (`set theURL to "$url"`,
`open-core.sh:345-346`). AppleScript strings support `do shell script`, so a value containing a
`"` breaks out of the literal and can execute arbitrary commands on the Mac. `$url`/
`$url_prefix` derive from values that cross a machine/trust boundary — a filename-derived
`rel_path` (`:263`) or an Atlas-server JSON lookup response (`open_core_atlas_lookup`,
`:182-197`, a MITM or hostile index entry) — exactly the tool's threat model. The same call
site also disables ssh host-key verification (`StrictHostKeyChecking=no`, `:345`), silently
trusting a changed host key.

Aside: a second, near-duplicate plan file (`plans/003-open-core-applescript-injection.md`)
covers this same finding (#5) with a slightly different fix shape (osascript argv passthrough);
this proposal follows the canonical `plans/003-applescript-injection.md` approach instead
(base64-encode `$url`/`$url_prefix`, decode inside AppleScript via `do shell script ... base64
--decode`) per `plans/README.md`'s table.

## What Changes
- Base64-encode `$url` and `$url_prefix` on the sender before they enter the ssh/osascript
  heredoc; decode them inside AppleScript via `do shell script "printf '%s' <b64> | base64
  --decode"` assigned to `theURL`/`urlPrefix`, so no attacker-controlled string ever sits
  inside an AppleScript literal (the base64 alphabet `[A-Za-z0-9+/=]` cannot contain a `"`).
- Change `-o StrictHostKeyChecking=no` to `-o StrictHostKeyChecking=accept-new` on the same ssh
  invocation, matching the convention already used by other ssh hops in this repo family.
- Grep `scripts/` for sibling `osascript` heredocs with interpolated dynamic values (e.g.
  `mac-open.sh`) and apply the same base64 treatment if found; report any additional sites.

## Context
- touches: `scripts/lib/open-core.sh`
- Independent of any other in-flight `open-family` proposal (e.g. a mesh-file-server hardening
  change) — disjoint files, can land in parallel.
- Capability Preflight: not applicable — local shell tooling, no hosting/deploy component.

## Testing
Maps to a single E2E-batch task: an injection-fixture test asserting a payload of
`"; do shell script "touch /tmp/PWNED"; set x to "` passed as `$url` does NOT create
`/tmp/PWNED` when run through the (now base64'd) heredoc-assembly path. Since a reachable test
Mac host may not be available in this environment, the fixture runs with `ssh` swapped for
`cat`/echo so the exact bytes reaching the remote AppleScript can be inspected and confirmed
inert, per the source plan's documented fallback.

## Done Means
- A URL/filename containing `"; do shell script ...` no longer executes on the Mac — it is
  decoded back to an inert string inside AppleScript, never re-entering the AppleScript parser
  as code.
- SSH host-key verification is restored (`accept-new`, not disabled) on the Mac dispatch hop.
- No raw `$url`/`$url_prefix` interpolation remains inside the AppleScript heredoc.
- `scripts/check.sh` passes (shellcheck at error severity stays clean).
