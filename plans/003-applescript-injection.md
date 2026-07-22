# Plan 003 — Fix AppleScript command injection in the open-family SSH hop

**Written against commit:** `d441448` — if the excerpt no longer matches, STOP and report drift.
**Finding:** #5 — unescaped URL interpolated into an `osascript` heredoc over ssh; also `StrictHostKeyChecking=no` on the Mac hop (MED confidence).
**Priority:** 3 of 8. Independent; small and self-contained.

## Why this matters

`scripts/lib/open-core.sh` opens URLs/files on the Mac from the headless homelab by piping
an AppleScript program to `osascript` over ssh. The URL is interpolated raw into an
AppleScript string literal. AppleScript supports `do shell script`, so a URL (or filename,
or Atlas-server JSON value) containing a `"` breaks out of the literal and can run arbitrary
commands on the Mac. The value crosses a machine boundary (filename-derived `rel_path` at
`:263`, or an Atlas lookup response at `:182-197`), which is exactly the tool's threat model.

## Current state (verified excerpt)

`scripts/lib/open-core.sh:344-350`:

```bash
  (
    ssh -o ConnectTimeout=3 -o StrictHostKeyChecking=no "$OPEN_MAC_HOST" "osascript -" <<APPLESCRIPT
set theURL to "$url"
set urlPrefix to "$url_prefix"
set tabFound to false
$body
APPLESCRIPT
  ) </dev/null >/dev/null 2>>"$LOG_FILE" & disown
```

`$url` is built from `rel_path` (filename-derived, `:263`) or `open_core_atlas_lookup`
(`:182-197`, a JSON response from an Atlas server — a MITM or hostile index entry).

## Conventions to match

- The file already `printf`s and quotes elsewhere; keep style consistent (2-space indent, `local` vars).
- Other ssh hops in this repo family use `StrictHostKeyChecking=accept-new` (grep to confirm: `rg "StrictHostKeyChecking" scripts/`), which pins on first use — that is the safer default to standardize on here.
- Must pass `shellcheck --severity=error`.

## Steps

1. **Pass the URL as data, not code.** Replace the interpolation of `$url`/`$url_prefix`
   into the AppleScript with values delivered out-of-band so no attacker-controlled string
   ever sits inside an AppleScript literal. Simplest robust approach: base64-encode on the
   sender, decode inside AppleScript.
   - Sender: `url_b64=$(printf '%s' "$url" | base64)` and likewise for `url_prefix`.
   - In the heredoc, replace `set theURL to "$url"` with a line that sets it from the
     decoded base64, e.g.:
     `set theURL to (do shell script "printf %s " & quoted form of "$url_b64" & " | base64 --decode")`
     — here `$url_b64` is safe because base64 output is `[A-Za-z0-9+/=]` only, so it cannot
     contain a `"`. (`quoted form of` further protects it.)
   - Do the same for `urlPrefix`.
   - If `$body` (the chrome/safari AppleScript block) also interpolates any
     attacker-derived value, audit it the same way; if it only uses the two vars above,
     it's fine.
   Verify: set `url='"; do shell script "touch /tmp/PWNED"; set x to "'` locally and run the
   path in dry-run/echo mode (or against a test Mac host) — confirm `/tmp/PWNED` is NOT
   created and the literal string is treated as a URL.

2. **Restore host-key verification.** Change `StrictHostKeyChecking=no` at `:345` to
   `StrictHostKeyChecking=accept-new`. This still works for the first connection to a known
   mesh host but stops silently trusting a changed key (MITM). Verify: `ssh -G` shows the
   option applied; a normal open still succeeds.

3. **Check sibling call sites.** `rg "osascript" scripts/` — if `open-core.sh` has other
   `osascript -` heredocs with interpolated values (or `mac-open.sh` does), apply the same
   base64 treatment. Report any you find and fix; if the pattern is widespread, note it.

4. **Run the gate:** `scripts/check.sh` → exit 0 (shellcheck covers this file).

## Boundaries

- **In scope:** `scripts/lib/open-core.sh`, and any other file surfaced by the `osascript` grep in step 3.
- **Out of scope:** `mac-open.sh`'s file-server (plan 002 owns it), the Atlas server itself, the URL-resolution logic (`resolve_target`, `open_core_atlas_lookup`) — you are hardening the sink, not the source.

## Done criteria (machine-checkable)

- `rg 'set theURL to "\$url"' scripts/lib/open-core.sh` → no hits (raw interpolation gone).
- `rg 'base64' scripts/lib/open-core.sh` → present in the AppleScript path.
- `rg 'StrictHostKeyChecking=no' scripts/lib/open-core.sh` → no hits.
- Injection fixture (step 1) run against a test host / echo harness does NOT execute the payload.
- `scripts/check.sh` → exit 0.

## Test plan

No harness exists for this script. The injection fixture in step 1 is the required test —
run it and paste the result (payload not executed) into your report. If a test Mac host
isn't available, run the heredoc assembly with `ssh` replaced by `cat` to show the exact
bytes that would be sent, and confirm the payload is inert base64.

## Maintenance note

Any future AppleScript-over-ssh call must use the base64-as-data pattern established here —
add a one-line comment above the heredoc saying so. This is the same class of issue as
cc-tmux's send-keys seeding (plan 007): untrusted text reaching an interpreter.

## Escape hatch

- If `do shell script ... base64 --decode` behaves oddly on the target macOS AppleScript version (quoting of `quoted form of` inside a heredoc is fiddly): fall back to passing the URL via ssh stdin as a separate data stream read by AppleScript, and report that you took the fallback. Do NOT ship the raw-interpolation version.
