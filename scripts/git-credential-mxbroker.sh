#!/usr/bin/env bash
# git-credential-mxbroker.sh - git credential helper backed by homelab mx-broker.
#
# PURPOSE
#   On `git get` for dev.azure.com, fetch a fresh Azure DevOps access token from
#   homelab's mx-broker (served over a forwarded unix socket at ~/.mx/broker/broker.sock)
#   and hand it to git as the basic-auth password. The token's audience is the
#   Azure DevOps resource GUID; ADO accepts such Entra access tokens as the git
#   BASIC-AUTH PASSWORD (any non-empty username + token as password) -- so we
#   return username=mxbroker / password=<token> (Decision D1 = basic-auth).
#
# GRACEFUL FALLBACK (the whole point)
#   On EVERY failure path -- not the get op, wrong host, socket absent, curl
#   failure, 503/junk body, empty token -- this prints NOTHING and exits 0. git
#   then falls through to the next configured helper (osxkeychain, holding the
#   PAT). We never emit a partial/garbage credential. set -uo pipefail (NOT -e)
#   so a failing pipeline falls through to exit 0 instead of aborting mid-credential.
#
# SECURITY
#   The token is NEVER logged, echoed to stderr, or written to any file. The only
#   place it appears is the two credential lines on stdout consumed by git.
#
# D4 NOTE
#   The same pattern extends to Microsoft Graph for any Mac tool needing a Graph
#   token: query the socket with resource=graph&identity=o365 instead of
#   resource=ado&identity=o365. Out of scope for git->ADO, documented here for reuse.

set -uo pipefail

# Act only on the credential "get" operation; store/erase/anything else: exit 0.
[ "${1:-}" = "get" ] || exit 0

# Read key=value lines from stdin until a blank line; capture host.
host=""
while IFS='=' read -r key value; do
  [ -z "$key" ] && break
  [ "$key" = "host" ] && host="$value"
done

# Only handle Azure DevOps; let other helpers handle every other host.
[ "$host" = "dev.azure.com" ] || exit 0

# Forwarded broker socket. If the tunnel is down, fall through to osxkeychain.
SOCK="$HOME/.mx/broker/broker.sock"
[ -S "$SOCK" ] || exit 0

# Defense-in-depth: only trust a socket (and its parent dir) owned by us. If the
# 0700 invariant ever drifts and something else plants a socket here, fall through
# rather than POST the token request to an attacker-controlled listener.
[ -O "$SOCK" ] && [ -O "$HOME/.mx/broker" ] || exit 0

# Fetch the ADO token over the unix socket. Small --max-time so a hung tunnel
# never stalls git. On curl failure, fall through.
resp=$(curl -s --max-time 5 --unix-socket "$SOCK" \
  "http://localhost/token?resource=ado&identity=o365") || exit 0

# Robustly extract access_token with python3 (NO eval). A 503 body or junk yields
# empty (not an error) because the except swallows the parse failure. Emit ONLY a
# genuine, non-empty STRING token with no embedded newline/carriage return:
#   - null / number / missing  -> empty -> graceful PAT fallback (all-or-nothing)
#   - newline/CR in value      -> empty -> fail closed (no credential injection)
# sys.stdout.write (not print) appends no trailing newline of its own.
tok=$(printf '%s' "$resp" | /usr/bin/python3 -c 'import sys,json
try:
    t = json.load(sys.stdin).get("access_token")
    if isinstance(t, str) and t and "\n" not in t and "\r" not in t:
        sys.stdout.write(t)
except Exception:
    pass')

# No token -> graceful fallback to osxkeychain.
[ -n "$tok" ] || exit 0

# Success: print exactly two lines and nothing else.
printf 'username=mxbroker\n'
printf 'password=%s\n' "$tok"
