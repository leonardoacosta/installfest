## ADDED Requirements

### Requirement: open-core.sh delivers untrusted values to AppleScript as data, never as code
`open_core_dispatch_browser` (`scripts/lib/open-core.sh`) SHALL deliver `$url` and
`$url_prefix` to the remote AppleScript program as base64-encoded data, decoded inside
AppleScript via `do shell script ... base64 --decode`, rather than interpolating either value
raw into an AppleScript string literal. This applies regardless of whether the value
originates from a filename-derived path or an Atlas-server JSON lookup response — both cross a
machine/trust boundary and MUST be treated as untrusted. No dynamic value reaching the
`osascript` heredoc may be capable of terminating an AppleScript string literal.

#### Scenario: an embedded quote does not break out of the AppleScript literal
- Given: `$url` contains the payload `"; do shell script "touch /tmp/PWNED"; set x to "`
- When: `open_core_dispatch_browser` assembles and sends the ssh/osascript heredoc
- Then: the payload is base64-encoded before transmission, decoded back to an inert string
  inside AppleScript, and no shell command executes on the Mac (`/tmp/PWNED` is not created)

#### Scenario: a normal URL still opens correctly
- Given: `$url` is a well-formed `http://` URL with no special characters
- When: `open_core_dispatch_browser` runs
- Then: the Mac's browser opens/focuses that exact URL, unchanged by the base64 round-trip

### Requirement: SSH hops between mesh machines verify host keys
Every ssh invocation in `scripts/lib/open-core.sh` that connects to a mesh machine SHALL use
`-o StrictHostKeyChecking=accept-new` (pin-on-first-use), never `StrictHostKeyChecking=no`
(which silently trusts a changed or spoofed host key).

#### Scenario: host key checking is not disabled
- Given: `open_core_dispatch_browser`'s ssh invocation to `$OPEN_MAC_HOST`
- When: the command is inspected (`ssh -G` or a static grep of the script)
- Then: `StrictHostKeyChecking` resolves to `accept-new`, and no `StrictHostKeyChecking=no`
  remains anywhere in the file

#### Scenario: first connection to a known mesh host still succeeds non-interactively
- Given: a mesh host not yet present in `known_hosts`
- When: `open_core_dispatch_browser` connects to it for the first time
- Then: the connection succeeds without an interactive prompt, and the host key is pinned for
  future connections
