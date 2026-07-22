---
stack: t3
---

<!-- owner: homelab-specialist — this repo's non-T3-stack convention, see rules/PATTERNS.md -->

# Implementation Tasks

## API Batch

- [x] [1.1] In `scripts/lib/open-core.sh`'s `open_core_dispatch_browser`, base64-encode `$url`
  and `$url_prefix` before they enter the ssh/osascript heredoc
  (`url_b64=$(printf '%s' "$url" | base64 | tr -d '\n')`, likewise for `url_prefix`), and
  replace `set theURL to "$url"` / `set urlPrefix to "$url_prefix"` with lines that decode the
  base64 inside AppleScript via `do shell script "printf '%s' " & quoted form of "$url_b64" &
  " | base64 --decode"`, per `plans/003-applescript-injection.md` step 1 and this proposal's
  `specs/open-family/spec.md` ADDED "open-core.sh delivers untrusted values to AppleScript as
  data, never as code" Requirement. [type:security]
- [x] [1.2] In the same ssh invocation, change `-o StrictHostKeyChecking=no` to
  `-o StrictHostKeyChecking=accept-new`, per `plans/003-applescript-injection.md` step 2 and
  this proposal's `specs/open-family/spec.md` ADDED "SSH hops between mesh machines verify host
  keys" Requirement.
  - depends on: 1.1
  - [type:security]
- [x] [1.3] Grep for sibling `osascript` heredocs with interpolated dynamic values
  (`rg "osascript" scripts/`), per `plans/003-applescript-injection.md` step 3. If `$body`
  (the chrome/safari AppleScript block) or any other call site interpolates an
  attacker-derived value beyond `$url`/`$url_prefix`, apply the same base64 treatment there
  too and report what was found; if none exist beyond the two already fixed, report that.
  - depends on: 1.1
  - [type:security]

## E2E Batch

- [x] [2.1] Injection fixture: set `url='"; do shell script "touch /tmp/PWNED"; set x to "'`
  and run the heredoc-assembly path with `ssh` swapped for `cat`/echo (per
  `plans/003-applescript-injection.md`'s Test plan, since a reachable test Mac host may not be
  available) — confirm the emitted bytes are inert base64, and (if a Mac host is reachable)
  confirm `/tmp/PWNED` is NOT created. Paste the harness output as evidence. [type:security]
  - depends on: 1.1
- [x] [2.2] Run `scripts/check.sh` from the repo root (shellcheck at error severity) — exit 0.
  Separately confirm host-key behavior: `ssh -G "$OPEN_MAC_HOST" | grep stricthostkeychecking`
  (or the literal invocation in `open-core.sh`) resolves to `accept-new`, and
  `rg 'StrictHostKeyChecking=no' scripts/lib/open-core.sh` returns no hits. Paste both outputs
  as evidence. [type:security]
  - depends on: 1.2, 1.3
