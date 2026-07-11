# Implementation Tasks

## Script Batch

- [x] [1.1] [P-1] Create `home/dot_local/bin/executable_tmux-nexus-creds` — shell script that queries `GET localhost:7402/credentials`, parses JSON with jq, outputs tmux-formatted string with color-coded utilization percentages (cyan <50%, yellow 50-80%, red >80%) [owner:general-purpose]
- [x] [1.2] [P-1] Test script output formatting with curl mock — verify correct tmux escape sequences, color thresholds, and percentage rendering [owner:general-purpose]
- [x] [1.3] [P-1] Test graceful degradation when nexus-agent is stopped — verify script outputs dim "offline" or empty string, no errors, returns within 2s [owner:general-purpose]

## Config Batch

- [x] [2.1] [P-1] Update `home/dot_config/tmux/tmux.conf.tmpl` — change `status-interval` from `15` to `2` [owner:general-purpose]
- [x] [2.2] [P-1] Update status-right to include `#(tmux-nexus-creds)` — position before clock segment in theme conf or tmux.conf.tmpl [owner:general-purpose]

## Verification Batch

- [x] [3.1] [P-1] Run `chezmoi apply` and verify script is deployed to `~/.local/bin/tmux-nexus-creds` with executable permissions [owner:general-purpose]
- [x] [3.2] [P-1] Verify in live tmux session — credential status appears in status bar with correct colors [owner:general-purpose]
- [x] [3.3] [P-2] Verify graceful degradation in live tmux — stop nexus-agent and confirm status bar shows "offline" or clears without errors [owner:general-purpose]

## Future (Blocked)

- [ ] [4.1] [P-3] [deferred] Add `prefix+a` keybinding for `display-popup` account switcher — blocked on `POST localhost:7402/credentials/swap` endpoint availability in nx [owner:general-purpose]
