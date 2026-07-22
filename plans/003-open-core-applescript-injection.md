# Plan 003 — Fix AppleScript injection and host-key bypass in open-core.sh

**Written against commit:** `d441448` — if the excerpt no longer matches, STOP and report drift.
**Finding:** #5 (MED confidence, verified).
**Priority:** 3 of 8. Independent; can land in parallel with 002.

## Why this matters

`scripts/lib/open-core.sh` is the shared library behind the `ropen`/`gopen`/`sopen`
"open on my Mac" family. To open a URL in the Mac's browser it pipes an AppleScript
program to `osascript` over ssh — and interpolates the URL **raw** into the AppleScript
source. AppleScript strings support `do shell script`, so breaking out of the string
literal with a `"` escalates to command execution on the Mac. The URL derives from
file paths (`rel_path`, `:263`) and from Atlas server JSON responses
(`open_core_atlas_lookup`, `:182-197`) — i.e. values that cross machine boundaries.
The same block also disables ssh host-key verification.

## Current state (verified excerpt)

`scripts/lib/open-core.sh:344-352`:

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

`$body` is assembled from `$chrome_block`/`$safari_block` (static script text, fine).
`$url` and `$url_prefix` are the injection points.

## Conventions to match

- This repo consistently prefers passing data out-of-band over escaping-in-place: wavetui
  delivers prompt text via `tmux load-buffer` stdin (`apps/wavetui/internal/dispatch/tmux.go:260-270`),
  and `view.sh` uses `printf '%q'`. Follow that spirit: move the URL out of the code channel.
- shellcheck at error severity must stay clean (`scripts/check.sh` gates it).

## Steps

1. **Read the full function** containing the heredoc (open `scripts/lib/open-core.sh`,
   locate the function around `:320-355`) plus every assignment of `url` and `url_prefix`
   feeding it. Confirm the only dynamic values entering the AppleScript are those two.
   If `$body` turns out to contain other interpolated dynamic values, STOP and report.

2. **Pass the URL as data, not code.** Replace the two `set theURL to "$url"` lines with
   AppleScript that reads arguments, and pass the values as osascript argv:

   - Change the ssh command to: `ssh ... "$OPEN_MAC_HOST" "osascript - \"\$@\"" _ "$url" "$url_prefix"` — careful: argv through ssh re-enters a remote shell parse. The robust variant this plan REQUIRES instead: base64 the values locally
     (`b64url=$(printf '%s' "$url" | base64 | tr -d '\n')`), interpolate only the base64
     string (alphabet `[A-Za-z0-9+/=]`, incapable of AppleScript string breakout), and
     decode inside AppleScript via `do shell script "printf '%s' '<b64>' | base64 -D"`
     assigned to `theURL`.
   - Both dynamic values get the same treatment.
   - Add a one-line comment: `# URL crosses ssh+AppleScript as base64 — raw interpolation was an injection vector (plan 003).`

3. **Restore host-key checking.** Replace `-o StrictHostKeyChecking=no` with
   `-o StrictHostKeyChecking=accept-new`. The mesh hosts are long-lived; accept-new
   pins on first contact without interactive prompts breaking the `& disown` flow.

4. **Syntax + lint:** `bash -n scripts/lib/open-core.sh` and `scripts/check.sh` → exit 0.

5. **Behavioral verification (needs the Mac reachable; otherwise record as pending):**
   - Normal case: `ropen <some-file>` opens the expected URL on the Mac.
   - Injection case: create a file whose name contains `"` and `$(...)` (e.g.
     `touch '/tmp/a"& do shell script "touch /tmp/pwned"b