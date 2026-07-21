---
stack: t3
---
<!-- beads:epic:if-870u -->
<!-- beads:feature:if-b920 -->

<!-- stack: one of t3 | cc-meta | effect | dotnet — see commands/apply/references/stacks.md § "Stack vocabulary crosswalk" for the full tasks.md-stack:/--stack-profile/detect_stack() mapping -->

# Implementation Tasks

## DB Batch

- [x] [1.1] [owner:homelab-specialist] Add a Darwin-only chezmoi run_onchange script (`home/run_onchange_darwin-screen-sharing.sh.tmpl`, gated on `{{ if eq .chezmoi.os "darwin" }}`) that idempotently enables macOS Screen Sharing via `sudo launchctl enable system/com.apple.screensharing` + `sudo launchctl load -w /System/Library/LaunchDaemons/com.apple.screensharing.plist` — never the MDM-restricted `kickstart -activate -configure` ARD flow. [beads:if-h15x]

## API Batch

- [x] [2.1] [owner:homelab-specialist] Add the `tigervnc` package to homelab's Arch package list in `scripts/install-arch.sh` (client only — provides `vncviewer`; no server component for homelab). [beads:if-vsph]

## UI Batch

- [x] [3.1] [owner:homelab-specialist] Add `scripts/vnc-mac.sh`, a front-door script (matching `mac-open.sh`/`view.sh` conventions — usage header, env var overrides, clear failure message) that resolves the Mac's Tailscale hostname from the existing `mac` SSH Host alias (`ssh -G mac`, never a second hardcoded copy of `macbook.tail296462.ts.net`) and launches `vncviewer` against it. [beads:if-5avp]
  - depends on: 1.1, 2.1

## E2E Batch

- [x] [4.1] [owner:homelab-specialist] [type:test] Manually verify on real hardware: with Screen Sharing enabled on the Mac (task 1.1) and `tigervnc` installed on homelab (task 2.1), running `scripts/vnc-mac.sh` from homelab opens a live, authenticated VNC session to the Mac over Tailscale. Paste the terminal output / a screenshot as verification evidence. [beads:if-ptpz]
  - depends on: 3.1
- [x] [4.2] [owner:homelab-specialist] Add a `vnc-mac` entry to CLAUDE.md's "Front-Door Tools" section, matching the existing `mac-open`/`view`/`copen` entries. [beads:if-kqbm]
  - depends on: 3.1
