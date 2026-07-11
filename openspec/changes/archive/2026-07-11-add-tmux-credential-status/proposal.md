# Proposal: Add tmux credential status bar element

> **SUPERSEDED, archived 2026-07-12.** The shell-script architecture this proposal describes
> (`home/dot_local/bin/executable_tmux-nexus-creds`, `curl`+`jq` against `localhost:7402`) no
> longer exists — it was replaced by `apps/cc-tmux` (`cc-tmux usage`, Python, port 7400, part of
> the `[CAPABILITY] cc-tmux` epic `if-bqw`), which ships the same account+5H/7D display this
> proposal asked for, correctly. Tasks 1.1-3.3 describe the retired script and are moot, not
> "done" in the current architecture. Task 4.1 (account-switcher popup) remains a real, un-shipped
> idea — re-file it fresh against `cc-tmux` if still wanted; it doesn't belong on this proposal's
> dead architecture. See `cc-tmux-session-usage-bars` (successor proposal) and
> `docs/diagrams/cc-tmux-sources.html` for the current design.

## Change ID
`add-tmux-credential-status`

## Summary
Add a tmux status bar element that displays the active nexus credential account with color-coded 5-hour and 7-day usage gauges, powered by a shell script querying the local nexus-agent daemon. Includes a future keybinding for an account-switching popup.

## Context
- Extends: `home/dot_config/tmux/tmux.conf.tmpl`, One Hunter Vercel theme config
- Related: nexus-agent (Rust daemon on `localhost:7402`), chezmoi script deployment (`dot_local/bin/executable_*`)

## Motivation
Active credential usage is invisible during development sessions. Developers hit rate limits unexpectedly because there is no ambient indicator of how close they are to 5-hour or 7-day ceilings. A persistent tmux status element provides at-a-glance awareness without switching context. The nexus-agent already exposes the data via a local HTTP endpoint; we just need to surface it.

## Requirements

### Req-1: Create tmux-nexus-creds status script
Create `home/dot_local/bin/executable_tmux-nexus-creds` (deploys to `~/.local/bin/tmux-nexus-creds`). The script:
- Queries `GET localhost:7402/credentials` via `curl` with a short timeout (1-2 seconds)
- Parses the JSON response to extract the active account name, its `five_hour.utilization`, and `seven_day.utilization`
- Outputs a tmux-formatted string: `<account> 5H:<pct>% 7D:<pct>%`
- Color-codes each percentage using the One Hunter Vercel palette:
  - `#5BD1B9` (cyan) when utilization < 50%
  - `#FAC760` (yellow) when utilization is 50-80%
  - `#E61F44` (red) when utilization > 80%
- Account name label uses dim color `#454D54`

### Req-2: Graceful degradation when nexus-agent is down
When the `curl` request fails (daemon stopped, network error, timeout):
- Output a dim "offline" indicator (`#[fg=#454D54]offline`) or output nothing
- Must not produce error text, stall tmux rendering, or leave stale output
- The curl timeout ensures the script returns within 2 seconds maximum

### Req-3: Update tmux status-interval
Change `status-interval` in `home/dot_config/tmux/tmux.conf.tmpl` from `15` to `2` so credential utilization updates are near-real-time.

### Req-4: Add script to status-right
Include `#(tmux-nexus-creds)` in the tmux `status-right` configuration (either in the theme conf or in `tmux.conf.tmpl`). Position it before the clock segment so the layout reads: `[nexus creds] [time] [hostname]`.

### Req-5 (Future): Account switching popup
Add a prefix keybinding (e.g. `prefix+a`) that opens a `display-popup` listing all accounts from the `/credentials` response, allowing the user to select one and trigger `POST localhost:7402/credentials/swap` with `{"account":"<name>"}`. This is blocked on the nx POST endpoint being available.

## Scope
- **IN**: New shell script, tmux config changes (status-interval, status-right), chezmoi deployment
- **OUT**: Changes to nexus-agent itself, changes to the One Hunter Vercel theme file beyond status-right, new tmux plugins

## Impact
| Area | Change |
|------|--------|
| Status bar | Adds credential account + usage gauges to status-right |
| status-interval | 15s -> 2s (more frequent redraws) |
| New file | `home/dot_local/bin/executable_tmux-nexus-creds` deployed to `~/.local/bin/` |
| Dependencies | Requires `curl` and `jq` (or awk/sed JSON parsing) on PATH |
| tmux performance | Script runs every 2s; curl timeout caps execution at 2s worst-case |

## Risks
| Risk | Mitigation |
|------|-----------|
| nexus-agent not running | Graceful fallback to dim "offline" or empty output (Req-2) |
| curl/jq not installed | Both are standard on macOS and Arch; `run_once_install-packages.sh.tmpl` already installs them |
| 2s status-interval increases tmux CPU | Script is lightweight (single curl + jq pipe); timeout prevents hangs |
| JSON response format changes | Script should fail gracefully on unexpected JSON (output nothing rather than broken formatting) |
| stale data if nexus-agent stops mid-session | `seconds_since_polled` field could be checked; if too stale, show warning color |
